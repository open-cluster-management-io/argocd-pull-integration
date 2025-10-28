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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
)

const (
	// ArgoCDAgentCAManifestWorkName is the name of the ManifestWork for CA distribution
	ArgoCDAgentCAManifestWorkName = "argocd-agent-ca-mw"

	// ArgoCDAgentPropagateCAAnnotation marks ManifestWork for CA propagation
	ArgoCDAgentPropagateCAAnnotation = "apps.open-cluster-management.io/propagate-argocd-agent-ca"
)

// createArgoCDAgentManifestWork creates a ManifestWork for a single managed cluster
// This distributes the CA certificate to the managed cluster whenever it changes
func (r *GitOpsClusterReconciler) createArgoCDAgentManifestWork(
	ctx context.Context,
	argoNamespace string,
	managedCluster *clusterv1.ManagedCluster) error {

	// Skip local-cluster - don't create ManifestWork for local cluster
	if isLocalCluster(managedCluster) {
		klog.V(2).InfoS("Skipping ManifestWork creation for local-cluster", "cluster", managedCluster.Name)
		return nil
	}

	// Get the CA certificate from the argocd-agent-ca secret on the hub
	caCert, err := r.getArgoCDAgentCACertString(ctx, argoNamespace)
	if err != nil {
		return fmt.Errorf("failed to get ArgoCD agent CA certificate: %w", err)
	}

	manifestWork := r.buildArgoCDAgentManifestWork(managedCluster.Name, argoNamespace, caCert)

	// Check if ManifestWork already exists
	existingMW := &workv1.ManifestWork{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      manifestWork.Name,
		Namespace: manifestWork.Namespace,
	}, existingMW)

	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Create new ManifestWork with proper annotations
			if manifestWork.Annotations == nil {
				manifestWork.Annotations = make(map[string]string)
			}
			manifestWork.Annotations[ArgoCDAgentPropagateCAAnnotation] = "true"

			err = r.Create(ctx, manifestWork)
			if err != nil {
				return fmt.Errorf("failed to create ManifestWork for cluster %s: %w", managedCluster.Name, err)
			}

			klog.InfoS("Created ManifestWork for ArgoCD agent CA",
				"cluster", managedCluster.Name, "namespace", manifestWork.Namespace, "name", manifestWork.Name)
			return nil
		}
		return fmt.Errorf("failed to get ManifestWork for cluster %s: %w", managedCluster.Name, err)
	}

	// ManifestWork exists - check if it needs updating
	needsUpdate := false

	// Check if certificate data has changed
	certificateChanged := false

	if len(existingMW.Spec.Workload.Manifests) > 0 {
		// Extract the existing certificate data from the ManifestWork
		existingManifest := existingMW.Spec.Workload.Manifests[0]

		if existingManifest.RawExtension.Raw != nil {
			existingSecret := &corev1.Secret{}
			err := json.Unmarshal(existingManifest.RawExtension.Raw, existingSecret)

			if err == nil {
				existingCert := string(existingSecret.Data["ca.crt"])

				if existingCert != caCert {
					certificateChanged = true
					klog.InfoS("Certificate data changed for ManifestWork",
						"cluster", managedCluster.Name, "namespace", existingMW.Namespace, "name", existingMW.Name)
				}
			}
		}
	}

	if certificateChanged {
		needsUpdate = true
	}

	if needsUpdate {
		// Update the ManifestWork
		existingMW.Spec = manifestWork.Spec

		// Update annotations
		if existingMW.Annotations == nil {
			existingMW.Annotations = make(map[string]string)
		}
		existingMW.Annotations[ArgoCDAgentPropagateCAAnnotation] = "true"

		err = r.Update(ctx, existingMW)
		if err != nil {
			return fmt.Errorf("failed to update ManifestWork for cluster %s: %w", managedCluster.Name, err)
		}

		klog.InfoS("Updated ManifestWork for ArgoCD agent CA",
			"cluster", managedCluster.Name, "namespace", existingMW.Namespace, "name", existingMW.Name)
	} else {
		klog.V(2).InfoS("ManifestWork for ArgoCD agent CA is up to date",
			"cluster", managedCluster.Name, "namespace", existingMW.Namespace, "name", existingMW.Name)
	}

	return nil
}

// buildArgoCDAgentManifestWork builds a ManifestWork object for the ArgoCD agent CA secret
func (r *GitOpsClusterReconciler) buildArgoCDAgentManifestWork(
	managedClusterName, hubNamespace, caCert string) *workv1.ManifestWork {

	// The secret to be created on the managed cluster
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ArgoCDAgentCASecretName,
			Namespace: hubNamespace, // This secret will be created in the ArgoCD namespace on the managed cluster
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"ca.crt": []byte(caCert),
		},
	}

	secretJSON, err := json.Marshal(secret)
	if err != nil {
		klog.ErrorS(err, "Failed to marshal secret to JSON", "secret", ArgoCDAgentCASecretName)
		return nil
	}

	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ArgoCDAgentCAManifestWorkName,
			Namespace: managedClusterName, // ManifestWork is created in the managed cluster namespace on the hub
			Labels: map[string]string{
				"apps.open-cluster-management.io/gitopscluster": "true",
			},
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: []workv1.Manifest{
					{
						RawExtension: runtime.RawExtension{
							Raw: secretJSON,
						},
					},
				},
			},
		},
	}
}

// isLocalCluster checks if the given managed cluster is the local-cluster
func isLocalCluster(cluster *clusterv1.ManagedCluster) bool {
	return cluster.Name == "local-cluster"
}

// getArgoCDAgentCACertString retrieves the CA certificate from the argocd-agent-ca secret
func (r *GitOpsClusterReconciler) getArgoCDAgentCACertString(ctx context.Context, namespace string) (string, error) {
	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      ArgoCDAgentCASecretName,
	}, secret)
	if err != nil {
		return "", fmt.Errorf("failed to get ArgoCD agent CA secret: %w", err)
	}

	// The CA certificate is stored in tls.crt key (standard TLS secret format)
	caCert, exists := secret.Data["tls.crt"]
	if !exists {
		return "", fmt.Errorf("tls.crt not found in ArgoCD agent CA secret")
	}

	return string(caCert), nil
}
