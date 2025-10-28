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

package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	appsv1alpha1 "open-cluster-management.io/argocd-pull-integration/api/v1alpha1"
)

func TestJoinClusterNames(t *testing.T) {
	tests := []struct {
		name     string
		clusters []string
		want     string
	}{
		{
			name:     "empty list",
			clusters: []string{},
			want:     "",
		},
		{
			name:     "single cluster",
			clusters: []string{"cluster1"},
			want:     "[cluster1]",
		},
		{
			name:     "two clusters",
			clusters: []string{"cluster1", "cluster2"},
			want:     "[cluster1 cluster2]",
		},
		{
			name:     "three clusters",
			clusters: []string{"cluster1", "cluster2", "cluster3"},
			want:     "[cluster1 cluster2 cluster3]",
		},
		{
			name:     "more than three clusters",
			clusters: []string{"cluster1", "cluster2", "cluster3", "cluster4", "cluster5"},
			want:     "[cluster1 cluster2 cluster3]... (5 total)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joinClusterNames(tt.clusters)
			if got != tt.want {
				t.Errorf("joinClusterNames() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildAddonVariables(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = appsv1alpha1.AddToScheme(s)

	tests := []struct {
		name          string
		gitOpsCluster *appsv1alpha1.GitOpsCluster
		serverAddress string
		serverPort    string
		wantVars      map[string]string
	}{
		{
			name: "basic configuration",
			gitOpsCluster: &appsv1alpha1.GitOpsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "argocd",
				},
				Spec: appsv1alpha1.GitOpsClusterSpec{
					ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{
						Mode: "managed",
					},
				},
			},
			serverAddress: "argocd-server.argocd.svc",
			serverPort:    "8080",
			wantVars: map[string]string{
				"ARGOCD_AGENT_SERVER_ADDRESS": "argocd-server.argocd.svc",
				"ARGOCD_AGENT_SERVER_PORT":    "8080",
				"ARGOCD_AGENT_MODE":           "managed",
				"ARGOCD_AGENT_UNINSTALL":      "false",
			},
		},
		{
			name: "with custom images",
			gitOpsCluster: &appsv1alpha1.GitOpsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "argocd",
				},
				Spec: appsv1alpha1.GitOpsClusterSpec{
					ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{
						Mode:          "autonomous",
						OperatorImage: "quay.io/operator:v1.0.0",
						AgentImage:    "quay.io/agent:v2.0.0",
					},
				},
			},
			serverAddress: "argocd-server.argocd.svc",
			serverPort:    "8080",
			wantVars: map[string]string{
				"ARGOCD_AGENT_SERVER_ADDRESS": "argocd-server.argocd.svc",
				"ARGOCD_AGENT_SERVER_PORT":    "8080",
				"ARGOCD_AGENT_MODE":           "autonomous",
				"ARGOCD_OPERATOR_IMAGE":       "quay.io/operator:v1.0.0",
				"ARGOCD_AGENT_IMAGE":          "quay.io/agent:v2.0.0",
				"ARGOCD_AGENT_UNINSTALL":      "false",
			},
		},
		{
			name: "with uninstall flag",
			gitOpsCluster: &appsv1alpha1.GitOpsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "argocd",
				},
				Spec: appsv1alpha1.GitOpsClusterSpec{
					ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{
						Uninstall: true,
					},
				},
			},
			serverAddress: "argocd-server.argocd.svc",
			serverPort:    "8080",
			wantVars: map[string]string{
				"ARGOCD_AGENT_SERVER_ADDRESS": "argocd-server.argocd.svc",
				"ARGOCD_AGENT_SERVER_PORT":    "8080",
				"ARGOCD_AGENT_UNINSTALL":      "true",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).Build(),
				Scheme: s,
			}

			got := r.buildAddonVariables(tt.gitOpsCluster, tt.serverAddress, tt.serverPort)

			if len(got) != len(tt.wantVars) {
				t.Errorf("buildAddonVariables() returned %d variables, want %d", len(got), len(tt.wantVars))
			}

			for key, wantValue := range tt.wantVars {
				if gotValue, ok := got[key]; !ok {
					t.Errorf("buildAddonVariables() missing key %q", key)
				} else if gotValue != wantValue {
					t.Errorf("buildAddonVariables()[%q] = %v, want %v", key, gotValue, wantValue)
				}
			}
		})
	}
}

func TestSetCondition(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = appsv1alpha1.AddToScheme(s)

	gitOpsCluster := &appsv1alpha1.GitOpsCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-cluster",
			Namespace:  "argocd",
			Generation: 1,
		},
		Spec: appsv1alpha1.GitOpsClusterSpec{
			PlacementRef: corev1.ObjectReference{
				Kind: "Placement",
				Name: "test-placement",
			},
			ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{},
		},
	}

	r := &GitOpsClusterReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithObjects(gitOpsCluster).WithStatusSubresource(gitOpsCluster).Build(),
		Scheme: s,
	}

	tests := []struct {
		name          string
		conditionType string
		status        metav1.ConditionStatus
		reason        string
		message       string
	}{
		{
			name:          "set condition to true",
			conditionType: appsv1alpha1.ConditionRBACReady,
			status:        metav1.ConditionTrue,
			reason:        appsv1alpha1.ReasonSuccess,
			message:       "RBAC resources created successfully",
		},
		{
			name:          "set condition to false",
			conditionType: appsv1alpha1.ConditionServerDiscovered,
			status:        metav1.ConditionFalse,
			reason:        appsv1alpha1.ReasonServerNotDiscovered,
			message:       "Server not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r.setCondition(context.TODO(), gitOpsCluster, tt.conditionType, tt.status, tt.reason, tt.message)

			// Check if condition was added
			found := false
			for _, cond := range gitOpsCluster.Status.Conditions {
				if cond.Type == tt.conditionType {
					found = true
					if cond.Status != tt.status {
						t.Errorf("condition status = %v, want %v", cond.Status, tt.status)
					}
					if cond.Reason != tt.reason {
						t.Errorf("condition reason = %v, want %v", cond.Reason, tt.reason)
					}
					if cond.Message != tt.message {
						t.Errorf("condition message = %v, want %v", cond.Message, tt.message)
					}
					if cond.ObservedGeneration != gitOpsCluster.Generation {
						t.Errorf("condition observedGeneration = %v, want %v", cond.ObservedGeneration, gitOpsCluster.Generation)
					}
					break
				}
			}
			if !found {
				t.Errorf("condition type %q not found in status", tt.conditionType)
			}
		})
	}
}
