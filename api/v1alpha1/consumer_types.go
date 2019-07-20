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

package v1alpha1

import (
	autoscalev1 "github.com/kubernetes/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type AutoscalerType string

const (
	AutoscalerTypePrometheus AutoscalerType = "prometheus"
	AutoscalerTypeVpa        AutoscalerType = "vpa"
	AutoscalerTypeNone       AutoscalerType = ""
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ConsumerSpec defines the desired state of Consumer
type ConsumerSpec struct {
	// Important: Run "make" to regenerate code after modifying this file
	NumPartitions *int32 `json:"numPartitions"` // Number of partitions
	Name          string `json:"name"`          // Name of the deployments to run
	Namespace     string `json:"namespace"`     // Namespace to run managed deployments
	// +optional
	Autoscaler *AutoscalerSpec `json:"autoscaler,omitempty"`

	// +optional
	PartitionEnvKey    string                         `json:"partitionEnvKey,omitempty"`
	DeploymentTemplate appsv1.DeploymentSpec          `json:"deploymentTemplate"`
	ResourcePolicy     *autoscalev1.PodResourcePolicy `json:"resourcePolicy"`
}

type AutoscalerSpec struct {
	Mode AutoscalerType `json:"mode"`
	// +optional
	Prometheus *PrometheusAutoscalerSpec `json:"prometheus,omitempty"`
}

type PrometheusAutoscalerSpec struct {
	// TODO: needs to be extended to support protocol,address,tls,etc...
	// for now just http://prometheus:9091/graph should work
	Address       []string         `json:"address"`
	MinSyncPeriod *metav1.Duration `json:"minSyncPeriod"`

	Offset      OffsetQuerySpec      `json:"offset"`
	Production  ProductionQuerySpec  `json:"production"`
	Consumption ConsumptionQuerySpec `json:"consumption"`

	RatePerCore             *int64            `json:"ratePerCore"`
	RamPerCore              resource.Quantity `json:"ramPerCore"`
	TolerableLag            *metav1.Duration  `json:"tolerableLag"`
	CriticalLag             *metav1.Duration  `json:"criticalLag"`
	PreferableCatchupPeriod *metav1.Duration  `json:"preferableCatchupPeriod"`
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
	Lagging *int32 `json:"lagging,omitempty"`
	// +optional
	Outdated *int32 `json:"outdated,omitempty"`
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
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.numPartitions",description="Number of replicas"
// +kubebuilder:printcolumn:name="Autoscaler",type="string",JSONPath=".spec.autoscaler.mode",description="Autoscaler in use"
// +kubebuilder:printcolumn:name="Lagging",type="integer",JSONPath=".status.lagging"
// +kubebuilder:printcolumn:name="Available",type="integer",JSONPath=".status.running"
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
