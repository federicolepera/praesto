# Praesto OpenVINO Model Server demo

This is the main Praesto demo.

It shows the local CSI flow with a real model server: Praesto downloads two OpenVINO-ready Hugging Face models, mounts both into one Pod through CSI, and OpenVINO Model Server serves them from the `/models` directory.

The demo uses the new compact multi-mount annotation:

```yaml
praesto.io/model-mounts: |
  [
    {"modelCache":"ovms-distilbert-squad","mountPath":"/models/distilbert/1"},
    {"modelCache":"ovms-vit-food101","mountPath":"/models/vit/1"}
  ]
```

Inside the OpenVINO container, the mounted layout becomes:

```text
/models/
  distilbert/
    1/
      openvino_model.xml
      openvino_model.bin
  vit/
    1/
      openvino_model.xml
      openvino_model.bin
```

## Prerequisites

- Praesto installed with CSI enabled.
- Praesto node-agent enabled.
- The mutating webhook enabled.
- Nodes can pull public images.
- Nodes have enough free disk under `/var/praesto` or your configured `localCache.basePath`.

Prepare the base cache path on cache-capable nodes:

```bash
sudo mkdir -p /var/praesto
sudo chmod 0775 /var/praesto
```

## Run the demo

Apply the OpenVINO demo manifests:

```bash
kubectl apply -k config/samples/demo/openvino
```

Wait for both model caches:

```bash
kubectl wait -n praesto-ovms --for=condition=Ready modelcache/ovms-distilbert-squad --timeout=10m
kubectl wait -n praesto-ovms --for=condition=Ready modelcache/ovms-vit-food101 --timeout=10m
```

Wait for OpenVINO Model Server:

```bash
kubectl rollout status -n praesto-ovms deployment/openvino-model-server --timeout=5m
```

## Verify

Check that OpenVINO loaded both models:

```bash
kubectl logs -n praesto-ovms deploy/openvino-model-server
```

Expected log lines include:

```text
Loaded model distilbert; version: 1
Loaded model vit; version: 1
```

Query metadata through the REST API:

```bash
kubectl exec -n praesto-ovms deploy/openvino-model-server -- \
  sh -c 'curl -s http://127.0.0.1:8000/v1/models/distilbert/metadata'

kubectl exec -n praesto-ovms deploy/openvino-model-server -- \
  sh -c 'curl -s http://127.0.0.1:8000/v1/models/vit/metadata'
```

Both endpoints should return model metadata.

## Why this demo matters

The demo shows Praesto doing more than mounting one folder:

- multiple `ModelCache` resources are prepared independently;
- the webhook injects multiple CSI volumes into one Pod;
- each model lands in the layout expected by OpenVINO Model Server;
- the application container only sees `/models`, not Praesto's node-local cache path.

## Cleanup

```bash
kubectl delete -k config/samples/demo/openvino --ignore-not-found
```

The local PVs use `Retain`, so node files under `/var/praesto/praesto-ovms/` may remain and can be removed manually if desired.
