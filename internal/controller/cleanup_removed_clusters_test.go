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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	appsv1alpha1 "open-cluster-management.io/argocd-pull-integration/api/v1alpha1"
)

const (
	cleanupTestNamespace = "argocd"
	cleanupTestPlacement = "placement"
)

func cleanupTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		scheme.AddToScheme, appsv1alpha1.AddToScheme, clusterv1.Install, addonv1alpha1.Install, workv1.Install,
	} {
		if err := add(s); err != nil {
			t.Fatalf("scheme: %v", err)
		}
	}
	return s
}

func cleanupTestGitOpsCluster() *appsv1alpha1.GitOpsCluster {
	return &appsv1alpha1.GitOpsCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "gitops-cluster", Namespace: cleanupTestNamespace},
		Spec: appsv1alpha1.GitOpsClusterSpec{
			PlacementRef: corev1.ObjectReference{Kind: "Placement", Name: cleanupTestPlacement},
		},
	}
}

func cleanupTestCluster(name string) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

// clusterSecret is the Argo CD cluster Secret the controller creates for a cluster in placement.
func clusterSecret(cluster, placement string) *corev1.Secret {
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name:      fmt.Sprintf("cluster-%s", cluster),
		Namespace: cleanupTestNamespace,
		Labels: map[string]string{
			ArgoCDSecretTypeLabel:                          ArgoCDSecretTypeValue,
			"apps.open-cluster-management.io/cluster-name": cluster,
			ArgoCDAgentClusterMappingLabel:                 cluster,
			"placement-name":                               placement,
		},
	}}
}

// clientSecret is the cluster's agent client certificate, which carries no labels.
func clientSecret(cluster string) *corev1.Secret {
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name: fmt.Sprintf("argocd-cluster-client-%s", cluster), Namespace: cleanupTestNamespace,
	}}
}

func addon(cluster string) *addonv1alpha1.ManagedClusterAddOn {
	return &addonv1alpha1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{
		Name: ArgoCDAgentAddonName, Namespace: cluster,
		Labels: map[string]string{"app.kubernetes.io/managed-by": "argocd-pull-integration"},
	}}
}

func addonConfig(cluster string) *addonv1alpha1.AddOnDeploymentConfig {
	return &addonv1alpha1.AddOnDeploymentConfig{ObjectMeta: metav1.ObjectMeta{
		Name: ArgoCDAgentAddonConfigName, Namespace: cluster,
		Labels: map[string]string{
			"app.kubernetes.io/managed-by": "argocd-pull-integration",
			"app.kubernetes.io/component":  "addon-config",
		},
	}}
}

func caManifestWork(cluster string) *workv1.ManifestWork {
	return &workv1.ManifestWork{ObjectMeta: metav1.ObjectMeta{Name: ArgoCDAgentCAManifestWorkName, Namespace: cluster}}
}

func crManifestWorkName() string {
	return getArgoCDCRManifestWorkName(cleanupTestNamespace, cleanupTestGitOpsCluster().Name)
}

// crManifestWork is the Argo CD CR ManifestWork this GitOpsCluster creates in a cluster's namespace.
func crManifestWork(cluster string) *workv1.ManifestWork {
	return &workv1.ManifestWork{ObjectMeta: metav1.ObjectMeta{Name: crManifestWorkName(), Namespace: cluster}}
}

func exists(t *testing.T, c client.Client, obj client.Object, namespace, name string) bool {
	t.Helper()
	err := c.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, obj)
	if k8serrors.IsNotFound(err) {
		return false
	}
	if err != nil {
		t.Fatalf("get %s/%s: %v", namespace, name, err)
	}
	return true
}

