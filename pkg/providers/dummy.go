package providers

import (
	"time"
)

type DummyLagSource struct {
	lag        MetricsMap
	partitions int32
}

func (l *DummyLagSource) GetLagByPartition(partition int32) time.Duration {
	if int(partition) > len(l.lag) {
		return 0
	}
	return time.Duration(l.lag[partition]) * time.Second
}

func (l *DummyLagSource) Update() error {
	for i := 0; i < int(l.partitions); i++ {
		l.lag[int32(i)] = int64(0)
	}
	return nil
}

func (l *DummyLagSource) GetProductionRate(partition int32) int64 {
	return 0
}
func (l *DummyLagSource) GetConsumptionRate(partition int32) int64 {
	return 0
}
func (l *DummyLagSource) GetMessagesBehind(partition int32) int64 {
	return 0
}

func NewLagSourceDummy(numPartitions int32) *DummyLagSource {
	return &DummyLagSource{partitions: numPartitions}
}
