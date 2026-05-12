<h1 align="center">Praesto</h1>

<hr />

<p align="center">
  <img src="docs/images/praesto.png" alt="SloK hero illustration" width="700" />
</p>

<p align="center">Kubernetes-native model cache operator for preparing and mounting AI models into workloads.</p>

<p align="center">
  <a href="https://opensource.org/licenses/Apache-2.0"><img alt="License" src="https://img.shields.io/badge/License-Apache%202.0-blue.svg"></a>
  <a href="https://kubernetes.io"><img alt="Kubernetes" src="https://img.shields.io/badge/Kubernetes-1.20%2B-brightgreen.svg"></a>
  <a href="https://goreportcard.com/report/github.com/federicolepera/praesto"><img alt="Go Report Card" src="https://goreportcard.com/badge/github.com/federicolepera/praesto"></a>
</p>

<p align="center">
  <a href="#what-praesto-does">What it does</a> ·
  <a href="#quick-start">Quick start</a> ·
  <a href="#modelcache-resources">ModelCache</a> ·
  <a href="#mutating-webhook">Webhook</a> ·
  <a href="#development">Development</a>
</p>

## What Praesto does

- Defines reusable model caches as Kubernetes custom resources.
- Creates a PVC for each model cache.
- Runs a downloader Job that prepares the model files into the PVC.
- Tracks cache readiness through Kubernetes status fields and conditions.
- Exposes model cache state directly with `kubectl get modelcache`.
- Mutates annotated Pods to mount a ready model cache PVC automatically.
- Keeps the official cluster installation based on Kustomize and cert-manager.
- Provides a local debug workflow for testing the mutating webhook outside the cluster.

## Quick start

Install the CRDs and controller manifests with Kustomize:

```bash
kubectl apply -k config/default
```

Create a `ModelCache`:

```yaml
apiVersion: praesto.praesto.io/v1alpha1
kind: ModelCache
metadata:
  name: tinyllama-test
  namespace: default
spec:
  source:
    type: huggingface
    model: TinyLlama/TinyLlama-1.1B-Chat-v1.0
  storage:
    size: 10Gi
```

Apply it:

```bash
kubectl apply -f config/samples/k8s/modelCache.yaml
```

Check status:

```bash
kubectl get modelcache
kubectl get modelcache tinyllama-test -o yaml
```

When the cache is ready, Praesto exposes status similar to:

```text
NAME             PHASE   PVC                    DOWNLOAD JOB
tinyllama-test   Ready   praesto-tinyllama-test praesto-download-tinyllama-test
```

## ModelCache resources

A `ModelCache` describes a model that should be downloaded and made available to
workloads through a Kubernetes volume.

Example:

```yaml
apiVersion: praesto.praesto.io/v1alpha1
kind: ModelCache
metadata:
  name: tinyllama-test
  namespace: default
spec:
  source:
    type: huggingface
    model: TinyLlama/TinyLlama-1.1B-Chat-v1.0
  storage:
    size: 10Gi
    storageClassName: standard
```

Praesto reconciles this resource by creating:

- a PVC named `praesto-<modelcache-name>`
- a downloader Job named `praesto-download-<modelcache-name>`

The downloader Job starts only after the PVC is bound.

### Status

Praesto updates the `ModelCache` status with:

| Field | Description |
|-------|-------------|
| `status.phase` | Current lifecycle phase |
| `status.pvcName` | Name of the generated PVC |
| `status.downloadJobName` | Name of the generated downloader Job |
| `status.conditions` | Kubernetes-style readiness conditions |

Supported phases:

| Phase | Description |
|-------|-------------|
| `Pending` | PVC or download preparation is still pending |
| `Downloading` | Downloader Job is running |
| `Ready` | Model files are available in the PVC |
| `Failed` | PVC or downloader Job failed |

Current conditions:

| Condition | Description |
|-----------|-------------|
| `PVCReady` | The model PVC is bound and usable |
| `DownloadComplete` | The downloader Job completed successfully |
| `Ready` | The model cache is ready to be mounted by workloads |

## Mutating webhook

Praesto includes a mutating webhook that injects a ready model cache into Pods.

