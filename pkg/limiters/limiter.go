package limiters

import (
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/resource"

	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

type ResourceLimiter interface {
	ApplyLimits(containerName string, resources *corev1.ResourceRequirements) *corev1.ResourceRequirements
}

type InstanceLimiter struct {
	policy *konsumeratorv1alpha1.ResourcePolicy
	log    logr.Logger
}

func NewInstanceLimiter(policy *konsumeratorv1alpha1.ResourcePolicy, log logr.Logger) *InstanceLimiter {
	return &InstanceLimiter{
		policy: policy,
		log:    log,
	}
}

func (il *InstanceLimiter) validateCpu(request, limit *resource.Quantity, policy *konsumeratorv1alpha1.ContainerResourcePolicy) (int64, int64) {
	l := adjustQuantity(limit, policy.MinAllowed.Cpu(), policy.MaxAllowed.Cpu())
	r := adjustQuantity(request, policy.MinAllowed.Cpu(), l)

	il.log.V(1).Info("CPU validation result", "outRequest", r, "outLimit", l)
	return r.MilliValue(), l.MilliValue()
}

func (il *InstanceLimiter) validateMemory(request, limit *resource.Quantity, policy *konsumeratorv1alpha1.ContainerResourcePolicy) (int64, int64) {
	l := adjustQuantity(limit, policy.MinAllowed.Memory(), policy.MaxAllowed.Memory())
	r := adjustQuantity(request, policy.MinAllowed.Memory(), l)

	il.log.V(1).Info("Memory validation result", "outRequest", r, "outLimit", l)
	return r.MilliValue(), l.MilliValue()
}
func (il *InstanceLimiter) ApplyLimits(containerName string, resources *corev1.ResourceRequirements) *corev1.ResourceRequirements {
	limits := il.containerResourcePolicy(containerName)
	if limits == nil {
		return resources
	}
	cpuReq, cpuLimit := il.validateCpu(resources.Requests.Cpu(), resources.Limits.Cpu(), limits)
	memoryReq, memoryLimit := il.validateMemory(resources.Requests.Memory(), resources.Limits.Memory(), limits)
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

func (il *InstanceLimiter) containerResourcePolicy(name string) *konsumeratorv1alpha1.ContainerResourcePolicy {
	for _, cp := range il.policy.ContainerPolicies {
		if cp.ContainerName == name {
			return &cp
		}
	}
	return nil
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
