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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/clock"
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
	Recorder record.EventRecorder
	Scheme   *runtime.Scheme

	limiter       limiters.ResourceLimiter
	globalLimiter limiters.ResourceLimiter

	usedResources *corev1.ResourceList

	predictor predictors.Predictor

	log logr.Logger
	mp  providers.MetricsProvider

	// ephemeral caching of the previous states to reduce logging
	// map of deployment name to the state
	states map[string]*InstanceState
	// XXX: should it be a part of mp?
	metricsUpdated bool

	// partition assignments, indicates relation of instanceId to partitions
	// these arrays are operate on deployments, not partitions
	assignments [][]int32

	toRemoveInstances   []*appsv1.Deployment
	toUpdateInstances   []*appsv1.Deployment
	toEstimateInstances []*appsv1.Deployment
	toCreateInstances   []*appsv1.Deployment

	clock clock.Clock
}

func (o *operator) init(consumer *konsumeratorv1.Consumer, managedInstances appsv1.DeploymentList) error {
	hash, err := hashstructure.Hash(consumer.Spec.DeploymentTemplate, nil)
	if err != nil {
		return err
	}
	consumer.Status.ObservedGeneration = helpers.Ptr(int64(hash))
	rl := make(corev1.ResourceList, 0)
	o.consumer = consumer
	o.usedResources = &rl
	o.mp = o.newMetricsProvider()
	var groupSize int32 = 1
	if o.consumer.Spec.NumPartitionsPerInstance != nil && *o.consumer.Spec.NumPartitionsPerInstance > 0 {
		groupSize = *o.consumer.Spec.NumPartitionsPerInstance
	}
	o.assignments = helpers.SplitIntoBuckets(*o.consumer.Spec.NumPartitions, groupSize)
	o.states = make(map[string]*InstanceState, len(o.assignments))

	o.toRemoveInstances = make([]*appsv1.Deployment, 0)
	o.toUpdateInstances = make([]*appsv1.Deployment, 0)
	o.toEstimateInstances = make([]*appsv1.Deployment, 0)
	o.toCreateInstances = make([]*appsv1.Deployment, 0)

	o.limiter = limiters.NewInstanceLimiter(consumer.Spec.ResourcePolicy, o.log)
	o.globalLimiter = limiters.NewGlobalLimiter(consumer.Spec.ResourcePolicy, o.usedResources, o.log)

	// expose the global limiter pool size. MaxAllowed returns a total before anything was assigned
	policy := consumer.Spec.ResourcePolicy
	if policy != nil && policy.GlobalPolicy != nil {
		consumerGlobalCPUPoolSize.WithLabelValues(consumer.Name).Set(float64(o.globalLimiter.MaxAllowed("").Cpu().MilliValue()))
		consumerGlobalMemoryPoolSize.WithLabelValues(consumer.Name).Set(o.globalLimiter.MaxAllowed("").Memory().AsApproximateFloat64())
	}

	if o.consumer.Spec.Autoscaler == nil || o.consumer.Spec.Autoscaler.Prometheus == nil {
		return fmt.Errorf("Spec.Autoscaler.Prometheus can't be empty")
	}
	o.predictor = predictors.NewNaivePredictor(o.mp, o.consumer.Spec.Autoscaler.Prometheus)

	o.syncInstanceStates(managedInstances)

	consumerGlobalCPUPoolAllocated.WithLabelValues(consumer.Name).Set(float64(o.usedResources.Cpu().MilliValue()))
	consumerGlobalMemoryPoolAllocated.WithLabelValues(consumer.Name).Set(o.usedResources.Memory().AsApproximateFloat64())
	return nil
}

