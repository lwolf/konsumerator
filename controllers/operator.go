package controllers

import (
	"context"
	"fmt"
	"github.com/lwolf/konsumerator/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"github.com/mitchellh/hashstructure"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/clock"
	ctrl "sigs.k8s.io/controller-runtime"

	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
	"github.com/lwolf/konsumerator/pkg/helpers"
	"github.com/lwolf/konsumerator/pkg/limiters"
	"github.com/lwolf/konsumerator/pkg/predictors"
	"github.com/lwolf/konsumerator/pkg/providers"
)

type operator struct {
	owner    metav1.Object
	consumer *konsumeratorv1alpha1.Consumer
	// new
	Recorder record.EventRecorder
	Scheme   *runtime.Scheme

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

func (o *operator) init(consumer *konsumeratorv1alpha1.Consumer, managedDeploys appsv1.DeploymentList) error {
	hash, err := hashstructure.Hash(consumer.Spec.DeploymentTemplate, nil)
	if err != nil {
		return err
	}
	consumer.Status.ObservedGeneration = helpers.Ptr2Int64(int64(hash))
	rl := make(corev1.ResourceList, 0)
	// TODO: refactor following list of actions
	o.consumer = consumer
	o.usedResources = &rl
	o.mp = o.newMetricsProvider()
	o.syncDeploys(managedDeploys)

	o.limiter = limiters.NewInstanceLimiter(consumer.Spec.ResourcePolicy, o.log)
	o.globalLimiter = limiters.NewGlobalLimiter(consumer.Spec.ResourcePolicy, o.usedResources, o.log)
	if o.consumer.Spec.Autoscaler == nil || o.consumer.Spec.Autoscaler.Prometheus == nil {
		return fmt.Errorf("Spec.Autoscaler.Prometheus can't be empty")
	}
	o.predictor = predictors.NewNaivePredictor(o.log, o.mp, o.consumer.Spec.Autoscaler.Prometheus)

	return nil
}

func (o *operator) reconcile(cl client.Client, req ctrl.Request) error {
	ctx := context.Background()
	for _, partition := range o.missingIds {
		newD, err := o.newDeploy(partition)
		if err != nil {
			deploymentsCreateErrors.WithLabelValues(req.Name).Inc()
			o.log.Error(err, "failed to create new deploy")
			continue
		}
		o.setOwner(newD)
		if err := cl.Create(ctx, newD); errors.IgnoreAlreadyExists(err) != nil {
			deploymentsCreateErrors.WithLabelValues(req.Name).Inc()
			o.log.Error(err, "unable to create new Deployment", "deployment", newD, "partition", partition)
			continue
		}
		o.log.V(1).Info("created new deployment", "deployment", newD, "partition", partition)
		deploymentsCreateTotal.WithLabelValues(req.Name).Inc()
		// o.Recorder.Eventf(
		// 	o.consumer,
		// 	corev1.EventTypeNormal,
		// 	"DeployCreate",
		// 	"deployment for partition was created %d", partition,
		// )
	}

	for _, deploy := range o.toRemoveInstances {
		if err := cl.Delete(ctx, deploy); errors.IgnoreNotFound(err) != nil {
			o.log.Error(err, "unable to delete deployment", "deployment", deploy)
			deploymentsDeleteErrors.WithLabelValues(req.Name).Inc()
			continue
		}
		deploymentsDeleteTotal.WithLabelValues(req.Name).Inc()
		// o.Recorder.Eventf(
		// 	o.consumer,
		// 	corev1.EventTypeNormal,
		// 	"DeployDelete",
		// 	"deployment %s was deleted", deploy.Name,
		// )
	}

	for _, origDeploy := range o.toUpdateInstances {
		deploy, err := o.updateDeploy(origDeploy.DeepCopy())
		if err != nil {
			deploymentsUpdateErrors.WithLabelValues(req.Name).Inc()
			o.log.Error(err, "failed to update deploy")
			continue
		}
		o.setOwner(deploy)
		if err := cl.Update(ctx, deploy); errors.IgnoreConflict(err) != nil {
			deploymentsUpdateErrors.WithLabelValues(req.Name).Inc()
			o.log.Error(err, "unable to update deployment", "deployment", deploy)
			continue
		}
		deploymentsUpdateTotal.WithLabelValues(req.Name).Inc()
	}

	for _, origDeploy := range o.toEstimateInstances {
		deploy, needsUpdate, err := o.estimateDeploy(origDeploy.DeepCopy())
		if err != nil {
			deploymentsUpdateErrors.WithLabelValues(req.Name).Inc()
			o.log.Error(err, "failed to update deploy")
			continue
		}
		if !needsUpdate {
			continue
		}
		o.setOwner(deploy)
		if err := cl.Update(ctx, deploy); errors.IgnoreConflict(err) != nil {
			deploymentsUpdateErrors.WithLabelValues(req.Name).Inc()
			o.log.Error(err, "unable to update deployment", "deployment", deploy)
			continue
		}
		deploymentsUpdateTotal.WithLabelValues(req.Name).Inc()
	}
	return nil
}

func (o operator) observedGeneration() string {
	return strconv.Itoa(int(*o.consumer.Status.ObservedGeneration))
}

func (o operator) isLagging(lag time.Duration) bool {
	tolerableLag := o.consumer.Spec.Autoscaler.Prometheus.TolerableLag
	if tolerableLag == nil {
		return false
	}
	return lag >= tolerableLag.Duration
}

func (o *operator) isAutoScaleEnabled() bool {
	_, autoscalerDisabled := o.consumer.Annotations[DisableAutoscalerAnnotation]
	return !autoscalerDisabled && o.consumer.Spec.Autoscaler != nil
}

func (o *operator) newMetricsProvider() providers.MetricsProvider {
	defaultProvider := providers.NewDummyMP(*o.consumer.Spec.NumPartitions)
	shouldUpdate, err := shouldUpdateMetrics(o.consumer, o.clock.Now())
	if err != nil {
		o.log.Error(err, "failed to verify autoscaler configuration")
		return defaultProvider
	}
	if !o.isAutoScaleEnabled() {
		return defaultProvider
	}
	switch o.consumer.Spec.Autoscaler.Mode {
	case konsumeratorv1alpha1.AutoscalerTypePrometheus:
		// setup prometheus metrics provider
		mp, err := providers.NewPrometheusMP(o.log, o.consumer.Spec.Autoscaler.Prometheus, o.consumer.Name)
		if err != nil {
			o.log.Error(err, "failed to initialize Prometheus Metrics Provider")
			return defaultProvider
		}
		providers.LoadSyncState(mp, o.consumer.Status)
		if shouldUpdate {
			if err := mp.Update(); err != nil {
				o.log.Error(err, "failed to query metrics from Prometheus Metrics Provider")
			} else {
				tm := metav1.NewTime(o.clock.Now())
				o.metricsUpdated = true
				o.consumer.Status.LastSyncTime = &tm
				o.consumer.Status.LastSyncState = providers.DumpSyncState(*o.consumer.Spec.NumPartitions, mp)
				o.log.Info("metrics data were updated successfully")
			}
		}
		return mp
	default:
		return defaultProvider
	}
}

func (o *operator) syncDeploys(managedDeploys appsv1.DeploymentList) {
	trackedPartitions := make(map[int32]bool)
	for i := range managedDeploys.Items {
		deploy := &managedDeploys.Items[i]
		partition, err := helpers.ParsePartitionAnnotation(deploy.Annotations[PartitionAnnotation])
		if err != nil {
			o.log.Error(err, "failed to parse annotation with partition number. Panic!!!")
			continue
		}
		trackedPartitions[partition] = true
		lag := o.mp.GetLagByPartition(partition)
		o.log.V(1).Info("lag per partition", "partition", partition, "lag", lag)
		if deployIsPaused(deploy) {
			o.pausedIds = append(o.pausedIds, partition)
			continue
		} else {
			o.runningIds = append(o.runningIds, partition)
		}
		if o.isLagging(lag) {
			o.laggingIds = append(o.laggingIds, partition)
		}
		if partition >= *o.consumer.Spec.NumPartitions {
			o.toRemoveInstances = append(o.toRemoveInstances, deploy)
			continue
		}

		// count used resources by each container in deployment
		for _, container := range deploy.Spec.Template.Spec.Containers {
			r := container.Resources.Requests
			o.usedResources.Cpu().Add(*r.Cpu())
			o.usedResources.Memory().Add(*r.Memory())
		}

		if deploy.Annotations[GenerationAnnotation] != o.observedGeneration() {
			o.toUpdateInstances = append(o.toUpdateInstances, deploy)
			continue
		}
		if o.metricsUpdated {
			o.toEstimateInstances = append(o.toEstimateInstances, deploy)
		}
	}
	for i := int32(0); i < *o.consumer.Spec.NumPartitions; i++ {
		if _, ok := trackedPartitions[i]; !ok {
			o.missingIds = append(o.missingIds, i)
		}
	}

	status := &o.consumer.Status
	status.Running = helpers.Ptr2Int32(int32(len(o.runningIds)))
	status.Paused = helpers.Ptr2Int32(int32(len(o.pausedIds)))
	status.Lagging = helpers.Ptr2Int32(int32(len(o.laggingIds)))
	status.Outdated = helpers.Ptr2Int32(int32(len(o.toUpdateInstances)))
	status.Expected = o.consumer.Spec.NumPartitions
	status.Missing = helpers.Ptr2Int32(int32(len(o.missingIds)))

	name := o.consumer.Name
	consumerStatus.WithLabelValues(name, "running").Set(float64(*status.Running))
	consumerStatus.WithLabelValues(name, "paused").Set(float64(*status.Paused))
	consumerStatus.WithLabelValues(name, "lagging").Set(float64(*status.Lagging))
	consumerStatus.WithLabelValues(name, "outdated").Set(float64(*status.Outdated))
	consumerStatus.WithLabelValues(name, "expected").Set(float64(*status.Expected))
	consumerStatus.WithLabelValues(name, "missing").Set(float64(*status.Missing))

	o.log.V(1).Info(
		"deployments count",
		"metricsUpdated", o.metricsUpdated,
		"expected", o.consumer.Spec.NumPartitions,
		"running", status.Running,
		"paused", status.Paused,
		"missing", status.Missing,
		"lagging", status.Lagging,
		"toUpdate", status.Outdated,
		"toEstimate", len(o.toEstimateInstances),
	)
}

func (o *operator) setOwner(deploy *appsv1.Deployment) {
	ownerRef := metav1.GetControllerOf(deploy)
	if ownerRef == nil || ownerRef.UID != o.consumer.UID {
		if err := ctrl.SetControllerReference(o.owner, deploy, o.Scheme); err != nil {
			o.log.Error(err, "unable to set owner reference", "deployment", deploy)
		}
	}
}

func (o *operator) newDeploy(partition int32) (*appsv1.Deployment, error) {
	deploy := o.constructDeploy(partition)
	return o.updateDeploy(deploy)
}

func (o *operator) estimateDeploy(deploy *appsv1.Deployment) (*appsv1.Deployment, bool, error) {
	if o.clock.Since(o.consumer.Status.LastSyncTime.Time) >= scaleStatePendingPeriod {
		return deploy, false, nil
	}
	partition, err := helpers.ParsePartitionAnnotation(deploy.Annotations[PartitionAnnotation])
	if err != nil {
		return nil, false, err
	}
	currentState := deploy.Annotations[ScalingStatusAnnotation]
	lastStateChange, err := helpers.ParseTimeAnnotation(deploy.Annotations[ScalingStatusChangeAnnotation])
	if err != nil {
		return nil, false, err
	}

	lag := o.mp.GetLagByPartition(partition)
	isLagging := o.isLagging(lag)
	needsUpdate := false
	for i := range deploy.Spec.Template.Spec.Containers {
		isChangedAnnotations := false
		container := &deploy.Spec.Template.Spec.Containers[i]
		resources, underProvision := o.updateResources(container, partition)
		cmpRes := helpers.CmpResourceRequirements(deploy.Spec.Template.Spec.Containers[i].Resources, *resources)
		switch cmpRes {
		case cmpResourcesEq:
			isChangedAnnotations = o.updateScaleAnnotations(deploy, underProvision)
		case cmpResourcesGt:
			if isLagging {
				if currentState == InstanceStatusPendingScaleUp && o.scalingAllowed(lastStateChange) {
					o.updateScaleAnnotations(deploy, underProvision)
					container.Resources = *resources
					container.Env = helpers.PopulateEnv(container.Env, &container.Resources, o.consumer.Spec.PartitionEnvKey, int(partition))
					needsUpdate = true
				} else if currentState == InstanceStatusSaturated {
					isChangedAnnotations = o.updateScaleAnnotations(deploy, underProvision)
				} else {
					isChangedAnnotations = o.updateScalingStatus(deploy, InstanceStatusPendingScaleUp)
				}
			}
		case cmpResourcesLt:
			if !isLagging {
				if currentState == InstanceStatusPendingScaleDown && o.scalingAllowed(lastStateChange) {
					o.updateScaleAnnotations(deploy, underProvision)
					container.Resources = *resources
					container.Env = helpers.PopulateEnv(container.Env, &container.Resources, o.consumer.Spec.PartitionEnvKey, int(partition))
					needsUpdate = true
				} else {
					isChangedAnnotations = o.updateScalingStatus(deploy, InstanceStatusPendingScaleDown)
				}
			}
		}
		o.log.Info(
			"cmp resource",
			"partition", partition,
			"container", container.Name,
			"cmp", cmpRes,
			"currentState", currentState,
			"scalingAllowed", o.scalingAllowed(lastStateChange),
			"isLagging", isLagging,
			"saturationLevel", underProvision,
		)
		if isChangedAnnotations {
			needsUpdate = true
		}
	}
	return deploy, needsUpdate, nil
}

func (o *operator) updateDeploy(deploy *appsv1.Deployment) (*appsv1.Deployment, error) {
	partition, err := helpers.ParsePartitionAnnotation(deploy.Annotations[PartitionAnnotation])
	if err != nil {
		return nil, err
	}
	deploy.Annotations[GenerationAnnotation] = o.observedGeneration()
	deploy.Spec = o.consumer.Spec.DeploymentTemplate
	for i := range deploy.Spec.Template.Spec.Containers {
		var resources *corev1.ResourceRequirements
		var underProvision int64
		container := &deploy.Spec.Template.Spec.Containers[i]
		if container.Resources.Requests.Cpu().IsZero() {
			resources, underProvision = o.allocateResources(container, partition)
		} else {
			resources, underProvision = o.updateResources(container, partition)
		}
		o.updateScaleAnnotations(deploy, underProvision)
		container.Resources = *resources
		container.Env = helpers.PopulateEnv(container.Env, &container.Resources, o.consumer.Spec.PartitionEnvKey, int(partition))
	}
	return deploy, nil
}

func (o *operator) constructDeploy(partition int32) *appsv1.Deployment {
	deployLabels := make(map[string]string)
	deployAnnotations := make(map[string]string)
	deploy := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Labels:      deployLabels,
			Annotations: deployAnnotations,
			Name:        fmt.Sprintf("%s-%d", o.consumer.Spec.Name, partition),
			Namespace:   o.consumer.Spec.Namespace,
		},
		Spec: o.consumer.Spec.DeploymentTemplate,
	}
	deploy.Annotations[PartitionAnnotation] = strconv.Itoa(int(partition))
	deploy.Annotations[GenerationAnnotation] = o.observedGeneration()
	o.updateScalingStatus(deploy, InstanceStatusRunning)
	return deploy
}

