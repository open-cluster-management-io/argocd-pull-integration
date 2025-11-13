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
	"strconv"
	"time"

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
	// PauseMarkerName is the name of the ConfigMap used to pause the addon controller
	PauseMarkerName = "argocd-agent-addon-pause"
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
// Step 0: Create pause marker to stop the addon controller from reconciling
// Step 1: Delete ArgoCD CR and wait for it to be fully removed
// Step 2: Delete operator resources
// Step 3: Wait and re-verify that resources stay deleted
func uninstallArgoCDAgentInternal(ctx context.Context, c client.Client) error {
	klog.Info("Starting ArgoCD agent addon uninstall")
	var cleanupErrors []error

	// Step 0: Create pause marker to prevent the addon controller from reconciling resources
	klog.Infof("Step 0: Creating pause marker in namespace: %s", operatorNamespace)
	if err := createPauseMarker(ctx, c, operatorNamespace); err != nil {
		klog.Errorf("Error creating pause marker (continuing with cleanup): %v", err)
		cleanupErrors = append(cleanupErrors, fmt.Errorf("failed to create pause marker: %w", err))
	}

	// Step 1: Delete ArgoCD CR first (if exists)
	// The operator will handle the cleanup of ArgoCD resources
	klog.Info("Step 1: Deleting ArgoCD CR from namespace: " + argoCDNamespace)
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
	klog.Info("Step 2: Deleting operator resources from namespace: " + operatorNamespace)
	if err := deleteOperatorResourcesInternal(ctx, c); err != nil {
		klog.Errorf("Error deleting operator resources (continuing with cleanup): %v", err)
		cleanupErrors = append(cleanupErrors, fmt.Errorf("failed to delete operator resources: %w", err))
	}

	// Step 3: Wait and re-verify that resources stay deleted
	// This is critical to handle timing issues where the controller might try to reconcile
	// Can be disabled for tests by setting CLEANUP_VERIFICATION_WAIT_SECONDS=0
	waitDuration := getCleanupVerificationWaitDuration()
	if waitDuration > 0 {
		klog.Infof("Step 3: Waiting and re-verifying that resources stay deleted (%v)...", waitDuration)
		if err := waitAndVerifyCleanup(ctx, c, waitDuration); err != nil {
			klog.Errorf("Error during cleanup verification (resources may have been recreated): %v", err)
			cleanupErrors = append(cleanupErrors, fmt.Errorf("cleanup verification failed: %w", err))
		}
	} else {
		klog.Info("Step 3: Skipping cleanup verification (wait duration is 0)")
	}

	if len(cleanupErrors) > 0 {
		klog.Errorf("ArgoCD agent addon uninstall completed with %d error(s)", len(cleanupErrors))
		// Return the first error but log all of them
		return cleanupErrors[0]
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

// createPauseMarker creates a ConfigMap marker to pause the addon controller
func createPauseMarker(ctx context.Context, c client.Client, namespace string) error {
	klog.Infof("Creating pause marker ConfigMap '%s' in namespace: %s", PauseMarkerName, namespace)

	pauseMarker := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PauseMarkerName,
			Namespace: namespace,
			Labels: map[string]string{
				"app": "argocd-agent-addon",
			},
		},
		Data: map[string]string{
			"paused":    "true",
			"reason":    "cleanup",
			"timestamp": time.Now().Format(time.RFC3339),
		},
	}

	err := c.Create(ctx, pauseMarker)
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create pause marker: %w", err)
	}

	klog.Infof("Pause marker created successfully in namespace: %s", namespace)
	return nil
}

// IsPaused checks if the addon controller should be paused
// This is exported so the controller can use it
func IsPaused(ctx context.Context, c client.Client, namespace string) bool {
	pauseMarker := &corev1.ConfigMap{}
	key := types.NamespacedName{
		Name:      PauseMarkerName,
		Namespace: namespace,
	}

	err := c.Get(ctx, key, pauseMarker)
	if err != nil {
		if errors.IsNotFound(err) {
			return false
		}
		klog.Warningf("Error checking pause marker: %v", err)
		return false
	}

	// Check if the pause marker indicates paused state
	if pauseMarker.Data["paused"] == "true" {
		klog.Infof("ArgoCD agent addon is paused (reason: %s, timestamp: %s)",
			pauseMarker.Data["reason"], pauseMarker.Data["timestamp"])
		return true
	}

	return false
}

