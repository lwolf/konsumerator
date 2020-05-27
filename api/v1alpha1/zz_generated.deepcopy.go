// +build !ignore_autogenerated

/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// autogenerated by controller-gen object, do not modify manually

package v1alpha1

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AutoscalerSpec) DeepCopyInto(out *AutoscalerSpec) {
	*out = *in
	if in.PendingScaleUpDuration != nil {
		in, out := &in.PendingScaleUpDuration, &out.PendingScaleUpDuration
		*out = new(metav1.Duration)
		**out = **in
	}
	if in.PendingScaleDownDuration != nil {
		in, out := &in.PendingScaleDownDuration, &out.PendingScaleDownDuration
		*out = new(metav1.Duration)
		**out = **in
	}
	if in.Prometheus != nil {
		in, out := &in.Prometheus, &out.Prometheus
		*out = new(PrometheusAutoscalerSpec)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AutoscalerSpec.
func (in *AutoscalerSpec) DeepCopy() *AutoscalerSpec {
	if in == nil {
		return nil
	}
	out := new(AutoscalerSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Consumer) DeepCopyInto(out *Consumer) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Consumer.
func (in *Consumer) DeepCopy() *Consumer {
	if in == nil {
		return nil
	}
	out := new(Consumer)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Consumer) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ConsumerList) DeepCopyInto(out *ConsumerList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Consumer, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ConsumerList.
func (in *ConsumerList) DeepCopy() *ConsumerList {
	if in == nil {
		return nil
	}
	out := new(ConsumerList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ConsumerList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ConsumerSpec) DeepCopyInto(out *ConsumerSpec) {
	*out = *in
	if in.NumPartitions != nil {
		in, out := &in.NumPartitions, &out.NumPartitions
		*out = new(int32)
		**out = **in
	}
	if in.NumPartitionsPerInstance != nil {
		in, out := &in.NumPartitionsPerInstance, &out.NumPartitionsPerInstance
		*out = new(int32)
		**out = **in
	}
	if in.Autoscaler != nil {
		in, out := &in.Autoscaler, &out.Autoscaler
		*out = new(AutoscalerSpec)
		(*in).DeepCopyInto(*out)
	}
	in.DeploymentTemplate.DeepCopyInto(&out.DeploymentTemplate)
	if in.ResourcePolicy != nil {
		in, out := &in.ResourcePolicy, &out.ResourcePolicy
		*out = new(ResourcePolicy)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ConsumerSpec.
func (in *ConsumerSpec) DeepCopy() *ConsumerSpec {
	if in == nil {
		return nil
	}
	out := new(ConsumerSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ConsumerStatus) DeepCopyInto(out *ConsumerStatus) {
	*out = *in
	if in.ObservedGeneration != nil {
		in, out := &in.ObservedGeneration, &out.ObservedGeneration
		*out = new(int64)
		**out = **in
	}
	if in.Expected != nil {
		in, out := &in.Expected, &out.Expected
		*out = new(int32)
		**out = **in
	}
	if in.Running != nil {
		in, out := &in.Running, &out.Running
		*out = new(int32)
		**out = **in
	}
	if in.Paused != nil {
		in, out := &in.Paused, &out.Paused
		*out = new(int32)
		**out = **in
	}
	if in.Lagging != nil {
		in, out := &in.Lagging, &out.Lagging
		*out = new(int32)
		**out = **in
	}
	if in.Missing != nil {
		in, out := &in.Missing, &out.Missing
		*out = new(int32)
		**out = **in
	}
	if in.Outdated != nil {
		in, out := &in.Outdated, &out.Outdated
		*out = new(int32)
		**out = **in
	}
	if in.LastSyncTime != nil {
		in, out := &in.LastSyncTime, &out.LastSyncTime
		*out = new(metav1.Time)
		(*in).DeepCopyInto(*out)
	}
	if in.LastSyncState != nil {
		in, out := &in.LastSyncState, &out.LastSyncState
		*out = make(map[string]InstanceState, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ConsumerStatus.
func (in *ConsumerStatus) DeepCopy() *ConsumerStatus {
	if in == nil {
		return nil
	}
	out := new(ConsumerStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ConsumptionQuerySpec) DeepCopyInto(out *ConsumptionQuerySpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ConsumptionQuerySpec.
func (in *ConsumptionQuerySpec) DeepCopy() *ConsumptionQuerySpec {
	if in == nil {
		return nil
	}
	out := new(ConsumptionQuerySpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ContainerResourcePolicy) DeepCopyInto(out *ContainerResourcePolicy) {
	*out = *in
	if in.Mode != nil {
		in, out := &in.Mode, &out.Mode
		*out = new(ContainerScalingMode)
		**out = **in
	}
	if in.MinAllowed != nil {
		in, out := &in.MinAllowed, &out.MinAllowed
		*out = make(v1.ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
	if in.MaxAllowed != nil {
		in, out := &in.MaxAllowed, &out.MaxAllowed
		*out = make(v1.ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ContainerResourcePolicy.
func (in *ContainerResourcePolicy) DeepCopy() *ContainerResourcePolicy {
	if in == nil {
		return nil
	}
	out := new(ContainerResourcePolicy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GlobalResourcePolicy) DeepCopyInto(out *GlobalResourcePolicy) {
	*out = *in
	if in.MaxAllowed != nil {
		in, out := &in.MaxAllowed, &out.MaxAllowed
		*out = make(v1.ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GlobalResourcePolicy.
func (in *GlobalResourcePolicy) DeepCopy() *GlobalResourcePolicy {
	if in == nil {
		return nil
	}
	out := new(GlobalResourcePolicy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *InstanceState) DeepCopyInto(out *InstanceState) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new InstanceState.
func (in *InstanceState) DeepCopy() *InstanceState {
	if in == nil {
		return nil
	}
	out := new(InstanceState)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *OffsetQuerySpec) DeepCopyInto(out *OffsetQuerySpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new OffsetQuerySpec.
func (in *OffsetQuerySpec) DeepCopy() *OffsetQuerySpec {
	if in == nil {
		return nil
	}
	out := new(OffsetQuerySpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ProductionQuerySpec) DeepCopyInto(out *ProductionQuerySpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ProductionQuerySpec.
func (in *ProductionQuerySpec) DeepCopy() *ProductionQuerySpec {
	if in == nil {
		return nil
	}
	out := new(ProductionQuerySpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PrometheusAutoscalerSpec) DeepCopyInto(out *PrometheusAutoscalerSpec) {
	*out = *in
	if in.Address != nil {
		in, out := &in.Address, &out.Address
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.MinSyncPeriod != nil {
		in, out := &in.MinSyncPeriod, &out.MinSyncPeriod
		*out = new(metav1.Duration)
		**out = **in
	}
	out.Offset = in.Offset
	out.Production = in.Production
	out.Consumption = in.Consumption
	if in.RatePerCore != nil {
		in, out := &in.RatePerCore, &out.RatePerCore
		*out = new(int64)
		**out = **in
	}
	out.RamPerCore = in.RamPerCore.DeepCopy()
	if in.TolerableLag != nil {
		in, out := &in.TolerableLag, &out.TolerableLag
		*out = new(metav1.Duration)
		**out = **in
	}
	if in.CriticalLag != nil {
		in, out := &in.CriticalLag, &out.CriticalLag
		*out = new(metav1.Duration)
		**out = **in
	}
	if in.RecoveryTime != nil {
		in, out := &in.RecoveryTime, &out.RecoveryTime
		*out = new(metav1.Duration)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PrometheusAutoscalerSpec.
func (in *PrometheusAutoscalerSpec) DeepCopy() *PrometheusAutoscalerSpec {
	if in == nil {
		return nil
	}
	out := new(PrometheusAutoscalerSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ResourcePolicy) DeepCopyInto(out *ResourcePolicy) {
	*out = *in
	if in.GlobalPolicy != nil {
		in, out := &in.GlobalPolicy, &out.GlobalPolicy
		*out = new(GlobalResourcePolicy)
		(*in).DeepCopyInto(*out)
	}
	if in.ContainerPolicies != nil {
		in, out := &in.ContainerPolicies, &out.ContainerPolicies
		*out = make([]ContainerResourcePolicy, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ResourcePolicy.
func (in *ResourcePolicy) DeepCopy() *ResourcePolicy {
	if in == nil {
		return nil
	}
	out := new(ResourcePolicy)
	in.DeepCopyInto(out)
	return out
}
