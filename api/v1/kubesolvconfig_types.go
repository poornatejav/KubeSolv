package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KubeSolvConfigSpec defines the desired state of KubeSolv
type KubeSolvConfigSpec struct {
	// Mode: "monitor" (passive) or "auto-heal" (active)
	// +kubebuilder:validation:Enum=monitor;auto-heal
	// +kubebuilder:default=monitor
	Mode string `json:"mode,omitempty"`

	// TargetNamespaces: List of namespaces to watch (e.g., ["default", "production"])
	// If empty, it watches ALL namespaces.
	TargetNamespaces []string `json:"targetNamespaces,omitempty"`
}

// KubeSolvConfigStatus defines the observed state
type KubeSolvConfigStatus struct {
	LastScanTime  metav1.Time `json:"lastScanTime,omitempty"`
	IncidentCount int         `json:"incidentCount"`
	Health        string      `json:"health"` // e.g., "Running", "Degraded"
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// KubeSolvConfig is the Schema for the kubesolvconfigs API
type KubeSolvConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KubeSolvConfigSpec   `json:"spec,omitempty"`
	Status KubeSolvConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KubeSolvConfigList contains a list of KubeSolvConfig
type KubeSolvConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubeSolvConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KubeSolvConfig{}, &KubeSolvConfigList{})
}
