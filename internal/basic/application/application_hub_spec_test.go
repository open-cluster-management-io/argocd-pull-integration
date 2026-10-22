/*
Copyright 2026.

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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
)

var _ = Describe("Application Pull controller hub spec", func() {
	const (
		appName      = "app-hub-spec"
		appNamespace = "default"
		clusterName  = "cluster-hub-spec"
		hubServer    = "https://cluster-hub-spec.example.com:6443"
	)

	appKey := types.NamespacedName{Name: appName, Namespace: appNamespace}
	ctx := context.Background()

	newApplication := func() *unstructured.Unstructured {
		app := &unstructured.Unstructured{}
		app.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "argoproj.io",
			Version: "v1alpha1",
			Kind:    "Application",
		})
		return app
	}

	It("Should not rewrite the hub Application destination when it removes the operation", func() {
		By("Creating the OCM ManagedCluster and its namespace")
		Expect(k8sClient.Create(ctx, &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName},
		})).Should(Succeed())
		Expect(k8sClient.Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName},
		})).Should(Succeed())

		By("Creating a pull Application that targets the managed cluster and has an operation")
		app := newApplication()
		app.SetName(appName)
		app.SetNamespace(appNamespace)
		app.SetLabels(map[string]string{LabelKeyPull: strconv.FormatBool(true)})
		app.SetAnnotations(map[string]string{AnnotationKeyOCMManagedCluster: clusterName})
		_ = unstructured.SetNestedField(app.Object, "default", "spec", "project")
		_ = unstructured.SetNestedField(app.Object, "default", "spec", "source", "repoURL")
		_ = unstructured.SetNestedMap(app.Object, map[string]interface{}{
			"server":    hubServer,
			"namespace": "guestbook",
		}, "spec", "destination")
		_ = unstructured.SetNestedMap(app.Object, map[string]interface{}{
			"sync": map[string]interface{}{},
		}, "operation")
		Expect(k8sClient.Create(ctx, app)).Should(Succeed())

		By("Waiting for the ManifestWork and for the operation to be removed from the hub Application")
		hubApp := newApplication()
		Expect(k8sClient.Get(ctx, appKey, hubApp)).Should(Succeed())
		mwKey := types.NamespacedName{Name: generateManifestWorkName(hubApp.GetName(), hubApp.GetUID()), Namespace: clusterName}
		Eventually(func() error {
			return k8sClient.Get(ctx, mwKey, &workv1.ManifestWork{})
		}).Should(Succeed())
		Eventually(func() bool {
			hubApp = newApplication()
			if err := k8sClient.Get(ctx, appKey, hubApp); err != nil {
				return false
			}
			_, ok := hubApp.Object["operation"]
			return !ok
		}).Should(BeTrue())

		By("Checking the hub Application keeps its own destination")
		server, _, _ := unstructured.NestedString(hubApp.Object, "spec", "destination", "server")
		Expect(server).To(Equal(hubServer))
		namespace, _, _ := unstructured.NestedString(hubApp.Object, "spec", "destination", "namespace")
		Expect(namespace).To(Equal("guestbook"))

		By("Checking the ManifestWork payload still points at the in-cluster API server")
		var mw workv1.ManifestWork
		Expect(k8sClient.Get(ctx, mwKey, &mw)).Should(Succeed())
		Expect(mw.Spec.Workload.Manifests).To(HaveLen(1))
		payload := &unstructured.Unstructured{}
		Expect(payload.UnmarshalJSON(mw.Spec.Workload.Manifests[0].Raw)).Should(Succeed())
		payloadServer, _, _ := unstructured.NestedString(payload.Object, "spec", "destination", "server")
		Expect(payloadServer).To(Equal(KubernetesInternalAPIServerAddr))

		By("Deleting the Application")
		Expect(k8sClient.Delete(ctx, hubApp)).Should(Succeed())
		Eventually(func() bool {
			return k8sClient.Get(ctx, mwKey, &workv1.ManifestWork{}) != nil
		}).Should(BeTrue())
	})
})
