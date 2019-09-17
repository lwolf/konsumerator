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
	"github.com/lwolf/konsumerator/pkg/limiters"
	"github.com/lwolf/konsumerator/pkg/predictors"
	"github.com/lwolf/konsumerator/pkg/providers"
)

const (
	InstanceStatusRunning          string = "RUNNING"
	InstanceStatusSaturated        string = "SATURATED"
	InstanceStatusPendingScaleUp   string = "PENDING_SCALE_UP"
	InstanceStatusPendingScaleDown string = "PENDING_SCALE_DOWN"

	partitionAnnotation           = "konsumerator.lwolf.org/partition"
	disableAutoscalerAnnotation   = "konsumerator.lwolf.org/disable-autoscaler"
	generationAnnotation          = "konsumerator.lwolf.org/generation"
	cpuSaturationLevel            = "konsumerator.lwolf.org/cpu-saturation-level"
	scalingStatusAnnotation       = "konsumerator.lwolf.org/scaling-status"
	scalingStatusChangeAnnotation = "konsumerator.lwolf.org/scaling-status-change"
	ownerKey                      = ".metadata.controller"

	cmpResourcesLt int = -1
	cmpResourcesEq int = 0
	cmpResourcesGt int = 1

	defaultMinSyncPeriod    = time.Minute
	scaleStatePendingPeriod = time.Minute * 5
)

var apiGVStr = konsumeratorv1alpha1.GroupVersion.String()

func scalingAllowed(lastChange time.Time) bool {
	return time.Since(lastChange) >= scaleStatePendingPeriod
}

func instanceStatusToInt(status string) int {
	switch status {
	case InstanceStatusRunning:
		return 0
	case InstanceStatusSaturated:
		return 1
	case InstanceStatusPendingScaleUp:
		return 2
	case InstanceStatusPendingScaleDown:
		return 3
	default:
		return -1
	}
}

