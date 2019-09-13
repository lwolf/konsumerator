package predictors

import (
	corev1 "k8s.io/api/core/v1"
)

type Predictor interface {
	Estimate(containerName string, partition int32) *corev1.ResourceRequirements
}
