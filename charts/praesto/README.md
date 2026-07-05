# Praesto Helm chart

This chart installs the Praesto operator, CRDs, RBAC, cert-manager webhook
certificate resources, services, admission webhooks, the CSI node driver, and the node-agent DaemonSet.

## Prerequisites

- Kubernetes cluster
- cert-manager installed when `webhooks.certManager.enabled=true` (default)
- For local per-node cache mode, prepare `localCache.basePath` on nodes that should host caches. The default is `/var/praesto`.
- Praesto CSI node driver and node-agent enabled for local per-node cache mode, or an RWX-capable StorageClass for shared PVC mode

The administrator prepares only the base path, for example:

```bash
sudo mkdir -p /var/praesto
sudo chmod 0775 /var/praesto
```

The Praesto node-agent creates per-cache directories below it, such as `/var/praesto/<namespace>/<modelcache>`.

## Install

```bash
helm install praesto ./charts/praesto --namespace praesto-system --create-namespace
```

Pin release images explicitly for v0.6.2:

```bash
helm install praesto ./charts/praesto \
  --namespace praesto-system \
  --create-namespace \
  --set image.tag=0.6.2 \
  --set downloader.image.tag=0.6.2 \
  --set csi.image.tag=0.6.2 \
  --set nodeAgent.image.tag=0.6.2
```

## Pod injection opt-in

The mutating Pod webhook only runs in namespaces labeled with:

```bash
kubectl label namespace <namespace> praesto.io/model-cache-injection=enabled
```

## Selecting cache nodes

To run the node-agent only on selected nodes, label those nodes and set `nodeAgent.nodeSelector`:

```bash
kubectl label node <node-name> praesto.io/cache-node=true
```

```yaml
nodeAgent:
  nodeSelector:
    praesto.io/cache-node: "true"
```

## Important values

| Value | Default | Description |
|-------|---------|-------------|
| `image.repository` | `ghcr.io/federicolepera/praesto` | Operator image repository |
| `image.tag` | `0.6.2` | Operator image tag |
| `downloader.image.repository` | `ghcr.io/federicolepera/praesto/downloader` | Default downloader image documented for users |
| `downloader.image.tag` | `0.6.2` | Default downloader image tag documented for users |
| `localCache.basePath` | `/var/praesto` | Host base path where Praesto stores node-local model caches |
| `csi.enabled` | `true` | Install the Praesto CSI node driver |
| `csi.driverName` | `csi.praesto.io` | CSI driver name used by injected volumes |
| `csi.image.repository` | `ghcr.io/federicolepera/praesto/csi-node-driver` | CSI node driver image repository |
| `csi.image.tag` | `0.6.2` | CSI node driver image tag |
| `nodeAgent.enabled` | `true` | Install the Praesto node-agent DaemonSet |
| `nodeAgent.image.repository` | `ghcr.io/federicolepera/praesto/node-agent` | Node-agent image repository |
| `nodeAgent.image.tag` | `0.6.2` | Node-agent image tag |

## ModelCache status columns

`kubectl get modelcache -A` shows a mode-neutral view:

```text
NAMESPACE      NAME                    PHASE   MODE   READY   TOTAL   PVC   DOWNLOAD JOB
praesto-ovms   ovms-distilbert-squad   Ready   Node   1       1
```

- `MODE`: `PVC` for shared PVC mode, `Node` for local CSI mode.
- `READY` / `TOTAL`: ready cache units over total cache units.
- `PVC` and `DOWNLOAD JOB`: shared PVC implementation details; empty in local CSI mode.
| `nodeAgent.nodeSelector` | `{}` | Node selector used to schedule the node-agent DaemonSet |
| `webhooks.enabled` | `true` | Install admission webhooks |
| `webhooks.certManager.enabled` | `true` | Use cert-manager CA injection and serving cert |
| `webhooks.mutating.namespaceSelector` | opt-in label | Namespace selector for Pod mutation |
| `metrics.enabled` | `true` | Expose HTTPS metrics service |
| `rbac.aggregateRoles.create` | `true` | Install helper admin/editor/viewer roles |

## CRD upgrades

Helm installs CRDs from `crds/` on first install but does not upgrade or delete
CRDs automatically. Apply updated CRDs manually before chart upgrades when the
`ModelCache` schema changes.
