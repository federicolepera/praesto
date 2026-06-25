# Praesto documentation

The [main README](../README.md) explains the project, installation, and the fastest quickstart.

Use this directory for deeper guides and examples.

## Guides

| Guide | Purpose |
|-------|---------|
| [Local CSI mode documentation](local-csi-mode.md) | Explains Praesto's primary mode: per-node model caches mounted through the CSI driver |
| [Legacy PVC mode documentation](legacy-pvc-mode.md) | Explains the simpler RWX PVC workflow used by the quickstart |
| [Demo documentation](../config/samples/demo/README.md) | End-to-end demo that downloads SmolLM2, mounts it via CSI, and runs real inference |

## Samples

| Path | Purpose |
|------|---------|
| [`config/samples/presto.csi/`](../config/samples/presto.csi/) | Minimal local CSI cache and annotated workload samples |
| [`config/samples/quickstart/`](../config/samples/quickstart/) | Legacy PVC quickstart samples |
| [Demo manifests](../config/samples/demo/) | Full LLM inference demo |
