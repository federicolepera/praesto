# Praesto samples

Samples are organized by scenario.

## Quick start

Use the quick start manifests to validate the full v0.2.0 flow:

1. create a `ModelCache`
2. wait for Praesto to create the PVC and downloader Job
3. wait for the model cache to become `Ready`
4. create a small workload annotated for webhook injection
5. verify that the workload can read the model files from `/models`

```bash
kubectl apply -f config/samples/quickstart/00-modelcache-tinyllama.yaml
kubectl get modelcache tinyllama-test -w
kubectl label namespace default praesto.io/model-cache-injection=enabled
kubectl apply -f config/samples/quickstart/01-tokenizer-deployment.yaml
kubectl logs -l app=praesto-tokenizer-test -f
```

The namespace label opts the workload namespace into Praesto Pod injection. The
mutating webhook ignores namespaces without `praesto.io/model-cache-injection=enabled`.

The tokenizer deployment is intentionally lightweight for local clusters such as
minikube. It does not load the full model into memory; it only verifies that the
HuggingFace tokenizer can be loaded from the mounted model cache.

Cleanup:

```bash
kubectl delete -f config/samples/quickstart/01-tokenizer-deployment.yaml
kubectl delete -f config/samples/quickstart/00-modelcache-tinyllama.yaml
```

## StorageClass

The quick start uses:

```yaml
storageClassName: standard
```

Change it if your cluster uses another ReadWriteMany-capable StorageClass.
