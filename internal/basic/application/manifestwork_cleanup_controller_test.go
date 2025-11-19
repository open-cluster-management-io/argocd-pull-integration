/*
Copyright 2025.

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

package application

import (
	"context"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
)

var _ = Describe("ManifestWork Cleanup Controller", func() {
	const (
		appName4     = "app-cleanup-4"
		appName5     = "app-cleanup-5"
		appNamespace = "default"
		clusterName3 = "cluster-cleanup-test"
	)

	appKey4 := types.NamespacedName{Name: appName4, Namespace: appNamespace}
	appKey5 := types.NamespacedName{Name: appName5, Namespace: appNamespace}
	ctx := context.Background()

	Context("When ManifestWork cleanup runs", func() {
		It("Should delete orphaned ManifestWorks", func() {
			By("Creating the OCM ManagedCluster")
			managedCluster3 := clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName3,
				},
			}
			_ = k8sClient.Delete(ctx, &managedCluster3) // Clean up if exists from previous run
			Expect(k8sClient.Create(ctx, &managedCluster3)).Should(Succeed())

			By("Creating the OCM ManagedCluster namespace")
			managedClusterNs3 := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName3,
				},
			}
			_ = k8sClient.Delete(ctx, &managedClusterNs3) // Clean up if exists from previous run
			Expect(k8sClient.Create(ctx, &managedClusterNs3)).Should(Succeed())

			By("Creating Application app-4")
			app4 := &unstructured.Unstructured{}
			app4.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "argoproj.io",
				Version: "v1alpha1",
				Kind:    "Application",
			})
			app4.SetName(appName4)
			app4.SetNamespace(appNamespace)
			app4.SetLabels(map[string]string{LabelKeyPull: strconv.FormatBool(true)})
			app4.SetAnnotations(map[string]string{AnnotationKeyOCMManagedCluster: clusterName3})

			// Set required spec fields
			_ = unstructured.SetNestedField(app4.Object, "default", "spec", "project")
			_ = unstructured.SetNestedField(app4.Object, "default", "spec", "source", "repoURL")
			_ = unstructured.SetNestedMap(app4.Object, map[string]interface{}{"server": KubernetesInternalAPIServerAddr}, "spec", "destination")

			Expect(k8sClient.Create(ctx, app4)).Should(Succeed())
			app4 = &unstructured.Unstructured{}
			app4.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "argoproj.io",
				Version: "v1alpha1",
				Kind:    "Application",
			})
			Expect(k8sClient.Get(ctx, appKey4, app4)).Should(Succeed())

			By("Creating Application app-5")
			app5 := &unstructured.Unstructured{}
			app5.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "argoproj.io",
				Version: "v1alpha1",
				Kind:    "Application",
			})
			app5.SetName(appName5)
			app5.SetNamespace(appNamespace)
			app5.SetLabels(map[string]string{LabelKeyPull: strconv.FormatBool(true)})
			app5.SetAnnotations(map[string]string{AnnotationKeyOCMManagedCluster: clusterName3})

			// Set required spec fields
			_ = unstructured.SetNestedField(app5.Object, "default", "spec", "project")
			_ = unstructured.SetNestedField(app5.Object, "default", "spec", "source", "repoURL")
			_ = unstructured.SetNestedMap(app5.Object, map[string]interface{}{"server": KubernetesInternalAPIServerAddr}, "spec", "destination")

			Expect(k8sClient.Create(ctx, app5)).Should(Succeed())
			app5 = &unstructured.Unstructured{}
			app5.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "argoproj.io",
				Version: "v1alpha1",
				Kind:    "Application",
			})
			Expect(k8sClient.Get(ctx, appKey5, app5)).Should(Succeed())

			By("Waiting for ManifestWorks to be created")
			mwKey4 := types.NamespacedName{Name: generateManifestWorkName(app4.GetName(), app4.GetUID()), Namespace: clusterName3}
			mwKey5 := types.NamespacedName{Name: generateManifestWorkName(app5.GetName(), app5.GetUID()), Namespace: clusterName3}

			Eventually(func() bool {
				mw4 := workv1.ManifestWork{}
				return k8sClient.Get(ctx, mwKey4, &mw4) == nil
			}).Should(BeTrue())

			Eventually(func() bool {
				mw5 := workv1.ManifestWork{}
				return k8sClient.Get(ctx, mwKey5, &mw5) == nil
			}).Should(BeTrue())

			By("Deleting Application app-5 (will be orphaned)")
			Expect(k8sClient.Delete(ctx, app5)).Should(Succeed())

			By("Waiting for app-5 to be deleted")
			Eventually(func() bool {
				tempApp := &unstructured.Unstructured{}
				tempApp.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "argoproj.io",
					Version: "v1alpha1",
					Kind:    "Application",
				})
				err := k8sClient.Get(ctx, appKey5, tempApp)
				return err != nil
			}).Should(BeTrue())

			By("Verifying ManifestWork for app-5 still exists initially")
			mw5 := workv1.ManifestWork{}
			Expect(k8sClient.Get(ctx, mwKey5, &mw5)).Should(Succeed())

			By("Running cleanup controller once")
			cleanupReconciler := &ManifestWorkCleanupReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Interval: 5 * time.Minute,
			}
			Expect(cleanupReconciler.cleanup(ctx)).Should(Succeed())

			By("Verifying orphaned ManifestWork for app-5 is deleted")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, mwKey5, &mw5)
				return err != nil
			}).Should(BeTrue())

			By("Verifying ManifestWork for app-4 still exists (not orphaned)")
			mw4 := workv1.ManifestWork{}
			Expect(k8sClient.Get(ctx, mwKey4, &mw4)).Should(Succeed())

			By("Cleaning up app-4")
			Expect(k8sClient.Delete(ctx, app4)).Should(Succeed())
		})
	})
})
