package controllers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
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

	konsumeratorv1 "github.com/lwolf/konsumerator/api/v1"
	"github.com/lwolf/konsumerator/pkg/errors"
	"github.com/lwolf/konsumerator/pkg/helpers"
	"github.com/lwolf/konsumerator/pkg/limiters"
	"github.com/lwolf/konsumerator/pkg/predictors"
	"github.com/lwolf/konsumerator/pkg/providers"
)

type operator struct {
	owner    metav1.Object
	consumer *konsumeratorv1.Consumer
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

	// partition assignments, indicates relation of consumerId to partitions
	// these arrays are operate on deployments, not partitions
	assignments [][]int32
	missingIds  map[int32]bool
	pausedIds   map[int32]bool
	runningIds  map[int32]bool
	laggingIds  map[int32]bool

	// toRemove is a map of deployment name to the deployment.
	// we can't use consumerId here, because deployment could be without it
	toRemoveInstances map[string]*appsv1.Deployment
	// toUpdate and toEstimate are maps of consumerId to deployment
	toUpdateInstances   map[int32]*appsv1.Deployment
	toEstimateInstances map[int32]*appsv1.Deployment

	clock clock.Clock
}

func (o *operator) init(consumer *konsumeratorv1.Consumer, managedDeploys appsv1.DeploymentList) error {
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
	var groupSize int32 = 1
	if o.consumer.Spec.NumPartitionsPerInstance != nil && *o.consumer.Spec.NumPartitionsPerInstance > 0 {
		groupSize = *o.consumer.Spec.NumPartitionsPerInstance
	}
	o.assignments = helpers.SplitIntoBuckets(*o.consumer.Spec.NumPartitions, groupSize)

	o.missingIds = make(map[int32]bool)
	o.pausedIds = make(map[int32]bool)
	o.runningIds = make(map[int32]bool)
	o.laggingIds = make(map[int32]bool)
	o.toRemoveInstances = make(map[string]*appsv1.Deployment)
	o.toUpdateInstances = make(map[int32]*appsv1.Deployment)
	o.toEstimateInstances = make(map[int32]*appsv1.Deployment)

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
	for consumerId := range o.missingIds {
		newD, err := o.newDeploy(consumerId)
		if err != nil {
			deploymentsCreateErrors.WithLabelValues(req.Name).Inc()
			o.log.Error(err, "failed to create new deploy")
			continue
		}
		o.setOwner(newD)
		if err := cl.Create(ctx, newD); errors.IgnoreAlreadyExists(err) != nil {
			deploymentsCreateErrors.WithLabelValues(req.Name).Inc()
			o.log.Error(err, "unable to create new Deployment", "deployment", newD, "consumerId", consumerId)
			continue
		}
		o.log.V(1).Info("created new deployment", "deployment", newD, "consumerId", consumerId)
		deploymentsCreateTotal.WithLabelValues(req.Name).Inc()
	}

	var deleted int
	for _, deploy := range o.toRemoveInstances {
		deploymentStatus.WithLabelValues(o.consumer.Name, deploy.Name).Set(0)
		if err := cl.Delete(ctx, deploy); errors.IgnoreNotFound(err) != nil {
			o.log.Error(err, "unable to delete deployment", "deployment", deploy)
			deploymentsDeleteErrors.WithLabelValues(req.Name).Inc()
			continue
		}
		deleted += 1
		deploymentsDeleteTotal.WithLabelValues(req.Name).Inc()
	}
	if deleted > 0 {
		o.log.Info("deployments were deleted", "count", deleted)
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
	case konsumeratorv1.AutoscalerTypePrometheus:
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
	// TODO: check that maximum partition is not greater than configured in the spec and ?warn?change?
	// TODO: consider making `numPartitions` optional and just get it from the production rate metrics
	//		* upside: no need to manual intervention when number of kafka partitions increases
	//		* downside: if prometheus is unavailable, we'll be unable to start consumption
	//		* unknown: is it possible to get inconsistent data from Prometheus between syncs? (100,100,95,100)
	buckets := int32(len(o.assignments))
	trackedConsumers := make(map[int32]bool)
	for i := range managedDeploys.Items {
		deploy := &managedDeploys.Items[i]
		consumerId, err := helpers.ParseIntAnnotation(deploy.Annotations[ConsumerAnnotation])
		if err != nil {
			o.log.Error(err, "failed to parse annotation with consumerId. Old deploy?")
			o.toRemoveInstances[deploy.Name] = deploy
			continue
		}
		parsedPartitions, err := helpers.ParsePartitionsListAnnotation(deploy.Annotations[PartitionAnnotation])
		if err != nil {
			o.log.Error(err, "failed to parse annotation with partition number.")
			o.toRemoveInstances[deploy.Name] = deploy
			continue
		}
		updateDeploymentMetrics(deploy, o.consumer.Name)
		trackedConsumers[consumerId] = true
		if consumerId > buckets-1 {
			o.log.Info("deployment with consumerId out of range", "consumerId", consumerId)
			o.toRemoveInstances[deploy.Name] = deploy
			continue
		}
		partitions := o.assignments[consumerId]
		if !cmp.Equal(parsedPartitions, partitions) {
			o.log.Info(
				"partitions from annotation differ from those that should be assigned",
				"expected", partitions,
				"annotation", parsedPartitions,
			)
			o.toRemoveInstances[deploy.Name] = deploy
			continue
		}
		for _, partition := range partitions {
			lag := o.mp.GetLagByPartition(partition)
			o.log.V(1).Info("lag per partition", "partition", partition, "lag", lag)

			if deployIsPaused(deploy) {
				o.pausedIds[consumerId] = true
				continue
			} else {
				o.runningIds[consumerId] = true
			}
			if o.isLagging(lag) {
				o.laggingIds[consumerId] = true
			}
		}
		// count used resources by each container in deployment
		for _, container := range deploy.Spec.Template.Spec.Containers {
			r := container.Resources.Requests
			o.usedResources.Cpu().Add(*r.Cpu())
			o.usedResources.Memory().Add(*r.Memory())
		}

		if deploy.Annotations[GenerationAnnotation] != o.observedGeneration() {
			o.toUpdateInstances[consumerId] = deploy
			continue
		}
		if o.metricsUpdated {
			o.toEstimateInstances[consumerId] = deploy
		}
	}
	for i := int32(0); i < buckets; i++ {
		if _, ok := trackedConsumers[i]; !ok {
			o.missingIds[i] = true
		}
	}

	status := &o.consumer.Status
	status.Running = helpers.Ptr2Int32(int32(len(o.runningIds)))
	status.Paused = helpers.Ptr2Int32(int32(len(o.pausedIds)))
	status.Lagging = helpers.Ptr2Int32(int32(len(o.laggingIds)))
	status.Outdated = helpers.Ptr2Int32(int32(len(o.toUpdateInstances)))
	status.Missing = helpers.Ptr2Int32(int32(len(o.missingIds)))
	status.Redundant = helpers.Ptr2Int32(int32(len(o.toRemoveInstances)))
	status.Expected = &buckets

	name := o.consumer.Name
	consumerStatus.WithLabelValues(name, "running").Set(float64(*status.Running))
	consumerStatus.WithLabelValues(name, "paused").Set(float64(*status.Paused))
	consumerStatus.WithLabelValues(name, "lagging").Set(float64(*status.Lagging))
	consumerStatus.WithLabelValues(name, "outdated").Set(float64(*status.Outdated))
	consumerStatus.WithLabelValues(name, "expected").Set(float64(*status.Expected))
	consumerStatus.WithLabelValues(name, "missing").Set(float64(*status.Missing))
	consumerStatus.WithLabelValues(name, "redundant").Set(float64(*status.Redundant))

	o.log.V(1).Info(
		"deployments count",
		"metricsUpdated", o.metricsUpdated,
		"expected", o.consumer.Spec.NumPartitions,
		"running", status.Running,
		"paused", status.Paused,
		"missing", status.Missing,
		"lagging", status.Lagging,
		"toUpdate", status.Outdated,
		"redundant", status.Redundant,
		"toEstimate", len(o.toEstimateInstances),
	)
}

// updateDeploymentMetrics updates deployment metrics to make sure that we do not have
// missing or stale metrics between app restarts.
func updateDeploymentMetrics(deploy *appsv1.Deployment, consumerName string) {
	status := deploy.Annotations[ScalingStatusAnnotation]
	ds := float64(instanceStatusToInt(status))
	deploymentStatus.WithLabelValues(consumerName, deploy.Name).Set(ds)

	var saturation float64
	saturationStr := deploy.Annotations[CPUSaturationLevel]
	s, err := strconv.Atoi(saturationStr)
	if err != nil {
		saturation = 0
	} else {
		saturation = float64(s)
	}
	deploymentSaturation.WithLabelValues(consumerName, deploy.Name).Set(saturation)
}

func (o *operator) setOwner(deploy *appsv1.Deployment) {
	ownerRef := metav1.GetControllerOf(deploy)
	if ownerRef == nil || ownerRef.UID != o.consumer.UID {
		if err := ctrl.SetControllerReference(o.owner, deploy, o.Scheme); err != nil {
			o.log.Error(err, "unable to set owner reference", "deployment", deploy)
		}
	}
}

func (o *operator) newDeploy(consumerId int32) (*appsv1.Deployment, error) {
	deploy := o.constructDeploy(consumerId)
	return o.updateDeploy(deploy)
}

func (o *operator) estimateDeploy(deploy *appsv1.Deployment) (*appsv1.Deployment, bool, error) {
	// TODO: compare with the minimum of (scaleUpPendingPeriod, scaleDownPendingPeriod)
	if o.clock.Since(o.consumer.Status.LastSyncTime.Time) >= o.scaleUpPendingPeriod() {
		return deploy, false, nil
	}
	consumerId, err := helpers.ParseIntAnnotation(deploy.Annotations[ConsumerAnnotation])
	if err != nil {
		return nil, false, err
	}
	currentState := deploy.Annotations[ScalingStatusAnnotation]
	lastStateChange, err := helpers.ParseTimeAnnotation(deploy.Annotations[ScalingStatusChangeAnnotation])
	if err != nil {
		return nil, false, err
	}

	partitions := o.assignments[consumerId]
	var isLagging bool
	for _, p := range partitions {
		lag := o.mp.GetLagByPartition(p)
		if o.isLagging(lag) {
			isLagging = true
			break
		}
	}
	needsUpdate := false
	for i := range deploy.Spec.Template.Spec.Containers {
		isChangedAnnotations := false
		container := &deploy.Spec.Template.Spec.Containers[i]
		resources, underProvision := o.updateResources(container, partitions)
		cmpRes := helpers.CmpResourceRequirements(deploy.Spec.Template.Spec.Containers[i].Resources, *resources)
		switch cmpRes {
		case cmpResourcesEq:
			isChangedAnnotations = o.updateScaleAnnotations(deploy, underProvision)
		case cmpResourcesGt:
			if isLagging {
				switch currentState {
				case InstanceStatusRunning:
					isChangedAnnotations = o.updateScalingStatus(deploy, InstanceStatusPendingScaleUp)
				case InstanceStatusPendingScaleUp:
					if o.scalingUpAllowed(lastStateChange, currentState) {
						o.updateScaleAnnotations(deploy, underProvision)
						container.Resources = *resources
						container.Env = helpers.PopulateEnv(
							container.Env,
							&container.Resources,
							o.consumer.Spec.PartitionEnvKey,
							partitions,
							int(consumerId),
							int(*o.consumer.Spec.NumPartitions),
							len(o.assignments),
						)
						needsUpdate = true
					}
				case InstanceStatusSaturated:
					isChangedAnnotations = o.updateScaleAnnotations(deploy, underProvision)
				}
			} else {
				isChangedAnnotations = o.updateScaleAnnotations(deploy, underProvision)
			}
		case cmpResourcesLt:
			if !isLagging {
				if o.scalingDownAllowed(lastStateChange, currentState) {
					o.updateScaleAnnotations(deploy, underProvision)
					container.Resources = *resources
					container.Env = helpers.PopulateEnv(
						container.Env,
						&container.Resources,
						o.consumer.Spec.PartitionEnvKey,
						partitions,
						int(consumerId),
						int(*o.consumer.Spec.NumPartitions),
						len(o.assignments),
					)
					needsUpdate = true
				} else {
					isChangedAnnotations = o.updateScalingStatus(deploy, InstanceStatusPendingScaleDown)
				}
			} else {
				isChangedAnnotations = o.updateScaleAnnotations(deploy, underProvision)
			}
		}
		o.log.Info(
			"cmp resource",
			"consumerId", consumerId,
			"partitions", partitions,
			"container", container.Name,
			"cmp", cmpRes,
			"currentState", currentState,
			"scalingUpAllowed", o.scalingUpAllowed(lastStateChange, currentState),
			"scalingDownAllowed", o.scalingDownAllowed(lastStateChange, currentState),
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
	consumerId, err := helpers.ParseIntAnnotation(deploy.Annotations[ConsumerAnnotation])
	if err != nil {
		return nil, err
	}
	partitions := o.assignments[consumerId]
	deploy.Annotations[GenerationAnnotation] = o.observedGeneration()
	deploy.Spec = o.consumer.Spec.DeploymentTemplate
	for i := range deploy.Spec.Template.Spec.Containers {
		var resources *corev1.ResourceRequirements
		var underProvision int64
		container := &deploy.Spec.Template.Spec.Containers[i]
		if container.Resources.Requests.Cpu().IsZero() {
			resources, underProvision = o.allocateResources(container, partitions)
		} else {
			resources, underProvision = o.updateResources(container, partitions)
		}
		o.updateScaleAnnotations(deploy, underProvision)
		container.Resources = *resources
		container.Env = helpers.PopulateEnv(
			container.Env,
			&container.Resources,
			o.consumer.Spec.PartitionEnvKey,
			partitions,
			int(consumerId),
			int(*o.consumer.Spec.NumPartitions),
			len(o.assignments),
		)
	}
	return deploy, nil
}

func (o *operator) constructDeploy(consumerId int32) *appsv1.Deployment {
	partitionIds := o.assignments[consumerId]
	deployLabels := map[string]string{
		"app":        helpers.EnsureValidLabelValue(o.consumer.Spec.Name),
		"controller": helpers.EnsureValidLabelValue(o.consumer.Name),
		"partitions": helpers.EnsureValidLabelValue(helpers.ConsecutiveIntsToRange(partitionIds)),
	}
	deployAnnotations := make(map[string]string)
	deploy := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Labels:      deployLabels,
			Annotations: deployAnnotations,
			Name:        fmt.Sprintf("%s-%d", o.consumer.Spec.Name, consumerId),
			Namespace:   o.consumer.Spec.Namespace,
		},
		Spec: o.consumer.Spec.DeploymentTemplate,
	}
	deploy.Annotations[PartitionAnnotation] = strings.Join(helpers.Int2Str(partitionIds), ",")
	deploy.Annotations[ConsumerAnnotation] = strconv.Itoa(int(consumerId))
	deploy.Annotations[GenerationAnnotation] = o.observedGeneration()
	o.updateScalingStatus(deploy, InstanceStatusRunning)
	return deploy
}

func (o *operator) getMaxLag(partitions []int32) time.Duration {
	var maxLag time.Duration
	for _, p := range partitions {
		lag := o.mp.GetLagByPartition(p)
		if lag > maxLag {
			maxLag = lag
		}
	}
	return maxLag
}

func (o *operator) isCritReached(maxLag time.Duration) bool {
	crit := o.consumer.Spec.Autoscaler.Prometheus.CriticalLag
	// critical lag is not set in the spec
	if crit == nil {
		return false
	}
	// metricsProvider is unavailable or not set
	if o.mp == nil {
		return false
	}
	return maxLag.Seconds() >= crit.Seconds()
}

func (o *operator) allocateResources(container *corev1.Container, partitions []int32) (*corev1.ResourceRequirements, int64) {
	var estimates *corev1.ResourceRequirements
	// Respect criticalLag value. If lag reached criticalLag, allocate maximum allowed resources
	if o.isCritReached(o.getMaxLag(partitions)) {
		maxAllowed := o.limiter.MaxAllowed(container.Name)
		estimates = &corev1.ResourceRequirements{
			Limits:   *maxAllowed,
			Requests: *maxAllowed,
		}
	}
	if estimates == nil {
		estimates = o.predictor.Estimate(container.Name, partitions)
	}

	resources := o.limiter.ApplyLimits(container.Name, estimates)
	reqDiff := estimates.Requests.Cpu().MilliValue() - resources.Requests.Cpu().MilliValue()
	return resources, reqDiff
}

func (o *operator) updateResources(container *corev1.Container, partitions []int32) (*corev1.ResourceRequirements, int64) {
	estimatedResources, reqDiff := o.allocateResources(container, partitions)
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
		oldSaturation := d.Annotations[CPUSaturationLevel]
		d.Annotations[CPUSaturationLevel] = strconv.Itoa(int(underProvision))
		deploymentSaturation.WithLabelValues(o.consumer.Name, d.Name).Set(float64(underProvision))
		// we should return true even if only saturation level annotation changed
		return o.updateScalingStatus(d, InstanceStatusSaturated) || oldSaturation != d.Annotations[CPUSaturationLevel]
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

func (o *operator) scaleUpPendingPeriod() time.Duration {
	if o.consumer.Spec.Autoscaler.PendingScaleUpDuration != nil {
		return o.consumer.Spec.Autoscaler.PendingScaleUpDuration.Duration
	}
	return defaultScaleStatePendingUpPeriod
}

func (o *operator) scaleDownPendingPeriod() time.Duration {
	if o.consumer.Spec.Autoscaler.PendingScaleDownDuration != nil {
		return o.consumer.Spec.Autoscaler.PendingScaleDownDuration.Duration
	}
	return defaultScaleStatePendingDownPeriod
}

func (o *operator) scalingUpAllowed(lastChange time.Time, currentState string) bool {
	return currentState == InstanceStatusPendingScaleUp && o.clock.Since(lastChange) >= o.scaleUpPendingPeriod()
}

func (o *operator) scalingDownAllowed(lastChange time.Time, currentState string) bool {
	return currentState == InstanceStatusPendingScaleDown && o.clock.Since(lastChange) >= o.scaleDownPendingPeriod()
}
