package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VNodeMachineTemplateSpec defines the desired state of VNodeMachineTemplate.
type VNodeMachineTemplateSpec struct {
	Template VNodeMachineTemplateResource `json:"template"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=vnodemachinetemplates,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion

// VNodeMachineTemplate is the Schema for the vnodemachinetemplates API.
type VNodeMachineTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec VNodeMachineTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// VNodeMachineTemplateList contains a list of VNodeMachineTemplate.
type VNodeMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VNodeMachineTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VNodeMachineTemplate{}, &VNodeMachineTemplateList{})
}

// VNodeMachineTemplateResource describes the data needed to create a VNodeMachine from a template.
type VNodeMachineTemplateResource struct {
	// Spec is the specification of the desired behavior of the machine.
	Spec VNodeMachineSpec `json:"spec"`
}
