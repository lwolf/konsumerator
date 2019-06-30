package helpers

import "strconv"

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
