package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// Common ConditionTypes used by Cluster API objects.
const (
	PodProvisionedCondition ConditionType = "PodProvisioned"
)

const (
	// MachineFinalizer allows ReconcileKubevirtMachine to clean up resources associated with machine before
	// removing it from the apiserver.
	MachineFinalizer = "vnodemachine.infrastructure.cluster.x-k8s.io"
)

// VNodeMachineSpec defines the desired state of VNodeMachine.
type VNodeMachineSpec struct {
	// ProviderID TBD what to use for VNode
	// +optional
	ProviderID *string `json:"providerID,omitempty"`

	// PodTemplate contains the pod template to use for the machine.
	PodTemplate PodTemplate `json:"podTemplate,omitempty"`
}

// VNodeMachineStatus defines the observed state of VNodeMachine.
type VNodeMachineStatus struct {
	// Ready denotes that the machine is ready
	// +kubebuilder:default=false
	Ready bool `json:"ready"`

	// Addresses contains the associated addresses for the machine.
	// +optional
	Addresses []clusterv1.MachineAddress `json:"addresses,omitempty"`

	// Conditions defines current service state of the KubevirtMachine.
	// +optional
	Conditions Conditions `json:"conditions,omitempty"`

	// NodeUpdated denotes that the ProviderID is updated on Node of this KubevirtMachine
	// +optional
	NodeUpdated bool `json:"nodeupdated"`

	// FailureReason will be set in the event that there is a terminal problem
	// reconciling the Machine and will contain a succinct value suitable
	// for machine interpretation.
	//
	// This field should not be set for transitive errors that a controller
	// faces that are expected to be fixed automatically over
	// time (like service outages), but instead indicate that something is
	// fundamentally wrong with the Machine's spec or the configuration of
	// the controller, and that manual intervention is required. Examples
	// of terminal errors would be invalid combinations of settings in the
	// spec, values that are unsupported by the controller, or the
	// responsible controller itself being critically misconfigured.
	//
	// Any transient errors that occur during the reconciliation of Machines
	// can be added as events to the Machine object and/or logged in the
	// controller's output.
	// +optional
	FailureReason string `json:"failureReason,omitempty"`

	// FailureMessage will be set in the event that there is a terminal problem
	// reconciling the Machine and will contain a more verbose string suitable
	// for logging and human consumption.
	//
	// This field should not be set for transitive errors that a controller
	// faces that are expected to be fixed automatically over
	// time (like service outages), but instead indicate that something is
	// fundamentally wrong with the Machine's spec or the configuration of
	// the controller, and that manual intervention is required. Examples
	// of terminal errors would be invalid combinations of settings in the
	// spec, values that are unsupported by the controller, or the
	// responsible controller itself being critically misconfigured.
	//
	// Any transient errors that occur during the reconciliation of Machines
	// can be added as events to the Machine object and/or logged in the
	// controller's output.
	// +optional
	FailureMessage *string `json:"failureMessage,omitempty"`
}

type PodTemplate struct {
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Specification of the desired behavior of the pod.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Spec runtime.RawExtension `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
}

// +kubebuilder:resource:path=vnodemachines,scope=Namespaced,categories=cluster-api
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready",description="Is machine ready"

// VNodeMachine is the Schema for the vnodemachines API.
type VNodeMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VNodeMachineSpec   `json:"spec,omitempty"`
	Status VNodeMachineStatus `json:"status,omitempty"`
}

func (c *VNodeMachine) GetConditions() Conditions {
	return c.Status.Conditions
}

func (c *VNodeMachine) SetConditions(conditions Conditions) {
	c.Status.Conditions = conditions
}

// +kubebuilder:object:root=true

// VNodeMachineList contains a list of VNodeMachine.
type VNodeMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VNodeMachine `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VNodeMachine{}, &VNodeMachineList{})
}
