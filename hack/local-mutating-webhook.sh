#!/usr/bin/env bash

set -euo pipefail

ACTION="${1:-install}"
WEBHOOK_NAME="${WEBHOOK_NAME:-praesto-local-mutating-webhook}"
WEBHOOK_HOST="${WEBHOOK_HOST:-host.docker.internal}"
WEBHOOK_PORT="${WEBHOOK_PORT:-9443}"
WEBHOOK_PATH="${WEBHOOK_PATH:-/mutate-v1-pod}"
CERT_DIR="${WEBHOOK_CERT_DIR:-${TMPDIR:-/tmp}/k8s-webhook-server/serving-certs}"
TLS_CRT="${CERT_DIR}/tls.crt"
TLS_KEY="${CERT_DIR}/tls.key"

usage() {
  cat <<EOF
Usage: $0 install|delete|certs|manifest

Environment variables:
  WEBHOOK_NAME      default: ${WEBHOOK_NAME}
  WEBHOOK_HOST      default: ${WEBHOOK_HOST}
  WEBHOOK_PORT      default: ${WEBHOOK_PORT}
  WEBHOOK_PATH      default: ${WEBHOOK_PATH}
  WEBHOOK_CERT_DIR  default: ${CERT_DIR}

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

render_manifest() {
  if [[ ! -f "${TLS_CRT}" ]]; then
    echo "Certificate not found: ${TLS_CRT}" >&2
    echo "Run: $0 certs" >&2
    exit 1
  fi

  local ca_bundle
  ca_bundle="$(base64 < "${TLS_CRT}" | tr -d '\n')"

  cat <<EOF
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: ${WEBHOOK_NAME}
webhooks:
- name: mpod.praesto.io
  admissionReviewVersions:
  - v1
  sideEffects: None
  failurePolicy: Fail
  clientConfig:
    url: https://${WEBHOOK_HOST}:${WEBHOOK_PORT}${WEBHOOK_PATH}
    caBundle: ${ca_bundle}
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

case "${ACTION}" in
  install)
    generate_certs
    render_manifest | kubectl apply -f -
    ;;
  delete|uninstall)
    kubectl delete mutatingwebhookconfiguration "${WEBHOOK_NAME}" --ignore-not-found=true
    ;;
  certs)
    generate_certs
    ;;
  manifest)
    render_manifest
    ;;
  help|-h|--help)
    usage
    ;;
  *)
    usage >&2
    exit 1
    ;;
esac
