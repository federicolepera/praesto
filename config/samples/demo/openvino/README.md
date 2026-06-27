# Praesto OpenVINO Model Server demo

This demo downloads two small OpenVINO-ready Hugging Face models with Praesto and serves both from a single OpenVINO Model Server Pod.

Praesto injects two CSI mounts with `praesto.io/model-mounts`:

```text
/models/distilbert/1 -> ovms-distilbert-squad
/models/vit/1        -> ovms-vit-food101
```

OpenVINO Model Server then loads both models from `/models` using `config.json`.

## Run

```bash
kubectl apply -k config/samples/demo/openvino
kubectl wait -n praesto-ovms --for=condition=Ready modelcache/ovms-distilbert-squad --timeout=10m
kubectl wait -n praesto-ovms --for=condition=Ready modelcache/ovms-vit-food101 --timeout=10m
kubectl rollout status -n praesto-ovms deployment/openvino-model-server --timeout=5m
```

## Verify

```bash
kubectl logs -n praesto-ovms deploy/openvino-model-server
kubectl exec -n praesto-ovms deploy/openvino-model-server -- \
  sh -c 'curl -s http://127.0.0.1:8000/v1/models/distilbert/metadata'
kubectl exec -n praesto-ovms deploy/openvino-model-server -- \
  sh -c 'curl -s http://127.0.0.1:8000/v1/models/vit/metadata'
```

Both models should report metadata and appear as `AVAILABLE` in the OpenVINO Model Server logs.

## Cleanup

```bash
kubectl delete -k config/samples/demo/openvino --ignore-not-found
```
