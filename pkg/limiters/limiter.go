package limiters

import (
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/resource"

	autoscalev1 "github.com/kubernetes/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	corev1 "k8s.io/api/core/v1"
)

type ResourceLimiter interface {
	ApplyLimits(containerName string, resources *corev1.ResourceRequirements) *corev1.ResourceRequirements
}

type InstanceLimiter struct {
	policy *autoscalev1.PodResourcePolicy
	log    logr.Logger
}

func NewInstanceLimiter(policy *autoscalev1.PodResourcePolicy, log logr.Logger) *InstanceLimiter {
	return &InstanceLimiter{
		policy: policy,
		log:    log,
	}
}

func (il *InstanceLimiter) validateCpu(request int64, limit int64, policy *autoscalev1.ContainerResourcePolicy) (int64, int64) {
	r := request
	l := limit
	if request < policy.MinAllowed.Cpu().MilliValue() {
		r = policy.MinAllowed.Cpu().MilliValue()
	}
	if limit < policy.MinAllowed.Cpu().MilliValue() {
		l = policy.MinAllowed.Cpu().MilliValue()
	}
	if limit > policy.MaxAllowed.Cpu().MilliValue() {
		l = policy.MaxAllowed.Cpu().MilliValue()
	}
	if request > l {
		r = l
	}
	il.log.V(1).Info("CPU validation result", "outRequest", r, "outLimit", l)
	return r, l
}

func (il *InstanceLimiter) validateMemory(request int64, limit int64, policy *autoscalev1.ContainerResourcePolicy) (int64, int64) {
	r := request
	l := limit
	if request < policy.MinAllowed.Memory().MilliValue() {
		r = policy.MinAllowed.Memory().MilliValue()
	}
	if limit < policy.MinAllowed.Memory().MilliValue() {
		l = policy.MinAllowed.Memory().MilliValue()
	}
	if limit > policy.MaxAllowed.Memory().MilliValue() {
		l = policy.MaxAllowed.Memory().MilliValue()
	}
	if request > l {
		r = l
	}
	il.log.V(1).Info("Memory validation result", "outRequest", r, "outLimit", l)
	return r, l
}

func (il *InstanceLimiter) ApplyLimits(containerName string, resources *corev1.ResourceRequirements) *corev1.ResourceRequirements {
	limits := il.containerResourcePolicy(containerName)
	if limits == nil {
		return resources
	}
	cpuReq, cpuLimit := il.validateCpu(resources.Requests.Cpu().MilliValue(), resources.Limits.Cpu().MilliValue(), limits)
	memoryReq, memoryLimit := il.validateMemory(resources.Requests.Memory().MilliValue(), resources.Requests.Memory().MilliValue(), limits)
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

func (il *InstanceLimiter) containerResourcePolicy(name string) *autoscalev1.ContainerResourcePolicy {
	for _, cp := range il.policy.ContainerPolicies {
		if cp.ContainerName == name {
			return &cp
		}
	}
	return nil
}
