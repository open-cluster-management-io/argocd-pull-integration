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
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	operatorNamespace = "argocd-operator-system"
	argoCDNamespace   = "argocd"
)

// Default images (used when not specified)
const (
	defaultOperatorImage = "quay.io/mikeshng/argocd-operator:latest"
	defaultAgentImage    = "ghcr.io/argoproj-labs/argocd-agent/argocd-agent:latest"
)

// ensureNamespace creates a namespace if it doesn't exist
func (r *ArgoCDAgentAddonReconciler) ensureNamespace(ctx context.Context, namespaceName string) error {
	namespace := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: namespaceName}, namespace)

	if err == nil {
		// Namespace exists, check if we should skip it
		annotations := namespace.GetAnnotations()
		if annotations != nil && annotations["argocd-addon.open-cluster-management.io/skip"] == "true" {
			klog.V(1).Infof("Skipping namespace %s due to skip annotation", namespaceName)
			return nil
		}

		// Update labels if needed
		needsUpdate := false
		if namespace.Labels == nil {
			namespace.Labels = make(map[string]string)
			needsUpdate = true
		}

		expectedLabels := map[string]string{
			"addon.open-cluster-management.io/namespace":   "true",
			"apps.open-cluster-management.io/argocd-addon": "true",
			"app.kubernetes.io/managed-by":                 "argocd-agent-addon",
		}

		for key, value := range expectedLabels {
			if namespace.Labels[key] != value {
				namespace.Labels[key] = value
				needsUpdate = true
			}
		}

		if needsUpdate {
			if err := r.Update(ctx, namespace); err != nil {
				return fmt.Errorf("failed to update namespace %s: %w", namespaceName, err)
			}
			klog.V(1).Infof("Updated namespace %s labels", namespaceName)
		}

		return nil
	}

	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get namespace %s: %w", namespaceName, err)
	}

	// Namespace doesn't exist, create it
	namespace = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespaceName,
			Labels: map[string]string{
				"addon.open-cluster-management.io/namespace":   "true",
				"apps.open-cluster-management.io/argocd-addon": "true",
				"app.kubernetes.io/managed-by":                 "argocd-agent-addon",
			},
		},
	}

	if err := r.Create(ctx, namespace); err != nil {
		if errors.IsAlreadyExists(err) {
			klog.V(1).Infof("Namespace %s was created by another process", namespaceName)
			return nil
		}
		return fmt.Errorf("failed to create namespace %s: %w", namespaceName, err)
	}

	klog.V(1).Infof("Created namespace %s", namespaceName)
	return nil
}

// installOrUpdateArgoCDAgent orchestrates the ArgoCD agent installation process
func (r *ArgoCDAgentAddonReconciler) installOrUpdateArgoCDAgent(ctx context.Context) error {
	klog.V(1).Info("Templating and applying ArgoCD agent addon")

	// Set defaults if images are not provided
	operatorImage := r.ArgoCDOperatorImage
	if operatorImage == "" {
		operatorImage = defaultOperatorImage
		klog.V(1).Infof("Using default operator image: %s", operatorImage)
	}

	agentImage := r.ArgoCDAgentImage
	if agentImage == "" {
		agentImage = defaultAgentImage
		klog.V(1).Infof("Using default agent image: %s", agentImage)
	}

	// 1. Apply CRDs if they don't exist
	if err := r.applyCRDIfNotExists(ctx, "argocds", "argoproj.io/v1beta1", "charts/argocd-agent-addon/crds/argocd-operator-crds.yaml"); err != nil {
		return fmt.Errorf("failed to apply ArgoCD CRDs: %w", err)
	}

	// 2. Create operator namespace
	if err := r.ensureNamespace(ctx, operatorNamespace); err != nil {
		return fmt.Errorf("failed to create operator namespace: %w", err)
	}

	// 3. Create ArgoCD namespace
	if err := r.ensureNamespace(ctx, argoCDNamespace); err != nil {
		return fmt.Errorf("failed to create ArgoCD namespace: %w", err)
	}

	// 4. Template and apply ArgoCD operator with the resolved images
	if err := r.templateAndApplyChart(ctx, "charts/argocd-agent-addon", operatorNamespace, "argocd-agent-addon", operatorImage, agentImage); err != nil {
		return fmt.Errorf("failed to template and apply ArgoCD operator: %w", err)
	}

	// 5. Copy OCM client certificate to ArgoCD namespace
	if err := r.copyClientCertificate(ctx); err != nil {
		klog.Warningf("Failed to copy client certificate (will retry): %v", err)
		// Don't fail the reconciliation - the certificate might not be ready yet
	}

	klog.Info("Successfully installed/updated ArgoCD agent addon")
	return nil
}

