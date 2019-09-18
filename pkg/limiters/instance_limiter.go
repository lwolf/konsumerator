package limiters

import (
	"github.com/go-logr/logr"
	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type InstanceLimiter struct {
	policy   *konsumeratorv1alpha1.ResourcePolicy
	registry map[string]*konsumeratorv1alpha1.ContainerResourcePolicy
	log      logr.Logger
}

func NewInstanceLimiter(policy *konsumeratorv1alpha1.ResourcePolicy, log logr.Logger) *InstanceLimiter {
	registry := make(map[string]*konsumeratorv1alpha1.ContainerResourcePolicy, len(policy.ContainerPolicies))
	for _, cp := range policy.ContainerPolicies {
		registry[cp.ContainerName] = &cp
	}
	return &InstanceLimiter{
		policy:   policy,
		registry: registry,
		log:      log,
	}
}

func (il *InstanceLimiter) ApplyLimits(containerName string, resources *corev1.ResourceRequirements) *corev1.ResourceRequirements {
	limits, ok := il.registry[containerName]
	if !ok {
		return resources
	}
	cpuReq, cpuLimit := il.validateCpu(resources.Requests.Cpu(), resources.Limits.Cpu(), limits)
	memoryReq, memoryLimit := il.validateMemory(resources.Requests.Memory(), resources.Limits.Memory(), limits)
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
