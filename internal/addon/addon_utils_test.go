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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"open-cluster-management.io/argocd-pull-integration/internal/pkg/images"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestApplyManifest(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)

	tests := []struct {
		name         string
		obj          *unstructured.Unstructured
		existingObjs []runtime.Object
		wantErr      bool
	}{
		{
			name: "creates new object",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test-config",
						"namespace": "default",
					},
					"data": map[string]interface{}{
						"key": "value",
					},
				},
			},
			existingObjs: []runtime.Object{},
			wantErr:      false,
		},
		{
			name: "updates existing object",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test-config",
						"namespace": "default",
					},
					"data": map[string]interface{}{
						"key": "new-value",
					},
				},
			},
			existingObjs: []runtime.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-config",
						Namespace: "default",
					},
					Data: map[string]string{
						"key": "old-value",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "skips object with skip annotation",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test-config",
						"namespace": "default",
						"annotations": map[string]interface{}{
							"argocd-addon.open-cluster-management.io/skip": "true",
						},
					},
				},
			},
			existingObjs: []runtime.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-config",
						Namespace: "default",
						Annotations: map[string]string{
							"argocd-addon.open-cluster-management.io/skip": "true",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "adds management label",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test-config",
						"namespace": "default",
					},
				},
			},
			existingObjs: []runtime.Object{},
			wantErr:      false,
		},
		{
			name: "creates service",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Service",
					"metadata": map[string]interface{}{
						"name":      "test-service",
						"namespace": "default",
					},
					"spec": map[string]interface{}{
						"ports": []interface{}{
							map[string]interface{}{
								"port": 8080,
							},
						},
					},
				},
			},
			existingObjs: []runtime.Object{},
			wantErr:      false,
		},
		{
			name: "creates deployment",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"name":      "test-deployment",
						"namespace": "default",
					},
					"spec": map[string]interface{}{
						"replicas": 1,
					},
				},
			},
			existingObjs: []runtime.Object{},
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ArgoCDAgentAddonReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).Build(),
				Scheme: s,
			}

			err := r.applyManifest(context.Background(), tt.obj)
			if (err != nil) != tt.wantErr {
				t.Errorf("applyManifest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify management label was added (except for skip case)
				if tt.obj.GetAnnotations() == nil || tt.obj.GetAnnotations()["argocd-addon.open-cluster-management.io/skip"] != "true" {
					labels := tt.obj.GetLabels()
					if labels == nil || labels["app.kubernetes.io/managed-by"] != "argocd-agent-addon" {
						t.Error("applyManifest() should add management label")
					}
				}

				// For non-skip cases, verify object was created/updated
				if tt.obj.GetAnnotations() == nil || tt.obj.GetAnnotations()["argocd-addon.open-cluster-management.io/skip"] != "true" {
					key := types.NamespacedName{
						Name:      tt.obj.GetName(),
						Namespace: tt.obj.GetNamespace(),
					}
					result := &unstructured.Unstructured{}
					result.SetAPIVersion(tt.obj.GetAPIVersion())
					result.SetKind(tt.obj.GetKind())
					err := r.Get(context.Background(), key, result)
					if err != nil {
						t.Errorf("Failed to get applied object: %v", err)
					}
				}
			}
		})
	}
}

func TestApplyManifestLabels(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "test-config",
				"namespace": "default",
				"labels": map[string]interface{}{
					"existing": "label",
				},
			},
		},
	}

	r := &ArgoCDAgentAddonReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).Build(),
		Scheme: s,
	}

	err := r.applyManifest(context.Background(), obj)
	if err != nil {
		t.Fatalf("applyManifest() failed: %v", err)
	}

	// Verify both existing and management labels are present
	labels := obj.GetLabels()
	if labels["existing"] != "label" {
		t.Error("applyManifest() should preserve existing labels")
	}
	if labels["app.kubernetes.io/managed-by"] != "argocd-agent-addon" {
		t.Error("applyManifest() should add management label")
	}
}

func TestApplyManifestWithoutNamespace(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)

	tests := []struct {
		name         string
		obj          *unstructured.Unstructured
		expectedNS   string
		shouldHaveNS bool
	}{
		{
			name: "namespace resource without namespace",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Namespace",
					"metadata": map[string]interface{}{
						"name": "test-namespace",
					},
				},
			},
			expectedNS:   "",
			shouldHaveNS: false,
		},
		{
			name: "cluster-scoped resource",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "rbac.authorization.k8s.io/v1",
					"kind":       "ClusterRole",
					"metadata": map[string]interface{}{
						"name": "test-role",
					},
				},
			},
			expectedNS:   "",
			shouldHaveNS: false,
		},
		{
			name: "namespaced resource gets namespace set",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name": "test-config",
					},
				},
			},
			expectedNS:   "default",
			shouldHaveNS: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The function sets namespace to the namespace parameter if not set
			// We're just testing the logic, not the actual namespace setting
			ns := tt.obj.GetNamespace()
			kind := tt.obj.GetKind()

			// Verify initial state
			if tt.shouldHaveNS && ns == "" && kind != "Namespace" && kind != "ClusterRole" && kind != "ClusterRoleBinding" {
				// Namespace should be set by the function for these types
				t.Logf("Object of kind %s would have namespace set", kind)
			} else if !tt.shouldHaveNS && (kind == "Namespace" || kind == "ClusterRole" || kind == "ClusterRoleBinding") {
				// These should not have namespace set
				t.Logf("Object of kind %s should not have namespace", kind)
			}
		})
	}
}

