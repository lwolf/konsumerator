package limiters

import (
	"errors"
	"testing"

	tlog "github.com/go-logr/logr/testing"
	"github.com/lwolf/konsumerator/pkg/helpers"
	"github.com/lwolf/konsumerator/pkg/helpers/tests"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestNewGlobalLimiter(t *testing.T) {
	testCases := map[string]struct {
		limit, used *corev1.ResourceRequirements
		expErr      string
	}{
		"used CPU is higher than limit": {
			limit:  resourceRequirements("100", "100M"),
			used:   resourceRequirements("110", "50M"),
			expErr: "cpu limit 100000 is less than used 110000",
		},
		"used Mem is higher than limit": {
			limit:  resourceRequirements("100", "100M"),
			used:   resourceRequirements("50", "150M"),
			expErr: "memory limit 100000000000 is less than used 150000000000",
		},
		"used resources are equal with limit": {
			limit:  resourceRequirements("100", "100M"),
			used:   resourceRequirements("100", "100M"),
			expErr: "",
		},
		"used resources are nil": {
			limit:  resourceRequirements("100", "100M"),
			expErr: "used resources can't be nil",
		},
		"no limits": {
			used:   resourceRequirements("50", "50M"),
			expErr: "",
		},
	}
	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			_, err := NewGlobalLimiter(tc.limit, tc.used, tlog.NullLogger{})
			if err == nil {
				err = errors.New("")
			}
			if tc.expErr != err.Error() {
				t.Fatalf("expected to get %q; got %q", tc.expErr, err)
			}
		})
	}
}

func TestGlobalLimiter_ApplyLimits(t *testing.T) {
	testCases := map[string]struct {
		limit, used *corev1.ResourceRequirements
		requested   *corev1.ResourceRequirements
		expRes      *corev1.ResourceRequirements
	}{
		"request less than limits": {
			limit:     resourceRequirements("100", "100M"),
			used:      resourceRequirements("10", "10M"),
			requested: resourceRequirements("50", "50M"),
			expRes:    resourceRequirements("50", "50M"),
		},
		"request equal to limits": {
			limit:     resourceRequirements("100", "100M"),
			used:      resourceRequirements("10", "10M"),
			requested: resourceRequirements("90", "90M"),
			expRes:    resourceRequirements("90", "90M"),
		},
		"request more than limits": {
			limit:     resourceRequirements("100", "100M"),
			used:      resourceRequirements("10", "10M"),
			requested: resourceRequirements("110", "120M"),
			expRes:    resourceRequirements("90", "90M"),
		},
		"request when there are no limits": {
			limit:     nil,
			used:      resourceRequirements("10", "10M"),
			requested: resourceRequirements("1000", "1000M"),
			expRes:    resourceRequirements("1000", "1000M"),
		},
	}
	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			l, err := NewGlobalLimiter(tc.limit, tc.used, tlog.NullLogger{})
			if err != nil {
				t.Fatalf("unexpected err: %s", err)
			}
			r := l.ApplyLimits("", tc.requested)
			if helpers.CmpResourceRequirements(*r, *tc.expRes) != 0 {
				t.Errorf("ApplyLimits() results mismatch. \nWant: \n%v; \nGot: \n%v", tc.expRes, r)
			}
		})
	}
}

// TestGlobalLimiter_ApplyLimits2 tests single GlobalLimiter object
// changing it's state by sequential list of steps
func TestGlobalLimiter_ApplyLimits2(t *testing.T) {
	limit := tests.NewResourceRequirements("100", "100M", "", "")
	used := tests.NewResourceRequirements("10", "10M", "", "")
	limiter, err := NewGlobalLimiter(limit, used, tlog.NullLogger{})
	if err != nil {
		t.Fatalf("unexpected err: %s", err)
	}

	steps := []struct {
		requested *corev1.ResourceRequirements
		expRes    *corev1.ResourceRequirements
		expState  *corev1.ResourceRequirements
	}{
		{
			requested: resourceRequirements("5", "5M"),
			expRes:    resourceRequirements("5", "5M"),
			expState:  resourceRequirements("85", "85M"),
		},
		{
			requested: resourceRequirements("15", "20M"),
			expRes:    resourceRequirements("15", "20M"),
			expState:  resourceRequirements("70", "65M"),
		},
		{
			requested: resourceRequirements("80", "20M"),
			expRes:    resourceRequirements("70", "20M"),
			expState:  resourceRequirements("0", "45M"),
		},
		{
			requested: resourceRequirements("10", "20M"),
			expRes:    nil,
			expState:  resourceRequirements("0", "45M"),
		},
	}
	for i, step := range steps {
		r := limiter.ApplyLimits("", step.requested)
		if r != nil {
			if helpers.CmpResourceRequirements(*r, *step.expRes) != 0 {
				t.Fatalf("step %d - ApplyLimits() results mismatch. \nWant: \n%v; \nGot: \n%v", i, step.expRes, r)
			}
		}
		state := &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    *resource.NewMilliQuantity(limiter.availCPU.MilliValue(), resource.DecimalSI),
				corev1.ResourceMemory: *resource.NewMilliQuantity(limiter.availMem.MilliValue(), resource.DecimalSI),
			},
		}
		if helpers.CmpResourceRequirements(*state, *step.expState) != 0 {
			t.Fatalf("step %d - limiter state results mismatch. \nWant: \n%v; \nGot: \n%v", i, step.expState, r)
		}
	}
}

func resourceRequirements(reqCpu, reqMem string) *corev1.ResourceRequirements {
	return tests.NewResourceRequirements(reqCpu, reqMem, "", "")
}
