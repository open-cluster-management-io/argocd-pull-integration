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
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestUninstallArgoCDAgentInternal(t *testing.T) {
	scheme := runtime.NewScheme()

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
				WithScheme(scheme).
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
