package providers

import (
	"time"
)

type DummyLagSource struct {
	lag        map[int32]float64
	partitions int32
}

func (l *DummyLagSource) GetLagByPartition(partition int32) time.Duration {
	if int(partition) > len(l.lag) {
		return 0
	}
	return time.Duration(l.lag[partition]) * time.Second
}

func (l *DummyLagSource) QueryConsumptionRate() (map[int32]float64, error) {
	return nil, nil
}

func (l *DummyLagSource) QueryProductionRate() (map[int32]float64, error) {
	return nil, nil
}

func (l *DummyLagSource) QueryOffset() (map[int32]float64, error) {
	return nil, nil
}
func (l *DummyLagSource) EstimateLag() error {
	return nil
}
func (l *DummyLagSource) GetLag() map[int32]float64 {
	return nil
}

func (l *DummyLagSource) QueryProductionRateDistribution() (map[int32]float64, error) {
	for i := 0; i < int(l.partitions); i++ {
		l.lag[int32(i)] = float64(0)
	}
	return l.lag, nil
}

func NewLagSourceDummy(numPartitions int32) *DummyLagSource {
	return &DummyLagSource{partitions: numPartitions}
}
