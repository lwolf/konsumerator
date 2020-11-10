package limiters

import (
	"github.com/go-logr/logr"
	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type InstanceLimiter struct {
	registry map[string]konsumeratorv1alpha1.ContainerResourcePolicy
	log      logr.Logger
}

func NewInstanceLimiter(policy *konsumeratorv1alpha1.ResourcePolicy, log logr.Logger) *InstanceLimiter {
	registry := make(map[string]konsumeratorv1alpha1.ContainerResourcePolicy, 0)
	if policy != nil {
		for i := range policy.ContainerPolicies {
			cp := policy.ContainerPolicies[i]
			registry[cp.ContainerName] = cp
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
	il.log.V(1).Info(
		"resource limiting results",
		"containerName", containerName,
		"cpuReq", cpuReq,
		"cpuLimit", cpuLimit,
		"memReq", memoryReq,
		"memLimit", memoryLimit,
	)

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

func (il *InstanceLimiter) validateCpu(request, limit *resource.Quantity, policy *konsumeratorv1alpha1.ContainerResourcePolicy) (int64, int64) {
	l := adjustQuantity(limit, policy.MinAllowed.Cpu(), policy.MaxAllowed.Cpu())
	r := adjustQuantity(request, policy.MinAllowed.Cpu(), l)
	return r.MilliValue(), l.MilliValue()
}

func (il *InstanceLimiter) validateMemory(request, limit *resource.Quantity, policy *konsumeratorv1alpha1.ContainerResourcePolicy) (int64, int64) {
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
