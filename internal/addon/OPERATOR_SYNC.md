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

**`argocd-operator-crds.yaml`** — All CRDs from the operator (`config/crd/bases/*.yaml`),
concatenated. As of v0.18.0 these are:
- `applications.argoproj.io`
- `applicationsets.argoproj.io`
- `appprojects.argoproj.io`
- `argocdexports.argoproj.io`
- `argocds.argoproj.io`
- `imageupdaters.argocd-image-updater.argoproj.io`
- `namespacemanagements.argoproj.io`
- `notificationsconfigurations.argoproj.io`

**`operator.yaml`** — Non-CRD Kubernetes resources produced by `kustomize build config/default`.
The exact set of resources may change between operator versions (e.g. v0.18.0 has
`argocd-operator-proxy-role` while later versions rename it to `argocd-operator-metrics-role`).
Typical resources include:
- Namespace
- ServiceAccount
- Role (leader-election)
- ClusterRoles (manager-role, metrics/proxy roles, editor/viewer roles)
- RoleBinding (leader-election)
- ClusterRoleBindings
- ConfigMap (manager-config)
- Services (metrics, webhook)
- Deployment (controller-manager)

## Update Procedure

### Prerequisites

- Access to the argocd-operator source at the target tag (clone or local copy)
- `kustomize` installed (v4+)
- `python3` with `PyYAML` (for resource listing/comparison)

### Step 1: Generate CRDs from operator source

```bash
cd <argocd-operator-repo>
git checkout <target-version-tag>  # e.g. v0.18.0

# Option A (recommended): regenerate CRDs from Go types first
# This ensures CRDs reflect the actual Go struct definitions.
# Requires controller-gen (the operator Makefile can install it).
make manifests

# Option B: skip regeneration if you trust the tag's committed CRD bases
# (safe for tagged releases since CI validates them)
```

Build CRDs with kustomize and strip the conversion webhook section.
The webhook references a cluster-local cert-manager Service that won't exist
on clusters without cert-manager, so it must be removed.

```bash
# Build CRDs via kustomize (applies labels and ordering from kustomization.yaml)
kustomize build config/crd > /tmp/operator-crds-raw.yaml

# Remove the .spec.conversion block from all CRD documents.
# This sed removes lines from "  conversion:" through the end of that block
# (detected by the next sibling field at the same 2-space indent level).
# It is version-agnostic — works regardless of what conversionReviewVersions
# or webhook config the operator declares.
sed '/^  conversion:$/,/^  [a-z]/{/^  [a-z]/!d; /^  conversion:$/d}' \
    /tmp/operator-crds-raw.yaml > /tmp/operator-crds-clean.yaml

# Verify no conversion sections remain
grep -c "conversion:" /tmp/operator-crds-clean.yaml  # should output 0
```

### Step 2: Replace CRD files

```bash
cd <this-repo>
cp /tmp/operator-crds-clean.yaml charts/argocd-agent-addon/crds/argocd-operator-crds.yaml
cp /tmp/operator-crds-clean.yaml internal/addon/charts/argocd-agent-addon/crds/argocd-operator-crds.yaml
```

### Step 3: Generate operator deployment manifests

```bash
cd <argocd-operator-repo>
kustomize build config/default > /tmp/operator-full.yaml
```

List the non-CRD resources to understand what the new version produces:

```bash
python3 -c "
import yaml
with open('/tmp/operator-full.yaml') as f:
    docs = list(yaml.safe_load_all(f))
for d in docs:
    if d and d.get('kind') not in ('CustomResourceDefinition', None):
        ns = d['metadata'].get('namespace', '(cluster-scoped)')
        print(f\"{d['kind']:30s} {ns:<30s} {d['metadata']['name']}\")
"
```

Extract just the non-CRD resources into a standalone file for comparison:

```bash
python3 -c "
import yaml
with open('/tmp/operator-full.yaml') as f:
    docs = list(yaml.safe_load_all(f))
non_crd = [d for d in docs if d and d.get('kind') not in ('CustomResourceDefinition', None)]
with open('/tmp/operator-non-crd.yaml', 'w') as f:
    yaml.dump_all(non_crd, f, default_flow_style=False)
"
```

### Step 4: Compare and update operator.yaml

Compare the new manifests against our embedded `operator.yaml`. Focus on:

1. **ClusterRole `argocd-operator-manager-role` rules** — this is where most RBAC
   changes happen between operator versions.
2. **New or renamed resources** — resources may be added, removed, or renamed
   (e.g. `proxy-role` → `metrics-role` between v0.18.0 and v0.19.0).
3. **Deployment spec changes** — new args, volume mounts, security context, etc.

```bash
cd <this-repo>

# Quick comparison (parse embedded template, replacing Helm vars with defaults)
python3 -c "
import yaml, json

with open('/tmp/operator-full.yaml') as f:
    kust_docs = list(yaml.safe_load_all(f))
kust = {}
for d in kust_docs:
    if d and d.get('kind') not in ('CustomResourceDefinition', None):
        kust[f\"{d['kind']}/{d['metadata']['name']}\"] = d

with open('internal/addon/charts/argocd-agent-addon/templates/operator.yaml') as f:
    content = f.read()
content = content.replace('{{ .Values.global.argoCDOperatorNamespace }}', 'argocd-operator-system')
content = content.replace('\"{{ .Values.operatorImage }}:{{ .Values.operatorImageTag }}\"', 'PLACEHOLDER_IMAGE')
emb_docs = list(yaml.safe_load_all(content))
emb = {}
for d in emb_docs:
    if d and d.get('kind') not in ('CustomResourceDefinition', None):
        emb[f\"{d['kind']}/{d['metadata']['name']}\"] = d

kust_keys = set(kust.keys())
emb_keys = set(emb.keys())
if kust_keys - emb_keys:
    print('NEW in operator (add to operator.yaml):')
    for k in sorted(kust_keys - emb_keys): print(f'  + {k}')
if emb_keys - kust_keys:
    print('REMOVED from operator (delete from operator.yaml):')
    for k in sorted(emb_keys - kust_keys): print(f'  - {k}')
if not (kust_keys - emb_keys) and not (emb_keys - kust_keys):
    print('Resource sets match.')
"
```

Manually update `operator.yaml` in both chart locations to match the new operator output.

**Important — preserve these intentional differences from upstream:**

1. **Custom env vars** in the Deployment (NOT present in upstream):
   ```yaml
   env:
   - name: ARGOCD_CLUSTER_CONFIG_NAMESPACES
     value: "*"
   - name: ARGOCD_PRINCIPAL_TLS_SERVER_ALLOW_GENERATE
     value: "false"
   ```

2. **Helm template variables** instead of hardcoded namespaces:
   - `{{ .Values.global.argoCDOperatorNamespace }}` instead of `argocd-operator-system`
   - `"{{ .Values.operatorImage }}:{{ .Values.operatorImageTag }}"` instead of the
     image digest from `config/manager/kustomization.yaml`

3. **`app.kubernetes.io/managed-by: argocd-agent-addon`** label on the Namespace resource.

4. **Image reference**: upstream uses a `@sha256:...` digest (set in
   `config/manager/kustomization.yaml`). We use a tag-based reference controlled
   by the Makefile's `ARGOCD_OPERATOR_IMAGE` variable.

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
