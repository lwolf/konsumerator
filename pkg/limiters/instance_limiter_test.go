package limiters

import (
	"testing"

	tlog "github.com/go-logr/logr/testing"
	corev1 "k8s.io/api/core/v1"

	konsumeratorv1 "github.com/lwolf/konsumerator/api/v1"
	"github.com/lwolf/konsumerator/pkg/helpers"
	"github.com/lwolf/konsumerator/pkg/helpers/tests"
)

func TestInstanceLimiter_MinAllowed(t *testing.T) {
	testCases := map[string]struct {
		containerName string
		policy        konsumeratorv1.ResourcePolicy
		expLimits     *corev1.ResourceList
	}{
		"should return min allowed by containerName": {
			containerName: "test",
			policy: konsumeratorv1.ResourcePolicy{ContainerPolicies: []konsumeratorv1.ContainerResourcePolicy{
				tests.NewContainerResourcePolicy("test", "100m", "100M", "2", "150M"),
			}},
			expLimits: tests.NewResourceList("100m", "100M"),
		},
		"should return nil if no such policy exists": {
			containerName: "not-test",
			policy: konsumeratorv1.ResourcePolicy{ContainerPolicies: []konsumeratorv1.ContainerResourcePolicy{
				tests.NewContainerResourcePolicy("test", "100m", "100M", "2", "150M"),
			}},
			expLimits: nil,
		},
	}
	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			limiter := NewInstanceLimiter(&tc.policy, tlog.NullLogger{})
			limits := limiter.MinAllowed(tc.containerName)
			if tc.expLimits != nil && limits != nil {
				if helpers.CmpResourceList(*limits, *tc.expLimits) != 0 {
					t.Errorf("MinAllowed() results mismatch. want %v, got %v", tc.expLimits, limits)
				}
			} else {
				if tc.expLimits != limits {
					t.Errorf("MinAllowed() results mismatch. want %v, got %v", tc.expLimits, limits)
				}
			}

		})
	}
}

func TestInstanceLimiter_MaxAllowed(t *testing.T) {
	testCases := map[string]struct {
		containerName string
		policy        konsumeratorv1.ResourcePolicy
		expLimits     *corev1.ResourceList
	}{
		"should return max allowed by containerName": {
			containerName: "test",
			policy: konsumeratorv1.ResourcePolicy{ContainerPolicies: []konsumeratorv1.ContainerResourcePolicy{
				tests.NewContainerResourcePolicy("test", "100m", "100M", "2", "150M"),
			}},
			expLimits: tests.NewResourceList("2", "150M"),
		},
		"should return nil if no such policy exists": {
			containerName: "not-test",
			policy: konsumeratorv1.ResourcePolicy{ContainerPolicies: []konsumeratorv1.ContainerResourcePolicy{
				tests.NewContainerResourcePolicy("test", "100m", "100M", "2", "150M"),
			}},
			expLimits: nil,
		},
	}
	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			limiter := NewInstanceLimiter(&tc.policy, tlog.NullLogger{})
			limits := limiter.MaxAllowed(tc.containerName)
			if tc.expLimits != nil && limits != nil {
				if helpers.CmpResourceList(*limits, *tc.expLimits) != 0 {
					t.Errorf("MaxAllowed() results mismatch. want %v, got %v", tc.expLimits, limits)
				}
			} else {
				if tc.expLimits != limits {
					t.Errorf("MaxAllowed() results mismatch. want %v, got %v", tc.expLimits, limits)
				}
			}
		})
	}
}

func TestInstanceLimiter_ApplyLimits(t *testing.T) {
	testCases := map[string]struct {
		containerName string
		policy        konsumeratorv1.ResourcePolicy
		estimates     *corev1.ResourceRequirements
		expRes        *corev1.ResourceRequirements
	}{
		"limits should override estimated cpu requests": {
			containerName: "test",
			policy: konsumeratorv1.ResourcePolicy{ContainerPolicies: []konsumeratorv1.ContainerResourcePolicy{
				tests.NewContainerResourcePolicy("test", "100m", "100M", "2", "150M"),
			}},
			estimates: tests.NewResourceRequirements("2.1", "100M", "2", "150M"),
			expRes:    tests.NewResourceRequirements("2", "100M", "2", "150M"),
		},
		"limits should override estimated memory requests": {
			containerName: "test",
			policy: konsumeratorv1.ResourcePolicy{ContainerPolicies: []konsumeratorv1.ContainerResourcePolicy{
				tests.NewContainerResourcePolicy("test", "100m", "100M", "1", "150M"),
			}},
			estimates: tests.NewResourceRequirements("100m", "200M", "1", "150M"),
			expRes:    tests.NewResourceRequirements("100m", "150M", "1", "150M"),
		},
		"limits should override estimated cpu limits": {
			containerName: "test",
			policy: konsumeratorv1.ResourcePolicy{ContainerPolicies: []konsumeratorv1.ContainerResourcePolicy{
				tests.NewContainerResourcePolicy("test", "100m", "100M", "2", "150M"),
			}},
			estimates: tests.NewResourceRequirements("2", "100M", "3", "100M"),
			expRes:    tests.NewResourceRequirements("2", "100M", "2", "100M"),
		},
		"limits should override estimated memory limits": {
			containerName: "test",
			policy: konsumeratorv1.ResourcePolicy{ContainerPolicies: []konsumeratorv1.ContainerResourcePolicy{
				tests.NewContainerResourcePolicy("test", "100m", "100M", "1", "150M"),
			}},
			estimates: tests.NewResourceRequirements("1", "100M", "1", "200M"),
			expRes:    tests.NewResourceRequirements("1", "100M", "1", "150M"),
		},
		"minimums should be applied on 0 estimates": {
			containerName: "test",
			policy: konsumeratorv1.ResourcePolicy{ContainerPolicies: []konsumeratorv1.ContainerResourcePolicy{
				tests.NewContainerResourcePolicy("test", "100m", "100M", "1", "150M"),
			}},
			estimates: tests.NewResourceRequirements("0", "0", "0", "0"),
			expRes:    tests.NewResourceRequirements("100m", "100M", "100m", "100M"),
		},
		"no resources are being set if neither estimates nor limits were test": {
			containerName: "test",
			policy:        konsumeratorv1.ResourcePolicy{},
			estimates:     tests.NewResourceRequirements("0", "0", "0", "0"),
			expRes:        &corev1.ResourceRequirements{},
		},
		"limits are not enforced because there are no limits for that container": {
			containerName: "test",
			policy: konsumeratorv1.ResourcePolicy{ContainerPolicies: []konsumeratorv1.ContainerResourcePolicy{
				tests.NewContainerResourcePolicy("another-test", "100m", "100M", "1", "150M"),
			}},
			estimates: tests.NewResourceRequirements("1", "100M", "1", "200M"),
			expRes:    tests.NewResourceRequirements("1", "100M", "1", "200M"),
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
