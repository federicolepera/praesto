<h1 align="center">Praesto</h1>

<h3 align="center">
  <a name="readme-top"></a>
  <img
    src="docs/images/praesto.png"
    alt="Praesto"
    width="700"
  >
</h3>

<div align="center">

<p align="center">
  Kubernetes-native model cache operator and CSI node driver for preparing AI model artifacts once per node and mounting them into workloads through simple Pod annotations.
</p>

<div align="center">
  <a href="https://github.com/federicolepera/praesto/blob/main/LICENSE">
    <img src="https://img.shields.io/github/license/federicolepera/praesto" alt="License">
  </a>
  <a href="https://github.com/federicolepera/praesto/releases">
    <img src="https://img.shields.io/github/v/release/federicolepera/praesto" alt="Release">
  </a>
  <a href="https://goreportcard.com/report/github.com/federicolepera/praesto">
    <img src="https://goreportcard.com/badge/github.com/federicolepera/praesto" alt="Go Report Card">
  </a>
</div>

<div align="center">
  <a href="https://ghcr.io/federicolepera/praesto">
    <img src="https://img.shields.io/badge/ghcr.io-praesto-blue" alt="GitHub Container Registry">
  </a>
  <a href="https://kubernetes.io/">
    <img src="https://img.shields.io/badge/kubernetes-operator-326CE5?logo=kubernetes&logoColor=white" alt="Kubernetes">
  </a>
  <a href="https://github.com/federicolepera/praesto/pkgs/container/praesto%2Fcharts%2Fpraesto">
    <img src="https://img.shields.io/badge/helm-chart-0F1689?logo=helm&logoColor=white" alt="Helm Chart">
  </a>
</div>
</div>

