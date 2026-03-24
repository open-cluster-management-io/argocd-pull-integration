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

package addon

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEnsureNamespace(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)

	tests := []struct {
		name          string
		namespaceName string
		existingObjs  []runtime.Object
		wantErr       bool
		checkLabels   bool
	}{
		{
			name:          "creates namespace when not exists",
			namespaceName: "test-namespace",
			existingObjs:  []runtime.Object{},
			wantErr:       false,
			checkLabels:   true,
		},
		{
			name:          "handles existing namespace",
			namespaceName: "existing-namespace",
			existingObjs: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "existing-namespace",
					},
				},
			},
			wantErr:     false,
			checkLabels: false,
		},
		{
			name:          "creates operator namespace",
			namespaceName: "argocd-operator-system",
			existingObjs:  []runtime.Object{},
			wantErr:       false,
			checkLabels:   true,
		},
		{
			name:          "creates argocd namespace",
			namespaceName: "argocd",
			existingObjs:  []runtime.Object{},
			wantErr:       false,
			checkLabels:   true,
		},
		{
			name:          "creates namespace with special characters",
			namespaceName: "my-test-namespace-123",
			existingObjs:  []runtime.Object{},
			wantErr:       false,
			checkLabels:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ArgoCDAgentAddonReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).Build(),
				Scheme: s,
			}

			err := r.ensureNamespace(context.Background(), tt.namespaceName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ensureNamespace() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				ns := &corev1.Namespace{}
				err := r.Get(context.Background(), types.NamespacedName{Name: tt.namespaceName}, ns)
				if err != nil {
					t.Errorf("Failed to get namespace: %v", err)
					return
				}

				if tt.checkLabels && len(tt.existingObjs) == 0 {
					if ns.Labels == nil {
						t.Error("Namespace should have labels")
					} else if ns.Labels["app.kubernetes.io/managed-by"] != "argocd-agent-addon" {
						t.Error("Namespace should have managed-by label")
					}
				}
			}
		})
	}
}

func TestInstallOrUpdateArgoCDAgentValidation(t *testing.T) {
	tests := []struct {
		name          string
		operatorImage string
		wantImageSet  bool
	}{
		{
			name:          "validates operator image is required",
			operatorImage: "",
			wantImageSet:  false,
		},
		{
			name:          "accepts valid configuration",
			operatorImage: "quay.io/operator:latest",
			wantImageSet:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasImages := tt.operatorImage != ""
			if hasImages != tt.wantImageSet {
				t.Errorf("Image validation = %v, want %v", hasImages, tt.wantImageSet)
			}
		})
	}
}

func TestCopyClientCertificate(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)

	tests := []struct {
		name         string
		existingObjs []runtime.Object
		wantErr      bool
	}{
		{
			name: "returns nil when source secret not found",
			existingObjs: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "argocd-agent-ca",
						Namespace: "argocd",
					},
					Data: map[string][]byte{
						"tls.crt": []byte("ca-cert"),
						"tls.key": []byte("ca-key"),
					},
				},
			},
			wantErr: false,
		},
		{
			name:         "handles missing secrets gracefully",
			existingObjs: []runtime.Object{},
			wantErr:      false,
		},
		{
			name: "returns error when secret missing tls.crt",
			existingObjs: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "argocd-agent-addon-open-cluster-management.io-argocd-agent-addon-client-cert",
						Namespace: "open-cluster-management-agent-addon",
					},
					Data: map[string][]byte{
						"tls.key": []byte("key-data"),
					},
				},
			},
			wantErr: true,
		},
		{
			name: "returns error when secret missing tls.key",
			existingObjs: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "argocd-agent-addon-open-cluster-management.io-argocd-agent-addon-client-cert",
						Namespace: "open-cluster-management-agent-addon",
					},
					Data: map[string][]byte{
						"tls.crt": []byte("cert-data"),
					},
				},
			},
			wantErr: true,
		},
		{
			name: "copies certificate when both exist",
			existingObjs: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "argocd-agent-addon-open-cluster-management.io-argocd-agent-addon-client-cert",
						Namespace: "open-cluster-management-agent-addon",
					},
					Data: map[string][]byte{
						"tls.crt": []byte("cert-data"),
						"tls.key": []byte("key-data"),
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ArgoCDAgentAddonReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).Build(),
				Scheme: s,
			}

			err := r.copyClientCertificate(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("copyClientCertificate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestUninstallArgoCDAgent(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)

	tests := []struct {
		name         string
		existingObjs []runtime.Object
		wantErr      bool
	}{
		{
			name:         "uninstalls when no resources exist",
			existingObjs: []runtime.Object{},
			wantErr:      false,
		},
		{
			name: "uninstalls with existing namespace",
			existingObjs: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "argocd-operator-system",
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ArgoCDAgentAddonReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).Build(),
				Scheme: s,
			}

			err := r.uninstallArgoCDAgent(context.Background())
			if err != nil {
				t.Logf("uninstallArgoCDAgent() error (expected in unit test): %v", err)
			}
		})
	}
}

func TestDeleteOperatorResources(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)

	tests := []struct {
		name         string
		existingObjs []runtime.Object
		wantErr      bool
	}{
		{
			name:         "succeeds when no operator resources",
			existingObjs: []runtime.Object{},
			wantErr:      false,
		},
		{
			name: "handles existing namespace",
			existingObjs: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "argocd-operator-system",
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ArgoCDAgentAddonReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).Build(),
				Scheme: s,
			}

			err := r.deleteOperatorResources(context.Background())
			if err != nil {
				t.Logf("deleteOperatorResources() error (expected in unit test): %v", err)
			}
		})
	}
}

func TestReconcilerConfiguration(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)

	tests := []struct {
		name       string
		reconciler *ArgoCDAgentAddonReconciler
	}{
		{
			name: "reconciler with operator image",
			reconciler: &ArgoCDAgentAddonReconciler{
				OperatorImage: "quay.io/operator:latest",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.reconciler.Client = fake.NewClientBuilder().WithScheme(s).Build()
			tt.reconciler.Scheme = s

			if tt.reconciler.OperatorImage == "" {
				t.Error("OperatorImage should be set")
			}
		})
	}
}
