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
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ref "k8s.io/client-go/tools/reference"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"

	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
)

var (
	managedPartitionAnnotation = "konsumerator.lwolf.org/partition"
	deployOwnerKey             = ".metadata.controller"
	apiGVStr                   = konsumeratorv1alpha1.GroupVersion.String()
)

func ignoreNotFound(err error) error {
	if apierrs.IsNotFound(err) {
		return nil
	}
	return err
}

func ignoreAlreadyExists(err error) error {
	if apierrs.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func ignoreConflict(err error) error {
	if apierrs.IsConflict(err) {
		return nil
	}
	return err
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
		return ctrl.Result{}, ignoreNotFound(err)
	}
	var managedDeploys v1.DeploymentList
	if err := r.List(ctx, &managedDeploys, client.InNamespace(req.Namespace), client.MatchingField(deployOwnerKey, req.Name)); err != nil {
		log.Error(err, "unable to list managed deployments")
		return ctrl.Result{}, err
	}
	rawRsp, err := getPrometheusMetrics(consumer.Spec.PrometheusAddress, consumer.Spec.PrometheusLagMetricName)
	if err != nil {
		log.Error(err, "unable to get metrics from prometheus, skipping adjustments to the lag")
	}
	metrics, err := parsePrometheus(rawRsp)
	if err != nil {
		log.Error(err, "unable to parse prometheus metrics, skipping adjustments to the lag")
	}

	var runningStatuses []konsumeratorv1alpha1.ObjectStatus
	var missingStatuses []konsumeratorv1alpha1.ObjectStatus
	var runningInstances []*appsv1.Deployment
	var laggingInstances []*appsv1.Deployment
	var redundantInstances []*appsv1.Deployment

	parts := make(map[int32]bool)
	for _, deploy := range managedDeploys.Items {
		partition := parsePartitionAnnotation(deploy.Annotations[managedPartitionAnnotation])
		if partition == nil {
			log.Error(nil, "failed to parse annotation with partition number. Panic!!!")
			continue
		}
		reference, err := ref.GetReference(r.Scheme, &deploy)
		if err != nil {
			log.Error(err, "failed to reference to the deployment")
			continue
		}
		parts[*partition] = true
		lag := metrics[*partition]
		status := konsumeratorv1alpha1.ObjectStatus{
			Partition: partition,
			Lag:       &lag,
			Ref:       *reference,
		}
		runningStatuses = append(runningStatuses, status)
		runningInstances = append(runningInstances, &deploy)
		if lag >= *consumer.Spec.AllowedLagSeconds {
			laggingInstances = append(laggingInstances, &deploy)
		}
		if *partition > *consumer.Spec.NumPartitions {
			redundantInstances = append(redundantInstances, &deploy)
		}
	}
	var i int32
	for i = 0; i < *consumer.Spec.NumPartitions; i++ {
		_, ok := parts[i]
		if ok {
			continue
		}
		lag := metrics[i]
		instance := konsumeratorv1alpha1.ObjectStatus{
			Partition: Ptr2Int32(i),
			Lag:       &lag,
			Ref:       corev1.ObjectReference{},
		}
		missingStatuses = append(missingStatuses, instance)
	}
	tm := metav1.Now()
	consumer.Status.Active = runningStatuses
	consumer.Status.LastUpdateTime = &tm
	consumer.Status.Running = Ptr2Int32(int32(len(runningInstances)))
	consumer.Status.Lagging = Ptr2Int32(int32(len(laggingInstances)))
	consumer.Status.Missing = Ptr2Int32(int32(len(missingStatuses)))
	consumer.Status.Expected = consumer.Spec.NumPartitions
	log.V(1).Info("deployments count", "expected", consumer.Spec.NumPartitions, "running", len(runningInstances), "missing", len(missingStatuses), "lagging", len(laggingInstances))

	if err := r.Status().Update(ctx, &consumer); ignoreConflict(err) != nil {
		log.Error(err, "unable to update Consumer status")
		return ctrl.Result{}, err
	}

	if len(missingStatuses) > 0 {
		for _, mi := range missingStatuses {
			d, err := r.constructDeployment(consumer, *mi.Partition)
			if err != nil {
				log.Error(err, "failed to construct deployment from template")
				continue
			}
			if err := r.Create(ctx, d); ignoreAlreadyExists(err) != nil {
				log.Error(err, "unable to create new Deployment", "deployment", d, "partition", *mi.Partition)
				continue
			}
			log.V(1).Info("created new Deployment", "deployment", d, "partition", *mi.Partition)
		}
	}

	if len(redundantInstances) > 0 {
		for _, deploy := range redundantInstances {
			if err := r.Delete(ctx, deploy); ignoreNotFound(err) != nil {
				log.Error(err, "unable to delete deployment", "deployment", deploy)
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *ConsumerReconciler) constructDeployment(consumer konsumeratorv1alpha1.Consumer, p int32) (*appsv1.Deployment, error) {
	name := fmt.Sprintf("%s-%d", consumer.Spec.Name, p)
	deploy := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Labels:      make(map[string]string),
			Annotations: make(map[string]string),
			Name:        name,
			Namespace:   consumer.Spec.Namespace,
		},
		Spec: consumer.Spec.DeploymentTemplate,
	}
	deploy.Annotations[managedPartitionAnnotation] = strconv.Itoa(int(p))
	if err := ctrl.SetControllerReference(&consumer, deploy, r.Scheme); err != nil {
		return nil, err
	}
	return deploy, nil
}

func Ptr2Int32(i int32) *int32 {
	return &i
}

func Ptr2Int64(i int64) *int64 {
	return &i
}

func parsePartitionAnnotation(partition string) *int32 {
	if len(partition) == 0 {
		return nil
	}
	p, err := strconv.ParseInt(partition, 10, 32)
	if err != nil {
		return nil
	}
	p32 := int32(p)
	return &p32
}

func parsePrometheus(s string) (map[int32]int64, error) {
	// needs to be processed differently depending on resultType (vector, matrix,scalar)
	return nil, nil
}

// getPrometheusMetrics doing actual query to Prometheus and returns it response
func getPrometheusMetrics(prom string, metricName string) (string, error) {
	return "", nil
}

func (r *ConsumerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(&appsv1.Deployment{}, deployOwnerKey, func(rawObj runtime.Object) []string {
		// grab the job object, extract the owner...
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
