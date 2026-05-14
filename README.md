<h1 align="center">Praesto</h1>

<hr />

<p align="center">
  <img src="docs/images/praesto.png" alt="Praesto hero illustration" width="700" />
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
  <a href="#admission-webhooks">Webhooks</a> ·
  <a href="#local-webhook-debugging">Debugging</a> ·
  <a href="#development">Development</a>
</p>

## What Praesto does

- Defines reusable model caches as Kubernetes custom resources.
- Creates a ReadWriteMany PVC for each `ModelCache`.
- Runs a downloader Job that prepares model files into the PVC.
- Tracks cache readiness through Kubernetes status fields and conditions.
- Mutates annotated Pods to mount a ready model cache PVC automatically.
- Provides a lightweight quick start workload that verifies the mounted model files.
- Uses Kustomize and cert-manager for the official cluster installation.
- Provides local webhook debug targets for running the manager from an IDE/debugger.

## Quick start

The quick start validates the complete v0.1.0 flow:

```text
ModelCache → PVC → downloader Job → Ready status → annotated Deployment → mounted model files
```

### 1. Prerequisites

You need:

- a Kubernetes cluster
- `kubectl`
- cert-manager installed in the cluster
- a StorageClass that supports `ReadWriteMany`

The sample uses:

```yaml
storageClassName: standard
```

If your cluster uses a different RWX StorageClass, edit:

```text
config/samples/quickstart/00-modelcache-tinyllama.yaml
```

### 2. Install Praesto

Install CRDs, RBAC, controller, webhook service, and cert-manager webhook certificates:

```bash
kubectl apply -k config/default
```

This installs the published operator image `ghcr.io/federicolepera/praesto:latest`.
For reproducible installs, pin a release tag instead:

```bash
make deploy IMG=ghcr.io/federicolepera/praesto:0.1.0
```

Use `make deploy IMG=...` for local builds, private registries, or any custom
operator image override. The generated manifests still support normal Kustomize
image replacement.

Wait for the controller manager:

```bash
kubectl get pods -n praesto-system
```

### 3. Create a ModelCache

The quick start creates a `ModelCache` for TinyLlama:

```yaml
apiVersion: praesto.praesto.io/v1alpha1
kind: ModelCache
metadata:
  name: tinyllama-test
  namespace: default
spec:
  source:
    huggingface:
      repo: TinyLlama/TinyLlama-1.1B-Chat-v1.0
      revision: main
  storage:
    storageClassName: standard
    size: 10Gi
```

Apply it:

```bash
kubectl apply -f config/samples/quickstart/00-modelcache-tinyllama.yaml
```

Watch the lifecycle:

```bash
kubectl get modelcache tinyllama-test -w
```

You can also inspect the generated PVC and Job:

```bash
kubectl get pvc
kubectl get jobs
kubectl get pods -l praesto.io/job-type=download
```

When the cache is ready, Praesto exposes status similar to:

```text
NAME             PHASE   PVC                     DOWNLOAD JOB
tinyllama-test   Ready   praesto-tinyllama-test  praesto-download-tinyllama-test
```

### 4. Create a workload that uses the model cache

Enable Praesto Pod injection in the workload namespace:

```bash
kubectl label namespace default praesto.io/model-cache-injection=enabled
```

Praesto only calls the mutating Pod webhook in namespaces with this label.

Apply the tokenizer test Deployment:

```bash
kubectl apply -f config/samples/quickstart/01-tokenizer-deployment.yaml
```

The Deployment has these annotations:

```yaml
praesto.io/model-cache: tinyllama-test
praesto.io/model-mount-path: /models
```

The mutating webhook uses them to mount the generated PVC read-only into the Pod.

### 5. Verify that the mounted model works

Check logs:

```bash
kubectl logs -l app=praesto-tokenizer-test -f
```

Expected output includes:

```text
Tokenizer loaded from /models
{'input_ids': [...], 'attention_mask': [...]}
```

This test intentionally loads only the HuggingFace tokenizer. It is lightweight
enough for local clusters such as minikube and proves that the workload can read
valid model files from the Praesto-mounted cache.

Inspect the mutated Pod if needed:

```bash
kubectl get pod -l app=praesto-tokenizer-test -o yaml
```

Open a shell in the container:

```bash
kubectl exec -it deploy/praesto-tokenizer-test -- /bin/sh
ls -lah /models
```

### 6. Cleanup

```bash
kubectl delete -f config/samples/quickstart/01-tokenizer-deployment.yaml
kubectl delete -f config/samples/quickstart/00-modelcache-tinyllama.yaml
```

## Samples

Samples live under:

```text
config/samples/
```

Current quick start samples:

| File | Purpose |
|------|---------|
| `config/samples/quickstart/00-modelcache-tinyllama.yaml` | Creates the TinyLlama `ModelCache` |
| `config/samples/quickstart/01-tokenizer-deployment.yaml` | Creates a lightweight tokenizer workload that uses the cache |

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
    huggingface:
      repo: TinyLlama/TinyLlama-1.1B-Chat-v1.0
      revision: main
  storage:
    storageClassName: standard
    size: 10Gi
