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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
	"github.com/lwolf/konsumerator/pkg/errors"
	"github.com/lwolf/konsumerator/pkg/helpers"
	"github.com/lwolf/konsumerator/pkg/predictors"
	"github.com/lwolf/konsumerator/pkg/providers"
)

var (
	partitionAnnotation         = "konsumerator.lwolf.org/partition"
	disableAutoscalerAnnotation = "konsumerator.lwolf.org/disable-autoscaler"
	generationAnnotation        = "konsumerator.lwolf.org/generation"
	ownerKey                    = ".metadata.controller"
	defaultMinSyncPeriod        = time.Minute
	apiGVStr                    = konsumeratorv1alpha1.GroupVersion.String()
)

func shouldUpdateMetrics(consumer *konsumeratorv1alpha1.Consumer) bool {
	status := consumer.Status
	if status.LastSyncTime == nil || status.LastSyncState == nil {
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
	Log      logr.Logger
	Recorder record.EventRecorder
	Scheme   *runtime.Scheme
}

// +kubebuilder:rbac:groups=konsumerator.lwolf.org,resources=consumers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konsumerator.lwolf.org,resources=consumers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=watch;create;get;update;patch;delete;list
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
	if err := r.List(ctx, &managedDeploys, client.InNamespace(req.Namespace), client.MatchingField(ownerKey, req.Name)); err != nil {
		eMsg := "unable to list managed deployments"
		log.Error(err, eMsg)
		r.Recorder.Event(&consumer, corev1.EventTypeWarning, "ListDeployFailure", eMsg)
		return ctrl.Result{}, err
	}

	co, err := newConsumerOperator(log, &consumer, managedDeploys)
	if err != nil {
		return ctrl.Result{}, err
	}
	log.V(1).Info(
		"deployments count",
		"expected", consumer.Spec.NumPartitions,
		"running", len(co.runningIds),
		"paused", len(co.pausedIds),
		"missing", len(co.missingIds),
		"lagging", len(co.laggingIds),
		"outdated", len(co.toUpdateInstances),
	)

	if err := r.Status().Update(ctx, co.consumer); errors.IgnoreConflict(err) != nil {
		eMsg := "unable to update Consumer status"
		log.Error(err, eMsg)
		r.Recorder.Event(&consumer, corev1.EventTypeWarning, "UpdateConsumerStatus", eMsg)
		return result, err
	}

	predictor := predictors.NewNaivePredictor(log, co.mp, co.consumer.Spec.Autoscaler.Prometheus)
	for _, partition := range co.missingIds {
		newD, err := co.newDeploy(predictor, partition)
		if err != nil {
			log.Error(err, "failed to create new deploy")
			continue
		}
		if err := ctrl.SetControllerReference(co.consumer, newD, r.Scheme); err != nil {
			log.Error(err, "unable to set owner reference for the new Deployment", "deployment", newD, "partition", partition)
			continue
		}
		if err := r.Create(ctx, newD); errors.IgnoreAlreadyExists(err) != nil {
			log.Error(err, "unable to create new Deployment", "deployment", newD, "partition", partition)
			continue
		}
		log.V(1).Info("created new deployment", "deployment", newD, "partition", partition)
		r.Recorder.Eventf(
			co.consumer,
			corev1.EventTypeNormal,
			"DeployCreate",
			"deployment for partition was created %d", partition,
		)
	}

	for _, deploy := range co.toRemoveInstances {
		if err := r.Delete(ctx, deploy); errors.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to delete deployment", "deployment", deploy)
		}
		r.Recorder.Eventf(
			co.consumer,
			corev1.EventTypeNormal,
			"DeployDelete",
			"deployment %s was deleted", deploy.Name,
		)
	}

	for _, origDeploy := range co.toUpdateInstances {
		deploy := origDeploy.DeepCopy()
		deploy, err := co.updateDeployWithPredictor(deploy, predictor)
		if err != nil {
			log.Error(err, "failed to update deploy")
			continue
		}
		if err := r.Update(ctx, deploy); errors.IgnoreConflict(err) != nil {
			log.Error(err, "unable to update deployment", "deployment", deploy)
			r.Recorder.Eventf(
				co.consumer,
				corev1.EventTypeWarning,
				"DeployUpdate",
				"deployment %s: update failed", deploy.Name,
			)
			continue
		}
		r.Recorder.Eventf(
			&consumer,
			corev1.EventTypeNormal,
			"DeployUpdate",
			"deployment %s: updated", deploy.Name,
		)
	}
	for _, origDeploy := range co.toEstimateInstances {
		deploy := origDeploy.DeepCopy()
		deploy, err := co.updateDeployWithPredictor(deploy, predictor)
		if err != nil {
			log.Error(err, "failed to update deploy")
			continue
		}
		if err := r.Update(ctx, deploy); errors.IgnoreConflict(err) != nil {
			log.Error(err, "unable to update deployment", "deployment", deploy)
			r.Recorder.Eventf(
				co.consumer,
				corev1.EventTypeWarning,
				"DeployUpdate",
				"deployment %s: update failed", deploy.Name,
			)
			continue
		}
		r.Recorder.Eventf(
			&consumer,
			corev1.EventTypeNormal,
			"DeployUpdate",
			"deployment %s: updated", deploy.Name,
		)
	}
	return result, nil
}

