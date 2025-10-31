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
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	operatorNamespace = "argocd-operator-system"
	argoCDNamespace   = "argocd"
)

// uninstallArgoCDAgent uninstalls the ArgoCD agent addon in reverse order
// Does NOT delete namespaces, only deletes operator after ArgoCD CR is gone
func (r *ArgoCDAgentAddonReconciler) uninstallArgoCDAgent(ctx context.Context) error {
	return uninstallArgoCDAgentInternal(ctx, r.Client)
}

// uninstallArgoCDAgent uninstalls the ArgoCD agent addon for cleanup reconciler
func (r *ArgoCDAgentCleanupReconciler) uninstallArgoCDAgent(ctx context.Context) error {
	return uninstallArgoCDAgentInternal(ctx, r.Client)
}

// uninstallArgoCDAgentInternal performs the actual uninstall logic
// Step 1: Delete ArgoCD CR and wait for it to be fully removed
// Step 2: Delete operator resources
func uninstallArgoCDAgentInternal(ctx context.Context, c client.Client) error {
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

	err := c.Get(ctx, argoCDKey, argoCD)
	if err == nil {
		// ArgoCD CR exists, delete it
		klog.Info("Deleting ArgoCD CR")
		if err := c.Delete(ctx, argoCD); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete ArgoCD CR: %w", err)
		}
		klog.Info("ArgoCD CR deletion initiated")

		// Wait for ArgoCD CR to be fully deleted before proceeding
		klog.Info("Waiting for ArgoCD CR to be fully deleted (max 2 minutes)")
		maxWait := 2 * time.Minute
		checkInterval := 5 * time.Second
		elapsed := time.Duration(0)

		for elapsed < maxWait {
			err := c.Get(ctx, argoCDKey, argoCD)
			if errors.IsNotFound(err) {
				klog.Info("ArgoCD CR has been fully deleted")
				break
			}
			if err != nil && !errors.IsNotFound(err) {
				klog.Warningf("Error checking ArgoCD CR status: %v", err)
			}

			klog.V(1).Infof("ArgoCD CR still exists, waiting... (elapsed: %v)", elapsed)
			time.Sleep(checkInterval)
			elapsed += checkInterval
		}

		// Check one final time
		err = c.Get(ctx, argoCDKey, argoCD)
		if err == nil {
			klog.Warning("ArgoCD CR still exists after timeout, proceeding with operator cleanup anyway")
		} else if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to check ArgoCD CR final status: %w", err)
		}
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check ArgoCD CR: %w", err)
	} else {
		klog.Info("ArgoCD CR not found, skipping deletion")
	}

	// Step 2: ArgoCD CR is gone (or timed out), now delete operator resources
	klog.Info("Proceeding to delete operator resources")
	if err := deleteOperatorResourcesInternal(ctx, c); err != nil {
		return fmt.Errorf("failed to delete operator resources: %w", err)
	}

	klog.Info("Successfully completed ArgoCD agent addon uninstall")
	return nil
}

// deleteOperatorResources deletes operator resources (deployment, RBAC, etc.)
func (r *ArgoCDAgentAddonReconciler) deleteOperatorResources(ctx context.Context) error {
	return deleteOperatorResourcesInternal(ctx, r.Client)
}

// deleteOperatorResourcesInternal deletes operator resources (deployment, RBAC, etc.)
func deleteOperatorResourcesInternal(ctx context.Context, c client.Client) error {
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

		err := c.List(ctx, list,
			client.InNamespace(operatorNamespace),
			client.MatchingLabels{"app.kubernetes.io/managed-by": "argocd-agent-addon"})

		if err != nil {
			klog.Warningf("Failed to list %s in namespace %s: %v", rt.kind, operatorNamespace, err)
			continue
		}

		for _, item := range list.Items {
			klog.V(1).Infof("Deleting %s/%s in namespace %s", rt.kind, item.GetName(), operatorNamespace)
			if err := c.Delete(ctx, &item); err != nil && !errors.IsNotFound(err) {
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

		err := c.List(ctx, list,
			client.MatchingLabels{"app.kubernetes.io/managed-by": "argocd-agent-addon"})

		if err != nil {
			klog.Warningf("Failed to list %s: %v", rt.kind, err)
			continue
		}

		for _, item := range list.Items {
			klog.V(1).Infof("Deleting %s/%s", rt.kind, item.GetName())
			if err := c.Delete(ctx, &item); err != nil && !errors.IsNotFound(err) {
				klog.Warningf("Failed to delete %s/%s: %v", rt.kind, item.GetName(), err)
			}
		}
	}

	klog.Info("Operator resources deletion completed")
	return nil
}
