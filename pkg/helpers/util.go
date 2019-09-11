package helpers

import (
	"strconv"
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

func ParsePartitionAnnotation(partition string) (int32, error) {
	p, err := strconv.ParseInt(partition, 10, 32)
	if err != nil {
		return int32(0), err
	}
	return int32(p), nil
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

func PopulateEnv(currentEnv []corev1.EnvVar, resources *corev1.ResourceRequirements, envKey string, partition int) []corev1.EnvVar {
	var partitionKey string
	if envKey != "" {
		partitionKey = envKey
	} else {
		partitionKey = defaultPartitionEnvKey
	}
	env := make([]corev1.EnvVar, len(currentEnv))
	copy(env, currentEnv)
	env = SetEnv(env, partitionKey, strconv.Itoa(partition))
	env = SetEnv(env, gomaxprocsEnvKey, GomaxprocsFromResource(resources.Limits.Cpu()))

	return env
}

func CmpResourceRequirements(old corev1.ResourceRequirements, new corev1.ResourceRequirements) int {
	reqCpu := new.Requests.Cpu().Cmp(*old.Requests.Cpu())
	limCpu := new.Limits.Cpu().Cmp(*old.Limits.Cpu())
	reqMem := new.Requests.Memory().Cmp(*old.Requests.Memory())
	limMem := new.Limits.Memory().Cmp(*old.Limits.Memory())
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
