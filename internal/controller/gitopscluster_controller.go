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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	appsv1alpha1 "open-cluster-management.io/argocd-pull-integration/api/v1alpha1"
)

// GitOpsClusterReconciler reconciles a GitOpsCluster object
type GitOpsClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Config *rest.Config
}

// +kubebuilder:rbac:groups=apps.open-cluster-management.io,resources=gitopsclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.open-cluster-management.io,resources=gitopsclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.open-cluster-management.io,resources=gitopsclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=addondeploymentconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=placements,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=placementdecisions,verbs=get;list;watch
// +kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=get;list;watch;create;update;patch;delete

func (r *GitOpsClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// Fetch the GitOpsCluster instance
	gitOpsCluster := &appsv1alpha1.GitOpsCluster{}
	err := r.Get(ctx, req.NamespacedName, gitOpsCluster)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			klog.ErrorS(err, "Unable to fetch GitOpsCluster", "namespacedName", req.NamespacedName)
			return ctrl.Result{}, err
		}
		// GitOpsCluster not found, likely deleted
		return ctrl.Result{}, nil
	}

	klog.InfoS("Reconciling GitOpsCluster", "namespace", gitOpsCluster.Namespace, "name", gitOpsCluster.Name)

	// argoCDNamespace is the same as GitOpsCluster namespace
	argoCDNamespace := gitOpsCluster.Namespace

	// 0) Ensure RBAC for the addon-manager
	if err := r.EnsureAddonManagerRBAC(ctx, argoCDNamespace); err != nil {
		klog.ErrorS(err, "Failed to ensure addon manager RBAC", "namespace", argoCDNamespace)
		r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionRBACReady, metav1.ConditionFalse,
			appsv1alpha1.ReasonFailed, fmt.Sprintf("Failed to ensure RBAC: %v", err))
		return ctrl.Result{}, err
	}
	r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionRBACReady, metav1.ConditionTrue,
		appsv1alpha1.ReasonSuccess, "RBAC resources created successfully")

	// 1) Server address and port discovery if empty
	specUpdated, err := r.EnsureServerAddressAndPort(ctx, gitOpsCluster, argoCDNamespace)
	if err != nil {
		klog.ErrorS(err, "Failed to ensure server address and port", "namespace", argoCDNamespace)
		r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionServerDiscovered, metav1.ConditionFalse,
			appsv1alpha1.ReasonServerNotDiscovered, fmt.Sprintf("Server discovery failed: %v", err))
		return ctrl.Result{}, err
	}

	// If spec was updated, save it
	if specUpdated {
		if err := r.Update(ctx, gitOpsCluster); err != nil {
			klog.ErrorS(err, "Failed to update GitOpsCluster spec with discovered server values")
			return ctrl.Result{}, fmt.Errorf("failed to update GitOpsCluster spec: %w", err)
		}
		klog.InfoS("Updated GitOpsCluster spec with discovered server values",
			"namespace", gitOpsCluster.Namespace, "name", gitOpsCluster.Name)
	}

	// Get the server address and port from the spec (either existing or newly discovered)
	serverAddress := gitOpsCluster.Spec.ArgoCDAgentAddon.PrincipalServerAddress
	serverPort := gitOpsCluster.Spec.ArgoCDAgentAddon.PrincipalServerPort

	if serverAddress == "" || serverPort == "" {
		err := fmt.Errorf("server address and/or port are empty after discovery")
		klog.ErrorS(err, "Server discovery incomplete", "address", serverAddress, "port", serverPort)
		r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionServerDiscovered, metav1.ConditionFalse,
			appsv1alpha1.ReasonServerNotDiscovered, "Server address and/or port are empty")
		return ctrl.Result{}, err
	}
	r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionServerDiscovered, metav1.ConditionTrue,
		appsv1alpha1.ReasonSuccess, fmt.Sprintf("Server at %s:%s", serverAddress, serverPort))

	// 2) Create JWT key if not exist
	if err := r.EnsureArgoCDAgentJWTSecret(ctx, argoCDNamespace); err != nil {
		klog.ErrorS(err, "Failed to ensure ArgoCD Agent JWT secret", "namespace", argoCDNamespace)
		r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionJWTSecretReady, metav1.ConditionFalse,
			appsv1alpha1.ReasonFailed, fmt.Sprintf("Failed to create JWT secret: %v", err))
		return ctrl.Result{}, err
	}
	r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionJWTSecretReady, metav1.ConditionTrue,
		appsv1alpha1.ReasonSuccess, "JWT secret created successfully")

	// 3) Setup the argocd-agent-ca secret if not exist
	if err := r.EnsureArgoCDAgentCASecret(ctx, argoCDNamespace); err != nil {
		klog.ErrorS(err, "Failed to ensure ArgoCD Agent CA secret", "namespace", argoCDNamespace)
		r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionCACertificateReady, metav1.ConditionFalse,
			appsv1alpha1.ReasonFailed, fmt.Sprintf("Failed to create CA certificate: %v", err))
		return ctrl.Result{}, err
	}
	r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionCACertificateReady, metav1.ConditionTrue,
		appsv1alpha1.ReasonSuccess, "CA certificate created successfully")

	// 4) Use the ca secret to generate the principal cert
	if err := r.EnsurePrincipalCertificate(ctx, argoCDNamespace); err != nil {
		klog.ErrorS(err, "Failed to ensure principal certificate", "namespace", argoCDNamespace)
		r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionPrincipalCertificateReady, metav1.ConditionFalse,
			appsv1alpha1.ReasonFailed, fmt.Sprintf("Failed to generate principal certificate: %v", err))
		return ctrl.Result{}, err
	}
	r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionPrincipalCertificateReady, metav1.ConditionTrue,
		appsv1alpha1.ReasonSuccess, "Principal certificate generated successfully")

	// 5) Use the ca secret to generate the resource proxy cert
	if err := r.EnsureResourceProxyCertificate(ctx, argoCDNamespace); err != nil {
		klog.ErrorS(err, "Failed to ensure resource proxy certificate", "namespace", argoCDNamespace)
		r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionResourceProxyCertificateReady, metav1.ConditionFalse,
			appsv1alpha1.ReasonFailed, fmt.Sprintf("Failed to generate resource proxy certificate: %v", err))
		return ctrl.Result{}, err
	}
	r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionResourceProxyCertificateReady, metav1.ConditionTrue,
		appsv1alpha1.ReasonSuccess, "Resource proxy certificate generated successfully")

	// 6) Evaluate placement
	managedClusters, err := r.getManagedClustersFromPlacement(ctx, gitOpsCluster)
	if err != nil {
		klog.ErrorS(err, "Failed to get managed clusters from placement",
			"namespace", gitOpsCluster.Namespace, "placement", gitOpsCluster.Spec.PlacementRef.Name)
		r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionPlacementEvaluated, metav1.ConditionFalse,
			appsv1alpha1.ReasonFailed, fmt.Sprintf("Failed to evaluate placement: %v", err))
		return ctrl.Result{}, err
	}
	if len(managedClusters) == 0 {
		klog.InfoS("No managed clusters selected by placement", "placement", gitOpsCluster.Spec.PlacementRef.Name)
		r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionPlacementEvaluated, metav1.ConditionTrue,
			appsv1alpha1.ReasonNoClusters, "No managed clusters selected by placement")
	} else {
		klog.InfoS("Found managed clusters from placement", "count", len(managedClusters))
		r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionPlacementEvaluated, metav1.ConditionTrue,
			appsv1alpha1.ReasonSuccess, fmt.Sprintf("Evaluated %d managed clusters", len(managedClusters)))
	}

	// 7) For each cluster, import OCM managed cluster to ArgoCD
	if len(managedClusters) > 0 {
		failedImportClusters := []string{}
		for _, managedCluster := range managedClusters {
			if err := r.importManagedClusterToArgoCD(ctx, argoCDNamespace, managedCluster); err != nil {
				klog.ErrorS(err, "Failed to import managed cluster to ArgoCD", "cluster", managedCluster.Name)
				failedImportClusters = append(failedImportClusters, managedCluster.Name)
			}
		}

		if len(failedImportClusters) > 0 {
			r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionClustersImported, metav1.ConditionFalse,
				appsv1alpha1.ReasonFailed, fmt.Sprintf("Failed to import clusters: %s", joinClusterNames(failedImportClusters)))
		} else {
			r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionClustersImported, metav1.ConditionTrue,
				appsv1alpha1.ReasonSuccess, fmt.Sprintf("Imported %d clusters to ArgoCD", len(managedClusters)))
		}
	}

	// 8) Create the manifestwork for the argocd-agent-ca
	if len(managedClusters) > 0 {
		failedMWClusters := []string{}
		for _, managedCluster := range managedClusters {
			if err := r.createArgoCDAgentManifestWork(ctx, argoCDNamespace, managedCluster); err != nil {
				klog.ErrorS(err, "Failed to create ManifestWork", "cluster", managedCluster.Name)
				failedMWClusters = append(failedMWClusters, managedCluster.Name)
			}
		}

		if len(failedMWClusters) > 0 {
			r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionManifestWorkCreated, metav1.ConditionFalse,
				appsv1alpha1.ReasonFailed, fmt.Sprintf("Failed to create ManifestWorks for clusters: %s", joinClusterNames(failedMWClusters)))
		} else {
			r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionManifestWorkCreated, metav1.ConditionTrue,
				appsv1alpha1.ReasonSuccess, fmt.Sprintf("Created ManifestWorks for %d clusters", len(managedClusters)))
		}
	}

	// 9) Finally create the addon config and addon in each managedcluster namespace
	if len(managedClusters) > 0 {
		failedAddonClusters := []string{}
		for _, managedCluster := range managedClusters {
			// Create AddOnDeploymentConfig with variables from GitOpsCluster spec
			variables := r.buildAddonVariables(gitOpsCluster, serverAddress, serverPort)
			if err := r.EnsureAddOnDeploymentConfig(ctx, managedCluster.Name, variables); err != nil {
				klog.ErrorS(err, "Failed to ensure AddOnDeploymentConfig", "cluster", managedCluster.Name)
				failedAddonClusters = append(failedAddonClusters, managedCluster.Name)
				continue
			}

			// Create ManagedClusterAddon
			if err := r.EnsureManagedClusterAddon(ctx, managedCluster.Name); err != nil {
				klog.ErrorS(err, "Failed to ensure ManagedClusterAddon", "cluster", managedCluster.Name)
				failedAddonClusters = append(failedAddonClusters, managedCluster.Name)
				continue
			}
		}

		if len(failedAddonClusters) > 0 {
			r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionAddonConfigured, metav1.ConditionFalse,
				appsv1alpha1.ReasonFailed, fmt.Sprintf("Failed to configure addon for clusters: %s", joinClusterNames(failedAddonClusters)))
		} else {
			r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionAddonConfigured, metav1.ConditionTrue,
				appsv1alpha1.ReasonSuccess, fmt.Sprintf("Addon configured for %d clusters", len(managedClusters)))
		}
	}

	klog.InfoS("Successfully reconciled GitOpsCluster", "namespace", gitOpsCluster.Namespace, "name", gitOpsCluster.Name)
	return ctrl.Result{}, nil
}

