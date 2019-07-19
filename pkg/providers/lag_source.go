package providers

import (
	"time"

	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
)

type LagSource interface {
	GetProductionRate(int32) int64
	GetConsumptionRate(int32) int64
	GetMessagesBehind(int32) int64
	GetLagByPartition(int32) time.Duration
	Update() error
}

func DumpSyncState(n int32, store LagSource) map[int32]konsumeratorv1alpha1.InstanceState {
	state := make(map[int32]konsumeratorv1alpha1.InstanceState, n)
	for i := int32(0); i < n; i++ {
		state[i] = konsumeratorv1alpha1.InstanceState{
			ProductionRate:  store.GetProductionRate(i),
			ConsumptionRate: store.GetConsumptionRate(i),
			MessagesBehind:  store.GetMessagesBehind(i),
		}
	}
	return state
}
