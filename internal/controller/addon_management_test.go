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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	appsv1alpha1 "open-cluster-management.io/argocd-pull-integration/api/v1alpha1"
)

func TestEnsureManagedClusterAddon(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = addonv1alpha1.AddToScheme(s)
	_ = appsv1alpha1.AddToScheme(s)

	clusterNamespace := "cluster1"

	tests := []struct {
		name         string
		existingObjs []runtime.Object
		wantErr      bool
	}{
		{
			name:         "creates addon when not exists",
			existingObjs: []runtime.Object{},
			wantErr:      false,
		},
		{
			name: "handles existing addon",
			existingObjs: []runtime.Object{
				&addonv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ArgoCDAgentAddonName,
						Namespace: clusterNamespace,
					},
					Spec: addonv1alpha1.ManagedClusterAddOnSpec{
						Configs: []addonv1alpha1.AddOnConfig{
							{
								ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
									Group:    "addon.open-cluster-management.io",
									Resource: "addondeploymentconfigs",
								},
								ConfigReferent: addonv1alpha1.ConfigReferent{
									Name:      ArgoCDAgentAddonConfigName,
									Namespace: clusterNamespace,
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).Build(),
				Scheme: s,
			}

			err := r.EnsureManagedClusterAddon(context.Background(), clusterNamespace)
			if (err != nil) != tt.wantErr {
				t.Errorf("EnsureManagedClusterAddon() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify addon was created or exists
				addon := &addonv1alpha1.ManagedClusterAddOn{}
				err := r.Get(context.Background(), types.NamespacedName{
					Name:      ArgoCDAgentAddonName,
					Namespace: clusterNamespace,
				}, addon)
				if err != nil {
					t.Errorf("Failed to get ManagedClusterAddOn: %v", err)
					return
				}

				// Verify config reference exists
				found := false
				for _, config := range addon.Spec.Configs {
					if config.Group == "addon.open-cluster-management.io" &&
						config.Resource == "addondeploymentconfigs" &&
						config.Name == ArgoCDAgentAddonConfigName {
						found = true
						break
					}
				}
				if !found {
					t.Error("ManagedClusterAddOn missing expected config reference")
				}
			}
		})
	}
}

func TestEnsureAddOnDeploymentConfig(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = addonv1alpha1.AddToScheme(s)

	clusterNamespace := "cluster1"

	tests := []struct {
		name         string
		existingObjs []runtime.Object
		variables    map[string]string
		wantErr      bool
	}{
		{
			name:         "creates config with variables",
			existingObjs: []runtime.Object{},
			variables: map[string]string{
				"ARGOCD_AGENT_SERVER_ADDRESS": "argocd-server.argocd.svc",
				"ARGOCD_AGENT_SERVER_PORT":    "8080",
				"ARGOCD_AGENT_MODE":           "managed",
			},
			wantErr: false,
		},
		{
			name: "updates existing config with new variables",
			existingObjs: []runtime.Object{
				&addonv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ArgoCDAgentAddonConfigName,
						Namespace: clusterNamespace,
					},
					Spec: addonv1alpha1.AddOnDeploymentConfigSpec{
						CustomizedVariables: []addonv1alpha1.CustomizedVariable{
							{
								Name:  "ARGOCD_AGENT_SERVER_ADDRESS",
								Value: "argocd-server.argocd.svc",
							},
						},
					},
				},
			},
			variables: map[string]string{
				"ARGOCD_AGENT_SERVER_PORT": "8080",
				"ARGOCD_AGENT_MODE":        "managed",
			},
			wantErr: false,
		},
		{
			name: "no update when all variables exist",
			existingObjs: []runtime.Object{
				&addonv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ArgoCDAgentAddonConfigName,
						Namespace: clusterNamespace,
					},
					Spec: addonv1alpha1.AddOnDeploymentConfigSpec{
						CustomizedVariables: []addonv1alpha1.CustomizedVariable{
							{
								Name:  "ARGOCD_AGENT_SERVER_ADDRESS",
								Value: "argocd-server.argocd.svc",
							},
							{
								Name:  "ARGOCD_AGENT_SERVER_PORT",
								Value: "8080",
							},
						},
					},
				},
			},
			variables: map[string]string{
				"ARGOCD_AGENT_SERVER_ADDRESS": "argocd-server.argocd.svc",
				"ARGOCD_AGENT_SERVER_PORT":    "8080",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).Build(),
				Scheme: s,
			}

			err := r.EnsureAddOnDeploymentConfig(context.Background(), clusterNamespace, tt.variables)
			if (err != nil) != tt.wantErr {
				t.Errorf("EnsureAddOnDeploymentConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify config was created or updated
				config := &addonv1alpha1.AddOnDeploymentConfig{}
				err := r.Get(context.Background(), types.NamespacedName{
					Name:      ArgoCDAgentAddonConfigName,
					Namespace: clusterNamespace,
				}, config)
				if err != nil {
					t.Errorf("Failed to get AddOnDeploymentConfig: %v", err)
					return
				}

				// Verify variables exist
				configVars := make(map[string]string)
				for _, v := range config.Spec.CustomizedVariables {
					configVars[v.Name] = v.Value
				}

				for name := range tt.variables {
					if _, exists := configVars[name]; !exists {
						t.Errorf("AddOnDeploymentConfig missing variable %s", name)
					}
				}
			}
		})
	}
}

func TestEnsureAddonConfig(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = addonv1alpha1.AddToScheme(s)

	clusterNamespace := "cluster1"

	tests := []struct {
		name    string
		addon   *addonv1alpha1.ManagedClusterAddOn
		wantErr bool
	}{
		{
			name: "adds config when missing",
			addon: &addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ArgoCDAgentAddonName,
					Namespace: clusterNamespace,
				},
				Spec: addonv1alpha1.ManagedClusterAddOnSpec{
					Configs: []addonv1alpha1.AddOnConfig{},
				},
			},
			wantErr: false,
		},
		{
			name: "no update when config exists",
			addon: &addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ArgoCDAgentAddonName,
					Namespace: clusterNamespace,
				},
				Spec: addonv1alpha1.ManagedClusterAddOnSpec{
					Configs: []addonv1alpha1.AddOnConfig{
						{
							ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonv1alpha1.ConfigReferent{
								Name:      ArgoCDAgentAddonConfigName,
								Namespace: clusterNamespace,
							},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.addon).Build(),
				Scheme: s,
			}

			err := r.ensureAddonConfig(context.Background(), tt.addon)
			if (err != nil) != tt.wantErr {
				t.Errorf("ensureAddonConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
