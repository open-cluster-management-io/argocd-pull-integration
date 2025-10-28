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
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	appsv1alpha1 "open-cluster-management.io/argocd-pull-integration/api/v1alpha1"
)

func TestEnsureArgoCDRedisSecret(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = appsv1alpha1.AddToScheme(s)

	namespace := "argocd"
	redisPassword := []byte("test-password-123")

	tests := []struct {
		name          string
		existingObjs  []runtime.Object
		wantErr       bool
		wantSecretKey string
	}{
		{
			name: "creates secret when redis initial password exists",
			existingObjs: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-argocd-redis-initial-password",
						Namespace: namespace,
					},
					Data: map[string][]byte{
						"admin.password": redisPassword,
					},
				},
			},
			wantErr:       false,
			wantSecretKey: "auth",
		},
		{
			name: "returns no error when secret already exists",
			existingObjs: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ArgoCDRedisSecretName,
						Namespace: namespace,
					},
					Data: map[string][]byte{
						"auth": redisPassword,
					},
				},
			},
			wantErr: false,
		},
		{
			name:         "returns error when no redis initial password secret",
			existingObjs: []runtime.Object{},
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).Build(),
				Scheme: s,
			}

			err := r.EnsureArgoCDRedisSecret(context.Background(), namespace)
			if (err != nil) != tt.wantErr {
				t.Errorf("EnsureArgoCDRedisSecret() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.wantSecretKey != "" {
				// Verify the secret was created
				secret := &corev1.Secret{}
				err := r.Get(context.Background(), types.NamespacedName{
					Name:      ArgoCDRedisSecretName,
					Namespace: namespace,
				}, secret)
				if err != nil {
					t.Errorf("Failed to get created secret: %v", err)
					return
				}

				if _, ok := secret.Data[tt.wantSecretKey]; !ok {
					t.Errorf("Secret missing expected key %q", tt.wantSecretKey)
				}
			}
		})
	}
}

func TestFindRedisInitialPasswordSecret(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)

	namespace := "argocd"

	tests := []struct {
		name         string
		existingObjs []runtime.Object
		wantName     string
		wantErr      bool
	}{
		{
			name: "finds secret with redis-initial-password suffix",
			existingObjs: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-argocd-redis-initial-password",
						Namespace: namespace,
					},
					Data: map[string][]byte{
						"admin.password": []byte("test"),
					},
				},
			},
			wantName: "my-argocd-redis-initial-password",
			wantErr:  false,
		},
		{
			name: "returns error when no matching secret",
			existingObjs: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "some-other-secret",
						Namespace: namespace,
					},
				},
			},
			wantErr: true,
		},
		{
			name:         "returns error when namespace is empty",
			existingObjs: []runtime.Object{},
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).Build(),
				Scheme: s,
			}

			secret, err := r.findRedisInitialPasswordSecret(context.Background(), namespace)
			if (err != nil) != tt.wantErr {
				t.Errorf("findRedisInitialPasswordSecret() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && secret.Name != tt.wantName {
				t.Errorf("findRedisInitialPasswordSecret() name = %v, want %v", secret.Name, tt.wantName)
			}
		})
	}
}

func TestEnsureArgoCDAgentJWTSecret(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)

	namespace := "argocd"

	tests := []struct {
		name         string
		existingObjs []runtime.Object
		wantErr      bool
	}{
		{
			name:         "creates JWT secret when not exists",
			existingObjs: []runtime.Object{},
			wantErr:      false,
		},
		{
			name: "returns no error when JWT secret already exists",
			existingObjs: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ArgoCDAgentJWTSecretName,
						Namespace: namespace,
					},
					Data: map[string][]byte{
						"jwt.key": []byte("existing-key"),
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).Build(),
				Scheme: s,
			}

			err := r.EnsureArgoCDAgentJWTSecret(context.Background(), namespace)
			if (err != nil) != tt.wantErr {
				t.Errorf("EnsureArgoCDAgentJWTSecret() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify the secret was created or exists
				secret := &corev1.Secret{}
				err := r.Get(context.Background(), types.NamespacedName{
					Name:      ArgoCDAgentJWTSecretName,
					Namespace: namespace,
				}, secret)
				if err != nil {
					t.Errorf("Failed to get JWT secret: %v", err)
					return
				}

				// Verify the jwt.key exists
				jwtKey, ok := secret.Data["jwt.key"]
				if !ok {
					t.Error("JWT secret missing jwt.key")
					return
				}

				// If the secret was newly created, verify it's a valid RSA private key
				if len(tt.existingObjs) == 0 {
					block, _ := pem.Decode(jwtKey)
					if block == nil {
						t.Error("Failed to decode PEM block from jwt.key")
						return
					}

					privateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
					if err != nil {
						t.Errorf("Failed to parse private key: %v", err)
						return
					}

					// Verify it's an RSA key
					if _, ok := privateKey.(*rsa.PrivateKey); !ok {
						t.Error("Private key is not RSA")
					}
				}
			}
		})
	}
}
