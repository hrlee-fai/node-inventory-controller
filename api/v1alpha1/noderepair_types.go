package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// RepairNodeRef identifies a specific Node incarnation.
type RepairNodeRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	UID string `json:"uid"`
}
type NodeRepairSpec struct {
	NodeRef RepairNodeRef `json:"nodeRef"`
	// +kubebuilder:validation:Enum=CordonDrain
	Operation string `json:"operation"`
	// Only observation is implemented in this version.
	// +kubebuilder:validation:Enum=true
	// +kubebuilder:default=true
	DryRun bool `json:"dryRun"`
	// +kubebuilder:validation:MinLength=1
	Reason string `json:"reason"`
}
type RepairPDB struct {
	Name               string `json:"name"`
	DisruptionsAllowed int32  `json:"disruptionsAllowed"`
	ObservedGeneration int64  `json:"observedGeneration"`
	Generation         int64  `json:"generation"`
}
type RepairPod struct {
	Namespace      string      `json:"namespace"`
	Name           string      `json:"name"`
	UID            string      `json:"uid"`
	Classification string      `json:"classification"`
	Reason         string      `json:"reason,omitempty"`
	PDBs           []RepairPDB `json:"pdbs,omitempty"`
	Warnings       []string    `json:"warnings,omitempty"`
}
type RepairPreview struct {
	ObservedAt          metav1.Time `json:"observedAt"`
	NodeUID             string      `json:"nodeUID"`
	NodeResourceVersion string      `json:"nodeResourceVersion"`
	Unschedulable       bool        `json:"unschedulable"`
	Pods                []RepairPod `json:"pods,omitempty"`
}
type NodeRepairStatus struct {
	Phase      string             `json:"phase,omitempty"`
	Preview    *RepairPreview     `json:"preview,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=nr
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=`.spec.nodeRef.name`
// +kubebuilder:printcolumn:name="Operation",type=string,JSONPath=`.spec.operation`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type NodeRepair struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              NodeRepairSpec   `json:"spec"`
	Status            NodeRepairStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type NodeRepairList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeRepair `json:"items"`
}
