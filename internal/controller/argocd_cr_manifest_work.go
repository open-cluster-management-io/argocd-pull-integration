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
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	appsv1alpha1 "open-cluster-management.io/argocd-pull-integration/api/v1alpha1"
)

const (
	ArgoCDCRManifestWorkPrefix = "argocd-agent-cr"
)

// getArgoCDCRManifestWorkName returns the ManifestWork name for the ArgoCD CR.
// Includes both namespace and name to guarantee uniqueness when multiple GitOpsClusters
// exist in the same namespace. Truncates to respect the 63-character Kubernetes name limit.
func getArgoCDCRManifestWorkName(gitOpsClusterNamespace, gitOpsClusterName string) string {
	name := fmt.Sprintf("%s-%s-%s", ArgoCDCRManifestWorkPrefix, gitOpsClusterNamespace, gitOpsClusterName)
	if len(name) <= 63 {
		return name
	}
	// Truncate and append a short hash for uniqueness
	suffix := fmt.Sprintf("%x", sha256.Sum256([]byte(name)))[:8]
	return name[:63-9] + "-" + suffix
}

// createArgoCDCRManifestWork creates or updates the ManifestWork that delivers the ArgoCD CR to a managed cluster
func (r *GitOpsClusterReconciler) createArgoCDCRManifestWork(
	ctx context.Context,
	gitOpsCluster *appsv1alpha1.GitOpsCluster,
	managedCluster *clusterv1.ManagedCluster,
	serverAddress, serverPort string) error {

	if isLocalCluster(managedCluster) {
		klog.V(2).InfoS("Skipping ArgoCD CR ManifestWork for local-cluster", "cluster", managedCluster.Name)
		return nil
	}

	spokeArgoCDNamespace := "argocd"
	if gitOpsCluster.Spec.ArgoCDAgentAddon.AgentNamespace != "" {
		spokeArgoCDNamespace = gitOpsCluster.Spec.ArgoCDAgentAddon.AgentNamespace
	}

	var manifests []workv1.Manifest

	if gitOpsCluster.Spec.ArgoCDAgentAddon.ArgoCDCRManifestWork != nil {
		// User-provided ArgoCD CR manifests - inject server address/port if not already set
		for _, rawExt := range gitOpsCluster.Spec.ArgoCDAgentAddon.ArgoCDCRManifestWork.Manifests {
			injectedRaw, err := injectServerEndpoint(rawExt.Raw, serverAddress, serverPort)
			if err != nil {
				klog.V(2).InfoS("Could not inject server endpoint into manifest, using as-is", "error", err)
				manifests = append(manifests, workv1.Manifest{RawExtension: rawExt})
			} else {
				manifests = append(manifests, workv1.Manifest{
					RawExtension: runtime.RawExtension{Raw: injectedRaw},
				})
			}
		}
	} else {
		// Auto-generate default ArgoCD CR
		defaultCR := r.buildDefaultArgoCDCR(gitOpsCluster, spokeArgoCDNamespace, serverAddress, serverPort)
		crJSON, err := json.Marshal(defaultCR)
		if err != nil {
			return fmt.Errorf("failed to marshal default ArgoCD CR: %w", err)
		}
		manifests = []workv1.Manifest{
			{
				RawExtension: runtime.RawExtension{Raw: crJSON},
			},
		}
	}

	manifestWorkName := getArgoCDCRManifestWorkName(gitOpsCluster.Namespace, gitOpsCluster.Name)
	manifestWork := &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      manifestWorkName,
			Namespace: managedCluster.Name,
			Labels: map[string]string{
				"apps.open-cluster-management.io/gitopscluster": "true",
				"app.kubernetes.io/managed-by":                  "argocd-pull-integration",
				"app.kubernetes.io/component":                   "argocd-cr",
			},
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: manifests,
			},
		},
	}

	existing := &workv1.ManifestWork{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      manifestWorkName,
		Namespace: managedCluster.Name,
	}, existing)

	if err != nil {
		if k8serrors.IsNotFound(err) {
			if err := r.Create(ctx, manifestWork); err != nil {
				return fmt.Errorf("failed to create ArgoCD CR ManifestWork for cluster %s: %w", managedCluster.Name, err)
			}
			klog.InfoS("Created ArgoCD CR ManifestWork", "cluster", managedCluster.Name, "name", manifestWorkName)
			return nil
		}
		return fmt.Errorf("failed to get ArgoCD CR ManifestWork for cluster %s: %w", managedCluster.Name, err)
	}

	// Check if update is needed by comparing spec and labels
	needsUpdate := !equality.Semantic.DeepEqual(existing.Spec, manifestWork.Spec)
	for k, v := range manifestWork.Labels {
		if existing.Labels[k] != v {
			needsUpdate = true
			break
		}
	}

	if !needsUpdate {
		klog.V(4).InfoS("ArgoCD CR ManifestWork already up-to-date", "cluster", managedCluster.Name, "name", manifestWorkName)
		return nil
	}

	// Update existing ManifestWork (merge labels to preserve any externally-added labels)
	existing.Spec = manifestWork.Spec
	if existing.Labels == nil {
		existing.Labels = make(map[string]string)
	}
	for k, v := range manifestWork.Labels {
		existing.Labels[k] = v
	}
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update ArgoCD CR ManifestWork for cluster %s: %w", managedCluster.Name, err)
	}
	klog.V(2).InfoS("Updated ArgoCD CR ManifestWork", "cluster", managedCluster.Name, "name", manifestWorkName)
	return nil
}

