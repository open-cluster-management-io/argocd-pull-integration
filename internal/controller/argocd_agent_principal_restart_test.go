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
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

const restartAnnotationKey = "argocd-pull-integration/principal-cert-restartedAt"

func principalDeployment(name, namespace string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{},
			},
		},
	}
}

func TestGetPrincipalTLSCertBytes(t *testing.T) {
	namespace := "argocd"

	tests := []struct {
		name      string
		secret    *corev1.Secret
		injectErr bool // simulate a transient (non-NotFound) GET failure
		wantNil   bool
		wantVal   string
		wantErr   bool
	}{
		{
			name:    "secret missing returns (nil, nil) (cold-start)",
			secret:  nil,
			wantNil: true,
			wantErr: false,
		},
		{
			name: "secret present returns tls.crt bytes",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: ArgoCDAgentPrincipalTLSSecretName, Namespace: namespace},
				Data:       map[string][]byte{corev1.TLSCertKey: []byte("CERT-A")},
			},
			wantNil: false,
			wantVal: "CERT-A",
			wantErr: false,
		},
		{
			// A transient GET error must surface as a non-nil error (NOT nil bytes),
			// so the caller can tell it apart from a genuinely absent secret and avoid
			// mistaking the read failure for a certificate change.
			name:      "transient GET error is propagated (not treated as absent)",
			secret:    nil,
			injectErr: true,
			wantNil:   true,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var kube *kubefake.Clientset
			if tt.secret != nil {
				kube = kubefake.NewSimpleClientset(tt.secret)
			} else {
				kube = kubefake.NewSimpleClientset()
			}
			if tt.injectErr {
				kube.PrependReactor("get", "secrets", func(clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, apierrors.NewServerTimeout(schema.GroupResource{Resource: "secrets"}, "get", 1)
				})
			}
			r := &GitOpsClusterReconciler{}
			got, err := r.getPrincipalTLSCertBytes(context.Background(), kube, namespace)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil bytes, got %q", string(got))
				}
				return
			}
			if string(got) != tt.wantVal {
				t.Errorf("got %q, want %q", string(got), tt.wantVal)
			}
		})
	}
}

func TestRestartPrincipalDeployment(t *testing.T) {
	namespace := "argocd"

	t.Run("patches argocd-agent-principal by name", func(t *testing.T) {
		kube := kubefake.NewSimpleClientset(principalDeployment("argocd-agent-principal", namespace))
		r := &GitOpsClusterReconciler{}

		if err := r.restartPrincipalDeployment(context.Background(), kube, namespace); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		dep, err := kube.AppsV1().Deployments(namespace).Get(context.Background(), "argocd-agent-principal", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get deployment: %v", err)
		}
		if _, ok := dep.Spec.Template.ObjectMeta.Annotations[restartAnnotationKey]; !ok {
			t.Errorf("expected restart annotation %q on pod template, got %v", restartAnnotationKey, dep.Spec.Template.ObjectMeta.Annotations)
		}
	})

	t.Run("falls back to openshift-gitops naming", func(t *testing.T) {
		kube := kubefake.NewSimpleClientset(principalDeployment("openshift-gitops-agent-principal", namespace))
		r := &GitOpsClusterReconciler{}

		if err := r.restartPrincipalDeployment(context.Background(), kube, namespace); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		dep, err := kube.AppsV1().Deployments(namespace).Get(context.Background(), "openshift-gitops-agent-principal", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get deployment: %v", err)
		}
		if _, ok := dep.Spec.Template.ObjectMeta.Annotations[restartAnnotationKey]; !ok {
			t.Errorf("expected restart annotation on fallback deployment")
		}
	})

	t.Run("missing deployment returns error (non-fatal to caller)", func(t *testing.T) {
		kube := kubefake.NewSimpleClientset() // no deployment
		r := &GitOpsClusterReconciler{}

		if err := r.restartPrincipalDeployment(context.Background(), kube, namespace); err == nil {
			t.Errorf("expected an error when the principal Deployment is absent")
		}
	})
}
