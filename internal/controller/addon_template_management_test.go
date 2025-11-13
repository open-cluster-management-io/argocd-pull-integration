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
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	appsv1alpha1 "open-cluster-management.io/argocd-pull-integration/api/v1alpha1"
)

func TestGetControllerImage(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = appsv1alpha1.AddToScheme(s)

	tests := []struct {
		name          string
		envVar        string
		expectedImage string
		expectError   bool
	}{
		{
			name:          "env var set",
			envVar:        "test-image:v1.0.0",
			expectedImage: "test-image:v1.0.0",
			expectError:   false,
		},
		{
			name:        "env var not set - should error",
			envVar:      "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			if tt.envVar != "" {
				os.Setenv(ControllerImageEnvVar, tt.envVar)
				defer os.Unsetenv(ControllerImageEnvVar)
			} else {
				os.Unsetenv(ControllerImageEnvVar)
			}

			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).Build(),
				Scheme: s,
			}

			image, err := r.getControllerImage(context.Background(), "test-namespace")

			if tt.expectError {
				if err == nil {
					t.Errorf("getControllerImage() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("getControllerImage() error = %v", err)
				return
			}

			if image != tt.expectedImage {
				t.Errorf("getControllerImage() = %v, want %v", image, tt.expectedImage)
			}
		})
	}
}

func TestGetAddOnTemplateName(t *testing.T) {
	tests := []struct {
		name          string
		gitOpsCluster *appsv1alpha1.GitOpsCluster
		expectedName  string
	}{
		{
			name: "standard gitopscluster",
			gitOpsCluster: &appsv1alpha1.GitOpsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
			},
			expectedName: "argocd-agent-addon-test-namespace-test-cluster",
		},
		{
			name: "different namespace",
			gitOpsCluster: &appsv1alpha1.GitOpsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cluster",
					Namespace: "production",
				},
			},
			expectedName: "argocd-agent-addon-production-my-cluster",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := getAddOnTemplateName(tt.gitOpsCluster)
			if name != tt.expectedName {
				t.Errorf("getAddOnTemplateName() = %v, want %v", name, tt.expectedName)
			}
		})
	}
}

func TestEnsureAddOnTemplate(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = appsv1alpha1.AddToScheme(s)
	_ = addonv1alpha1.AddToScheme(s)

	gitOpsCluster := &appsv1alpha1.GitOpsCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "test-namespace",
		},
		Spec: appsv1alpha1.GitOpsClusterSpec{
			PlacementRef: corev1.ObjectReference{
				Name: "test-placement",
				Kind: "Placement",
			},
			ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{
				OperatorImage: "test-operator:v1",
				AgentImage:    "test-agent:v1",
			},
		},
	}

	tests := []struct {
		name         string
		existingObjs []runtime.Object
		wantErr      bool
	}{
		{
			name:         "creates new template",
			existingObjs: []runtime.Object{},
			wantErr:      false,
		},
		{
			name: "updates existing template",
			existingObjs: []runtime.Object{
				&addonv1alpha1.AddOnTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name: "argocd-agent-addon-test-namespace-test-cluster",
					},
					Spec: addonv1alpha1.AddOnTemplateSpec{
						AddonName: ArgoCDAgentAddonName,
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set CONTROLLER_IMAGE environment variable for test
			originalEnv := os.Getenv(ControllerImageEnvVar)
			os.Setenv(ControllerImageEnvVar, "test-controller:v1.0.0")
			defer func() {
				if originalEnv != "" {
					os.Setenv(ControllerImageEnvVar, originalEnv)
				} else {
					os.Unsetenv(ControllerImageEnvVar)
				}
			}()

			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).Build(),
				Scheme: s,
			}

			err := r.EnsureAddOnTemplate(context.Background(), gitOpsCluster)
			if (err != nil) != tt.wantErr {
				t.Errorf("EnsureAddOnTemplate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify template was created/updated
				template := &addonv1alpha1.AddOnTemplate{}
				err := r.Get(context.Background(), types.NamespacedName{
					Name: "argocd-agent-addon-test-namespace-test-cluster",
				}, template)
				if err != nil {
					t.Errorf("Failed to get AddOnTemplate after ensuring: %v", err)
				}
			}
		})
	}
}

func TestBuildAddonManifests(t *testing.T) {
	manifests := buildAddonManifests("addon-image:v1", "operator-image:v1", "agent-image:v1")

	if len(manifests) == 0 {
		t.Error("buildAddonManifests() returned no manifests")
	}

	// Verify we have the expected manifests (pre-delete job, serviceaccount, clusterrolebinding, deployment)
	if len(manifests) != 4 {
		t.Errorf("buildAddonManifests() returned %d manifests, want 4", len(manifests))
	}
}
