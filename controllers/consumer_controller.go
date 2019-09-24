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
	"github.com/google/go-cmp/cmp"
	"github.com/mitchellh/hashstructure"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/clock"
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

	PartitionAnnotation           = "konsumerator.lwolf.org/partition"
	DisableAutoscalerAnnotation   = "konsumerator.lwolf.org/disable-autoscaler"
	GenerationAnnotation          = "konsumerator.lwolf.org/generation"
	CPUSaturationLevel            = "konsumerator.lwolf.org/cpu-saturation-level"
	ScalingStatusAnnotation       = "konsumerator.lwolf.org/scaling-status"
	scalingStatusChangeAnnotation = "konsumerator.lwolf.org/scaling-status-change"
	OwnerKey                      = ".metadata.controller"

	cmpResourcesLt int = -1
	cmpResourcesEq int = 0
	cmpResourcesGt int = 1

	defaultMinSyncPeriod    = time.Minute
	scaleStatePendingPeriod = time.Minute * 5
)

var apiGVStr = konsumeratorv1alpha1.GroupVersion.String()

// ConsumerReconciler reconciles a Consumer object
type ConsumerReconciler struct {
	client.Client

	Log      logr.Logger
	Recorder record.EventRecorder
	Scheme   *runtime.Scheme
	Clock    clock.Clock
}

