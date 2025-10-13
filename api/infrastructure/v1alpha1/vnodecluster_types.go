/*
Copyright 2022.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VNodeClusterSpec defines the desired state of VCluster
type VNodeClusterSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// ControlPlaneEndpoint represents the endpoint used to communicate with the control plane.
	// +optional
	ControlPlaneEndpoint clusterv1.APIEndpoint `json:"controlPlaneEndpoint"`
}

// VNodeClusterStatus defines the observed state of VCluster
type VNodeClusterStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	// Ready defines if the virtual cluster control plane is ready.
	// +optional
	// +kubebuilder:default=false
	Ready bool `json:"ready"`

	// Reason describes the reason in machine readable form why the cluster is in the current
	// phase
	// +optional
	Reason string `json:"reason,omitempty"`

	// Message describes the reason in human readable form why the cluster is in the currrent
	// phase
	// +optional
	Message string `json:"message,omitempty"`

	// Conditions holds several conditions the vcluster might be in
	// +optional
	Conditions Conditions `json:"conditions,omitempty"`

	// ObservedGeneration is the latest generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ExternalManagedControlPlane is required by Cluster API to indicate that the control plane
	// is externally managed.
	// +optional
	ExternalManagedControlPlane *bool `json:"externalManagedControlPlane,omitempty"`
}

// GetConditions returns the set of conditions for this object.
func (v *VNodeCluster) GetConditions() Conditions {
	return v.Status.Conditions
}

// SetConditions sets the conditions on this object.
func (v *VNodeCluster) SetConditions(conditions Conditions) {
	v.Status.Conditions = conditions
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
//+kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// VNodeCluster is the Schema for the vnodeclusters API
type VNodeCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VNodeClusterSpec   `json:"spec,omitempty"`
	Status VNodeClusterStatus `json:"status,omitempty"`
}

func (v *VNodeCluster) GetSpec() *VNodeClusterSpec {
	return &v.Spec
}

func (v *VNodeCluster) GetStatus() *VNodeClusterStatus {
	return &v.Status
}

//+kubebuilder:object:root=true

// VNodeClusterList contains a list of VNodeCluster
type VNodeClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VNodeCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VNodeCluster{}, &VNodeClusterList{})
}