// ParseImageReference parses an image reference into repository and tag
func ParseImageReference(imageRef string) (string, string, error) {
	// First try to parse as image@digest format
	if strings.Contains(imageRef, "@") {
		parts := strings.Split(imageRef, "@")
		if len(parts) == 2 {
			return parts[0], parts[1], nil
		}
	}

	// Try to parse as image:tag format
	if strings.Contains(imageRef, ":") {
		// Find the last colon to handle registry URLs with ports
		lastColonIndex := strings.LastIndex(imageRef, ":")
		if lastColonIndex != -1 {
			repository := imageRef[:lastColonIndex]
			tag := imageRef[lastColonIndex+1:]

			// Check if this might be a port number instead of a tag
			// Port numbers are typically numeric and shorter
			if !strings.Contains(tag, "/") && len(tag) < 10 {
				// This looks like it could be a tag
				return repository, tag, nil
			}
		}
	}

	// If no tag or digest found, assume "latest"
	return imageRef, "latest", nil
}

// uninstallArgoCDAgent uninstalls the ArgoCD agent addon in reverse order
// Does NOT delete namespaces, only deletes operator after ArgoCD CR is gone
func (r *ArgoCDAgentAddonReconciler) uninstallArgoCDAgent(ctx context.Context) error {
	klog.Info("Starting ArgoCD agent addon uninstall")

	// Step 1: Delete ArgoCD CR first (if exists)
	// The operator will handle the cleanup of ArgoCD resources
	argoCD := &unstructured.Unstructured{}
	argoCD.SetAPIVersion("argoproj.io/v1beta1")
	argoCD.SetKind("ArgoCD")
	argoCDKey := types.NamespacedName{
		Name:      "argocd",
		Namespace: argoCDNamespace,
	}

	err := r.Get(ctx, argoCDKey, argoCD)
	if err == nil {
		// ArgoCD CR exists, delete it
		klog.Info("Deleting ArgoCD CR")
		if err := r.Delete(ctx, argoCD); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete ArgoCD CR: %w", err)
		}
		klog.Info("ArgoCD CR deletion initiated, operator will reconcile the cleanup")

		// Don't proceed to delete operator yet - the CR still exists (being deleted)
		// The operator needs to run to finalize the ArgoCD deletion
		klog.Info("Waiting for ArgoCD CR to be fully deleted before removing operator")
		return nil
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check ArgoCD CR: %w", err)
	}

	// Step 2: ArgoCD CR is gone, now we can delete operator resources
	klog.Info("ArgoCD CR not found, proceeding to delete operator resources")

	// Delete operator deployment and related resources
	// We don't use the helm chart for deletion, we delete resources with our label
	if err := r.deleteOperatorResources(ctx); err != nil {
		return fmt.Errorf("failed to delete operator resources: %w", err)
	}

	klog.Info("Successfully completed ArgoCD agent addon uninstall")
	return nil
}

