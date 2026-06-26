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
- The node-agent prepares a local directory under `localCache.basePath`.
- A downloader Job fills the node-local cache.
- The CSI driver mounts the ready cache into annotated Pods.

Default local path:

```text
/var/praesto/<namespace>/<modelcache>
```

The administrator only prepares the base path, for example `/var/praesto`. Praesto creates the per-model directories below it.

## Legacy PVC mode

When `storageClassName` is set, Praesto uses the legacy shared PVC workflow.

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

This mode requires a StorageClass that supports `ReadWriteMany` if multiple Pods or nodes need to consume the cache.

For deeper details, see:

- [Local CSI mode](local-csi-mode.md)
- [Legacy PVC mode](legacy-pvc-mode.md)
