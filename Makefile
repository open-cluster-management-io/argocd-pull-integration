# Image URL to use all building/pushing image targets
IMG ?= quay.io/open-cluster-management/argocd-pull-integration:latest

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./api/..." paths="./internal/controller/..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..." paths="./internal/controller/..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet setup-envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out

# E2E Test Configuration
# Hub and spoke cluster names for OCM-based e2e tests
HUB_CLUSTER ?= hub
SPOKE_CLUSTER ?= cluster1
E2E_IMG ?= quay.io/open-cluster-management/argocd-pull-integration:latest
# clusteradm version (see: https://github.com/open-cluster-management-io/clusteradm/releases)
CLUSTERADM_VERSION ?= v1.1.1

##@ E2E Tests

.PHONY: setup-e2e-clusters
setup-e2e-clusters: ## Setup KinD clusters, build and load controller image
	@echo "===== Cleaning up existing clusters ====="
	$(KIND) delete clusters --all || true
	@echo ""
	@echo "===== Creating KinD clusters ====="
	$(KIND) create cluster --name $(HUB_CLUSTER)
	$(KIND) create cluster --name $(SPOKE_CLUSTER)
	@echo ""
	@echo "===== Building controller image ====="
	$(MAKE) docker-build IMG=$(E2E_IMG)
	@echo ""
	@echo "===== Loading image to clusters ====="
	$(KIND) load docker-image $(E2E_IMG) --name $(HUB_CLUSTER)
	$(KIND) load docker-image $(E2E_IMG) --name $(SPOKE_CLUSTER)

.PHONY: test-e2e
test-e2e: manifests generate fmt vet ## Run e2e deployment tests only (checks pods running and logs)
	@echo "===== Installing MetalLB ====="
	./test/e2e/scripts/install_metallb.sh
	@echo ""
	@echo "===== Setting up OCM environment ====="
	CLUSTERADM_VERSION=$(CLUSTERADM_VERSION) ./test/e2e/scripts/setup_ocm_env.sh
	@echo ""
	@echo "===== Installing addon via Helm ====="
	$(KUBECTL) config use-context kind-$(HUB_CLUSTER)
	helm install argocd-agent-addon \
		./charts/argocd-agent-addon \
		--namespace argocd \
		--create-namespace \
		--set image=quay.io/open-cluster-management/argocd-pull-integration \
		--set tag=latest \
		--wait \
		--timeout 10m
	@echo ""
	@echo "===== Running e2e deployment tests ====="
	go test -tags=e2e ./test/e2e/ -v -ginkgo.v --ginkgo.label-filter="deploy"
	@echo ""
	@echo "===== E2E Deployment Tests Complete ====="
	@echo "Hub context: kind-$(HUB_CLUSTER)"
	@echo "Spoke context: kind-$(SPOKE_CLUSTER)"
	@echo ""
	@echo "Verify deployment with:"
	@echo "  # Hub cluster resources"
	@echo "  kubectl get pods -n argocd --context kind-$(HUB_CLUSTER)"
	@echo "  kubectl logs -n argocd -l app.kubernetes.io/name=argocd-pull-integration-controller --context kind-$(HUB_CLUSTER) --tail=20"
	@echo ""
	@echo "  # Spoke cluster resources"
	@echo "  kubectl get pods -n open-cluster-management-agent-addon --context kind-$(SPOKE_CLUSTER)"
	@echo "  kubectl logs -n open-cluster-management-agent-addon -l app=argocd-agent-addon --context kind-$(SPOKE_CLUSTER) --tail=20"
	@echo "  kubectl get pods -n argocd --context kind-$(SPOKE_CLUSTER)"
	@echo "  kubectl logs -n argocd -l app.kubernetes.io/name=argocd-agent-agent --context kind-$(SPOKE_CLUSTER) --tail=20"

.PHONY: test-e2e-integration
test-e2e-integration: ## Run full e2e integration tests including AppProject and Application sync (assumes setup already done)
	@echo "===== Running full e2e integration tests ====="
	go test -tags=e2e ./test/e2e/ -v -ginkgo.v --ginkgo.label-filter="full"
	@echo ""
	@echo "===== Full E2E Integration Tests Complete ====="

