package predictors

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	konsumeratorv1 "github.com/lwolf/konsumerator/api/v1"
	"github.com/lwolf/konsumerator/pkg/providers"
)

type NaivePredictor struct {
	lagSource providers.MetricsProvider
	promSpec  *konsumeratorv1.PrometheusAutoscalerSpec
}

func NewNaivePredictor(store providers.MetricsProvider, promSpec *konsumeratorv1.PrometheusAutoscalerSpec) *NaivePredictor {
	// TODO: we need to do a basic validation of all the fields during creating of the Predictor
	return &NaivePredictor{
		lagSource: store,
		promSpec:  promSpec,
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
func (s *NaivePredictor) estimateCpu(consumption int64, ratePerCore int64, scalingStep int64) (int64, int64) {
	requestCPU := ceilToMultipleOf(consumption*1000/ratePerCore, scalingStep)
	limitCPU := ceilToMultipleOf(requestCPU, 1000)
	return requestCPU, limitCPU
}
func (s *NaivePredictor) estimateMemory(ramPerCore int64, cpuL int64) (int64, int64) {
	limit := cpuL * (ramPerCore / 1000)
	return limit, limit
}

func (s *NaivePredictor) Estimate(_ string, partitions []int32) *corev1.ResourceRequirements {
	var expectedConsumption int64
	for _, p := range partitions {
		expectedConsumption += s.expectedConsumption(p)
	}
	// for backward compatibility, cpuIncrement is an optional field
	var step int64
	if s.promSpec.CpuIncrement == nil {
		step = 100
	} else {
		step = s.promSpec.CpuIncrement.MilliValue()
	}
	cpuReq, cpuLimit := s.estimateCpu(expectedConsumption, *s.promSpec.RatePerCore, step)
	memoryReq, memoryLimit := s.estimateMemory(s.promSpec.RamPerCore.MilliValue(), cpuLimit)

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

func ceilToMultipleOf(in int64, step int64) int64 {
	if in%step == 0 {
		return in
	}
	return in + (step - in%step)
}
