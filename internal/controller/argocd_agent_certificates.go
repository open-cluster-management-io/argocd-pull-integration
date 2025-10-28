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
	"time"

	"crypto/x509"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"github.com/openshift/library-go/pkg/crypto"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"
	"open-cluster-management.io/sdk-go/pkg/certrotation"
)

const (
	// ArgoCDAgentCASecretName is the name of the CA secret
	ArgoCDAgentCASecretName = "argocd-agent-ca"

	// ArgoCDAgentPrincipalTLSSecretName is the name of the principal TLS certificate
	ArgoCDAgentPrincipalTLSSecretName = "argocd-agent-principal-tls"

	// ArgoCDAgentResourceProxyTLSSecretName is the name of the resource proxy TLS certificate
	ArgoCDAgentResourceProxyTLSSecretName = "argocd-agent-resource-proxy-tls"

	// CASignerNamePrefix is the prefix for the CA signer name
	CASignerNamePrefix = "argocd-agent-ca"
)

// Certificate validity periods
var (
	// SigningCertValidity is the validity for the CA certificate (1 year)
	SigningCertValidity = time.Hour * 24 * 365

	// TargetCertValidity is the validity for service certificates (30 days)
	TargetCertValidity = time.Hour * 24 * 30

	// ResyncInterval is how often to check for rotation (10 minutes)
	ResyncInterval = time.Minute * 10
)

// EnsureArgoCDAgentCASecret ensures the ArgoCD agent CA secret exists
// This creates only the CA certificate and CA bundle ConfigMap
func (r *GitOpsClusterReconciler) EnsureArgoCDAgentCASecret(ctx context.Context, namespace string) error {
	klog.V(2).InfoS("Ensuring ArgoCD agent CA certificate", "namespace", namespace)

	// Get Kubernetes clientset
	kubeClient, err := r.getKubernetesClientset()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes clientset: %w", err)
	}

	// Setup informers
	informerFactory := informers.NewSharedInformerFactoryWithOptions(
		kubeClient,
		ResyncInterval,
		informers.WithNamespace(namespace),
	)

	secretLister := informerFactory.Core().V1().Secrets().Lister()
	configMapLister := informerFactory.Core().V1().ConfigMaps().Lister()

	// Start the informer and wait for cache sync
	stopCh := make(chan struct{})
	defer close(stopCh)
	informerFactory.Start(stopCh)

	// Wait for cache to sync
	cacheSyncs := informerFactory.WaitForCacheSync(stopCh)
	for informerType, synced := range cacheSyncs {
		if !synced {
			return fmt.Errorf("failed to sync informer cache for type %v", informerType)
		}
	}

	// Create SigningRotation for the CA certificate
	signingRotation := &certrotation.SigningRotation{
		Namespace:        namespace,
		Name:             ArgoCDAgentCASecretName,
		SignerNamePrefix: CASignerNamePrefix,
		Validity:         SigningCertValidity,
		Lister:           secretLister,
		Client:           kubeClient.CoreV1(),
	}

	// Ensure the CA signing certificate key pair
	signingCertKeyPair, err := signingRotation.EnsureSigningCertKeyPair()
	if err != nil {
		return fmt.Errorf("failed to ensure signing cert key pair: %w", err)
	}

	// Create CABundleRotation for the CA bundle ConfigMap
	caBundleRotation := &certrotation.CABundleRotation{
		Namespace: namespace,
		Name:      "argocd-agent-ca-bundle",
		Lister:    configMapLister,
		Client:    kubeClient.CoreV1(),
	}

	// Ensure the CA bundle ConfigMap
	_, err = caBundleRotation.EnsureConfigMapCABundle(signingCertKeyPair)
	if err != nil {
		return fmt.Errorf("failed to ensure CA bundle: %w", err)
	}

	klog.InfoS("Successfully ensured ArgoCD agent CA certificate",
		"namespace", namespace, "secret", ArgoCDAgentCASecretName)

	return nil
}

