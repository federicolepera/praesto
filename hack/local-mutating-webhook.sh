#!/usr/bin/env bash

set -euo pipefail

ACTION="${1:-install}"
MUTATING_WEBHOOK_NAME="${MUTATING_WEBHOOK_NAME:-${WEBHOOK_NAME:-praesto-local-mutating-webhook}}"
VALIDATING_WEBHOOK_NAME="${VALIDATING_WEBHOOK_NAME:-praesto-local-validating-webhook}"
WEBHOOK_HOST="${WEBHOOK_HOST:-host.docker.internal}"
WEBHOOK_PORT="${WEBHOOK_PORT:-9443}"
MUTATING_WEBHOOK_PATH="${MUTATING_WEBHOOK_PATH:-${WEBHOOK_PATH:-/mutate-v1-pod}}"
VALIDATING_WEBHOOK_PATH="${VALIDATING_WEBHOOK_PATH:-/validate-praesto-praesto-io-v1alpha1-modelcache}"
CERT_DIR="${WEBHOOK_CERT_DIR:-${TMPDIR:-/tmp}/k8s-webhook-server/serving-certs}"
TLS_CRT="${CERT_DIR}/tls.crt"
TLS_KEY="${CERT_DIR}/tls.key"

usage() {
  cat <<EOF
Usage: $0 certs|mutating|validating|install|delete|mutating-manifest|validating-manifest

Environment variables:
  MUTATING_WEBHOOK_NAME    default: ${MUTATING_WEBHOOK_NAME}
  VALIDATING_WEBHOOK_NAME  default: ${VALIDATING_WEBHOOK_NAME}
  WEBHOOK_HOST             default: ${WEBHOOK_HOST}
  WEBHOOK_PORT             default: ${WEBHOOK_PORT}
  MUTATING_WEBHOOK_PATH    default: ${MUTATING_WEBHOOK_PATH}
  VALIDATING_WEBHOOK_PATH  default: ${VALIDATING_WEBHOOK_PATH}
  WEBHOOK_CERT_DIR         default: ${CERT_DIR}

Run the manager/debugger with:
  --webhook-cert-path=${CERT_DIR}
EOF
}

generate_certs() {
  mkdir -p "${CERT_DIR}"

  openssl req -x509 -newkey rsa:2048 -nodes \
    -keyout "${TLS_KEY}" \
    -out "${TLS_CRT}" \
    -days 365 \
    -subj "/CN=${WEBHOOK_HOST}" \
    -addext "subjectAltName=DNS:${WEBHOOK_HOST},DNS:host.docker.internal,DNS:localhost,IP:127.0.0.1"

  echo "Generated local webhook certificates:"
  echo "  ${TLS_CRT}"
  echo "  ${TLS_KEY}"
  echo
  echo "Run the manager/debugger with:"
  echo "  --webhook-cert-path=${CERT_DIR}"
}

ca_bundle() {
  if [[ ! -f "${TLS_CRT}" ]]; then
    echo "Certificate not found: ${TLS_CRT}" >&2
    echo "Run: $0 certs" >&2
    exit 1
  fi

  base64 < "${TLS_CRT}" | tr -d '\n'
}

render_mutating_manifest() {
  local bundle
  bundle="$(ca_bundle)"

  cat <<EOF
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: ${MUTATING_WEBHOOK_NAME}
webhooks:
- name: mpod.praesto.io
  admissionReviewVersions:
  - v1
  sideEffects: None
  failurePolicy: Fail
  namespaceSelector:
    matchLabels:
      praesto.io/model-cache-injection: enabled
  clientConfig:
    url: https://${WEBHOOK_HOST}:${WEBHOOK_PORT}${MUTATING_WEBHOOK_PATH}
    caBundle: ${bundle}
  rules:
  - operations:
    - CREATE
    apiGroups:
    - ""
    apiVersions:
    - v1
    resources:
    - pods
EOF
}

render_validating_manifest() {
  local bundle
  bundle="$(ca_bundle)"

  cat <<EOF
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: ${VALIDATING_WEBHOOK_NAME}
webhooks:
- name: vmodelcache.praesto.io
  admissionReviewVersions:
  - v1
  sideEffects: None
  failurePolicy: Fail
  clientConfig:
    url: https://${WEBHOOK_HOST}:${WEBHOOK_PORT}${VALIDATING_WEBHOOK_PATH}
    caBundle: ${bundle}
  rules:
  - operations:
    - CREATE
    - UPDATE
    apiGroups:
    - praesto.praesto.io
    apiVersions:
    - v1alpha1
    resources:
    - modelcaches
EOF
}

case "${ACTION}" in
  install)
    generate_certs
    render_mutating_manifest | kubectl apply -f -
    render_validating_manifest | kubectl apply -f -
    ;;
  mutating)
    render_mutating_manifest | kubectl apply -f -
    ;;
  validating)
    render_validating_manifest | kubectl apply -f -
    ;;
  delete|uninstall)
    kubectl delete mutatingwebhookconfiguration "${MUTATING_WEBHOOK_NAME}" --ignore-not-found=true
    kubectl delete validatingwebhookconfiguration "${VALIDATING_WEBHOOK_NAME}" --ignore-not-found=true
    ;;
  certs)
    generate_certs
    ;;
  manifest|mutating-manifest)
    render_mutating_manifest
    ;;
  validating-manifest)
    render_validating_manifest
    ;;
  help|-h|--help)
    usage
    ;;
  *)
    usage >&2
    exit 1
    ;;
esac
