package limiters

import (
	corev1 "k8s.io/api/core/v1"
)

type ResourceLimiter interface {
	ApplyLimits(containerName string, resources *corev1.ResourceRequirements) *corev1.ResourceRequirements
}
