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

	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

const (
	// AddonManagerRoleName is the name of the addon manager role
	AddonManagerRoleName = "addon-manager-controller-role"

	// AddonManagerRoleBindingName is the name of the addon manager role binding
	AddonManagerRoleBindingName = "addon-manager-controller-rolebinding"

	// AddonManagerServiceAccount is the service account name for addon manager
	AddonManagerServiceAccount = "addon-manager-controller-sa"

	// AddonManagerNamespace is the namespace for addon manager
	// Note: This might need to be configurable or discovered
	AddonManagerNamespace = "open-cluster-management-hub"
)

// ensureAddonManagerRBAC creates the addon-manager-controller RBAC resources
func (r *GitOpsClusterReconciler) EnsureAddonManagerRBAC(ctx context.Context, namespace string) error {
	// Create Role
	if err := r.ensureAddonManagerRole(ctx, namespace); err != nil {
		return fmt.Errorf("failed to ensure addon-manager-controller role: %w", err)
	}

	// Create RoleBinding
	if err := r.ensureAddonManagerRoleBinding(ctx, namespace); err != nil {
		return fmt.Errorf("failed to ensure addon-manager-controller rolebinding: %w", err)
	}

	return nil
}

// ensureAddonManagerRole creates the addon-manager-controller role
func (r *GitOpsClusterReconciler) ensureAddonManagerRole(ctx context.Context, namespace string) error {
	roleName := types.NamespacedName{
		Namespace: namespace,
		Name:      AddonManagerRoleName,
	}

	// Check if role exists
	role := &rbacv1.Role{}
	err := r.Get(ctx, roleName, role)
	if err == nil {
		klog.V(2).InfoS("addon-manager-controller-role already exists", "namespace", namespace)
		return nil
	}

	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to check addon-manager-controller-role: %w", err)
	}

	klog.InfoS("Creating addon-manager-controller-role", "namespace", namespace)

	// Create new role
	newRole := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      AddonManagerRoleName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "argocd-pull-integration",
				"app.kubernetes.io/component":  "rbac",
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}

	if err := r.Create(ctx, newRole); err != nil {
		if k8serrors.IsAlreadyExists(err) {
			klog.InfoS("addon-manager-controller-role was created by another process", "namespace", namespace)
			return nil
		}
		return fmt.Errorf("failed to create addon-manager-controller-role: %w", err)
	}

	klog.InfoS("Successfully created addon-manager-controller-role", "namespace", namespace)
	return nil
}

// ensureAddonManagerRoleBinding creates the addon-manager-controller rolebinding
func (r *GitOpsClusterReconciler) ensureAddonManagerRoleBinding(ctx context.Context, namespace string) error {
	roleBindingName := types.NamespacedName{
		Namespace: namespace,
		Name:      AddonManagerRoleBindingName,
	}

	// Check if rolebinding exists
	roleBinding := &rbacv1.RoleBinding{}
	err := r.Get(ctx, roleBindingName, roleBinding)
	if err == nil {
		klog.V(2).InfoS("addon-manager-controller-rolebinding already exists", "namespace", namespace)
		return nil
	}

	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to check addon-manager-controller-rolebinding: %w", err)
	}

	klog.InfoS("Creating addon-manager-controller-rolebinding", "namespace", namespace)

	// Create new rolebinding
	newRoleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      AddonManagerRoleBindingName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "argocd-pull-integration",
				"app.kubernetes.io/component":  "rbac",
			},
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      AddonManagerServiceAccount,
				Namespace: AddonManagerNamespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "Role",
			Name:     AddonManagerRoleName,
			APIGroup: "rbac.authorization.k8s.io",
		},
	}

	if err := r.Create(ctx, newRoleBinding); err != nil {
		if k8serrors.IsAlreadyExists(err) {
			klog.InfoS("addon-manager-controller-rolebinding was created by another process", "namespace", namespace)
			return nil
		}
		return fmt.Errorf("failed to create addon-manager-controller-rolebinding: %w", err)
	}

	klog.InfoS("Successfully created addon-manager-controller-rolebinding", "namespace", namespace)
	return nil
}