func TestCleanupRemovedClusters(t *testing.T) {
	tests := []struct {
		name        string
		objects     []client.Object
		current     []string
		wantRemoved int
		// failDelete names an object whose deletion fails.
		failDelete string
		wantErr    bool
		// gone and kept: "secret/<name>", "addon/<cluster>", "config/<cluster>", "ca-work/<cluster>",
		// "cr-work/<cluster>"
		gone []string
		kept []string
	}{
		{
			// A ManagedCluster deleted with the hub's ResourceCleanup garbage collector on (the default):
			// the collector deletes the cluster's add-on and ManifestWorks before this reconcile runs,
			// and the cluster namespace stays.
			name: "a deleted cluster whose add-on the hub's garbage collector already removed",
			objects: []client.Object{
				clusterSecret("cell1", cleanupTestPlacement), clientSecret("cell1"), addonConfig("cell1"),
			},
			wantRemoved: 1,
			gone:        []string{"secret/cluster-cell1", "secret/argocd-cluster-client-cell1", "config/cell1"},
		},
		{
			name: "a cluster removed from placement, its add-on still there",
			objects: []client.Object{
				cleanupTestCluster("cell1"), addon("cell1"), caManifestWork("cell1"),
				clusterSecret("cell1", cleanupTestPlacement), clientSecret("cell1"), addonConfig("cell1"),
			},
			wantRemoved: 1,
			gone: []string{
				"addon/cell1", "ca-work/cell1", "secret/cluster-cell1", "secret/argocd-cluster-client-cell1",
			},
			// The add-on's pre-delete cleanup needs its config, and the cluster still exists.
			kept: []string{"config/cell1"},
		},
		{
			name: "the config of a deleted cluster, once its add-on has finished deleting",
			objects: []client.Object{
				addonConfig("cell1"),
			},
			wantRemoved: 1,
			gone:        []string{"config/cell1"},
		},
		{
			name: "a cluster still in placement",
			objects: []client.Object{
				cleanupTestCluster("cell1"), addon("cell1"), caManifestWork("cell1"),
				clusterSecret("cell1", cleanupTestPlacement), clientSecret("cell1"), addonConfig("cell1"),
			},
			current:     []string{"cell1"},
			wantRemoved: 0,
			kept: []string{
				"addon/cell1", "ca-work/cell1", "secret/cluster-cell1", "secret/argocd-cluster-client-cell1",
				"config/cell1",
			},
		},
		{
			name: "a cluster Secret of another placement",
			objects: []client.Object{
				clusterSecret("cell2", "other-placement"), clientSecret("cell2"),
			},
			wantRemoved: 0,
			kept:        []string{"secret/cluster-cell2", "secret/argocd-cluster-client-cell2"},
		},
		{
			name: "the config of a cluster that still exists, outside placement, with no add-on",
			objects: []client.Object{
				cleanupTestCluster("cell3"), addonConfig("cell3"),
			},
			wantRemoved: 0,
			kept:        []string{"config/cell3"},
		},
		{
			// Two GitOpsClusters in one Argo CD namespace: the other one's cluster is not this one's to clean.
			name: "a cluster another placement registered, found from its add-on",
			objects: []client.Object{
				addon("cell4"), clusterSecret("cell4", "other-placement"), clientSecret("cell4"),
			},
			wantRemoved: 0,
			kept:        []string{"addon/cell4", "secret/cluster-cell4", "secret/argocd-cluster-client-cell4"},
		},
		{
			name: "a cluster another placement registered, found from its config",
			objects: []client.Object{
				addonConfig("cell5"), clusterSecret("cell5", "other-placement"), clientSecret("cell5"),
			},
			wantRemoved: 0,
			kept:        []string{"config/cell5", "secret/cluster-cell5", "secret/argocd-cluster-client-cell5"},
		},
		{
			name: "a deleted cluster's Argo CD CR ManifestWork, its last object but the config",
			objects: []client.Object{
				crManifestWork("cell6"), addonConfig("cell6"),
			},
			wantRemoved: 1,
			gone:        []string{"cr-work/cell6", "config/cell6"},
		},
		{
			// The config is how a later reconcile finds the cluster again.
			name: "a failed delete keeps the config",
			objects: []client.Object{
				clusterSecret("cell7", cleanupTestPlacement), clientSecret("cell7"), addonConfig("cell7"),
			},
			failDelete:  "argocd-cluster-client-cell7",
			wantErr:     true,
			wantRemoved: 1,
			gone:        []string{"secret/cluster-cell7"},
			kept:        []string{"secret/argocd-cluster-client-cell7", "config/cell7"},
		},
		{
			name:        "nothing left of any cluster",
			wantRemoved: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(cleanupTestScheme(t)).WithObjects(tt.objects...)
			if tt.failDelete != "" {
				builder = builder.WithInterceptorFuncs(interceptor.Funcs{
					Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
						if obj.GetName() == tt.failDelete {
							return fmt.Errorf("injected failure deleting %s", obj.GetName())
						}
						return c.Delete(ctx, obj, opts...)
					},
				})
			}
			c := builder.Build()
			r := &GitOpsClusterReconciler{Client: c}
			current := []*clusterv1.ManagedCluster{}
			for _, name := range tt.current {
				current = append(current, cleanupTestCluster(name))
			}

			removed, err := r.cleanupRemovedClusters(context.TODO(), cleanupTestGitOpsCluster(), current)
			if (err != nil) != tt.wantErr {
				t.Fatalf("cleanupRemovedClusters: error %v, want error %v", err, tt.wantErr)
			}
			if removed != tt.wantRemoved {
				t.Errorf("removed %d clusters, want %d", removed, tt.wantRemoved)
			}

			find := func(ref string) bool {
				kind, name, _ := strings.Cut(ref, "/")
				switch kind {
				case "secret":
					return exists(t, c, &corev1.Secret{}, cleanupTestNamespace, name)
				case "addon":
					return exists(t, c, &addonv1alpha1.ManagedClusterAddOn{}, name, ArgoCDAgentAddonName)
				case "config":
					return exists(t, c, &addonv1alpha1.AddOnDeploymentConfig{}, name, ArgoCDAgentAddonConfigName)
				case "ca-work":
					return exists(t, c, &workv1.ManifestWork{}, name, ArgoCDAgentCAManifestWorkName)
				case "cr-work":
					return exists(t, c, &workv1.ManifestWork{}, name, crManifestWorkName())
				}
				t.Fatalf("unknown ref %q", ref)
				return false
			}
			for _, ref := range tt.gone {
				if find(ref) {
					t.Errorf("%s still exists", ref)
				}
			}
			for _, ref := range tt.kept {
				if !find(ref) {
					t.Errorf("%s was deleted", ref)
				}
			}
		})
	}
}