// deleteOperatorResources deletes operator resources (deployment, RBAC, etc.)
func (r *ArgoCDAgentAddonReconciler) deleteOperatorResources(ctx context.Context) error {
	klog.Info("Deleting operator resources")

	// Delete resources in the operator namespace with our management label
	resourceTypes := []struct {
		kind       string
		apiVersion string
	}{
		{"Deployment", "apps/v1"},
		{"Service", "v1"},
		{"ServiceAccount", "v1"},
		{"ConfigMap", "v1"},
		{"RoleBinding", "rbac.authorization.k8s.io/v1"},
		{"Role", "rbac.authorization.k8s.io/v1"},
	}

	for _, rt := range resourceTypes {
		list := &unstructured.UnstructuredList{}
		list.SetAPIVersion(rt.apiVersion)
		list.SetKind(rt.kind + "List")

		err := r.List(ctx, list,
			client.InNamespace(operatorNamespace),
			client.MatchingLabels{"app.kubernetes.io/managed-by": "argocd-agent-addon"})

		if err != nil {
			klog.Warningf("Failed to list %s in namespace %s: %v", rt.kind, operatorNamespace, err)
			continue
		}

		for _, item := range list.Items {
			klog.V(1).Infof("Deleting %s/%s in namespace %s", rt.kind, item.GetName(), operatorNamespace)
			if err := r.Delete(ctx, &item); err != nil && !errors.IsNotFound(err) {
				klog.Warningf("Failed to delete %s/%s: %v", rt.kind, item.GetName(), err)
			}
		}
	}

	// Delete cluster-scoped resources
	clusterResourceTypes := []struct {
		kind       string
		apiVersion string
	}{
		{"ClusterRoleBinding", "rbac.authorization.k8s.io/v1"},
		{"ClusterRole", "rbac.authorization.k8s.io/v1"},
	}

	for _, rt := range clusterResourceTypes {
		list := &unstructured.UnstructuredList{}
		list.SetAPIVersion(rt.apiVersion)
		list.SetKind(rt.kind + "List")

		err := r.List(ctx, list,
			client.MatchingLabels{"app.kubernetes.io/managed-by": "argocd-agent-addon"})

		if err != nil {
			klog.Warningf("Failed to list %s: %v", rt.kind, err)
			continue
		}

		for _, item := range list.Items {
			klog.V(1).Infof("Deleting %s/%s", rt.kind, item.GetName())
			if err := r.Delete(ctx, &item); err != nil && !errors.IsNotFound(err) {
				klog.Warningf("Failed to delete %s/%s: %v", rt.kind, item.GetName(), err)
			}
		}
	}

	klog.Info("Operator resources deletion completed")
	return nil
}

// copyClientCertificate copies the OCM client certificate to the ArgoCD namespace as a TLS secret
func (r *ArgoCDAgentAddonReconciler) copyClientCertificate(ctx context.Context) error {
	// Get the secret from OCM addon namespace
	sourceSecret := &corev1.Secret{}
	sourceKey := types.NamespacedName{
		Name:      "argocd-agent-addon-open-cluster-management.io-argocd-agent-addon-client-cert",
		Namespace: "open-cluster-management-agent-addon",
	}

	err := r.Get(ctx, sourceKey, sourceSecret)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.V(1).Info("OCM client certificate not ready yet")
			return nil
		}
		return fmt.Errorf("failed to get source secret: %w", err)
	}

	// Extract tls.crt and tls.key from the OCM secret
	tlsCrt, hasCrt := sourceSecret.Data["tls.crt"]
	tlsKey, hasKey := sourceSecret.Data["tls.key"]

	if !hasCrt || !hasKey {
		return fmt.Errorf("OCM secret missing tls.crt or tls.key")
	}

	// Create or update the secret in ArgoCD namespace as a proper TLS secret
	targetSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "argocd-agent-open-cluster-management.io-argocd-agent-addon-client-cert",
			Namespace: argoCDNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "argocd-agent-addon",
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": tlsCrt,
			"tls.key": tlsKey,
		},
	}

	// Check if target secret exists
	existingSecret := &corev1.Secret{}
	targetKey := types.NamespacedName{
		Name:      targetSecret.Name,
		Namespace: targetSecret.Namespace,
	}

	err = r.Get(ctx, targetKey, existingSecret)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.V(1).Info("Creating TLS client certificate in ArgoCD namespace")
			return r.Create(ctx, targetSecret)
		}
		return fmt.Errorf("failed to check existing secret: %w", err)
	}

	// Update existing secret
	existingSecret.Data = map[string][]byte{
		"tls.crt": tlsCrt,
		"tls.key": tlsKey,
	}
	existingSecret.Type = corev1.SecretTypeTLS
	if existingSecret.Labels == nil {
		existingSecret.Labels = make(map[string]string)
	}
	existingSecret.Labels["app.kubernetes.io/managed-by"] = "argocd-agent-addon"

	klog.V(1).Info("Updating TLS client certificate in ArgoCD namespace")
	return r.Update(ctx, existingSecret)
}
