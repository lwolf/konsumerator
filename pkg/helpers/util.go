package helpers

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	defaultPartitionEnvKey = "KONSUMERATOR_PARTITION"
	instanceEnvKey         = "KONSUMERATOR_INSTANCE"
	numInstancesEnvKey     = "KONSUMERATOR_NUM_INSTANCES"
	numPartitionsEnvKey    = "KONSUMERATOR_NUM_PARTITIONS"
	gomaxprocsEnvKey       = "GOMAXPROCS"
	gomemlimitEnvKey       = "GOMEMLIMIT"
	TimeLayout             = time.RFC3339
)

func Ptr[T any](v T) *T {
	return &v
}

func MapToArray(data map[int32]bool) []int32 {
	var res []int32
	for key := range data {
		res = append(res, key)
	}
	sort.Slice(res, func(i, j int) bool { return res[i] < res[j] })
	return res
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
	if len(partitions) == 0 {
		return nil, fmt.Errorf("partitions not found in string %s, have to be a comma-separated list of ids", partitions)
	}
	parts := strings.Split(partitions, ",")
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

func GomemlimitFromResource(memory *resource.Quantity) string {
	// TODO: hardcoded 80% value, allow configuration through the spec
	v := math.Ceil(float64(memory.ScaledValue(resource.Mega)) * 0.8)
	return fmt.Sprintf("%vMiB", v)
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

func PopulateEnv(currentEnv []corev1.EnvVar, resources *corev1.ResourceRequirements, envKey string, partitions []int32, id, numPartitions, numInstances int) []corev1.EnvVar {
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
	if !resources.Limits.Memory().IsZero() {
		env = SetEnv(env, gomemlimitEnvKey, GomemlimitFromResource(resources.Limits.Memory()))
	}
	env = SetEnv(env, instanceEnvKey, strconv.Itoa(id))
	env = SetEnv(env, numPartitionsEnvKey, strconv.Itoa(numPartitions))
	env = SetEnv(env, numInstancesEnvKey, strconv.Itoa(numInstances))

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
//
//	SplitIntoBuckets(10, 3) -> [0,1,2] [3,4,5] [6,7] [8,9]
//	SplitIntoBuckets(12, 3) -> [0,1,2] [3,4,5] [6,7,8] [9,10,11]
func SplitIntoBuckets(size int32, desiredBucketSize int32) (buckets [][]int32) {
	if size == 0 || desiredBucketSize == 0 {
		return
	}

	numBuckets := size / desiredBucketSize
	if size%desiredBucketSize > 0 {
		// Not every size is divisible by bucket size. So sometimes we
		// need an extra bucket.
		numBuckets++
	}

	bucketSize := size / numBuckets

	// This is the number of buckets that will be 1 item larger.
	numLargeBuckets := size % numBuckets

	buckets = make([][]int32, numBuckets)
	var i int32
	for b := range buckets {
		thisBucketSize := bucketSize
		if int32(b) < numLargeBuckets {
			thisBucketSize++
		}
		buckets[b] = make([]int32, thisBucketSize)
		for j := range buckets[b] {
			buckets[b][j] = i
			i++
		}
	}

	return buckets
}

func Int2Str(ints []int32) []string {
	result := make([]string, len(ints))
	for i, d := range ints {
		result[i] = strconv.Itoa(int(d))
	}
	return result
}

// ConsecutiveIntsToRange takes a list of sorted integers and returns
// string representation of its range
// ConsecutiveIntsToRange(0,1,2,3,4,5,6) -> 0-6
// ConsecutiveIntsToRange(12,13,14,15,16) -> 12-16
func ConsecutiveIntsToRange(ints []int32) string {
	res := make([]int32, 2)
	if len(ints) < 3 {
		res = ints
	} else {
		res = []int32{ints[0], ints[len(ints)-1]}
	}
	return strings.Join(Int2Str(res), "-")
}

func EnsureValidLabelValue(s string) string {
	/*
		https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/
		Valid label value:
		* must be 63 characters or less (cannot be empty),
		* must begin and end with an alphanumeric character ([a-z0-9A-Z]),
		* could contain dashes (-), underscores (_), dots (.), and alphanumerics between.

		For now we only ensure the max length here
		TODO: decide what to do with labels containing incompatible chars. At the moment having bad name will break in
		many places, not only in labels.

	*/
	if len(s) > validation.LabelValueMaxLength {
		return s[:validation.LabelValueMaxLength]
	}
	return s
}
