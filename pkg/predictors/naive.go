package predictors

import (
	"math"

	"github.com/go-logr/logr"
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
	// TODO: we need to do a basic validation of all the fields during creating of the Predictor
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

func (s *NaivePredictor) Estimate(containerName string, partitions []int32) *corev1.ResourceRequirements {
	var expectedConsumption int64
	for _, p := range partitions {
		expectedConsumption += s.expectedConsumption(p)
	}
	cpuReq, cpuLimit := s.estimateCpu(expectedConsumption, *s.promSpec.RatePerCore)
	memoryReq, memoryLimit := s.estimateMemory(expectedConsumption, s.promSpec.RamPerCore.MilliValue(), cpuReq, cpuLimit)
	s.log.V(1).Info(
		"resource estimation results",
		"containerName", containerName,
		"partitions", partitions,
		"expected consumption", expectedConsumption,
		"cpuReq", cpuReq,
		"cpuLimit", cpuLimit,
		"memReq", memoryReq,
		"memLimit", memoryLimit,
	)

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
