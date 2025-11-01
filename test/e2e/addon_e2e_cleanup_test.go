//go:build e2e
// +build e2e

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

package e2e

import (
	"fmt"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/argocd-pull-integration/test/utils"
)

var _ = Describe("ArgoCD Agent Addon Cleanup E2E", Ordered, Label("cleanup"), func() {
	SetDefaultEventuallyTimeout(5 * time.Minute)
	SetDefaultEventuallyPollingInterval(5 * time.Second)

	Context("Complete Lifecycle with Cleanup", func() {
		It("should deploy the GitOpsCluster controller on hub", func() {
			By("Verifying test environment is ready")
			cmd := exec.Command("kubectl", "config", "get-contexts", hubContext)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			cmd = exec.Command("kubectl", "config", "get-contexts", cluster1Context)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying controller deployment exists on hub")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "deployment", "argocd-pull-integration-controller",
					"-n", argoCDNamespace,
					"-o", "jsonpath={.status.availableReplicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("1"))
			}).Should(Succeed())

			By("verifying controller pod is running on hub")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "pods",
					"-l", "app.kubernetes.io/name=argocd-pull-integration-controller",
					"-n", argoCDNamespace,
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}).Should(Succeed())
		})

		It("should verify all ArgoCD pods running on hub", func() {
			By("verifying ArgoCD principal pod is running")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "pods",
					"-n", argoCDNamespace,
					"-l", "app.kubernetes.io/name=argocd-agent-principal",
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}).Should(Succeed())

			By("verifying ArgoCD redis pod is running on hub")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "pods",
					"-n", argoCDNamespace,
					"-l", "app.kubernetes.io/name=argocd-redis",
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}).Should(Succeed())

			By("verifying ArgoCD repo-server pod is running on hub")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "pods",
					"-n", argoCDNamespace,
					"-l", "app.kubernetes.io/name=argocd-repo-server",
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}).Should(Succeed())

			By("verifying ArgoCD server pod is running on hub")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "pods",
					"-n", argoCDNamespace,
					"-l", "app.kubernetes.io/name=argocd-server",
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}).Should(Succeed())
		})

		It("should verify GitOpsCluster reconciles successfully", func() {
			By("verifying GitOpsCluster exists")
			cmd := exec.Command("kubectl", "--context", hubContext,
				"get", "gitopscluster", "gitops-cluster", "-n", argoCDNamespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for all conditions to be ready")
			conditions := []string{
				"RBACReady",
				"ServerDiscovered",
				"JWTSecretReady",
				"CACertificateReady",
				"PrincipalCertificateReady",
				"ResourceProxyCertificateReady",
				"PlacementTolerationConfigured",
				"AddOnTemplateReady",
				"PlacementEvaluated",
				"ClustersImported",
				"ManifestWorkCreated",
				"AddonConfigured",
			}

			for _, condition := range conditions {
				By("verifying " + condition + " condition")
				Eventually(func(g Gomega) {
					cmd := exec.Command("kubectl", "--context", hubContext,
						"get", "gitopscluster", "gitops-cluster", "-n", argoCDNamespace,
						"-o", "jsonpath={.status.conditions[?(@.type=='"+condition+"')].status}")
					output, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(output).To(Equal("True"))
				}).Should(Succeed())
			}
		})

		It("should deploy addon agent on spoke cluster", func() {
			By("verifying addon deployment exists on cluster1")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "deployment", "argocd-agent-addon",
					"-n", "open-cluster-management-agent-addon",
					"-o", "jsonpath={.status.availableReplicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("1"))
			}).Should(Succeed())

			By("verifying addon pod is running on cluster1")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "pods",
					"-l", "app=argocd-agent-addon",
					"-n", "open-cluster-management-agent-addon",
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}).Should(Succeed())
		})

		It("should deploy ArgoCD operator on spoke cluster", func() {
			By("verifying operator namespace exists")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "namespace", "argocd-operator-system")
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())

			By("verifying operator deployment exists")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "deployment",
					"-n", "argocd-operator-system",
					"-l", "app.kubernetes.io/managed-by=argocd-agent-addon",
					"-o", "jsonpath={.items[0].status.availableReplicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty())
			}).Should(Succeed())
		})

		It("should deploy ArgoCD CR on spoke cluster", func() {
			By("verifying ArgoCD CR is created")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "argocd", "argocd", "-n", argoCDNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}, 3*time.Minute, 5*time.Second).Should(Succeed())

			By("verifying ArgoCD agent pod is running")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "pods", "-n", argoCDNamespace,
					"-l", "app.kubernetes.io/name=argocd-agent-agent",
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}, 3*time.Minute, 5*time.Second).Should(Succeed())

			By("verifying ArgoCD application-controller pod is running")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "pods",
					"-n", argoCDNamespace,
					"-l", "app.kubernetes.io/name=argocd-application-controller",
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}).Should(Succeed())

			By("verifying ArgoCD redis pod is running on cluster1")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "pods",
					"-n", argoCDNamespace,
					"-l", "app.kubernetes.io/name=argocd-redis",
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}).Should(Succeed())

			By("verifying ArgoCD repo-server pod is running on cluster1")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "pods",
					"-n", argoCDNamespace,
					"-l", "app.kubernetes.io/name=argocd-repo-server",
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}).Should(Succeed())
		})

		It("should cleanup when placement is updated to remove clusters", func() {
			By("recording initial state")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "argocd", "argocd", "-n", argoCDNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}, 1*time.Minute, 5*time.Second).Should(Succeed(), "ArgoCD CR should exist before cleanup")

			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "deployment", "-n", "argocd-operator-system",
					"-l", "app.kubernetes.io/managed-by=argocd-agent-addon")
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}, 1*time.Minute, 5*time.Second).Should(Succeed(), "Operator deployment should exist before cleanup")

			By("verifying ManagedClusterAddOn exists")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "managedclusteraddon", "argocd-agent-addon",
					"-n", "cluster1")
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}, 1*time.Minute, 5*time.Second).Should(Succeed(), "ManagedClusterAddOn should exist before triggering cleanup")

			By("updating placement to select non-existent cluster to trigger cleanup")
			patchYAML := `{"spec":{"predicates":[{"requiredClusterSelector":{"labelSelector":{"matchLabels":{"non-existent-cluster":"true"}}}}]}}`
			cmd := exec.Command("kubectl", "--context", hubContext,
				"patch", "placement", "placement",
				"-n", argoCDNamespace,
				"--type=merge",
				"-p", patchYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying placement no longer selects cluster1")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "placement", "placement",
					"-n", argoCDNamespace,
					"-o", "jsonpath={.status.numberOfSelectedClusters}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("0"))
			}, 1*time.Minute, 5*time.Second).Should(Succeed(), "Placement should select 0 clusters")

			By("waiting for ManagedClusterAddOn to be automatically deleted")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "managedclusteraddon", "argocd-agent-addon",
					"-n", "cluster1")
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred(), "ManagedClusterAddOn should be deleted automatically")
			}, 5*time.Minute, 5*time.Second).Should(Succeed(), "ManagedClusterAddOn should be removed when placement list becomes empty")

			By("verifying ArgoCD cluster secret is cleaned up from hub")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "secret", "cluster-cluster1",
					"-n", argoCDNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred(), "ArgoCD cluster secret should be deleted")
			}, 1*time.Minute, 5*time.Second).Should(Succeed(), "ArgoCD cluster secret should be cleaned up")

			By("verifying ManifestWork is cleaned up from hub")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "manifestwork", "argocd-agent-ca-argocd",
					"-n", "cluster1")
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred(), "ManifestWork should be deleted")
			}, 1*time.Minute, 5*time.Second).Should(Succeed(), "ManifestWork should be cleaned up")
		})

		AfterAll(func() {
			By("Test complete - clusters preserved for inspection")
			fmt.Fprintf(GinkgoWriter, "\n")
			fmt.Fprintf(GinkgoWriter, "Clusters have been preserved for inspection:\n")
			fmt.Fprintf(GinkgoWriter, "  Hub: kubectl config use-context kind-hub\n")
			fmt.Fprintf(GinkgoWriter, "  Spoke: kubectl config use-context kind-cluster1\n")
			fmt.Fprintf(GinkgoWriter, "\n")
		})
	})
})
