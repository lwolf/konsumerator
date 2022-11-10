package predictors

import (
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	konsumeratorv1 "github.com/lwolf/konsumerator/api/v1"
	"github.com/lwolf/konsumerator/pkg/providers"
)

type NaivePredictor struct {
	lagSource     providers.MetricsProvider
	promSpec      *konsumeratorv1.PrometheusAutoscalerSpec
	fallbackValue map[string]*corev1.ResourceRequirements
	log           logr.Logger
}

func NewNaivePredictor(store providers.MetricsProvider, promSpec *konsumeratorv1.PrometheusAutoscalerSpec, fallback map[string]*corev1.ResourceRequirements, log logr.Logger) *NaivePredictor {
	// TODO: we need to do a basic validation of all the fields during creating of the Predictor
	ctrlLogger := log.WithName("naivePredictor")
	return &NaivePredictor{
		lagSource:     store,
		promSpec:      promSpec,
		fallbackValue: fallback,
		log:           ctrlLogger,
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

func (s *NaivePredictor) Estimate(containerName string, partitions []int32) *corev1.ResourceRequirements {
	var expectedConsumption int64
	for _, p := range partitions {
		expectedConsumption += s.expectedConsumption(p)
	}
	// metrics are missing, no need for estimation, return fallback value instead
	if expectedConsumption == 0 {
		if v, ok := s.fallbackValue[containerName]; ok {
			s.log.Info(
				"Metrics are missing for all partitions. Using fallback value.",
				"partitions", partitions,
				"req.cpu", v.Requests.Cpu(),
				"req.mem", v.Requests.Memory().ScaledValue(resource.Mega),
			)
			return v
		}
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
