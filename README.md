# ArgoCD Pull Integration Controller

An ArgoCD controller that implements a pull-based application delivery model for multi-cluster environments using [Open Cluster Management (OCM)](https://open-cluster-management.io/).

## Overview

Traditional ArgoCD deployments use a "push" model where applications are pushed from a centralized ArgoCD instance to remote clusters. This controller enables a "pull" model where remote clusters pull their applications from a central hub, providing better scalability, security, and resilience.

### Push Model vs Pull Model

**Traditional Push Model:**
![push model](assets/push.png)

**Pull Model with OCM:**
![pull model](assets/pull.png)

### Key Benefits

- **Scalability**: Hub-spoke architecture scales better than centralized push
- **Security**: Cluster credentials are not stored in a centralized location
- **Resilience**: Reduces single point of failure impact
- **Decentralization**: Remote clusters maintain autonomy while staying synchronized

## Architecture

The controller consists of three main components:

1. **Application Controller**: Watches ArgoCD Applications with specific labels/annotations and wraps them in OCM ManifestWork objects
2. **Application Status Controller**: Syncs application status from ManifestWork back to the hub Application
3. **Cluster Controller**: Manages cluster registration and ArgoCD cluster secrets

### How It Works

1. ArgoCD Applications on the hub cluster are marked with special labels and annotations
2. The Application Controller detects these applications and creates ManifestWork objects containing the Application as payload
3. OCM agents on managed clusters pull these ManifestWork objects and apply the contained Applications
4. Application status is synced back from managed clusters to the hub through ManifestWork status feedback

## Prerequisites

### Required Components

- **Open Cluster Management (OCM)**: Multi-cluster environment with hub and managed clusters
  - See [OCM Quick Start](https://open-cluster-management.io/getting-started/quick-start/) for setup instructions
- **ArgoCD**: Installed on both hub and managed clusters
  - See [ArgoCD Getting Started](https://argo-cd.readthedocs.io/en/stable/getting_started/) for installation

### Required Annotations and Labels

For Applications to be processed by the pull controller, they must include:

```yaml
metadata:
  labels:
    apps.open-cluster-management.io/pull-to-ocm-managed-cluster: "true"
  annotations:
    argocd.argoproj.io/skip-reconcile: "true"
    apps.open-cluster-management.io/ocm-managed-cluster: "<target-cluster-name>"
```

- `pull-to-ocm-managed-cluster` label: Enables the pull controller to process this Application
- `skip-reconcile` annotation: Prevents the Application from reconciling on the hub cluster
- `ocm-managed-cluster` annotation: Specifies which managed cluster should receive this Application

## Installation

### Step 1: Prepare Hub Cluster

**Option A: Disable Hub ArgoCD Application Controller (Legacy Method)**

If your ArgoCD version doesn't support the `skip-reconcile` annotation:

```bash
kubectl -n argocd scale statefulset/argocd-application-controller --replicas 0
```

**Option B: Use Skip Reconcile (Recommended)**

For ArgoCD versions that support [skip reconcile](https://argo-cd.readthedocs.io/en/latest/user-guide/skip_reconcile/), no additional configuration needed.

### Step 2: Install the Pull Controller

```bash
kubectl apply -f https://raw.githubusercontent.com/open-cluster-management-io/argocd-pull-integration/main/deploy/install.yaml
```

### Step 3: Verify Installation

Check that the controller is running:

```bash
kubectl -n open-cluster-management get deploy | grep pull
```

Expected output:
```
argocd-pull-integration-controller-manager   1/1     1            1           106s
```

## Configuration

### Hub Cluster Setup

#### Create ArgoCD Cluster Secret

Create an ArgoCD cluster secret representing each managed cluster:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: <cluster-name>-secret
  namespace: argocd
  labels:
    argocd.argoproj.io/secret-type: cluster
type: Opaque
stringData:
  name: <cluster-name>
  server: https://<cluster-name>-control-plane:6443
EOF
```

**Note**: Replace `<cluster-name>` with your actual managed cluster name. This step can be automated using the [OCM auto import controller](https://github.com/open-cluster-management-io/multicloud-integrations/).

#### Apply Hub Configuration

```bash
kubectl apply -f example/hub
```

### Managed Cluster Setup

Apply the required RBAC and configuration on each managed cluster:

```bash
kubectl apply -f example/managed
```

## Quick Start Example

Deploy the included guestbook example to test the pull integration:

### Deploy ApplicationSet

```bash
kubectl apply -f example/guestbook-app-set.yaml
```

### Verify ApplicationSet Template

The ApplicationSet template must include the required annotations and labels:

```yaml
spec:
  template:
    metadata:
      labels:
        apps.open-cluster-management.io/pull-to-ocm-managed-cluster: "true"
      annotations:
        argocd.argoproj.io/skip-reconcile: "true"
        apps.open-cluster-management.io/ocm-managed-cluster: "{{name}}"
```

## Verification

### Check ApplicationSet Creation

```bash
kubectl -n argocd get appset
```

Expected output:
```
NAME            AGE
guestbook-app   84s
```

### Check Generated Applications

```bash
kubectl -n argocd get app
```

Expected output:
```
NAME                     SYNC STATUS   HEALTH STATUS
cluster1-guestbook-app   Unknown       Unknown
```

### Check ManifestWork Creation

On the hub cluster, verify ManifestWork objects are created:

```bash
kubectl -n <cluster-name> get manifestwork
```

Expected output:
```
NAME                          AGE
cluster1-guestbook-app-d0e5   2m41s
```

### Check Application on Managed Cluster

On the managed cluster, verify the Application is pulled and deployed:

```bash
kubectl -n argocd get app
```

Expected output:
```
NAME                     SYNC STATUS   HEALTH STATUS
cluster1-guestbook-app   Synced        Healthy
```

### Check Application Resources

Verify the actual application resources are deployed:

```bash
kubectl -n guestbook get deploy
```

Expected output:
```
NAME           READY   UP-TO-DATE   AVAILABLE   AGE
guestbook-ui   1/1     1            1           7m36s
```

### Check Status Synchronization

Back on the hub cluster, verify status is synced from the managed cluster:

```bash
kubectl -n argocd get app
```

Expected output:
```
NAME                     SYNC STATUS   HEALTH STATUS
cluster1-guestbook-app   Synced        Healthy
```

## Troubleshooting

### Common Issues

**Applications not being processed:**
- Verify the required labels and annotations are present
- Check that the managed cluster name in annotations matches an actual OCM managed cluster
- Ensure the pull controller is running in the `open-cluster-management` namespace

**ManifestWork not created:**
- Check controller logs: `kubectl -n open-cluster-management logs deployment/argocd-pull-integration-controller-manager`
- Verify OCM is properly installed and managed clusters are registered

**Status not syncing back:**
- Ensure the Application Status Controller is running
- Check ManifestWork status feedback configuration on managed clusters

### Controller Logs

```bash
kubectl -n open-cluster-management logs deployment/argocd-pull-integration-controller-manager -f
```

## Development

For development setup and contribution guidelines, see the [CONTRIBUTING.md](CONTRIBUTING.md) document.

### Building from Source

```bash
make build
```

### Running Tests

```bash
make test
```

### Building Container Image

```bash
make docker-build
```

## Community and Support

### Communication Channels

- **Slack**: [#open-cluster-mgmt](https://kubernetes.slack.com/channels/open-cluster-mgmt) on Kubernetes Slack
- **GitHub Discussions**: Use GitHub Issues for bug reports and feature requests
- **Mailing List**: [OCM Community](https://open-cluster-management.io/community/)

### Contributing

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) for details on:
- Code of conduct
- Development process
- Submitting pull requests
- Reporting issues

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

