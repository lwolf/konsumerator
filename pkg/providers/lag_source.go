package providers

import "time"

type MetricsMap map[int32]int64

type LagSource interface {
	GetProductionRate(int32) int64
	GetConsumptionRate(int32) int64
	GetMessagesBehind(int32) int64
	GetLagByPartition(int32) time.Duration
	QueryConsumptionRate() (MetricsMap, error)
	QueryProductionRate() (MetricsMap, error)
	QueryOffset() (MetricsMap, error)
	EstimateLag() error
}
