/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/go-logr/logr"
	autoscalev1 "github.com/kubernetes/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"github.com/mitchellh/hashstructure"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
	"github.com/lwolf/konsumerator/pkg/errors"
	"github.com/lwolf/konsumerator/pkg/helpers"
	"github.com/lwolf/konsumerator/pkg/providers"
)

var (
	managedPartitionAnnotation  = "konsumerator.lwolf.org/partition"
	disableAutoscalerAnnotation = "konsumerator.lwolf.org/disable-autoscaler"
	deploymentGeneration        = "konsumerator.lwolf.org/deployment-generation"
	deployOwnerKey              = ".metadata.controller"
	defaultPartitionEnvKey      = "KONSUMERATOR_PARTITION"
	gomaxprocsEnvKey            = "GOMAXPROCS"
	apiGVStr                    = konsumeratorv1alpha1.GroupVersion.String()
)

func debugExtractNames(deploys []*appsv1.Deployment) (names []string) {
	for _, d := range deploys {
		names = append(names, d.Name)
	}
	return
}

func shouldUpdateLag(consumer *konsumeratorv1alpha1.Consumer) bool {
	status := consumer.Status
	if status.LastSyncTime == nil {
		return true
	}
	timeToSync := metav1.Now().Sub(status.LastSyncTime.Time) > consumer.Spec.Autoscaler.Prometheus.MinSyncPeriod.Duration
	if timeToSync {
		return true
	}
	return false
}

// ConsumerReconciler reconciles a Consumer object
type ConsumerReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=konsumerator.lwolf.org,resources=consumers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konsumerator.lwolf.org,resources=consumers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;update;patch;delete;list
// +kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get

