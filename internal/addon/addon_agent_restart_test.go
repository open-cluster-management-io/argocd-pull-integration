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
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	sourceCertSecretName = "argocd-agent-addon-open-cluster-management.io-argocd-agent-addon-client-cert"
	sourceCertNamespace  = "open-cluster-management-agent-addon"
	targetCertSecretName = "argocd-agent-open-cluster-management.io-argocd-agent-addon-client-cert"
)

func sourceCertSecret(crt, key string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: sourceCertSecretName, Namespace: sourceCertNamespace},
		Data:       map[string][]byte{"tls.crt": []byte(crt), "tls.key": []byte(key)},
	}
}

func targetCertSecret(crt, key string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: targetCertSecretName, Namespace: argoCDNamespace},
		Type:       corev1.SecretTypeTLS,
		Data:       map[string][]byte{"tls.crt": []byte(crt), "tls.key": []byte(key)},
	}
}

func agentDeployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: agentDeploymentName, Namespace: argoCDNamespace},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{}},
		},
	}
}

// restartedAt returns the restart annotation currently on the agent Deployment's pod
// template, and whether it is set at all.
func restartedAt(t *testing.T, c client.Client) (string, bool) {
	t.Helper()
	d := &appsv1.Deployment{}
	if err := c.Get(context.Background(),
		types.NamespacedName{Name: agentDeploymentName, Namespace: argoCDNamespace}, d); err != nil {
		t.Fatalf("failed to read agent Deployment: %v", err)
	}
	v, ok := d.Spec.Template.Annotations[agentCertRestartedAtAnnotation]
	return v, ok
}

// TestCopyClientCertificateRestartsAgentOnChange covers the whole point of the feature: the
// agent caches its client cert at startup, so a rotation must roll it — and an UNCHANGED
// cert must NOT, or the periodic reconcile becomes a restart loop.
func TestCopyClientCertificateRestartsAgentOnChange(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)

	tests := []struct {
		name        string
		objs        []runtime.Object
		wantRestart bool
		desc        string
	}{
		{
			name: "cert rotated -> restart",
			objs: []runtime.Object{
				sourceCertSecret("new-cert", "new-key"),
				targetCertSecret("old-cert", "old-key"),
				agentDeployment(),
			},
			wantRestart: true,
			desc:        "rotation is exactly the case the agent cannot survive on its own",
		},
		{
			name: "cert unchanged -> NO restart (loop-safety)",
			objs: []runtime.Object{
				sourceCertSecret("same-cert", "same-key"),
				targetCertSecret("same-cert", "same-key"),
				agentDeployment(),
			},
			wantRestart: false,
			desc:        "reconcile runs on a timer; restarting here would roll the agent forever",
		},
		{
			name: "only the KEY changed -> restart",
			objs: []runtime.Object{
				sourceCertSecret("same-cert", "new-key"),
				targetCertSecret("same-cert", "old-key"),
				agentDeployment(),
			},
			wantRestart: true,
			desc:        "a keypair whose key no longer matches is as broken as an expired cert",
		},
		{
			name: "first issuance (no target secret yet) -> restart attempted",
			objs: []runtime.Object{
				sourceCertSecret("first-cert", "first-key"),
				agentDeployment(),
			},
			wantRestart: true,
			desc:        "covers the cold-start race where the agent booted before the cert existed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.objs...).Build()
			r := &ArgoCDAgentAddonReconciler{Client: c, Scheme: s}

			if err := r.copyClientCertificate(context.Background()); err != nil {
				t.Fatalf("copyClientCertificate() unexpected error: %v", err)
			}

			_, restarted := restartedAt(t, c)
			if restarted != tt.wantRestart {
				t.Errorf("restart = %v, want %v (%s)", restarted, tt.wantRestart, tt.desc)
			}

			// Whatever happened to the Deployment, the certificate itself must be persisted.
			got := &corev1.Secret{}
			if err := c.Get(context.Background(),
				types.NamespacedName{Name: targetCertSecretName, Namespace: argoCDNamespace}, got); err != nil {
				t.Fatalf("target secret was not written: %v", err)
			}
			if got.Type != corev1.SecretTypeTLS {
				t.Errorf("target secret type = %v, want %v", got.Type, corev1.SecretTypeTLS)
			}
		})
	}
}

