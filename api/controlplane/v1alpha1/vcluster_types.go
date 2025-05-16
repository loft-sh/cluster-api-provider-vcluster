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
	infrastructurev1alpha1 "github.com/loft-sh/cluster-api-provider-vcluster/api/infrastructure/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetConditions returns the set of conditions for this object.
func (v *VCluster) GetConditions() infrastructurev1alpha1.Conditions {
	return v.Status.Conditions
}

// SetConditions sets the conditions on this object.
func (v *VCluster) SetConditions(conditions infrastructurev1alpha1.Conditions) {
	v.Status.Conditions = conditions
}

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

	Spec   infrastructurev1alpha1.VClusterSpec   `json:"spec,omitempty"`
	Status infrastructurev1alpha1.VClusterStatus `json:"status,omitempty"`
}

func (v *VCluster) GetSpec() *infrastructurev1alpha1.VClusterSpec {
	return &v.Spec
}

func (v *VCluster) GetStatus() *infrastructurev1alpha1.VClusterStatus {
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
