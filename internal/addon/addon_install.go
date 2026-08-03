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
	"crypto/sha256"
	"encoding/hex"
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

	// agentCertDigestAnnotation records WHICH client-certificate revision the agent was last
	// rolled for. It is the state that makes the restart decision level-triggered rather than
	// edge-triggered, so a restart missed due to a transient API error is retried on the next
	// reconcile instead of being lost until the following rotation.
	agentCertDigestAnnotation = "argocd-pull-integration/agent-cert-digest"

	// agentCertRestartedAtAnnotation records WHEN that restart happened. Operator-facing
	// only — the digest above is what actually triggers the rollout. Deliberately namespaced
	// to this project rather than reusing kubectl.kubernetes.io/restartedAt, so an
	// operator-driven restart and a certificate-driven one remain distinguishable.
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
			// creates it from the ArgoCD CR), in which case this is a no-op NotFound and the
			// agent will read this secret on its own first start. Converge anyway to cover the
			// cold-start race where the agent booted just BEFORE the secret landed and is
			// therefore running with no client cert at all.
			r.ensureAgentRestartedForCert(ctx, argoCDNamespace, agentCertDigest(tlsCrt, tlsKey))
			return nil
		}
		return fmt.Errorf("failed to check existing secret: %w", err)
	}

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

	// Only converge the Deployment once the new bytes are actually persisted — acting before
	// a failed Update would roll the agent onto the same stale cert and log a success.
	r.ensureAgentRestartedForCert(ctx, argoCDNamespace, agentCertDigest(tlsCrt, tlsKey))

	return nil
}

// agentCertDigest fingerprints the keypair. This value is stamped on the agent's pod
// template so the Deployment records WHICH certificate revision it was last rolled for,
// which is what makes the restart decision level-triggered (see
// ensureAgentRestartedForCert).
//
// Lengths are mixed in so the digest is unambiguous: hashing cert||key alone would give the
// same result for any other split of the same concatenated bytes.
//
// SHA-256 is one-way, so stamping this on a pod template exposes no key material. It is the
// same approach as Helm's conventional checksum/secret annotation.
func agentCertDigest(tlsCrt, tlsKey []byte) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d:", len(tlsCrt))
	h.Write(tlsCrt)
	fmt.Fprintf(h, "%d:", len(tlsKey))
	h.Write(tlsKey)
	return hex.EncodeToString(h.Sum(nil))
}

// ensureAgentRestartedForCert converges the agent Deployment onto the certificate revision
// identified by wantDigest, rolling it only when the Deployment is not already recorded as
// running that revision.
//
// 🔴 LEVEL-TRIGGERED ON PURPOSE — DO NOT REWRITE THIS AS "restart if the secret changed".
// That edge-triggered shape looks equivalent and is not. If the Patch fails transiently
// (conflict, throttling, webhook blip) right after the Secret was persisted, the next
// reconcile sees the stored bytes already matching the source, concludes "nothing changed",
// and never retries — leaving the agent on a certificate that will expire, which is exactly
// the outage this whole change exists to prevent. Comparing the DESIRED digest against what
// the Deployment actually carries makes a missed restart self-healing: the mismatch persists
// until a Patch succeeds, and disappears on its own once it does.
//
// The same property gives us loop-safety for free — a steady-state reconcile finds the
// digests equal and does nothing, so the periodic
// ArgoCDAgentAddonReconciler.reconcile does not roll the agent every interval.
//
// Best-effort, exactly like restartPrincipalDeployment: the certificate IS provisioned by the
// time we get here, so a missing Deployment (the operator has not created it yet) or a
// transient patch error must never fail certificate reconciliation.
func (r *ArgoCDAgentAddonReconciler) ensureAgentRestartedForCert(ctx context.Context, namespace, wantDigest string) {
	deployment := &appsv1.Deployment{}
	key := types.NamespacedName{Name: agentDeploymentName, Namespace: namespace}

	if err := r.Get(ctx, key, deployment); err != nil {
		if errors.IsNotFound(err) {
			// Normal before the operator has created the agent from the ArgoCD CR. The agent
			// reads the certificate itself on first start, so there is nothing to roll.
			klog.V(1).InfoS("argocd-agent Deployment not found; it will read the certificate on first start",
				"namespace", namespace, "deployment", agentDeploymentName)
			return
		}
		// Cannot tell whether a restart is needed. Do NOT patch blindly — that would roll the
		// agent on every reconcile whenever reads are failing. The next reconcile retries, and
		// the digest mismatch (if any) is still there waiting.
		klog.ErrorS(err, "Could not read argocd-agent Deployment to check certificate revision (non-fatal; will retry next reconcile)",
			"namespace", namespace, "deployment", agentDeploymentName)
		return
	}

	if deployment.Spec.Template.Annotations[agentCertDigestAnnotation] == wantDigest {
		klog.V(2).InfoS("argocd-agent already running the current TLS client certificate",
			"namespace", namespace, "deployment", agentDeploymentName)
		return
	}

	klog.InfoS("argocd-agent is not running the current TLS client certificate — restarting it to load the new keypair",
		"namespace", namespace, "deployment", agentDeploymentName,
		"have", deployment.Spec.Template.Annotations[agentCertDigestAnnotation], "want", wantDigest)

	if err := r.restartAgentDeployment(ctx, namespace, wantDigest); err != nil {
		if errors.IsNotFound(err) {
			klog.V(1).InfoS("argocd-agent Deployment disappeared before it could be restarted",
				"namespace", namespace, "deployment", agentDeploymentName)
			return
		}
		klog.ErrorS(err, "Failed to restart argocd-agent after certificate change (non-fatal; will retry next reconcile)",
			"namespace", namespace, "deployment", agentDeploymentName)
	}
}

// restartAgentDeployment rolls the agent by stamping the certificate digest — plus a
// human-readable timestamp — on its pod template, the same mechanism as
// `kubectl rollout restart`.
//
// The DIGEST is what makes the rollout happen and what records the revision for the
// level-triggered check above; the timestamp is purely for operators reading
// `kubectl describe`. Note the digest cannot be replaced by a timestamp alone: RFC3339 is
// second-precision, so two rotations inside the same second would produce an identical
// annotation and the second would not roll the Deployment at all.
func (r *ArgoCDAgentAddonReconciler) restartAgentDeployment(ctx context.Context, namespace, certDigest string) error {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agentDeploymentName,
			Namespace: namespace,
		},
	}

	// A raw merge patch (not a full Update) so this cannot clobber operator-managed fields
	// and needs no read-modify-write conflict retry.
	patch := []byte(fmt.Sprintf(
		`{"spec":{"template":{"metadata":{"annotations":{%q:%q,%q:%q}}}}}`,
		agentCertDigestAnnotation, certDigest,
		agentCertRestartedAtAnnotation, time.Now().UTC().Format(time.RFC3339)))

	if err := r.Patch(ctx, deployment, client.RawPatch(types.MergePatchType, patch)); err != nil {
		return err
	}

	klog.InfoS("Restarted argocd-agent Deployment after certificate change",
		"namespace", namespace, "deployment", agentDeploymentName, "certDigest", certDigest)
	return nil
}