.PHONY: test-e2e-full
test-e2e-full: ## Complete e2e test with kind cluster setup, build, deployment, and full integration tests
	$(MAKE) setup-e2e-clusters
	@echo ""
	@echo "===== Running deployment tests ====="
	$(MAKE) test-e2e
	@echo ""
	@echo "===== Running full integration tests ====="
	$(MAKE) test-e2e-integration
	@echo "Verify deployment with:"
	@echo "  # Hub cluster resources"
	@echo "  kubectl get pods -n argocd --context kind-$(HUB_CLUSTER)"
	@echo "  sleep 2"
	@echo "  kubectl get clustermanagementaddon --context kind-$(HUB_CLUSTER)"
	@echo "  sleep 2"
	@echo "  kubectl get gitopscluster -n argocd --context kind-$(HUB_CLUSTER)"
	@echo "  sleep 2"
	@echo "  kubectl get managedclusteraddon -n $(SPOKE_CLUSTER) --context kind-$(HUB_CLUSTER)"
	@echo "  sleep 2"
	@echo "  kubectl get appproject -n argocd --context kind-$(HUB_CLUSTER)"
	@echo "  sleep 2"
	@echo "  kubectl get application -n $(SPOKE_CLUSTER) --context kind-$(HUB_CLUSTER)"
	@echo "  sleep 2"
	@echo ""
	@echo "  # Spoke cluster resources"
	@echo "  kubectl get pods -n open-cluster-management-agent-addon --context kind-$(SPOKE_CLUSTER)"
	@echo "  sleep 2"
	@echo "  kubectl get pods -n argocd --context kind-$(SPOKE_CLUSTER)"
	@echo "  sleep 2"
	@echo "  kubectl get appproject -n argocd --context kind-$(SPOKE_CLUSTER)"
	@echo "  sleep 2"
	@echo "  kubectl get application -n argocd --context kind-$(SPOKE_CLUSTER)"
	@echo "  sleep 2"

.PHONY: test-e2e-cleanup
test-e2e-cleanup: manifests generate fmt vet ## Run e2e cleanup tests (checks addon cleanup behavior)
	@echo "===== Installing MetalLB ====="
	./test/e2e/scripts/install_metallb.sh
	@echo ""
	@echo "===== Setting up OCM environment ====="
	CLUSTERADM_VERSION=$(CLUSTERADM_VERSION) ./test/e2e/scripts/setup_ocm_env.sh
	@echo ""
	@echo "===== Installing addon via Helm ====="
	$(KUBECTL) config use-context kind-$(HUB_CLUSTER)
	helm install argocd-agent-addon \
		./charts/argocd-agent-addon \
		--namespace argocd \
		--create-namespace \
		--set image=quay.io/open-cluster-management/argocd-pull-integration \
		--set tag=latest \
		--wait \
		--timeout 10m
	@echo ""
	@echo "===== Running cleanup e2e tests ====="
	go test -tags=e2e ./test/e2e/ -v -ginkgo.v --ginkgo.label-filter="cleanup"
	@echo ""
	@echo "===== E2E Cleanup Tests Complete ====="

.PHONY: test-e2e-cleanup-full
test-e2e-cleanup-full: ## Complete e2e test with cleanup verification including Application (cluster setup + deployment + cleanup)
	$(MAKE) setup-e2e-clusters
	@echo ""
	@echo "===== Installing MetalLB ====="
	./test/e2e/scripts/install_metallb.sh
	@echo ""
	@echo "===== Setting up OCM environment ====="
	CLUSTERADM_VERSION=$(CLUSTERADM_VERSION) ./test/e2e/scripts/setup_ocm_env.sh
	@echo ""
	@echo "===== Installing addon via Helm ====="
	$(KUBECTL) config use-context kind-$(HUB_CLUSTER)
	helm install argocd-agent-addon \
		./charts/argocd-agent-addon \
		--namespace argocd \
		--create-namespace \
		--set image=quay.io/open-cluster-management/argocd-pull-integration \
		--set tag=latest \
		--wait \
		--timeout 10m
	@echo ""
	@echo "===== Running full cleanup e2e tests (with Application) ====="
	go test -tags=e2e ./test/e2e/ -v -ginkgo.v --ginkgo.label-filter="cleanup-full"
	@echo ""
	@echo "===== E2E Cleanup Full Tests Complete ====="
	@echo "Hub context: kind-$(HUB_CLUSTER)"
	@echo "Spoke context: kind-$(SPOKE_CLUSTER)"

