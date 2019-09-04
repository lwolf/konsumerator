package predictors

import (
	"math"

	"github.com/go-logr/logr"
	autoscalev1 "github.com/kubernetes/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
	"github.com/lwolf/konsumerator/pkg/providers"
)

type NaivePredictor struct {
	lagSource providers.MetricsProvider
	promSpec  *konsumeratorv1alpha1.PrometheusAutoscalerSpec
	log       logr.Logger
}

func NewNaivePredictor(log logr.Logger, store providers.MetricsProvider, promSpec *konsumeratorv1alpha1.PrometheusAutoscalerSpec) *NaivePredictor {
	ctrlLogger := log.WithName("naivePredictor")
	return &NaivePredictor{
		lagSource: store,
		promSpec:  promSpec,
		log:       ctrlLogger,
	}
}

func (s *NaivePredictor) expectedConsumption(partition int32) int64 {
	production := s.lagSource.GetProductionRate(partition)
	lagTime := s.lagSource.GetLagByPartition(partition)
	if lagTime > 0 {
		work := int64(lagTime.Seconds())*production + production*int64(s.promSpec.RecoveryTime.Seconds())
		return work / int64(s.promSpec.RecoveryTime.Seconds())
	}
	return production
}
func (s *NaivePredictor) estimateCpu(consumption int64, ratePerCore int64) (int64, int64) {
	// round cpuRequests to the nearest 100 Millicore, and cpuLimit to the nearest Core
	cpuReq := math.Ceil(float64(consumption)/float64(ratePerCore)*10) / 10
	return int64(cpuReq * 1000), int64(math.Ceil(cpuReq)) * 1000
}
func (s *NaivePredictor) estimateMemory(consumption int64, ramPerCore int64, cpuR int64, cpuL int64) (int64, int64) {
	requests := cpuR * (ramPerCore / 1000)
	limit := cpuL * (ramPerCore / 1000)
	return requests, limit
}

func (s *NaivePredictor) validateCpu(request int64, limit int64, policy *autoscalev1.ContainerResourcePolicy) (int64, int64) {
	r := request
	l := limit
	if request < policy.MinAllowed.Cpu().MilliValue() {
		r = policy.MinAllowed.Cpu().MilliValue()
	}
	if limit < policy.MinAllowed.Cpu().MilliValue() {
		l = policy.MinAllowed.Cpu().MilliValue()
	}
	if limit > policy.MaxAllowed.Cpu().MilliValue() {
		l = policy.MaxAllowed.Cpu().MilliValue()
	}
	if request > l {
		r = l
	}
	s.log.V(1).Info("CPU validation result", "outRequest", r, "outLimit", l)
	return r, l
}

func (s *NaivePredictor) validateMemory(request int64, limit int64, policy *autoscalev1.ContainerResourcePolicy) (int64, int64) {
	r := request
	l := limit
	if request < policy.MinAllowed.Memory().MilliValue() {
		r = policy.MinAllowed.Memory().MilliValue()
	}
	if limit < policy.MinAllowed.Memory().MilliValue() {
		l = policy.MinAllowed.Memory().MilliValue()
	}
	if limit > policy.MaxAllowed.Memory().MilliValue() {
		l = policy.MaxAllowed.Memory().MilliValue()
	}
	if request > l {
		r = l
	}
	s.log.V(1).Info("Memory validation result", "outRequest", r, "outLimit", l)
	return r, l
}

func (s *NaivePredictor) Estimate(containerName string, limits *autoscalev1.ContainerResourcePolicy, partition int32) *corev1.ResourceRequirements {
	log := s.log.WithValues("ContainerName", containerName, "partition", partition)
	log.V(1).Info("Estimating resources")
	expectedConsumption := s.expectedConsumption(partition)
	log.V(1).Info("expected consumption", "value", expectedConsumption)
	cpuReq, cpuLimit := s.estimateCpu(expectedConsumption, *s.promSpec.RatePerCore)
	log.V(1).Info("estimated CPU", "req", cpuReq, "limit", cpuLimit)
	memoryReq, memoryLimit := s.estimateMemory(expectedConsumption, s.promSpec.RamPerCore.MilliValue(), cpuReq, cpuLimit)
	log.V(1).Info("estimated Memory", "req", memoryReq, "limit", memoryLimit)

	if limits != nil {
		cpuReq, cpuLimit = s.validateCpu(cpuReq, cpuLimit, limits)
		memoryReq, memoryLimit = s.validateMemory(memoryReq, memoryLimit, limits)
	}

	return &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    *resource.NewMilliQuantity(cpuReq, resource.DecimalSI),
			corev1.ResourceMemory: *resource.NewMilliQuantity(memoryReq, resource.DecimalSI),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    *resource.NewMilliQuantity(cpuLimit, resource.DecimalSI),
			corev1.ResourceMemory: *resource.NewMilliQuantity(memoryLimit, resource.DecimalSI),
		},
	}
}

func GetResourcePolicy(name string, spec *konsumeratorv1alpha1.ConsumerSpec) *autoscalev1.ContainerResourcePolicy {
	for _, cp := range spec.ResourcePolicy.ContainerPolicies {
		if cp.ContainerName == name {
			return &cp
		}
	}
	return nil
}
