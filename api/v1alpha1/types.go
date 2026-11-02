// Package v1alpha1 contains the NodeInventory API.
// +kubebuilder:object:generate=true
// +groupName=infra.fai.devops
package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var GroupVersion = schema.GroupVersion{Group: "infra.fai.devops", Version: "v1alpha1"}
var SchemeBuilder = runtime.NewSchemeBuilder(func(s *runtime.Scheme) error {
	s.AddKnownTypes(GroupVersion, &NodeInventory{}, &NodeInventoryList{}, &NodeRepair{}, &NodeRepairList{})
	metav1.AddToGroupVersion(s, GroupVersion)
	return nil
})
var AddToScheme = SchemeBuilder.AddToScheme

type NodeRef struct { // UID identifies the approved Node incarnation.
	// +kubebuilder:validation:MinLength=1
	UID string `json:"uid"`
}
type NodeInfo struct {
	// +kubebuilder:validation:MinLength=1
	OperatingSystem string `json:"operatingSystem"`
	// +kubebuilder:validation:MinLength=1
	Architecture string `json:"architecture"`
	// +kubebuilder:validation:MinLength=1
	KernelVersion string `json:"kernelVersion"`
}

type Snapshot struct {
	NodeInfo NodeInfo `json:"nodeInfo"`
	// Capacity includes cpu, memory, and explicitly approved GPU resource names.
	Capacity corev1.ResourceList `json:"capacity"`
}

// +kubebuilder:validation:XValidation:rule="'cpu' in self.capacity && 'memory' in self.capacity",message="cpu and memory capacities are required"
type Baseline struct {
	CapturedAt metav1.Time `json:"capturedAt"`
	Snapshot   `json:",inline"`
}
type NodeInventorySpec struct {
	NodeRef  NodeRef  `json:"nodeRef"`
	Baseline Baseline `json:"baseline"`
}
type Observation struct {
	NodeUID         string      `json:"nodeUID"`
	ResourceVersion string      `json:"resourceVersion"`
	ObservedAt      metav1.Time `json:"observedAt"`
	Snapshot        `json:",inline"`
}
type NodeInventoryStatus struct {
	Observed   *Observation       `json:"observed,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=ni
// +kubebuilder:printcolumn:name="Matched",type=string,JSONPath=`.status.conditions[?(@.type=="BaselineMatched")].status`
// +kubebuilder:printcolumn:name="Observed",type=date,JSONPath=`.status.observed.observedAt`
type NodeInventory struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              NodeInventorySpec   `json:"spec"`
	Status            NodeInventoryStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type NodeInventoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeInventory `json:"items"`
}
