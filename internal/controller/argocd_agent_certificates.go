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
	"bytes"
	"context"
	"fmt"
	"time"

	"crypto/x509"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// EnsurePrincipalCertificate ensures the principal TLS certificate is generated from the CA.
// serverAddress is the address the controller advertises to agents
// (GitOpsCluster.Spec.ArgoCDAgentAddon.PrincipalServerAddress); it is added to the
// certificate SANs so the cert is valid for exactly what agents dial — whether that is a
// bare IP (e.g. an internal LoadBalancer VIP) or a DNS name fronting the LB. Pass "" to
// derive SANs from the principal Service alone.
func (r *GitOpsClusterReconciler) EnsurePrincipalCertificate(ctx context.Context, namespace, serverAddress string) error {
	klog.V(2).InfoS("Ensuring principal TLS certificate", "namespace", namespace, "serverAddress", serverAddress)

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
		HostNames: r.getPrincipalHostNames(ctx, namespace, serverAddress),
		Lister:    secretLister,
		Client:    kubeClient.CoreV1(),
	}

	// Snapshot the current cert bytes BEFORE rotation so we can tell whether
	// EnsureTargetCertKeyPair actually changed them. The principal reads its TLS
	// keypair ONCE at startup (no fsnotify/informer/reload — argocd-agent
	// principal caches the tls.Config on boot), so it must be restarted whenever
	// this secret's contents change. certrotation rewrites the secret on the ~30-day
	// rotation (and on first creation / SAN change); comparing the tls.crt bytes
	// across the call detects exactly those events and nothing else, so a stable
	// cert on the 10-minute resync produces NO restart (loop-safe).
	certBefore, errBefore := r.getPrincipalTLSCertBytes(ctx, kubeClient, namespace)

	// Ensure the principal TLS certificate
	if err := principalRotation.EnsureTargetCertKeyPair(signingCertKeyPair, caBundleCerts); err != nil {
		return fmt.Errorf("failed to ensure principal TLS certificate: %w", err)
	}

	// Stop informer
	defer informerFactory.Shutdown()

	klog.InfoS("Successfully ensured principal TLS certificate",
		"namespace", namespace, "secret", ArgoCDAgentPrincipalTLSSecretName)

	// If the cert changed (rotation, first issuance, or SAN change), restart the
	// principal so it picks up the new keypair. Best-effort: a restart failure must
	// not fail the reconcile (the cert IS provisioned; the next resync retries).
	certAfter, errAfter := r.getPrincipalTLSCertBytes(ctx, kubeClient, namespace)

	// A read error (timeout, throttling, forbidden) is NOT a certificate change.
	// Treating it as one would spuriously restart the principal (error on the before
	// read) or silently miss a real rotation (error on the after read). Skip the
	// change decision entirely when either read failed and let the next resync retry —
	// the cert itself is already provisioned, so this only defers the restart.
	if errBefore != nil || errAfter != nil {
		klog.V(2).InfoS("Skipping principal restart decision: could not reliably read cert bytes; will retry next resync",
			"namespace", namespace, "errBefore", errBefore, "errAfter", errAfter)
		return nil
	}

	if !bytes.Equal(certBefore, certAfter) {
		klog.InfoS("Principal TLS certificate changed — restarting principal to load it",
			"namespace", namespace, "secret", ArgoCDAgentPrincipalTLSSecretName,
			"firstIssuance", len(certBefore) == 0)
		if err := r.restartPrincipalDeployment(ctx, kubeClient, namespace); err != nil {
			klog.ErrorS(err, "Failed to restart principal after certificate change (non-fatal; will retry next resync)",
				"namespace", namespace)
		}
	}

	return nil
}