func (o *operator) syncInstanceStates(managedDeploys appsv1.DeploymentList) {
	var missing, paused, running, lagging int32
	expectedInstances := int32(len(o.assignments))
	trackedInstances := make(map[int32]bool)

	for i := range managedDeploys.Items {
		deploy := &managedDeploys.Items[i]
		instanceId, err := helpers.ParseIntAnnotation(deploy.Annotations[ConsumerAnnotation])
		if err != nil {
			o.log.Error(err, "failed to parse annotation with instanceId. Old deploy?")
			o.toRemoveInstances = append(o.toRemoveInstances, deploy)
			continue
		}
		parsedPartitions, err := helpers.ParsePartitionsListAnnotation(deploy.Annotations[PartitionAnnotation])
		if err != nil {
			o.log.Error(err, "failed to parse annotation with partition number.")
			o.toRemoveInstances = append(o.toRemoveInstances, deploy)
			continue
		}
		forceMetricsUpdate(deploy, o.consumer.Name)
		trackedInstances[instanceId] = true
		if instanceId > expectedInstances-1 {
			o.log.Info("deployment with instanceId out of range", "instanceId", instanceId)
			o.toRemoveInstances = append(o.toRemoveInstances, deploy)
			continue
		}
		partitions := o.assignments[instanceId]
		if !cmp.Equal(parsedPartitions, partitions) {
			o.log.Info(
				"partitions from annotation differ from those that should be assigned",
				"expected", partitions,
				"annotation", parsedPartitions,
			)
			o.toRemoveInstances = append(o.toRemoveInstances, deploy)
			continue
		}

		// TODO: expose metric when deployment doesn't have an active pod for some time?
		// if deploy.Status.Replicas == deploy.Status.UnavailableReplicas {
		// 	o.log.Info("deployment is unable to create a replica")
		// }
		state := &InstanceState{
			instanceId:   instanceId,
			partitions:   partitions,
			scalingState: deploy.Annotations[ScalingStatusAnnotation],
		}
		state.lastStateChange, _ = helpers.ParseTimeAnnotation(deploy.Annotations[ScalingStatusChangeAnnotation])
		if _, ok := deploy.Annotations[VerboseLoggingAnnotation]; ok {
			state.verbose = true
		}

		if deployIsPaused(deploy) {
			paused++
		} else {
			running++
		}

		state.maxLag = o.getMaxLag(partitions)
		if o.isLagging(state.maxLag) {
			state.isLagging = true
			lagging++
		}
		o.states[deploy.Name] = state

		// count used resources by each container in deployment
		claimedResources := sumAllRequestedResourcesInPod(deploy.Spec.Template.Spec.Containers)
		o.usedResources = resourceListSum(o.usedResources, claimedResources)
		state.currentResources = claimedResources

		if deploy.Annotations[GenerationAnnotation] != o.observedGeneration() {
			o.toUpdateInstances = append(o.toUpdateInstances, deploy)
			continue
		}
		if o.metricsUpdated {
			o.toEstimateInstances = append(o.toEstimateInstances, deploy)
		}

	}
	for i := int32(0); i < expectedInstances; i++ {
		if _, ok := trackedInstances[i]; !ok {
			missing++
			o.toCreateInstances = append(o.toCreateInstances, o.updateDeploy(o.constructDeploy(i)))
		}
	}

	status := &o.consumer.Status
	status.Running = helpers.Ptr(running)
	status.Paused = helpers.Ptr(paused)
	status.Lagging = helpers.Ptr(lagging)
	status.Outdated = helpers.Ptr(int32(len(o.toUpdateInstances)))
	status.Missing = helpers.Ptr(missing)
	status.Redundant = helpers.Ptr(int32(len(o.toRemoveInstances)))
	status.Expected = helpers.Ptr(int32(len(o.assignments)))

	name := o.consumer.Name
	consumerStatus.WithLabelValues(name, "running").Set(float64(*status.Running))
	consumerStatus.WithLabelValues(name, "paused").Set(float64(*status.Paused))
	consumerStatus.WithLabelValues(name, "lagging").Set(float64(*status.Lagging))
	consumerStatus.WithLabelValues(name, "outdated").Set(float64(*status.Outdated))
	consumerStatus.WithLabelValues(name, "expected").Set(float64(*status.Expected))
	consumerStatus.WithLabelValues(name, "missing").Set(float64(*status.Missing))
	consumerStatus.WithLabelValues(name, "redundant").Set(float64(*status.Redundant))
}

