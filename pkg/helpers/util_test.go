package helpers

import (
	"testing"

	"github.com/google/go-cmp/cmp"
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
			got := SetEnv(tt.initialEnv, tt.envKey, tt.envValue)
			if diff := cmp.Diff(tt.expEnv, got); diff != "" {
				t.Errorf("%s PopulateEnv() mismatch (-tt.expEnv +got):\n%s", testName, diff)
			}
		})
	}
}

func constructResourceRequirements(reqCpu, reqMem, limCpu, limMem string) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(reqCpu),
			corev1.ResourceMemory: resource.MustParse(reqMem),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(limCpu),
			corev1.ResourceMemory: resource.MustParse(limMem),
		},
	}
}

func TestCmpResourceRequirements(t *testing.T) {
	tests := map[string]struct {
		old corev1.ResourceRequirements
		new corev1.ResourceRequirements
		exp int
	}{
		"old has higher cpu request": {
			old: constructResourceRequirements("2.1", "2G", "3", "2G"),
			new: constructResourceRequirements("2", "2G", "3", "2G"),
			exp: -1,
		},
		"new has higher cpu request": {
			old: constructResourceRequirements("2", "2G", "3", "2G"),
			new: constructResourceRequirements("2.5", "2G", "3", "2G"),
			exp: 1,
		},
		"old has higher cpu limit": {
			old: constructResourceRequirements("2", "2G", "3", "2G"),
			new: constructResourceRequirements("2", "2G", "2", "2G"),
			exp: -1,
		},
		"old has higher memory request": {
			old: constructResourceRequirements("2", "3G", "2", "3G"),
			new: constructResourceRequirements("2", "2G", "2", "3G"),
			exp: -1,
		},
		"old has higher memory limit": {
			old: constructResourceRequirements("2", "2G", "2", "3G"),
			new: constructResourceRequirements("2", "2G", "2", "2G"),
			exp: -1,
		},
		"equal requirements": {
			old: constructResourceRequirements("2", "2G", "2", "2G"),
			new: constructResourceRequirements("2", "2G", "2", "2G"),
			exp: 0,
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			res := CmpResourceRequirements(tt.old, tt.new)
			if res != tt.exp {
				t.Fatalf("expected %d, got %d", tt.exp, res)
			}
		})
	}
}
