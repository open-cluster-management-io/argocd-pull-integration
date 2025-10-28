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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workv1 "open-cluster-management.io/api/work/v1"
	appsv1alpha1 "open-cluster-management.io/argocd-pull-integration/api/v1alpha1"
)

func TestGetManagedClustersFromPlacement(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = appsv1alpha1.AddToScheme(s)
	_ = clusterv1.AddToScheme(s)
	_ = clusterv1beta1.AddToScheme(s)

	namespace := "argocd"

	tests := []struct {
		name          string
		existingObjs  []runtime.Object
		gitOpsCluster *appsv1alpha1.GitOpsCluster
		wantCount     int
		wantErr       bool
	}{
		{
			name: "finds clusters from placement decisions",
			existingObjs: []runtime.Object{
				&clusterv1beta1.Placement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: namespace,
					},
				},
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement-decision-1",
						Namespace: namespace,
						Labels: map[string]string{
							"cluster.open-cluster-management.io/placement": "test-placement",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{
							{ClusterName: "cluster1"},
							{ClusterName: "cluster2"},
						},
					},
				},
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster1",
					},
				},
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster2",
					},
				},
			},
			gitOpsCluster: &appsv1alpha1.GitOpsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
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
			wantCount: 2,
			wantErr:   false,
		},
		{
			name: "returns empty when no placement decisions",
			existingObjs: []runtime.Object{
				&clusterv1beta1.Placement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: namespace,
					},
				},
			},
			gitOpsCluster: &appsv1alpha1.GitOpsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
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
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:         "returns error when placement not found",
			existingObjs: []runtime.Object{},
			gitOpsCluster: &appsv1alpha1.GitOpsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: namespace,
				},
				Spec: appsv1alpha1.GitOpsClusterSpec{
					PlacementRef: corev1.ObjectReference{
						Kind: "Placement",
						Name: "missing-placement",
					},
					ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{},
				},
			},
			wantCount: 0,
			wantErr:   true,
		},
		{
			name:         "returns error for invalid placement kind",
			existingObjs: []runtime.Object{},
			gitOpsCluster: &appsv1alpha1.GitOpsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: namespace,
				},
				Spec: appsv1alpha1.GitOpsClusterSpec{
					PlacementRef: corev1.ObjectReference{
						Kind: "InvalidKind",
						Name: "test-placement",
					},
					ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{},
				},
			},
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).Build(),
				Scheme: s,
			}

			clusters, err := r.getManagedClustersFromPlacement(context.Background(), tt.gitOpsCluster)
			if (err != nil) != tt.wantErr {
				t.Errorf("getManagedClustersFromPlacement() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(clusters) != tt.wantCount {
				t.Errorf("getManagedClustersFromPlacement() returned %d clusters, want %d", len(clusters), tt.wantCount)
			}
		})
	}
}

func TestReconcile(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = appsv1alpha1.AddToScheme(s)
	_ = clusterv1.AddToScheme(s)
	_ = clusterv1beta1.AddToScheme(s)
	_ = workv1.AddToScheme(s)

	namespace := "argocd"

	tests := []struct {
		name         string
		existingObjs []runtime.Object
		req          ctrl.Request
		wantErr      bool
	}{
		{
			name:         "returns nil when GitOpsCluster not found",
			existingObjs: []runtime.Object{},
			req: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "missing",
					Namespace: namespace,
				},
			},
			wantErr: false,
		},
		{
			name: "reconciles GitOpsCluster with existing server config",
			existingObjs: []runtime.Object{
				&appsv1alpha1.GitOpsCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster",
						Namespace: namespace,
					},
					Spec: appsv1alpha1.GitOpsClusterSpec{
						PlacementRef: corev1.ObjectReference{
							Kind: "Placement",
							Name: "test-placement",
						},
						ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{
							PrincipalServerAddress: "argocd-server.argocd.svc",
							PrincipalServerPort:    "8080",
						},
					},
				},
				&clusterv1beta1.Placement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: namespace,
					},
				},
			},
			req: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-cluster",
					Namespace: namespace,
				},
			},
			wantErr: true, // Will fail due to missing JWT secret, but that's expected
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).WithStatusSubresource(&appsv1alpha1.GitOpsCluster{}).Build(),
				Scheme: s,
			}

			_, err := r.Reconcile(context.Background(), tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Reconcile() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetArgoCDAgentCACertString(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)

	namespace := "argocd"
	caCert := "-----BEGIN CERTIFICATE-----\ntest-ca-cert\n-----END CERTIFICATE-----"

	tests := []struct {
		name         string
		existingObjs []runtime.Object
		wantErr      bool
		wantContains string
	}{
		{
			name: "retrieves CA cert from secret",
			existingObjs: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ArgoCDAgentCASecretName,
						Namespace: namespace,
					},
					Data: map[string][]byte{
						"tls.crt": []byte(caCert),
					},
				},
			},
			wantErr:      false,
			wantContains: "test-ca-cert",
		},
		{
			name:         "returns error when secret not found",
			existingObjs: []runtime.Object{},
			wantErr:      true,
		},
		{
			name: "returns error when tls.crt missing",
			existingObjs: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ArgoCDAgentCASecretName,
						Namespace: namespace,
					},
					Data: map[string][]byte{
						"tls.key": []byte("key-data"),
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).Build(),
				Scheme: s,
			}

			cert, err := r.getArgoCDAgentCACertString(context.Background(), namespace)
			if (err != nil) != tt.wantErr {
				t.Errorf("getArgoCDAgentCACertString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.wantContains != "" {
				if cert != caCert {
					t.Errorf("getArgoCDAgentCACertString() = %v, want %v", cert, caCert)
				}
			}
		})
	}
}

func TestCreateArgoCDAgentManifestWork(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = workv1.AddToScheme(s)
	_ = clusterv1.AddToScheme(s)

	namespace := "argocd"

	tests := []struct {
		name         string
		cluster      *clusterv1.ManagedCluster
		existingObjs []runtime.Object
		wantErr      bool
	}{
		{
			name: "creates manifestwork with CA secret",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
			},
			existingObjs: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ArgoCDAgentCASecretName,
						Namespace: namespace,
					},
					Data: map[string][]byte{
						"tls.crt": []byte("test-ca-cert"),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "handles missing CA secret",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
			},
			existingObjs: []runtime.Object{},
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).Build(),
				Scheme: s,
			}

			err := r.createArgoCDAgentManifestWork(context.Background(), namespace, tt.cluster)
			if (err != nil) != tt.wantErr {
				t.Errorf("createArgoCDAgentManifestWork() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuildArgoCDAgentManifestWork(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = workv1.AddToScheme(s)

	namespace := "argocd"
	clusterName := "test-cluster"
	caCert := "test-ca-cert"

	tests := []struct {
		name        string
		clusterName string
	}{
		{
			name:        "builds manifestwork for regular cluster",
			clusterName: clusterName,
		},
		{
			name:        "builds manifestwork for local-cluster",
			clusterName: "local-cluster",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).Build(),
				Scheme: s,
			}

			mw := r.buildArgoCDAgentManifestWork(tt.clusterName, namespace, caCert)
			if mw == nil {
				t.Error("buildArgoCDAgentManifestWork() returned nil")
				return
			}

			if mw.Name != ArgoCDAgentCAManifestWorkName {
				t.Errorf("ManifestWork name = %v, want %v", mw.Name, ArgoCDAgentCAManifestWorkName)
			}
			if mw.Namespace != tt.clusterName {
				t.Errorf("ManifestWork namespace = %v, want %v", mw.Namespace, tt.clusterName)
			}
		})
	}
}
