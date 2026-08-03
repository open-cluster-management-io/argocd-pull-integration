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
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Default namespace constants
	operatorNamespace = "argocd-operator-system"
	argoCDNamespace   = "argocd"

	// agentDeploymentName is the argocd-agent agent Deployment created by argocd-operator
	// from the ArgoCD CR. It consumes the client certificate this file copies.
	agentDeploymentName = "argocd-agent-agent"

	// agentCertRestartedAtAnnotation is stamped on the agent's POD TEMPLATE to trigger a
	// rolling restart when the client certificate changes. Deliberately namespaced to this
	// project rather than reusing kubectl.kubernetes.io/restartedAt, so an operator-driven
	// restart and a certificate-driven one remain distinguishable when debugging.
	agentCertRestartedAtAnnotation = "argocd-pull-integration/agent-cert-restartedAt"
)

// getNamespaceConfig returns namespace configuration from environment variables or defaults
func getNamespaceConfig() (string, string) {
	opNamespace := os.Getenv("ARGOCD_OPERATOR_NAMESPACE")
	if opNamespace == "" {
		opNamespace = operatorNamespace
	}

	agentNamespace := os.Getenv("ARGOCD_NAMESPACE")
	if agentNamespace == "" {
		agentNamespace = argoCDNamespace
	}

	return opNamespace, agentNamespace
}

// ensureNamespace creates a namespace if it doesn't exist
func (r *ArgoCDAgentAddonReconciler) ensureNamespace(ctx context.Context, namespaceName string) error {
	namespace := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: namespaceName}, namespace)

	if err == nil {
		// Namespace exists, check if we should skip it
		annotations := namespace.GetAnnotations()
		if annotations != nil && annotations["argocd-addon.open-cluster-management.io/skip"] == "true" {
			klog.V(1).Infof("Skipping namespace %s due to skip annotation", namespaceName)
			return nil
		}

		// Update labels if needed
		needsUpdate := false
		if namespace.Labels == nil {
			namespace.Labels = make(map[string]string)
			needsUpdate = true
		}

		expectedLabels := map[string]string{
			"addon.open-cluster-management.io/namespace":   "true",
			"apps.open-cluster-management.io/argocd-addon": "true",
			"app.kubernetes.io/managed-by":                 "argocd-agent-addon",
		}

		for key, value := range expectedLabels {
			if namespace.Labels[key] != value {
				namespace.Labels[key] = value
				needsUpdate = true
			}
		}

		if needsUpdate {
			if err := r.Update(ctx, namespace); err != nil {
				return fmt.Errorf("failed to update namespace %s: %w", namespaceName, err)
			}
			klog.V(1).Infof("Updated namespace %s labels", namespaceName)
		}

		return nil
	}

	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get namespace %s: %w", namespaceName, err)
	}

	// Namespace doesn't exist, create it
	namespace = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespaceName,
			Labels: map[string]string{
				"addon.open-cluster-management.io/namespace":   "true",
				"apps.open-cluster-management.io/argocd-addon": "true",
				"app.kubernetes.io/managed-by":                 "argocd-agent-addon",
			},
		},
	}

	if err := r.Create(ctx, namespace); err != nil {
		if errors.IsAlreadyExists(err) {
			klog.V(1).Infof("Namespace %s was created by another process", namespaceName)
			return nil
		}
		return fmt.Errorf("failed to create namespace %s: %w", namespaceName, err)
	}

	klog.V(1).Infof("Created namespace %s", namespaceName)
	return nil
}

// installOrUpdateArgoCDAgent orchestrates the ArgoCD agent installation process
func (r *ArgoCDAgentAddonReconciler) installOrUpdateArgoCDAgent(ctx context.Context) error {
	klog.V(1).Info("Templating and applying ArgoCD agent addon")

	// Get namespace configuration from environment
	operatorNamespace, argoCDNamespace := getNamespaceConfig()
	klog.Infof("Using namespaces - operator: %s, argocd: %s", operatorNamespace, argoCDNamespace)

	// 1. Apply CRDs if they don't exist
	if err := r.applyCRDIfNotExists(ctx, "argocds", "argoproj.io/v1beta1", "charts/argocd-agent-addon/crds/argocd-operator-crds.yaml"); err != nil {
		return fmt.Errorf("failed to apply ArgoCD CRDs: %w", err)
	}

	// 2. Create operator namespace
	if err := r.ensureNamespace(ctx, operatorNamespace); err != nil {
		return fmt.Errorf("failed to create operator namespace: %w", err)
	}

	// 3. Create ArgoCD namespace
	if err := r.ensureNamespace(ctx, argoCDNamespace); err != nil {
		return fmt.Errorf("failed to create ArgoCD namespace: %w", err)
	}

	// 4. Template and apply ArgoCD operator with the build-time operator image
	if err := r.templateAndApplyChart(ctx, "charts/argocd-agent-addon", operatorNamespace, argoCDNamespace, "argocd-agent-addon"); err != nil {
		return fmt.Errorf("failed to template and apply ArgoCD operator: %w", err)
	}

	// 5. Copy OCM client certificate to ArgoCD namespace
	if err := r.copyClientCertificate(ctx); err != nil {
		klog.Warningf("Failed to copy client certificate (will retry): %v", err)
	}

	klog.Info("Successfully installed/updated ArgoCD agent addon")
	return nil
}

