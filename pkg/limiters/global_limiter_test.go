package limiters

import (
	"github.com/go-logr/logr"
	"testing"

	konsumeratorv1 "github.com/lwolf/konsumerator/api/v1"
	"github.com/lwolf/konsumerator/pkg/helpers"
	"github.com/lwolf/konsumerator/pkg/helpers/tests"
	corev1 "k8s.io/api/core/v1"
)

func TestGlobalLimiter_ApplyLimits(t *testing.T) {
	testCases := map[string]struct {
		policy    *konsumeratorv1.ResourcePolicy
		used      *corev1.ResourceList
		requested *corev1.ResourceRequirements
		expRes    *corev1.ResourceRequirements
	}{
		"request less than limits": {
			policy:    newGlobalPolicy("100", "100M"),
			used:      tests.NewResourceList("10", "10M"),
			requested: tests.NewResourceRequirements("50", "50M", "0", "0"),
			expRes:    tests.NewResourceRequirements("50", "50M", "0", "0"),
		},
		"request equal to limits": {
			policy:    newGlobalPolicy("100", "100M"),
			used:      tests.NewResourceList("10", "10M"),
			requested: tests.NewResourceRequirements("90", "90M", "0", "0"),
			expRes:    tests.NewResourceRequirements("90", "90M", "0", "0"),
		},
		"request more than limits": {
			policy:    newGlobalPolicy("100", "100M"),
			used:      tests.NewResourceList("10", "10M"),
			requested: tests.NewResourceRequirements("110", "120M", "0", "0"),
			expRes:    tests.NewResourceRequirements("90", "90M", "0", "0"),
		},
		"request when there are no limits": {
			policy:    nil,
			used:      tests.NewResourceList("10", "10M"),
			requested: tests.NewResourceRequirements("1000", "1000M", "0", "0"),
			expRes:    tests.NewResourceRequirements("1000", "1000M", "0", "0"),
		},
		"request negative": {
			policy:    newGlobalPolicy("100", "100M"),
			used:      tests.NewResourceList("10", "10M"),
			requested: tests.NewResourceRequirements("-10", "-20M", "0", "0"),
			expRes:    tests.NewResourceRequirements("-10", "-20M", "0", "0"),
		},
		"request negative when there are no limits": {
			policy:    nil,
			used:      tests.NewResourceList("10", "10M"),
			requested: tests.NewResourceRequirements("-10", "-20M", "0", "0"),
			expRes:    tests.NewResourceRequirements("-10", "-20M", "0", "0"),
		},
		"request while memory pool is exhausted": {
			policy:    newGlobalPolicy("100", "10M"),
			used:      tests.NewResourceList("10", "10M"),
			requested: tests.NewResourceRequirements("10", "20M", "0", "0"),
			expRes:    tests.NewResourceRequirements("10", "0", "0", "0"),
		},
		"request while cpu pool is exhausted": {
			policy:    newGlobalPolicy("100", "10M"),
			used:      tests.NewResourceList("100", "5M"),
			requested: tests.NewResourceRequirements("10", "5M", "0", "0"),
			expRes:    tests.NewResourceRequirements("0", "5M", "0", "0"),
		},
		"request while both pools are exhausted": {
			policy:    newGlobalPolicy("100", "10M"),
			used:      tests.NewResourceList("100", "10M"),
			requested: tests.NewResourceRequirements("10", "20M", "0", "0"),
			expRes:    tests.NewResourceRequirements("0", "0", "0", "0"),
		},
	}
	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			l := NewGlobalLimiter(tc.policy, tc.used, logr.Discard())
			r := l.ApplyLimits("", tc.requested)
			if helpers.CmpResourceRequirements(*r, *tc.expRes) != 0 {
				t.Errorf("ApplyLimits() results mismatch. \nWant: \n%v; \nGot: \n%v", tc.expRes, r)
			}
		})
	}
}

// TestGlobalLimiter_ApplyLimits2 tests single GlobalLimiter object
// changing its state by sequential list of steps
func TestGlobalLimiter_ApplyLimits2(t *testing.T) {
	policy := newGlobalPolicy("100", "100M")
	used := tests.NewResourceList("10", "10M")
	limiter := NewGlobalLimiter(policy, used, logr.Discard())

	steps := []struct {
		requested *corev1.ResourceRequirements
		expRes    *corev1.ResourceRequirements
		expState  *corev1.ResourceList
	}{
		{
			requested: tests.NewResourceRequirements("5", "5M", "0", "0"),
			expRes:    tests.NewResourceRequirements("5", "5M", "0", "0"),
			expState:  tests.NewResourceList("85", "85M"),
		},
		{
			requested: tests.NewResourceRequirements("15", "20M", "0", "0"),
			expRes:    tests.NewResourceRequirements("15", "20M", "0", "0"),
			expState:  tests.NewResourceList("70", "65M"),
		},
		{
			requested: tests.NewResourceRequirements("80", "20M", "0", "0"),
			expRes:    tests.NewResourceRequirements("70", "20M", "0", "0"),
			expState:  tests.NewResourceList("0", "45M"),
		},
		{
			requested: tests.NewResourceRequirements("-20", "-40M", "0", "0"),
			expRes:    tests.NewResourceRequirements("-20", "-40M", "0", "0"),
			expState:  tests.NewResourceList("20", "85M"),
		},
		{
			requested: tests.NewResourceRequirements("-10", "-10M", "0", "0"),
			expRes:    tests.NewResourceRequirements("-10", "-10M", "0", "0"),
			expState:  tests.NewResourceList("30", "95M"),
		},
		{
			requested: tests.NewResourceRequirements("25", "60M", "0", "0"),
			expRes:    tests.NewResourceRequirements("25", "60M", "0", "0"),
			expState:  tests.NewResourceList("5", "35M"),
		},
	}
	for i, step := range steps {
		r := limiter.ApplyLimits("", step.requested)
		if helpers.CmpResourceRequirements(*r, *step.expRes) != 0 {
			t.Fatalf("step %d - ApplyLimits() results mismatch. \nWant: \n%v; \nGot: \n%v", i+1, step.expRes, r)
		}
		state := tests.NewResourceList(limiter.availCPU.String(), limiter.availMem.String())
		if helpers.CmpResourceList(*state, *step.expState) != 0 {
			t.Fatalf("step %d - limiter state results mismatch. \nWant: \n%v; \nGot: \n%v", i+1, step.expState, r)
		}
		// verify that ".MaxAllowed()" returns the same amount of "free"  resources as expected
		if helpers.CmpResourceList(*limiter.MaxAllowed(""), *step.expState) != 0 {
			t.Fatalf("step %d - limiter state results mismatch. \nWant: \n%v; \nGot: \n%v", i+1, step.expState, r)
		}
	}
}

func newGlobalPolicy(cpu, mem string) *konsumeratorv1.ResourcePolicy {
	return &konsumeratorv1.ResourcePolicy{
		GlobalPolicy: &konsumeratorv1.GlobalResourcePolicy{
			MaxAllowed: *tests.NewResourceList(cpu, mem),
		}}
}