// EnsurePrincipalCertificate ensures the principal TLS certificate is generated from the CA
func (r *GitOpsClusterReconciler) EnsurePrincipalCertificate(ctx context.Context, namespace string) error {
	klog.V(2).InfoS("Ensuring principal TLS certificate", "namespace", namespace)

	// Verify CA secret exists
	if err := r.verifyCACertificateExists(ctx, namespace); err != nil {
		return fmt.Errorf("CA certificate not found: %w", err)
	}

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

	// Load the CA cert
	signingCertKeyPair, caBundleCerts, err := r.loadCACertificate(kubeClient, secretLister, namespace)
	if err != nil {
		return fmt.Errorf("failed to load CA certificate: %w", err)
	}

	// Create TargetRotation for the principal TLS certificate
	principalRotation := &certrotation.TargetRotation{
		Namespace: namespace,
		Name:      ArgoCDAgentPrincipalTLSSecretName,
		Validity:  TargetCertValidity,
		HostNames: r.getPrincipalHostNames(ctx, namespace),
		Lister:    secretLister,
		Client:    kubeClient.CoreV1(),
	}

	// Ensure the principal TLS certificate
	if err := principalRotation.EnsureTargetCertKeyPair(signingCertKeyPair, caBundleCerts); err != nil {
		return fmt.Errorf("failed to ensure principal TLS certificate: %w", err)
	}

	// Stop informer
	defer informerFactory.Shutdown()

	klog.InfoS("Successfully ensured principal TLS certificate",
		"namespace", namespace, "secret", ArgoCDAgentPrincipalTLSSecretName)

	return nil
}

// EnsureResourceProxyCertificate ensures the resource proxy TLS certificate is generated from the CA
func (r *GitOpsClusterReconciler) EnsureResourceProxyCertificate(ctx context.Context, namespace string) error {
	klog.V(2).InfoS("Ensuring resource proxy TLS certificate", "namespace", namespace)

	// Verify CA secret exists
	if err := r.verifyCACertificateExists(ctx, namespace); err != nil {
		return fmt.Errorf("CA certificate not found: %w", err)
	}

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

	// Load the CA cert
	signingCertKeyPair, caBundleCerts, err := r.loadCACertificate(kubeClient, secretLister, namespace)
	if err != nil {
		return fmt.Errorf("failed to load CA certificate: %w", err)
	}

	// Create TargetRotation for the resource proxy TLS certificate
	resourceProxyRotation := &certrotation.TargetRotation{
		Namespace: namespace,
		Name:      ArgoCDAgentResourceProxyTLSSecretName,
		Validity:  TargetCertValidity,
		HostNames: r.getResourceProxyHostNames(ctx, namespace),
		Lister:    secretLister,
		Client:    kubeClient.CoreV1(),
	}

	// Ensure the resource proxy TLS certificate
	if err := resourceProxyRotation.EnsureTargetCertKeyPair(signingCertKeyPair, caBundleCerts); err != nil {
		return fmt.Errorf("failed to ensure resource proxy TLS certificate: %w", err)
	}

	// Stop informer
	defer informerFactory.Shutdown()

	klog.InfoS("Successfully ensured resource proxy TLS certificate",
		"namespace", namespace, "secret", ArgoCDAgentResourceProxyTLSSecretName)

	return nil
}

// verifyCACertificateExists checks if the CA secret exists
func (r *GitOpsClusterReconciler) verifyCACertificateExists(ctx context.Context, namespace string) error {
	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      ArgoCDAgentCASecretName,
	}, secret)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("CA secret %s not found in namespace %s", ArgoCDAgentCASecretName, namespace)
		}
		return err
	}
	return nil
}

// setupInformers creates and starts the informer factory
func (r *GitOpsClusterReconciler) setupInformers(
	kubeClient *kubernetes.Clientset,
	namespace string) (informers.SharedInformerFactory, v1.SecretLister, error) {

	informerFactory := informers.NewSharedInformerFactoryWithOptions(
		kubeClient,
		ResyncInterval,
		informers.WithNamespace(namespace),
	)

	secretLister := informerFactory.Core().V1().Secrets().Lister()

	// Start the informer and wait for cache sync
	stopCh := make(chan struct{})
	go func() {
		<-time.After(30 * time.Second)
		close(stopCh)
	}()
	informerFactory.Start(stopCh)

	// Wait for cache to sync
	cacheSyncs := informerFactory.WaitForCacheSync(stopCh)
	for informerType, synced := range cacheSyncs {
		if !synced {
			return nil, nil, fmt.Errorf("failed to sync informer cache for type %v", informerType)
		}
	}

	return informerFactory, secretLister, nil
}

