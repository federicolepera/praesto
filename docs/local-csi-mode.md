# Local CSI mode

Local CSI mode is Praesto's primary and most important workflow.

## What it gives you

- One local model cache per selected node.
- Model files stored under the configured node-local base path, for example `/var/praesto/<namespace>/<modelcache>`.
- A Praesto node-agent that creates per-cache directories.
- A downloader Job per node that fills the local cache once.
- A CSI node driver that mounts the ready cache into application Pods.
- Simple workload annotations instead of direct PVC wiring.

## Admin preparation

Prepare only the base path on nodes that should host caches:

```bash
sudo mkdir -p /var/praesto
sudo chmod 0775 /var/praesto
```

Praesto creates the per-model directories below that path.

## Try it

- Minimal CSI sample: [`../config/samples/presto.csi/`](../config/samples/presto.csi/)
- Real LLM inference demo: [Demo documentation](../config/samples/demo/README.md)
