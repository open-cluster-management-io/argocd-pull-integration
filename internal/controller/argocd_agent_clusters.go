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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/certrotation"
)

const (
	// ArgoCDClusterSecretType is the type for ArgoCD cluster secrets
	ArgoCDClusterSecretType = "Opaque"

	// Labels for ArgoCD cluster secrets
	ArgoCDSecretTypeLabel = "argocd.argoproj.io/secret-type"
	ArgoCDSecretTypeValue = "cluster"
	// The principal server expects this specific label to map agent connections to clusters
	ArgoCDAgentClusterMappingLabel = "argocd-agent.argoproj-labs.io/agent-name"
)

// ClusterConfig represents the ArgoCD cluster configuration
type ClusterConfig struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	CertData []byte `json:"certData,omitempty"`
	KeyData  []byte `json:"keyData,omitempty"`
	CAData   []byte `json:"caData,omitempty"`
}

// importManagedClusterToArgoCD imports a single managed cluster to ArgoCD in agent format
// This creates a secret in ArgoCD namespace for the managed cluster
func (r *GitOpsClusterReconciler) importManagedClusterToArgoCD(
	ctx context.Context,
	argoCDNamespace string,
	managedCluster *clusterv1.ManagedCluster,
	placementName string) error {

	klog.V(2).InfoS("Importing managed cluster to ArgoCD", "cluster", managedCluster.Name, "namespace", argoCDNamespace, "placement", placementName)

	if err := r.createArgoCDClusterSecret(ctx, argoCDNamespace, managedCluster, placementName); err != nil {
		return fmt.Errorf("failed to create ArgoCD cluster secret for %s: %w", managedCluster.Name, err)
	}

	klog.InfoS("Successfully imported managed cluster to ArgoCD", "cluster", managedCluster.Name)
	return nil
}

// createArgoCDClusterSecret creates an ArgoCD cluster secret for a managed cluster
func (r *GitOpsClusterReconciler) createArgoCDClusterSecret(
	ctx context.Context,
	argoCDNamespace string,
	cluster *clusterv1.ManagedCluster,
	placementName string) error {

	secretName := fmt.Sprintf("cluster-%s", cluster.Name)

	// Check if secret already exists
	existing := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{
		Namespace: argoCDNamespace,
		Name:      secretName,
	}, existing)

	if err == nil {
		klog.V(2).InfoS("ArgoCD cluster secret already exists", "cluster", cluster.Name, "secret", secretName)
		return r.updateArgoCDClusterSecret(ctx, argoCDNamespace, existing, cluster, placementName)
	}

	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to check ArgoCD cluster secret: %w", err)
	}

	klog.InfoS("Creating ArgoCD cluster secret", "cluster", cluster.Name, "secret", secretName)

	// Build secret data with certificate information
	secretData, err := r.buildArgoCDClusterSecretData(ctx, argoCDNamespace, cluster)
	if err != nil {
		return fmt.Errorf("failed to build cluster secret data: %w", err)
	}

	// Create new secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: argoCDNamespace,
			Labels: map[string]string{
				ArgoCDSecretTypeLabel:                          ArgoCDSecretTypeValue,
				"apps.open-cluster-management.io/cluster-name": cluster.Name,
				ArgoCDAgentClusterMappingLabel:                 cluster.Name,
				"placement-name":                               placementName,
			},
		},
		Type: ArgoCDClusterSecretType,
		Data: secretData,
	}

	if err := r.Create(ctx, secret); err != nil {
		if k8serrors.IsAlreadyExists(err) {
			klog.InfoS("ArgoCD cluster secret was created by another process", "cluster", cluster.Name)
			return nil
		}
		return fmt.Errorf("failed to create ArgoCD cluster secret: %w", err)
	}

	klog.InfoS("Successfully created ArgoCD cluster secret", "cluster", cluster.Name, "secret", secretName, "placement", placementName)
	return nil
}

