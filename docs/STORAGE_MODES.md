# Storage modes

Praesto supports two storage modes. The mode is selected by `ModelCache.spec.storage.storageClassName`.

## Local CSI mode

This is the primary Praesto mode.

When `storageClassName` is empty or omitted, Praesto caches the model on selected Kubernetes nodes and exposes it through the Praesto CSI driver.

```yaml
apiVersion: praesto.praesto.io/v1alpha1
kind: ModelCache
metadata:
  name: smollm2-demo
spec:
  source:
    huggingface:
      repo: HuggingFaceTB/SmolLM2-135M-Instruct
      revision: main
  storage:
    size: 10Gi
```

In this mode:

- Praesto creates one `ModelCacheNode` per selected node.
- The node-agent prepares a local directory under `localCache.basePath` and downloads the model.
- The CSI driver mounts the ready cache into annotated Pods.
- `kubectl get modelcache` reports `MODE=Node`; `READY` and `TOTAL` count ready and selected nodes.

Default local path:

```text
/var/praesto/<namespace>/<modelcache>
```

The administrator only prepares the base path, for example `/var/praesto`. Praesto creates the per-model directories below it.

## Shared PVC mode

When `storageClassName` is set, Praesto uses the shared PVC workflow.

```yaml
apiVersion: praesto.praesto.io/v1alpha1
kind: ModelCache
metadata:
  name: tinyllama-test
spec:
  source:
    huggingface:
      repo: TinyLlama/TinyLlama-1.1B-Chat-v1.0
      revision: main
  storage:
    storageClassName: standard
    size: 10Gi
```

In this mode:

- Praesto creates one namespaced PVC.
- A downloader Job downloads the model into that PVC.
- The mutating webhook injects the PVC read-only into annotated Pods.
- `kubectl get modelcache` reports `MODE=PVC`; `READY` and `TOTAL` represent the single shared cache unit.

## Status columns

Both modes use the same high-level columns:

```text
NAMESPACE      NAME                    PHASE         MODE   READY   TOTAL   PVC                      DOWNLOAD JOB
default        tinyllama-test          Downloading   PVC    0       1       praesto-tinyllama-test   praesto-download-tinyllama-test
praesto-ovms   ovms-distilbert-squad   Ready         Node   1       1
```

- `PHASE` is the overall cache phase.
- `MODE` is the selected storage backend: `Node` for local CSI mode, `PVC` for shared PVC mode.
- `READY` and `TOTAL` summarize how many cache units are ready.
- `PVC` and `DOWNLOAD JOB` are populated only for shared PVC mode.

This mode requires a StorageClass that supports `ReadWriteMany` if multiple Pods or nodes need to consume the cache.

For deeper details, see:

- [Local CSI mode](local-csi-mode.md)
- [Shared PVC mode](shared-pvc-mode.md)