func (r *ConsumerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(&appsv1.Deployment{}, ownerKey, func(rawObj runtime.Object) []string {
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

type consumerOperator struct {
	consumer *konsumeratorv1alpha1.Consumer
	mp       providers.MetricsProvider
	log      logr.Logger

	missingIds          []int32
	pausedIds           []int32
	runningIds          []int32
	laggingIds          []int32
	outdatedIds         []int32
	toRemoveInstances   []*appsv1.Deployment
	toUpdateInstances   []*appsv1.Deployment
	toEstimateInstances []*appsv1.Deployment
}

func newConsumerOperator(log logr.Logger, consumer *konsumeratorv1alpha1.Consumer, managedDeploys v1.DeploymentList) (*consumerOperator, error) {
	hash, err := hashstructure.Hash(consumer.Spec.DeploymentTemplate, nil)
	if err != nil {
		return nil, err
	}
	consumer.Status.ObservedGeneration = helpers.Ptr2Int64(int64(hash))
	co := &consumerOperator{
		consumer: consumer,
		log:      log,
	}
	co.mp = co.newMetricsProvider()
	co.syncDeploys(managedDeploys)
	return co, err
}

func (co consumerOperator) observedGeneration() string {
	return strconv.Itoa(int(*co.consumer.Status.ObservedGeneration))
}

func (co consumerOperator) isLagging(lag time.Duration) bool {
	tolerableLag := co.consumer.Spec.Autoscaler.Prometheus.TolerableLag
	if tolerableLag == nil {
		return false
	}
	return lag >= tolerableLag.Duration
}

func (co consumerOperator) isAutoScaleEnabled() bool {
	_, autoscalerDisabled := co.consumer.Annotations[disableAutoscalerAnnotation]
	return !autoscalerDisabled && co.consumer.Spec.Autoscaler != nil
}

func (co consumerOperator) newMetricsProvider() providers.MetricsProvider {
	defaultProvider := providers.NewDummyMP(*co.consumer.Spec.NumPartitions)
	if !co.isAutoScaleEnabled() {
		return defaultProvider
	}
	switch co.consumer.Spec.Autoscaler.Mode {
	case konsumeratorv1alpha1.AutoscalerTypePrometheus:
		// setup prometheus metrics provider
		mp, err := providers.NewPrometheusMP(co.log, co.consumer.Spec.Autoscaler.Prometheus)
		if err != nil {
			co.log.Error(err, "failed to initialize Prometheus Metrics Provider")
			return defaultProvider
		}
		providers.LoadSyncState(mp, co.consumer.Status)
		if shouldUpdateMetrics(co.consumer) {
			co.log.Info("going to update metrics info")
			if err := mp.Update(); err != nil {
				co.log.Error(err, "failed to query lag provider")
			} else {
				tm := metav1.Now()
				co.consumer.Status.LastSyncTime = &tm
				co.consumer.Status.LastSyncState = providers.DumpSyncState(*co.consumer.Spec.NumPartitions, mp)
			}
		}
		return mp
	default:
		return defaultProvider
	}
}

func (co *consumerOperator) syncDeploys(managedDeploys v1.DeploymentList) {
	trackedPartitions := make(map[int32]bool)
	for i := range managedDeploys.Items {
		deploy := &managedDeploys.Items[i]
		partition, err := helpers.ParsePartitionAnnotation(deploy.Annotations[partitionAnnotation])
		if err != nil {
			co.log.Error(err, "failed to parse annotation with partition number. Panic!!!")
			continue
		}
		trackedPartitions[partition] = true
		lag := co.mp.GetLagByPartition(partition)
		co.log.Info("lag per partition", "partition", partition, "lag", lag)
		if co.isLagging(lag) {
			co.laggingIds = append(co.laggingIds, partition)
		}
		if deploy.Status.Replicas > 0 {
			co.runningIds = append(co.runningIds, partition)
		} else {
			co.pausedIds = append(co.pausedIds, partition)
		}
		if partition >= *co.consumer.Spec.NumPartitions {
			co.toRemoveInstances = append(co.toRemoveInstances, deploy)
			continue
		}
		if deploy.Annotations[generationAnnotation] != co.observedGeneration() {
			co.outdatedIds = append(co.outdatedIds, partition)
			co.toUpdateInstances = append(co.toUpdateInstances, deploy)
			continue
		}
		co.toEstimateInstances = append(co.toEstimateInstances, deploy)
	}
	for i := int32(0); i < *co.consumer.Spec.NumPartitions; i++ {
		if _, ok := trackedPartitions[i]; !ok {
			co.missingIds = append(co.missingIds, i)
		}
	}

	status := &co.consumer.Status
	status.Running = helpers.Ptr2Int32(int32(len(co.runningIds)))
	status.Paused = helpers.Ptr2Int32(int32(len(co.pausedIds)))
	status.Lagging = helpers.Ptr2Int32(int32(len(co.laggingIds)))
	status.Outdated = helpers.Ptr2Int32(int32(len(co.toUpdateInstances)))
	status.Expected = co.consumer.Spec.NumPartitions
	status.Missing = helpers.Ptr2Int32(int32(len(co.missingIds)))
}

func (co consumerOperator) newDeploy(predictor predictors.Predictor, partition int32) (*appsv1.Deployment, error) {
	deploy := co.constructDeploy(partition)
	return co.updateDeployWithPredictor(deploy, predictor)
}

func (co consumerOperator) updateDeployWithPredictor(deploy *appsv1.Deployment, predictor predictors.Predictor) (*appsv1.Deployment, error) {
	partition, err := helpers.ParsePartitionAnnotation(deploy.Annotations[partitionAnnotation])
	if err != nil {
		return nil, err
	}
	deploy.Annotations[generationAnnotation] = co.observedGeneration()
	deploy.Spec = co.consumer.Spec.DeploymentTemplate
	for i := range deploy.Spec.Template.Spec.Containers {
		container := &deploy.Spec.Template.Spec.Containers[i]
		container.Resources = estimateResources(predictor, container.Name, &co.consumer.Spec, partition)
		container.Env = helpers.PopulateEnv(container.Env, &container.Resources, co.consumer.Spec.PartitionEnvKey, int(partition))
	}
	return deploy, nil
}

func (co consumerOperator) constructDeploy(partition int32) *appsv1.Deployment {
	deployLabels := make(map[string]string)
	deployAnnotations := make(map[string]string)
	deploy := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Labels:      deployLabels,
			Annotations: deployAnnotations,
			Name:        fmt.Sprintf("%s-%d", co.consumer.Spec.Name, partition),
			Namespace:   co.consumer.Spec.Namespace,
		},
		Spec: co.consumer.Spec.DeploymentTemplate,
	}
	deploy.Annotations[partitionAnnotation] = strconv.Itoa(int(partition))
	deploy.Annotations[generationAnnotation] = co.observedGeneration()
	return deploy
}

func estimateResources(predictor predictors.Predictor, containerName string, consumerSpec *konsumeratorv1alpha1.ConsumerSpec, partition int32) corev1.ResourceRequirements {
	limits := predictors.GetResourcePolicy(containerName, consumerSpec)
	resources := predictor.Estimate(containerName, limits, partition)
	return *resources
}
