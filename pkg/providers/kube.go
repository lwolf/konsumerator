package providers

// KubeLagSource makes data stored in the Consumer status
// compatible with base LagSource data provider interface
type KubeLagSource struct {
	currentLag []int64
}

func (l *KubeLagSource) GetLagByPartition(partition int32) int64 {
	if int(partition) > len(l.currentLag) {
		return 0
	}
	return l.currentLag[partition]
}
func (l *KubeLagSource) Query() error {
	return nil
}
func (l *KubeLagSource) GetLag() []int64 {
	return l.currentLag
}
func NewLagSourceKube(l []int64) *KubeLagSource {
	return &KubeLagSource{
		currentLag: l,
	}
}
