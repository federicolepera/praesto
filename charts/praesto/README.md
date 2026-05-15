# Praesto Helm chart

This chart installs the Praesto operator, CRD, RBAC, cert-manager webhook
certificate resources, services, and admission webhooks.

## Prerequisites

- Kubernetes cluster
- cert-manager installed when `webhooks.certManager.enabled=true` (default)
- RWX-capable StorageClass for `ModelCache` resources

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
| `webhooks.enabled` | `true` | Install admission webhooks |
| `webhooks.certManager.enabled` | `true` | Use cert-manager CA injection and serving cert |
| `webhooks.mutating.namespaceSelector` | opt-in label | Namespace selector for Pod mutation |
| `metrics.enabled` | `true` | Expose HTTPS metrics service |
| `rbac.aggregateRoles.create` | `true` | Install helper admin/editor/viewer roles |

## CRD upgrades

Helm installs CRDs from `crds/` on first install but does not upgrade or delete
CRDs automatically. Apply updated CRDs manually before chart upgrades when the
`ModelCache` schema changes.
