# Legacy PVC mode

Legacy PVC mode is the simpler compatibility workflow.

Use it when your cluster already provides a `ReadWriteMany` StorageClass and you want a fast smoke test of the operator flow.

## How it works

When `ModelCache.spec.storage.storageClassName` is set, Praesto:

1. creates one namespaced PVC;
2. runs one downloader Job into that PVC;
3. injects the PVC read-only into annotated workloads.

## Trade-off

This is simple, but it depends on external RWX storage and does not provide Praesto's per-node local cache behavior.

The primary Praesto direction is [local CSI mode](local-csi-mode.md).

## Try it

Use the quickstart samples:

```text
config/samples/quickstart/
```