// buildDefaultArgoCDCR generates the default ArgoCD CR for agent mode
func (r *GitOpsClusterReconciler) buildDefaultArgoCDCR(
	gitOpsCluster *appsv1alpha1.GitOpsCluster,
	spokeArgoCDNamespace, serverAddress, serverPort string) *unstructured.Unstructured {

	mode := gitOpsCluster.Spec.ArgoCDAgentAddon.Mode
	if mode == "" {
		mode = "managed"
	}

	cr := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "argoproj.io/v1beta1",
			"kind":       "ArgoCD",
			"metadata": map[string]interface{}{
				"name":      "argocd",
				"namespace": spokeArgoCDNamespace,
			},
			"spec": map[string]interface{}{
				"server": map[string]interface{}{
					"enabled": false,
				},
				"sourceNamespaces": []interface{}{"*"},
				"argoCDAgent": map[string]interface{}{
					"agent": map[string]interface{}{
						"enabled":           true,
						"allowedNamespaces": []interface{}{"*"},
						"destinationBasedMapping": map[string]interface{}{
							"enabled":         true,
							"createNamespace": true,
						},
						"creds":     "mtls:open-cluster-management:cluster:([^:]+):addon:argocd-agent",
						"logLevel":  "info",
						"logFormat": "text",
						"client": map[string]interface{}{
							"principalServerAddress": serverAddress,
							"principalServerPort":    serverPort,
							"mode":                   mode,
							"enableWebSocket":        false,
							"enableCompression":      false,
							"keepAliveInterval":      "50s",
						},
						"tls": map[string]interface{}{
							"secretName":       "argocd-agent-open-cluster-management.io-argocd-agent-addon-client-cert",
							"rootCASecretName": "argocd-agent-ca",
							"insecure":         false,
						},
						"redis": map[string]interface{}{
							"serverAddress": "argocd-redis:6379",
						},
					},
				},
			},
		},
	}

	return cr
}

