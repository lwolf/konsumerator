package predictors

import (
	autoscalev1 "github.com/kubernetes/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	corev1 "k8s.io/api/core/v1"
)

type Predictor interface {
	// TODO: do not expose autoscalerv1 type. Replace with some internal type
	Estimate(containerName string, limits *autoscalev1.ContainerResourcePolicy, partition int32) *corev1.ResourceRequirements
}
