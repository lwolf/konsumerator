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
	var err error
	var mp providers.MetricsProvider

	_, autoscalerDisabled := consumer.Annotations[disableAutoscalerAnnotation]
	if !autoscalerDisabled && consumer.Spec.Autoscaler != nil {
		switch consumer.Spec.Autoscaler.Mode {
		case konsumeratorv1alpha1.AutoscalerTypePrometheus:
			// setup prometheus metrics provider
			mp, err = providers.NewPrometheusMP(log, consumer.Spec.Autoscaler.Prometheus)
			if err != nil {
				log.Error(err, "failed to initialize Prometheus Metrics Provider")
				break
			}
			providers.LoadSyncState(mp, consumer.Status)
			if shouldUpdateMetrics(&consumer) {
				log.Info("going to update metrics info")
				if err := mp.Update(); err != nil {
					log.Error(err, "failed to query lag provider")
				} else {
					tm := metav1.Now()
					consumer.Status.LastSyncTime = &tm
					consumer.Status.LastSyncState = providers.DumpSyncState(*consumer.Spec.NumPartitions, mp)
				}
			}
		default:
		}
	}
	if mp == nil {
		mp = providers.NewDummyMP(*consumer.Spec.NumPartitions)
	}

	var missingIds []int32
	var pausedIds []int32
	var runningIds []int32
	var laggingIds []int32
	var outdatedIds []int32
	var toRemoveInstances []*appsv1.Deployment
	var toUpdateInstances []*appsv1.Deployment
	var toEstimateInstances []*appsv1.Deployment

	hash, err := hashstructure.Hash(consumer.Spec.DeploymentTemplate, nil)
	if err != nil {
		return ctrl.Result{}, err
	}
	consumer.Status.ObservedGeneration = helpers.Ptr2Int64(int64(hash))

	trackedPartitions := make(map[int32]bool)
	for i := range managedDeploys.Items {
		deploy := managedDeploys.Items[i].DeepCopy()
		partition := helpers.ParsePartitionAnnotation(deploy.Annotations[partitionAnnotation])
		if partition == nil {
			log.Error(nil, "failed to parse annotation with partition number. Panic!!!")
			continue
		}
		trackedPartitions[*partition] = true
		lag := mp.GetLagByPartition(*partition)
		r.Log.Info("lag per partition", "partition", *partition, "lag", lag)
		if consumer.Spec.Autoscaler.Prometheus.TolerableLag != nil && lag >= consumer.Spec.Autoscaler.Prometheus.TolerableLag.Duration {
			laggingIds = append(laggingIds, *partition)
		}
		if deploy.Status.Replicas > 0 {
			runningIds = append(runningIds, *partition)
		} else {
			pausedIds = append(pausedIds, *partition)
		}
		if *partition >= *consumer.Spec.NumPartitions {
			toRemoveInstances = append(toRemoveInstances, deploy)
			continue
		}
		if deploy.Annotations[generationAnnotation] != strconv.Itoa(int(*consumer.Status.ObservedGeneration)) {
			outdatedIds = append(outdatedIds, *partition)
			toUpdateInstances = append(toUpdateInstances, deploy)
			continue
		}
		toEstimateInstances = append(toEstimateInstances, deploy)
	}
	for i := int32(0); i < *consumer.Spec.NumPartitions; i++ {
		if _, ok := trackedPartitions[i]; !ok {
			missingIds = append(missingIds, i)
		}
	}
	consumer.Status.Running = helpers.Ptr2Int32(int32(len(runningIds)))
	consumer.Status.Paused = helpers.Ptr2Int32(int32(len(pausedIds)))
	consumer.Status.Lagging = helpers.Ptr2Int32(int32(len(laggingIds)))
	consumer.Status.Outdated = helpers.Ptr2Int32(int32(len(toUpdateInstances)))
	consumer.Status.Expected = consumer.Spec.NumPartitions
	consumer.Status.Missing = helpers.Ptr2Int32(int32(len(missingIds)))
	log.V(1).Info(
		"deployments count",
		"expected", consumer.Spec.NumPartitions,
		"running", len(runningIds),
		"paused", len(pausedIds),
		"missing", len(missingIds),
		"lagging", len(laggingIds),
		"outdated", len(toUpdateInstances),
	)

	if err := r.Status().Update(ctx, &consumer); errors.IgnoreConflict(err) != nil {
		eMsg := "unable to update Consumer status"
		log.Error(err, eMsg)
		r.Recorder.Event(&consumer, corev1.EventTypeWarning, "UpdateConsumerStatus", eMsg)
		return result, err
	}

	predictor := predictors.NewNaivePredictor(log, mp, consumer.Spec.Autoscaler.Prometheus)
	for _, partition := range missingIds {
		newD := constructDeploy(consumer, partition)
		for i := range newD.Spec.Template.Spec.Containers {
			container := &newD.Spec.Template.Spec.Containers[i]
			container.Resources = estimateResources(predictor, container.Name, &consumer.Spec, partition)
			container.Env = helpers.PopulateEnv(container.Env, &container.Resources, consumer.Spec.PartitionEnvKey, int(partition))
		}
		if err := ctrl.SetControllerReference(&consumer, newD, r.Scheme); err != nil {
			log.Error(err, "unable to set ownder reference for the new Deployment", "deployment", newD, "partition", partition)
			continue
		}
		if err := r.Create(ctx, newD); errors.IgnoreAlreadyExists(err) != nil {
			log.Error(err, "unable to create new Deployment", "deployment", newD, "partition", partition)
			continue
		}
		log.V(1).Info("created new deployment", "deployment", newD, "partition", partition)
		r.Recorder.Eventf(
			&consumer,
			corev1.EventTypeNormal,
			"DeployCreate",
			"deployment for partition was created %d", partition,
		)
	}

	for _, deploy := range toRemoveInstances {
		if err := r.Delete(ctx, deploy); errors.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to delete deployment", "deployment", deploy)
		}
		r.Recorder.Eventf(
			&consumer,
			corev1.EventTypeNormal,
			"DeployDelete",
			"deployment %s was deleted", deploy.Name,
		)
	}

	for _, origDeploy := range toUpdateInstances {
		deploy := origDeploy.DeepCopy()
		deploy.Annotations[generationAnnotation] = strconv.Itoa(int(*consumer.Status.ObservedGeneration))
		deploy.Spec = consumer.Spec.DeploymentTemplate
		partition := helpers.ParsePartitionAnnotation(deploy.Annotations[partitionAnnotation])
		if partition == nil {
			log.Error(nil, "failed to parse annotation with partition number.")
			continue
		}
		for i := range deploy.Spec.Template.Spec.Containers {
			container := &deploy.Spec.Template.Spec.Containers[i]
			container.Resources = estimateResources(predictor, container.Name, &consumer.Spec, *partition)
			container.Env = helpers.PopulateEnv(container.Env, &container.Resources, consumer.Spec.PartitionEnvKey, int(*partition))
		}
		if err := r.Update(ctx, deploy); errors.IgnoreConflict(err) != nil {
			log.Error(err, "unable to update deployment", "deployment", deploy)
			r.Recorder.Eventf(
				&consumer,
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
	for _, origDeploy := range toEstimateInstances {
		deploy := origDeploy.DeepCopy()
		partition := helpers.ParsePartitionAnnotation(deploy.Annotations[partitionAnnotation])
		if partition == nil {
			log.Error(nil, "failed to parse annotation with partition number.")
			continue
		}
		for i := range deploy.Spec.Template.Spec.Containers {
			container := &deploy.Spec.Template.Spec.Containers[i]
			container.Resources = estimateResources(predictor, container.Name, &consumer.Spec, *partition)
			container.Env = helpers.PopulateEnv(container.Env, &container.Resources, consumer.Spec.PartitionEnvKey, int(*partition))
		}
		if err := r.Update(ctx, deploy); errors.IgnoreConflict(err) != nil {
			log.Error(err, "unable to update deployment", "deployment", deploy)
			r.Recorder.Eventf(
				&consumer,
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

func constructDeploy(consumer konsumeratorv1alpha1.Consumer, partition int32) *appsv1.Deployment {
	deployLabels := make(map[string]string)
	deployAnnotations := make(map[string]string)
	deploy := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Labels:      deployLabels,
			Annotations: deployAnnotations,
			Name:        fmt.Sprintf("%s-%d", consumer.Spec.Name, partition),
			Namespace:   consumer.Spec.Namespace,
		},
		Spec: consumer.Spec.DeploymentTemplate,
	}
	deploy.Annotations[partitionAnnotation] = strconv.Itoa(int(partition))
	deploy.Annotations[generationAnnotation] = strconv.Itoa(int(*consumer.Status.ObservedGeneration))
	return deploy
}

func estimateResources(predictor predictors.Predictor, containerName string, consumerSpec *konsumeratorv1alpha1.ConsumerSpec, partition int32) corev1.ResourceRequirements {
	limits := predictors.GetResourcePolicy(containerName, consumerSpec)
	resources := predictor.Estimate(containerName, limits, partition)
	return *resources
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