func (o *operator) reconcile(cl client.Client, req ctrl.Request) error {
	ctx := context.Background()
	for _, deploy := range o.toCreateInstances {
		o.setOwner(deploy)
		if err := cl.Create(ctx, deploy); errors.IgnoreAlreadyExists(err) != nil {
			deploymentsCreateErrors.WithLabelValues(req.Name).Inc()
			o.log.Error(err, "unable to create new Deployment", "deployment", deploy.Name)
			continue
		}
		o.log.Info("created new deployment", "deployment", deploy.Name)
		deploymentsCreateTotal.WithLabelValues(req.Name).Inc()
	}

	for _, deploy := range o.toRemoveInstances {
		deploymentStatus.WithLabelValues(o.consumer.Name, deploy.Name).Set(0)
		if err := cl.Delete(ctx, deploy); errors.IgnoreNotFound(err) != nil {
			o.log.Error(err, "unable to delete deployment", "deployment", deploy.Name)
			deploymentsDeleteErrors.WithLabelValues(req.Name).Inc()
			continue
		}
		o.log.Info("deleting the deployment", "deployment", deploy.Name)
		deploymentsDeleteTotal.WithLabelValues(req.Name).Inc()
	}

	for _, origDeploy := range o.toUpdateInstances {
		if o.shouldLog(origDeploy.Name) {
			o.log.Info("deployment needs to updated", "deployment", origDeploy.Name)
		}
		deploy := o.updateDeploy(origDeploy.DeepCopy())
		o.setOwner(deploy)
		if err := cl.Update(ctx, deploy); errors.IgnoreConflict(err) != nil {
			deploymentsUpdateErrors.WithLabelValues(req.Name).Inc()
			o.log.Error(err, "unable to update deployment", "deployment", deploy.Name)
			continue
		}
		deploymentsUpdateTotal.WithLabelValues(req.Name).Inc()
	}

	for _, origDeploy := range o.toEstimateInstances {
		if o.shouldLog(origDeploy.Name) {
			o.log.Info("deployment needs to resource estimation", "deployment", origDeploy.Name)
		}
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

func (o *operator) observedGeneration() string {
	return strconv.Itoa(int(*o.consumer.Status.ObservedGeneration))
}

func (o *operator) isLagging(lag time.Duration) bool {
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

// forceMetricsUpdate updates deployment metrics to make sure that we do not have
// missing or stale metrics between app restarts.
func forceMetricsUpdate(deploy *appsv1.Deployment, consumerName string) {
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

func (o *operator) estimateDeploy(deploy *appsv1.Deployment) (*appsv1.Deployment, bool, error) {
	if o.clock.Since(o.consumer.Status.LastSyncTime.Time) >= minDuration(o.scaleUpPendingPeriod(), o.scaleDownPendingPeriod()) {
		o.log.Info(
			"WARNING: Long time since the last metrics update",
			"lastSyncTime", o.consumer.Status.LastSyncTime.Time,
		)
		return deploy, false, nil
	}
	state := o.states[deploy.Name]
	instanceId := state.instanceId
	isLagging := state.isLagging
	currentState := state.scalingState
	lastStateChange := state.lastStateChange

	needsUpdate := false
	for i := range deploy.Spec.Template.Spec.Containers {
		isChangedAnnotations := false
		container := &deploy.Spec.Template.Spec.Containers[i]
		estimates := o.estimateResources(container, state)
		resources, underProvision := o.applyResourcesLimiters(container, estimates, state, true)
		cmpRes := helpers.CmpResourceList(deploy.Spec.Template.Spec.Containers[i].Resources.Requests, resources.Requests)
		var logHeadline string
		switch cmpRes {
		case cmpResourcesEq:
			isChangedAnnotations = o.updateScaleAnnotations(deploy, underProvision)
			logHeadline = fmt.Sprintf("No action. Same amount of resources estimated")
		case cmpResourcesGt:
			if isLagging {
				switch currentState {
				case InstanceStatusRunning:
					isChangedAnnotations = o.updateScalingStatus(deploy, InstanceStatusPendingScaleUp)
					logHeadline = fmt.Sprintf("PENDING_SCALE_UP, as more resources estimated with lag present")
				case InstanceStatusPendingScaleUp:
					if o.scalingUpAllowed(lastStateChange, currentState) {
						o.updateScaleAnnotations(deploy, underProvision)
						container.Resources = *resources
						container.Env = helpers.PopulateEnv(
							container.Env,
							&container.Resources,
							o.consumer.Spec.PartitionEnvKey,
							state.partitions,
							int(instanceId),
							int(*o.consumer.Spec.NumPartitions),
							len(o.assignments),
						)
						needsUpdate = true
						logHeadline = fmt.Sprintf("SCALING UP as more resources estimated and scaling action is allowed")
					}
					logHeadline = fmt.Sprintf("No action. More resources estimated, but scaling action is not allowed for another %v", o.scaleUpPendingPeriod()-o.clock.Since(lastStateChange))
				case InstanceStatusSaturated:
					isChangedAnnotations = o.updateScaleAnnotations(deploy, underProvision)
					logHeadline = fmt.Sprintf("No action. More resources estimated, but instance is SATURATED")
				}
			} else {
				// XXX: changing annotation here doesn't make any sense.
				// current state is: pod is running, no lag detected, but estimations thinks that it needs more resources
				// this leads to changing state from RUNNING to SATURATED ?!
				// isChangedAnnotations = o.updateScaleAnnotations(deploy, underProvision)
				logHeadline = fmt.Sprintf("No action. More resources estimated, but no lag detected")
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
						state.partitions,
						int(instanceId),
						int(*o.consumer.Spec.NumPartitions),
						len(o.assignments),
					)
					needsUpdate = true
					logHeadline = fmt.Sprintf("SCALING DOWN as less resources estimated and scaling action is allowed")
				} else {
					isChangedAnnotations = o.updateScalingStatus(deploy, InstanceStatusPendingScaleDown)
					logHeadline = fmt.Sprintf("PENDING_SCALE_DOWN. Less resources estimated, but scaling action is not allowed for another %v", o.scaleDownPendingPeriod()-o.clock.Since(lastStateChange))
				}
			} else {
				isChangedAnnotations = o.updateScaleAnnotations(deploy, underProvision)
				logHeadline = fmt.Sprintf("No action. Less resources estimated, but lag is still present")
			}
		}
		if isChangedAnnotations {
			needsUpdate = true
		}
		o.log.Info(
			fmt.Sprintf("%s. cpu[current=%v, ideal=%v, ilimited=%v, glimited=%v], memory[current=%v, ideal=%v, ilimited=%v, glimited=%v]", logHeadline,
				state.currentResources.Cpu(), state.estimatedResources.Cpu(), state.iLimitResources.Cpu(), state.gLimitResources.Cpu(),
				state.currentResources.Memory().ScaledValue(resource.Mega), state.estimatedResources.Memory().ScaledValue(resource.Mega), state.iLimitResources.Memory().ScaledValue(resource.Mega), state.gLimitResources.Memory().ScaledValue(resource.Mega),
			),
			"instanceId", instanceId,
			"partitions", state.partitions,
			"container", container.Name,
			"oldStatus", currentState,
			"newStatus", deploy.Annotations[ScalingStatusAnnotation],
			"isChangedAnnotations", isChangedAnnotations,
			"maxLag", o.getMaxLag(state.partitions),
			"isLagging", isLagging,
			"isCritLag", state.isLagCritical,
			"cpu.shortage", underProvision.Cpu(),
			"ram.shortage", underProvision.Memory(),
		)
	}
	return deploy, needsUpdate, nil
}

func (o *operator) updateDeploy(deploy *appsv1.Deployment) *appsv1.Deployment {
	state := o.states[deploy.Name]
	instanceId := state.instanceId
	partitions := o.assignments[instanceId]
	deploy.Annotations[GenerationAnnotation] = o.observedGeneration()
	deploy.Spec = *o.consumer.Spec.DeploymentTemplate.DeepCopy()
	var shortage corev1.ResourceList
	for i := range deploy.Spec.Template.Spec.Containers {
		var resources *corev1.ResourceRequirements
		container := &deploy.Spec.Template.Spec.Containers[i]
		// GlobalLimit should be ignored when new deployment is being created.
		// otherwise we won't be able to set limits and/or create it.
		respectGlobalLimit := !container.Resources.Requests.Cpu().IsZero()
		estimates := o.estimateResources(container, state)
		resources, resShortage := o.applyResourcesLimiters(container, estimates, state, respectGlobalLimit)
		shortage = *resourceListSum(&shortage, resShortage)
		container.Resources = *resources
		container.Env = helpers.PopulateEnv(
			container.Env,
			&container.Resources,
			o.consumer.Spec.PartitionEnvKey,
			partitions,
			int(instanceId),
			int(*o.consumer.Spec.NumPartitions),
			len(o.assignments),
		)
	}
	o.updateScaleAnnotations(deploy, &shortage)
	return deploy
}

func (o *operator) constructDeploy(instanceId int32) *appsv1.Deployment {
	partitionIds := o.assignments[instanceId]
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
			Name:        fmt.Sprintf("%s-%d", o.consumer.Spec.Name, instanceId),
			Namespace:   o.consumer.Spec.Namespace,
		},
		Spec: o.consumer.Spec.DeploymentTemplate,
	}
	deploy.Annotations[PartitionAnnotation] = strings.Join(helpers.Int2Str(partitionIds), ",")
	deploy.Annotations[ConsumerAnnotation] = strconv.Itoa(int(instanceId))
	deploy.Annotations[GenerationAnnotation] = o.observedGeneration()
	o.updateScalingStatus(deploy, InstanceStatusRunning)

	o.states[deploy.Name] = &InstanceState{
		instanceId:       instanceId,
		partitions:       o.assignments[instanceId],
		currentResources: &corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("0"), corev1.ResourceMemory: resource.MustParse("0")},
		scalingState:     InstanceStatusRunning,
		verbose:          false,
	}

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