// waitAndVerifyCleanup waits for a specified duration and periodically verifies that
// resources stay deleted. If resources are recreated, it deletes them again.
func waitAndVerifyCleanup(ctx context.Context, c client.Client, waitDuration time.Duration) error {
	klog.Infof("Starting cleanup verification for %v", waitDuration)

	checkInterval := 20 * time.Second
	elapsed := time.Duration(0)
	checkCount := 0

	for elapsed < waitDuration {
		time.Sleep(checkInterval)
		elapsed += checkInterval
		checkCount++

		klog.Infof("Cleanup verification check %d/%d (elapsed: %v)", checkCount, int(waitDuration/checkInterval), elapsed)

		// Check if ArgoCD CR has been recreated
		argoCD := &unstructured.Unstructured{}
		argoCD.SetAPIVersion("argoproj.io/v1beta1")
		argoCD.SetKind("ArgoCD")
		argoCDKey := types.NamespacedName{
			Name:      "argocd",
			Namespace: argoCDNamespace,
		}
		err := c.Get(ctx, argoCDKey, argoCD)
		if err == nil {
			klog.Warning("ArgoCD CR detected, attempting to delete again...")
			if err := c.Delete(ctx, argoCD); err != nil && !errors.IsNotFound(err) {
				klog.Errorf("Failed to re-delete ArgoCD CR: %v", err)
			}
		}

		// Check if operator resources have been recreated
		operatorResourcesExist, err := verifyNamespaceCleanup(ctx, c, operatorNamespace)
		if err != nil {
			klog.Warningf("Error checking operator namespace: %v", err)
		}

		if operatorResourcesExist {
			klog.Warning("Resources detected in operator namespace, attempting to delete again...")
			if err := deleteOperatorResourcesInternal(ctx, c); err != nil {
				klog.Errorf("Failed to re-delete operator resources: %v", err)
			}
		}
	}

	klog.Infof("Cleanup verification completed after %v", waitDuration)

	// Final check - verify everything is still clean
	argoCD := &unstructured.Unstructured{}
	argoCD.SetAPIVersion("argoproj.io/v1beta1")
	argoCD.SetKind("ArgoCD")
	argoCDKey := types.NamespacedName{
		Name:      "argocd",
		Namespace: argoCDNamespace,
	}
	argoCDExists := c.Get(ctx, argoCDKey, argoCD) == nil

	operatorClean, _ := verifyNamespaceCleanup(ctx, c, operatorNamespace)

	if argoCDExists || operatorClean {
		return fmt.Errorf("resources still exist after cleanup verification period")
	}

	return nil
}

// verifyNamespaceCleanup checks if there are any resources with the addon label in the namespace
// Returns true if resources exist, false if clean
func verifyNamespaceCleanup(ctx context.Context, c client.Client, namespace string) (bool, error) {
	// Check for deployments with the label
	deployments := &unstructured.UnstructuredList{}
	deployments.SetAPIVersion("apps/v1")
	deployments.SetKind("DeploymentList")

	err := c.List(ctx, deployments,
		client.InNamespace(namespace),
		client.MatchingLabels{"app.kubernetes.io/managed-by": "argocd-agent-addon"})

	if err != nil && !errors.IsNotFound(err) {
		return false, err
	}

	if len(deployments.Items) > 0 {
		klog.Infof("Found %d deployment(s) in namespace %s", len(deployments.Items), namespace)
		return true, nil
	}

	return false, nil
}

// getCleanupVerificationWaitDuration returns the wait duration for cleanup verification
// Defaults to 0 seconds, can be overridden with CLEANUP_VERIFICATION_WAIT_SECONDS env var
// Set to 0 to skip verification
func getCleanupVerificationWaitDuration() time.Duration {
	if waitSecondsStr := os.Getenv("CLEANUP_VERIFICATION_WAIT_SECONDS"); waitSecondsStr != "" {
		if waitSeconds, err := strconv.Atoi(waitSecondsStr); err == nil {
			return time.Duration(waitSeconds) * time.Second
		}
	}
	return 0
}