func (r *ConsumerReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("consumer", req.NamespacedName)

	var consumer konsumeratorv1alpha1.Consumer
	if err := r.Get(ctx, req.NamespacedName, &consumer); err != nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, errors.IgnoreNotFound(err)
	}
	var managedDeploys v1.DeploymentList
	if err := r.List(ctx, &managedDeploys, client.InNamespace(req.Namespace), client.MatchingField(deployOwnerKey, req.Name)); err != nil {
		log.Error(err, "unable to list managed deployments")
		return ctrl.Result{}, err
	}

	var err error
	var lagProvider providers.LagSource
	lagProvider = providers.NewLagSourceDummy(*consumer.Spec.NumPartitions)
	autoscalerDisabled := len(consumer.Annotations[disableAutoscalerAnnotation]) > 0
	if !autoscalerDisabled && consumer.Spec.Autoscaler.Mode == konsumeratorv1alpha1.AutoscalerTypePrometheus {
		lagProvider, err = providers.NewLagSourcePrometheus(consumer.Spec.Autoscaler.Prometheus)
		if err != nil {
			lagProvider = providers.NewLagSourceDummy(*consumer.Spec.NumPartitions)
		}
		if shouldUpdateLag(&consumer) {
			log.Info("going to upgrade lag")
			if err := lagProvider.EstimateLag(); err != nil {
				log.Error(err, "failed to query lag provider")
			}
			tm := metav1.Now()
			consumer.Status.LastSyncTime = &tm
		}
	}

	var missingPartitions []int32
	var runningInstances []*appsv1.Deployment
	var laggingInstances []*appsv1.Deployment
	var redundantInstances []*appsv1.Deployment
	var outdatedInstances []*appsv1.Deployment

	hash, err := hashstructure.Hash(consumer.Spec.DeploymentTemplate, nil)
	if err != nil {
		return ctrl.Result{}, err
	}
	consumer.Status.ObservedGeneration = helpers.Ptr2Int64(int64(hash))

	parts := make(map[int32]bool)
	for i := range managedDeploys.Items {
		deploy := managedDeploys.Items[i]
		var isRedundant bool
		partition := helpers.ParsePartitionAnnotation(deploy.Annotations[managedPartitionAnnotation])
		if partition == nil {
			log.Error(nil, "failed to parse annotation with partition number. Panic!!!")
			continue
		}
		parts[*partition] = true
		lag := lagProvider.GetLagByPartition(*partition)
		runningInstances = append(runningInstances, &deploy)
		if consumer.Spec.Autoscaler.Prometheus.TolerableLag != nil && lag >= consumer.Spec.Autoscaler.Prometheus.TolerableLag.Duration {
			laggingInstances = append(laggingInstances, &deploy)
		}
		if *partition > *consumer.Spec.NumPartitions {
			isRedundant = true
			redundantInstances = append(redundantInstances, &deploy)
		}
		if !isRedundant && deploy.Annotations[deploymentGeneration] != strconv.Itoa(int(*consumer.Status.ObservedGeneration)) {
			outdatedInstances = append(outdatedInstances, &deploy)
		}
	}
	for i := int32(0); i < *consumer.Spec.NumPartitions; i++ {
		if _, ok := parts[i]; !ok {
			missingPartitions = append(missingPartitions, i)
		}
	}
	consumer.Status.Running = helpers.Ptr2Int32(int32(len(runningInstances)))
	consumer.Status.Lagging = helpers.Ptr2Int32(int32(len(laggingInstances)))
	consumer.Status.Outdated = helpers.Ptr2Int32(int32(len(outdatedInstances)))
	consumer.Status.Expected = consumer.Spec.NumPartitions
	log.V(1).Info(
		"deployments count",
		"expected", consumer.Spec.NumPartitions,
		"running", len(runningInstances),
		"missing", len(missingPartitions),
		"lagging", len(laggingInstances),
		"outdated", len(outdatedInstances),
	)
	// log.V(2).Info(
	// 	"deployments",
	// 	"running", debugExtractNames(runningInstances),
	// 	"lagging", debugExtractNames(laggingInstances),
	// 	"outdated", debugExtractNames(outdatedInstances),
	// )

	if err := r.Status().Update(ctx, &consumer); errors.IgnoreConflict(err) != nil {
		log.Error(err, "unable to update Consumer status")
		return ctrl.Result{}, err
	}

	for _, mp := range missingPartitions {
		d, err := r.constructDeployment(consumer, mp)
		if err != nil {
			log.Error(err, "failed to construct deployment from template")
			continue
		}
		if err := r.Create(ctx, d); errors.IgnoreAlreadyExists(err) != nil {
			log.Error(err, "unable to create new Deployment", "deployment", d, "partition", mp)
			continue
		}
		log.V(1).Info("created new Deployment", "deployment", d, "partition", mp)
	}

	for _, deploy := range redundantInstances {
		if err := r.Delete(ctx, deploy); errors.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to delete deployment", "deployment", deploy)
		}
	}

	var partitionKey string
	if consumer.Spec.PartitionEnvKey != "" {
		partitionKey = consumer.Spec.PartitionEnvKey
	} else {
		partitionKey = defaultPartitionEnvKey
	}

	for _, deploy := range outdatedInstances {
		deploy.Annotations[deploymentGeneration] = strconv.Itoa(int(*consumer.Status.ObservedGeneration))
		deploy.Spec = consumer.Spec.DeploymentTemplate
		partition := helpers.ParsePartitionAnnotation(deploy.Annotations[managedPartitionAnnotation])
		if partition == nil {
			log.Error(nil, "failed to parse annotation with partition number.")
			continue
		}
		// update resources if needed
		// update GOMAXPROCs if needed
		for containerIndex := range deploy.Spec.Template.Spec.Containers {
			name := deploy.Spec.Template.Spec.Containers[containerIndex].Name
			resources := EstimateResources(name, &consumer.Spec, lagProvider, *partition)
			deploy.Spec.Template.Spec.Containers[containerIndex].Resources = *resources
			envs := setEnvVariable(deploy.Spec.Template.Spec.Containers[containerIndex].Env, partitionKey, strconv.Itoa(int(*partition)))
			envs = setEnvVariable(envs, gomaxprocsEnvKey, goMaxProcsFromRequests(resources.Limits.Cpu()))
			deploy.Spec.Template.Spec.Containers[containerIndex].Env = envs

		}
		if err := r.Update(ctx, deploy); errors.IgnoreConflict(err) != nil {
			log.Error(err, "unable to update deployment", "deployment", deploy)
			continue
		}
	}
	return ctrl.Result{}, nil
}

func GetResourcePolicy(name string, spec *konsumeratorv1alpha1.ConsumerSpec) *autoscalev1.ContainerResourcePolicy {
	for _, cp := range spec.ResourcePolicy.ContainerPolicies {
		if cp.ContainerName == name {
			return &cp
		}
	}
	return nil

}

