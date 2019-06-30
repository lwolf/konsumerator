package providers

import (
	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
)

type LagSourcePrometheus struct {
	currentLag []int64
}

func NewLagSourcePrometheus(spec *konsumeratorv1alpha1.LagProviderPrometheus) *LagSourcePrometheus {
	return &LagSourcePrometheus{}
}

func (l *LagSourcePrometheus) GetLagByPartition(partition int32) int64 {
	// needs to be processed differently depending on resultType (vector, matrix,scalar)
	return 0
}

func (l *LagSourcePrometheus) Query() error {
	return nil
}

func (l *LagSourcePrometheus) GetLag() []int64 {
	return l.currentLag
}
