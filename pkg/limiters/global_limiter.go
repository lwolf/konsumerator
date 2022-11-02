package limiters

import (
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/go-logr/logr"
	konsumeratorv1 "github.com/lwolf/konsumerator/api/v1"
)

type GlobalLimiter struct {
	availCPU *resource.Quantity
	availMem *resource.Quantity
	log      logr.Logger

	sync.Mutex
}

func NewGlobalLimiter(policy *konsumeratorv1.ResourcePolicy, used *corev1.ResourceList, log logr.Logger) *GlobalLimiter {
	l := &GlobalLimiter{log: log}
	if policy == nil || policy.GlobalPolicy == nil {
		// no limits were set
		return l
	}
	if used == nil {
		used = &corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("0"),
			corev1.ResourceMemory: resource.MustParse("0"),
		}
	}

	limit := policy.GlobalPolicy.MaxAllowed
	if used.Cpu().Cmp(*limit.Cpu()) == 1 || used.Memory().Cmp(*limit.Memory()) == 1 {
		l.log.Info(
			"Resource allocation is higher than global limit",
			"limit.CPU", limit.Cpu().MilliValue(),
			"limit.Memory", limit.Memory().MilliValue(),
			"used.CPU", used.Cpu().MilliValue(),
			"used.Memory", used.Memory().MilliValue(),
		)
	}

	cpu := limit.Cpu()
	cpu.Sub(*used.Cpu())
	l.availCPU = cpu

	mem := limit.Memory()
	mem.Sub(*used.Memory())
	l.availMem = mem

	return l
}

// nop
func (l *GlobalLimiter) MinAllowed(_ string) *corev1.ResourceList {
	return nil
}

func (l *GlobalLimiter) MaxAllowed(_ string) *corev1.ResourceList {
	return &corev1.ResourceList{
		corev1.ResourceCPU:    *resource.NewMilliQuantity(l.availCPU.MilliValue(), resource.DecimalSI),
		corev1.ResourceMemory: *resource.NewMilliQuantity(l.availMem.MilliValue(), resource.DecimalSI),
	}
}

// ApplyLimits returns amount resources that is possible to allocate, if pool is exhasted returns zeros
func (l *GlobalLimiter) ApplyLimits(_ string, resources *corev1.ResourceRequirements) *corev1.ResourceRequirements {
	if l.availMem == nil || l.availCPU == nil {
		// no limits were applied - returning as is
		return resources
	}
	l.Lock()
	defer l.Unlock()

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
