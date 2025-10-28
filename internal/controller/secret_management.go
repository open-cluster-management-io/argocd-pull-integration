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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ArgoCDRedisSecretName is the name of the Redis secret
	ArgoCDRedisSecretName = "argocd-redis"

	// ArgoCDAgentJWTSecretName is the name of the JWT secret
	ArgoCDAgentJWTSecretName = "argocd-agent-jwt"
)

// EnsureArgoCDRedisSecret ensures the ArgoCD Redis secret exists
func (r *GitOpsClusterReconciler) EnsureArgoCDRedisSecret(ctx context.Context, namespace string) error {
	secretName := types.NamespacedName{
		Namespace: namespace,
		Name:      ArgoCDRedisSecretName,
	}

	// Check if secret already exists
	secret := &corev1.Secret{}
	err := r.Get(ctx, secretName, secret)
	if err == nil {
		klog.V(2).InfoS("ArgoCD Redis secret already exists", "namespace", namespace)
		return nil
	}

	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to check ArgoCD Redis secret: %w", err)
	}

	klog.InfoS("ArgoCD Redis secret not found, creating it", "namespace", namespace)

	// Find the Redis initial password secret
	redisPasswordSecret, err := r.findRedisInitialPasswordSecret(ctx, namespace)
	if err != nil {
		return fmt.Errorf("failed to find Redis initial password secret: %w", err)
	}

	// Extract the admin.password value
	adminPassword, exists := redisPasswordSecret.Data["admin.password"]
	if !exists {
		return fmt.Errorf("admin.password not found in secret %s", redisPasswordSecret.Name)
	}

	// Create the argocd-redis secret
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ArgoCDRedisSecretName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "argocd-pull-integration",
				"app.kubernetes.io/component":  "redis",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"auth": adminPassword,
		},
	}

	if err := r.Create(ctx, newSecret); err != nil {
		if k8serrors.IsAlreadyExists(err) {
			klog.InfoS("ArgoCD Redis secret was created by another process", "namespace", namespace)
			return nil
		}
		return fmt.Errorf("failed to create ArgoCD Redis secret: %w", err)
	}

	klog.InfoS("Successfully created ArgoCD Redis secret", "namespace", namespace)
	return nil
}

// findRedisInitialPasswordSecret finds the secret ending with "redis-initial-password"
func (r *GitOpsClusterReconciler) findRedisInitialPasswordSecret(ctx context.Context, namespace string) (*corev1.Secret, error) {
	secretList := &corev1.SecretList{}
	err := r.List(ctx, secretList, client.InNamespace(namespace))
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets in namespace %s: %w", namespace, err)
	}

	for i := range secretList.Items {
		if strings.HasSuffix(secretList.Items[i].Name, "redis-initial-password") {
			klog.V(2).InfoS("Found Redis initial password secret",
				"namespace", namespace, "secret", secretList.Items[i].Name)
			return &secretList.Items[i], nil
		}
	}

	return nil, fmt.Errorf("no secret found ending with 'redis-initial-password' in namespace %s", namespace)
}

// EnsureArgoCDAgentJWTSecret ensures the ArgoCD Agent JWT secret exists
func (r *GitOpsClusterReconciler) EnsureArgoCDAgentJWTSecret(ctx context.Context, namespace string) error {
	secretName := types.NamespacedName{
		Namespace: namespace,
		Name:      ArgoCDAgentJWTSecretName,
	}

	// Check if secret already exists
	secret := &corev1.Secret{}
	err := r.Get(ctx, secretName, secret)
	if err == nil {
		klog.V(2).InfoS("ArgoCD Agent JWT secret already exists", "namespace", namespace)
		return nil
	}

	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to check ArgoCD Agent JWT secret: %w", err)
	}

	klog.InfoS("ArgoCD Agent JWT secret not found, creating JWT signing key", "namespace", namespace)

	// Generate 4096-bit RSA private key for JWT signing
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return fmt.Errorf("failed to generate RSA private key: %w", err)
	}

	// Convert to PKCS#8 PEM format
	keyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyBytes,
	})

	// Create the argocd-agent-jwt secret
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ArgoCDAgentJWTSecretName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "argocd-pull-integration",
				"app.kubernetes.io/component":  "jwt",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"jwt.key": keyPEM,
		},
	}

	if err := r.Create(ctx, newSecret); err != nil {
		if k8serrors.IsAlreadyExists(err) {
			klog.InfoS("ArgoCD Agent JWT secret was created by another process", "namespace", namespace)
			return nil
		}
		return fmt.Errorf("failed to create ArgoCD Agent JWT secret: %w", err)
	}

	klog.InfoS("Successfully created ArgoCD Agent JWT secret with RSA-4096 signing key", "namespace", namespace)
	return nil
}