```

Praesto reconciles this resource by creating:

- a PVC named `praesto-<modelcache-name>`
- a downloader Job named `praesto-download-<modelcache-name>`

The downloader Job starts only after the PVC is bound.

Praesto verifies that `spec.storage.storageClassName` exists before creating the
PVC. If the StorageClass is missing, the `ModelCache` moves to `Failed` with a
`PVCReady=False` condition reason `StorageClassNotFound`. Kubernetes does not
expose a generic way to know whether a StorageClass supports `ReadWriteMany`, so
PVCs that remain pending include a status message that points users to verify
RWX support for the configured StorageClass.

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

## Admission webhooks

Praesto uses admission webhooks for two workflows.

### Mutating Pod webhook

The mutating webhook injects a ready model cache into annotated Pods.

Before creating annotated Pods, enable injection in the workload namespace:

```bash
kubectl label namespace <namespace> praesto.io/model-cache-injection=enabled
```

Namespaces without this label are ignored by the mutating webhook. This keeps
unrelated workloads from depending on Praesto webhook availability.

Required annotation:

```yaml
praesto.io/model-cache: tinyllama-test
```

Optional annotation:

```yaml
praesto.io/model-mount-path: /models
praesto.io/target-container: app
```

If the mount path is omitted, Praesto uses `/models`.
If the target container is omitted, Praesto mounts the cache into the first
container in the Pod spec. For multi-container Pods, set
`praesto.io/target-container` on the Pod template to choose the application
container explicitly.

The webhook:

- reads the requested `ModelCache` from the Pod namespace
- requires the `ModelCache` to be `Ready`
- injects the generated PVC as a read-only volume
- mounts it into the target container, or the first container when no target is configured

The webhook uses `failurePolicy: Fail` inside opt-in namespaces. If Praesto is
unavailable, annotated Pods in enabled namespaces are rejected instead of running
without their model cache.

### Validating ModelCache webhook

The validating webhook checks simple `ModelCache` input errors on create/update:

- `spec.storage.storageClassName` is required
- `spec.storage.size` is required
- `spec.storage.size` must be a valid Kubernetes quantity greater than zero
- `spec.source.huggingface.repo` is required
- HuggingFace token `secretRef.name` and `secretRef.key` must be configured together

## Installation options

### Kustomize

The official cluster installation path is Kustomize:

```bash
kubectl apply -k config/default
```

This path expects cert-manager to be installed because Praesto uses admission
webhooks. It uses the published `ghcr.io/federicolepera/praesto:latest` operator
image by default. To pin a release or deploy a custom build, use:

```bash
make deploy IMG=ghcr.io/federicolepera/praesto:0.1.0
```

### Local development without webhooks

Install only the CRDs:

```bash
make install
```

Run the controller locally against your current kubeconfig:

```bash
make run
```

## Local webhook debugging

For webhook debugging, run the manager locally from your IDE/debugger and install
webhook configurations that point directly to your local machine instead of the
in-cluster webhook Service.

Create local certificates:

```bash
make certs-create
```

Start the manager/debugger with:

```bash
--webhook-cert-path=$TMPDIR/k8s-webhook-server/serving-certs
```

Install the local mutating webhook configuration:

```bash
make mutatingwebhook
```

The local mutating webhook uses the same namespace opt-in as the cluster
installation. Label any namespace you want to debug:

```bash
kubectl label namespace <namespace> praesto.io/model-cache-injection=enabled
```

Install the local validating webhook configuration:

```bash
make validatewebhook
```

Defaults:

| Setting | Default |
|---------|---------|
| Webhook host | `host.docker.internal` |
| Webhook port | `9443` |
| Mutating path | `/mutate-v1-pod` |
| Validating path | `/validate-praesto-praesto-io-v1alpha1-modelcache` |
| Cert directory | `$TMPDIR/k8s-webhook-server/serving-certs` |

Useful targets:

| Target | Description |
|--------|-------------|
| `make certs-create` | Generate local TLS certs only |
| `make mutatingwebhook` | Install the local debug `MutatingWebhookConfiguration` |
| `make validatewebhook` | Install the local debug `ValidatingWebhookConfiguration` |
| `make mutatingwebhook-manifest` | Print the local mutating webhook manifest |
| `make validatingwebhook-manifest` | Print the local validating webhook manifest |
| `make mutatingwebhook-delete` | Delete local debug webhook configurations |

Override defaults if needed:

```bash
WEBHOOK_HOST=localhost make mutatingwebhook
WEBHOOK_PORT=9443 make validatewebhook
WEBHOOK_CERT_DIR=/tmp/praesto-certs make certs-create
```

Cleanup local webhook configurations:

```bash
make mutatingwebhook-delete
```

## Requirements

| Requirement | Notes |
|-------------|-------|
| Kubernetes 1.20+ | CRDs, Jobs, PVCs, and admission webhooks |
| cert-manager | Required by the official Kustomize webhook installation |
| ReadWriteMany-capable StorageClass | Required for sharing model caches across workloads |
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

# Generate deepcopy code
make generate

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
- Multi-container Pods should set `praesto.io/target-container`; otherwise the mutating webhook mounts the cache into the first container.
- The mutating webhook expects a ready `ModelCache` in the same namespace as the Pod.
- The mutating webhook only runs in namespaces labeled `praesto.io/model-cache-injection=enabled`.
- The official installation path currently uses Kustomize, not Helm.
- Storage must be provided by a user-managed RWX-capable StorageClass. Praesto
  validates StorageClass existence, but RWX support is diagnosed from PVC
  pending status because it is provisioner-specific.

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.
