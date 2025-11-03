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

	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
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
			},
		},
		{
			name: "minimal configuration",
			gitOpsCluster: &appsv1alpha1.GitOpsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "argocd",
				},
				Spec: appsv1alpha1.GitOpsClusterSpec{
					ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{},
				},
			},
			serverAddress: "argocd-server.argocd.svc",
			serverPort:    "8080",
			wantVars: map[string]string{
				"ARGOCD_AGENT_SERVER_ADDRESS": "argocd-server.argocd.svc",
				"ARGOCD_AGENT_SERVER_PORT":    "8080",
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

func TestInitializeConditions(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = appsv1alpha1.AddToScheme(s)

	tests := []struct {
		name               string
		existingConditions []metav1.Condition
		wantConditionCount int
	}{
		{
			name:               "initializes all conditions when none exist",
			existingConditions: []metav1.Condition{},
			wantConditionCount: 12, // All 12 conditions should be initialized
		},
		{
			name: "keeps existing conditions and adds missing ones",
			existingConditions: []metav1.Condition{
				{
					Type:   appsv1alpha1.ConditionRBACReady,
					Status: metav1.ConditionTrue,
					Reason: appsv1alpha1.ReasonSuccess,
				},
				{
					Type:   appsv1alpha1.ConditionServerDiscovered,
					Status: metav1.ConditionTrue,
					Reason: appsv1alpha1.ReasonSuccess,
				},
			},
			wantConditionCount: 12, // Should add 10 more conditions
		},
		{
			name: "does not modify when all conditions exist",
			existingConditions: []metav1.Condition{
				{Type: appsv1alpha1.ConditionRBACReady, Status: metav1.ConditionTrue, Reason: appsv1alpha1.ReasonSuccess},
				{Type: appsv1alpha1.ConditionServerDiscovered, Status: metav1.ConditionTrue, Reason: appsv1alpha1.ReasonSuccess},
				{Type: appsv1alpha1.ConditionJWTSecretReady, Status: metav1.ConditionTrue, Reason: appsv1alpha1.ReasonSuccess},
				{Type: appsv1alpha1.ConditionCACertificateReady, Status: metav1.ConditionTrue, Reason: appsv1alpha1.ReasonSuccess},
				{Type: appsv1alpha1.ConditionPrincipalCertificateReady, Status: metav1.ConditionTrue, Reason: appsv1alpha1.ReasonSuccess},
				{Type: appsv1alpha1.ConditionResourceProxyCertificateReady, Status: metav1.ConditionTrue, Reason: appsv1alpha1.ReasonSuccess},
				{Type: appsv1alpha1.ConditionPlacementTolerationConfigured, Status: metav1.ConditionTrue, Reason: appsv1alpha1.ReasonSuccess},
				{Type: appsv1alpha1.ConditionAddOnTemplateReady, Status: metav1.ConditionTrue, Reason: appsv1alpha1.ReasonSuccess},
				{Type: appsv1alpha1.ConditionPlacementEvaluated, Status: metav1.ConditionTrue, Reason: appsv1alpha1.ReasonSuccess},
				{Type: appsv1alpha1.ConditionClustersImported, Status: metav1.ConditionTrue, Reason: appsv1alpha1.ReasonSuccess},
				{Type: appsv1alpha1.ConditionManifestWorkCreated, Status: metav1.ConditionTrue, Reason: appsv1alpha1.ReasonSuccess},
				{Type: appsv1alpha1.ConditionAddonConfigured, Status: metav1.ConditionTrue, Reason: appsv1alpha1.ReasonSuccess},
			},
			wantConditionCount: 12, // Should keep all 12 (RemovedClustersCleanedUp is not initialized by default)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
				Status: appsv1alpha1.GitOpsClusterStatus{
					Conditions: tt.existingConditions,
				},
			}

			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithObjects(gitOpsCluster).WithStatusSubresource(gitOpsCluster).Build(),
				Scheme: s,
			}

			r.initializeConditions(context.TODO(), gitOpsCluster)

			if len(gitOpsCluster.Status.Conditions) != tt.wantConditionCount {
				t.Errorf("initializeConditions() resulted in %d conditions, want %d",
					len(gitOpsCluster.Status.Conditions), tt.wantConditionCount)
			}

			// Verify that newly added conditions have Unknown status and InProgress reason
			for _, cond := range gitOpsCluster.Status.Conditions {
				// If this was not in the existing conditions, it should be Unknown/InProgress
				found := false
				for _, existingCond := range tt.existingConditions {
					if existingCond.Type == cond.Type {
						found = true
						break
					}
				}
				if !found {
					if cond.Status != metav1.ConditionUnknown {
						t.Errorf("new condition %s has status %v, want Unknown", cond.Type, cond.Status)
					}
					if cond.Reason != appsv1alpha1.ReasonInProgress {
						t.Errorf("new condition %s has reason %v, want InProgress", cond.Type, cond.Reason)
					}
				}
			}
		})
	}
}

