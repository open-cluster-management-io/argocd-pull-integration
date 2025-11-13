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
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestUninstallArgoCDAgentInternal(t *testing.T) {
	// Register corev1 scheme for ConfigMap support
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)

	tests := []struct {
		name           string
		existingObjs   []runtime.Object
		expectError    bool
		expectOperator bool
	}{
		{
			name:           "no ArgoCD CR exists",
			existingObjs:   []runtime.Object{},
			expectError:    false,
			expectOperator: false,
		},
		{
			name: "ArgoCD CR exists",
			existingObjs: []runtime.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "argoproj.io/v1beta1",
						"kind":       "ArgoCD",
						"metadata": map[string]interface{}{
							"name":      "argocd",
							"namespace": argoCDNamespace,
						},
					},
				},
			},
			expectError:    false,
			expectOperator: false, // Will timeout waiting for deletion in test
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(tt.existingObjs...).
				Build()

			// Create a short timeout context for tests
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			err := uninstallArgoCDAgentInternal(ctx, c)
			if (err != nil) != tt.expectError {
				t.Errorf("uninstallArgoCDAgentInternal() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestDeleteOperatorResourcesInternal(t *testing.T) {
	scheme := runtime.NewScheme()

	// Create test operator deployment
	operatorDeployment := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "test-operator",
				"namespace": operatorNamespace,
				"labels": map[string]interface{}{
					"app.kubernetes.io/managed-by": "argocd-agent-addon",
				},
			},
		},
	}

	tests := []struct {
		name         string
		existingObjs []runtime.Object
		expectError  bool
	}{
		{
			name:         "no operator resources exist",
			existingObjs: []runtime.Object{},
			expectError:  false,
		},
		{
			name: "operator deployment exists",
			existingObjs: []runtime.Object{
				operatorDeployment,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tt.existingObjs...).
				Build()

			ctx := context.Background()
			err := deleteOperatorResourcesInternal(ctx, c)
			if (err != nil) != tt.expectError {
				t.Errorf("deleteOperatorResourcesInternal() error = %v, expectError %v", err, tt.expectError)
			}

			// Verify operator deployment is deleted
			if !tt.expectError && len(tt.existingObjs) > 0 {
				deployment := &unstructured.Unstructured{}
				deployment.SetAPIVersion("apps/v1")
				deployment.SetKind("Deployment")
				err := c.Get(ctx, types.NamespacedName{
					Name:      "test-operator",
					Namespace: operatorNamespace,
				}, deployment)
				// Should be not found after deletion
				if err == nil {
					t.Error("Expected deployment to be deleted, but it still exists")
				}
			}
		})
	}
}

func TestCreatePauseMarker(t *testing.T) {
	// Register corev1 scheme for ConfigMap
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)

	tests := []struct {
		name          string
		namespace     string
		existingObjs  []runtime.Object
		expectError   bool
		expectCreated bool
	}{
		{
			name:          "create pause marker in empty namespace",
			namespace:     operatorNamespace,
			existingObjs:  []runtime.Object{},
			expectError:   false,
			expectCreated: true,
		},
		{
			name:      "pause marker already exists",
			namespace: operatorNamespace,
			existingObjs: []runtime.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      PauseMarkerName,
						Namespace: operatorNamespace,
					},
					Data: map[string]string{
						"paused": "true",
						"reason": "cleanup",
					},
				},
			},
			expectError:   false,
			expectCreated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(tt.existingObjs...).
				Build()

			ctx := context.Background()
			err := createPauseMarker(ctx, c, tt.namespace)

			if (err != nil) != tt.expectError {
				t.Errorf("createPauseMarker() error = %v, expectError %v", err, tt.expectError)
			}

			if tt.expectCreated {
				// Verify the pause marker was created
				cm := &corev1.ConfigMap{}
				err := c.Get(ctx, types.NamespacedName{
					Name:      PauseMarkerName,
					Namespace: tt.namespace,
				}, cm)
				if err != nil {
					t.Errorf("Failed to get pause marker: %v", err)
				}
				if cm.Data["paused"] != "true" {
					t.Errorf("Expected paused=true, got %s", cm.Data["paused"])
				}
				if cm.Data["reason"] != "cleanup" {
					t.Errorf("Expected reason=cleanup, got %s", cm.Data["reason"])
				}
			}
		})
	}
}

