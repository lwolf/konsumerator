package limiters

import (
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type GlobalLimiter struct {
	availCPU, availMem *resource.Quantity
	log                logr.Logger
}

func NewGlobalLimiter(limit, used *corev1.ResourceRequirements, log logr.Logger) (*GlobalLimiter, error) {
	if used == nil {
		return nil, fmt.Errorf("used resources can't be nil")
	}

	l := &GlobalLimiter{log: log}
	if limit == nil {
		// no limits were set
		return l, nil
	}

	if used.Requests.Cpu().Cmp(*limit.Requests.Cpu()) == 1 {
		return nil, fmt.Errorf("cpu limit %d is less than used %d",
			limit.Requests.Cpu().MilliValue(), used.Requests.Cpu().MilliValue())
	}
	if used.Requests.Memory().Cmp(*limit.Requests.Memory()) == 1 {
		return nil, fmt.Errorf("memory limit %d is less than used %d",
			limit.Requests.Memory().MilliValue(), used.Requests.Memory().MilliValue())
	}

	cpu := limit.Requests.Cpu()
	cpu.Sub(*used.Requests.Cpu())
	l.availCPU = cpu

	mem := limit.Requests.Memory()
	mem.Sub(*used.Requests.Memory())
	l.availMem = mem

	return l, nil
}

func (l *GlobalLimiter) ApplyLimits(_ string, resources *corev1.ResourceRequirements) *corev1.ResourceRequirements {
	if l.availMem == nil || l.availCPU == nil {
		// no limits were applied - returning as is
		return resources
	}

	if l.availCPU.IsZero() || l.availMem.IsZero() {
		l.log.V(1).Info(
			"global limiter exhausted",
			"cpuReq", resources.Requests.Cpu().MilliValue(),
			"cpuLimit", l.availCPU.MilliValue(),
			"memReq", resources.Requests.Memory().MilliValue(),
			"memLimit", l.availCPU.MilliValue(),
		)
		return nil
	}
	cpu := l.deductCPU(resources.Requests.Cpu())
	mem := l.deductMem(resources.Requests.Memory())
	return &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    *resource.NewMilliQuantity(cpu.MilliValue(), resource.DecimalSI),
			corev1.ResourceMemory: *resource.NewMilliQuantity(mem.MilliValue(), resource.DecimalSI),
		},
	}
}

func (l *GlobalLimiter) deductCPU(r *resource.Quantity) *resource.Quantity {
	cpu := limitQuantity(r, l.availCPU)
	l.availCPU.Sub(*cpu)
	return cpu
}

func (l *GlobalLimiter) deductMem(r *resource.Quantity) *resource.Quantity {
	mem := limitQuantity(r, l.availMem)
	l.availMem.Sub(*mem)
	return mem
}

func limitQuantity(a, b *resource.Quantity) *resource.Quantity {
	if a.Cmp(*b) == 1 {
		// if a is greater - return b as max available value resource
		return resource.NewMilliQuantity(b.MilliValue(), resource.DecimalSI)
	}
	// if a is less or equal - return a since we have enough resources
	return resource.NewMilliQuantity(a.MilliValue(), resource.DecimalSI)
}