.PHONY: test-e2e-custom-namespace
test-e2e-custom-namespace: manifests generate fmt vet ## Run e2e test with custom ArgoCD namespaces (hub: notargocd, spoke: argocdnot)
	@echo "===== Installing MetalLB ====="
	./test/e2e/scripts/install_metallb.sh
	@echo ""
	@echo "===== Setting up OCM environment ====="
	CLUSTERADM_VERSION=$(CLUSTERADM_VERSION) ./test/e2e/scripts/setup_ocm_env.sh
	@echo ""
	@echo "===== Installing addon via Helm with custom namespaces ====="
	$(KUBECTL) config use-context kind-$(HUB_CLUSTER)
	helm install argocd-agent-addon \
		./charts/argocd-agent-addon \
		--namespace notargocd \
		--create-namespace \
		--set global.argoCDNamespace=notargocd \
		--set global.argoCDOperatorNamespace=notargocd-operator-system \
		--set controller.namespace=notargocd \
		--set gitOpsCluster.namespace=notargocd \
		--set gitOpsCluster.argoCDAgentAddon.agentNamespace=argocdnot \
		--set gitOpsCluster.argoCDAgentAddon.operatorNamespace=argocdnot-operator-system \
		--set image=quay.io/open-cluster-management/argocd-pull-integration \
		--set tag=latest \
		--wait \
		--timeout 10m
	@echo ""
	@echo "===== Running e2e tests with custom namespaces ====="
	HUB_ARGOCD_NAMESPACE=notargocd \
	HUB_ARGOCD_OPERATOR_NAMESPACE=notargocd-operator-system \
	SPOKE_ARGOCD_NAMESPACE=argocdnot \
	SPOKE_ARGOCD_OPERATOR_NAMESPACE=argocdnot-operator-system \
	go test -tags=e2e ./test/e2e/ -v -ginkgo.v --ginkgo.label-filter="custom-namespace"
	@echo ""
	@echo "===== E2E Custom Namespace Tests Complete ====="
	@echo "Hub context: kind-$(HUB_CLUSTER) (ArgoCD: notargocd, Operator: notargocd-operator-system)"
	@echo "Spoke context: kind-$(SPOKE_CLUSTER) (ArgoCD: argocdnot, Operator: argocdnot-operator-system)"

.PHONY: test-e2e-custom-namespace-full
test-e2e-custom-namespace-full: ## Complete e2e test with custom namespaces (cluster setup + custom namespace deployment + tests)
	$(MAKE) setup-e2e-clusters
	@echo ""
	$(MAKE) test-e2e-custom-namespace

