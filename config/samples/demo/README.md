# Praesto LLM smoke demo

Manual demo: Praesto downloads a small-ish LLM on the node local cache, exposes it through the CSI driver, then a Kubernetes `Job` loads the mounted model from `/model` and runs one CPU inference.

This is intentionally **not** a CI test: the inference image and model are large enough that it is better as an operator demo.

## Prerequisites

- Praesto installed with CSI enabled.
- Praesto node-agent enabled.
- The mutating webhook enabled.
- Nodes can pull public images.
- Nodes have enough free disk under `/var/praesto`.
- The admin only needs to prepare the base path, for example `/var/praesto`, on selected nodes. The Praesto node-agent creates per-cache directories below it.

If you installed the chart from this repository, use `latest` images for the current CSI/local-cache code:

```bash
helm upgrade --install praesto ./charts/praesto \
  --namespace praesto-system \
  --create-namespace \
  --set image.tag=latest \
  --set image.pullPolicy=Always \
  --set downloader.image.tag=latest \
  --set nodeAgent.image.tag=latest \
  --set nodeAgent.image.pullPolicy=Always \
  --set csi.image.tag=latest \
  --set csi.image.pullPolicy=Always
```

## Run

Apply the namespace and ModelCache:

```bash
kubectl apply -k config/samples/demo
```

Wait for the model to be downloaded on the selected nodes:

```bash
kubectl wait -n praesto-demo --for=condition=Ready modelcache/smollm2-demo --timeout=45m
kubectl get modelcache -n praesto-demo smollm2-demo -o wide
kubectl get modelcachenode -l praesto.io/model-cache-namespace=praesto-demo,praesto.io/model-cache-name=smollm2-demo
```

Then launch the inference job:

```bash
kubectl apply -f config/samples/demo/20-inference-job.yaml
kubectl wait -n praesto-demo --for=condition=Complete job/smollm2-inference --timeout=20m
kubectl logs -n praesto-demo job/smollm2-inference
```

Success looks like:

```text
Mounted files:
...
Generated text:
...
PRAESTO_LLM_SMOKE_OK
```

The generated text does not need to be deterministic. The marker means the model was loaded from the Praesto CSI mount and inference completed.

## Optional: choose a specific node

For a multi-node cluster, you can constrain the cache to a node label by editing `10-modelcache.yaml`:

```yaml
spec:
  nodeSelector:
    kubernetes.io/hostname: <node-name>
```

If you do that, also add the same selector under `spec.template.spec.nodeSelector` in `20-inference-job.yaml` so the inference Pod lands on a node with the local cache.

## Cleanup

```bash
kubectl delete -f config/samples/demo/20-inference-job.yaml --ignore-not-found
kubectl delete -k config/samples/demo --ignore-not-found
```

The PV uses `Retain`, so node files under `/var/praesto/praesto-demo/smollm2-demo` may remain and can be removed manually if desired.