func (r *ConsumerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Clock == nil {
		r.Clock = clock.RealClock{}
	}

	if err := mgr.GetFieldIndexer().IndexField(&appsv1.Deployment{}, OwnerKey, func(rawObj runtime.Object) []string {
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

// +kubebuilder:rbac:groups=konsumerator.lwolf.org,resources=consumers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konsumerator.lwolf.org,resources=consumers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=watch;create;get;update;patch;delete;list
// +kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get

func (r *ConsumerReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	reconcileTotal.WithLabelValues(req.Name).Inc()
	start := time.Now()
	defer func() {
		reconcileDuration.WithLabelValues(req.Name).Observe(time.Since(start).Seconds())
	}()

	ctx := context.Background()
	log := r.Log.WithValues("consumer", req.NamespacedName)
	result := ctrl.Result{RequeueAfter: defaultMinSyncPeriod}

	var consumer konsumeratorv1alpha1.Consumer
	if err := r.Get(ctx, req.NamespacedName, &consumer); err != nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		reconcileErrors.WithLabelValues(req.Name).Inc()
		return ctrl.Result{}, errors.IgnoreNotFound(err)
	}
	var managedDeploys appsv1.DeploymentList
	if err := r.List(ctx, &managedDeploys, client.InNamespace(req.Namespace), client.MatchingField(OwnerKey, req.Name)); err != nil {
		eMsg := "unable to list managed deployments"
		log.Error(err, eMsg)
		r.Recorder.Event(&consumer, corev1.EventTypeWarning, "ListDeployFailure", eMsg)
		reconcileErrors.WithLabelValues(req.Name).Inc()
		return ctrl.Result{}, err
	}

	co, err := newConsumerOperator(log, consumer.DeepCopy(), managedDeploys, r.Clock)
	if err != nil {
		reconcileErrors.WithLabelValues(req.Name).Inc()
		return ctrl.Result{}, err
	}

	if cmp.Equal(consumer.Status, co.consumer.Status) {
		log.V(1).Info("no change detected...")
		return result, nil
	}

	start = time.Now()
	if err := r.Status().Update(ctx, co.consumer); err != nil {
		properError := errors.IgnoreConflict(err)
		if properError != nil {
			eMsg := "unable to update Consumer status"
			log.Error(err, eMsg)
			r.Recorder.Event(co.consumer, corev1.EventTypeWarning, "UpdateConsumerStatus", eMsg)
			reconcileErrors.WithLabelValues(req.Name).Inc()
		}
		return result, properError
	}
	statusUpdateDuration.WithLabelValues(req.Name).Observe(time.Since(start).Seconds())

	for _, partition := range co.missingIds {
		newD, err := co.newDeploy(partition)
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
		deploy, err := co.updateDeploy(origDeploy.DeepCopy())
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
		deploy, needsUpdate, err := co.estimateDeploy(origDeploy.DeepCopy())
		if err != nil {
			deploymentsUpdateErrors.WithLabelValues(req.Name).Inc()
			log.Error(err, "failed to update deploy")
			continue
		}
		if !needsUpdate {
			continue
		}
		if err := r.Update(ctx, deploy); errors.IgnoreConflict(err) != nil {
			deploymentsUpdateErrors.WithLabelValues(req.Name).Inc()
			log.Error(err, "unable to update deployment", "deployment", deploy)
			continue
		}
		deploymentsUpdateTotal.WithLabelValues(req.Name).Inc()
	}

	return result, nil
}

type consumerOperator struct {
	consumer *konsumeratorv1alpha1.Consumer

	limiter       limiters.ResourceLimiter
	globalLimiter limiters.ResourceLimiter

	usedResources *corev1.ResourceList

	predictor predictors.Predictor

	log logr.Logger
	mp  providers.MetricsProvider

	// XXX: should it be a part of mp?
	metricsUpdated bool

	missingIds          []int32
	pausedIds           []int32
	runningIds          []int32
	laggingIds          []int32
	toRemoveInstances   []*appsv1.Deployment
	toUpdateInstances   []*appsv1.Deployment
	toEstimateInstances []*appsv1.Deployment

	clock clock.Clock
}

func newConsumerOperator(log logr.Logger, consumer *konsumeratorv1alpha1.Consumer, managedDeploys appsv1.DeploymentList, clock clock.Clock) (*consumerOperator, error) {
	hash, err := hashstructure.Hash(consumer.Spec.DeploymentTemplate, nil)
	if err != nil {
		return nil, err
	}
	consumer.Status.ObservedGeneration = helpers.Ptr2Int64(int64(hash))
	rl := make(corev1.ResourceList, 0)
	co := &consumerOperator{
		consumer:      consumer,
		limiter:       limiters.NewInstanceLimiter(consumer.Spec.ResourcePolicy, log),
		log:           log,
		usedResources: &rl,
		clock:         clock,
	}
	// TODO: refactor following list of actions
	co.mp = co.newMetricsProvider()
	co.syncDeploys(managedDeploys)

	co.globalLimiter = limiters.NewGlobalLimiter(consumer.Spec.ResourcePolicy, co.usedResources, log)
	if co.consumer.Spec.Autoscaler == nil || co.consumer.Spec.Autoscaler.Prometheus == nil {
		return nil, fmt.Errorf("Spec.Autoscaler.Prometheus can't be empty")
	}
	co.predictor = predictors.NewNaivePredictor(log, co.mp, co.consumer.Spec.Autoscaler.Prometheus)

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

func (co *consumerOperator) isAutoScaleEnabled() bool {
	_, autoscalerDisabled := co.consumer.Annotations[DisableAutoscalerAnnotation]
	return !autoscalerDisabled && co.consumer.Spec.Autoscaler != nil
}

func (co *consumerOperator) newMetricsProvider() providers.MetricsProvider {
	defaultProvider := providers.NewDummyMP(*co.consumer.Spec.NumPartitions)
	shouldUpdate, err := shouldUpdateMetrics(co.consumer, co.clock.Now())
	if err != nil {
		co.log.Error(err, "failed to verify autoscaler configuration")
		return defaultProvider
	}
	if !co.isAutoScaleEnabled() {
		return defaultProvider
	}
	switch co.consumer.Spec.Autoscaler.Mode {
	case konsumeratorv1alpha1.AutoscalerTypePrometheus:
		// setup prometheus metrics provider
		mp, err := providers.NewPrometheusMP(co.log, co.consumer.Spec.Autoscaler.Prometheus, co.consumer.Name)
		if err != nil {
			co.log.Error(err, "failed to initialize Prometheus Metrics Provider")
			return defaultProvider
		}
		providers.LoadSyncState(mp, co.consumer.Status)
		if shouldUpdate {
			if err := mp.Update(); err != nil {
				co.log.Error(err, "failed to query metrics from Prometheus Metrics Provider")
			} else {
				tm := metav1.NewTime(co.clock.Now())
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

func (co *consumerOperator) syncDeploys(managedDeploys appsv1.DeploymentList) {
	trackedPartitions := make(map[int32]bool)
	for i := range managedDeploys.Items {
		deploy := &managedDeploys.Items[i]
		partition, err := helpers.ParsePartitionAnnotation(deploy.Annotations[PartitionAnnotation])
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

		// count used resources by each container in deployment
		for _, container := range deploy.Spec.Template.Spec.Containers {
			r := container.Resources.Requests
			co.usedResources.Cpu().Add(*r.Cpu())
			co.usedResources.Memory().Add(*r.Memory())
		}

		if deploy.Annotations[GenerationAnnotation] != co.observedGeneration() {
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

	name := co.consumer.Name
	consumerStatus.WithLabelValues(name, "running").Set(float64(*status.Running))
	consumerStatus.WithLabelValues(name, "paused").Set(float64(*status.Paused))
	consumerStatus.WithLabelValues(name, "lagging").Set(float64(*status.Lagging))
	consumerStatus.WithLabelValues(name, "outdated").Set(float64(*status.Outdated))
	consumerStatus.WithLabelValues(name, "expected").Set(float64(*status.Expected))
	consumerStatus.WithLabelValues(name, "missing").Set(float64(*status.Missing))

	co.log.V(1).Info(
		"deployments count",
		"metricsUpdated", co.metricsUpdated,
		"expected", co.consumer.Spec.NumPartitions,
		"running", status.Running,
		"paused", status.Paused,
		"missing", status.Missing,
		"lagging", status.Lagging,
		"toUpdate", status.Outdated,
		"toEstimate", len(co.toEstimateInstances),
	)
}

func (co *consumerOperator) newDeploy(partition int32) (*appsv1.Deployment, error) {
	deploy := co.constructDeploy(partition)
	return co.updateDeploy(deploy)
}

func (co *consumerOperator) estimateDeploy(deploy *appsv1.Deployment) (*appsv1.Deployment, bool, error) {
	if co.clock.Since(co.consumer.Status.LastSyncTime.Time) >= scaleStatePendingPeriod {
		return deploy, false, nil
	}
	partition, err := helpers.ParsePartitionAnnotation(deploy.Annotations[PartitionAnnotation])
	if err != nil {
		return nil, false, err
	}
	currentState := deploy.Annotations[ScalingStatusAnnotation]
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
		resources, underProvision := co.updateResources(container, partition)
		cmpRes := helpers.CmpResourceRequirements(deploy.Spec.Template.Spec.Containers[i].Resources, *resources)
		switch cmpRes {
		case cmpResourcesEq:
			isChangedAnnotations = co.updateScaleAnnotations(deploy, underProvision)
		case cmpResourcesGt:
			if currentState == InstanceStatusPendingScaleUp && co.scalingAllowed(lastStateChange) {
				co.updateScaleAnnotations(deploy, underProvision)
				container.Resources = *resources
				container.Env = helpers.PopulateEnv(container.Env, &container.Resources, co.consumer.Spec.PartitionEnvKey, int(partition))
				needsUpdate = true
			} else if currentState == InstanceStatusSaturated {
				isChangedAnnotations = co.updateScaleAnnotations(deploy, underProvision)
			} else {
				isChangedAnnotations = co.updateScalingStatus(deploy, InstanceStatusPendingScaleUp)
			}
		case cmpResourcesLt:
			if currentState == InstanceStatusPendingScaleDown && co.scalingAllowed(lastStateChange) {
				co.updateScaleAnnotations(deploy, underProvision)
				container.Resources = *resources
				container.Env = helpers.PopulateEnv(container.Env, &container.Resources, co.consumer.Spec.PartitionEnvKey, int(partition))
				needsUpdate = true
			} else if !isLagging {
				isChangedAnnotations = co.updateScalingStatus(deploy, InstanceStatusPendingScaleDown)
			}
		}
		co.log.Info(
			"cmp resource",
			"partition", partition,
			"container", container.Name,
			"cmp", cmpRes,
			"currentState", currentState,
			"scalingAllowed", co.scalingAllowed(lastStateChange),
			"isLagging", isLagging,
			"saturationLevel", underProvision,
		)
		if isChangedAnnotations {
			needsUpdate = true
		}
	}
	return deploy, needsUpdate, nil
}

func (co *consumerOperator) updateDeploy(deploy *appsv1.Deployment) (*appsv1.Deployment, error) {
	partition, err := helpers.ParsePartitionAnnotation(deploy.Annotations[PartitionAnnotation])
	if err != nil {
		return nil, err
	}
	deploy.Annotations[GenerationAnnotation] = co.observedGeneration()
	deploy.Spec = co.consumer.Spec.DeploymentTemplate
	for i := range deploy.Spec.Template.Spec.Containers {
		var resources *corev1.ResourceRequirements
		var underProvision int64
		container := &deploy.Spec.Template.Spec.Containers[i]
		if container.Resources.Requests.Cpu().IsZero() {
			resources, underProvision = co.allocateResources(container, partition)
		} else {
			resources, underProvision = co.updateResources(container, partition)
		}
		co.updateScaleAnnotations(deploy, underProvision)
		container.Resources = *resources
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
	deploy.Annotations[PartitionAnnotation] = strconv.Itoa(int(partition))
	deploy.Annotations[GenerationAnnotation] = co.observedGeneration()
	co.updateScalingStatus(deploy, InstanceStatusRunning)
	return deploy
}

func (co *consumerOperator) allocateResources(container *corev1.Container, partition int32) (*corev1.ResourceRequirements, int64) {
	estimates := co.predictor.Estimate(container.Name, partition)
	resources := co.limiter.ApplyLimits(container.Name, estimates)
	reqDiff := estimates.Requests.Cpu().MilliValue() - resources.Requests.Cpu().MilliValue()
	return resources, reqDiff
}

func (co *consumerOperator) updateResources(container *corev1.Container, partition int32) (*corev1.ResourceRequirements, int64) {
	estimatedResources, reqDiff := co.allocateResources(container, partition)
	currentResources := container.Resources.DeepCopy()

	request := resourceRequirementsDiff(estimatedResources, currentResources)
	requestedResources := co.globalLimiter.ApplyLimits("", request)
	if requestedResources == nil {
		// global limiter exhausted
		// return existing resources
		return &container.Resources, reqDiff
	}

	//// sum-up current and requested resources
	limitedResources := resourceRequirementsSum(currentResources, requestedResources)
	globalDiff := request.Requests.Cpu().MilliValue() - requestedResources.Requests.Cpu().MilliValue()
	return limitedResources, reqDiff + globalDiff
}

func (co *consumerOperator) updateScaleAnnotations(d *appsv1.Deployment, underProvision int64) bool {
	if underProvision > 0 {
		d.Annotations[CPUSaturationLevel] = strconv.Itoa(int(underProvision))
		deploymentSaturation.WithLabelValues(co.consumer.Name, d.Name).Set(float64(underProvision))
		return co.updateScalingStatus(d, InstanceStatusSaturated)
	}
	delete(d.Annotations, CPUSaturationLevel)
	deploymentSaturation.WithLabelValues(co.consumer.Name, d.Name).Set(float64(0))
	return co.updateScalingStatus(d, InstanceStatusRunning)
}

func (co *consumerOperator) updateScalingStatus(d *appsv1.Deployment, newStatus string) bool {
	curStatus := d.Annotations[ScalingStatusAnnotation]
	if curStatus == newStatus {
		return false
	}
	d.Annotations[ScalingStatusAnnotation] = newStatus
	d.Annotations[scalingStatusChangeAnnotation] = co.clock.Now().Format(helpers.TimeLayout)
	ds := float64(instanceStatusToInt(newStatus))
	deploymentStatus.WithLabelValues(co.consumer.Name, d.Name).Set(ds)
	return true
}

func (co *consumerOperator) scalingAllowed(lastChange time.Time) bool {
	return co.clock.Since(lastChange) >= scaleStatePendingPeriod
}