.PHONY: test-e2e-basic
test-e2e-basic: manifests generate fmt vet ## Run e2e tests for basic pull model (assumes clusters exist and images loaded)
	@echo "===== Setting up OCM environment ====="
	CLUSTERADM_VERSION=$(CLUSTERADM_VERSION) ./test/e2e/scripts/setup_ocm_env.sh
	@echo ""
	@echo "===== Installing ArgoCD on hub cluster (optimized) ====="
	$(KUBECTL) config use-context kind-$(HUB_CLUSTER)
	$(KUBECTL) create namespace argocd --dry-run=client -o yaml | $(KUBECTL) apply -f -
	$(KUBECTL) apply -n argocd --server-side --force-conflicts -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
	sleep 30
	$(KUBECTL) -n argocd scale deployment/argocd-dex-server --replicas 0
	$(KUBECTL) -n argocd scale deployment/argocd-repo-server --replicas 0
	$(KUBECTL) -n argocd scale deployment/argocd-server --replicas 0
	$(KUBECTL) -n argocd scale deployment/argocd-redis --replicas 0
	$(KUBECTL) -n argocd scale deployment/argocd-notifications-controller --replicas 0
	$(KUBECTL) -n argocd scale statefulset/argocd-application-controller --replicas 0
	$(KUBECTL) -n argocd patch configmap argocd-cmd-params-cm --type merge -p '{"data":{"applicationsetcontroller.enable.progressive.syncs":"true"}}'
	$(KUBECTL) -n argocd rollout restart deployment argocd-applicationset-controller
	$(KUBECTL) -n argocd rollout status deployment argocd-applicationset-controller --timeout=60s
	@echo ""
	@echo "===== Installing basic pull controller via Helm ====="
	$(KUBECTL) config use-context kind-$(HUB_CLUSTER)
	helm install argocd-pull-integration \
		./charts/argocd-pull-integration \
		--namespace argocd \
		--wait \
		--timeout 10m
	@echo ""
	$(KUBECTL) apply -f hack/e2e/mca.yaml --context kind-$(HUB_CLUSTER)
	$(KUBECTL) apply -f hack/e2e/appproj.yaml --context kind-$(HUB_CLUSTER)
	@echo ""
	@echo "===== Running e2e basic tests ====="
	$(KUBECTL) config use-context kind-$(HUB_CLUSTER)
	go test -tags=e2e ./test/e2e/ -v -ginkgo.v --ginkgo.label-filter="basic-full"
	@echo ""
	@echo "===== E2E Basic Tests Complete ====="
	@echo "Hub context: kind-$(HUB_CLUSTER)"
	@echo "Spoke context: kind-$(SPOKE_CLUSTER)"

.PHONY: test-e2e-basic-full
test-e2e-basic-full: ## Complete e2e test with basic pull model (cluster setup + deployment + tests)
	$(MAKE) setup-e2e-clusters
	@echo ""
	@echo "===== Running basic tests ====="
	$(MAKE) test-e2e-basic

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${IMG} .

.PHONY: build-images
build-images: docker-build ## Alias for docker-build to support CI workflows.

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

# PLATFORMS defines the target platforms for the manager image be built to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have enabled BuildKit. More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image to your registry (i.e. if you do not set a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To adequately provide solutions that are compatible with multiple platforms, you should consider using this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- $(CONTAINER_TOOL) buildx create --name argocd-pull-integration-builder
	$(CONTAINER_TOOL) buildx use argocd-pull-integration-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm argocd-pull-integration-builder
	rm Dockerfile.cross

# NOTE: build-installer disabled - config/default not used since we deploy via Helm
# .PHONY: build-installer
# build-installer: manifests generate kustomize ## Generate a consolidated YAML with CRDs and deployment.
# 	mkdir -p dist
# 	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
# 	$(KUSTOMIZE) build config/default > dist/install.yaml

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	@out="$$( $(KUSTOMIZE) build config/crd 2>/dev/null || true )"; \
	if [ -n "$$out" ]; then echo "$$out" | $(KUBECTL) apply -f -; else echo "No CRDs to install; skipping."; fi

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	@out="$$( $(KUSTOMIZE) build config/crd 2>/dev/null || true )"; \
	if [ -n "$$out" ]; then echo "$$out" | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -; else echo "No CRDs to delete; skipping."; fi

# NOTE: deploy/undeploy disabled - config/default not used since we deploy via Helm
# Use 'make setup-addon-env' for Helm-based deployment instead
# .PHONY: deploy
# deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
# 	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
# 	$(KUSTOMIZE) build config/default | $(KUBECTL) apply -f -

# .PHONY: undeploy
# undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
# 	$(KUSTOMIZE) build config/default | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KIND ?= kind
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest

## Tool Versions
KUSTOMIZE_VERSION ?= v5.7.1
CONTROLLER_TOOLS_VERSION ?= v0.19.0
#ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script (i.e. release-0.20)
ENVTEST_VERSION ?= $(shell go list -m -f "{{ .Version }}" sigs.k8s.io/controller-runtime | awk -F'[v.]' '{printf "release-%d.%d", $$2, $$3}')
#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= $(shell go list -m -f "{{ .Version }}" k8s.io/api | awk -F'[v.]' '{printf "1.%d", $$3}')

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: setup-envtest
setup-envtest: envtest ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@$(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] && [ "$$(readlink -- "$(1)" 2>/dev/null)" = "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $$(realpath $(1)-$(3)) $(1)
endef
