package providers

import (
	"time"
)

type DummyMP struct {
	lag        map[int32]int64
	partitions int32
}

func (l *DummyMP) GetLagByPartition(partition int32) time.Duration {
	if int(partition) > len(l.lag) {
		return 0
	}
	return time.Duration(l.lag[partition]) * time.Second
}

func (l *DummyMP) Update() error {
	for i := 0; i < int(l.partitions); i++ {
		l.lag[int32(i)] = int64(0)
	}
	return nil
}
func (l *DummyMP) Load(production map[int32]int64, consumption map[int32]int64, offset map[int32]int64) {
	return
}

func (l *DummyMP) GetProductionRate(partition int32) int64 {
	return 0
}
func (l *DummyMP) GetConsumptionRate(partition int32) int64 {
	return 0
}
func (l *DummyMP) GetMessagesBehind(partition int32) int64 {
	return 0
}

func NewDummyMP(numPartitions int32) *DummyMP {
	return &DummyMP{partitions: numPartitions}
}
