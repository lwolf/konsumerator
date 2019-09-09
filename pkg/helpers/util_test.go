package helpers

import (
	"github.com/google/go-cmp/cmp"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestPopulateEnv(t *testing.T) {
	tests := map[string]struct {
		initialEnv []corev1.EnvVar
		resources  *corev1.ResourceRequirements
		envKey     string
		partition  int
		expEnv     []corev1.EnvVar
	}{
		"simple case": {
			initialEnv: nil,
			resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("2G"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("2G"),
				},
			},
			envKey:    "",
			partition: 0,
			expEnv: []corev1.EnvVar{
				{
					Name:  defaultPartitionEnvKey,
					Value: "0",
				}, {
					Name:  gomaxprocsEnvKey,
					Value: "2",
				},
			},
		},
		"custom partition key": {
			initialEnv: nil,
			resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("2G"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("2G"),
				},
			},
			envKey:    "TEST_PARTITION",
			partition: 0,
			expEnv: []corev1.EnvVar{
				{
					Name:  "TEST_PARTITION",
					Value: "0",
				}, {
					Name:  gomaxprocsEnvKey,
					Value: "1",
				},
			},
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			backupEnv := append(tt.initialEnv[:0:0], tt.initialEnv...) // backup original env
			got := PopulateEnv(tt.initialEnv, tt.resources, tt.envKey, tt.partition)
			if diff := cmp.Diff(tt.expEnv, got); diff != "" {
				t.Errorf("%s PopulateEnv() mismatch (-tt.expEnv +got):\n%s", testName, diff)
			}
			if diff := cmp.Diff(tt.initialEnv, backupEnv); diff != "" {
				t.Errorf("%s PopulateEnv() original env should not be modified (-tt.initialEnv +backupEnv):\n%s", testName, diff)
			}
		})
	}
}

func TestSetOrUpdateEnv(t *testing.T) {
	tests := map[string]struct {
		initialEnv []corev1.EnvVar
		envKey     string
		envValue   string
		expEnv     []corev1.EnvVar
	}{
		"add value to an empty env": {
			initialEnv: nil,
			envKey:     gomaxprocsEnvKey,
			envValue:   "1",
			expEnv: []corev1.EnvVar{
				{
					Name:  gomaxprocsEnvKey,
					Value: "1",
				},
			},
		},
		"add value to non-empty env": {
			initialEnv: []corev1.EnvVar{
				{
					Name:  "TEST_PARTITION",
					Value: "0",
				},
			},
			envKey:   gomaxprocsEnvKey,
			envValue: "1",
			expEnv: []corev1.EnvVar{
				{
					Name:  "TEST_PARTITION",
					Value: "0",
				}, {
					Name:  gomaxprocsEnvKey,
					Value: "1",
				},
			},
		},
		"update existing value in env": {
			initialEnv: []corev1.EnvVar{
				{
					Name:  gomaxprocsEnvKey,
					Value: "1",
				},
			},
			envKey:   gomaxprocsEnvKey,
			envValue: "2",
			expEnv: []corev1.EnvVar{
				{
					Name:  gomaxprocsEnvKey,
					Value: "2",
				},
			},
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			got := SetOrUpdateEnv(&tt.initialEnv, tt.envKey, tt.envValue)
			if diff := cmp.Diff(tt.expEnv, got); diff != "" {
				t.Errorf("%s PopulateEnv() mismatch (-tt.expEnv +got):\n%s", testName, diff)
			}
			// make sure we're updating the slice in place
			if diff := cmp.Diff(tt.initialEnv, tt.expEnv); diff != "" {
				t.Errorf("%s PopulateEnv() original env should be modified (-tt.initialEnv +tt.expEnv):\n%s", testName, diff)
			}
		})
	}
}
