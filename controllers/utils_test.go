package controllers

import (
	"github.com/lwolf/konsumerator/pkg/helpers/tests"
	appsv1 "k8s.io/api/apps/v1"
	"testing"
	"time"

	"github.com/lwolf/konsumerator/pkg/helpers"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/google/go-cmp/cmp"
	konsumeratorv1 "github.com/lwolf/konsumerator/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPopulateStatusFromAnnotation(t *testing.T) {
	tm, _ := time.Parse(helpers.TimeLayout, "2019-10-03T13:38:03Z")
	mtm := metav1.NewTime(tm)
	testCases := map[string]struct {
		in     map[string]string
		status *konsumeratorv1.ConsumerStatus
	}{
		"expect to get empty status": {
			in: map[string]string{},
			status: &konsumeratorv1.ConsumerStatus{
				Expected:      helpers.Ptr[int32](0),
				Running:       helpers.Ptr[int32](0),
				Paused:        helpers.Ptr[int32](0),
				Lagging:       helpers.Ptr[int32](0),
				Missing:       helpers.Ptr[int32](0),
				Outdated:      helpers.Ptr[int32](0),
				LastSyncTime:  nil,
				LastSyncState: nil,
			},
		},
		"expect to get status counters": {
			in: map[string]string{
				annotationStatusExpected: "6",
				annotationStatusRunning:  "1",
				annotationStatusPaused:   "2",
				annotationStatusLagging:  "3",
				annotationStatusMissing:  "4",
				annotationStatusOutdated: "5",
			},
			status: &konsumeratorv1.ConsumerStatus{
				Expected:      helpers.Ptr[int32](6),
				Running:       helpers.Ptr[int32](1),
				Paused:        helpers.Ptr[int32](2),
				Lagging:       helpers.Ptr[int32](3),
				Missing:       helpers.Ptr[int32](4),
				Outdated:      helpers.Ptr[int32](5),
				LastSyncTime:  nil,
				LastSyncState: nil,
			},
		},
		"expect to get lastsync timestamp and empty status": {
			in: map[string]string{
				annotationStatusExpected:     "6",
				annotationStatusRunning:      "1",
				annotationStatusPaused:       "2",
				annotationStatusLagging:      "3",
				annotationStatusMissing:      "4",
				annotationStatusOutdated:     "5",
				annotationStatusLastSyncTime: "2019-10-03T13:38:03Z",
				annotationStatusLastState:    "bad state",
			},
			status: &konsumeratorv1.ConsumerStatus{
				Expected:      helpers.Ptr[int32](6),
				Running:       helpers.Ptr[int32](1),
				Paused:        helpers.Ptr[int32](2),
				Lagging:       helpers.Ptr[int32](3),
				Missing:       helpers.Ptr[int32](4),
				Outdated:      helpers.Ptr[int32](5),
				LastSyncTime:  &mtm,
				LastSyncState: nil,
			},
		},
	}
	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			status := &konsumeratorv1.ConsumerStatus{}
			PopulateStatusFromAnnotation(tc.in, status)
			if diff := cmp.Diff(status, tc.status); diff != "" {
				t.Fatalf("status mismatch (-status +tc.status):\n%s", diff)
			}
		})
	}
}

func TestUpdateStatusAnnotations(t *testing.T) {
	tm, _ := time.Parse(helpers.TimeLayout, "2019-10-03T13:38:03Z")
	mtm := metav1.NewTime(tm)
	testCases := map[string]struct {
		status    *konsumeratorv1.ConsumerStatus
		configMap *corev1.ConfigMap
		expErr    bool
	}{
		"sanity check": {
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationStatusExpected: "6",
						annotationStatusRunning:  "1",
						annotationStatusPaused:   "2",
						annotationStatusLagging:  "3",
						annotationStatusMissing:  "4",
						annotationStatusOutdated: "5",
					},
				},
			},
			status: &konsumeratorv1.ConsumerStatus{
				Expected:     helpers.Ptr[int32](6),
				Running:      helpers.Ptr[int32](1),
				Paused:       helpers.Ptr[int32](2),
				Lagging:      helpers.Ptr[int32](3),
				Missing:      helpers.Ptr[int32](4),
				Outdated:     helpers.Ptr[int32](5),
				LastSyncTime: &mtm,
				LastSyncState: map[string]konsumeratorv1.InstanceState{
					"0": {ProductionRate: 20, ConsumptionRate: 10, MessagesBehind: 1000},
				},
			},
		},
	}
	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			}
			err := UpdateStatusAnnotations(cm, tc.status)
			if (tc.expErr && err == nil) || (!tc.expErr && err != nil) {
				t.Fatalf("error expectation failed, expected to get error=%v, got=%v", tc.expErr, err)
			}
		})
	}

}

