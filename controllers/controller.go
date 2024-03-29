package controllers

import (
	"context"
	"time"

	konsumeratorv1 "github.com/lwolf/konsumerator/api/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	InstanceStatusRunning          string = "RUNNING"
	InstanceStatusSaturated        string = "SATURATED"
	InstanceStatusPendingScaleUp   string = "PENDING_SCALE_UP"
	InstanceStatusPendingScaleDown string = "PENDING_SCALE_DOWN"

	PartitionAnnotation           = "konsumerator.lwolf.org/partition"
	ConsumerAnnotation            = "konsumerator.lwolf.org/consumer-id"
	DisableAutoscalerAnnotation   = "konsumerator.lwolf.org/disable-autoscaler"
	GenerationAnnotation          = "konsumerator.lwolf.org/generation"
	CPUSaturationLevel            = "konsumerator.lwolf.org/cpu-saturation-level"
	ScalingStatusAnnotation       = "konsumerator.lwolf.org/scaling-status"
	ScalingStatusChangeAnnotation = "konsumerator.lwolf.org/scaling-status-change"
	VerboseLoggingAnnotation      = "konsumerator.lwolf.org/verbose"

	cmpResourcesLt int = -1
	cmpResourcesEq int = 0
	cmpResourcesGt int = 1

	defaultMinSyncPeriod               = time.Minute
	defaultScaleStatePendingUpPeriod   = time.Minute * 5
	defaultScaleStatePendingDownPeriod = time.Minute * 15
)

var apiGVStr = konsumeratorv1.GroupVersion.String()

type Controller interface {
	SetupWithManager(mgr ctrl.Manager) error
	Reconcile(context.Context, ctrl.Request) (ctrl.Result, error)
}
