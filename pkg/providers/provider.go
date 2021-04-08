package providers

import (
	"strconv"
	"time"

	konsumeratorv1 "github.com/lwolf/konsumerator/api/v1"
)

type MetricsProvider interface {
	GetProductionRate(int32) int64
	GetConsumptionRate(int32) int64
	GetMessagesBehind(int32) int64
	GetLagByPartition(int32) time.Duration
	Update() error
	Load(map[int32]int64, map[int32]int64, map[int32]int64)
}

func DumpSyncState(n int32, prov MetricsProvider) map[string]konsumeratorv1.InstanceState {
	state := make(map[string]konsumeratorv1.InstanceState, n)
	for i := int32(0); i < n; i++ {
		state[strconv.Itoa(int(i))] = konsumeratorv1.InstanceState{
			ProductionRate:  prov.GetProductionRate(i),
			ConsumptionRate: prov.GetConsumptionRate(i),
			MessagesBehind:  prov.GetMessagesBehind(i),
		}
	}
	return state
}

func LoadSyncState(mp MetricsProvider, status konsumeratorv1.ConsumerStatus) {
	if status.LastSyncState == nil {
		return
	}
	productionMap := make(map[int32]int64)
	consumptionMap := make(map[int32]int64)
	offsetsMap := make(map[int32]int64)
	for iStr, state := range status.LastSyncState {
		p, err := strconv.Atoi(iStr)
		if err != nil {
			continue
		}
		partition := int32(p)
		productionMap[partition] = state.ProductionRate
		consumptionMap[partition] = state.ConsumptionRate
		offsetsMap[partition] = state.MessagesBehind
	}
	mp.Load(productionMap, consumptionMap, offsetsMap)
}