Annotate a Pod or Pod template with:

```yaml
metadata:
  annotations:
    praesto.io/model-cache: tinyllama-test
    praesto.io/model-mount-path: /models
```

Example Deployment:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: praesto-webhook-test
spec:
  replicas: 1
  selector:
    matchLabels:
      app: praesto-webhook-test
  template:
    metadata:
      labels:
        app: praesto-webhook-test
      annotations:
        praesto.io/model-cache: tinyllama-test
        praesto.io/model-mount-path: /models
    spec:
      containers:
      - name: app
        image: busybox:1.36
        command:
        - sleep
        - "3600"
```

Apply the sample:

```bash
kubectl apply -f config/samples/k8s/deployment-webhook-test.yaml
```

Check the mutated Pod:

```bash
kubectl get pod -l app=praesto-webhook-test -o yaml
```

Open a shell in the container:

```bash
kubectl exec -it <pod-name> -- /bin/sh
```

## Local webhook debugging

The official Kustomize installation uses cert-manager for webhook certificates.

For local debugging, Praesto also provides a Make target that creates local TLS
certificates and installs a `MutatingWebhookConfiguration` that points directly to
your local debugger.

Install the local debug webhook configuration:

```bash
make mutatingwebhook
```

This command only:

- generates local `tls.crt` and `tls.key`
- installs the correct `MutatingWebhookConfiguration` in the cluster
- injects the generated certificate into `clientConfig.caBundle`

It does not start the manager or debugger.

Start the manager yourself with:

```bash
--webhook-cert-path=$TMPDIR/k8s-webhook-server/serving-certs
```

The local webhook URL defaults to:

```text
https://host.docker.internal:9443/mutate-v1-pod
```

Useful targets:

| Target | Description |
|--------|-------------|
| `make mutatingwebhook` | Generate certs and install the local debug webhook |
| `make mutatingwebhook-certs` | Generate only local TLS certs |
| `make mutatingwebhook-manifest` | Print the local webhook manifest |
| `make mutatingwebhook-delete` | Delete the local debug webhook |

You can override defaults:

```bash
WEBHOOK_HOST=localhost make mutatingwebhook
WEBHOOK_PORT=9443 make mutatingwebhook
WEBHOOK_CERT_DIR=/tmp/praesto-certs make mutatingwebhook
```

When finished:

```bash
make mutatingwebhook-delete
```

## Installation options

### Kustomize

For a cluster installation:

```bash
kubectl apply -k config/default
```

The Kustomize installation is the official path and uses cert-manager for webhook
certificates.

### Local development

Install CRDs:

```bash
make install
```

Run the controller locally against your current kubeconfig:

```bash
make run
```

For webhook debugging, install the local webhook configuration first:

```bash
make mutatingwebhook
```

Then start your debugger with the generated certificate directory.

## Requirements

| Requirement | Notes |
|-------------|-------|
| Kubernetes 1.20+ | CRDs, Jobs, PVCs, and admission webhooks |
| cert-manager | Required by the official Kustomize webhook installation |
| A ReadWriteMany-capable StorageClass | Required for sharing model caches across workloads |
| kubectl | Required for local install and debug workflows |
| OpenSSL | Required for local webhook certificate generation |

## Development

```bash
# Run the controller locally against your current kubeconfig
make run

# Build the operator
make build

# Run tests
make test

# Run lint
make lint

# Build an operator image
make docker-build IMG=ghcr.io/federicolepera/praesto:dev
```

Useful development commands:

```bash
# Generate manifests and CRDs
make manifests

# Install CRDs into the current cluster
make install

# Uninstall CRDs from the current cluster
make uninstall

# Deploy the controller to the current cluster
make deploy IMG=ghcr.io/federicolepera/praesto:dev

# Remove the controller from the current cluster
make undeploy
```

## Current limitations

- Praesto is currently early-stage and focused on the v0.1.0 workflow.
- The downloader flow is intentionally simple and may change.
- The mutating webhook expects a ready `ModelCache` in the same namespace as the Pod.
- The official installation path currently uses Kustomize, not Helm.
- Documentation is intentionally minimal for now and will be expanded later.

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.
