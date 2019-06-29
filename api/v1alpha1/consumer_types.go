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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ConsumerSpec defines the desired state of Consumer
type ConsumerSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Number of partitions
	NumPartitions *int32 `json:"numPartitions"`

	Name string `json:"name"`
	// Labels    []labels.Labels `json:"labels"`
	Namespace string `json:"namespace"`

	AllowedLagSeconds       *int64 `json:"allowedLagSeconds"`
	PrometheusLagMetricName string `json:"prometheusLagMetricName"`
	// TODO: needs to be extended to support protocol,address,tls,etc...
	// for now just http://prometheus:9091/graph should work
	PrometheusAddress string `json:"prometheusAddress"`

	// +optional
	ResourcesMaximum corev1.ResourceRequirements `json:"resourcesMaximum,omitempty"`

	DeploymentTemplate appsv1.DeploymentSpec `json:"deploymentTemplate"`
}

// ConsumerStatus defines the observed state of Consumer
type ConsumerStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// A list of pointers to currently running deployments.

	// +optional
	Running *int32 `json:"running,omitempty"`
	// +optional
	Lagging *int32 `json:"lagging,omitempty"`
	// +optional
	Actual *int32 `json:"actual,omitempty"`
	// +optional
	Missing *int32 `json:"missing,omitempty"`
	// +optional
	LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`
	// +optional
	Active []ObjectStatus `json:"active,omitempty"`
}

type ObjectStatus struct {
	Partition *int32                 `json:"partition"`
	Lag       *int64                 `json:"lag"`
	Ref       corev1.ObjectReference `json:"ref"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

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