// ParseImageReference parses an image reference into repository and tag
func ParseImageReference(imageRef string) (string, string, error) {
	if strings.Contains(imageRef, "@") {
		parts := strings.Split(imageRef, "@")
		if len(parts) == 2 {
			return parts[0], parts[1], nil
		}
	}

	if strings.Contains(imageRef, ":") {
		lastColonIndex := strings.LastIndex(imageRef, ":")
		if lastColonIndex != -1 {
			repository := imageRef[:lastColonIndex]
			tag := imageRef[lastColonIndex+1:]

			if !strings.Contains(tag, "/") {
				return repository, tag, nil
			}
		}
	}

	// If no tag or digest found, assume "latest"
	return imageRef, "latest", nil
}

// copyClientCertificate copies the OCM client certificate to the ArgoCD namespace as a TLS
// secret, and restarts the argocd-agent agent whenever those bytes change.
//
// WHY THE RESTART BELONGS HERE. The agent reads its mTLS client keypair from this secret
// ONCE at startup and caches the tls.Config in memory — there is no fsnotify, no informer
// and no reload (pkg/client/remote.go WithTLSClientCertFromSecret does a one-shot
// TLSCertFromSecret and appends to tlsConfig.Certificates). Unlike the OCM-managed source
// secret, this copy is NOT volume-mounted into the agent, so kubelet's auto-updating
// projection cannot help either. Meanwhile OCM's addon framework rotates the source cert
// on a short lifetime (24h for a CustomSigner registration — hardcoded in OCM's
// TemplateCSRSignFunc), so every rotation leaves the running agent presenting a cert that
// eventually expires:
//
//	Auth failure: rpc error: code = Unavailable desc = connection error:
//	desc = "error reading server preface: remote error: tls: expired certificate"
//
// That message names the SERVER preface, which reads like a principal-side problem and
// sends the investigation the wrong way — the expired cert is the CLIENT's.
//
// The failure is SILENT: the agent pod stays Running and Ready, and every Argo CD
// Application keeps reporting its last Synced/Healthy status forever, so a frozen fleet and
// a healthy fleet look identical. This mirrors what we already do for the principal in
// EnsurePrincipalCertificate (#220): the component that writes the certificate is the
// component that restarts its consumer.
func (r *ArgoCDAgentAddonReconciler) copyClientCertificate(ctx context.Context) error {
	_, argoCDNamespace := getNamespaceConfig()

	sourceSecret := &corev1.Secret{}
	sourceKey := types.NamespacedName{
		Name:      "argocd-agent-addon-open-cluster-management.io-argocd-agent-addon-client-cert",
		Namespace: "open-cluster-management-agent-addon",
	}

	err := r.Get(ctx, sourceKey, sourceSecret)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.V(1).Info("OCM client certificate not ready yet")
			return nil
		}
		return fmt.Errorf("failed to get source secret: %w", err)
	}

	tlsCrt, hasCrt := sourceSecret.Data["tls.crt"]
	tlsKey, hasKey := sourceSecret.Data["tls.key"]

	if !hasCrt || !hasKey {
		return fmt.Errorf("OCM secret missing tls.crt or tls.key")
	}

	targetSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "argocd-agent-open-cluster-management.io-argocd-agent-addon-client-cert",
			Namespace: argoCDNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "argocd-agent-addon",
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": tlsCrt,
			"tls.key": tlsKey,
		},
	}

	existingSecret := &corev1.Secret{}
	targetKey := types.NamespacedName{
		Name:      targetSecret.Name,
		Namespace: targetSecret.Namespace,
	}

	err = r.Get(ctx, targetKey, existingSecret)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.V(1).Info("Creating TLS client certificate in ArgoCD namespace")
			if err := r.Create(ctx, targetSecret); err != nil {
				return err
			}
			// First issuance. The agent Deployment usually does not exist yet (the operator
			// creates it from the ArgoCD CR), in which case restartAgentDeployment is a no-op
			// NotFound and the agent will read this secret on its own first start. Attempt it
			// anyway to cover the cold-start race where the agent booted just BEFORE the
			// secret landed and is therefore running with no client cert at all.
			r.restartAgentAfterCertChange(ctx, argoCDNamespace, true)
			return nil
		}
		return fmt.Errorf("failed to check existing secret: %w", err)
	}

	// Content-gate the restart: compare what is already stored against what we are about to
	// store. Only a real rotation (or a SAN/CA change) differs, so the periodic reconcile in
	// ArgoCDAgentAddonReconciler.reconcile does NOT roll the agent every interval. Getting
	// this wrong would be a restart loop, which is why the comparison is on the bytes rather
	// than on resourceVersion (which changes on any write, including our own no-op update).
	certChanged := !bytes.Equal(existingSecret.Data["tls.crt"], tlsCrt) ||
		!bytes.Equal(existingSecret.Data["tls.key"], tlsKey)

	existingSecret.Data = map[string][]byte{
		"tls.crt": tlsCrt,
		"tls.key": tlsKey,
	}
	existingSecret.Type = corev1.SecretTypeTLS
	if existingSecret.Labels == nil {
		existingSecret.Labels = make(map[string]string)
	}
	existingSecret.Labels["app.kubernetes.io/managed-by"] = "argocd-agent-addon"

	klog.V(1).Info("Updating TLS client certificate in ArgoCD namespace")
	if err := r.Update(ctx, existingSecret); err != nil {
		return err
	}

	// Only restart once the new bytes are actually persisted — restarting before a failed
	// Update would boot the agent onto the same stale cert and log a misleading success.
	if certChanged {
		r.restartAgentAfterCertChange(ctx, argoCDNamespace, false)
	}

	return nil
}

