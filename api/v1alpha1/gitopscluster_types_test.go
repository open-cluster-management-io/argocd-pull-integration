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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGitOpsClusterSpec(t *testing.T) {
	tests := []struct {
		name string
		spec GitOpsClusterSpec
	}{
		{
			name: "valid spec with placement",
			spec: GitOpsClusterSpec{
				PlacementRef: corev1.ObjectReference{
					Kind: "Placement",
					Name: "test-placement",
				},
				ArgoCDAgentAddon: ArgoCDAgentAddonSpec{
					PrincipalServerAddress: "argocd-server.argocd.svc",
					PrincipalServerPort:    "8080",
					Mode:                   "managed",
				},
			},
		},
		{
			name: "minimal spec",
			spec: GitOpsClusterSpec{
				PlacementRef: corev1.ObjectReference{
					Kind: "Placement",
					Name: "test-placement",
				},
				ArgoCDAgentAddon: ArgoCDAgentAddonSpec{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gitOpsCluster := &GitOpsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "argocd",
				},
				Spec: tt.spec,
			}

			// Verify the object can be created
			if gitOpsCluster.Name != "test" {
				t.Errorf("GitOpsCluster name = %v, want test", gitOpsCluster.Name)
			}
			if gitOpsCluster.Namespace != "argocd" {
				t.Errorf("GitOpsCluster namespace = %v, want argocd", gitOpsCluster.Namespace)
			}
		})
	}
}

func TestArgoCDAgentAddonSpec(t *testing.T) {
	tests := []struct {
		name string
		spec ArgoCDAgentAddonSpec
	}{
		{
			name: "full configuration",
			spec: ArgoCDAgentAddonSpec{
				PrincipalServerAddress: "argocd-server.argocd.svc",
				PrincipalServerPort:    "8080",
				Mode:                   "managed",
				OperatorImage:          "quay.io/operator:v1.0.0",
				AgentImage:             "quay.io/agent:v1.0.0",
			},
		},
		{
			name: "minimal configuration",
			spec: ArgoCDAgentAddonSpec{},
		},
		{
			name: "autonomous mode",
			spec: ArgoCDAgentAddonSpec{
				Mode: "autonomous",
			},
		},
		{
			name: "uninstall mode",
			spec: ArgoCDAgentAddonSpec{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify the spec can be created
			_ = tt.spec
		})
	}
}

func TestGitOpsClusterStatus(t *testing.T) {
	tests := []struct {
		name   string
		status GitOpsClusterStatus
	}{
		{
			name: "empty status",
			status: GitOpsClusterStatus{
				Conditions: []metav1.Condition{},
			},
		},
		{
			name: "status with conditions",
			status: GitOpsClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:               ConditionRBACReady,
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.Now(),
						Reason:             ReasonSuccess,
						Message:            "RBAC configured",
					},
					{
						Type:               ConditionServerDiscovered,
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.Now(),
						Reason:             ReasonSuccess,
						Message:            "Server discovered",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gitOpsCluster := &GitOpsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "argocd",
				},
				Status: tt.status,
			}

			// Verify status can be set
			if len(gitOpsCluster.Status.Conditions) != len(tt.status.Conditions) {
				t.Errorf("Status conditions count = %v, want %v",
					len(gitOpsCluster.Status.Conditions), len(tt.status.Conditions))
			}
		})
	}
}

func TestConditionConstants(t *testing.T) {
	// Test that condition type constants are defined
	conditions := []string{
		ConditionRBACReady,
		ConditionServerDiscovered,
		ConditionJWTSecretReady,
		ConditionCACertificateReady,
		ConditionPrincipalCertificateReady,
		ConditionResourceProxyCertificateReady,
		ConditionPlacementEvaluated,
		ConditionClustersImported,
		ConditionManifestWorkCreated,
		ConditionAddonConfigured,
	}

	for _, cond := range conditions {
		if cond == "" {
			t.Errorf("Condition constant is empty")
		}
	}
}

func TestReasonConstants(t *testing.T) {
	// Test that reason constants are defined
	reasons := []string{
		ReasonSuccess,
		ReasonFailed,
		ReasonInProgress,
		ReasonServerNotDiscovered,
		ReasonNoClusters,
	}

	for _, reason := range reasons {
		if reason == "" {
			t.Errorf("Reason constant is empty")
		}
	}
}