// injectServerEndpoint sets principalServerAddress and principalServerPort on an ArgoCD CR
// only when they are absent or empty. User-provided values are preserved.
// It handles both the structured path (spec.argoCDAgent.agent.client) and the extraConfig path.
// Non-ArgoCD manifests are returned as-is.
func injectServerEndpoint(raw []byte, serverAddress, serverPort string) ([]byte, error) {
	if raw == nil {
		return nil, fmt.Errorf("nil raw data")
	}

	var obj unstructured.Unstructured
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw, err
	}

	if obj.GetAPIVersion() != "argoproj.io/v1beta1" || obj.GetKind() != "ArgoCD" {
		return raw, nil
	}

	spec, ok := obj.Object["spec"].(map[string]interface{})
	if !ok {
		spec = map[string]interface{}{}
		obj.Object["spec"] = spec
	}

	// Try structured path first: spec.argoCDAgent.agent.client
	if argoCDAgent, ok := spec["argoCDAgent"].(map[string]interface{}); ok {
		if agent, ok := argoCDAgent["agent"].(map[string]interface{}); ok {
			client, ok := agent["client"].(map[string]interface{})
			if !ok {
				client = map[string]interface{}{}
				agent["client"] = client
			}
			if existing, ok := client["principalServerAddress"].(string); !ok || existing == "" {
				client["principalServerAddress"] = serverAddress
			}
			if existing, ok := client["principalServerPort"].(string); !ok || existing == "" {
				client["principalServerPort"] = serverPort
			}
			return json.Marshal(obj.Object)
		}
	}

	// Fallback: inject into spec.extraConfig
	extraConfig, ok := spec["extraConfig"].(map[string]interface{})
	if !ok {
		extraConfig = map[string]interface{}{}
		spec["extraConfig"] = extraConfig
	}
	if existing, ok := extraConfig["agent.principal.server.address"].(string); !ok || existing == "" {
		extraConfig["agent.principal.server.address"] = serverAddress
	}
	if existing, ok := extraConfig["agent.principal.server.port"].(string); !ok || existing == "" {
		extraConfig["agent.principal.server.port"] = serverPort
	}

	return json.Marshal(obj.Object)
}

// validateArgoCDCRManifestWork validates that the user-provided manifests contain an ArgoCD CR
func validateArgoCDCRManifestWork(spec *appsv1alpha1.ArgoCDCRManifestWorkSpec) error {
	if spec == nil {
		return nil
	}

	if len(spec.Manifests) == 0 {
		return fmt.Errorf("argoCDCRManifestWork.manifests must contain at least one manifest")
	}

	foundArgoCDCR := false
	for i, rawExt := range spec.Manifests {
		if rawExt.Raw == nil {
			return fmt.Errorf("manifest at index %d has nil raw data", i)
		}

		var obj unstructured.Unstructured
		if err := json.Unmarshal(rawExt.Raw, &obj); err != nil {
			return fmt.Errorf("failed to unmarshal manifest at index %d: %w", i, err)
		}

		if obj.GetAPIVersion() == "argoproj.io/v1beta1" && obj.GetKind() == "ArgoCD" {
			foundArgoCDCR = true
		}
	}

	if !foundArgoCDCR {
		return fmt.Errorf("argoCDCRManifestWork.manifests must contain at least one ArgoCD CR (apiVersion: argoproj.io/v1beta1, kind: ArgoCD)")
	}

	return nil
}

// deleteArgoCDCRManifestWork deletes the ArgoCD CR ManifestWork for a managed cluster
func (r *GitOpsClusterReconciler) deleteArgoCDCRManifestWork(ctx context.Context, clusterName, gitOpsClusterNamespace, gitOpsClusterName string) error {
	manifestWorkName := getArgoCDCRManifestWorkName(gitOpsClusterNamespace, gitOpsClusterName)
	manifestWork := &workv1.ManifestWork{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      manifestWorkName,
		Namespace: clusterName,
	}, manifestWork)

	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get ArgoCD CR ManifestWork: %w", err)
	}

	if err := r.Delete(ctx, manifestWork); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete ArgoCD CR ManifestWork: %w", err)
	}

	klog.InfoS("Deleted ArgoCD CR ManifestWork", "cluster", clusterName, "name", manifestWorkName)
	return nil
}
