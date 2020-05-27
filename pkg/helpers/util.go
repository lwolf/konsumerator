package helpers

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	defaultPartitionEnvKey = "KONSUMERATOR_PARTITION"
	gomaxprocsEnvKey       = "GOMAXPROCS"
	TimeLayout             = time.RFC3339
)

func Ptr2Int32(i int32) *int32 {
	return &i
}

func Ptr2Int64(i int64) *int64 {
	return &i
}

func ParseTimeAnnotation(ts string) (time.Time, error) {
	t, err := time.Parse(TimeLayout, ts)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

func ParseIntAnnotation(key string) (int32, error) {
	p, err := strconv.ParseInt(key, 10, 32)
	if err != nil {
		return int32(0), err
	}
	return int32(p), nil
}

func ParsePartitionsListAnnotation(partitions string) ([]int32, error) {
	parts := strings.Split(partitions, ",")
	if len(partitions) == 0 {
		return nil, fmt.Errorf("partitions not found in string %s, have to be a comma-separated list of ids", partitions)
	}
	var res []int32
	for _, p := range parts {
		partition, err := ParseIntAnnotation(p)
		if err != nil {
			return nil, fmt.Errorf("unable to parse one or more partitions as int %s", p)
		}
		res = append(res, partition)
	}
	return res, nil
}

func GomaxprocsFromResource(cpu *resource.Quantity) string {
	value := int(cpu.Value())
	if value < 1 {
		value = 1
	}
	return strconv.Itoa(value)
}

func SetEnv(env []corev1.EnvVar, key string, value string) []corev1.EnvVar {
	for i, e := range env {
		if e.Name == key {
			env[i].Value = value
			return env
		}
	}
	env = append(env, corev1.EnvVar{
		Name:  key,
		Value: value,
	})
	return env
}

func PopulateEnv(currentEnv []corev1.EnvVar, resources *corev1.ResourceRequirements, envKey string, partitions []int32) []corev1.EnvVar {
	var partitionKey string
	if envKey != "" {
		partitionKey = envKey
	} else {
		partitionKey = defaultPartitionEnvKey
	}
	env := make([]corev1.EnvVar, len(currentEnv))
	copy(env, currentEnv)
	env = SetEnv(env, partitionKey, strings.Join(Int2Str(partitions), ","))
	env = SetEnv(env, gomaxprocsEnvKey, GomaxprocsFromResource(resources.Limits.Cpu()))

	return env
}

func CmpResourceList(a corev1.ResourceList, b corev1.ResourceList) int {
	reqCpu := b.Cpu().Cmp(*a.Cpu())
	reqMem := b.Memory().Cmp(*a.Memory())
	switch {
	case reqCpu != 0:
		return reqCpu
	case reqMem != 0:
		return reqMem
	default:
		return 0
	}
}

func CmpResourceRequirements(a corev1.ResourceRequirements, b corev1.ResourceRequirements) int {
	reqCpu := b.Requests.Cpu().Cmp(*a.Requests.Cpu())
	limCpu := b.Limits.Cpu().Cmp(*a.Limits.Cpu())
	reqMem := b.Requests.Memory().Cmp(*a.Requests.Memory())
	limMem := b.Limits.Memory().Cmp(*a.Limits.Memory())
	switch {
	case reqCpu != 0:
		return reqCpu
	case limCpu != 0:
		return limCpu
	case reqMem != 0:
		return reqMem
	case limMem != 0:
		return limMem
	default:
		return 0
	}
}

// SplitIntoBuckets takes array size and group size and returns array of arrays
// e.g.
// 	SplitIntoBuckets(10, 3) -> [0,1,2],[3,4,5][6,7][8,9]
// 	SplitIntoBuckets(12, 3) -> [0,1,2],[3,4,5][6,7,8][9,10,11]
func SplitIntoBuckets(size int32, bucketSize int32) (buckets [][]int32) {
	if size == 0 || bucketSize == 0 {
		return
	}

	numBuckets := size / bucketSize
	if size%bucketSize > 0 {
		// Not every size is divisible by bucket size. So sometimes we
		// need an extra bucket.
		numBuckets++
	}

	buckets = make([][]int32, numBuckets)

	// For the same reason as above, the average bucket size can be a
	// non-integer. But we want to only operate on integer values. It is
	// possible to do if we premultiply all terms by the number of buckets.
	//
	// The original algorithm with fractions would look like this:
	//
	//     avgGroupSize := size / numBuckets
	//
	//     bucket := 0
	//     itemsInBucket := int32(0)
	//     for i := int32(0); i < size; i++ {
	//         if itemsInBucket >= avgGroupSize {
	//             bucket++
	//             itemsInBucket -= avgGroupSize
	//         }
	//         buckets[bucket] = append(buckets[bucket], i)
	//         itemsInBucket += 1
	//     }
	//
	// Now multiply everything by numBuckets and end up with this:
	bucket := 0
	itemsInBucketPremultiplied := int32(0)
	for i := int32(0); i < size; i++ {
		if itemsInBucketPremultiplied >= size {
			bucket++
			itemsInBucketPremultiplied -= size
		}
		buckets[bucket] = append(buckets[bucket], i)
		itemsInBucketPremultiplied += numBuckets
	}

	return buckets
}

// GetBucketId takes array of arrays of partitions and returns the bucketId holding it or error if id is out of range
func GetBucketId(buckets [][]int32, groupSize int32, targetId int32) (int32, error) {
	lastBucket := buckets[len(buckets)-1]
	if targetId > lastBucket[len(lastBucket)-1] {
		return 0, fmt.Errorf("targetId is out of range")
	}
	return targetId / groupSize, nil
}

func Int2Str(ints []int32) []string {
	result := make([]string, len(ints))
	for i, d := range ints {
		result[i] = strconv.Itoa(int(d))
	}
	return result
}
