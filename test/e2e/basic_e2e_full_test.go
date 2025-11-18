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

var _ = Describe("ArgoCD Basic Pull Model Full Integration E2E", Label("basic-full"), Ordered, func() {
	SetDefaultEventuallyTimeout(10 * time.Minute)
	SetDefaultEventuallyPollingInterval(5 * time.Second)

	const (
		appSetName      = "test-appset"
		appNamespace    = "argocd"
		manifestWorkNs  = "cluster1"
		targetNamespace = "guestbook"
	)

	BeforeAll(func() {
		By("Verifying test environment is ready")
		cmd := exec.Command("kubectl", "config", "get-contexts", hubContext)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Hub context should exist")

		cmd = exec.Command("kubectl", "config", "get-contexts", cluster1Context)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Spoke context should exist")

		By("Verifying ManagedClusterAddon is ready")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "--context", hubContext,
				"get", "managedclusteraddon", "argocd",
				"-n", manifestWorkNs,
				"-o", "jsonpath={.status.conditions[?(@.type=='Available')].status}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("True"))
		}).Should(Succeed())

		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "--context", hubContext,
				"get", "managedclusteraddon", "argocd",
				"-n", manifestWorkNs,
				"-o", "jsonpath={.status.conditions[?(@.type=='Progressing')].status}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("False"))
		}).Should(Succeed())

		fmt.Fprintf(GinkgoWriter, "ManagedClusterAddon argocd is ready\n")
	})

	AfterAll(func() {
		// No cleanup - leave resources for debugging
		fmt.Fprintf(GinkgoWriter, "\n")
		fmt.Fprintf(GinkgoWriter, "Full integration test complete - resources left for debugging\n")
	})

	Context("Application Sync via ApplicationSet and ManifestWork", func() {
		var appName string

		It("should create ApplicationSet", func() {
			By("creating test ApplicationSet on hub")
			appSetYaml := fmt.Sprintf(`
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: %s
  namespace: %s
spec:
  generators:
  - clusterDecisionResource:
      configMapRef: ocm-placement-generator
      labelSelector:
        matchLabels:
          cluster.open-cluster-management.io/placement: app-placement
      requeueAfterSeconds: 30
  template:
    metadata:
      annotations:
        apps.open-cluster-management.io/ocm-managed-cluster: '{{name}}'
        apps.open-cluster-management.io/ocm-managed-cluster-app-namespace: argocd
        argocd.argoproj.io/skip-reconcile: "true"
      labels:
        apps.open-cluster-management.io/pull-to-ocm-managed-cluster: "true"
      name: '{{name}}-app'
    spec:
      destination:
        namespace: %s
        server: https://kubernetes.default.svc
      project: default
      source:
        path: guestbook
        repoURL: https://github.com/argoproj/argocd-example-apps.git
        targetRevision: HEAD
      syncPolicy:
        automated:
          prune: true
        syncOptions:
        - CreateNamespace=true
`, appSetName, appNamespace, targetNamespace)

			cmd := exec.Command("kubectl", "--context", hubContext, "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(appSetYaml)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying ApplicationSet is created on hub")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "applicationset", appSetName,
					"-n", appNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())

			By("verifying ApplicationSet has no errors")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "applicationset", appSetName,
					"-n", appNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='ErrorOccurred')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("False"))
			}).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "ApplicationSet %s created successfully\n", appSetName)
		})

		It("should create Application from ApplicationSet", func() {
			By("verifying Application is created by ApplicationSet")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "application",
					"-n", appNamespace,
					"-o", "jsonpath={.items[*].metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty())
				appName = strings.TrimSpace(output)
			}).Should(Succeed())

			By("verifying Application has correct labels and annotations")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "application", appName,
					"-n", appNamespace,
					"-o", "jsonpath={.metadata.labels.apps\\.open-cluster-management\\.io/pull-to-ocm-managed-cluster}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("true"))
			}).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "Application %s created by ApplicationSet\n", appName)
		})

		It("should create ManifestWork for Application", func() {
			By("verifying ManifestWork is created with pattern <appname>-<uid-first-5-chars>")
			var manifestWorkName string
			Eventually(func(g Gomega) {
				// Get the Application UID to construct expected ManifestWork name
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "application", appName,
					"-n", appNamespace,
					"-o", "jsonpath={.metadata.uid}")
				uidOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(uidOutput).NotTo(BeEmpty())

				// ManifestWork name is appname + "-" + first 5 chars of UID
				expectedMWName := appName + "-" + uidOutput[:5]

				cmd = exec.Command("kubectl", "--context", hubContext,
					"get", "manifestwork", expectedMWName,
					"-n", manifestWorkNs)
				_, err = utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				manifestWorkName = expectedMWName
			}).Should(Succeed())

			By("verifying ManifestWork contains Application payload")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "manifestwork", manifestWorkName,
					"-n", manifestWorkNs,
					"-o", "jsonpath={.spec.workload.manifests[0].kind}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Application"))
			}).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "ManifestWork %s created successfully\n", manifestWorkName)
		})

		It("should pull Application to spoke cluster", func() {
			By("verifying Application is pulled to spoke cluster")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "application", appName,
					"-n", appNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())

			By("verifying Application sync status on spoke")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "application", appName,
					"-n", appNamespace,
					"-o", "jsonpath={.status.sync.status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Synced"))
			}).Should(Succeed())

			By("verifying Application health status on spoke")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "application", appName,
					"-n", appNamespace,
					"-o", "jsonpath={.status.health.status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Healthy"))
			}).Should(Succeed())
		})

		It("should deploy application resources on spoke", func() {
			By("verifying guestbook namespace is created")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "namespace", targetNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())

			By("verifying guestbook deployment is created")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "deployment", "guestbook-ui",
					"-n", targetNamespace,
					"-o", "jsonpath={.status.availableReplicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("1"))
			}).Should(Succeed())
		})

		It("should sync Application status back to hub", func() {
			By("verifying ManifestWork status is updated")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "manifestwork",
					"-n", manifestWorkNs,
					"-o", "jsonpath={.items[0].status.conditions[?(@.type=='Applied')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}).Should(Succeed())

			By("verifying Application sync status on hub is updated")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "application", appName,
					"-n", appNamespace,
					"-o", "jsonpath={.status.sync.status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Synced"))
			}).Should(Succeed())

			By("verifying Application health status on hub is updated")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "application", appName,
					"-n", appNamespace,
					"-o", "jsonpath={.status.health.status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Healthy"))
			}).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "Application status successfully synced back to hub\n")
		})
	})
})
