package helpers

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	defaultPartitionEnvKey = "KONSUMERATOR_PARTITION"
	gomaxprocsEnvKey       = "GOMAXPROCS"
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

func GomaxprocsFromResource(cpu *resource.Quantity) string {
	value := int(cpu.Value())
	if value < 1 {
		value = 1
	}
	return strconv.Itoa(value)
}

func SetOrUpdateEnv(env []corev1.EnvVar, key string, value string) []corev1.EnvVar {
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
	env = SetOrUpdateEnv(env, partitionKey, strconv.Itoa(partition))
	env = SetOrUpdateEnv(env, gomaxprocsEnvKey, GomaxprocsFromResource(resources.Limits.Cpu()))

	return env
}
