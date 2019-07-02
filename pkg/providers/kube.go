package providers

import "time"

// KubeLagSource makes data stored in the Consumer status
// compatible with base LagSource data provider interface
type KubeLagSource struct {
	lag []float64
}

func (l *KubeLagSource) GetLagByPartition(partition int32) time.Duration {
	if int(partition) > len(l.lag) {
		return 0
	}
	return time.Duration(l.lag[partition]) * time.Second

}
func (l *KubeLagSource) QueryConsumptionRate() (map[int32]float64, error) {
	return nil, nil
}
func (l *KubeLagSource) QueryProductionRate() (map[int32]float64, error) {
	return nil, nil
}
func (l *KubeLagSource) QueryProductionRateDistribution() (map[int32]float64, error) {
	return nil, nil
}
func (l *KubeLagSource) QueryOffset() (map[int32]float64, error) {
	return nil, nil
}
func (l *KubeLagSource) EstimateLag() error {
	return nil
}

func NewLagSourceKube(l []float64) *KubeLagSource {
	return &KubeLagSource{
		lag: l,
	}
}
