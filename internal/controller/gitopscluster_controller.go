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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workv1 "open-cluster-management.io/api/work/v1"
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
// +kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=addontemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=placements,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=placementdecisions,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
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

	// Initialize all conditions upfront so users can see the full status immediately
	r.initializeConditions(ctx, gitOpsCluster)

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

	// 6) Create or update AddOnTemplate for this GitOpsCluster
	if err := r.EnsureAddOnTemplate(ctx, gitOpsCluster); err != nil {
		klog.ErrorS(err, "Failed to ensure AddOnTemplate")
		r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionAddOnTemplateReady, metav1.ConditionFalse,
			appsv1alpha1.ReasonFailed, fmt.Sprintf("Failed to ensure AddOnTemplate: %v", err))
		return ctrl.Result{}, err
	}
	r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionAddOnTemplateReady, metav1.ConditionTrue,
		appsv1alpha1.ReasonSuccess, "AddOnTemplate created/updated successfully")

	// 7) Evaluate placement and ensure tolerations
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

	// 8) For each cluster, import OCM managed cluster to ArgoCD
	if len(managedClusters) > 0 {
		failedImportClusters := []string{}
		placementName := gitOpsCluster.Spec.PlacementRef.Name
		for _, managedCluster := range managedClusters {
			if err := r.importManagedClusterToArgoCD(ctx, argoCDNamespace, managedCluster, placementName); err != nil {
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

	// 9) Create the manifestwork for the argocd-agent-ca
	// Determine the spoke ArgoCD namespace - either from spec or default to "argocd"
	spokeArgoCDNamespace := "argocd"
	if gitOpsCluster.Spec.ArgoCDAgentAddon.AgentNamespace != "" {
		spokeArgoCDNamespace = gitOpsCluster.Spec.ArgoCDAgentAddon.AgentNamespace
	}

	if len(managedClusters) > 0 {
		failedMWClusters := []string{}
		for _, managedCluster := range managedClusters {
			// Pass both hub namespace (where CA cert is stored) and spoke namespace (where it should be created)
			if err := r.createArgoCDAgentManifestWork(ctx, argoCDNamespace, spokeArgoCDNamespace, managedCluster); err != nil {
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

	// 10) Clean up addons for clusters no longer in placement
	clustersRemoved, err := r.cleanupRemovedClusters(ctx, gitOpsCluster, managedClusters)
	if err != nil {
		klog.ErrorS(err, "Failed to cleanup removed clusters")
		r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionRemovedClustersCleanedUp, metav1.ConditionFalse,
			appsv1alpha1.ReasonFailed, fmt.Sprintf("Failed to cleanup removed clusters: %v", err))
		// Continue - don't fail the reconciliation
	} else if clustersRemoved > 0 {
		// Only set the condition if clusters were actually removed
		r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionRemovedClustersCleanedUp, metav1.ConditionTrue,
			appsv1alpha1.ReasonSuccess, fmt.Sprintf("Cleaned up %d removed cluster(s)", clustersRemoved))
	}

	// 11) Finally create the addon config and addon in each managedcluster namespace
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
			if err := r.EnsureManagedClusterAddon(ctx, managedCluster.Name, gitOpsCluster); err != nil {
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

	// Ensure placement has required tolerations
	if err := r.ensurePlacementTolerations(ctx, placement, gitOpsCluster); err != nil {
		klog.ErrorS(err, "Failed to ensure placement tolerations", "placement", placement.Name)
		r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionPlacementTolerationConfigured, metav1.ConditionFalse,
			appsv1alpha1.ReasonFailed, fmt.Sprintf("Failed to configure placement tolerations: %v", err))
		// Don't fail - just log the error and continue
	} else {
		r.setCondition(ctx, gitOpsCluster, appsv1alpha1.ConditionPlacementTolerationConfigured, metav1.ConditionTrue,
			appsv1alpha1.ReasonSuccess, "Placement tolerations configured successfully")
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

// cleanupRemovedClusters removes addons from clusters that are no longer in the placement
// Returns the number of clusters that were cleaned up
func (r *GitOpsClusterReconciler) cleanupRemovedClusters(ctx context.Context, gitOpsCluster *appsv1alpha1.GitOpsCluster, currentClusters []*clusterv1.ManagedCluster) (int, error) {
	// Create a set of current cluster names for quick lookup
	currentClusterNames := make(map[string]bool)
	for _, cluster := range currentClusters {
		currentClusterNames[cluster.Name] = true
	}

	// List all ManagedClusterAddons with our label across all namespaces
	addonList := &addonv1alpha1.ManagedClusterAddOnList{}
	if err := r.List(ctx, addonList, client.MatchingLabels{
		"app.kubernetes.io/managed-by": "argocd-pull-integration",
	}); err != nil {
		return 0, fmt.Errorf("failed to list ManagedClusterAddons: %w", err)
	}

	clustersRemoved := 0
	argoCDNamespace := gitOpsCluster.Namespace

	// Delete addons for clusters not in current placement
	for _, addon := range addonList.Items {
		clusterName := addon.Namespace
		if addon.Name == ArgoCDAgentAddonName && !currentClusterNames[clusterName] {
			klog.InfoS("Deleting ManagedClusterAddon for removed cluster", "cluster", clusterName)

			// Delete the addon
			if err := r.Delete(ctx, &addon); err != nil && !k8serrors.IsNotFound(err) {
				klog.ErrorS(err, "Failed to delete ManagedClusterAddon", "cluster", clusterName)
				continue
			}

			// NOTE: Do NOT delete the AddOnDeploymentConfig here.
			// The addon framework's finalizer needs it to run the cleanup job on the spoke.
			// The config will be cleaned up when the ManagedClusterAddon is fully deleted,
			// or when the GitOpsCluster is deleted (if set as owner).

			// Also delete ManifestWork
			manifestWorkName := types.NamespacedName{
				Namespace: clusterName,
				Name:      fmt.Sprintf("argocd-agent-ca-%s", gitOpsCluster.Namespace),
			}
			manifestWork := &workv1.ManifestWork{}
			if err := r.Get(ctx, manifestWorkName, manifestWork); err == nil {
				if err := r.Delete(ctx, manifestWork); err != nil && !k8serrors.IsNotFound(err) {
					klog.ErrorS(err, "Failed to delete ManifestWork", "cluster", clusterName)
				}
			}

			// Also delete the ArgoCD cluster secret
			clusterSecretName := types.NamespacedName{
				Namespace: argoCDNamespace,
				Name:      fmt.Sprintf("cluster-%s", clusterName),
			}
			clusterSecret := &corev1.Secret{}
			if err := r.Get(ctx, clusterSecretName, clusterSecret); err == nil {
				if err := r.Delete(ctx, clusterSecret); err != nil && !k8serrors.IsNotFound(err) {
					klog.ErrorS(err, "Failed to delete ArgoCD cluster secret", "cluster", clusterName)
				}
			}

			klog.InfoS("Successfully deleted resources for removed cluster", "cluster", clusterName)
			clustersRemoved++
		}
	}

	return clustersRemoved, nil
}

// ensurePlacementTolerations ensures the placement has required tolerations for unreachable/unavailable clusters
func (r *GitOpsClusterReconciler) ensurePlacementTolerations(ctx context.Context, placement *clusterv1beta1.Placement, gitOpsCluster *appsv1alpha1.GitOpsCluster) error {
	requiredTolerations := []clusterv1beta1.Toleration{
		{
			Key:      "cluster.open-cluster-management.io/unreachable",
			Operator: clusterv1beta1.TolerationOpExists,
		},
		{
			Key:      "cluster.open-cluster-management.io/unavailable",
			Operator: clusterv1beta1.TolerationOpExists,
		},
	}

	needsUpdate := false
	for _, reqTol := range requiredTolerations {
		found := false
		for _, existingTol := range placement.Spec.Tolerations {
			if existingTol.Key == reqTol.Key && existingTol.Operator == reqTol.Operator {
				found = true
				break
			}
		}
		if !found {
			placement.Spec.Tolerations = append(placement.Spec.Tolerations, reqTol)
			needsUpdate = true
			klog.InfoS("Adding required toleration to placement", "placement", placement.Name, "toleration", reqTol.Key)
		}
	}

	if needsUpdate {
		if err := r.Update(ctx, placement); err != nil {
			return fmt.Errorf("failed to update placement with tolerations: %w", err)
		}
		klog.InfoS("Updated placement with required tolerations", "placement", placement.Name)
	} else {
		klog.V(2).InfoS("Placement already has required tolerations", "placement", placement.Name)
	}

	return nil
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

	// Add namespace configuration
	// AgentNamespace: where the ArgoCD CR will be deployed on the managed/spoke cluster
	// If not specified, addon will use defaults
	if gitOpsCluster.Spec.ArgoCDAgentAddon.AgentNamespace != "" {
		variables["ARGOCD_NAMESPACE"] = gitOpsCluster.Spec.ArgoCDAgentAddon.AgentNamespace
	}

	// OperatorNamespace: where the ArgoCD operator will be deployed on the managed/spoke cluster
	// If not specified, addon will use defaults
	if gitOpsCluster.Spec.ArgoCDAgentAddon.OperatorNamespace != "" {
		variables["ARGOCD_OPERATOR_NAMESPACE"] = gitOpsCluster.Spec.ArgoCDAgentAddon.OperatorNamespace
	}

	// Add operator image if specified
	if gitOpsCluster.Spec.ArgoCDAgentAddon.OperatorImage != "" {
		variables["ARGOCD_OPERATOR_IMAGE"] = gitOpsCluster.Spec.ArgoCDAgentAddon.OperatorImage
	}

	// Add agent image if specified
	if gitOpsCluster.Spec.ArgoCDAgentAddon.AgentImage != "" {
		variables["ARGOCD_AGENT_IMAGE"] = gitOpsCluster.Spec.ArgoCDAgentAddon.AgentImage
	}

	return variables
}

// initializeConditions initializes all conditions with Unknown status if they don't exist yet
// This ensures users can see all expected conditions immediately, rather than seeing them appear one by one
func (r *GitOpsClusterReconciler) initializeConditions(
	ctx context.Context,
	gitOpsCluster *appsv1alpha1.GitOpsCluster) {

	// List of all conditions that should be tracked
	// Note: ConditionRemovedClustersCleanedUp is intentionally NOT initialized here
	// as it should only appear when clusters are actually removed
	conditionTypes := []string{
		appsv1alpha1.ConditionRBACReady,
		appsv1alpha1.ConditionServerDiscovered,
		appsv1alpha1.ConditionJWTSecretReady,
		appsv1alpha1.ConditionCACertificateReady,
		appsv1alpha1.ConditionPrincipalCertificateReady,
		appsv1alpha1.ConditionResourceProxyCertificateReady,
		appsv1alpha1.ConditionPlacementTolerationConfigured,
		appsv1alpha1.ConditionAddOnTemplateReady,
		appsv1alpha1.ConditionPlacementEvaluated,
		appsv1alpha1.ConditionClustersImported,
		appsv1alpha1.ConditionManifestWorkCreated,
		appsv1alpha1.ConditionAddonConfigured,
	}

	needsUpdate := false
	for _, condType := range conditionTypes {
		// Check if condition already exists
		if meta.FindStatusCondition(gitOpsCluster.Status.Conditions, condType) == nil {
			// Condition doesn't exist, initialize it
			condition := metav1.Condition{
				Type:               condType,
				Status:             metav1.ConditionUnknown,
				ObservedGeneration: gitOpsCluster.Generation,
				LastTransitionTime: metav1.Now(),
				Reason:             appsv1alpha1.ReasonInProgress,
				Message:            "Reconciliation in progress",
			}
			meta.SetStatusCondition(&gitOpsCluster.Status.Conditions, condition)
			needsUpdate = true
		}
	}

	// Only update status if we added new conditions
	if needsUpdate {
		if err := r.Status().Update(ctx, gitOpsCluster); err != nil {
			klog.ErrorS(err, "Failed to initialize GitOpsCluster conditions",
				"namespace", gitOpsCluster.Namespace, "name", gitOpsCluster.Name)
		}
	}
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

// placementDecisionMapper maps PlacementDecision changes to GitOpsCluster reconcile requests
func (r *GitOpsClusterReconciler) placementDecisionMapper(ctx context.Context, obj client.Object) []reconcile.Request {
	placementDecision, ok := obj.(*clusterv1beta1.PlacementDecision)
	if !ok {
		return nil
	}

	// Get the placement name from the PlacementDecision labels
	placementName, ok := placementDecision.Labels["cluster.open-cluster-management.io/placement"]
	if !ok {
		return nil
	}

	// List all GitOpsClusters in the same namespace that reference this placement
	gitOpsClusters := &appsv1alpha1.GitOpsClusterList{}
	if err := r.List(ctx, gitOpsClusters, client.InNamespace(placementDecision.Namespace)); err != nil {
		klog.ErrorS(err, "Failed to list GitOpsClusters for PlacementDecision",
			"placementDecision", placementDecision.Name, "namespace", placementDecision.Namespace)
		return nil
	}

	// Find GitOpsClusters that reference this placement
	requests := []reconcile.Request{}
	for _, gitOpsCluster := range gitOpsClusters.Items {
		if gitOpsCluster.Spec.PlacementRef.Name == placementName &&
			gitOpsCluster.Spec.PlacementRef.Kind == "Placement" {
			klog.InfoS("PlacementDecision changed, triggering GitOpsCluster reconciliation",
				"placementDecision", placementDecision.Name,
				"gitOpsCluster", gitOpsCluster.Name,
				"namespace", gitOpsCluster.Namespace)
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      gitOpsCluster.Name,
					Namespace: gitOpsCluster.Namespace,
				},
			})
		}
	}

	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *GitOpsClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1alpha1.GitOpsCluster{}).
		Watches(
			&clusterv1beta1.PlacementDecision{},
			handler.EnqueueRequestsFromMapFunc(r.placementDecisionMapper),
		).
		Named("gitopscluster").
		Complete(r)
}
