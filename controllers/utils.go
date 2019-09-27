package controllers

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/lwolf/konsumerator/pkg/helpers"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/json"

	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
)

// TODO: could be replaced with resourceRequirementsDiff
// turned to Neg()
func resourceRequirementsSum(a, b *corev1.ResourceRequirements) *corev1.ResourceRequirements {
	cpu := a.Requests.Cpu()
	mem := a.Requests.Memory()
	cpu.Add(*b.Requests.Cpu())
	mem.Add(*b.Requests.Memory())

	return &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    *cpu,
			corev1.ResourceMemory: *mem,
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    roundQuantity(cpu),
			corev1.ResourceMemory: *mem,
		},
	}
}

func resourceRequirementsDiff(a, b *corev1.ResourceRequirements) *corev1.ResourceRequirements {
	requests := resourceListDiff(a.Requests, b.Requests)
	limits := resourceListDiff(a.Limits, b.Limits)

	// round up limit CPU
	limits[corev1.ResourceCPU] = roundQuantity(limits.Cpu())

	return &corev1.ResourceRequirements{
		Requests: requests,
		Limits:   limits,
	}
}

func roundQuantity(q *resource.Quantity) resource.Quantity {
	rounded := int64(math.Ceil(float64(q.MilliValue()) / 1e3))
	return *resource.NewQuantity(rounded, resource.DecimalSI)
}

func resourceListDiff(a, b corev1.ResourceList) corev1.ResourceList {
	cpu := a.Cpu()
	mem := a.Memory()
	cpu.Sub(*b.Cpu())
	mem.Sub(*b.Memory())
	return corev1.ResourceList{
		corev1.ResourceCPU:    *cpu,
		corev1.ResourceMemory: *mem,
	}
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

func shouldUpdateMetrics(consumer *konsumeratorv1alpha1.Consumer, now time.Time) (bool, error) {
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
	timeToSync := now.Sub(status.LastSyncTime.Time) > consumer.Spec.Autoscaler.Prometheus.MinSyncPeriod.Duration
	if timeToSync {
		return true, nil
	}
	return false, nil
}

func deployIsPaused(d *appsv1.Deployment) bool {
	_, pausedAnnotation := d.Annotations[DisableAutoscalerAnnotation]
	return d.Status.Replicas == 0 || pausedAnnotation
}

func PopulateStatusFromAnnotation(a map[string]string, status *konsumeratorv1alpha1.ConsumerStatus) {
	am := annotationMngr{a}
	status.Expected = am.GetInt32(annotationStatusExpected)
	status.Running = am.GetInt32(annotationStatusRunning)
	status.Paused = am.GetInt32(annotationStatusPaused)
	status.Lagging = am.GetInt32(annotationStatusLagging)
	status.Missing = am.GetInt32(annotationStatusMissing)
	status.Outdated = am.GetInt32(annotationStatusOutdated)
	status.LastSyncTime = am.GetTime(annotationStatusLastSyncTime)
	status.LastSyncState = am.GetMap(annotationStatusLastState)
}

func UpdateStatusAnnotations(cm *corev1.ConfigMap, status *konsumeratorv1alpha1.ConsumerStatus) error {
	cm.Annotations[annotationStatusExpected] = fmt.Sprintf("%d", *status.Expected)
	cm.Annotations[annotationStatusRunning] = fmt.Sprintf("%d", *status.Running)
	cm.Annotations[annotationStatusPaused] = fmt.Sprintf("%d", *status.Paused)
	cm.Annotations[annotationStatusLagging] = fmt.Sprintf("%d", *status.Lagging)
	cm.Annotations[annotationStatusMissing] = fmt.Sprintf("%d", *status.Missing)
	cm.Annotations[annotationStatusOutdated] = fmt.Sprintf("%d", *status.Outdated)
	cm.Annotations[annotationStatusLastSyncTime] = status.LastSyncTime.Format(helpers.TimeLayout)
	state, err := json.Marshal(status.LastSyncState)
	if err != nil {
		return err
	}
	cm.Annotations[annotationStatusLastState] = string(state)
	return nil
}

type annotationMngr struct {
	a map[string]string
}

func (am annotationMngr) GetMap(k string) map[string]konsumeratorv1alpha1.InstanceState {
	v, ok := am.a[k]
	if !ok {
		return nil
	}
	d := make(map[string]konsumeratorv1alpha1.InstanceState)
	if err := json.Unmarshal([]byte(v), &d); err != nil {
		return nil
	}
	return d

}
func (am annotationMngr) GetTime(k string) *metav1.Time {
	v, ok := am.a[k]
	if !ok {
		return nil
	}
	t, err := time.Parse(helpers.TimeLayout, v)
	if err != nil {
		return nil
	}
	return &metav1.Time{Time: t}
}
func (am annotationMngr) GetInt32(k string) *int32 {
	v, ok := am.a[k]
	if !ok {
		return helpers.Ptr2Int32(0)
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return helpers.Ptr2Int32(0)
	}
	return helpers.Ptr2Int32(int32(i))
}