// getPrincipalTLSCertBytes returns the tls.crt bytes of the principal TLS secret. A
// genuinely absent secret (cold-start, before first issuance) returns (nil, nil); any
// other GET failure returns a non-nil error so the caller can distinguish "no cert yet"
// from "couldn't read the cert" and avoid mistaking a transient error for a cert change.
// Read directly from the API server (not the informer lister) so the value reflects the
// just-written secret.
func (r *GitOpsClusterReconciler) getPrincipalTLSCertBytes(
	ctx context.Context, kubeClient kubernetes.Interface, namespace string) ([]byte, error) {

	secret, err := kubeClient.CoreV1().Secrets(namespace).Get(ctx, ArgoCDAgentPrincipalTLSSecretName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return secret.Data[corev1.TLSCertKey], nil
}

// restartPrincipalDeployment triggers a rolling restart of the argocd-agent principal
// Deployment by stamping the conventional restartedAt annotation on its pod template —
// the same mechanism as `kubectl rollout restart`. This is how the controller keeps the
// principal's in-memory TLS keypair in sync with the rotated secret WITHOUT any external
// controller (e.g. Stakater Reloader) or a hand-run `kubectl rollout restart`.
//
// Best-effort by design: it patches by name (with the openshift-gitops fallback) and
// returns an error only for the caller to log — a missing Deployment (the operator hasn't
// created it yet) or a transient patch failure must not fail cert reconciliation.
func (r *GitOpsClusterReconciler) restartPrincipalDeployment(
	ctx context.Context, kubeClient kubernetes.Interface, namespace string) error {

	// Merge-patch the pod-template restartedAt annotation. This is idempotent per
	// timestamp and rolls the Deployment exactly once per cert change.
	patch := fmt.Sprintf(
		`{"spec":{"template":{"metadata":{"annotations":{"argocd-pull-integration/principal-cert-restartedAt":%q}}}}}`,
		time.Now().UTC().Format(time.RFC3339))

	names := []string{"argocd-agent-principal", "openshift-gitops-agent-principal"}
	var lastErr error
	for _, name := range names {
		_, err := kubeClient.AppsV1().Deployments(namespace).Patch(
			ctx, name, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{})
		if err == nil {
			klog.InfoS("Restarted argocd-agent principal Deployment after certificate change",
				"namespace", namespace, "deployment", name)
			return nil
		}
		if !k8serrors.IsNotFound(err) {
			lastErr = fmt.Errorf("failed to patch Deployment %s/%s: %w", namespace, name, err)
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("argocd-agent principal Deployment not found in namespace %s (tried %v) — operator may not have created it yet", namespace, names)
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

// getPrincipalHostNames returns the hostnames for the principal certificate.
// Includes LoadBalancer IPs/hostnames, NodePort node IPs, and internal DNS names.
func (r *GitOpsClusterReconciler) getPrincipalHostNames(ctx context.Context, namespace, serverAddress string) []string {
	hostnames := []string{}

	// The advertised server address is added LAST (see appendServerAddress below), not
	// first: certrotation derives the certificate's Subject CN from the first HostNames
	// entry, so we keep the stable service-derived name as the CN and add serverAddress
	// only as an additional SAN. An operator-set DNS name or an IP LB VIP would otherwise
	// become the CN, which is undesirable (and could be a long/host:port string).

	service, err := r.findArgoCDAgentPrincipalService(ctx, namespace)
	if err != nil {
		klog.V(2).InfoS("Could not find principal service for hostname discovery, using defaults", "error", err)
		serviceName := "argocd-agent-principal"
		hostnames = append(hostnames,
			fmt.Sprintf("%s.%s.svc", serviceName, namespace),
			fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, namespace),
		)
		return dedupeHostNames(appendServerAddress(hostnames, serverAddress))
	}

	// Add LoadBalancer external hostnames/IPs
	for _, ingress := range service.Status.LoadBalancer.Ingress {
		if ingress.Hostname != "" {
			hostnames = append(hostnames, ingress.Hostname)
			klog.V(2).InfoS("Added LoadBalancer hostname to principal certificate", "hostname", ingress.Hostname)
		}
		if ingress.IP != "" {
			hostnames = append(hostnames, ingress.IP)
			klog.V(2).InfoS("Added LoadBalancer IP to principal certificate", "ip", ingress.IP)
		}
	}

	// For NodePort services, include node IPs so agents connecting via NodePort pass TLS validation
	if service.Spec.Type == corev1.ServiceTypeNodePort {
		nodeList := &corev1.NodeList{}
		if err := r.List(ctx, nodeList); err == nil {
			for _, node := range nodeList.Items {
				for _, addr := range node.Status.Addresses {
					if addr.Type == corev1.NodeInternalIP {
						hostnames = append(hostnames, addr.Address)
						klog.V(2).InfoS("Added node IP to principal certificate for NodePort", "ip", addr.Address)
					}
				}
			}
		}
	}

	// Always add internal DNS names
	hostnames = append(hostnames,
		fmt.Sprintf("%s.%s.svc", service.Name, namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", service.Name, namespace),
	)

	hostnames = append(hostnames, "localhost", "127.0.0.1", "::1")

	return dedupeHostNames(appendServerAddress(hostnames, serverAddress))
}

// appendServerAddress appends the advertised server address as an additional SAN, if set.
// This is the address the controller writes into every agent's ARGOCD_AGENT_REMOTE_SERVER,
// so the principal cert MUST be valid for it. It is appended (not prepended) so the stable
// service-derived name remains the certificate Subject CN. certrotation classifies the
// entry as an IP or DNS SAN via crypto.IPAddressesDNSNames, so this is correct whether
// serverAddress is a bare IP (e.g. an internal LoadBalancer VIP) or a DNS name.
func appendServerAddress(hostnames []string, serverAddress string) []string {
	if serverAddress != "" {
		hostnames = append(hostnames, serverAddress)
	}
	return hostnames
}

// dedupeHostNames removes duplicate SAN entries while preserving order. The advertised
// server address can coincide with a Service ingress IP/hostname, so the combined list
// may contain duplicates; a duplicate SAN is harmless but noisy in the issued cert.
func dedupeHostNames(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, h := range in {
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	return out
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