func TestResourceListSum(t *testing.T) {
	testCases := map[string]struct {
		a   corev1.ResourceList
		b   corev1.ResourceList
		exp corev1.ResourceList
	}{
		"sanity check": {
			corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("100M"),
			},
			corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("10M"),
			},
			corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("150m"),
				corev1.ResourceMemory: resource.MustParse("110M"),
			},
		},
	}
	for tcName, tc := range testCases {
		t.Run(tcName, func(t *testing.T) {
			beforeA := tc.a.DeepCopy()
			beforeB := tc.b.DeepCopy()
			res := resourceListSum(&tc.a, &tc.b)
			if isResourceListEqual(*res, tc.exp) {
				t.Fatalf("expected value: %v, got: %v", tc.exp, res)
			}
			if isResourceListEqual(beforeA, tc.a) {
				t.Fatalf("unexpected mutation of the initial data!")
			}
			if isResourceListEqual(beforeB, tc.b) {
				t.Fatalf("unexpected mutation of the initial data!")
			}
		})
	}
}

func isResourceListEqual(a, b corev1.ResourceList) bool {
	if a.Cpu().Cmp(*b.Cpu()) != 0 {
		return false
	}
	if a.Memory().Cmp(*b.Cpu()) != 0 {
		return false
	}
	return true
}

func TestCalculateFallbackFromPolicy(t *testing.T) {
	testCases := map[string]struct {
		strategy      konsumeratorv1.FallbackStrategy
		containerName []string
		policy        *konsumeratorv1.ResourcePolicy
		expFallback   map[string]*corev1.ResourceRequirements
	}{
		"should return min allowed by containerName in the map": {
			strategy:      konsumeratorv1.FallbackStrategyMin,
			containerName: []string{"test"},
			policy: &konsumeratorv1.ResourcePolicy{ContainerPolicies: []konsumeratorv1.ContainerResourcePolicy{
				tests.NewContainerResourcePolicy("test", "100m", "100M", "2", "150M"),
			}},
			expFallback: map[string]*corev1.ResourceRequirements{"test": tests.NewResourceRequirements("100m", "100M", "100m", "100M")},
		},
		"should return max allowed by containerName in the map": {
			strategy:      konsumeratorv1.FallbackStrategyMax,
			containerName: []string{"test"},
			policy: &konsumeratorv1.ResourcePolicy{ContainerPolicies: []konsumeratorv1.ContainerResourcePolicy{
				tests.NewContainerResourcePolicy("test", "100m", "100M", "2", "150M"),
			}},
			expFallback: map[string]*corev1.ResourceRequirements{"test": tests.NewResourceRequirements("2", "150M", "2", "150M")},
		},
		"multi container setup should return correct values": {
			strategy:      konsumeratorv1.FallbackStrategyMin,
			containerName: []string{"test", "test2"},
			policy: &konsumeratorv1.ResourcePolicy{ContainerPolicies: []konsumeratorv1.ContainerResourcePolicy{
				tests.NewContainerResourcePolicy("test", "100m", "100M", "2", "150M"),
				tests.NewContainerResourcePolicy("test2", "1", "1G", "5", "2G"),
			}},
			expFallback: map[string]*corev1.ResourceRequirements{
				"test":  tests.NewResourceRequirements("100m", "100M", "100m", "100M"),
				"test2": tests.NewResourceRequirements("1", "1G", "1", "1G"),
			},
		},
		"should return nil if no such policy exists": {
			strategy:      konsumeratorv1.FallbackStrategyMax,
			containerName: []string{"test"},
			policy:        nil,
			expFallback:   nil,
		},
	}
	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			fallback := calculateFallbackFromPolicy(tc.policy, tc.strategy)
			if tc.expFallback == nil && fallback != nil {
				t.Fatalf("Fallback is expected to be nil, got %v", fallback)
			}
			if tc.expFallback != nil {
				for _, containerName := range tc.containerName {
					if helpers.CmpResourceList(fallback[containerName].Requests, tc.expFallback[containerName].Requests) != 0 {
						t.Errorf("Fallback results mismatch. want %v, got %v", tc.expFallback[containerName].Requests, fallback[containerName].Requests)
					}
					if helpers.CmpResourceList(fallback[containerName].Limits, tc.expFallback[containerName].Limits) != 0 {
						t.Errorf("Fallback results mismatch. want %v, got %v", tc.expFallback[containerName].Requests, fallback[containerName].Requests)
					}
				}
			}
		})
	}
}

