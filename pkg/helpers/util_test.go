package helpers

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestGomemlimitFromResource(t *testing.T) {
	testCases := map[string]struct {
		in     string
		expOut string
	}{
		"simple M":   {"100M", "80MiB"},
		"rounding M": {"111M", "89MiB"},
		"simple G":   {"1G", "800MiB"},
		"big G":      {"20G", "16000MiB"},
	}
	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			v := resource.MustParse(tc.in)
			out := GomemlimitFromResource(&v)
			if tc.expOut != out {
				t.Fatalf("expected to get %s, got %s", tc.expOut, out)
			}
		})
	}
}

func TestPopulateEnv(t *testing.T) {
	tests := map[string]struct {
		initialEnv    []corev1.EnvVar
		resources     *corev1.ResourceRequirements
		envKey        string
		partitions    []int32
		instanceId    int
		numPartitions int
		numInstances  int
		expEnv        []corev1.EnvVar
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
			envKey:        "",
			partitions:    []int32{0},
			instanceId:    0,
			numPartitions: 1,
			numInstances:  1,
			expEnv: []corev1.EnvVar{
				{
					Name:  defaultPartitionEnvKey,
					Value: "0",
				},
				{
					Name:  gomaxprocsEnvKey,
					Value: "2",
				},
				{
					Name:  gomemlimitEnvKey,
					Value: "1600MiB",
				},
				{
					Name:  instanceEnvKey,
					Value: "0",
				},
				{
					Name:  numPartitionsEnvKey,
					Value: "1",
				},
				{
					Name:  numInstancesEnvKey,
					Value: "1",
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
			envKey:        "TEST_PARTITION",
			partitions:    []int32{0},
			instanceId:    0,
			numPartitions: 1,
			numInstances:  1,
			expEnv: []corev1.EnvVar{
				{
					Name:  "TEST_PARTITION",
					Value: "0",
				},
				{
					Name:  gomaxprocsEnvKey,
					Value: "1",
				},
				{
					Name:  gomemlimitEnvKey,
					Value: "1600MiB",
				},
				{
					Name:  instanceEnvKey,
					Value: "0",
				},
				{
					Name:  numPartitionsEnvKey,
					Value: "1",
				},
				{
					Name:  numInstancesEnvKey,
					Value: "1",
				},
			},
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			backupEnv := append(tt.initialEnv[:0:0], tt.initialEnv...) // backup original env
			got := PopulateEnv(tt.initialEnv, tt.resources, tt.envKey, tt.partitions, tt.instanceId, tt.numPartitions, tt.numInstances)
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

func TestParsePartitionsListAnnotation(t *testing.T) {
	testCases := map[string]struct {
		in     string
		expIds []int32
		expErr bool
	}{
		"should return error on empty string": {
			"", nil, true,
		},
		"should parse a single id": {
			"1", []int32{1}, false,
		},
		"should parse a multiple ids": {
			"0,1", []int32{0, 1}, false,
		},
		"should error on incorrect data": {
			"0-1", nil, true,
		},
		"should error on incorrect data 2": {
			"0,1,a", nil, true,
		},
	}
	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			res, err := ParsePartitionsListAnnotation(tc.in)
			if err != nil {
				if !tc.expErr {
					t.Fatalf("got unexpected error: %v", err)
				}
			} else {
				if tc.expErr {
					t.Fatalf("expected to get an error, but haven't get one")
				}
			}
			if !cmp.Equal(tc.expIds, res) {
				t.Fatalf("expected %v, got %v", tc.expIds, res)
			}
		})
	}
}

func TestConsecutiveIntsToRange(t *testing.T) {
	testCases := map[string]struct {
		in  []int32
		exp string
	}{
		"single digits not a range": {
			[]int32{0},
			"0",
		},
		"double digit is a range": {
			[]int32{0, 1},
			"0-1",
		},
		"many digits making range": {
			[]int32{0, 1, 2, 3, 4, 5, 6, 7, 8, 10, 12},
			"0-12",
		},
		"vary many digits still make range": {
			[]int32{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29},
			"2-29",
		},
	}
	for testName, tc := range testCases {
		if res := ConsecutiveIntsToRange(tc.in); res != tc.exp {
			t.Fatalf("%s: expected to get `%s`, got `%s`", testName, tc.exp, res)
		}
	}

}

func TestEnsureValidLabelValue(t *testing.T) {
	testCases := map[string]struct {
		in  string
		exp string
	}{
		"short label value is OK": {
			"0-1-2-3-4-5-6-7-8-10-12",
			"0-1-2-3-4-5-6-7-8-10-12",
		},
		"long name should be cut up to the limit": {
			"0-1-2-3-4-5-6-7-8-9-10-11-12-13-14-15-16-17-18-19-20-21-22-23-24-25-26-27-28-29",
			"0-1-2-3-4-5-6-7-8-9-10-11-12-13-14-15-16-17-18-19-20-21-22-23-2",
		},
	}
	for testName, tc := range testCases {
		if res := EnsureValidLabelValue(tc.in); res != tc.exp {
			t.Fatalf("%s: expected to get `%s`, got `%s`", testName, tc.exp, res)
		}
	}
}
