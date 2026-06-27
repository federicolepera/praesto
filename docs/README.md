# Praesto documentation

The [main README](../README.md) is intentionally short. Use this directory for walkthroughs and deeper configuration notes.

## Guides

| Guide | Purpose |
|-------|---------|
| [Storage modes](STORAGE_MODES.md) | Short comparison between local CSI mode and legacy PVC mode |
| [Local CSI mode documentation](local-csi-mode.md) | Explains Praesto's primary mode: per-node model caches mounted through the CSI driver |
| [Legacy PVC mode documentation](legacy-pvc-mode.md) | Explains the simpler RWX PVC workflow used by the quickstart |
| [Demo documentation](DEMO.md) | OpenVINO Model Server demo with two models mounted through Praesto CSI |
| [Quickstart](QUICKSTART.md) | Short legacy PVC walkthrough |
| [Commented Helm values](helm/values.yaml) | Example values file with comments for common chart options |

## Samples

| Path | Purpose |
|------|---------|
| [`config/samples/presto.csi/`](../config/samples/presto.csi/) | Minimal local CSI cache and annotated workload samples |
| [`config/samples/quickstart/`](../config/samples/quickstart/) | Legacy PVC quickstart samples |
| [OpenVINO demo manifests](../config/samples/demo/openvino/) | Main demo: two OpenVINO-ready models mounted into one model server |
