package providers

import (
	"strconv"
	"time"

	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
)

type MetricsProvider interface {
	GetProductionRate(int32) int64
	GetConsumptionRate(int32) int64
	GetMessagesBehind(int32) int64
	GetLagByPartition(int32) time.Duration
	Update() error
}

func DumpSyncState(n int32, prov MetricsProvider) map[string]konsumeratorv1alpha1.InstanceState {
	state := make(map[string]konsumeratorv1alpha1.InstanceState, n)
	for i := int32(0); i < n; i++ {
		state[strconv.Itoa(int(i))] = konsumeratorv1alpha1.InstanceState{
			ProductionRate:  prov.GetProductionRate(i),
			ConsumptionRate: prov.GetConsumptionRate(i),
			MessagesBehind:  prov.GetMessagesBehind(i),
		}
	}
	return state
}
