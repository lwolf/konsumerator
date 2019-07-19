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
	"strconv"
	"time"

	"github.com/go-logr/logr"

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
	"github.com/lwolf/konsumerator/pkg/predictors"
	"github.com/lwolf/konsumerator/pkg/providers"
)

var (
	managedPartitionAnnotation  = "konsumerator.lwolf.org/partition"
	disableAutoscalerAnnotation = "konsumerator.lwolf.org/disable-autoscaler"
	deploymentGeneration        = "konsumerator.lwolf.org/deployment-generation"
	deployOwnerKey              = ".metadata.controller"
	defaultPartitionEnvKey      = "KONSUMERATOR_PARTITION"
	gomaxprocsEnvKey            = "GOMAXPROCS"
	defaultMinSyncPeriod        = time.Minute
	apiGVStr                    = konsumeratorv1alpha1.GroupVersion.String()
)

func debugExtractNames(deploys []*appsv1.Deployment) (names []string) {
	for _, d := range deploys {
		names = append(names, d.Name)
	}
	return
}
func debugPrettyResources(r *corev1.ResourceRequirements) string {
	if r == nil {
		return ""
	}
	return fmt.Sprintf(
		"Req: cpu:%s, ram:%s \t Limit: cpu:%s, ram:%s",
		r.Requests.Cpu().String(),
		r.Requests.Memory().String(),
		r.Limits.Cpu().String(),
		r.Limits.Memory().String(),
	)
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
	result := ctrl.Result{RequeueAfter: defaultMinSyncPeriod}

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
			if err := lagProvider.Update(); err != nil {
				log.Error(err, "failed to query lag provider")
			} else {
				tm := metav1.Now()
				consumer.Status.LastSyncTime = &tm
				consumer.Status.LastSyncState = providers.DumpSyncState(*consumer.Spec.NumPartitions, lagProvider)
			}
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
		return result, err
	}

	for _, mp := range missingPartitions {
		d, err := r.constructDeployment(consumer, mp, lagProvider)
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
			log.Info("calling estimate resources from main loop")
			predictor := predictors.NewStaticEstimator(lagProvider, consumer.Spec.Autoscaler.Prometheus)
			limits := predictors.GetResourcePolicy(name, &consumer.Spec)
			resources := predictor.Estimate(name, limits, *partition)
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
	for _, deploy := range laggingInstances {
		partition := helpers.ParsePartitionAnnotation(deploy.Annotations[managedPartitionAnnotation])
		if partition == nil {
			log.Error(nil, "failed to parse annotation with partition number.")
			continue
		}
		for containerIndex := range deploy.Spec.Template.Spec.Containers {
			name := deploy.Spec.Template.Spec.Containers[containerIndex].Name
			log.Info("calling estimate resources from main loop", "name", name)
			predictor := predictors.NewStaticEstimator(lagProvider, consumer.Spec.Autoscaler.Prometheus)
			limits := predictors.GetResourcePolicy(name, &consumer.Spec)
			resources := predictor.Estimate(name, limits, *partition)
			log.Info("resource estimation", "name", name, "min", limits, "resources", debugPrettyResources(resources))
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
	return result, nil
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

func (r *ConsumerReconciler) constructDeployment(consumer konsumeratorv1alpha1.Consumer, p int32, store providers.LagSource) (*appsv1.Deployment, error) {
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
	for containerIndex := range deploy.Spec.Template.Spec.Containers {
		name := deploy.Spec.Template.Spec.Containers[containerIndex].Name
		ctrl.Log.WithName("controllers").WithName("Consumer").Info("calling estimate resources from construct deploy")
		predictor := predictors.NewStaticEstimator(store, consumer.Spec.Autoscaler.Prometheus)
		limits := predictors.GetResourcePolicy(name, &consumer.Spec)
		resources := predictor.Estimate(name, limits, p)
		deploy.Spec.Template.Spec.Containers[containerIndex].Resources = *resources
		envs := setEnvVariable(deploy.Spec.Template.Spec.Containers[containerIndex].Env, partitionKey, strconv.Itoa(int(p)))
		envs = setEnvVariable(envs, gomaxprocsEnvKey, goMaxProcsFromRequests(resources.Limits.Cpu()))
		deploy.Spec.Template.Spec.Containers[containerIndex].Env = envs
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