func (o *operator) isCriticalLagReached(maxLag time.Duration) bool {
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

func (o *operator) estimateResources(container *corev1.Container, state *InstanceState) *corev1.ResourceRequirements {
	estimatedResources := o.predictor.Estimate(container.Name, state.partitions)
	state.estimatedResources = &estimatedResources.Requests
	return estimatedResources
}

// applyResourcesLimiters applies both limiters (local and global)
// returns allowed resource requirements and resource shortage if any
func (o *operator) applyResourcesLimiters(container *corev1.Container, estimates *corev1.ResourceRequirements, state *InstanceState, applyGlobalLimiter bool) (*corev1.ResourceRequirements, *corev1.ResourceList) {
	var iLimitResources *corev1.ResourceRequirements
	// Respect criticalLag value. If lag reached criticalLag, allocate maximum allowed resources
	if o.isCriticalLagReached(o.getMaxLag(state.partitions)) {
		state.isLagCritical = true
		maxAllowed := o.limiter.MaxAllowed(container.Name)
		iLimitResources = &corev1.ResourceRequirements{Limits: *maxAllowed, Requests: *maxAllowed}
	} else {
		iLimitResources = o.limiter.ApplyLimits(container.Name, estimates)
	}
	state.iLimitResources = &iLimitResources.Requests
	resDiff := resourceListDiff(estimates.Requests, iLimitResources.Requests)
	if !applyGlobalLimiter {
		return iLimitResources, &resDiff
	}

	currentResources := container.Resources.DeepCopy()
	resourcesToRequest := resourceRequirementsDiff(iLimitResources, currentResources)
	allocatableResources := o.globalLimiter.ApplyLimits("", resourcesToRequest)
	state.gLimitResources = &allocatableResources.Requests
	if !resourcesToRequest.Requests.Cpu().IsZero() && allocatableResources.Requests.Cpu().IsZero() {
		o.log.Info("CPU global limit is reached", " instanceId", state.instanceId)
	}
	if !resourcesToRequest.Requests.Memory().IsZero() && allocatableResources.Requests.Memory().IsZero() {
		o.log.Info("Memory global limit is reached", "instanceId", state.instanceId)
	}
	// sum-up current and requested resources
	resourcesAfterLimitApplied := resourceRequirementsSum(currentResources, allocatableResources)
	globalShortage := resourceListDiff(resourcesToRequest.Requests, allocatableResources.Requests)

	return resourcesAfterLimitApplied, resourceListSum(&resDiff, &globalShortage)
}

func (o *operator) updateScaleAnnotations(d *appsv1.Deployment, resourceShortage *corev1.ResourceList) bool {
	if resourceShortage.Cpu().MilliValue() > 0 {
		underProvision := resourceShortage.Cpu().MilliValue()
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

func (o *operator) shouldLog(deploymentName string) bool {
	if s, ok := o.states[deploymentName]; ok {
		return s.verbose
	}
	if _, ok := o.consumer.Annotations[VerboseLoggingAnnotation]; ok {
		return true
	}
	return false
}