// getManagedClustersFromPlacement retrieves managed clusters from the placement reference
func (r *GitOpsClusterReconciler) getManagedClustersFromPlacement(
	ctx context.Context,
	gitOpsCluster *appsv1alpha1.GitOpsCluster) ([]*clusterv1.ManagedCluster, error) {

	placementRef := gitOpsCluster.Spec.PlacementRef

	// Validate placement reference
	if placementRef.Kind != "Placement" {
		return nil, fmt.Errorf("invalid placement kind: %s, expected Placement", placementRef.Kind)
	}

	// Get the Placement
	placement := &clusterv1beta1.Placement{}
	err := r.Get(ctx, types.NamespacedName{
		Namespace: gitOpsCluster.Namespace,
		Name:      placementRef.Name,
	}, placement)
	if err != nil {
		return nil, fmt.Errorf("failed to get Placement: %w", err)
	}

	// List PlacementDecisions for this Placement
	placementDecisionList := &clusterv1beta1.PlacementDecisionList{}
	err = r.List(ctx, placementDecisionList,
		client.InNamespace(gitOpsCluster.Namespace),
		client.MatchingLabels{
			"cluster.open-cluster-management.io/placement": placementRef.Name,
		})
	if err != nil {
		return nil, fmt.Errorf("failed to list PlacementDecisions: %w", err)
	}

	// Extract managed cluster names from decisions
	managedClusters := []*clusterv1.ManagedCluster{}
	for _, pd := range placementDecisionList.Items {
		for _, decision := range pd.Status.Decisions {
			// Get the ManagedCluster
			managedCluster := &clusterv1.ManagedCluster{}
			err := r.Get(ctx, types.NamespacedName{Name: decision.ClusterName}, managedCluster)
			if err != nil {
				klog.ErrorS(err, "Failed to get ManagedCluster", "cluster", decision.ClusterName)
				continue
			}
			managedClusters = append(managedClusters, managedCluster)
		}
	}

	return managedClusters, nil
}