func shouldUpdateMetrics(consumer *konsumeratorv1alpha1.Consumer) (bool, error) {
	status := consumer.Status
	if status.LastSyncTime == nil || status.LastSyncState == nil {
		return true, nil
	}
	if consumer.Spec.Autoscaler == nil {
		return false, fmt.Errorf("autoscaler is not present in consumer spec")
	}
	if consumer.Spec.Autoscaler.Mode == konsumeratorv1alpha1.AutoscalerTypePrometheus &&
		consumer.Spec.Autoscaler.Prometheus == nil {
		return false, fmt.Errorf("autoscaler misconfiguration: prometheus setup is missing")
	}
	timeToSync := metav1.Now().Sub(status.LastSyncTime.Time) > consumer.Spec.Autoscaler.Prometheus.MinSyncPeriod.Duration
	if timeToSync {
		return true, nil
	}
	return false, nil
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
	start := time.Now()
	defer func() {
		reconcileDuration.WithLabelValues(req.Name).Observe(time.Since(start).Seconds())
	}()

	ctx := context.Background()
	log := r.Log.WithValues("consumer", req.NamespacedName)
	result := ctrl.Result{RequeueAfter: defaultMinSyncPeriod}
	reconcileTotal.WithLabelValues(req.Name).Inc()

	var consumer konsumeratorv1alpha1.Consumer
	if err := r.Get(ctx, req.NamespacedName, &consumer); err != nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		reconcileErrors.WithLabelValues(req.Name).Inc()
		return ctrl.Result{}, errors.IgnoreNotFound(err)
	}
	var managedDeploys v1.DeploymentList
	if err := r.List(ctx, &managedDeploys, client.InNamespace(req.Namespace), client.MatchingField(ownerKey, req.Name)); err != nil {
		eMsg := "unable to list managed deployments"
		log.Error(err, eMsg)
		r.Recorder.Event(&consumer, corev1.EventTypeWarning, "ListDeployFailure", eMsg)
		reconcileErrors.WithLabelValues(req.Name).Inc()
		return ctrl.Result{}, err
	}

	co, err := newConsumerOperator(log, &consumer, managedDeploys)
	if err != nil {
		reconcileErrors.WithLabelValues(req.Name).Inc()
		return ctrl.Result{}, err
	}
	log.V(1).Info(
		"deployments count",
		"metricsUpdated", co.metricsUpdated,
		"expected", consumer.Spec.NumPartitions,
		"running", *co.consumer.Status.Running,
		"paused", *co.consumer.Status.Paused,
		"missing", *co.consumer.Status.Missing,
		"lagging", *co.consumer.Status.Lagging,
		"toUpdate", *co.consumer.Status.Outdated,
		"toEstimate", len(co.toEstimateInstances),
	)

	start = time.Now()
	if err := r.Status().Update(ctx, co.consumer); errors.IgnoreConflict(err) != nil {
		eMsg := "unable to update Consumer status"
		log.Error(err, eMsg)
		r.Recorder.Event(&consumer, corev1.EventTypeWarning, "UpdateConsumerStatus", eMsg)
		reconcileErrors.WithLabelValues(req.Name).Inc()
		return result, err
	}
	statusUpdateDuration.WithLabelValues(req.Name).Observe(time.Since(start).Seconds())

	// TODO: if consumer.spec.autoscaler is absent from the spec, we get panic
	// we need to either enforce autoscaler in the manifest of check for it here
	predictor := predictors.NewNaivePredictor(log, co.mp, co.consumer.Spec.Autoscaler.Prometheus)
	for _, partition := range co.missingIds {
		newD, err := co.newDeploy(predictor, partition)
		if err != nil {
			deploymentsCreateErrors.WithLabelValues(req.Name).Inc()
			log.Error(err, "failed to create new deploy")
			continue
		}
		if err := ctrl.SetControllerReference(co.consumer, newD, r.Scheme); err != nil {
			deploymentsCreateErrors.WithLabelValues(req.Name).Inc()
			log.Error(err, "unable to set owner reference for the new Deployment", "deployment", newD, "partition", partition)
			continue
		}
		if err := r.Create(ctx, newD); errors.IgnoreAlreadyExists(err) != nil {
			deploymentsCreateErrors.WithLabelValues(req.Name).Inc()
			log.Error(err, "unable to create new Deployment", "deployment", newD, "partition", partition)
			continue
		}
		log.V(1).Info("created new deployment", "deployment", newD, "partition", partition)
		deploymentsCreateTotal.WithLabelValues(req.Name).Inc()
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
			deploymentsDeleteErrors.WithLabelValues(req.Name).Inc()
			continue
		}
		deploymentsDeleteTotal.WithLabelValues(req.Name).Inc()
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
			deploymentsUpdateErrors.WithLabelValues(req.Name).Inc()
			log.Error(err, "failed to update deploy")
			continue
		}

		if err := r.Update(ctx, deploy); errors.IgnoreConflict(err) != nil {
			deploymentsUpdateErrors.WithLabelValues(req.Name).Inc()
			log.Error(err, "unable to update deployment", "deployment", deploy)
			continue
		}
		deploymentsUpdateTotal.WithLabelValues(req.Name).Inc()
	}
	for _, origDeploy := range co.toEstimateInstances {
		deploy := origDeploy.DeepCopy()
		deploy, needsUpdate, err := co.updateEstimatedDeployWithPredictor(deploy, predictor)
		if err != nil {
			deploymentsUpdateErrors.WithLabelValues(req.Name).Inc()
			log.Error(err, "failed to update deploy")
			continue
		}

		if needsUpdate {
			if err := r.Update(ctx, deploy); errors.IgnoreConflict(err) != nil {
				deploymentsUpdateErrors.WithLabelValues(req.Name).Inc()
				log.Error(err, "unable to update deployment", "deployment", deploy)
				continue
			}
			deploymentsUpdateTotal.WithLabelValues(req.Name).Inc()
		}
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
	limiter  limiters.ResourceLimiter
	log      logr.Logger
	mp       providers.MetricsProvider
	// XXX: should it be a part of mp?
	metricsUpdated bool

	missingIds          []int32
	pausedIds           []int32
	runningIds          []int32
	laggingIds          []int32
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
		limiter:  limiters.NewInstanceLimiter(consumer.Spec.ResourcePolicy, log),
		log:      log,
	}
	co.mp = co.newMetricsProvider()
	co.syncDeploys(managedDeploys)

	name := co.consumer.Name
	status := co.consumer.Status
	consumerStatus.WithLabelValues(name, "running").Set(float64(*status.Running))
	consumerStatus.WithLabelValues(name, "paused").Set(float64(*status.Paused))
	consumerStatus.WithLabelValues(name, "lagging").Set(float64(*status.Lagging))
	consumerStatus.WithLabelValues(name, "outdated").Set(float64(*status.Outdated))
	consumerStatus.WithLabelValues(name, "expected").Set(float64(*status.Expected))
	consumerStatus.WithLabelValues(name, "missing").Set(float64(*status.Missing))

	return co, err
}

