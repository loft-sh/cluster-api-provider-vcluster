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
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VClusterSpec defines the desired state of VCluster
type VClusterSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// ControlPlaneEndpoint represents the endpoint used to communicate with the control plane.
	// +optional
	ControlPlaneEndpoint clusterv1beta1.APIEndpoint `json:"controlPlaneEndpoint"`

	// The helm release configuration for the virtual cluster. This is optional, but
	// when filled, specified chart will be deployed.
	// +optional
	HelmRelease *VirtualClusterHelmRelease `json:"helmRelease,omitempty"`
}

// VClusterStatus defines the observed state of VCluster
type VClusterStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	// Ready defines if the virtual cluster control plane is ready.
	// +optional
	// +kubebuilder:default=false
	Ready bool `json:"ready"`

	// Initialized defines if the virtual cluster control plane was initialized.
	// +optional
	// +kubebuilder:default=false
	Initialized bool `json:"initialized"`

	// Phase describes the current phase the virtual cluster is in
	// +optional
	Phase VirtualClusterPhase `json:"phase,omitempty"`

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
func (v *VCluster) GetConditions() Conditions {
	return v.Status.Conditions
}

// SetConditions sets the conditions on this object.
func (v *VCluster) SetConditions(conditions Conditions) {
	v.Status.Conditions = conditions
}

type VirtualClusterHelmRelease struct {
	// infos about what chart to deploy
	// +optional
	Chart VirtualClusterHelmChart `json:"chart,omitempty"`

	// the values for the given chart
	// +optional
	Values string `json:"values,omitempty"`

	// the values for the given chart
	// +optional
	ValuesObject *runtime.RawExtension `json:"valuesObject,omitempty"`
}

type VirtualClusterHelmChart struct {
	// the name of the helm chart
	// +optional
	Name string `json:"name,omitempty"`

	// the repo of the helm chart
	// +optional
	Repo string `json:"repo,omitempty"`

	// the version of the helm chart to use
	// +optional
	Version string `json:"version,omitempty"`
}

// VirtualClusterPhase describes the phase of a virtual cluster
// +kubebuilder:validation:Enum="";Pending;Deployed;Failed
type VirtualClusterPhase string

const (
	// VirtualClusterUnknown represents an unknown phase
	VirtualClusterUnknown VirtualClusterPhase = ""

	// VirtualClusterPending indicates the cluster is being created
	VirtualClusterPending VirtualClusterPhase = "Pending"

	// VirtualClusterDeployed indicates the cluster is fully deployed and operational
	VirtualClusterDeployed VirtualClusterPhase = "Deployed"

	// VirtualClusterFailed indicates the cluster deployment has failed
	VirtualClusterFailed VirtualClusterPhase = "Failed"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Version",type="string",JSONPath=".spec.helmRelease.chart.version"
//+kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
//+kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
//+kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// VCluster is the Schema for the vclusters API
type VCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VClusterSpec   `json:"spec,omitempty"`
	Status VClusterStatus `json:"status,omitempty"`
}

func (v *VCluster) GetSpec() *VClusterSpec {
	return &v.Spec
}

func (v *VCluster) GetStatus() *VClusterStatus {
	return &v.Status
}

//+kubebuilder:object:root=true

// VClusterList contains a list of VCluster
type VClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VCluster{}, &VClusterList{})
}
