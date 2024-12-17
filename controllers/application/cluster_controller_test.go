/*
Copyright 2024.

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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	corev1 "k8s.io/api/core/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

var _ = Describe("Cluster Controller", func() {

	const (
		clusterName  = "cluster3"
		clusterName2 = "cluster4"
		argocdNs     = "argocd"
	)

	secretKey := types.NamespacedName{Name: clusterName + "-secret", Namespace: argocdNs}
	secretKey2 := types.NamespacedName{Name: clusterName2 + "-secret", Namespace: argocdNs}
	ctx := context.Background()

	BeforeEach(func() {
		argocdNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: argocdNs,
			},
		}
		_ = k8sClient.Create(ctx, argocdNamespace)
	})

	Context("When a ManagedCluster is created", func() {
		It("Should create a corresponding Secret with correct content", func() {
			By("Creating the ManagedCluster")
			managedCluster := &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
			}
			Expect(k8sClient.Create(ctx, managedCluster)).Should(Succeed())

			By("Ensuring the Secret is created with correct content")
			secret := &corev1.Secret{}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, secretKey, secret); err != nil {
					return false
				}
				name, nameOk := secret.Data["name"]
				server, serverOk := secret.Data["server"]
				return nameOk && serverOk &&
					string(name) == clusterName &&
					string(server) == "https://"+clusterName+"-control-plane:6443"
			}).Should(BeTrue())
		})
	})

	Context("When a ManagedCluster is deleted", func() {
		It("Should delete the corresponding Secret", func() {
			By("Creating the ManagedCluster")
			managedCluster2 := &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName2,
				},
			}
			Expect(k8sClient.Create(ctx, managedCluster2)).Should(Succeed())

			By("Ensuring the Secret is created")
			secret := &corev1.Secret{}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, secretKey2, secret); err != nil {
					return false
				}
				return true
			}).Should(BeTrue())

			By("Deleting the ManagedCluster")
			Expect(k8sClient.Delete(ctx, managedCluster2)).Should(Succeed())

			By("Ensuring the Secret is deleted")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, secretKey2, secret)
				return err != nil
			}).Should(BeTrue())
		})
	})
})
