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
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	appsv1alpha1 "open-cluster-management.io/argocd-pull-integration/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestValidateArgoCDCRManifestWork(t *testing.T) {
	validArgoCDCR := map[string]interface{}{
		"apiVersion": "argoproj.io/v1beta1",
		"kind":       "ArgoCD",
		"metadata": map[string]interface{}{
			"name":      "argocd",
			"namespace": "argocd",
		},
		"spec": map[string]interface{}{
			"server": map[string]interface{}{
				"enabled": false,
			},
		},
	}
	validJSON, _ := json.Marshal(validArgoCDCR)

	invalidKindCR := map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]interface{}{
			"name":      "test",
			"namespace": "default",
		},
	}
	invalidJSON, _ := json.Marshal(invalidKindCR)

	tests := []struct {
		name    string
		spec    *appsv1alpha1.ArgoCDCRManifestWorkSpec
		wantErr bool
	}{
		{
			name:    "nil spec is valid",
			spec:    nil,
			wantErr: false,
		},
		{
			name: "valid spec with ArgoCD CR",
			spec: &appsv1alpha1.ArgoCDCRManifestWorkSpec{
				Manifests: []runtime.RawExtension{
					{Raw: validJSON},
				},
			},
			wantErr: false,
		},
		{
			name: "empty manifests list",
			spec: &appsv1alpha1.ArgoCDCRManifestWorkSpec{
				Manifests: []runtime.RawExtension{},
			},
			wantErr: true,
		},
		{
			name: "manifests without ArgoCD CR",
			spec: &appsv1alpha1.ArgoCDCRManifestWorkSpec{
				Manifests: []runtime.RawExtension{
					{Raw: invalidJSON},
				},
			},
			wantErr: true,
		},
		{
			name: "manifest with nil raw data",
			spec: &appsv1alpha1.ArgoCDCRManifestWorkSpec{
				Manifests: []runtime.RawExtension{
					{Raw: nil},
				},
			},
			wantErr: true,
		},
		{
			name: "multiple manifests including ArgoCD CR",
			spec: &appsv1alpha1.ArgoCDCRManifestWorkSpec{
				Manifests: []runtime.RawExtension{
					{Raw: invalidJSON},
					{Raw: validJSON},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateArgoCDCRManifestWork(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateArgoCDCRManifestWork() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInjectServerEndpoint(t *testing.T) {
	makeJSON := func(obj map[string]interface{}) []byte {
		b, _ := json.Marshal(obj)
		return b
	}

	tests := []struct {
		name          string
		raw           []byte
		wantAddress   string
		wantPort      string
		wantErr       bool
		checkPath     string // "structured" or "extraConfig"
	}{
		{
			name: "injects into empty client",
			raw: makeJSON(map[string]interface{}{
				"apiVersion": "argoproj.io/v1beta1",
				"kind":       "ArgoCD",
				"spec": map[string]interface{}{
					"argoCDAgent": map[string]interface{}{
						"agent": map[string]interface{}{
							"client": map[string]interface{}{},
						},
					},
				},
			}),
			wantAddress: "10.0.0.1",
			wantPort:    "443",
			checkPath:   "structured",
		},
		{
			name: "preserves user-provided address and port",
			raw: makeJSON(map[string]interface{}{
				"apiVersion": "argoproj.io/v1beta1",
				"kind":       "ArgoCD",
				"spec": map[string]interface{}{
					"argoCDAgent": map[string]interface{}{
						"agent": map[string]interface{}{
							"client": map[string]interface{}{
								"principalServerAddress": "user-lb.example.com",
								"principalServerPort":    "8443",
							},
						},
					},
				},
			}),
			wantAddress: "user-lb.example.com",
			wantPort:    "8443",
			checkPath:   "structured",
		},
		{
			name: "non-ArgoCD manifest returned as-is",
			raw: makeJSON(map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata":   map[string]interface{}{"name": "test"},
			}),
			wantErr:   false,
			checkPath: "skip",
		},
		{
			name:    "nil raw returns error",
			raw:     nil,
			wantErr: true,
		},
		{
			name: "falls back to extraConfig when no structured path",
			raw: makeJSON(map[string]interface{}{
				"apiVersion": "argoproj.io/v1beta1",
				"kind":       "ArgoCD",
				"spec":       map[string]interface{}{},
			}),
			wantAddress: "10.0.0.1",
			wantPort:    "443",
			checkPath:   "extraConfig",
		},
		{
			name: "preserves user-provided extraConfig values",
			raw: makeJSON(map[string]interface{}{
				"apiVersion": "argoproj.io/v1beta1",
				"kind":       "ArgoCD",
				"spec": map[string]interface{}{
					"extraConfig": map[string]interface{}{
						"agent.principal.server.address": "custom-addr.example.com",
						"agent.principal.server.port":    "9443",
					},
				},
			}),
			wantAddress: "custom-addr.example.com",
			wantPort:    "9443",
			checkPath:   "extraConfig",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := injectServerEndpoint(tt.raw, "10.0.0.1", "443")
			if (err != nil) != tt.wantErr {
				t.Fatalf("injectServerEndpoint() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr || tt.checkPath == "skip" {
				return
			}

			var obj map[string]interface{}
			if err := json.Unmarshal(result, &obj); err != nil {
				t.Fatalf("failed to unmarshal result: %v", err)
			}

			spec := obj["spec"].(map[string]interface{})

			switch tt.checkPath {
			case "structured":
				client := spec["argoCDAgent"].(map[string]interface{})["agent"].(map[string]interface{})["client"].(map[string]interface{})
				if client["principalServerAddress"] != tt.wantAddress {
					t.Errorf("principalServerAddress = %v, want %v", client["principalServerAddress"], tt.wantAddress)
				}
				if client["principalServerPort"] != tt.wantPort {
					t.Errorf("principalServerPort = %v, want %v", client["principalServerPort"], tt.wantPort)
				}
			case "extraConfig":
				ec := spec["extraConfig"].(map[string]interface{})
				if ec["agent.principal.server.address"] != tt.wantAddress {
					t.Errorf("agent.principal.server.address = %v, want %v", ec["agent.principal.server.address"], tt.wantAddress)
				}
				if ec["agent.principal.server.port"] != tt.wantPort {
					t.Errorf("agent.principal.server.port = %v, want %v", ec["agent.principal.server.port"], tt.wantPort)
				}
			}
		})
	}
}

func TestBuildDefaultArgoCDCR(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = appsv1alpha1.AddToScheme(s)

	r := &GitOpsClusterReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).Build(),
		Scheme: s,
	}

	tests := []struct {
		name                 string
		gitOpsCluster        *appsv1alpha1.GitOpsCluster
		spokeArgoCDNamespace string
		serverAddress        string
		serverPort           string
		wantNamespace        string
		wantMode             string
	}{
		{
			name: "default mode",
			gitOpsCluster: &appsv1alpha1.GitOpsCluster{
				Spec: appsv1alpha1.GitOpsClusterSpec{
					ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{},
				},
			},
			spokeArgoCDNamespace: "argocd",
			serverAddress:        "10.0.0.1",
			serverPort:           "443",
			wantNamespace:        "argocd",
			wantMode:             "managed",
		},
		{
			name: "autonomous mode",
			gitOpsCluster: &appsv1alpha1.GitOpsCluster{
				Spec: appsv1alpha1.GitOpsClusterSpec{
					ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{
						Mode: "autonomous",
					},
				},
			},
			spokeArgoCDNamespace: "custom-argocd",
			serverAddress:        "lb.example.com",
			serverPort:           "8443",
			wantNamespace:        "custom-argocd",
			wantMode:             "autonomous",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := r.buildDefaultArgoCDCR(tt.gitOpsCluster, tt.spokeArgoCDNamespace, tt.serverAddress, tt.serverPort)

			if cr.GetAPIVersion() != "argoproj.io/v1beta1" {
				t.Errorf("Expected apiVersion argoproj.io/v1beta1, got %s", cr.GetAPIVersion())
			}
			if cr.GetKind() != "ArgoCD" {
				t.Errorf("Expected kind ArgoCD, got %s", cr.GetKind())
			}
			if cr.GetNamespace() != tt.wantNamespace {
				t.Errorf("Expected namespace %s, got %s", tt.wantNamespace, cr.GetNamespace())
			}
			if cr.GetName() != "argocd" {
				t.Errorf("Expected name argocd, got %s", cr.GetName())
			}

			// Verify agent config
			spec, ok := cr.Object["spec"].(map[string]interface{})
			if !ok {
				t.Fatal("spec is not a map")
			}
			argoCDAgent, ok := spec["argoCDAgent"].(map[string]interface{})
			if !ok {
				t.Fatal("argoCDAgent is not a map")
			}
			agent, ok := argoCDAgent["agent"].(map[string]interface{})
			if !ok {
				t.Fatal("agent is not a map")
			}
			client, ok := agent["client"].(map[string]interface{})
			if !ok {
				t.Fatal("client is not a map")
			}

			if client["principalServerAddress"] != tt.serverAddress {
				t.Errorf("Expected serverAddress %s, got %v", tt.serverAddress, client["principalServerAddress"])
			}
			if client["principalServerPort"] != tt.serverPort {
				t.Errorf("Expected serverPort %s, got %v", tt.serverPort, client["principalServerPort"])
			}
			if client["mode"] != tt.wantMode {
				t.Errorf("Expected mode %s, got %v", tt.wantMode, client["mode"])
			}
		})
	}
}

func TestCreateArgoCDCRManifestWork(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = appsv1alpha1.AddToScheme(s)
	_ = clusterv1.Install(s)
	_ = workv1.Install(s)

	validArgoCDCR := map[string]interface{}{
		"apiVersion": "argoproj.io/v1beta1",
		"kind":       "ArgoCD",
		"metadata": map[string]interface{}{
			"name":      "argocd",
			"namespace": "argocd",
		},
		"spec": map[string]interface{}{},
	}
	validJSON, _ := json.Marshal(validArgoCDCR)

	tests := []struct {
		name          string
		gitOpsCluster *appsv1alpha1.GitOpsCluster
		cluster       *clusterv1.ManagedCluster
		wantErr       bool
		wantCreated   bool
	}{
		{
			name: "creates ManifestWork with auto-generated CR when spec is nil",
			gitOpsCluster: &appsv1alpha1.GitOpsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "argocd",
				},
				Spec: appsv1alpha1.GitOpsClusterSpec{
					ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{
						PrincipalServerAddress: "10.0.0.1",
						PrincipalServerPort:    "443",
					},
				},
			},
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster1"},
			},
			wantErr:     false,
			wantCreated: true,
		},
		{
			name: "creates ManifestWork with user-provided CR",
			gitOpsCluster: &appsv1alpha1.GitOpsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "argocd",
				},
				Spec: appsv1alpha1.GitOpsClusterSpec{
					ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{
						ArgoCDCRManifestWork: &appsv1alpha1.ArgoCDCRManifestWorkSpec{
							Manifests: []runtime.RawExtension{
								{Raw: validJSON},
							},
						},
					},
				},
			},
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster1"},
			},
			wantErr:     false,
			wantCreated: true,
		},
		{
			name: "skips local-cluster",
			gitOpsCluster: &appsv1alpha1.GitOpsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "argocd",
				},
				Spec: appsv1alpha1.GitOpsClusterSpec{
					ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{},
				},
			},
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "local-cluster"},
			},
			wantErr:     false,
			wantCreated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).Build(),
				Scheme: s,
			}

			err := r.createArgoCDCRManifestWork(context.TODO(), tt.gitOpsCluster, tt.cluster, "10.0.0.1", "443")
			if (err != nil) != tt.wantErr {
				t.Errorf("createArgoCDCRManifestWork() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantCreated {
				mw := &workv1.ManifestWork{}
				mwName := getArgoCDCRManifestWorkName(tt.gitOpsCluster.Namespace, tt.gitOpsCluster.Name)
				err := r.Get(context.TODO(), types.NamespacedName{
					Name:      mwName,
					Namespace: tt.cluster.Name,
				}, mw)
				if err != nil {
					t.Errorf("Expected ManifestWork to be created, got error: %v", err)
				}
				if len(mw.Spec.Workload.Manifests) == 0 {
					t.Error("Expected ManifestWork to have at least one manifest")
				}
			} else {
				mw := &workv1.ManifestWork{}
				mwName := getArgoCDCRManifestWorkName(tt.gitOpsCluster.Namespace, tt.gitOpsCluster.Name)
				err := r.Get(context.TODO(), types.NamespacedName{
					Name:      mwName,
					Namespace: tt.cluster.Name,
				}, mw)
				if err == nil {
					t.Error("Expected no ManifestWork to be created for local-cluster")
				}
			}
		})
	}
}