func (o *operator) allocateResources(container *corev1.Container, partition int32) (*corev1.ResourceRequirements, int64) {
	estimates := o.predictor.Estimate(container.Name, partition)
	resources := o.limiter.ApplyLimits(container.Name, estimates)
	reqDiff := estimates.Requests.Cpu().MilliValue() - resources.Requests.Cpu().MilliValue()
	return resources, reqDiff
}

func (o *operator) updateResources(container *corev1.Container, partition int32) (*corev1.ResourceRequirements, int64) {
	estimatedResources, reqDiff := o.allocateResources(container, partition)
	currentResources := container.Resources.DeepCopy()

	request := resourceRequirementsDiff(estimatedResources, currentResources)
	requestedResources := o.globalLimiter.ApplyLimits("", request)
	if requestedResources == nil {
		// global limiter exhausted
		// return existing resources
		return &container.Resources, reqDiff
	}

	// sum-up current and requested resources
	limitedResources := resourceRequirementsSum(currentResources, requestedResources)
	globalDiff := request.Requests.Cpu().MilliValue() - requestedResources.Requests.Cpu().MilliValue()
	return limitedResources, reqDiff + globalDiff
}

func (o *operator) updateScaleAnnotations(d *appsv1.Deployment, underProvision int64) bool {
	if underProvision > 0 {
		d.Annotations[CPUSaturationLevel] = strconv.Itoa(int(underProvision))
		deploymentSaturation.WithLabelValues(o.consumer.Name, d.Name).Set(float64(underProvision))
		return o.updateScalingStatus(d, InstanceStatusSaturated)
	}
	delete(d.Annotations, CPUSaturationLevel)
	deploymentSaturation.WithLabelValues(o.consumer.Name, d.Name).Set(float64(0))
	return o.updateScalingStatus(d, InstanceStatusRunning)
}

func (o *operator) updateScalingStatus(d *appsv1.Deployment, newStatus string) bool {
	curStatus := d.Annotations[ScalingStatusAnnotation]
	if curStatus == newStatus {
		return false
	}
	d.Annotations[ScalingStatusAnnotation] = newStatus
	d.Annotations[ScalingStatusChangeAnnotation] = o.clock.Now().Format(helpers.TimeLayout)
	ds := float64(instanceStatusToInt(newStatus))
	deploymentStatus.WithLabelValues(o.consumer.Name, d.Name).Set(ds)
	return true
}

func (o *operator) scalingAllowed(lastChange time.Time) bool {
	return o.clock.Since(lastChange) >= scaleStatePendingPeriod
}
