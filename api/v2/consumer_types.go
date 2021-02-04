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

package v2

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type AutoscalerType string

// ContainerScalingMode controls whether autoscaler is enabled for a specific
// container.
type ContainerScalingMode string

const (
	AutoscalerTypePrometheus AutoscalerType = "prometheus"
	AutoscalerTypeVpa        AutoscalerType = "vpa"
	AutoscalerTypeNone       AutoscalerType = ""

	// ContainerScalingModeAuto means autoscaling is enabled for a container.
	ContainerScalingModeAuto ContainerScalingMode = "Auto"
	// ContainerScalingModeOff means autoscaling is disabled for a container.
	ContainerScalingModeOff ContainerScalingMode = "Off"
)

// ConsumerSpec defines the desired state of Consumer
type ConsumerSpec struct {
	// Important: Run "make" to regenerate code after modifying this file
	NumPartitions            *int32          `json:"numPartitions"`                      // Number of partitions
	NumPartitionsPerInstance *int32          `json:"numPartitionsPerInstance,omitempty"` // Number of partitions to assign to each consumer
	Name                     string          `json:"name"`                               // Name of the instance to run
	Namespace                string          `json:"namespace"`                          // Namespace to run managed instances
	Autoscaler               *AutoscalerSpec `json:"autoscaler"`                         // Auto-scaler configuration

	// +optional
	PartitionEnvKey    string                `json:"partitionEnvKey,omitempty"`
	DeploymentTemplate appsv1.DeploymentSpec `json:"deploymentTemplate"`
	ResourcePolicy     *ResourcePolicy       `json:"resourcePolicy"`
}

type ResourcePolicy struct {
	// +optional
	GlobalPolicy *GlobalResourcePolicy `json:"globalPolicy,omitempty"`
	// Per-container resource policies.
	// +optional
	// +patchMergeKey=containerName
	// +patchStrategy=merge
	ContainerPolicies []ContainerResourcePolicy `json:"containerPolicies,omitempty" patchStrategy:"merge" patchMergeKey:"containerName"`
}

type GlobalResourcePolicy struct {
	// Specifies the maximum amount of resources that could be allocated
	// to the entire application
	// +optional
	MaxAllowed corev1.ResourceList `json:"maxAllowed,omitempty"`
}

// ContainerResourcePolicy controls how autoscaler computes the recommended
// resources for a specific container.
type ContainerResourcePolicy struct {
	// Name of the container or DefaultContainerResourcePolicy, in which
	// case the policy is used by the containers that don't have their own
	// policy specified.
	ContainerName string `json:"containerName,omitempty"`
	// Whether autoscaler is enabled for the container. The default is "Auto".
	// +optional
	Mode *ContainerScalingMode `json:"mode,omitempty"`
	// Specifies the minimal amount of resources that will be recommended
	// for the container. The default is no minimum.
	// +optional
	MinAllowed corev1.ResourceList `json:"minAllowed,omitempty"`
	// Specifies the maximum amount of resources that will be recommended
	// for the container. The default is no maximum.
	// +optional
	MaxAllowed corev1.ResourceList `json:"maxAllowed,omitempty"`
}

type AutoscalerSpec struct {
	Mode AutoscalerType `json:"mode"`
	// +optional
	PendingScaleUpDuration *metav1.Duration `json:"pendingScaleUpDuration,omitempty"`
	// +optional
	PendingScaleDownDuration *metav1.Duration `json:"pendingScaleDownDuration,omitempty"`
	// +optional
	Prometheus *PrometheusAutoscalerSpec `json:"prometheus,omitempty"`
}

type PrometheusAutoscalerSpec struct {
	// TODO: needs to be extended to support protocol,address,tls,etc...
	// for now just http://prometheus:9091 should work
	Address       []string         `json:"address"`
	MinSyncPeriod *metav1.Duration `json:"minSyncPeriod"`

	Offset      OffsetQuerySpec      `json:"offset"`
	Production  ProductionQuerySpec  `json:"production"`
	Consumption ConsumptionQuerySpec `json:"consumption"`

	RatePerCore  *int64            `json:"ratePerCore"`
	RamPerCore   resource.Quantity `json:"ramPerCore"`
	TolerableLag *metav1.Duration  `json:"tolerableLag"`
	CriticalLag  *metav1.Duration  `json:"criticalLag"`
	RecoveryTime *metav1.Duration  `json:"recoveryTime"`
}

type OffsetQuerySpec struct {
	Query          string `json:"query"`
	PartitionLabel string `json:"partitionLabel"`
}

type ProductionQuerySpec struct {
	Query          string `json:"query"`
	PartitionLabel string `json:"partitionLabel"`
}

type ConsumptionQuerySpec struct {
	Query          string `json:"query"`
	PartitionLabel string `json:"partitionLabel"`
}

// ConsumerStatus defines the observed state of Consumer
type ConsumerStatus struct {
	// Important: Run "make" to regenerate code after modifying this file
	// +optional
	ObservedGeneration *int64 `json:"observedGeneration,omitempty"`
	// +optional
	Expected *int32 `json:"expected,omitempty"`
	// +optional
	Running *int32 `json:"running,omitempty"`
	// +optional
	Paused *int32 `json:"paused,omitempty"`
	// +optional
	Lagging *int32 `json:"lagging,omitempty"`
	// +optional
	Missing *int32 `json:"missing,omitempty"`
	// +optional
	Outdated *int32 `json:"outdated,omitempty"`
	// +optional
	Redundant *int32 `json:"redundant,omitempty"`
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`
	// +optional
	LastSyncState map[string]InstanceState `json:"lastSyncState,omitempty"`
}

type InstanceState struct {
	ProductionRate  int64 `json:"productionRate"`
	ConsumptionRate int64 `json:"consumptionRate"`
	MessagesBehind  int64 `json:"messageBehind"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:printcolumn:name="Expected",type="integer",JSONPath=".status.expected",description="Number of replicas supposed to run"
// +kubebuilder:printcolumn:name="Running",type="integer",JSONPath=".status.running"
// +kubebuilder:printcolumn:name="Paused",type="integer",JSONPath=".status.paused"
// +kubebuilder:printcolumn:name="Missing",type="integer",JSONPath=".status.missing"
// +kubebuilder:printcolumn:name="Lagging",type="integer",JSONPath=".status.lagging"
// +kubebuilder:printcolumn:name="Outdated",type="integer",JSONPath=".status.outdated"
// +kubebuilder:printcolumn:name="Autoscaler",type="string",JSONPath=".spec.autoscaler.mode",description="Autoscaler in use"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Consumer is the Schema for the consumers API
type Consumer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConsumerSpec   `json:"spec,omitempty"`
	Status ConsumerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ConsumerList contains a list of Consumer
type ConsumerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Consumer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Consumer{}, &ConsumerList{})
}