func TestDeleteArgoCDCRManifestWork(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = workv1.Install(s)

	existingMW := &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getArgoCDCRManifestWorkName("argocd", "test"),
			Namespace: "cluster1",
		},
	}

	tests := []struct {
		name        string
		clusterName string
		namespace   string
		existingMW  bool
		wantErr     bool
	}{
		{
			name:        "deletes existing ManifestWork",
			clusterName: "cluster1",
			namespace:   "argocd",
			existingMW:  true,
			wantErr:     false,
		},
		{
			name:        "no error when ManifestWork does not exist",
			clusterName: "cluster2",
			namespace:   "argocd",
			existingMW:  false,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(s)
			if tt.existingMW {
				builder = builder.WithObjects(existingMW.DeepCopy())
			}

			r := &GitOpsClusterReconciler{
				Client: builder.Build(),
				Scheme: s,
			}

			err := r.deleteArgoCDCRManifestWork(context.TODO(), tt.clusterName, tt.namespace, "test")
			if (err != nil) != tt.wantErr {
				t.Errorf("deleteArgoCDCRManifestWork() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.existingMW && err == nil {
				mw := &workv1.ManifestWork{}
				getErr := r.Get(context.TODO(), types.NamespacedName{
					Name:      getArgoCDCRManifestWorkName(tt.namespace, "test"),
					Namespace: tt.clusterName,
				}, mw)
				if getErr == nil {
					t.Error("Expected ManifestWork to be deleted")
				}
			}
		})
	}
}

func TestGetArgoCDCRManifestWorkName(t *testing.T) {
	tests := []struct {
		namespace string
		name      string
		want      string
	}{
		{"argocd", "gitops-cluster", "argocd-agent-cr-argocd-gitops-cluster"},
		{"custom-ns", "my-cluster", "argocd-agent-cr-custom-ns-my-cluster"},
	}

	for _, tt := range tests {
		got := getArgoCDCRManifestWorkName(tt.namespace, tt.name)
		if got != tt.want {
			t.Errorf("getArgoCDCRManifestWorkName(%s, %s) = %s, want %s", tt.namespace, tt.name, got, tt.want)
		}
	}
}
