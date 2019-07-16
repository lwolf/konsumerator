package controllers

import (
	"github.com/lwolf/konsumerator/pkg/providers"
	"testing"
)

func TestEstimateResources(t *testing.T) {

	promSpec := spec.Autoscaler.Prometheus
	resourceLimits := GetResourcePolicy(containerName, spec)
	production := store.GetProductionRate(partition)
	consumption := store.GetConsumptionRate(partition)
	lagTime := store.GetLagByPartition(partition)
	work := int64(lagTime.Seconds())*production + production*int64(promSpec.PreferableCatchupPeriod.Seconds())
	expectedConsumption := work / int64(promSpec.PreferableCatchupPeriod.Seconds())
	cpuRequests := float64(expectedConsumption) / float64(*promSpec.RatePerCore)
}

/*
func EstimateResources(containerName string, spec *konsumeratorv1alpha1.ConsumerSpec, store providers.LagSource, partition int32) *v12.ResourceRequirements {
	promSpec := spec.Autoscaler.Prometheus
	resourceLimits := GetResourcePolicy(containerName, spec)
	production := store.GetProductionRate(partition)
	consumption := store.GetConsumptionRate(partition)
	lagTime := store.GetLagByPartition(partition)
	work := int64(lagTime.Seconds()) * production + production * int64(promSpec.PreferableCatchupPeriod.Seconds())
	expectedConsumption := work / int64(promSpec.PreferableCatchupPeriod.Seconds())
	cpuRequests := float64(expectedConsumption) / float64(*promSpec.RatePerCore)

	// spec.Autoscaler.Prometheus.RatePerCore
	return &v12.ResourceRequirements{

	}
}


*/
