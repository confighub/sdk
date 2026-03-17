#!/usr/bin/env bash
# Copyright (C) ConfigHub, Inc.
# SPDX-License-Identifier: MIT
#
# End-to-end demo of the kyverno-server example worker.
#
# Prerequisites:
#   - kind, kubectl, cub, docker installed
#   - Authenticated with: cub auth login
#
# Usage:
#   cd ../..
#   bash examples/kyverno-server/demo.sh

set -euo pipefail

# --- Prerequisites -----------------------------------------------------------

for tool in kind kubectl cub docker; do
  if ! command -v "$tool" &>/dev/null; then
    echo "ERROR: $tool is required but not found in PATH"
    exit 1
  fi
done

# --- Configuration -----------------------------------------------------------

CLUSTER_NAME="kyverno-demo-$(( RANDOM % 9000 + 1000 ))"
SPACE="kyverno-demo-$(( RANDOM % 9000 + 1000 ))"
K8S_WORKER="k8s-worker"
K8S_TARGET="k8s-worker-kubernetes-yaml-cluster"
KYVERNO_WORKER="kyverno-worker"
KYVERNO_WORKER_NAMESPACE="kyverno-worker"
IMAGE_NAME="kyverno-server-worker:demo"

echo "=== Kyverno Server Demo ==="
echo "Cluster:  $CLUSTER_NAME"
echo "Space:    $SPACE"
echo ""

# --- Cleanup trap ------------------------------------------------------------

cleanup() {
  if [ "${NOCLEANUP:-}" = "1" ]; then
    echo ""
    echo "=== Skipping cleanup (NOCLEANUP=1) ==="
    echo "Cluster: $CLUSTER_NAME"
    echo "Space:   $SPACE"
    return
  fi
  echo ""
  echo "=== Cleaning up ==="
  kind delete cluster --name "$CLUSTER_NAME" 2>/dev/null || true
  echo "Done."
}
trap cleanup EXIT

# --- Create kind cluster -----------------------------------------------------

echo "--- Creating kind cluster ---"
kind create cluster --name "$CLUSTER_NAME"
kubectl cluster-info --context "kind-$CLUSTER_NAME"
echo ""

# --- Build and load Docker image (while cluster is fresh) --------------------

echo "--- Building Docker image ---"
docker build -f examples/kyverno-server/Dockerfile -t "$IMAGE_NAME" .

echo "--- Loading image into kind ---"
kind load docker-image "$IMAGE_NAME" --name "$CLUSTER_NAME"
echo ""

# --- Create ConfigHub space --------------------------------------------------

echo "--- Creating ConfigHub space ---"
cub space create "$SPACE"
echo ""

# --- Bootstrap standard Kubernetes worker ------------------------------------

echo "--- Bootstrapping standard Kubernetes worker ---"
cub worker install --space "$SPACE" \
  --export --include-secret \
  -t Kubernetes \
  "$K8S_WORKER" 2>/dev/null | kubectl apply -f -

echo "Waiting for k8s-worker deployment..."
kubectl -n confighub rollout status deployment/"$K8S_WORKER" --timeout=120s

echo "Waiting for target to be created by the server..."
cub target get --space "$SPACE" --wait --timeout 60s "$K8S_TARGET" &>/dev/null
echo "Target $K8S_TARGET is ready."
echo ""

# --- Install Kyverno via ConfigHub -------------------------------------------

echo "--- Installing Kyverno via cub unit create + apply ---"
cub unit create --space "$SPACE" kyverno-install \
  https://github.com/kyverno/kyverno/releases/latest/download/install.yaml \
  --toolchain Kubernetes/YAML \
  --target "$K8S_TARGET"
cub unit apply --space "$SPACE" kyverno-install --wait

echo "Waiting for Kyverno to be ready..."
kubectl -n kyverno rollout status deployment/kyverno-admission-controller --timeout=120s
kubectl -n kyverno rollout status deployment/kyverno-background-controller --timeout=60s
kubectl -n kyverno rollout status deployment/kyverno-cleanup-controller --timeout=60s
kubectl -n kyverno rollout status deployment/kyverno-reports-controller --timeout=60s
echo "Kyverno is ready."
echo ""

# --- Create Kyverno policy via ConfigHub -------------------------------------

echo "--- Creating Kyverno ValidatingPolicy (require-labels) ---"
cub unit create --space "$SPACE" require-labels-policy - \
  --toolchain Kubernetes/YAML \
  --target "$K8S_TARGET" <<'POLICY'
apiVersion: policies.kyverno.io/v1
kind: ValidatingPolicy
metadata:
  name: require-labels
spec:
  validationActions: [Deny]
  matchConstraints:
    resourceRules:
      - apiGroups: ['']
        apiVersions: [v1]
        operations: [CREATE, UPDATE]
        resources: [pods]
    namespaceSelector:
      matchExpressions:
        - key: kubernetes.io/metadata.name
          operator: NotIn
          values: [kyverno, kyverno-worker, confighub, kube-system]
  validations:
    - expression: >
        has(object.metadata.labels) &&
        'team' in object.metadata.labels &&
        object.metadata.labels['team'] != ''
      message: "The label 'team' is required."
