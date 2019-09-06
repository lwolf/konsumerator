package helpers

import (
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
)

func Ptr2Int32(i int32) *int32 {
	return &i
}

func Ptr2Int64(i int64) *int64 {
	return &i
}

func ParsePartitionAnnotation(partition string) *int32 {
	if len(partition) == 0 {
		return nil
	}
	p, err := strconv.ParseInt(partition, 10, 32)
	if err != nil {
		return nil
	}
	p32 := int32(p)
	return &p32
}

func DebugPrettyResources(r *corev1.ResourceRequirements) string {
	if r == nil {
		return ""
	}
	return fmt.Sprintf(
		"Req: cpu:%s, ram:%s; Limit: cpu:%s, ram:%s",
		r.Requests.Cpu().String(),
		r.Requests.Memory().String(),
		r.Limits.Cpu().String(),
		r.Limits.Memory().String(),
	)
}
