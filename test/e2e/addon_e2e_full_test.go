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

var _ = Describe("ArgoCD Agent Addon Full E2E", Label("full"), Ordered, func() {
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
			var agentPodName string
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "pods",
					"-n", argoCDNamespace,
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

	Context("ArgoCD Agent Sync Verification", func() {
		It("should sync AppProject from hub to spoke", func() {
			By("creating AppProject on hub argocd namespace")
			appProjectYAML := `apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: default
  namespace: argocd
spec:
  clusterResourceWhitelist:
  - group: '*'
    kind: '*'
  destinations:
  - namespace: '*'
    server: '*'
  sourceRepos:
  - '*'
  sourceNamespaces:
  - '*'`
			cmd := exec.Command("kubectl", "--context", hubContext, "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(appProjectYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying AppProject synced to spoke cluster argocd namespace")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "appproject", "default",
					"-n", argoCDNamespace,
					"-o", "jsonpath={.metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("default"))
			}, 30*time.Second).Should(Succeed())

			By("verifying AppProject spec on spoke")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "appproject", "default",
					"-n", argoCDNamespace,
					"-o", "jsonpath={.spec.sourceRepos[0]}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("*"))
			}).Should(Succeed())
		})

		It("should sync Application from hub to spoke and reflect status", func() {
			By("creating cluster1 namespace on hub")
			cmd := exec.Command("kubectl", "--context", hubContext, "create", "namespace", "cluster1")
			_, _ = utils.Run(cmd) // Ignore error if namespace already exists

			By("creating Application on hub in cluster1 namespace")
			applicationYAML := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: test-app
  namespace: cluster1
spec:
  project: default
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps
    targetRevision: HEAD
    path: guestbook
  destination:
    server: https://172.18.255.200:443?agentName=cluster1
    namespace: guestbook
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
    - CreateNamespace=true`
			appCmd := exec.Command("kubectl", "--context", hubContext, "apply", "-f", "-")
			appCmd.Stdin = strings.NewReader(applicationYAML)
			_, err := utils.Run(appCmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying Application synced to spoke cluster argocd namespace")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "application", "test-app",
					"-n", argoCDNamespace,
					"-o", "jsonpath={.metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("test-app"))
			}, 30*time.Second, 5*time.Second).Should(Succeed())

			By("waiting for Application status to be populated on spoke")
			time.Sleep(10 * time.Second)

			By("verifying Application status on spoke is not empty")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "application", "test-app",
					"-n", argoCDNamespace,
					"-o", "jsonpath={.status.sync.status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "Spoke Application sync status should not be empty")
			}, 20*time.Second).Should(Succeed())

			By("getting Application status from spoke")
			var spokeSyncStatus, spokeHealthStatus string
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "application", "test-app",
					"-n", argoCDNamespace,
					"-o", "jsonpath={.status.sync.status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				spokeSyncStatus = output

				cmd = exec.Command("kubectl", "--context", cluster1Context,
					"get", "application", "test-app",
					"-n", argoCDNamespace,
					"-o", "jsonpath={.status.health.status}")
				output, err = utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				spokeHealthStatus = output
			}).Should(Succeed())

			By(fmt.Sprintf("Spoke status - Sync: %s, Health: %s", spokeSyncStatus, spokeHealthStatus))

			By("verifying Application status on hub matches spoke")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "application", "test-app",
					"-n", "cluster1",
					"-o", "jsonpath={.status.sync.status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "Hub Application sync status should not be empty")
				g.Expect(output).To(Equal(spokeSyncStatus), "Hub and Spoke sync status should match")
			}, 30*time.Second).Should(Succeed())

			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "application", "test-app",
					"-n", "cluster1",
					"-o", "jsonpath={.status.health.status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "Hub Application health status should not be empty")
				g.Expect(output).To(Equal(spokeHealthStatus), "Hub and Spoke health status should match")
			}, 30*time.Second).Should(Succeed())

			By("Hub and Spoke Application status are in sync ✓")
		})
	})
})
