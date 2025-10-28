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
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

const (
	// ArgoCDAgentAddonName is the name of the ArgoCD agent addon
	ArgoCDAgentAddonName = "argocd-agent-addon"

	// ArgoCDAgentAddonConfigName is the name of the addon deployment config
	ArgoCDAgentAddonConfigName = "argocd-agent-addon-config"
)

// EnsureManagedClusterAddon creates the ManagedClusterAddon for a specific cluster
// This function can be called from external controllers or for each managed cluster
func (r *GitOpsClusterReconciler) EnsureManagedClusterAddon(ctx context.Context, clusterNamespace string) error {
	addonName := types.NamespacedName{
		Namespace: clusterNamespace,
		Name:      ArgoCDAgentAddonName,
	}

	// Check if ManagedClusterAddOn already exists
	existing := &addonv1alpha1.ManagedClusterAddOn{}
	err := r.Get(ctx, addonName, existing)
	if err == nil {
		klog.V(2).InfoS("ManagedClusterAddOn already exists", "namespace", clusterNamespace, "name", ArgoCDAgentAddonName)
		return r.ensureAddonConfig(ctx, existing)
	}

	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to get ManagedClusterAddOn: %w", err)
	}

	klog.InfoS("Creating ManagedClusterAddOn", "namespace", clusterNamespace, "name", ArgoCDAgentAddonName)

	// Create new ManagedClusterAddOn with config reference
	addon := &addonv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ArgoCDAgentAddonName,
			Namespace: clusterNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "argocd-pull-integration",
				"app.kubernetes.io/component":  "addon",
			},
		},
		Spec: addonv1alpha1.ManagedClusterAddOnSpec{
			Configs: []addonv1alpha1.AddOnConfig{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonv1alpha1.ConfigReferent{
						Name:      ArgoCDAgentAddonConfigName,
						Namespace: clusterNamespace,
					},
				},
			},
		},
	}

	if err := r.Create(ctx, addon); err != nil {
		if k8serrors.IsAlreadyExists(err) {
			klog.InfoS("ManagedClusterAddOn was created by another process", "namespace", clusterNamespace)
			return nil
		}
		return fmt.Errorf("failed to create ManagedClusterAddOn: %w", err)
	}

	klog.InfoS("Successfully created ManagedClusterAddOn", "namespace", clusterNamespace, "name", ArgoCDAgentAddonName)
	return nil
}

// ensureAddonConfig ensures the addon has the correct config reference
func (r *GitOpsClusterReconciler) ensureAddonConfig(ctx context.Context, addon *addonv1alpha1.ManagedClusterAddOn) error {
	expectedConfig := addonv1alpha1.AddOnConfig{
		ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
			Group:    "addon.open-cluster-management.io",
			Resource: "addondeploymentconfigs",
		},
		ConfigReferent: addonv1alpha1.ConfigReferent{
			Name:      ArgoCDAgentAddonConfigName,
			Namespace: addon.Namespace,
		},
	}

	// Check if the expected config already exists
	for _, config := range addon.Spec.Configs {
		if config.Group == expectedConfig.Group &&
			config.Resource == expectedConfig.Resource &&
			config.Name == expectedConfig.Name &&
			config.Namespace == expectedConfig.Namespace {
			klog.V(2).InfoS("ManagedClusterAddOn already has correct config reference", "namespace", addon.Namespace)
			return nil
		}
	}

	// Add the config reference
	klog.InfoS("Adding config reference to ManagedClusterAddOn", "namespace", addon.Namespace)
	addon.Spec.Configs = append(addon.Spec.Configs, expectedConfig)

	if err := r.Update(ctx, addon); err != nil {
		return fmt.Errorf("failed to update ManagedClusterAddOn config: %w", err)
	}

	klog.InfoS("Updated ManagedClusterAddOn config reference", "namespace", addon.Namespace)
	return nil
}

// EnsureAddOnDeploymentConfig creates or updates an AddOnDeploymentConfig
func (r *GitOpsClusterReconciler) EnsureAddOnDeploymentConfig(ctx context.Context, clusterNamespace string, variables map[string]string) error {
	configName := types.NamespacedName{
		Namespace: clusterNamespace,
		Name:      ArgoCDAgentAddonConfigName,
	}

	// Check if AddOnDeploymentConfig already exists
	existing := &addonv1alpha1.AddOnDeploymentConfig{}
	err := r.Get(ctx, configName, existing)
	if err == nil {
		// Config exists, update it by merging variables
		klog.InfoS("Updating AddOnDeploymentConfig", "namespace", clusterNamespace)
		return r.updateAddOnDeploymentConfig(ctx, existing, variables)
	}

	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to get AddOnDeploymentConfig: %w", err)
	}

	klog.InfoS("Creating AddOnDeploymentConfig", "namespace", clusterNamespace)

	// Create customized variables from the map
	customizedVariables := make([]addonv1alpha1.CustomizedVariable, 0, len(variables))
	for name, value := range variables {
		customizedVariables = append(customizedVariables, addonv1alpha1.CustomizedVariable{
			Name:  name,
			Value: value,
		})
	}

	// Create new AddOnDeploymentConfig
	config := &addonv1alpha1.AddOnDeploymentConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ArgoCDAgentAddonConfigName,
			Namespace: clusterNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "argocd-pull-integration",
				"app.kubernetes.io/component":  "addon-config",
			},
		},
		Spec: addonv1alpha1.AddOnDeploymentConfigSpec{
			CustomizedVariables: customizedVariables,
		},
	}

	if err := r.Create(ctx, config); err != nil {
		if k8serrors.IsAlreadyExists(err) {
			klog.InfoS("AddOnDeploymentConfig was created by another process", "namespace", clusterNamespace)
			return nil
		}
		return fmt.Errorf("failed to create AddOnDeploymentConfig: %w", err)
	}

	klog.InfoS("Successfully created AddOnDeploymentConfig", "namespace", clusterNamespace)
	return nil
}

// updateAddOnDeploymentConfig updates an existing AddOnDeploymentConfig
func (r *GitOpsClusterReconciler) updateAddOnDeploymentConfig(ctx context.Context, config *addonv1alpha1.AddOnDeploymentConfig, variables map[string]string) error {
	// Create a map of existing variables
	existingVars := make(map[string]string)
	for _, v := range config.Spec.CustomizedVariables {
		existingVars[v.Name] = v.Value
	}

	// Merge new variables (only add new ones, don't overwrite existing)
	needsUpdate := false
	for name, value := range variables {
		if _, exists := existingVars[name]; !exists {
			needsUpdate = true
			config.Spec.CustomizedVariables = append(config.Spec.CustomizedVariables, addonv1alpha1.CustomizedVariable{
				Name:  name,
				Value: value,
			})
		}
	}

	if !needsUpdate {
		klog.V(2).InfoS("AddOnDeploymentConfig already has all required variables", "namespace", config.Namespace)
		return nil
	}

	if err := r.Update(ctx, config); err != nil {
		return fmt.Errorf("failed to update AddOnDeploymentConfig: %w", err)
	}

	klog.InfoS("Updated AddOnDeploymentConfig", "namespace", config.Namespace)
	return nil
}
