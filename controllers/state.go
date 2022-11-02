package controllers

import (
	"time"

	corev1 "k8s.io/api/core/v1"
)

// InstanceState in a current form just caches the values in a single struct to reduce
// the duplication inside the operator.
type InstanceState struct {
	verbose    bool    // verbose logging
	instanceId int32   // instance ordinal
	partitions []int32 // array of assigned partitions

	maxLag        time.Duration
	isLagging     bool
	isLagCritical bool

	scalingState    string
	lastStateChange time.Time

	currentResources   *corev1.ResourceList
	estimatedResources *corev1.ResourceList
	iLimitResources    *corev1.ResourceList
	gLimitResources    *corev1.ResourceList
}