func EstimateResources(containerName string, spec *konsumeratorv1alpha1.ConsumerSpec, store providers.LagSource, partition int32) *corev1.ResourceRequirements {
	promSpec := spec.Autoscaler.Prometheus
	resourceLimits := GetResourcePolicy(containerName, spec)
	production := store.GetProductionRate(partition)
	// consumption := store.GetConsumptionRate(partition)
	lagTime := store.GetLagByPartition(partition)
	work := int64(lagTime.Seconds())*production + production*int64(promSpec.PreferableCatchupPeriod.Seconds())
	expectedConsumption := work / int64(promSpec.PreferableCatchupPeriod.Seconds())
	cpuRequests := float64(expectedConsumption) / float64(*promSpec.RatePerCore)
	cpuReq := resource.NewMilliQuantity(int64(cpuRequests*1000), resource.DecimalSI)
	cpuLimit := resource.NewQuantity(int64(math.Ceil(cpuRequests)), resource.DecimalSI)
	if resourceLimits != nil {
		if cpuReq.MilliValue() < resourceLimits.MinAllowed.Cpu().MilliValue() {
			cpuReq = resourceLimits.MinAllowed.Cpu()
		}
		if cpuLimit.MilliValue() > resourceLimits.MaxAllowed.Cpu().MilliValue() {
			cpuLimit = resourceLimits.MaxAllowed.Cpu()
		}
	}
	// TODO: make proper memory estimation
	// memory is currently being set to the same value as CPU: 1 core = 1G of memory
	return &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    *cpuReq,
			corev1.ResourceMemory: *cpuReq,
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    *cpuLimit,
			corev1.ResourceMemory: *cpuLimit,
		},
	}
}

func goMaxProcsFromRequests(cpu *resource.Quantity) string {
	return strconv.Itoa(int(cpu.Value()))
}

func setEnvVariable(envVars []corev1.EnvVar, key string, value string) []corev1.EnvVar {
	for i, e := range envVars {
		if e.Name == key {
			envVars[i].Value = value
			return envVars
		}
	}
	envVars = append(envVars, corev1.EnvVar{
		Name:  key,
		Value: value,
	})
	return envVars
}

func (r *ConsumerReconciler) constructDeployment(consumer konsumeratorv1alpha1.Consumer, p int32) (*appsv1.Deployment, error) {
	deployLabels := make(map[string]string)
	deployAnnotations := make(map[string]string)
	deploy := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Labels:      deployLabels,
			Annotations: deployAnnotations,
			Name:        fmt.Sprintf("%s-%d", consumer.Spec.Name, p),
			Namespace:   consumer.Spec.Namespace,
		},
		Spec: consumer.Spec.DeploymentTemplate,
	}
	deploy.Annotations[managedPartitionAnnotation] = strconv.Itoa(int(p))
	deploy.Annotations[deploymentGeneration] = strconv.Itoa(int(*consumer.Status.ObservedGeneration))
	var partitionKey string
	if consumer.Spec.PartitionEnvKey != "" {
		partitionKey = consumer.Spec.PartitionEnvKey
	} else {
		partitionKey = defaultPartitionEnvKey
	}
	for i, _ := range deploy.Spec.Template.Spec.Containers {
		name := deploy.Spec.Template.Spec.Containers[i].Name
		envs := setEnvVariable(deploy.Spec.Template.Spec.Containers[i].Env, partitionKey, strconv.Itoa(int(p)))
		deploy.Spec.Template.Spec.Containers[i].Env = envs
		for _, cp := range consumer.Spec.ResourcePolicy.ContainerPolicies {
			if cp.ContainerName == name {
				deploy.Spec.Template.Spec.Containers[i].Resources.Requests = cp.MinAllowed
				deploy.Spec.Template.Spec.Containers[i].Resources.Limits = cp.MaxAllowed
			}
		}
	}
	if err := ctrl.SetControllerReference(&consumer, deploy, r.Scheme); err != nil {
		return nil, err
	}
	return deploy, nil
}

func (r *ConsumerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(&appsv1.Deployment{}, deployOwnerKey, func(rawObj runtime.Object) []string {
		// grab the object, extract the owner...
		d := rawObj.(*appsv1.Deployment)
		owner := metav1.GetControllerOf(d)
		if owner == nil {
			return nil
		}
		if owner.APIVersion != apiGVStr || owner.Kind != "Consumer" {
			return nil
		}
		return []string{owner.Name}
	}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&konsumeratorv1alpha1.Consumer{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
