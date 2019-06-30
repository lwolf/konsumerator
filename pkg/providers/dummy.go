package providers

import "math/rand"

type DummyLagSource struct {
	currentLag []int64
	random     bool
	partitions int32
}

func (l *DummyLagSource) GetLagByPartition(partition int32) int64 {
	if int(partition) > len(l.currentLag) {
		return 0
	}
	return l.currentLag[partition]
}
func (l *DummyLagSource) Query() error {
	for i := 0; i < int(l.partitions); i++ {
		lag := int64(0)
		if l.random {
			lag = rand.Int63n(500)
		}
		l.currentLag = append(l.currentLag, lag)
	}
	return nil
}
func (l *DummyLagSource) GetLag() []int64 {
	return l.currentLag
}
func NewLagSourceDummy(random bool, numPartitions int32) *DummyLagSource {
	return &DummyLagSource{random: random, partitions: numPartitions}
}
