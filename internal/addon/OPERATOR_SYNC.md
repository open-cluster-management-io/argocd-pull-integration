# Syncing argocd-operator Manifests

This document describes how to update the embedded argocd-operator manifests in this
repo when the upstream [argocd-operator](https://github.com/argoproj-labs/argocd-operator)
releases a new version.

## What This Repo Embeds

The argocd-operator is deployed on both hub and spoke clusters. This repo carries
a copy of its manifests in two locations (which must stay in sync):

| Location | Purpose |
|----------|---------|
| `charts/argocd-agent-addon/crds/argocd-operator-crds.yaml` | CRDs installed on the hub via Helm |
| `charts/argocd-agent-addon/templates/argocd-operator/operator.yaml` | Operator deployment manifests (hub) |
| `charts/argocd-agent-addon/templates/argocd-operator/argocd.yaml` | Hub ArgoCD CR (principal config) |
| `internal/addon/charts/argocd-agent-addon/crds/argocd-operator-crds.yaml` | CRDs installed on spokes via addon |
| `internal/addon/charts/argocd-agent-addon/templates/operator.yaml` | Operator deployment manifests (spoke) |
| `internal/addon/charts/argocd-agent-addon/templates/argocd.yaml` | Spoke ArgoCD CR (agent config) |

### What each file contains

**`argocd-operator-crds.yaml`** — All 8 CRDs from the operator, concatenated:
- `applications.argoproj.io`
- `applicationsets.argoproj.io`
- `appprojects.argoproj.io`
- `argocdexports.argoproj.io`
- `argocds.argoproj.io`
- `imageupdaters.argocd-image-updater.argoproj.io`
- `namespacemanagements.argoproj.io`
- `notificationsconfigurations.argoproj.io`

**`operator.yaml`** — 15 Kubernetes resources (non-CRD):
- Namespace
- ServiceAccount
- Role (leader-election)
- 5x ClusterRole (manager-role, metrics-reader, namespacemanagement-editor/viewer, proxy-role)
- RoleBinding (leader-election)
- 2x ClusterRoleBinding (manager, proxy)
- ConfigMap (manager-config)
- 2x Service (metrics, webhook)
- Deployment (controller-manager)

## Update Procedure

### Prerequisites

- Access to the argocd-operator source (clone or local copy)
- `kustomize` installed (v4+)
- `controller-gen` available (the operator Makefile can install it)

### Step 1: Generate CRDs from operator source

```bash
cd <argocd-operator-repo>

# Regenerate CRDs from Go types (ensures they're up to date)
make manifests

# Build CRDs with kustomize, stripping the conversion webhook
# (webhook causes issues on clusters without cert-manager)
kustomize build config/crd | sed '/conversion:/,/- v1beta1/d' > /tmp/operator-crds-clean.yaml
```

### Step 2: Replace CRD files

```bash
cd <this-repo>
cp /tmp/operator-crds-clean.yaml charts/argocd-agent-addon/crds/argocd-operator-crds.yaml
cp /tmp/operator-crds-clean.yaml internal/addon/charts/argocd-agent-addon/crds/argocd-operator-crds.yaml
```

### Step 3: Generate and compare operator deployment manifests

```bash
cd <argocd-operator-repo>
kustomize build config/default > /tmp/operator-full.yaml
```

Extract the non-CRD resources and compare with `operator.yaml`:

```bash
cd <this-repo>

# List resources from operator
python3 -c "
import yaml
with open('/tmp/operator-full.yaml') as f:
    docs = list(yaml.safe_load_all(f))
for d in docs:
    if d and d.get('kind') not in ('CustomResourceDefinition', 'Namespace', None):
        print(f\"{d['kind']:30s} {d['metadata']['name']}\")
"
```

Compare the ClusterRole `argocd-operator-manager-role` rules carefully — this is where
most RBAC changes happen between operator versions.

### Step 4: Update operator.yaml

Manually update `operator.yaml` in both chart locations to match the new operator output.

**Important:** This repo adds 2 intentional env vars to the Deployment that are NOT
in the upstream operator. Preserve these when updating:

```yaml
env:
- name: ARGOCD_CLUSTER_CONFIG_NAMESPACES
  value: "*"
- name: ARGOCD_PRINCIPAL_TLS_SERVER_ALLOW_GENERATE
  value: "false"
```

All namespace references use Helm template variables (`{{ .Values.global.argoCDOperatorNamespace }}`
etc.) instead of hardcoded `argocd-operator-system`. Preserve this templating.

### Step 5: Update argocd.yaml (if needed)

If the ArgoCD CR schema changed (new fields in `argoCDAgent`, `principal`, `agent`), update:
- `charts/argocd-agent-addon/templates/argocd-operator/argocd.yaml` (hub/principal)
- `internal/addon/charts/argocd-agent-addon/templates/argocd.yaml` (spoke/agent)

### Step 6: Verify

```bash
# Verify both operator.yaml files are in sync (ignoring helm templates)
diff <(grep -v '{{' charts/argocd-agent-addon/templates/argocd-operator/operator.yaml) \
     <(grep -v '{{' internal/addon/charts/argocd-agent-addon/templates/operator.yaml)

# Verify CRD files are identical
diff charts/argocd-agent-addon/crds/argocd-operator-crds.yaml \
     internal/addon/charts/argocd-agent-addon/crds/argocd-operator-crds.yaml

# Run unit tests
make test

# Run all e2e tests
make test-e2e-advanced-pull-local
make test-e2e-advanced-pull-custom-namespace-local
make test-e2e-basic-pull-local
```

### Step 7: Update operator image tag

Update the operator image version in:
- `Makefile` — `ARGOCD_OPERATOR_IMAGE` variable
- `charts/argocd-agent-addon/values.yaml` — `operatorImageTag`
- `internal/addon/charts/argocd-agent-addon/values.yaml` — `operatorImageTag`
