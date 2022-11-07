package limiters

import (
	"fmt"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	konsumeratorv1 "github.com/lwolf/konsumerator/api/v1"
)

type InstanceLimiter struct {
	registry map[string]konsumeratorv1.ContainerResourcePolicy
	log      logr.Logger
}

func forceValidPolicy(p konsumeratorv1.ContainerResourcePolicy) (*konsumeratorv1.ContainerResourcePolicy, bool) {
	policy := konsumeratorv1.ContainerResourcePolicy{
		ContainerName: p.ContainerName,
		Mode:          p.Mode,
	}
	var swapped bool
	var minCPU, maxCPU, minMemory, maxMemory *resource.Quantity
	if p.MaxAllowed.Cpu().Cmp(*p.MinAllowed.Cpu()) == -1 {
		swapped = true
		maxCPU = p.MinAllowed.Cpu()
		minCPU = p.MaxAllowed.Cpu()
	} else {
		minCPU = p.MinAllowed.Cpu()
		maxCPU = p.MaxAllowed.Cpu()
	}
	if p.MaxAllowed.Memory().Cmp(*p.MinAllowed.Memory()) == -1 {
		swapped = true
		maxMemory = p.MinAllowed.Memory()
		minMemory = p.MaxAllowed.Memory()
	} else {
		minMemory = p.MinAllowed.Memory()
		maxMemory = p.MaxAllowed.Memory()
	}
	policy.MinAllowed = corev1.ResourceList{
		corev1.ResourceCPU:    *minCPU,
		corev1.ResourceMemory: *minMemory,
	}
	policy.MaxAllowed = corev1.ResourceList{
		corev1.ResourceCPU:    *maxCPU,
		corev1.ResourceMemory: *maxMemory,
	}
	return &policy, swapped
}

func NewInstanceLimiter(policy *konsumeratorv1.ResourcePolicy, log logr.Logger) *InstanceLimiter {
	registry := make(map[string]konsumeratorv1.ContainerResourcePolicy, 0)
	if policy != nil {
		for i := range policy.ContainerPolicies {
			cp, swapped := forceValidPolicy(policy.ContainerPolicies[i])
			if swapped {
				log.Error(fmt.Errorf("invalid container policy: Min > Max"), "Force swapping min and max! Please fix!")
			}
			registry[cp.ContainerName] = *cp
		}
	}
	return &InstanceLimiter{
		registry: registry,
		log:      log,
	}
}

func (il *InstanceLimiter) MinAllowed(containerName string) *corev1.ResourceList {
	policy, ok := il.registry[containerName]
	if !ok {
		return nil
	}
	return &policy.MinAllowed
}

func (il *InstanceLimiter) MaxAllowed(containerName string) *corev1.ResourceList {
	policy, ok := il.registry[containerName]
	if !ok {
		return nil
	}
	return &policy.MaxAllowed
}

func (il *InstanceLimiter) ApplyLimits(containerName string, resources *corev1.ResourceRequirements) *corev1.ResourceRequirements {
	limits, ok := il.registry[containerName]
	if !ok {
		return resources
	}
	cpuReq, cpuLimit := il.validateCpu(resources.Requests.Cpu(), resources.Limits.Cpu(), &limits)
	memoryReq, memoryLimit := il.validateMemory(resources.Requests.Memory(), resources.Limits.Memory(), &limits)
	return &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    *resource.NewMilliQuantity(cpuReq, resource.DecimalSI),
			corev1.ResourceMemory: *resource.NewMilliQuantity(memoryReq, resource.DecimalSI),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    *resource.NewMilliQuantity(cpuLimit, resource.DecimalSI),
			corev1.ResourceMemory: *resource.NewMilliQuantity(memoryLimit, resource.DecimalSI),
		},
	}
}

func (il *InstanceLimiter) validateCpu(request, limit *resource.Quantity, policy *konsumeratorv1.ContainerResourcePolicy) (int64, int64) {
	l := adjustQuantity(limit, policy.MinAllowed.Cpu(), policy.MaxAllowed.Cpu())
	r := adjustQuantity(request, policy.MinAllowed.Cpu(), l)
	return r.MilliValue(), l.MilliValue()
}

func (il *InstanceLimiter) validateMemory(request, limit *resource.Quantity, policy *konsumeratorv1.ContainerResourcePolicy) (int64, int64) {
	l := adjustQuantity(limit, policy.MinAllowed.Memory(), policy.MaxAllowed.Memory())
	r := adjustQuantity(request, policy.MinAllowed.Memory(), l)
	return r.MilliValue(), l.MilliValue()
}

func adjustQuantity(resource, min, max *resource.Quantity) *resource.Quantity {
	switch {
	case resource.MilliValue() > max.MilliValue():
		resource = max
	case resource.MilliValue() < min.MilliValue():
		resource = min
	default:
	}
	return resource
}
