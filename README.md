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
  Kubernetes-native model cache operator and CSI node driver for mounting AI model artifacts into workloads.
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

## What is Praesto?

Praesto helps Kubernetes workloads use AI and LLM models without downloading the same files again and again.

You declare which model you need, and Praesto prepares it for your applications. Workloads receive the model as a normal mounted folder, so application containers do not need custom download logic.

The main focus is the **CSI driver**: Praesto can cache models on Kubernetes nodes and mount them into Pods as read-only volumes. From the application point of view, the model is simply available at a path like `/models` or `/model`.

Praesto also supports a legacy shared PVC mode, but the CSI-based local cache is the direction of the project.

## Installation

Install Praesto first, then create `ModelCache` resources and annotated workloads.

### Prerequisites

You need:

- a Kubernetes cluster
- `kubectl`
- `helm`
- cert-manager installed in the cluster when `webhooks.certManager.enabled=true` (default)
- for local CSI mode, node-local storage prepared as described below
- for legacy PVC mode, a StorageClass that supports `ReadWriteMany`

### Prepare node local cache storage

For local CSI mode, prepare the cache base path on every node where Praesto may cache models. This is typically a fast local SSD mount:

```text
/var/praesto
```

Example on each cache-capable node:

```bash
sudo mkdir -p /var/praesto
sudo chmod 0775 /var/praesto
```

If you use a different path, set it in Helm:

```yaml
localCache:
  basePath: /mnt/fast-ssd/praesto
```

Praesto expects the base path to already exist. The `praesto-node-agent` DaemonSet creates per-cache directories below it:

```text
<basePath>/<namespace>/<modelcache>
```

To run the node-agent only on selected nodes, label those nodes and configure `nodeAgent.nodeSelector`:

```bash
kubectl label node <node-name> praesto.io/cache-node=true
```

```yaml
nodeAgent:
  nodeSelector:
    praesto.io/cache-node: "true"
```

### Install with Helm

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
  --set image.tag=0.2.0 \
  --set downloader.image.tag=0.2.0 \
  --set csi.image.tag=0.2.0 \
  --set nodeAgent.image.tag=0.2.0
```

The chart can also be published and installed as an OCI Helm package from GHCR:

```bash
helm install praesto oci://ghcr.io/federicolepera/praesto/charts/praesto \
  --version 0.2.0 \
  --namespace praesto-system \
  --create-namespace
```

Wait for Praesto components:

```bash
kubectl get pods -n praesto-system
```

With local CSI mode enabled, you should see the controller manager, CSI node DaemonSet, and node-agent DaemonSet running in `praesto-system`.

For chart options, see the commented [Helm values example](docs/helm/values.yaml) and the [Chart documentation](charts/praesto/README.md).

## Storage modes

Praesto supports two storage modes:

- **Local CSI mode**: the primary mode. Praesto caches the model on selected nodes and mounts it into Pods through the CSI driver.
- **Legacy PVC mode**: compatibility mode. Praesto downloads the model into a shared RWX PVC and mounts that PVC into Pods.

See the [storage modes documentation](docs/STORAGE_MODES.md) for examples and details.

## Demo

Praesto includes a CSI-based LLM demo: it downloads a small model, mounts it through the CSI driver, and runs a CPU inference Job from the mounted path.

See the [demo documentation](docs/DEMO.md).

## Quickstart

For a short end-to-end walkthrough, see the [quickstart guide](docs/QUICKSTART.md).

## Admission Webhooks

Praesto uses admission webhooks for model validation and Pod volume injection.

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

If the mount path is omitted, Praesto uses `/models`. If the target container is omitted, Praesto mounts the cache into the first container in the Pod spec.

The webhook:

- reads the requested `ModelCache` from the Pod namespace
- requires the `ModelCache` to be `Ready`
- injects a read-only CSI volume for local CSI mode
- injects a read-only PVC volume for legacy PVC mode

The webhook uses `failurePolicy: Fail` inside opt-in namespaces. If Praesto is unavailable, annotated Pods in enabled namespaces are rejected instead of running without their model cache.

### Validating ModelCache webhook

The validating webhook checks common `ModelCache` input errors:

- `spec.storage.size` is required and must be greater than zero
- `spec.source.huggingface.repo` is required
- HuggingFace token `secretRef.name` and `secretRef.key` must be configured together
- `spec` is immutable after creation
- in legacy PVC mode, `spec.storage.storageClassName` must reference an existing StorageClass

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.