// loadCACertificate loads the CA certificate from the secret
func (r *GitOpsClusterReconciler) loadCACertificate(
	kubeClient *kubernetes.Clientset,
	secretLister v1.SecretLister,
	namespace string) (*crypto.CA, []*x509.Certificate, error) {

	// Create SigningRotation to load the existing CA
	signingRotation := &certrotation.SigningRotation{
		Namespace:        namespace,
		Name:             ArgoCDAgentCASecretName,
		SignerNamePrefix: CASignerNamePrefix,
		Validity:         SigningCertValidity,
		Lister:           secretLister,
		Client:           kubeClient.CoreV1(),
	}

	// Load the existing CA certificate
	signingCertKeyPair, err := signingRotation.EnsureSigningCertKeyPair()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load signing cert key pair: %w", err)
	}

	// Load CA bundle
	configMapLister := informers.NewSharedInformerFactoryWithOptions(
		kubeClient,
		ResyncInterval,
		informers.WithNamespace(namespace),
	).Core().V1().ConfigMaps().Lister()

	caBundleRotation := &certrotation.CABundleRotation{
		Namespace: namespace,
		Name:      "argocd-agent-ca-bundle",
		Lister:    configMapLister,
		Client:    kubeClient.CoreV1(),
	}

	caBundleCerts, err := caBundleRotation.EnsureConfigMapCABundle(signingCertKeyPair)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load CA bundle: %w", err)
	}

	return signingCertKeyPair, caBundleCerts, nil
}

// getPrincipalHostNames returns the hostnames for the principal certificate
// For principal, we need external endpoints (LoadBalancer IPs/hostnames) plus internal DNS
func (r *GitOpsClusterReconciler) getPrincipalHostNames(ctx context.Context, namespace string) []string {
	hostnames := []string{}

	// Try to get the service to find LoadBalancer endpoints
	service, err := r.findArgoCDAgentPrincipalService(ctx, namespace)
	if err != nil {
		klog.V(2).InfoS("Could not find principal service for hostname discovery, using defaults", "error", err)
		// Return default internal hostnames
		serviceName := "argocd-agent-principal"
		return []string{
			fmt.Sprintf("%s.%s.svc", serviceName, namespace),
			fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, namespace),
		}
	}

	// Add LoadBalancer external hostnames/IPs
	for _, ingress := range service.Status.LoadBalancer.Ingress {
		if ingress.Hostname != "" {
			hostnames = append(hostnames, ingress.Hostname)
			klog.V(2).InfoS("Added LoadBalancer hostname to principal certificate", "hostname", ingress.Hostname)
		}
		if ingress.IP != "" {
			// IPs should be added but cert libraries typically use IPs field, not DNS names
			// Still add as hostname for compatibility
			hostnames = append(hostnames, ingress.IP)
			klog.V(2).InfoS("Added LoadBalancer IP to principal certificate", "ip", ingress.IP)
		}
	}

	// Always add internal DNS names
	hostnames = append(hostnames,
		fmt.Sprintf("%s.%s.svc", service.Name, namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", service.Name, namespace),
	)

	// Add localhost for local access
	hostnames = append(hostnames, "localhost", "127.0.0.1", "::1")

	return hostnames
}

// getResourceProxyHostNames returns the hostnames for the resource proxy certificate
// For resource proxy, we need internal cluster DNS names
func (r *GitOpsClusterReconciler) getResourceProxyHostNames(ctx context.Context, namespace string) []string {
	hostnames := []string{}

	// Try to get the service
	service, err := r.findArgoCDAgentPrincipalService(ctx, namespace)
	serviceName := "argocd-agent-principal"
	if err == nil {
		serviceName = service.Name
	}

	// Add internal DNS names for resource proxy
	hostnames = append(hostnames,
		fmt.Sprintf("%s.%s.svc", serviceName, namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, namespace),
	)

	// Add localhost for local access
	hostnames = append(hostnames, "localhost", "127.0.0.1", "::1")

	return hostnames
}

// findArgoCDAgentPrincipalService finds the ArgoCD agent principal service
func (r *GitOpsClusterReconciler) findArgoCDAgentPrincipalService(
	ctx context.Context,
	namespace string) (*corev1.Service, error) {

	// First try to find by the specific name
	service := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      "argocd-agent-principal",
		Namespace: namespace,
	}, service)

	if err == nil {
		klog.V(2).InfoS("Found ArgoCD agent principal service", "name", "argocd-agent-principal", "namespace", namespace)
		return service, nil
	}

	// Fallback: try openshift-gitops naming
	err = r.Get(ctx, types.NamespacedName{
		Name:      "openshift-gitops-agent-principal",
		Namespace: namespace,
	}, service)

	if err == nil {
		klog.V(2).InfoS("Found ArgoCD agent principal service", "name", "openshift-gitops-agent-principal", "namespace", namespace)
		return service, nil
	}

	return nil, fmt.Errorf("ArgoCD agent principal service not found in namespace %s (tried argocd-agent-principal and openshift-gitops-agent-principal)", namespace)
}

// getKubernetesClientset creates a Kubernetes clientset from the controller-runtime client
func (r *GitOpsClusterReconciler) getKubernetesClientset() (*kubernetes.Clientset, error) {
	config := r.Config
	if config == nil {
		return nil, fmt.Errorf("failed to get REST config")
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	return clientset, nil
}
