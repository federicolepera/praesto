# Praesto CSI LLM demo

This demo shows the Praesto local CSI flow with a real LLM workload.

Praesto downloads `HuggingFaceTB/SmolLM2-135M-Instruct` into the node-local cache, exposes it through the CSI driver, and a Kubernetes `Job` loads the model from `/model` to run one CPU inference.

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

Apply the namespace, `ModelCache`, and demo manifests:

```bash
kubectl apply -k config/samples/demo
```

Wait for the model cache:

```bash
kubectl wait -n praesto-demo --for=condition=Ready modelcache/smollm2-demo --timeout=45m
kubectl get modelcache -n praesto-demo smollm2-demo -o wide
kubectl get modelcachenode -l praesto.io/model-cache-namespace=praesto-demo,praesto.io/model-cache-name=smollm2-demo
```

Launch the inference Job:

```bash
kubectl apply -f config/samples/demo/20-inference-job.yaml
kubectl wait -n praesto-demo --for=condition=Complete job/smollm2-inference --timeout=20m
kubectl logs -n praesto-demo job/smollm2-inference
```

Success looks like:

```text
Mounted files:
...
PROMPT:
In una frase semplice, spiega a cosa serve una cache locale su un nodo Kubernetes.

ANSWER:
...
PRAESTO_LLM_SMOKE_OK
```

The generated text does not need to be deterministic. The marker means the model was loaded from the Praesto CSI mount and inference completed.

## Multi-node note

Scheduling-aware injection is still on the roadmap. On a multi-node cluster, make sure the inference Pod lands on a node where the `ModelCacheNode` is ready.

You can constrain the demo by editing `config/samples/demo/10-modelcache.yaml`:

```yaml
spec:
  nodeSelector:
    kubernetes.io/hostname: <node-name>
```

Then add the same selector to `config/samples/demo/20-inference-job.yaml` under `spec.template.spec.nodeSelector`.

## Cleanup

```bash
kubectl delete -f config/samples/demo/20-inference-job.yaml --ignore-not-found
kubectl delete -k config/samples/demo --ignore-not-found
```

The local PV uses `Retain`, so node files under `/var/praesto/praesto-demo/smollm2-demo` may remain and can be removed manually if desired.