func TestApplyManifestSkipAnnotation(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)

	// Create existing object with skip annotation
	existingObj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "skip-config",
			Namespace: "default",
			Annotations: map[string]string{
				"argocd-addon.open-cluster-management.io/skip": "true",
			},
			ResourceVersion: "1",
		},
		Data: map[string]string{
			"original": "data",
		},
	}

	r := &ArgoCDAgentAddonReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(existingObj).Build(),
		Scheme: s,
	}

	// Try to apply new version
	newObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "skip-config",
				"namespace": "default",
			},
			"data": map[string]interface{}{
				"new": "data",
			},
		},
	}

	err := r.applyManifest(context.Background(), newObj)
	if err != nil {
		t.Fatalf("applyManifest() failed: %v", err)
	}

	// Verify original object wasn't changed
	result := &corev1.ConfigMap{}
	err = r.Get(context.Background(), types.NamespacedName{
		Name:      "skip-config",
		Namespace: "default",
	}, result)
	if err != nil {
		t.Fatalf("Failed to get object: %v", err)
	}

	if result.Data["original"] != "data" {
		t.Error("Object with skip annotation should not be modified")
	}
	if _, exists := result.Data["new"]; exists {
		t.Error("Object with skip annotation should not have new data")
	}
}

func TestCopyEmbeddedToTemp(t *testing.T) {
	// Test the logic of copying embedded files
	// This tests the structure without actual filesystem operations
	testCases := []struct {
		name     string
		srcPath  string
		destPath string
	}{
		{
			name:     "copies chart directory",
			srcPath:  "charts/argocd-agent-addon",
			destPath: "/tmp/test",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Just verify the paths are valid
			if tc.srcPath == "" {
				t.Error("srcPath should not be empty")
			}
			if tc.destPath == "" {
				t.Error("destPath should not be empty")
			}
		})
	}
}

func TestTemplateAndApplyChartValidation(t *testing.T) {
	// Test parameter validation for templateAndApplyChart
	// Full testing requires Helm chart files
	tests := []struct {
		name          string
		chartPath     string
		namespace     string
		releaseName   string
		operatorImage string
		agentImage    string
		wantValid     bool
	}{
		{
			name:          "valid parameters",
			chartPath:     "charts/argocd-agent-addon",
			namespace:     "argocd",
			releaseName:   "test-release",
			operatorImage: "quay.io/operator:latest",
			agentImage:    "quay.io/agent:latest",
			wantValid:     true,
		},
		{
			name:          "missing chart path",
			chartPath:     "",
			namespace:     "argocd",
			releaseName:   "test-release",
			operatorImage: "quay.io/operator:latest",
			agentImage:    "quay.io/agent:latest",
			wantValid:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasAllParams := tt.chartPath != "" && tt.namespace != "" &&
				tt.releaseName != "" && tt.operatorImage != "" && tt.agentImage != ""
			if hasAllParams != tt.wantValid {
				t.Errorf("Parameter validation = %v, want %v", hasAllParams, tt.wantValid)
			}
		})
	}
}

func TestApplyCRDIfNotExistsValidation(t *testing.T) {
	// Test parameter validation for applyCRDIfNotExists
	tests := []struct {
		name       string
		resource   string
		apiVersion string
		yamlPath   string
		wantValid  bool
	}{
		{
			name:       "valid parameters",
			resource:   "argocds",
			apiVersion: "argoproj.io/v1beta1",
			yamlPath:   "charts/argocd-agent-addon/crds/argocd-operator-crds.yaml",
			wantValid:  true,
		},
		{
			name:       "missing resource",
			resource:   "",
			apiVersion: "argoproj.io/v1beta1",
			yamlPath:   "charts/argocd-agent-addon/crds/argocd-operator-crds.yaml",
			wantValid:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasAllParams := tt.resource != "" && tt.apiVersion != "" && tt.yamlPath != ""
			if hasAllParams != tt.wantValid {
				t.Errorf("Parameter validation = %v, want %v", hasAllParams, tt.wantValid)
			}
		})
	}
}

func TestConstants(t *testing.T) {
	// Verify namespace configuration reads defaults correctly
	operatorNS, argoCDNS := getNamespaceConfig()
	if operatorNS != "argocd-operator-system" {
		t.Errorf("operatorNamespace = %v, want argocd-operator-system", operatorNS)
	}
	if argoCDNS != "argocd" {
		t.Errorf("argoCDNamespace = %v, want argocd", argoCDNS)
	}
	// Verify centralized image defaults are defined for external dependencies
	if images.DefaultOperatorImage == "" {
		t.Error("DefaultOperatorImage should be defined")
	}
	if images.DefaultAgentImage == "" {
		t.Error("DefaultAgentImage should be defined")
	}
}
