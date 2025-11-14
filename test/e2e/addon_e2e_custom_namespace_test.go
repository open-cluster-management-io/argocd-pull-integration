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
	"os"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/argocd-pull-integration/test/utils"
)

var _ = Describe("ArgoCD Agent Addon Custom Namespace E2E", Label("custom-namespace"), Ordered, func() {
	SetDefaultEventuallyTimeout(5 * time.Minute)
	SetDefaultEventuallyPollingInterval(5 * time.Second)

	var (
		hubArgoCDNamespace           string
		hubArgoCDOperatorNamespace   string
		spokeArgoCDNamespace         string
		spokeArgoCDOperatorNamespace string
	)

	BeforeAll(func() {
		By("Getting custom namespace configuration from environment")
		hubArgoCDNamespace = os.Getenv("HUB_ARGOCD_NAMESPACE")
		if hubArgoCDNamespace == "" {
			hubArgoCDNamespace = "notargocd"
		}
		hubArgoCDOperatorNamespace = os.Getenv("HUB_ARGOCD_OPERATOR_NAMESPACE")
		if hubArgoCDOperatorNamespace == "" {
			hubArgoCDOperatorNamespace = "notargocd-operator-system"
		}
		spokeArgoCDNamespace = os.Getenv("SPOKE_ARGOCD_NAMESPACE")
		if spokeArgoCDNamespace == "" {
			spokeArgoCDNamespace = "argocdnot"
		}
		spokeArgoCDOperatorNamespace = os.Getenv("SPOKE_ARGOCD_OPERATOR_NAMESPACE")
		if spokeArgoCDOperatorNamespace == "" {
			spokeArgoCDOperatorNamespace = "argocdnot-operator-system"
		}

		fmt.Fprintf(GinkgoWriter, "Using custom namespaces:\n")
		fmt.Fprintf(GinkgoWriter, "  Hub ArgoCD: %s\n", hubArgoCDNamespace)
		fmt.Fprintf(GinkgoWriter, "  Hub Operator: %s\n", hubArgoCDOperatorNamespace)
		fmt.Fprintf(GinkgoWriter, "  Spoke ArgoCD: %s\n", spokeArgoCDNamespace)
		fmt.Fprintf(GinkgoWriter, "  Spoke Operator: %s\n", spokeArgoCDOperatorNamespace)

		By("Verifying test environment is ready")
		// Environment setup is done by Makefile
		cmd := exec.Command("kubectl", "config", "get-contexts", hubContext)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Hub context should exist")

		cmd = exec.Command("kubectl", "config", "get-contexts", cluster1Context)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Spoke context should exist")
	})

	AfterAll(func() {
		By("Test complete - clusters preserved for inspection")
		fmt.Fprintf(GinkgoWriter, "\n")
		fmt.Fprintf(GinkgoWriter, "Clusters have been preserved for inspection:\n")
		fmt.Fprintf(GinkgoWriter, "  Hub: kubectl config use-context kind-hub\n")
		fmt.Fprintf(GinkgoWriter, "    ArgoCD: %s, Operator: %s\n", hubArgoCDNamespace, hubArgoCDOperatorNamespace)
		fmt.Fprintf(GinkgoWriter, "  Spoke: kubectl config use-context kind-cluster1\n")
		fmt.Fprintf(GinkgoWriter, "    ArgoCD: %s, Operator: %s\n", spokeArgoCDNamespace, spokeArgoCDOperatorNamespace)
		fmt.Fprintf(GinkgoWriter, "\n")
	})

	Context("Custom Namespace Deployment", func() {
		It("should deploy the GitOpsCluster controller on hub in custom namespace", func() {
			By("verifying controller deployment exists on hub")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "deployment", "argocd-pull-integration-controller",
					"-n", hubArgoCDNamespace,
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
					"-n", hubArgoCDNamespace,
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}).Should(Succeed())
		})

		It("should verify GitOpsCluster status conditions on hub", func() {
			By("verifying GitOpsCluster exists")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "gitopscluster", "gitops-cluster",
					"-n", hubArgoCDNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())

			By("verifying GitOpsCluster RBACReady condition")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "gitopscluster", "gitops-cluster",
					"-n", hubArgoCDNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='RBACReady')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}).Should(Succeed())

			By("verifying GitOpsCluster ServerDiscovered condition")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "gitopscluster", "gitops-cluster",
					"-n", hubArgoCDNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='ServerDiscovered')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}).Should(Succeed())
		})

		It("should verify ArgoCD operator running on hub in custom operator namespace", func() {
			By("verifying ArgoCD operator pod is running")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "pods",
					"-n", hubArgoCDOperatorNamespace,
					"-l", "control-plane=argocd-operator",
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}).Should(Succeed())
		})

		It("should verify all ArgoCD pods running on hub in custom namespace", func() {
			By("verifying ArgoCD principal pod is running")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "pods",
					"-n", hubArgoCDNamespace,
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
					"-n", hubArgoCDNamespace,
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
					"-n", hubArgoCDNamespace,
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
					"-n", hubArgoCDNamespace,
					"-l", "app.kubernetes.io/name=argocd-server",
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
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
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "pods",
					"-l", "app=argocd-agent-addon",
					"-n", addonNamespace,
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}).Should(Succeed())
		})

		It("should verify ArgoCD operator running on spoke in custom operator namespace", func() {
			By("verifying ArgoCD operator namespace exists")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "namespace", spokeArgoCDOperatorNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())

			By("verifying ArgoCD operator pod is running")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "pods",
					"-n", spokeArgoCDOperatorNamespace,
					"-l", "control-plane=argocd-operator",
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}).Should(Succeed())
		})

		It("should deploy ArgoCD agent on spoke cluster in custom namespace", func() {
			By("verifying ArgoCD namespace exists")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "namespace", spokeArgoCDNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())

			By("verifying ArgoCD CR is created in custom namespace")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "argocd", "argocd",
					"-n", spokeArgoCDNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())

			By("verifying ArgoCD agent pod is running in custom namespace")
			var agentPodName string
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "pods",
					"-n", spokeArgoCDNamespace,
					"-l", "app.kubernetes.io/name=argocd-agent-agent",
					"-o", "jsonpath={.items[0].metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty())
				agentPodName = output
			}).Should(Succeed())

			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "pod", agentPodName,
					"-n", spokeArgoCDNamespace,
					"-o", "jsonpath={.status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}).Should(Succeed())

			By("verifying ArgoCD application-controller pod is running in custom namespace")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "pods",
					"-n", spokeArgoCDNamespace,
					"-l", "app.kubernetes.io/name=argocd-application-controller",
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}).Should(Succeed())

			By("verifying ArgoCD redis pod is running on cluster1 in custom namespace")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "pods",
					"-n", spokeArgoCDNamespace,
					"-l", "app.kubernetes.io/name=argocd-redis",
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}).Should(Succeed())

			By("verifying ArgoCD repo-server pod is running on cluster1 in custom namespace")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "pods",
					"-n", spokeArgoCDNamespace,
					"-l", "app.kubernetes.io/name=argocd-repo-server",
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}).Should(Succeed())

			By("verifying CA secret exists in custom namespace")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "secret", "argocd-agent-ca",
					"-n", spokeArgoCDNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())
		})
	})
})
