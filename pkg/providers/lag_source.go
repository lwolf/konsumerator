package providers

type LagSource interface {
	// Query calls remote storage and populates internal map
	// with the per partition lag
	Query() error
	// GetLagByPartition returns lag time in seconds for provided partition number
	GetLagByPartition(int32) int64
	// GetLag returns entire list
	GetLag() []int64
}
