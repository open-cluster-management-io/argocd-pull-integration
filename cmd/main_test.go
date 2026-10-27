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

package main

import (
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "open-cluster-management.io/argocd-pull-integration/api/v1alpha1"
)

func TestControllerCacheOptionsDefaultIsClusterWide(t *testing.T) {
	opts := controllerCacheOptions("")
	if opts.ByObject != nil || opts.DefaultNamespaces != nil {
		t.Fatalf("expected default cache options, got ByObject=%v DefaultNamespaces=%v", opts.ByObject, opts.DefaultNamespaces)
	}
}

func TestControllerCacheOptionsScopesNamespacedObjects(t *testing.T) {
	opts := controllerCacheOptions("argocd")
	if opts.DefaultNamespaces != nil {
		t.Fatalf("expected no default namespace restriction, got %v", opts.DefaultNamespaces)
	}

	scoped := map[string]bool{}
	for obj, byObject := range opts.ByObject {
		if _, ok := byObject.Namespaces["argocd"]; !ok || len(byObject.Namespaces) != 1 {
			t.Errorf("%T: expected only namespace argocd, got %v", obj, byObject.Namespaces)
		}
		scoped[fmt.Sprintf("%T", obj)] = true
	}

	// Everything the controller reads and writes in the GitOpsCluster namespace.
	for _, obj := range []client.Object{
		&corev1.Secret{}, &corev1.ConfigMap{}, &corev1.ServiceAccount{}, &corev1.Service{},
		&appsv1.Deployment{}, &rbacv1.Role{}, &rbacv1.RoleBinding{},
		&appsv1alpha1.GitOpsCluster{}, &clusterv1beta1.Placement{}, &clusterv1beta1.PlacementDecision{},
	} {
		if !scoped[fmt.Sprintf("%T", obj)] {
			t.Errorf("%T is not scoped to the cache namespace", obj)
		}
	}

	// Cluster-scoped objects, and objects in the managed clusters' namespaces, stay cluster-wide.
	for _, obj := range []client.Object{&clusterv1.ManagedCluster{}, &workv1.ManifestWork{}} {
		if scoped[fmt.Sprintf("%T", obj)] {
			t.Errorf("%T must not be scoped to the cache namespace", obj)
		}
	}
}
