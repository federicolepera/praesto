# Quickstart

This quickstart validates the shared RWX PVC flow:

```text
ModelCache → PVC → downloader Job → Ready status → annotated Deployment → mounted model files
```

For the primary CSI flow, use the [demo](DEMO.md).

## Prerequisites

- Praesto installed.
- A StorageClass that supports `ReadWriteMany`.
- Pod injection enabled in the workload namespace.

The sample uses:

```yaml
storageClassName: standard
```

If your cluster uses a different RWX StorageClass, edit:

```text
config/samples/quickstart/00-modelcache-tinyllama.yaml
```

## Create a ModelCache

Apply the TinyLlama `ModelCache`:

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

## Mount the ModelCache in a workload

Enable Praesto Pod injection in the workload namespace:

```bash
kubectl label namespace default praesto.io/model-cache-injection=enabled --overwrite
```

Apply the tokenizer test Deployment:

```bash
kubectl apply -f config/samples/quickstart/01-tokenizer-deployment.yaml
```

The Deployment uses these annotations:

```yaml
praesto.io/model-mounts: |
  [
    {"modelCache":"tinyllama-test","mountPath":"/models"}
  ]
```

The mutating webhook mounts the generated PVC read-only into the Pod.

## Verify

Check logs:

```bash
kubectl logs -l app=praesto-tokenizer-test -f
```

Expected output includes:

```text
Tokenizer loaded from /models
{'input_ids': [...], 'attention_mask': [...]}
```

Inspect the mounted files if needed:

```bash
kubectl exec -it deploy/praesto-tokenizer-test -- /bin/sh
ls -lah /models
```

## Cleanup

```bash
kubectl delete -f config/samples/quickstart/01-tokenizer-deployment.yaml
kubectl delete -f config/samples/quickstart/00-modelcache-tinyllama.yaml
```
