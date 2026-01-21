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

	Context("Application without finalizer deletion", func() {
		const (
			appNoFinalizerName = "test-app-nofinalizer"
		)
		var manifestWorkName string

		It("should create Application without finalizer on hub", func() {
			By("creating test Application without finalizer on hub")
			appYaml := fmt.Sprintf(`
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: %s
  labels:
    apps.open-cluster-management.io/pull-to-ocm-managed-cluster: "true"
  annotations:
    apps.open-cluster-management.io/ocm-managed-cluster: %s
    apps.open-cluster-management.io/ocm-managed-cluster-app-namespace: argocd
    argocd.argoproj.io/skip-reconcile: "true"
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
`, appNoFinalizerName, appNamespace, manifestWorkNs, targetNamespace)

			cmd := exec.Command("kubectl", "--context", hubContext, "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(appYaml)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying Application is created without finalizer")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "application", appNoFinalizerName,
					"-n", appNamespace,
					"-o", "jsonpath={.metadata.finalizers}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				// Empty or no finalizers expected
				g.Expect(output).To(Or(BeEmpty(), Equal("[]")))
			}).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "Application %s created without finalizer\n", appNoFinalizerName)
		})

		It("should create ManifestWork for Application without finalizer", func() {
			By("verifying ManifestWork is created with pattern <appname>-<uid-first-5-chars>")
			Eventually(func(g Gomega) {
				// Get the Application UID to construct expected ManifestWork name
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "application", appNoFinalizerName,
					"-n", appNamespace,
					"-o", "jsonpath={.metadata.uid}")
				uidOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(uidOutput).NotTo(BeEmpty())

				// ManifestWork name is appname + "-" + first 5 chars of UID
				expectedMWName := appNoFinalizerName + "-" + uidOutput[:5]

				cmd = exec.Command("kubectl", "--context", hubContext,
					"get", "manifestwork", expectedMWName,
					"-n", manifestWorkNs)
				_, err = utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				manifestWorkName = expectedMWName
			}).Should(Succeed())

			By("verifying ManifestWork has correct annotations for tracking")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "manifestwork", manifestWorkName,
					"-n", manifestWorkNs,
					"-o", "jsonpath={.metadata.annotations.apps\\.open-cluster-management\\.io/hub-application-namespace}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal(appNamespace))
			}).Should(Succeed())

			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "manifestwork", manifestWorkName,
					"-n", manifestWorkNs,
					"-o", "jsonpath={.metadata.annotations.apps\\.open-cluster-management\\.io/hub-application-name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal(appNoFinalizerName))
			}).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "ManifestWork %s created successfully with tracking annotations\n", manifestWorkName)
		})

		It("should pull Application to spoke cluster", func() {
			By("verifying Application is pulled to spoke cluster")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "application", appNoFinalizerName,
					"-n", appNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())

			By("verifying Application sync status on spoke")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "application", appNoFinalizerName,
					"-n", appNamespace,
					"-o", "jsonpath={.status.sync.status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Synced"))
			}).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "Application %s synced on spoke\n", appNoFinalizerName)
		})

		It("should delete Application without finalizer from hub", func() {
			By("deleting Application without finalizer from hub")
			cmd := exec.Command("kubectl", "--context", hubContext,
				"delete", "application", appNoFinalizerName,
				"-n", appNamespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying Application is immediately deleted (no finalizer)")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "application", appNoFinalizerName,
					"-n", appNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred())
			}).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "Application %s deleted from hub\n", appNoFinalizerName)
		})

		It("should cleanup ManifestWork even though Application had no finalizer", func() {
			By("verifying ManifestWork is deleted from hub")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "manifestwork", manifestWorkName,
					"-n", manifestWorkNs)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred())
			}).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "ManifestWork %s successfully cleaned up\n", manifestWorkName)
		})

		It("should remove Application from spoke after ManifestWork deletion", func() {
			By("verifying Application is removed from spoke cluster")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "application", appNoFinalizerName,
					"-n", appNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred())
			}).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "Application %s removed from spoke\n", appNoFinalizerName)
		})
	})

	Context("Application with custom destination annotations", func() {
		const (
			appCustomDestName = "test-app-custom-dest"
		)
		var manifestWorkName string

		It("should create Application with custom destination-name annotation", func() {
			By("creating test Application with custom destination-name annotation on hub")
			appYaml := fmt.Sprintf(`
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: %s
  labels:
    apps.open-cluster-management.io/pull-to-ocm-managed-cluster: "true"
  annotations:
    apps.open-cluster-management.io/ocm-managed-cluster: %s
    apps.open-cluster-management.io/ocm-managed-cluster-app-namespace: argocd
    apps.open-cluster-management.io/destination-name: in-cluster
    argocd.argoproj.io/skip-reconcile: "true"
  finalizers:
  - resources-finalizer.argocd.argoproj.io
spec:
  destination:
    namespace: %s
    server: https://some-external-server:6443
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
`, appCustomDestName, appNamespace, manifestWorkNs, targetNamespace)

			cmd := exec.Command("kubectl", "--context", hubContext, "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(appYaml)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying Application is created on hub")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "application", appCustomDestName,
					"-n", appNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "Application %s created with custom destination-name annotation\n", appCustomDestName)
		})

		It("should create ManifestWork with custom destination-name in payload", func() {
			By("verifying ManifestWork is created with pattern <appname>-<uid-first-5-chars>")
			Eventually(func(g Gomega) {
				// Get the Application UID to construct expected ManifestWork name
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "application", appCustomDestName,
					"-n", appNamespace,
					"-o", "jsonpath={.metadata.uid}")
				uidOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(uidOutput).NotTo(BeEmpty())

				// ManifestWork name is appname + "-" + first 5 chars of UID
				expectedMWName := appCustomDestName + "-" + uidOutput[:5]

				cmd = exec.Command("kubectl", "--context", hubContext,
					"get", "manifestwork", expectedMWName,
					"-n", manifestWorkNs)
				_, err = utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				manifestWorkName = expectedMWName
			}).Should(Succeed())

			By("verifying ManifestWork payload has custom destination.name set to 'in-cluster'")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "manifestwork", manifestWorkName,
					"-n", manifestWorkNs,
					"-o", "jsonpath={.spec.workload.manifests[0].spec.destination.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("in-cluster"))
			}).Should(Succeed())

			By("verifying ManifestWork payload has empty destination.server (since only name was specified)")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "manifestwork", manifestWorkName,
					"-n", manifestWorkNs,
					"-o", "jsonpath={.spec.workload.manifests[0].spec.destination.server}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(BeEmpty())
			}).Should(Succeed())

			By("verifying destination annotations are NOT copied to the payload")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "manifestwork", manifestWorkName,
					"-n", manifestWorkNs,
					"-o", "jsonpath={.spec.workload.manifests[0].metadata.annotations.apps\\.open-cluster-management\\.io/destination-name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(BeEmpty())
			}).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "ManifestWork %s has custom destination-name 'in-cluster' in payload\n", manifestWorkName)
		})

		It("should cleanup custom destination Application", func() {
			By("deleting Application with custom destination annotation")
			cmd := exec.Command("kubectl", "--context", hubContext,
				"delete", "application", appCustomDestName,
				"-n", appNamespace,
				"--timeout=60s")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying ManifestWork is deleted")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "manifestwork", manifestWorkName,
					"-n", manifestWorkNs)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred())
			}).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "Application %s and ManifestWork %s cleaned up\n", appCustomDestName, manifestWorkName)
		})
	})

	Context("Application with custom destination-server annotation", func() {
		const (
			appCustomServerName = "test-app-custom-server"
		)
		var manifestWorkName string

		It("should create Application with custom destination-server annotation", func() {
			By("creating test Application with custom destination-server annotation on hub")
			appYaml := fmt.Sprintf(`
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: %s
  labels:
    apps.open-cluster-management.io/pull-to-ocm-managed-cluster: "true"
  annotations:
    apps.open-cluster-management.io/ocm-managed-cluster: %s
    apps.open-cluster-management.io/ocm-managed-cluster-app-namespace: argocd
    apps.open-cluster-management.io/destination-server: https://kubernetes.default.svc
    argocd.argoproj.io/skip-reconcile: "true"
  finalizers:
  - resources-finalizer.argocd.argoproj.io
spec:
  destination:
    namespace: %s
    name: some-external-cluster
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
`, appCustomServerName, appNamespace, manifestWorkNs, targetNamespace)

			cmd := exec.Command("kubectl", "--context", hubContext, "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(appYaml)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying Application is created on hub")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "application", appCustomServerName,
					"-n", appNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "Application %s created with custom destination-server annotation\n", appCustomServerName)
		})

		It("should create ManifestWork with custom destination-server in payload", func() {
			By("verifying ManifestWork is created")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "application", appCustomServerName,
					"-n", appNamespace,
					"-o", "jsonpath={.metadata.uid}")
				uidOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(uidOutput).NotTo(BeEmpty())

				expectedMWName := appCustomServerName + "-" + uidOutput[:5]

				cmd = exec.Command("kubectl", "--context", hubContext,
					"get", "manifestwork", expectedMWName,
					"-n", manifestWorkNs)
				_, err = utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				manifestWorkName = expectedMWName
			}).Should(Succeed())

			By("verifying ManifestWork payload has custom destination.server")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "manifestwork", manifestWorkName,
					"-n", manifestWorkNs,
					"-o", "jsonpath={.spec.workload.manifests[0].spec.destination.server}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("https://kubernetes.default.svc"))
			}).Should(Succeed())

			By("verifying ManifestWork payload has empty destination.name (since only server was specified)")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "manifestwork", manifestWorkName,
					"-n", manifestWorkNs,
					"-o", "jsonpath={.spec.workload.manifests[0].spec.destination.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(BeEmpty())
			}).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "ManifestWork %s has custom destination-server in payload\n", manifestWorkName)
		})

		It("should pull Application to spoke and sync successfully", func() {
			By("verifying Application is pulled to spoke cluster")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "application", appCustomServerName,
					"-n", appNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())

			By("verifying Application sync status on spoke")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "application", appCustomServerName,
					"-n", appNamespace,
					"-o", "jsonpath={.status.sync.status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Synced"))
			}).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "Application %s synced on spoke with custom destination-server\n", appCustomServerName)
		})

		It("should cleanup custom destination-server Application", func() {
			By("deleting Application with custom destination-server annotation")
			cmd := exec.Command("kubectl", "--context", hubContext,
				"delete", "application", appCustomServerName,
				"-n", appNamespace,
				"--timeout=60s")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying ManifestWork is deleted")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "manifestwork", manifestWorkName,
					"-n", manifestWorkNs)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred())
			}).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "Application %s cleaned up\n", appCustomServerName)
		})
	})

	Context("ApplicationSet with preserveResourcesOnDeletion and periodic cleanup", func() {
		const (
			appSetPreserveName = "test-appset-preserve"
		)
		var (
			appFromAppSetName   string
			manifestWorkName    string
			cleanupIntervalSecs = 310 // Cleanup interval is 5 minutes (300s) + buffer for processing
		)

		It("should create ApplicationSet with preserveResourcesOnDeletion", func() {
			By("creating ApplicationSet with preserveResourcesOnDeletion on hub")
			appSetYaml := fmt.Sprintf(`
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: %s
  namespace: %s
spec:
  syncPolicy:
    preserveResourcesOnDeletion: true
  generators:
  - list:
      elements:
      - cluster: cluster1
        name: cluster1-preserve
  template:
    metadata:
      name: '{{name}}-app'
      labels:
        apps.open-cluster-management.io/pull-to-ocm-managed-cluster: "true"
      annotations:
        apps.open-cluster-management.io/ocm-managed-cluster: '{{cluster}}'
        apps.open-cluster-management.io/ocm-managed-cluster-app-namespace: argocd
        argocd.argoproj.io/skip-reconcile: "true"
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
`, appSetPreserveName, appNamespace, targetNamespace)

			cmd := exec.Command("kubectl", "--context", hubContext, "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(appSetYaml)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying ApplicationSet is created")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "applicationset", appSetPreserveName,
					"-n", appNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "ApplicationSet %s created with preserveResourcesOnDeletion\n", appSetPreserveName)
		})

		It("should create Application without finalizer from ApplicationSet", func() {
			By("waiting for ApplicationSet to create Application")
			time.Sleep(10 * time.Second)

			appFromAppSetName = "cluster1-preserve-app"

			By("verifying Application is created by ApplicationSet")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "application", appFromAppSetName,
					"-n", appNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("verifying Application has NO finalizer (preserveResourcesOnDeletion)")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "application", appFromAppSetName,
					"-n", appNamespace,
					"-o", "jsonpath={.metadata.finalizers}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				// Should be empty or [] since preserveResourcesOnDeletion prevents finalizer
				g.Expect(output).To(Or(BeEmpty(), Equal("[]")))
			}).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "Application %s created without finalizer by ApplicationSet\n", appFromAppSetName)
		})

		It("should create ManifestWork and propagate to spoke", func() {
			By("verifying ManifestWork is created")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "application", appFromAppSetName,
					"-n", appNamespace,
					"-o", "jsonpath={.metadata.uid}")
				uidOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(uidOutput).NotTo(BeEmpty())

				expectedMWName := appFromAppSetName + "-" + uidOutput[:5]

				cmd = exec.Command("kubectl", "--context", hubContext,
					"get", "manifestwork", expectedMWName,
					"-n", manifestWorkNs)
				_, err = utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				manifestWorkName = expectedMWName
			}).Should(Succeed())

			By("verifying Application is pulled to spoke cluster")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "application", appFromAppSetName,
					"-n", appNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())

			By("verifying Application sync status on spoke")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "application", appFromAppSetName,
					"-n", appNamespace,
					"-o", "jsonpath={.status.sync.status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Synced"))
			}).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "ManifestWork %s created and Application synced on spoke\n", manifestWorkName)
		})

		It("should cleanup orphaned ManifestWork via periodic controller after ApplicationSet deletion", func() {
			By("scaling down the controller to stop reconciliation")
			cmd := exec.Command("kubectl", "--context", hubContext,
				"-n", appNamespace,
				"scale", "deploy", "argocd-pull-integration",
				"--replicas=0")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for controller pod to be terminated")
			time.Sleep(5 * time.Second)

			By("deleting the ApplicationSet")
			cmd = exec.Command("kubectl", "--context", hubContext,
				"delete", "applicationset", appSetPreserveName,
				"-n", appNamespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying Application is deleted from hub (no finalizer)")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "application", appFromAppSetName,
					"-n", appNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred())
			}).Should(Succeed())

			By("verifying ManifestWork still exists (controller not running)")
			cmd = exec.Command("kubectl", "--context", hubContext,
				"get", "manifestwork", manifestWorkName,
				"-n", manifestWorkNs)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying Application is still on spoke")
			cmd = exec.Command("kubectl", "--context", cluster1Context,
				"get", "application", appFromAppSetName,
				"-n", appNamespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			fmt.Fprintf(GinkgoWriter, "ManifestWork %s is orphaned with controller stopped\n", manifestWorkName)

			By("scaling up the controller to resume reconciliation")
			cmd = exec.Command("kubectl", "--context", hubContext,
				"-n", appNamespace,
				"scale", "deploy", "argocd-pull-integration",
				"--replicas=1")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for controller pod to be ready")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"-n", appNamespace,
					"get", "deploy", "argocd-pull-integration",
					"-o", "jsonpath={.status.readyReplicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("1"))
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			By(fmt.Sprintf("waiting for periodic cleanup controller to delete orphaned ManifestWork (max %d seconds)", cleanupIntervalSecs+10))
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", hubContext,
					"get", "manifestwork", manifestWorkName,
					"-n", manifestWorkNs)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred())
			}, time.Duration(cleanupIntervalSecs+10)*time.Second, 5*time.Second).Should(Succeed())

			By("verifying Application is removed from spoke after ManifestWork deletion")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "--context", cluster1Context,
					"get", "application", appFromAppSetName,
					"-n", appNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred())
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			fmt.Fprintf(GinkgoWriter, "Periodic cleanup controller successfully deleted orphaned ManifestWork %s\n", manifestWorkName)
		})
	})
})