// restartAgentAfterCertChange rolls the argocd-agent agent Deployment so it re-reads the
// client certificate from the API server.
//
// Best-effort by design, exactly like restartPrincipalDeployment: the certificate IS
// provisioned by the time we get here, so a missing Deployment (the operator has not created
// it yet) or a transient patch error must never fail certificate reconciliation. The next
// reconcile interval retries, and the content-gate still holds because the gate compares the
// SECRET's bytes, not whether a previous restart succeeded.
//
// NOTE: the agent Deployment is owned by the ArgoCD CR (argocd-operator), not by this
// controller. Patching only spec.template.metadata.annotations is safe there: the operator's
// field manager does not own that field, so server-side apply does not fight us and the
// annotation is not reconciled away. Verified against a live spoke — the patch bumped
// .metadata.generation, rolled the pod, and the annotation persisted across operator resyncs.
func (r *ArgoCDAgentAddonReconciler) restartAgentAfterCertChange(ctx context.Context, namespace string, firstIssuance bool) {
	klog.InfoS("Agent TLS client certificate changed — restarting argocd-agent to load it",
		"namespace", namespace, "deployment", agentDeploymentName, "firstIssuance", firstIssuance)

	if err := r.restartAgentDeployment(ctx, namespace); err != nil {
		if errors.IsNotFound(err) {
			// Expected on first issuance / before the operator has created the agent.
			klog.V(1).InfoS("argocd-agent Deployment not found; it will read the certificate on first start",
				"namespace", namespace, "deployment", agentDeploymentName)
			return
		}
		klog.ErrorS(err, "Failed to restart argocd-agent after certificate change (non-fatal; will retry next reconcile)",
			"namespace", namespace, "deployment", agentDeploymentName)
	}
}

// restartAgentDeployment stamps the conventional restartedAt annotation on the agent
// Deployment's pod template — the same mechanism as `kubectl rollout restart` — using a
// merge patch so we touch nothing else in the operator-owned spec.
func (r *ArgoCDAgentAddonReconciler) restartAgentDeployment(ctx context.Context, namespace string) error {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agentDeploymentName,
			Namespace: namespace,
		},
	}

	// A raw merge patch (not a full Update) so this cannot clobber operator-managed fields
	// and needs no read-modify-write conflict retry.
	patch := []byte(fmt.Sprintf(
		`{"spec":{"template":{"metadata":{"annotations":{%q:%q}}}}}`,
		agentCertRestartedAtAnnotation, time.Now().UTC().Format(time.RFC3339)))

	if err := r.Patch(ctx, deployment, client.RawPatch(types.MergePatchType, patch)); err != nil {
		return err
	}

	klog.InfoS("Restarted argocd-agent Deployment after certificate change",
		"namespace", namespace, "deployment", agentDeploymentName)
	return nil
}