// updateArgoCDClusterSecret updates an existing ArgoCD cluster secret
func (r *GitOpsClusterReconciler) updateArgoCDClusterSecret(
	ctx context.Context,
	argoCDNamespace string,
	secret *corev1.Secret,
	cluster *clusterv1.ManagedCluster,
	placementName string) error {

	needsUpdate := false

	// Update labels
	if secret.Labels == nil {
		secret.Labels = make(map[string]string)
	}

	// Check if placement-name label needs updating
	if secret.Labels["placement-name"] != placementName {
		needsUpdate = true
		klog.InfoS("Placement name changed, updating cluster secret", "cluster", cluster.Name, "old", secret.Labels["placement-name"], "new", placementName)
	}

	secret.Labels[ArgoCDSecretTypeLabel] = ArgoCDSecretTypeValue
	secret.Labels["apps.open-cluster-management.io/cluster-name"] = cluster.Name
	secret.Labels[ArgoCDAgentClusterMappingLabel] = cluster.Name
	secret.Labels["placement-name"] = placementName

	// Update data
	secretData, err := r.buildArgoCDClusterSecretData(ctx, argoCDNamespace, cluster)
	if err != nil {
		return fmt.Errorf("failed to build cluster secret data: %w", err)
	}
	secret.Data = secretData

	if err := r.Update(ctx, secret); err != nil {
		return fmt.Errorf("failed to update ArgoCD cluster secret: %w", err)
	}

	if needsUpdate {
		klog.InfoS("Successfully updated ArgoCD cluster secret with new placement name", "cluster", cluster.Name, "placement", placementName)
	} else {
		klog.V(2).InfoS("Successfully updated ArgoCD cluster secret", "cluster", cluster.Name)
	}
	return nil
}

// buildArgoCDClusterSecretData builds the data for an ArgoCD cluster secret with certificate information
func (r *GitOpsClusterReconciler) buildArgoCDClusterSecretData(
	ctx context.Context,
	argoCDNamespace string,
	cluster *clusterv1.ManagedCluster) (map[string][]byte, error) {

	// Ensure the cluster client certificate exists using certrotation
	clientSecretName := fmt.Sprintf("argocd-cluster-client-%s", cluster.Name)
	if err := r.ensureClusterClientCertificate(ctx, argoCDNamespace, cluster.Name, clientSecretName); err != nil {
		return nil, fmt.Errorf("failed to ensure cluster client certificate: %w", err)
	}

	// Get the generated client certificate
	clientSecret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{
		Namespace: argoCDNamespace,
		Name:      clientSecretName,
	}, clientSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to get client certificate secret: %w", err)
	}

	clientCertPEM, ok := clientSecret.Data["tls.crt"]
	if !ok {
		return nil, fmt.Errorf("client certificate not found in secret")
	}

	clientKeyPEM, ok := clientSecret.Data["tls.key"]
	if !ok {
		return nil, fmt.Errorf("client key not found in secret")
	}

	// Get CA certificate
	caSecret := &corev1.Secret{}
	err = r.Get(ctx, types.NamespacedName{
		Namespace: argoCDNamespace,
		Name:      ArgoCDAgentCASecretName,
	}, caSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to get ArgoCD agent CA secret: %w", err)
	}

	caCertPEM, ok := caSecret.Data["tls.crt"]
	if !ok {
		return nil, fmt.Errorf("CA certificate not found in secret")
	}

	// Build cluster config with certificate data
	config := ClusterConfig{
		Username: cluster.Name,
		Password: cluster.Name, // Using cluster name as password for simplicity
		CertData: clientCertPEM,
		KeyData:  clientKeyPEM,
		CAData:   caCertPEM,
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal cluster config: %w", err)
	}

	// Build secret data
	data := map[string][]byte{
		"name":   []byte(cluster.Name),
		"server": []byte(fmt.Sprintf("https://%s-control-plane", cluster.Name)),
		"config": configJSON,
	}

	return data, nil
}

// ensureClusterClientCertificate ensures a client certificate for a cluster using certrotation
func (r *GitOpsClusterReconciler) ensureClusterClientCertificate(
	ctx context.Context,
	namespace string,
	clusterName string,
	secretName string) error {

	// Get Kubernetes clientset
	kubeClient, err := r.getKubernetesClientset()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes clientset: %w", err)
	}

	// Setup informers
	informerFactory, secretLister, err := r.setupInformers(kubeClient, namespace)
	if err != nil {
		return err
	}
	defer informerFactory.Shutdown()

	// Load the CA cert
	signingCertKeyPair, caBundleCerts, err := r.loadCACertificate(kubeClient, secretLister, namespace)
	if err != nil {
		return fmt.Errorf("failed to load CA certificate: %w", err)
	}

	// Create TargetRotation for the cluster client certificate
	// Use the cluster name as the Common Name for the client certificate
	clientRotation := &certrotation.TargetRotation{
		Namespace: namespace,
		Name:      secretName,
		Validity:  TargetCertValidity,
		HostNames: func() []string {
			// For client certificates, we use the cluster name as CN
			return []string{clusterName}
		}(),
		Lister: secretLister,
		Client: kubeClient.CoreV1(),
	}

	// Ensure the client certificate
	if err := clientRotation.EnsureTargetCertKeyPair(signingCertKeyPair, caBundleCerts); err != nil {
		return fmt.Errorf("failed to ensure cluster client certificate: %w", err)
	}

	klog.V(2).InfoS("Successfully ensured cluster client certificate",
		"namespace", namespace, "cluster", clusterName, "secret", secretName)

	return nil
}