## Table of Contents
- [What is Praesto?](#what-is-praesto)
- [How it Works](#how-it-works)
- [Storage modes](#storage-modes)
- [Quickstart](#quickstart)
  - [Prerequisites](#prerequisites)
  - [Helm](#helm)
  - [Kustomize](#kustomize)
  - [Create a ModelCache](#create-a-modelcache)
  - [Mount the ModelCache in a workload](#mount-the-modelcache-in-a-workload)
  - [Verify the mounted files](#verify-the-mounted-files)
- [ModelCache Resources](#modelcache-resources)
- [Admission Webhooks](#admission-webhooks)
- [Configuration](#configuration)
  - [Helm Values](#helm-values)
  - [Pod Annotations](#pod-annotations)
  - [Downloader Settings](#downloader-settings)
- [Samples](#samples)
- [Local Development](#local-development)
- [Local Webhook Debugging](#local-webhook-debugging)
- [Requirements](#requirements)
- [Current Limitations](#current-limitations)
- [Roadmap](#roadmap)
- [Contributing](#contributing)
- [License](#license)

## What is Praesto?

**Praesto** is a Kubernetes operator and CSI node driver that makes AI model artifacts reusable across workloads.

Instead of downloading the same model inside every Pod, Praesto lets you define a `ModelCache` custom resource. The operator prepares the model once per selected node, tracks readiness through `ModelCacheNode` resources, and the Praesto CSI driver mounts the local cache into workloads.

The core workflow is the local CSI mode:

- **ModelCache CRD**: declare which model should be cached.
- **ModelCacheNode CRD**: represent the model cache state on each Kubernetes node.
- **Local node cache**: store model files under `/var/praesto/<namespace>/<modelcache>`.
- **Downloader Job per node**: download HuggingFace artifacts into the local cache.
- **CSI node driver**: mount the local cache into user Pods with a CSI inline volume.
- **Mutating webhook**: inject the CSI volume into annotated Pods.

Praesto also keeps a legacy RWX PVC mode for clusters that already provide a shared `ReadWriteMany` StorageClass.

## How it Works

```text
ModelCache
  ↓
ModelCacheNode per selected node
  ↓
Local PV/PVC + downloader Job on each node
  ↓
Model files in /var/praesto/<namespace>/<modelcache>
  ↓
Praesto CSI node driver
  ↓
Annotated Pod with read-only model mount
```

In local CSI mode, Praesto reconciles each `ModelCache` by creating one cluster-scoped `ModelCacheNode` per target node. Each `ModelCacheNode` owns the node-local storage preparation and downloader Job. Once the node cache is ready, workloads can request the cache with Pod annotations and the mutating webhook injects a CSI volume using the `csi.praesto.io` driver.

## Storage modes

Praesto currently supports two storage modes. The mode is selected by `ModelCache.spec.storage.storageClassName`.

### Local CSI mode: `storageClassName` empty

This is the primary Praesto direction.

```yaml
spec:
  storage:
    size: 10Gi
```

When `storageClassName` is empty or omitted, Praesto:

1. creates `ModelCacheNode` resources for matching nodes;
2. prepares local node storage under `/var/praesto/<namespace>/<modelcache>`;
3. runs downloader Jobs on those nodes;
4. exposes the ready cache through the Praesto CSI node driver;
5. injects a CSI volume into annotated Pods.

Injected Pod volume:

```yaml
volumes:
  - name: praesto-model-cache
    csi:
      driver: csi.praesto.io
      readOnly: true
      volumeAttributes:
        modelCacheNamespace: default
        modelCacheName: tinyllama-test
```

This avoids requiring user workloads to mount internal Praesto PVCs directly.

The local cache base path is configurable with Helm:

```yaml
localCache:
  basePath: /mnt/fast-ssd/praesto
```

Praesto will then use paths like:

```text
/mnt/fast-ssd/praesto/<namespace>/<modelcache>
```

The base path should point to storage that exists on every node where models may be cached, for example a mounted local SSD. Praesto does not currently create the node-local source directory automatically; this is planned for the node agent.

### Legacy RWX PVC mode: `storageClassName` set

```yaml
spec:
  storage:
    storageClassName: standard
    size: 10Gi
```

When `storageClassName` is set, Praesto uses the older shared-volume workflow:

1. creates a namespaced PVC;
2. runs one downloader Job into that PVC;
3. injects the PVC read-only into annotated Pods.

This mode requires a StorageClass that supports `ReadWriteMany` if multiple Pods or nodes need to consume the cache.

## Quickstart

The quickstart validates the legacy RWX PVC flow:

```text
ModelCache → PVC → downloader Job → Ready status → annotated Deployment → mounted model files
```

For the local CSI flow, use the samples in `config/samples/presto.csi/`.

### Prerequisites

You need:

- a Kubernetes cluster
- `kubectl`
- `helm` if installing via the Helm chart
- cert-manager installed in the cluster
- the Praesto CSI node driver enabled for local CSI mode, or a StorageClass that supports `ReadWriteMany` for legacy PVC mode

The quickstart sample uses the legacy PVC mode:

```yaml
storageClassName: standard
```

If your cluster uses a different RWX StorageClass, edit:

```text
config/samples/quickstart/00-modelcache-tinyllama.yaml
```

### Helm

Install Praesto with the local chart:

```bash
helm install praesto ./charts/praesto \
  --namespace praesto-system \
  --create-namespace
```

Pin release images explicitly:

```bash
helm install praesto ./charts/praesto \
  --namespace praesto-system \
  --create-namespace \
  --set image.tag=0.1.0 \
  --set downloader.image.tag=0.1.0
```

The chart can also be published and installed as an OCI Helm package from GHCR:

```bash
helm install praesto oci://ghcr.io/federicolepera/praesto/charts/praesto \
  --version 0.1.0 \
  --namespace praesto-system \
  --create-namespace
```

See [`charts/praesto/README.md`](charts/praesto/README.md) for the full values reference and CRD upgrade notes.

### Kustomize

Install CRDs, RBAC, controller, webhook service, and cert-manager webhook certificates:

```bash
kubectl apply -k config/default
```

This installs the published operator image `ghcr.io/federicolepera/praesto:latest`.
For reproducible installs, pin a release tag instead:

```bash
make deploy IMG=ghcr.io/federicolepera/praesto:0.1.0
```

Wait for the controller manager:

```bash
kubectl get pods -n praesto-system
```

### Create a ModelCache

The quickstart creates a `ModelCache` for TinyLlama:

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

Inspect the generated PVC and Job:

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

### Mount the ModelCache in a workload

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

### Verify the mounted files

Check logs:

```bash
kubectl logs -l app=praesto-tokenizer-test -f
```

Expected output includes:

```text
Tokenizer loaded from /models
{'input_ids': [...], 'attention_mask': [...]}
```

This test intentionally loads only the HuggingFace tokenizer. It is lightweight enough for local clusters such as minikube and proves that the workload can read valid model files from the Praesto-mounted cache.

Inspect the mutated Pod if needed:

```bash
kubectl get pod -l app=praesto-tokenizer-test -o yaml
```

Open a shell in the container:

```bash
kubectl exec -it deploy/praesto-tokenizer-test -- /bin/sh
ls -lah /models
```

Cleanup:

```bash
kubectl delete -f config/samples/quickstart/01-tokenizer-deployment.yaml
kubectl delete -f config/samples/quickstart/00-modelcache-tinyllama.yaml
```

## ModelCache Resources

A `ModelCache` describes a model that should be downloaded and made available to workloads through either the Praesto CSI driver or a legacy PVC volume.

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
    size: 10Gi
```

With `storageClassName` omitted, Praesto uses local CSI mode and creates:

- one `ModelCacheNode` per selected node
- a local PV/PVC pair for each node cache
- a downloader Job per node
- a CSI mount surface for workloads

Each `ModelCacheNode` tracks node-local readiness and points to the local path used by the CSI driver.

With `storageClassName` set, Praesto uses legacy PVC mode and creates:

- a PVC named `praesto-<modelcache-name>`
- a downloader Job named `praesto-download-<modelcache-name>`

In legacy PVC mode, Praesto verifies that `spec.storage.storageClassName` exists before creating the PVC. If the StorageClass is missing, the `ModelCache` moves to `Failed` with a `PVCReady=False` condition reason `StorageClassNotFound`.

Kubernetes does not expose a generic way to know whether a StorageClass supports `ReadWriteMany`, so PVCs that remain pending include a status message that points users to verify RWX support for the configured StorageClass.

### Status

Praesto updates the `ModelCache` status with:

| Field | Description |
|-------|-------------|
| `status.phase` | Current lifecycle phase |
| `status.pvcName` | Name of the generated PVC in legacy PVC mode |
| `status.downloadJobName` | Name of the generated downloader Job in legacy PVC mode |
| `status.totalNodes` | Number of `ModelCacheNode` resources for local CSI mode |
| `status.readyNodes` | Number of nodes where the cache is ready |
| `status.downloadingNodes` | Number of nodes currently downloading |
| `status.pendingNodes` | Number of nodes pending storage/download preparation |
| `status.failedNodes` | Number of nodes where cache preparation failed |
| `status.conditions` | Kubernetes-style readiness conditions |

Supported phases:

| Phase | Description |
|-------|-------------|
| `Pending` | PVC or download preparation is still pending |
| `Downloading` | Downloader Job is running |
| `Ready` | Model files are available to workloads |
| `Failed` | PVC or downloader Job failed |

Current conditions:

| Condition | Description |
|-----------|-------------|
| `PVCReady` | The model storage is bound and usable |
| `DownloadComplete` | The downloader Job completed successfully |
| `Ready` | The model cache is ready to be mounted by workloads |

## Admission Webhooks

Praesto uses admission webhooks for two workflows.

### Mutating Pod webhook

The mutating webhook injects a ready model cache into annotated Pods.

Before creating annotated Pods, enable injection in the workload namespace:

```bash
kubectl label namespace <namespace> praesto.io/model-cache-injection=enabled
```

Namespaces without this label are ignored by the mutating webhook. This keeps unrelated workloads from depending on Praesto webhook availability.

Required annotation:

```yaml
praesto.io/model-cache: tinyllama-test
```

Optional annotations:

```yaml
praesto.io/model-mount-path: /models
praesto.io/target-container: app
```

If the mount path is omitted, Praesto uses `/models`.
If the target container is omitted, Praesto mounts the cache into the first container in the Pod spec. For multi-container Pods, set `praesto.io/target-container` explicitly.

The webhook:

- reads the requested `ModelCache` from the Pod namespace
- requires the `ModelCache` to be `Ready`
- injects a read-only volume:
  - CSI volume for local per-node caches (`spec.storage.storageClassName` empty)
  - PVC volume for the legacy RWX flow (`spec.storage.storageClassName` set)
- mounts it into the target container, or the first container when no target is configured

The webhook uses `failurePolicy: Fail` inside opt-in namespaces. If Praesto is unavailable, annotated Pods in enabled namespaces are rejected instead of running without their model cache.

### Validating ModelCache webhook

The validating webhook checks `ModelCache` input errors on create and update:

- `spec.storage.storageClassName`, when set, must reference an existing StorageClass
- `spec.storage.size` is required
- `spec.storage.size` must be a valid Kubernetes quantity greater than zero
- `spec.source.huggingface.repo` is required
- HuggingFace token `secretRef.name` and `secretRef.key` must be configured together
- `spec` is immutable after creation

## Configuration

Praesto is configured primarily through Kubernetes manifests, Helm values, and Pod annotations.

### Helm Values

Common chart values:

| Value | Description | Default |
|-------|-------------|---------|
| `image.repository` | Operator image repository | `ghcr.io/federicolepera/praesto` |
| `image.tag` | Operator image tag | Chart app version |
| `downloader.image.repository` | Default downloader image repository | `ghcr.io/federicolepera/praesto/downloader` |
| `downloader.image.tag` | Default downloader image tag | Chart app version |
| `csi.enabled` | Install the Praesto CSI node driver | `true` |
| `csi.driverName` | CSI driver name used by injected volumes | `csi.praesto.io` |
| `localCache.basePath` | Host path where node-local model caches live | `/var/praesto` |
| `csi.image.repository` | CSI node driver image repository | `ghcr.io/federicolepera/praesto/csi-node-driver` |
| `csi.image.tag` | CSI node driver image tag | `0.1.0` |
| `webhooks.enabled` | Install admission webhooks | `true` |
| `certManager.enabled` | Create cert-manager issuer/certificate resources | `true` |
| `metrics.enabled` | Expose metrics service and RBAC | `true` |

See [`charts/praesto/values.yaml`](charts/praesto/values.yaml) and [`charts/praesto/README.md`](charts/praesto/README.md) for all options.

### Pod Annotations

| Annotation | Required | Description |
|------------|----------|-------------|
| `praesto.io/model-cache` | Yes | Name of the `ModelCache` to mount |
| `praesto.io/model-mount-path` | No | Mount path inside the target container. Defaults to `/models` |
| `praesto.io/target-container` | No | Container name to mutate. Defaults to the first container |

### Downloader Settings

`ModelCache.spec.downloader` can customize the downloader Job, including image, resource requests/limits, and container security context.

Example:

```yaml
spec:
  downloader:
    image: ghcr.io/federicolepera/praesto/downloader:0.1.0
    resources:
      requests:
        cpu: 100m
        memory: 256Mi
      limits:
        memory: 1Gi
```

## Samples

Samples live under:

```text
config/samples/
```

Current quickstart samples:

| File | Purpose |
|------|---------|
| `config/samples/quickstart/00-modelcache-tinyllama.yaml` | Creates the TinyLlama `ModelCache` |
| `config/samples/quickstart/01-tokenizer-deployment.yaml` | Creates a lightweight tokenizer workload that uses the cache |

CSI/local-cache samples:

| File | Purpose |
|------|---------|
| `config/samples/presto.csi/modelCache.yaml` | Creates a local per-node TinyLlama `ModelCache` with CSI-style storage mode |
| `config/samples/presto.csi/webhookDeployment.yaml` | Creates an annotated Deployment that lets the mutating webhook inject the Praesto CSI volume |

For the CSI webhook sample, enable namespace opt-in first:

```bash
kubectl label namespace default praesto.io/model-cache-injection=enabled --overwrite
kubectl apply -f config/samples/presto.csi/modelCache.yaml
kubectl apply -f config/samples/presto.csi/webhookDeployment.yaml
```

Inspect the injected mount:

```bash
kubectl exec -it deploy/praesto-csi-webhook-test -- bash
ls -lah /model
findmnt /model
```

## Local Development

Install only the CRDs:

```bash
make install
```

Run the controller locally against your current kubeconfig:

```bash
make run
```

Common commands:

```bash
# Build the operator
make build

# Run tests
make test

# Run lint
make lint

# Generate manifests and CRDs
make manifests

# Generate deepcopy code
make generate

# Build an operator image
make docker-build IMG=ghcr.io/federicolepera/praesto:dev

# Deploy the controller to the current cluster
make deploy IMG=ghcr.io/federicolepera/praesto:dev

# Remove the controller from the current cluster
make undeploy
```

## Local Webhook Debugging

For webhook debugging, run the manager locally from your IDE/debugger and install webhook configurations that point directly to your local machine instead of the in-cluster webhook Service.

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

The local mutating webhook uses the same namespace opt-in as the cluster installation. Label any namespace you want to debug:

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
| Kubernetes | CRDs, Jobs, PVCs, and admission webhooks |
| cert-manager | Required by the default Helm/Kustomize webhook installation |
| Praesto CSI node driver | Required for local per-node CSI cache mounts |
| ReadWriteMany-capable StorageClass | Required only for the legacy PVC workflow |
| kubectl | Required for local install and debug workflows |
| helm | Required for Helm chart installation |
| OpenSSL | Required for local webhook certificate generation |

## Current Limitations

- Praesto is early-stage; local CSI mode is the primary direction while the legacy RWX PVC flow remains supported.
- The downloader flow is intentionally simple and may change.
- `ModelCache.spec` is immutable after creation. To change source, storage, or downloader settings, delete and recreate the `ModelCache`.
- Multi-container Pods should set `praesto.io/target-container`; otherwise the mutating webhook mounts the cache into the first container.
- The mutating webhook expects a ready `ModelCache` in the same namespace as the Pod.
- The mutating webhook only runs in namespaces labeled `praesto.io/model-cache-injection=enabled`.
- Scheduling-aware injection is still on the roadmap. For CSI/local caches, workloads must currently be scheduled on nodes where the requested `ModelCacheNode` is ready.
- For the legacy PVC flow, storage must be provided by a user-managed RWX-capable StorageClass. Praesto validates StorageClass existence, but RWX support is diagnosed from PVC pending status because it is provisioner-specific.

## Roadmap

Praesto's storage roadmap is centered on the CSI driver.

- **Scheduling-aware injection**: the mutating webhook should inspect ready `ModelCacheNode` resources and add compatible scheduling constraints so Pods land on nodes that already have the requested cache.
- **CSI StorageClass support**: the CSI driver should become usable through a Praesto StorageClass, not only inline CSI volumes. The goal is to let users request model-backed volumes with PVCs while Praesto handles local cache placement.
- **RWX-like model distribution without external RWX storage**: the long-term goal is to provide the practical experience people want from RWX model volumes without requiring a user-managed RWX StorageClass. Praesto would distribute/cache models per node and expose them through CSI.
- **Node agent integration**: node-local directory creation, permissions, cleanup, disk checks, and cache health should move into a dedicated node agent.
- **Cache lifecycle policies**: configurable retention, eviction, refresh, and cleanup for node-local model data.

## Contributing

Contributions are welcome. Please:

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run relevant checks (`make lint`, `make test`, Helm rendering when chart files change)
5. Submit a pull request with a clear description

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.