// TestCopyClientCertificateSucceedsWithoutAgentDeployment asserts the restart is
// best-effort. On a fresh spoke the operator has not created the agent yet; that must not
// fail certificate reconciliation, or the cert never lands and the agent can never start.
func TestCopyClientCertificateSucceedsWithoutAgentDeployment(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)

	c := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(
		sourceCertSecret("new-cert", "new-key"),
		targetCertSecret("old-cert", "old-key"),
		// deliberately NO agent Deployment
	).Build()
	r := &ArgoCDAgentAddonReconciler{Client: c, Scheme: s}

	if err := r.copyClientCertificate(context.Background()); err != nil {
		t.Fatalf("a missing agent Deployment must not fail cert reconciliation, got: %v", err)
	}

	got := &corev1.Secret{}
	if err := c.Get(context.Background(),
		types.NamespacedName{Name: targetCertSecretName, Namespace: argoCDNamespace}, got); err != nil {
		t.Fatalf("target secret was not written: %v", err)
	}
	if string(got.Data["tls.crt"]) != "new-cert" {
		t.Errorf("tls.crt = %q, want %q", got.Data["tls.crt"], "new-cert")
	}
}

// TestRestartAgentDeploymentIsIdempotentlyRepeatable asserts a second restart overwrites the
// annotation rather than erroring or accumulating keys — each cert change yields exactly one
// value, so the Deployment rolls once per change.
func TestRestartAgentDeploymentIsIdempotentlyRepeatable(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)

	c := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(agentDeployment()).Build()
	r := &ArgoCDAgentAddonReconciler{Client: c, Scheme: s}

	if err := r.restartAgentDeployment(context.Background(), argoCDNamespace); err != nil {
		t.Fatalf("first restart failed: %v", err)
	}
	first, ok := restartedAt(t, c)
	if !ok {
		t.Fatal("restart annotation was not set")
	}

	if err := r.restartAgentDeployment(context.Background(), argoCDNamespace); err != nil {
		t.Fatalf("second restart failed: %v", err)
	}

	d := &appsv1.Deployment{}
	if err := c.Get(context.Background(),
		types.NamespacedName{Name: agentDeploymentName, Namespace: argoCDNamespace}, d); err != nil {
		t.Fatalf("failed to read agent Deployment: %v", err)
	}
	if len(d.Spec.Template.Annotations) != 1 {
		t.Errorf("pod template annotations = %v, want exactly 1 (the restart stamp)", d.Spec.Template.Annotations)
	}
	if _, ok := d.Spec.Template.Annotations[agentCertRestartedAtAnnotation]; !ok {
		t.Errorf("restart annotation missing after second restart; first was %q", first)
	}
}

// TestRestartAgentDeploymentPreservesOtherAnnotations guards the operator-coexistence
// property: we merge-patch a single key, so an annotation set by anyone else (e.g. the
// operator, or `kubectl rollout restart`) must survive.
func TestRestartAgentDeploymentPreservesOtherAnnotations(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)

	d := agentDeployment()
	d.Spec.Template.Annotations = map[string]string{
		"kubectl.kubernetes.io/restartedAt": "2026-07-31T13:08:13-05:00",
	}

	c := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(d).Build()
	r := &ArgoCDAgentAddonReconciler{Client: c, Scheme: s}

	if err := r.restartAgentDeployment(context.Background(), argoCDNamespace); err != nil {
		t.Fatalf("restart failed: %v", err)
	}

	got := &appsv1.Deployment{}
	if err := c.Get(context.Background(),
		types.NamespacedName{Name: agentDeploymentName, Namespace: argoCDNamespace}, got); err != nil {
		t.Fatalf("failed to read agent Deployment: %v", err)
	}
	if got.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] != "2026-07-31T13:08:13-05:00" {
		t.Errorf("pre-existing annotation was clobbered: %v", got.Spec.Template.Annotations)
	}
	if _, ok := got.Spec.Template.Annotations[agentCertRestartedAtAnnotation]; !ok {
		t.Errorf("restart annotation was not added: %v", got.Spec.Template.Annotations)
	}
}
