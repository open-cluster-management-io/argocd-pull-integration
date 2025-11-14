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
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"open-cluster-management.io/argocd-pull-integration/internal/pkg/images"
)

const (
	// Default namespace constants
	operatorNamespace = "argocd-operator-system"
	argoCDNamespace   = "argocd"
)

// getNamespaceConfig returns namespace configuration from environment variables or defaults
func getNamespaceConfig() (string, string) {
	opNamespace := os.Getenv("ARGOCD_OPERATOR_NAMESPACE")
	if opNamespace == "" {
		opNamespace = operatorNamespace
	}

	agentNamespace := os.Getenv("ARGOCD_NAMESPACE")
	if agentNamespace == "" {
		agentNamespace = argoCDNamespace
	}

	return opNamespace, agentNamespace
}

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

	// Get namespace configuration from environment
	operatorNamespace, argoCDNamespace := getNamespaceConfig()
	klog.Infof("Using namespaces - operator: %s, argocd: %s", operatorNamespace, argoCDNamespace)

	// Set defaults if images are not provided
	operatorImage := r.ArgoCDOperatorImage
	if operatorImage == "" {
		operatorImage = images.GetFullImageReference(images.DefaultOperatorImage, images.DefaultOperatorTag)
		klog.V(1).Infof("Using default operator image: %s", operatorImage)
	}

	agentImage := r.ArgoCDAgentImage
	if agentImage == "" {
		agentImage = images.GetFullImageReference(images.DefaultAgentImage, images.DefaultAgentTag)
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
	if err := r.templateAndApplyChart(ctx, "charts/argocd-agent-addon", operatorNamespace, argoCDNamespace, "argocd-agent-addon", operatorImage, agentImage); err != nil {
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

			// Check if this is a tag (not a port number in the registry URL)
			// Tags don't contain slashes and are typically shorter than paths
			// Port numbers would be numeric-only
			if !strings.Contains(tag, "/") {
				// This looks like a tag
				return repository, tag, nil
			}
		}
	}

	// If no tag or digest found, assume "latest"
	return imageRef, "latest", nil
}

// copyClientCertificate copies the OCM client certificate to the ArgoCD namespace as a TLS secret
func (r *ArgoCDAgentAddonReconciler) copyClientCertificate(ctx context.Context) error {
	// Get namespace configuration from environment
	_, argoCDNamespace := getNamespaceConfig()

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