func (co *consumerOperator) observedGeneration() string {
	return strconv.Itoa(int(*co.consumer.Status.ObservedGeneration))
}

func (co *consumerOperator) isLagging(lag time.Duration) bool {
	tolerableLag := co.consumer.Spec.Autoscaler.Prometheus.TolerableLag
	if tolerableLag == nil {
		return false
	}
	return lag >= tolerableLag.Duration
}

func (co *consumerOperator) isAutoScaleEnabled() bool {
	_, autoscalerDisabled := co.consumer.Annotations[disableAutoscalerAnnotation]
	return !autoscalerDisabled && co.consumer.Spec.Autoscaler != nil
}

func (co *consumerOperator) newMetricsProvider() providers.MetricsProvider {
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
		shouldUpdate, err := shouldUpdateMetrics(co.consumer)
		if err != nil {
			co.log.Error(err, "failed to initialize Prometheus Metrics Provider")
			return defaultProvider
		}
		if shouldUpdate {
			if err := mp.Update(); err != nil {
				co.log.Error(err, "failed to query metrics from the lag provider")
			} else {
				tm := metav1.Now()
				co.metricsUpdated = true
				co.consumer.Status.LastSyncTime = &tm
				co.consumer.Status.LastSyncState = providers.DumpSyncState(*co.consumer.Spec.NumPartitions, mp)
				co.log.Info("metrics data were updated successfully")
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
		co.log.V(1).Info("lag per partition", "partition", partition, "lag", lag)
		if deployIsPaused(deploy) {
			co.pausedIds = append(co.pausedIds, partition)
			continue
		} else {
			co.runningIds = append(co.runningIds, partition)
		}
		if co.isLagging(lag) {
			co.laggingIds = append(co.laggingIds, partition)
		}
		if partition >= *co.consumer.Spec.NumPartitions {
			co.toRemoveInstances = append(co.toRemoveInstances, deploy)
			continue
		}
		if deploy.Annotations[generationAnnotation] != co.observedGeneration() {
			co.toUpdateInstances = append(co.toUpdateInstances, deploy)
			continue
		}
		if co.metricsUpdated {
			co.toEstimateInstances = append(co.toEstimateInstances, deploy)
		}
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

func (co *consumerOperator) newDeploy(predictor predictors.Predictor, partition int32) (*appsv1.Deployment, error) {
	deploy := co.constructDeploy(partition)
	return co.updateDeployWithPredictor(deploy, predictor)
}

func (co *consumerOperator) updateEstimatedDeployWithPredictor(deploy *appsv1.Deployment, predictor predictors.Predictor) (*appsv1.Deployment, bool, error) {
	if time.Since(co.consumer.Status.LastSyncTime.Time) >= scaleStatePendingPeriod {
		return deploy, false, nil
	}
	partition, err := helpers.ParsePartitionAnnotation(deploy.Annotations[partitionAnnotation])
	if err != nil {
		return nil, false, err
	}
	currentState := deploy.Annotations[scalingStatusAnnotation]
	lastStateChange, err := helpers.ParseTimeAnnotation(deploy.Annotations[scalingStatusChangeAnnotation])
	if err != nil {
		return nil, false, err
	}

	lag := co.mp.GetLagByPartition(partition)
	isLagging := co.isLagging(lag)
	needsUpdate := false
	for i := range deploy.Spec.Template.Spec.Containers {
		isChangedAnnotations := false
		container := &deploy.Spec.Template.Spec.Containers[i]
		resources, underProvision := estimateResources(partition, container.Name, predictor, co.limiter)
		cmpRes := helpers.CmpResourceRequirements(deploy.Spec.Template.Spec.Containers[i].Resources, resources)
		switch cmpRes {
		case cmpResourcesEq:
			isChangedAnnotations = updateScaleAnnotations(deploy, underProvision)
		case cmpResourcesGt:
			if currentState == InstanceStatusPendingScaleUp && scalingAllowed(lastStateChange) {
				updateScaleAnnotations(deploy, underProvision)
				container.Resources = resources
				container.Env = helpers.PopulateEnv(container.Env, &container.Resources, co.consumer.Spec.PartitionEnvKey, int(partition))
				needsUpdate = true
			} else {
				isChangedAnnotations = updateScalingStatus(deploy, InstanceStatusPendingScaleUp)
			}
		case cmpResourcesLt:
			if currentState == InstanceStatusPendingScaleDown && scalingAllowed(lastStateChange) {
				updateScaleAnnotations(deploy, underProvision)
				container.Resources = resources
				container.Env = helpers.PopulateEnv(container.Env, &container.Resources, co.consumer.Spec.PartitionEnvKey, int(partition))
				needsUpdate = true
			} else if !isLagging {
				isChangedAnnotations = updateScalingStatus(deploy, InstanceStatusPendingScaleDown)
			}
		}
		co.log.Info(
			"cmp resource",
			"partition", partition,
			"container", container.Name,
			"cmp", cmpRes,
			"currentState", currentState,
			"scalingAllowed", scalingAllowed(lastStateChange),
			"isLagging", isLagging,
			"saturationLevel", underProvision,
		)
		if isChangedAnnotations {
			needsUpdate = true
		}
	}
	return deploy, needsUpdate, nil
}

func (co *consumerOperator) updateDeployWithPredictor(deploy *appsv1.Deployment, predictor predictors.Predictor) (*appsv1.Deployment, error) {
	partition, err := helpers.ParsePartitionAnnotation(deploy.Annotations[partitionAnnotation])
	if err != nil {
		return nil, err
	}
	deploy.Annotations[generationAnnotation] = co.observedGeneration()
	deploy.Spec = co.consumer.Spec.DeploymentTemplate
	for i := range deploy.Spec.Template.Spec.Containers {
		container := &deploy.Spec.Template.Spec.Containers[i]
		resources, underProvision := estimateResources(partition, container.Name, predictor, co.limiter)
		container.Resources = resources
		if underProvision > 0 {
			updateScalingStatus(deploy, InstanceStatusSaturated)
			deploy.Annotations[cpuSaturationLevel] = strconv.Itoa(int(underProvision))
		} else {
			updateScalingStatus(deploy, InstanceStatusRunning)
			delete(deploy.Annotations, cpuSaturationLevel)
		}
		container.Env = helpers.PopulateEnv(container.Env, &container.Resources, co.consumer.Spec.PartitionEnvKey, int(partition))
	}
	return deploy, nil
}

func (co *consumerOperator) constructDeploy(partition int32) *appsv1.Deployment {
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
	updateScalingStatus(deploy, InstanceStatusRunning)
	return deploy
}

func estimateResources(partition int32, containerName string, predictor predictors.Predictor, limiter limiters.ResourceLimiter) (corev1.ResourceRequirements, int64) {
	estimates := predictor.Estimate(containerName, partition)
	resources := limiter.ApplyLimits(containerName, estimates)
	reqDiff := estimates.Requests.Cpu().MilliValue() - resources.Requests.Cpu().MilliValue()
	return *resources, reqDiff
}

func deployIsPaused(d *appsv1.Deployment) bool {
	_, pausedAnnotation := d.Annotations[disableAutoscalerAnnotation]
	return d.Status.Replicas == 0 || pausedAnnotation
}

func updateScaleAnnotations(d *appsv1.Deployment, underProvision int64) bool {
	if underProvision > 0 {
		d.Annotations[cpuSaturationLevel] = strconv.Itoa(int(underProvision))
		deploymentSaturation.WithLabelValues(d.Name).Set(float64(underProvision))
		return updateScalingStatus(d, InstanceStatusSaturated)
	}
	delete(d.Annotations, cpuSaturationLevel)
	deploymentSaturation.WithLabelValues(d.Name).Set(float64(0))
	return updateScalingStatus(d, InstanceStatusRunning)
}

func updateScalingStatus(d *appsv1.Deployment, newStatus string) bool {
	curStatus := d.Annotations[scalingStatusAnnotation]
	if curStatus == newStatus {
		return false
	}
	d.Annotations[scalingStatusAnnotation] = newStatus
	d.Annotations[scalingStatusChangeAnnotation] = time.Now().Format(helpers.TimeLayout)
	ds := float64(instanceStatusToInt(newStatus))
	deploymentStatus.WithLabelValues(d.Name).Set(ds)
	return true
}
