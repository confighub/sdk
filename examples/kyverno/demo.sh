#!/usr/bin/env bash
# Copyright (C) ConfigHub, Inc.
# SPDX-License-Identifier: MIT
#
# End-to-end demo of the kyverno example worker.
#
# Unlike the kyverno-server example, this worker uses the kyverno CLI for
# offline validation — policies are passed as a function parameter, so Kyverno
# does not need to be deployed in the cluster.
#
# Prerequisites:
#   - kind, kubectl, cub, docker installed
#   - Authenticated with: cub auth login
#
# Usage:
#   cd ../..
#   bash examples/kyverno/demo.sh

set -euo pipefail

# --- Prerequisites -----------------------------------------------------------

for tool in kind kubectl cub docker; do
  if ! command -v "$tool" &>/dev/null; then
    echo "ERROR: $tool is required but not found in PATH"
    exit 1
  fi
done

# --- Configuration -----------------------------------------------------------

CLUSTER_NAME="kyverno-cli-demo-$(( RANDOM % 9000 + 1000 ))"
SPACE="kyverno-cli-demo-$(( RANDOM % 9000 + 1000 ))"
K8S_WORKER="k8s-worker"
K8S_TARGET="k8s-worker-kubernetes-yaml-cluster"
KYVERNO_WORKER="kyverno-cli-worker"
KYVERNO_WORKER_NAMESPACE="kyverno-cli-worker"
IMAGE_NAME="kyverno-cli-worker:demo"

echo "=== Kyverno CLI Demo ==="
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

# --- Build and load Docker image ---------------------------------------------

echo "--- Building Docker image ---"
docker build -f examples/kyverno/Dockerfile -t "$IMAGE_NAME" .

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

# --- Install kyverno CLI worker ----------------------------------------------

echo "--- Installing kyverno CLI worker ---"
cub worker install --space "$SPACE" \
  --unit kyverno-cli-worker-unit \
  --target "$K8S_TARGET" \
  -n "$KYVERNO_WORKER_NAMESPACE" \
  --image "$IMAGE_NAME" \
  --image-pull-policy Never \
  "$KYVERNO_WORKER"

cub unit apply --space "$SPACE" kyverno-cli-worker-unit

kubectl -n "$KYVERNO_WORKER_NAMESPACE" wait --for=create deployment/"$KYVERNO_WORKER" --timeout=120s

cub worker install --space "$SPACE" \
  --export-secret-only \
  -n "$KYVERNO_WORKER_NAMESPACE" \
  "$KYVERNO_WORKER" 2>/dev/null | kubectl apply -f -

echo "Waiting for kyverno-cli-worker deployment..."
kubectl -n "$KYVERNO_WORKER_NAMESPACE" rollout status deployment/"$KYVERNO_WORKER" --timeout=120s
echo "Kyverno CLI worker is ready."
echo ""

# --- Define policies ----------------------------------------------------------

REQUIRE_LABELS_POLICY='apiVersion: policies.kyverno.io/v1
kind: ValidatingPolicy
metadata:
  name: require-labels
spec:
  validationActions: [Deny]
  matchConstraints:
    resourceRules:
      - apiGroups: ["apps"]
        apiVersions: [v1]
        operations: [CREATE, UPDATE]
        resources: [deployments]
  validations:
    - expression: >
        has(object.metadata.labels) &&
        '"'"'app'"'"' in object.metadata.labels &&
        object.metadata.labels['"'"'app'"'"'] != '"'"''"'"'
      message: "The label '"'"'app'"'"' is required."'

DISALLOW_LATEST_TAG_VAP='apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: disallow-latest-tag
spec:
  matchConstraints:
    resourceRules:
      - apiGroups: ["apps"]
        apiVersions: [v1]
        operations: [CREATE, UPDATE]
        resources: [deployments]
  validations:
    - expression: >
        object.spec.template.spec.containers.all(c,
          !c.image.endsWith('"'"':latest'"'"')
        )
      message: "Using '"'"'latest'"'"' tag is not allowed."'

# --- Create test units -------------------------------------------------------

echo "--- Creating test unit: deploy-with-labels (should pass require-labels) ---"
cub unit create --space "$SPACE" deploy-with-labels - --toolchain Kubernetes/YAML <<'UNIT'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: good-deploy
  namespace: default
  labels:
    app: example
spec:
  replicas: 1
  selector:
    matchLabels:
      app: example
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
        - name: nginx
          image: nginx:1.21
UNIT
echo ""

echo "--- Creating test unit: deploy-without-labels (should fail require-labels) ---"
cub unit create --space "$SPACE" deploy-without-labels - --toolchain Kubernetes/YAML <<'UNIT'
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
          image: nginx:1.21
UNIT
echo ""

echo "--- Creating test unit: deploy-latest-tag (should fail disallow-latest-tag VAP) ---"
cub unit create --space "$SPACE" deploy-latest-tag - --toolchain Kubernetes/YAML <<'UNIT'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: latest-deploy
  namespace: default
  labels:
    app: test
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

# --- Run validation with Kyverno ValidatingPolicy ----------------------------

echo "=== Testing with Kyverno ValidatingPolicy ==="
echo ""

echo "--- Running vet-kyverno on deploy-with-labels (should pass) ---"
cub function do vet-kyverno "$REQUIRE_LABELS_POLICY" \
  --space "$SPACE" --unit deploy-with-labels --worker "$SPACE/$KYVERNO_WORKER"
echo ""

echo "--- Running vet-kyverno on deploy-without-labels (should fail) ---"
cub function do vet-kyverno "$REQUIRE_LABELS_POLICY" \
  --space "$SPACE" --unit deploy-without-labels --worker "$SPACE/$KYVERNO_WORKER"
echo ""

# --- Run validation with Kubernetes ValidatingAdmissionPolicy -----------------

echo "=== Testing with Kubernetes ValidatingAdmissionPolicy ==="
echo ""

echo "--- Running vet-kyverno on deploy-with-labels (should pass) ---"
cub function do vet-kyverno "$DISALLOW_LATEST_TAG_VAP" \
  --space "$SPACE" --unit deploy-with-labels --worker "$SPACE/$KYVERNO_WORKER"
echo ""

echo "--- Running vet-kyverno on deploy-latest-tag (should fail) ---"
cub function do vet-kyverno "$DISALLOW_LATEST_TAG_VAP" \
  --space "$SPACE" --unit deploy-latest-tag --worker "$SPACE/$KYVERNO_WORKER"
echo ""

# --- Run validation with mixed policies --------------------------------------

echo "=== Testing with mixed ValidatingPolicy + ValidatingAdmissionPolicy ==="
echo ""

MIXED_POLICIES="${REQUIRE_LABELS_POLICY}
---
${DISALLOW_LATEST_TAG_VAP}"

echo "--- Running vet-kyverno on deploy-latest-tag (should fail both) ---"
cub function do vet-kyverno "$MIXED_POLICIES" \
  --space "$SPACE" --unit deploy-latest-tag --worker "$SPACE/$KYVERNO_WORKER"
echo ""

echo "=== Demo complete ==="
