# Praesto Helm chart

This chart installs the Praesto operator, CRDs, RBAC, cert-manager webhook
certificate resources, services, admission webhooks, and the CSI node driver.

## Prerequisites

- Kubernetes cluster
- cert-manager installed when `webhooks.certManager.enabled=true` (default)
- Praesto CSI node driver enabled for local per-node cache mode, or an RWX-capable StorageClass for the legacy PVC mode

## Install

```bash
helm install praesto ./charts/praesto --namespace praesto-system --create-namespace
```

Pin release images explicitly for v0.1.0:

```bash
helm install praesto ./charts/praesto \
  --namespace praesto-system \
  --create-namespace \
  --set image.tag=0.1.0 \
  --set downloader.image.tag=0.1.0
```

## Pod injection opt-in

The mutating Pod webhook only runs in namespaces labeled with:

```bash
kubectl label namespace <namespace> praesto.io/model-cache-injection=enabled
```

## Important values

| Value | Default | Description |
|-------|---------|-------------|
| `image.repository` | `ghcr.io/federicolepera/praesto` | Operator image repository |
| `image.tag` | `0.1.0` | Operator image tag |
| `downloader.image.repository` | `ghcr.io/federicolepera/praesto/downloader` | Default downloader image documented for users |
| `downloader.image.tag` | `0.1.0` | Default downloader image tag documented for users |
| `localCache.basePath` | `/var/praesto` | Host base path where Praesto stores node-local model caches |
| `csi.enabled` | `true` | Install the Praesto CSI node driver |
| `csi.driverName` | `csi.praesto.io` | CSI driver name used by injected volumes |
| `csi.image.repository` | `ghcr.io/federicolepera/praesto/csi-node-driver` | CSI node driver image repository |
| `csi.image.tag` | `0.1.0` | CSI node driver image tag |
| `webhooks.enabled` | `true` | Install admission webhooks |
| `webhooks.certManager.enabled` | `true` | Use cert-manager CA injection and serving cert |
| `webhooks.mutating.namespaceSelector` | opt-in label | Namespace selector for Pod mutation |
| `metrics.enabled` | `true` | Expose HTTPS metrics service |
| `rbac.aggregateRoles.create` | `true` | Install helper admin/editor/viewer roles |

## CRD upgrades

Helm installs CRDs from `crds/` on first install but does not upgrade or delete
CRDs automatically. Apply updated CRDs manually before chart upgrades when the
`ModelCache` schema changes.