func TestPlacementDecisionMapper(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = appsv1alpha1.AddToScheme(s)
	_ = clusterv1beta1.AddToScheme(s)

	namespace := "argocd"

	tests := []struct {
		name             string
		existingObjs     []runtime.Object
		placementName    string
		wantRequestCount int
	}{
		{
			name: "maps PlacementDecision to GitOpsCluster",
			existingObjs: []runtime.Object{
				&appsv1alpha1.GitOpsCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gitops-cluster-1",
						Namespace: namespace,
					},
					Spec: appsv1alpha1.GitOpsClusterSpec{
						PlacementRef: corev1.ObjectReference{
							Kind: "Placement",
							Name: "test-placement",
						},
						ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{},
					},
				},
				&appsv1alpha1.GitOpsCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gitops-cluster-2",
						Namespace: namespace,
					},
					Spec: appsv1alpha1.GitOpsClusterSpec{
						PlacementRef: corev1.ObjectReference{
							Kind: "Placement",
							Name: "test-placement",
						},
						ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{},
					},
				},
				&appsv1alpha1.GitOpsCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gitops-cluster-3",
						Namespace: namespace,
					},
					Spec: appsv1alpha1.GitOpsClusterSpec{
						PlacementRef: corev1.ObjectReference{
							Kind: "Placement",
							Name: "other-placement",
						},
						ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{},
					},
				},
			},
			placementName:    "test-placement",
			wantRequestCount: 2, // Should match gitops-cluster-1 and gitops-cluster-2
		},
		{
			name: "returns empty when no GitOpsClusters reference placement",
			existingObjs: []runtime.Object{
				&appsv1alpha1.GitOpsCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gitops-cluster-1",
						Namespace: namespace,
					},
					Spec: appsv1alpha1.GitOpsClusterSpec{
						PlacementRef: corev1.ObjectReference{
							Kind: "Placement",
							Name: "other-placement",
						},
						ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{},
					},
				},
			},
			placementName:    "test-placement",
			wantRequestCount: 0,
		},
		{
			name:             "returns empty when no GitOpsClusters exist",
			existingObjs:     []runtime.Object{},
			placementName:    "test-placement",
			wantRequestCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).Build(),
				Scheme: s,
			}

			placementDecision := &clusterv1beta1.PlacementDecision{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-placement-decision-1",
					Namespace: namespace,
					Labels: map[string]string{
						"cluster.open-cluster-management.io/placement": tt.placementName,
					},
				},
			}

			requests := r.placementDecisionMapper(context.Background(), placementDecision)

			if len(requests) != tt.wantRequestCount {
				t.Errorf("placementDecisionMapper() returned %d requests, want %d", len(requests), tt.wantRequestCount)
			}

			// Verify that the returned requests are correct
			if tt.wantRequestCount > 0 {
				for _, req := range requests {
					if req.Namespace != namespace {
						t.Errorf("request namespace = %v, want %v", req.Namespace, namespace)
					}
				}
			}
		})
	}
}
