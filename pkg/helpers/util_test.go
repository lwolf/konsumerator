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
		partitions []int32
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
			envKey:     "",
			partitions: []int32{0},
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
			envKey:     "TEST_PARTITION",
			partitions: []int32{0},
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
			got := PopulateEnv(tt.initialEnv, tt.resources, tt.envKey, tt.partitions)
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

func TestSplitIntoBuckets(t *testing.T) {
	testCases := map[string]struct {
		size      int32
		groupSize int32
		exp       [][]int32
	}{
		"zero size and zero groupSize":      {0, 0, nil},
		"zero size and non-zero groupSize":  {0, 10, nil},
		"non-zero size with zero groupSize": {10, 0, nil},
		"one element":                       {1, 1, [][]int32{{0}}},
		"size less than groupSize":          {3, 5, [][]int32{{0, 1, 2}}},
		"non equal group":                   {10, 3, [][]int32{{0, 1, 2}, {3, 4, 5}, {6, 7}, {8, 9}}},
		"equal groupping":                   {9, 3, [][]int32{{0, 1, 2}, {3, 4, 5}, {6, 7, 8}}},
		"1 partition per group":             {5, 1, [][]int32{{0}, {1}, {2}, {3}, {4}}},
	}
	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			res := SplitIntoBuckets(tc.size, tc.groupSize)
			if !cmp.Equal(res, tc.exp) {
				t.Fatalf("expected %v, got %v", tc.exp, res)
			}
		})
	}
}

func TestGetBucketId(t *testing.T) {
	testCases := map[string]struct {
		buckets   [][]int32
		groupSize int32
		id        int32
		expId     int32
		expErr    bool
	}{
		"correct bucket from the groups with 1 id": {
			[][]int32{{0}, {1}, {2}, {3}, {4}}, 1, 3, 3, false,
		},
		"correct bucket from the groups with 2 ids": {
			[][]int32{{0, 1}, {2, 3}, {3, 4}, {5, 6}, {7, 8}}, 2, 3, 1, false,
		},
		"correct bucket for the first element": {
			[][]int32{{0, 1}, {2, 3}, {3, 4}, {5, 6}, {7, 8}}, 2, 0, 0, false,
		},
		"correct bucket for the last element": {
			[][]int32{{0, 1}, {2, 3}, {3, 4}, {5, 6}, {7, 8}}, 2, 8, 4, false,
		},
		"should return error if id is out of range ": {
			[][]int32{{0, 1}, {2, 3}, {3, 4}, {5, 6}, {7, 8}}, 2, 10, 0, true,
		},
	}
	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			res, err := GetBucketId(tc.buckets, tc.groupSize, tc.id)
			if err != nil {
				if !tc.expErr {
					t.Fatalf("got unexpected error %v", err)
				}
			} else {
				if tc.expErr {
					t.Fatalf("expected to get an error, but haven't get one")
				}
			}
			if tc.expId != res {
				t.Fatalf("expected %v, got %v", tc.expId, res)
			}
		})
	}
}