func TestCalculateFallbackFromRunningInstances(t *testing.T) {
	instances := []appsv1.Deployment{
		{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "busybox", Resources: *tests.NewResourceRequirements("100m", "100M", "100m", "100M")},
							{Name: "test", Resources: *tests.NewResourceRequirements("600m", "600M", "600m", "600M")},
						}},
				},
			},
		},
		{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "busybox", Resources: *tests.NewResourceRequirements("150m", "150M", "1", "100G")},
							{Name: "test", Resources: *tests.NewResourceRequirements("50m", "50M", "2", "200G")},
						}},
				},
			},
		},
		{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "busybox", Resources: *tests.NewResourceRequirements("200m", "200M", "300m", "300M")},
							{Name: "test", Resources: *tests.NewResourceRequirements("100m", "100M", "300m", "300M")},
						}},
				},
			},
		},
	}

	testCases := map[string]struct {
		strategy      konsumeratorv1.FallbackStrategy
		containerName []string
		instances     []appsv1.Deployment
		expFallback   map[string]*corev1.ResourceRequirements
	}{
		"min": {
			strategy:      konsumeratorv1.FallbackStrategyMin,
			containerName: []string{"busybox", "test"},
			instances:     instances,
			expFallback: map[string]*corev1.ResourceRequirements{
				"busybox": tests.NewResourceRequirements("100m", "100M", "100m", "100M"),
				"test":    tests.NewResourceRequirements("50m", "50M", "2", "200G"),
			},
		},
		"max": {
			strategy:      konsumeratorv1.FallbackStrategyMax,
			containerName: []string{"busybox", "test"},
			instances:     instances,
			expFallback: map[string]*corev1.ResourceRequirements{
				"busybox": tests.NewResourceRequirements("200m", "200M", "300m", "300M"),
				"test":    tests.NewResourceRequirements("600m", "600M", "600m", "600M"),
			},
		},
	}
	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			fallback := calculateFallbackFromRunningInstances(tc.instances, tc.strategy)
			if tc.expFallback == nil && fallback != nil {
				t.Fatalf("Fallback is expected to be nil, got %v", fallback)
			}
			if tc.expFallback != nil {
				for _, containerName := range tc.containerName {
					if helpers.CmpResourceList(fallback[containerName].Requests, tc.expFallback[containerName].Requests) != 0 {
						t.Errorf("Fallback results mismatch. want %v, got %v", tc.expFallback[containerName].Requests, fallback[containerName].Requests)
					}
					if helpers.CmpResourceList(fallback[containerName].Limits, tc.expFallback[containerName].Limits) != 0 {
						t.Errorf("Fallback results mismatch. want %v, got %v", tc.expFallback[containerName].Requests, fallback[containerName].Requests)
					}
				}
			}

		})
	}
}
