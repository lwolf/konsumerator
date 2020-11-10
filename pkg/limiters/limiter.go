package limiters

import (
	corev1 "k8s.io/api/core/v1"
)

type ResourceLimiter interface {
	MinAllowed(containerName string) *corev1.ResourceList
	MaxAllowed(containerName string) *corev1.ResourceList
	ApplyLimits(containerName string, resources *corev1.ResourceRequirements) *corev1.ResourceRequirements
}
