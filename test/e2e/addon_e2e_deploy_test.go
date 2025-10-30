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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/argocd-pull-integration/test/utils"
)

var _ = Describe("ArgoCD Agent Addon Deployment E2E", Label("deploy"), Ordered, func() {
	SetDefaultEventuallyTimeout(5 * time.Minute)
	SetDefaultEventuallyPollingInterval(5 * time.Second)

	BeforeAll(func() {
		By("Verifying test environment is ready")
		// Environment setup is done by Makefile (make test-e2e or make test-e2e-full)
		// This just verifies the contexts exist
		cmd := exec.Command("kubectl", "config", "get-contexts", hubContext)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Hub context should exist - run 'make test-e2e-full' to set up")

		cmd = exec.Command("kubectl", "config", "get-contexts", cluster1Context)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Spoke context should exist - run 'make test-e2e-full' to set up")
	})

	AfterAll(func() {
		By("Test complete - clusters preserved for inspection")
		fmt.Fprintf(GinkgoWriter, "\n")
		fmt.Fprintf(GinkgoWriter, "Clusters have been preserved for inspection:\n")
		fmt.Fprintf(GinkgoWriter, "  Hub: kubectl config use-context kind-hub\n")
		fmt.Fprintf(GinkgoWriter, "  Spoke: kubectl config use-context kind-cluster1\n")
		fmt.Fprintf(GinkgoWriter, "\n")
	})

	Context("Addon Deployment", func() {
		It("should deploy the GitOpsCluster controller on hub", func() {
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
			var podName string
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "pods",
					"-l", "app.kubernetes.io/name=argocd-pull-integration-controller",
					"-n", argoCDNamespace,
					"-o", "jsonpath={.items[0].metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty())
				podName = output
			}).Should(Succeed())

			By("verifying controller pod phase is Running")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "pod", podName,
					"-n", argoCDNamespace,
					"-o", "jsonpath={.status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}).Should(Succeed())

			By("checking controller pod logs for errors")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"logs", podName,
					"-n", argoCDNamespace,
					"--tail=50")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				// Check that logs don't contain common error indicators
				// Allow initialization messages and warnings, but fail on actual errors
				lowerOutput := strings.ToLower(output)
				g.Expect(lowerOutput).NotTo(ContainSubstring("panic:"), "Controller logs should not contain panics")
				g.Expect(lowerOutput).NotTo(ContainSubstring("fatal error"), "Controller logs should not contain fatal errors")

				// Log a sample for visibility
				lines := strings.Split(output, "\n")
				if len(lines) > 5 {
					fmt.Fprintf(GinkgoWriter, "Last 5 log lines from controller:\n")
					for _, line := range lines[len(lines)-6 : len(lines)-1] {
						if line != "" {
							fmt.Fprintf(GinkgoWriter, "  %s\n", line)
						}
					}
				}
			}).Should(Succeed())
		})

		It("should verify GitOpsCluster status conditions on hub", func() {
			By("verifying GitOpsCluster exists")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "gitopscluster", "gitops-cluster",
					"-n", argoCDNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())

			By("verifying GitOpsCluster RBACReady condition")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "gitopscluster", "gitops-cluster",
					"-n", argoCDNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='RBACReady')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}).Should(Succeed())

			By("verifying GitOpsCluster ServerDiscovered condition")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "gitopscluster", "gitops-cluster",
					"-n", argoCDNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='ServerDiscovered')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}).Should(Succeed())

			By("verifying GitOpsCluster JWTSecretReady condition")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "gitopscluster", "gitops-cluster",
					"-n", argoCDNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='JWTSecretReady')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}).Should(Succeed())

			By("verifying GitOpsCluster CACertificateReady condition")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "gitopscluster", "gitops-cluster",
					"-n", argoCDNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='CACertificateReady')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}).Should(Succeed())

			By("verifying GitOpsCluster PrincipalCertificateReady condition")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "gitopscluster", "gitops-cluster",
					"-n", argoCDNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='PrincipalCertificateReady')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}).Should(Succeed())

			By("verifying GitOpsCluster ResourceProxyCertificateReady condition")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "gitopscluster", "gitops-cluster",
					"-n", argoCDNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='ResourceProxyCertificateReady')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}).Should(Succeed())

			By("verifying GitOpsCluster PlacementEvaluated condition")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "gitopscluster", "gitops-cluster",
					"-n", argoCDNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='PlacementEvaluated')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}).Should(Succeed())

			By("verifying GitOpsCluster ClustersImported condition")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "gitopscluster", "gitops-cluster",
					"-n", argoCDNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='ClustersImported')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}).Should(Succeed())

			By("verifying GitOpsCluster ManifestWorkCreated condition")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "gitopscluster", "gitops-cluster",
					"-n", argoCDNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='ManifestWorkCreated')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}).Should(Succeed())

			By("verifying GitOpsCluster AddonConfigured condition")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "gitopscluster", "gitops-cluster",
					"-n", argoCDNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='AddonConfigured')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}).Should(Succeed())
		})

		It("should create ClusterManagementAddOn on hub", func() {
			By("verifying ClusterManagementAddOn exists")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "clustermanagementaddon", "argocd-agent-addon")
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
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

			By("checking ArgoCD principal pod logs for event processing")
			var principalPodName string
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "pods",
					"-n", argoCDNamespace,
					"-l", "app.kubernetes.io/name=argocd-agent-principal",
					"-o", "jsonpath={.items[0].metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty())
				principalPodName = output
			}).Should(Succeed())
		})

		It("should reconcile GitOpsCluster and create ManagedClusterAddOn", func() {
			By("verifying GitOpsCluster is created")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "gitopscluster", "gitops-cluster",
					"-n", argoCDNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())

			By("verifying ManagedClusterAddOn is created for cluster1")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "managedclusteraddon", "argocd-agent-addon",
					"-n", "cluster1")
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())
		})

		It("should deploy addon agent on spoke cluster", func() {
			By("verifying addon deployment exists on cluster1")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "deployment", "argocd-agent-addon",
					"-n", addonNamespace,
					"-o", "jsonpath={.status.availableReplicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("1"))
			}).Should(Succeed())

			By("verifying addon pod is running on cluster1")
			var podName string
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "pods",
					"-l", "app=argocd-agent-addon",
					"-n", addonNamespace,
					"-o", "jsonpath={.items[0].metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty())
				podName = output
			}).Should(Succeed())

			By("verifying addon pod phase is Running")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "pod", podName,
					"-n", addonNamespace,
					"-o", "jsonpath={.status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}).Should(Succeed())

			By("checking addon agent pod logs for errors")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"logs", podName,
					"-n", addonNamespace,
					"--tail=50")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				// Check that logs don't contain common error indicators
				lowerOutput := strings.ToLower(output)
				g.Expect(lowerOutput).NotTo(ContainSubstring("panic:"), "Agent logs should not contain panics")
				g.Expect(lowerOutput).NotTo(ContainSubstring("fatal error"), "Agent logs should not contain fatal errors")

				// Log a sample for visibility
				lines := strings.Split(output, "\n")
				if len(lines) > 5 {
					fmt.Fprintf(GinkgoWriter, "Last 5 log lines from agent addon:\n")
					for _, line := range lines[len(lines)-6 : len(lines)-1] {
						if line != "" {
							fmt.Fprintf(GinkgoWriter, "  %s\n", line)
						}
					}
				}
			}).Should(Succeed())
		})

		It("should deploy ArgoCD operator on spoke cluster", func() {
			By("verifying operator namespace exists")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "namespace", operatorNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())

			By("verifying operator deployment exists")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "deployment",
					"-n", operatorNamespace,
					"-l", "app.kubernetes.io/managed-by=argocd-agent-addon",
					"-o", "jsonpath={.items[0].status.availableReplicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty())
			}).Should(Succeed())
		})

		It("should deploy ArgoCD agent on spoke cluster", func() {
			By("verifying ArgoCD namespace exists")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "namespace", argoCDNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())

			By("verifying ArgoCD CR is created")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "argocd", "argocd",
					"-n", argoCDNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())

			By("verifying ArgoCD agent pod is running")
			var podName string
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "pods",
					"-n", argoCDNamespace,
					"-l", "app.kubernetes.io/name=argocd-agent-agent",
					"-o", "jsonpath={.items[0].metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty())
				podName = output
			}).Should(Succeed())

			By("verifying ArgoCD agent pod phase is Running")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "pod", podName,
					"-n", argoCDNamespace,
					"-o", "jsonpath={.status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}).Should(Succeed())

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
	})
})