POLICY
cub unit apply --space "$SPACE" require-labels-policy --wait
echo ""

echo "--- Creating Kyverno ValidatingPolicy (disallow-latest-tag) ---"
cub unit create --space "$SPACE" disallow-latest-tag-policy - \
  --toolchain Kubernetes/YAML \
  --target "$K8S_TARGET" <<'POLICY'
apiVersion: policies.kyverno.io/v1
kind: ValidatingPolicy
metadata:
  name: disallow-latest-tag
spec:
  validationActions: [Deny]
  matchConstraints:
    resourceRules:
      - apiGroups: ['apps']
        apiVersions: [v1]
        operations: [CREATE, UPDATE]
        resources: [deployments]
    namespaceSelector:
      matchExpressions:
        - key: kubernetes.io/metadata.name
          operator: NotIn
          values: [kyverno, kyverno-worker, confighub, kube-system]
  validations:
    - expression: >
        object.spec.template.spec.containers.all(c,
          !c.image.endsWith(':latest')
        )
      message: "Using 'latest' tag is not allowed."
POLICY
cub unit apply --space "$SPACE" disallow-latest-tag-policy --wait
echo ""

# --- Install kyverno-server worker ------------------------------------------

echo "--- Installing kyverno-server worker ---"
# Create the worker unit in ConfigHub (not exported to stdout)
cub worker install --space "$SPACE" \
  --unit kyverno-worker-unit \
  --target "$K8S_TARGET" \
  -n "$KYVERNO_WORKER_NAMESPACE" \
  --image "$IMAGE_NAME" \
  --image-pull-policy Never \
  -e "KYVERNO_URL=https://kyverno-svc.kyverno.svc:443" \
  -e "KYVERNO_SKIP_TLS_VERIFY=true" \
  "$KYVERNO_WORKER"

# Apply the worker unit to the cluster (creates namespace, deployment)
cub unit apply --space "$SPACE" kyverno-worker-unit

# Grant the worker permission to discover Kyverno webhook configurations
kubectl -n "$KYVERNO_WORKER_NAMESPACE" wait --for=create deployment/"$KYVERNO_WORKER" --timeout=120s
kubectl create clusterrole kyverno-webhook-reader \
  --verb=list --resource=validatingwebhookconfigurations.admissionregistration.k8s.io
kubectl create clusterrolebinding kyverno-worker-webhook-reader \
  --clusterrole=kyverno-webhook-reader \
  --group="system:serviceaccounts:$KYVERNO_WORKER_NAMESPACE"

# Apply the ConfigHub connection secret
cub worker install --space "$SPACE" \
  --export-secret-only \
  -n "$KYVERNO_WORKER_NAMESPACE" \
  "$KYVERNO_WORKER" 2>/dev/null | kubectl apply -f -

echo "Waiting for kyverno-worker deployment..."
kubectl -n "$KYVERNO_WORKER_NAMESPACE" rollout status deployment/"$KYVERNO_WORKER" --timeout=120s
echo "Kyverno worker is ready."
echo ""

# --- Create test units -------------------------------------------------------

echo "--- Creating test unit: pod-with-labels (should pass) ---"
cub unit create --space "$SPACE" pod-with-labels - --toolchain Kubernetes/YAML <<'UNIT'
apiVersion: v1
kind: Pod
metadata:
  name: good-pod
  namespace: default
  labels:
    team: platform
spec:
  containers:
    - name: nginx
      image: nginx:latest
UNIT
echo ""

echo "--- Creating test unit: pod-without-labels (should fail require-labels) ---"
cub unit create --space "$SPACE" pod-without-labels - --toolchain Kubernetes/YAML <<'UNIT'
apiVersion: v1
kind: Pod
metadata:
  name: bad-pod
  namespace: default
spec:
  containers:
    - name: nginx
      image: nginx:1.21
UNIT
echo ""

echo "--- Creating test unit: deploy-latest-tag (should fail disallow-latest-tag) ---"
cub unit create --space "$SPACE" deploy-latest-tag - --toolchain Kubernetes/YAML <<'UNIT'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: bad-deploy
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: test
  template:
    metadata:
      labels:
        app: test
    spec:
      containers:
        - name: nginx
          image: nginx:latest
UNIT
echo ""

# --- Wait for Kyverno webhook to be fully ready ------------------------------

echo "--- Waiting for Kyverno webhook endpoints ---"
kubectl -n kyverno get endpoints kyverno-svc
sleep 10

# --- Run validation ----------------------------------------------------------

echo "--- Running vet-kyverno-server on pod-with-labels (should pass) ---"
cub function do vet-kyverno-server --space "$SPACE" --unit pod-with-labels --worker "$SPACE/$KYVERNO_WORKER"
echo ""

echo "--- Running vet-kyverno-server on pod-without-labels (should fail) ---"
cub function do vet-kyverno-server --space "$SPACE" --unit pod-without-labels --worker "$SPACE/$KYVERNO_WORKER"
echo ""

echo "--- Running vet-kyverno-server on deploy-latest-tag (should fail) ---"
cub function do vet-kyverno-server --space "$SPACE" --unit deploy-latest-tag --worker "$SPACE/$KYVERNO_WORKER"
echo ""

echo "=== Demo complete ==="
