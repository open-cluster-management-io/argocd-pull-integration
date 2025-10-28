/*
Copyright 2025 Open Cluster Management.

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GitOpsClusterSpec defines the desired state of GitOpsCluster
type GitOpsClusterSpec struct {
	// PlacementRef references a Placement resource to select managed clusters
	// +kubebuilder:validation:Required
	PlacementRef corev1.ObjectReference `json:"placementRef"`

	// ArgoCDAgentAddon defines the configuration for the ArgoCD agent addon
	// +kubebuilder:validation:Required
	ArgoCDAgentAddon ArgoCDAgentAddonSpec `json:"argoCDAgentAddon"`
}

// ArgoCDAgentAddonSpec defines the configuration for the ArgoCD agent addon
type ArgoCDAgentAddonSpec struct {
	// PrincipalServerAddress is the address of the ArgoCD principal server
	// +optional
	PrincipalServerAddress string `json:"principalServerAddress,omitempty"`

	// PrincipalServerPort is the port of the ArgoCD principal server
	// +optional
	PrincipalServerPort string `json:"principalServerPort,omitempty"`

	// Mode defines the agent mode (managed or autonomous)
	// +optional
	// +kubebuilder:default=managed
	Mode string `json:"mode,omitempty"`

	// OperatorImage is the ArgoCD operator image to use
	// +optional
	OperatorImage string `json:"operatorImage,omitempty"`

	// AgentImage is the ArgoCD agent image to use
	// +optional
	AgentImage string `json:"agentImage,omitempty"`

	// Uninstall indicates whether to uninstall the addon
	// +optional
	Uninstall bool `json:"uninstall,omitempty"`
}

// GitOpsClusterStatus defines the observed state of GitOpsCluster
type GitOpsClusterStatus struct {
	// Conditions represent the latest available observations of the GitOpsCluster's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// Condition types for GitOpsCluster
const (
	// ConditionRBACReady indicates that RBAC for addon-manager is ready
	ConditionRBACReady = "RBACReady"

	// ConditionServerDiscovered indicates that server address and port have been discovered
	ConditionServerDiscovered = "ServerDiscovered"

	// ConditionJWTSecretReady indicates that JWT secret is created
	ConditionJWTSecretReady = "JWTSecretReady"

	// ConditionCACertificateReady indicates that CA certificate is ready
	ConditionCACertificateReady = "CACertificateReady"

	// ConditionPrincipalCertificateReady indicates that principal certificate is generated
	ConditionPrincipalCertificateReady = "PrincipalCertificateReady"

	// ConditionResourceProxyCertificateReady indicates that resource proxy certificate is generated
	ConditionResourceProxyCertificateReady = "ResourceProxyCertificateReady"

	// ConditionPlacementEvaluated indicates that placement has been evaluated
	ConditionPlacementEvaluated = "PlacementEvaluated"

	// ConditionClustersImported indicates that managed clusters have been imported
	ConditionClustersImported = "ClustersImported"

	// ConditionManifestWorkCreated indicates that ManifestWork for CA has been created
	ConditionManifestWorkCreated = "ManifestWorkCreated"

	// ConditionAddonConfigured indicates that addon config and ManagedClusterAddon are configured
	ConditionAddonConfigured = "AddonConfigured"
)

// Condition reasons
const (
	ReasonSuccess             = "Success"
	ReasonFailed              = "Failed"
	ReasonInProgress          = "InProgress"
	ReasonServerNotDiscovered = "ServerNotDiscovered"
	ReasonNoClusters          = "NoClusters"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=gitopscluster;goc
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:storageversion

// GitOpsCluster is the Schema for the gitopsclusters API
type GitOpsCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GitOpsClusterSpec   `json:"spec,omitempty"`
	Status GitOpsClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GitOpsClusterList contains a list of GitOpsCluster
type GitOpsClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GitOpsCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GitOpsCluster{}, &GitOpsClusterList{})
}
