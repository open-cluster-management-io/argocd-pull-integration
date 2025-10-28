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
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// TestE2E is the entry point for the e2e test suite
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting ArgoCD Pull Integration E2E test suite\n")
	RunSpecs(t, "ArgoCD Agent Addon E2E Suite")
}

const (
	hubContext        = "kind-hub"
	cluster1Context   = "kind-cluster1"
	addonNamespace    = "open-cluster-management-agent-addon"
	argoCDNamespace   = "argocd"
	operatorNamespace = "argocd-operator-system"
)