// buildAddonVariables builds the variables for AddOnDeploymentConfig from GitOpsCluster spec
func (r *GitOpsClusterReconciler) buildAddonVariables(
	gitOpsCluster *appsv1alpha1.GitOpsCluster,
	serverAddress string,
	serverPort string) map[string]string {

	variables := make(map[string]string)

	// Use discovered/configured server address and port
	variables["ARGOCD_AGENT_SERVER_ADDRESS"] = serverAddress
	variables["ARGOCD_AGENT_SERVER_PORT"] = serverPort

	if gitOpsCluster.Spec.ArgoCDAgentAddon.Mode != "" {
		variables["ARGOCD_AGENT_MODE"] = gitOpsCluster.Spec.ArgoCDAgentAddon.Mode
	}

	// Add operator image if specified
	if gitOpsCluster.Spec.ArgoCDAgentAddon.OperatorImage != "" {
		variables["ARGOCD_OPERATOR_IMAGE"] = gitOpsCluster.Spec.ArgoCDAgentAddon.OperatorImage
	}

	// Add agent image if specified
	if gitOpsCluster.Spec.ArgoCDAgentAddon.AgentImage != "" {
		variables["ARGOCD_AGENT_IMAGE"] = gitOpsCluster.Spec.ArgoCDAgentAddon.AgentImage
	}

	// Add uninstall flag if set
	if gitOpsCluster.Spec.ArgoCDAgentAddon.Uninstall {
		variables["ARGOCD_AGENT_UNINSTALL"] = "true"
	} else {
		variables["ARGOCD_AGENT_UNINSTALL"] = "false"
	}

	return variables
}

// setCondition sets or updates a condition in the GitOpsCluster status
func (r *GitOpsClusterReconciler) setCondition(
	ctx context.Context,
	gitOpsCluster *appsv1alpha1.GitOpsCluster,
	conditionType string,
	status metav1.ConditionStatus,
	reason string,
	message string) {

	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: gitOpsCluster.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}

	meta.SetStatusCondition(&gitOpsCluster.Status.Conditions, condition)

	// Update status
	if err := r.Status().Update(ctx, gitOpsCluster); err != nil {
		klog.ErrorS(err, "Failed to update GitOpsCluster status",
			"namespace", gitOpsCluster.Namespace, "name", gitOpsCluster.Name)
	}
}

// joinClusterNames joins cluster names with comma
func joinClusterNames(clusters []string) string {
	if len(clusters) == 0 {
		return ""
	}
	if len(clusters) <= 3 {
		return fmt.Sprintf("%v", clusters)
	}
	// If more than 3, show first 3 and count
	return fmt.Sprintf("%v... (%d total)", clusters[:3], len(clusters))
}

// SetupWithManager sets up the controller with the Manager.
func (r *GitOpsClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1alpha1.GitOpsCluster{}).
		Named("gitopscluster").
		Complete(r)
}
