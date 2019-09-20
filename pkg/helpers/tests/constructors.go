package tests

import (
	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func NewResourceList(cpu, mem string) *corev1.ResourceList {
	return &corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse(cpu),
		corev1.ResourceMemory: resource.MustParse(mem),
	}
}

func NewContainerResourcePolicy(name, minCpu, minMem, maxCpu, maxMem string) konsumeratorv1alpha1.ContainerResourcePolicy {
	return konsumeratorv1alpha1.ContainerResourcePolicy{
		ContainerName: name,
		MinAllowed:    *NewResourceList(minCpu, minMem),
		MaxAllowed:    *NewResourceList(maxCpu, maxMem),
	}
}

func NewResourceRequirements(reqCpu, reqMem, limCpu, limMem string) *corev1.ResourceRequirements {
	return &corev1.ResourceRequirements{
		Requests: *NewResourceList(reqCpu, reqMem),
		Limits:   *NewResourceList(limCpu, limMem),
	}
}
