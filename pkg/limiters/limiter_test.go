package limiters

import (
	"testing"

	tlog "github.com/go-logr/logr/testing"
	autoscalev1 "github.com/kubernetes/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"github.com/lwolf/konsumerator/pkg/helpers"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func constructContainerResourcePolicy(name, minCpu, minMem, maxCpu, maxMem string) autoscalev1.ContainerResourcePolicy {
	return autoscalev1.ContainerResourcePolicy{
		ContainerName: name,
		MinAllowed: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(minCpu),
			corev1.ResourceMemory: resource.MustParse(minMem),
		},
		MaxAllowed: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(maxCpu),
			corev1.ResourceMemory: resource.MustParse(maxMem),
		},
	}
}

func constructResourceRequirements(reqCpu, reqMem, limCpu, limMem string) *corev1.ResourceRequirements {
	return &corev1.ResourceRequirements{
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

func TestInstanceLimiter_ApplyLimits(t *testing.T) {
	testCases := map[string]struct {
		containerName string
		policy        autoscalev1.PodResourcePolicy
		estimates     *corev1.ResourceRequirements
		expRes        *corev1.ResourceRequirements
	}{
		"limits should override estimates": {
			containerName: "test",
			policy: autoscalev1.PodResourcePolicy{ContainerPolicies: []autoscalev1.ContainerResourcePolicy{
				constructContainerResourcePolicy("test", "100m", "100M", "1", "150M"),
			}},
			estimates: constructResourceRequirements("2.1", "200M", "3", "200M"),
			expRes:    constructResourceRequirements("1", "150M", "1", "150M"),
		},
		"minimums should be applied on 0 estimates": {
			containerName: "test",
			policy: autoscalev1.PodResourcePolicy{ContainerPolicies: []autoscalev1.ContainerResourcePolicy{
				constructContainerResourcePolicy("test", "100m", "100M", "1", "150M"),
			}},
			estimates: constructResourceRequirements("0", "0", "0", "0"),
			expRes:    constructResourceRequirements("100m", "100M", "100m", "100M"),
		},
		"no resources are being set if neither estimates nor limits were test": {
			containerName: "test",
			policy:        autoscalev1.PodResourcePolicy{},
			estimates:     constructResourceRequirements("0", "0", "0", "0"),
			expRes:        &corev1.ResourceRequirements{},
		},
	}
	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			limiter := NewInstanceLimiter(&tc.policy, tlog.NullLogger{})
			resources := limiter.ApplyLimits(tc.containerName, tc.estimates)
			if helpers.CmpResourceRequirements(*resources, *tc.expRes) != 0 {
				t.Errorf("ApplyLimits() results mismatch. want %v, got %v", tc.expRes, resources)
			}
		})
	}
}