func TestIsPaused(t *testing.T) {
	// Register corev1 scheme for ConfigMap
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)

	tests := []struct {
		name         string
		namespace    string
		existingObjs []runtime.Object
		expectPaused bool
	}{
		{
			name:         "no pause marker exists",
			namespace:    operatorNamespace,
			existingObjs: []runtime.Object{},
			expectPaused: false,
		},
		{
			name:      "pause marker exists with paused=true",
			namespace: operatorNamespace,
			existingObjs: []runtime.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      PauseMarkerName,
						Namespace: operatorNamespace,
					},
					Data: map[string]string{
						"paused": "true",
						"reason": "cleanup",
					},
				},
			},
			expectPaused: true,
		},
		{
			name:      "pause marker exists with paused=false",
			namespace: operatorNamespace,
			existingObjs: []runtime.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      PauseMarkerName,
						Namespace: operatorNamespace,
					},
					Data: map[string]string{
						"paused": "false",
					},
				},
			},
			expectPaused: false,
		},
		{
			name:      "pause marker exists without paused field",
			namespace: operatorNamespace,
			existingObjs: []runtime.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      PauseMarkerName,
						Namespace: operatorNamespace,
					},
					Data: map[string]string{
						"reason": "test",
					},
				},
			},
			expectPaused: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(tt.existingObjs...).
				Build()

			ctx := context.Background()
			paused := IsPaused(ctx, c, tt.namespace)

			if paused != tt.expectPaused {
				t.Errorf("IsPaused() = %v, want %v", paused, tt.expectPaused)
			}
		})
	}
}

func TestVerifyNamespaceCleanup(t *testing.T) {
	s := runtime.NewScheme()

	tests := []struct {
		name                 string
		namespace            string
		existingObjs         []runtime.Object
		expectResourcesExist bool
		expectError          bool
	}{
		{
			name:                 "no resources exist",
			namespace:            operatorNamespace,
			existingObjs:         []runtime.Object{},
			expectResourcesExist: false,
			expectError:          false,
		},
		{
			name:      "deployment exists with addon label",
			namespace: operatorNamespace,
			existingObjs: []runtime.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "apps/v1",
						"kind":       "Deployment",
						"metadata": map[string]interface{}{
							"name":      "test-operator",
							"namespace": operatorNamespace,
							"labels": map[string]interface{}{
								"app.kubernetes.io/managed-by": "argocd-agent-addon",
							},
						},
					},
				},
			},
			expectResourcesExist: true,
			expectError:          false,
		},
		{
			name:      "deployment exists without addon label",
			namespace: operatorNamespace,
			existingObjs: []runtime.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "apps/v1",
						"kind":       "Deployment",
						"metadata": map[string]interface{}{
							"name":      "other-deployment",
							"namespace": operatorNamespace,
							"labels": map[string]interface{}{
								"app": "other",
							},
						},
					},
				},
			},
			expectResourcesExist: false,
			expectError:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(tt.existingObjs...).
				Build()

			ctx := context.Background()
			resourcesExist, err := verifyNamespaceCleanup(ctx, c, tt.namespace)

			if (err != nil) != tt.expectError {
				t.Errorf("verifyNamespaceCleanup() error = %v, expectError %v", err, tt.expectError)
			}

			if resourcesExist != tt.expectResourcesExist {
				t.Errorf("verifyNamespaceCleanup() resourcesExist = %v, want %v", resourcesExist, tt.expectResourcesExist)
			}
		})
	}
}

func TestGetCleanupVerificationWaitDuration(t *testing.T) {
	tests := []struct {
		name          string
		envValue      string
		setEnv        bool
		expectSeconds int
	}{
		{
			name:          "no env var set - default to 0",
			envValue:      "",
			setEnv:        false,
			expectSeconds: 0,
		},
		{
			name:          "env var set to 60",
			envValue:      "60",
			setEnv:        true,
			expectSeconds: 60,
		},
		{
			name:          "env var set to 0",
			envValue:      "0",
			setEnv:        true,
			expectSeconds: 0,
		},
		{
			name:          "env var set to invalid value",
			envValue:      "invalid",
			setEnv:        true,
			expectSeconds: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original env var
			originalValue := os.Getenv("CLEANUP_VERIFICATION_WAIT_SECONDS")
			defer func() {
				if originalValue != "" {
					os.Setenv("CLEANUP_VERIFICATION_WAIT_SECONDS", originalValue)
				} else {
					os.Unsetenv("CLEANUP_VERIFICATION_WAIT_SECONDS")
				}
			}()

			// Set or unset env var
			if tt.setEnv {
				os.Setenv("CLEANUP_VERIFICATION_WAIT_SECONDS", tt.envValue)
			} else {
				os.Unsetenv("CLEANUP_VERIFICATION_WAIT_SECONDS")
			}

			duration := getCleanupVerificationWaitDuration()
			expected := time.Duration(tt.expectSeconds) * time.Second

			if duration != expected {
				t.Errorf("getCleanupVerificationWaitDuration() = %v, want %v", duration, expected)
			}
		})
	}
}
