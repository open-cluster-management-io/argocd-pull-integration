/*
Copyright 2022.

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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	argov1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
)

var _ = Describe("Application Pull controller", func() {

	const (
		appName      = "app-1"
		appName2     = "app-2"
		appNamespace = "default"
		clusterName  = "cluster1"
	)

	appKey := types.NamespacedName{Name: appName, Namespace: appNamespace}
	appKey2 := types.NamespacedName{Name: appName2, Namespace: appNamespace}
	ctx := context.Background()

	Context("When Application without OCM pull label is created", func() {
		It("Should not create ManifestWork", func() {
			By("Creating the Application without OCM pull label")
			app1 := argov1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:        appName,
					Namespace:   appNamespace,
					Annotations: map[string]string{AnnotationKeyOCMManagedCluster: clusterName},
				},
			}
			Expect(k8sClient.Create(ctx, &app1)).Should(Succeed())
			app1 = argov1alpha1.Application{}
			Expect(k8sClient.Get(ctx, appKey, &app1)).Should(Succeed())

			mwKey := types.NamespacedName{Name: generateManifestWorkName(app1), Namespace: clusterName}
			mw := workv1.ManifestWork{}
			Consistently(func() bool {
				if err := k8sClient.Get(ctx, mwKey, &mw); err != nil {
					return false
				}
				return true
			}).Should(BeFalse())
		})
	})

	Context("When Application with OCM pull label is created/updated/deleted", func() {
		It("Should create/update/delete ManifestWork", func() {
			By("Creating the OCM ManagedCluster")
			managedCluster := clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
			}
			Expect(k8sClient.Create(ctx, &managedCluster)).Should(Succeed())

			By("Creating the OCM ManagedCluster namespace")
			managedClusterNs := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
			}
			Expect(k8sClient.Create(ctx, &managedClusterNs)).Should(Succeed())

			By("Creating the Application with OCM pull label")
			app2 := argov1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:        appName2,
					Namespace:   appNamespace,
					Labels:      map[string]string{LabelKeyPull: strconv.FormatBool(true)},
					Annotations: map[string]string{AnnotationKeyOCMManagedCluster: clusterName},
					Finalizers:  []string{argov1alpha1.ResourcesFinalizerName},
				},
				Spec: argov1alpha1.ApplicationSpec{
					Project: "default",
				},
			}
			Expect(k8sClient.Create(ctx, &app2)).Should(Succeed())
			app2 = argov1alpha1.Application{}
			Expect(k8sClient.Get(ctx, appKey2, &app2)).Should(Succeed())

			mwKey := types.NamespacedName{Name: generateManifestWorkName(app2), Namespace: clusterName}
			mw := workv1.ManifestWork{}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, mwKey, &mw); err != nil {
					return false
				}
				return true
			}).Should(BeTrue())

			By("Updating the Application")
			oldRv := mw.GetResourceVersion()
			app2.Spec.Project = "somethingelse"
			Expect(k8sClient.Update(ctx, &app2)).Should(Succeed())
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, mwKey, &mw); err != nil {
					return false
				}
				return oldRv != mw.GetResourceVersion()
			}).Should(BeTrue())

			By("Deleting the Application")
			Expect(k8sClient.Delete(ctx, &app2)).Should(Succeed())
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, mwKey, &mw); err != nil {
					return true
				}
				return false
			}).Should(BeTrue())
		})
	})
})
