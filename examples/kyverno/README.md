# Kyverno Policy Validation Example

This example demonstrates a custom ConfigHub function that validates Kubernetes resources against [Kyverno](https://kyverno.io/) policies and Kubernetes [ValidatingAdmissionPolicy](https://kubernetes.io/docs/reference/access-authn-authz/validating-admission-policy/) resources. It uses the `kyverno` CLI for offline validation, avoiding heavy build dependencies.

## Prerequisites

- The `kyverno` CLI must be installed and available in `PATH`. See [Kyverno CLI installation](https://kyverno.io/docs/installation/#install-kyverno-cli).
- A running ConfigHub server (for the worker mode).

## Quick Start

Build the kyverno CLI (if not already installed):

    # From a kyverno source checkout:
    go build -o /usr/local/bin/kyverno ./cmd/cli/kubectl-kyverno/

    # Or install from release:
    # See https://kyverno.io/docs/installation/#install-kyverno-cli

Build the example worker:

    go build

### Running locally with `cub worker run`

The simplest way to run the example is with `cub worker run`, which automatically creates the worker and sets up the environment:

    cub worker run --space $SPACE --executable ./kyverno my-kyverno-worker

This will create the worker if it doesn't exist, set the required environment variables (`CONFIGHUB_WORKER_ID`, `CONFIGHUB_WORKER_SECRET`, `CONFIGHUB_URL`), and start the executable. The `kyverno` CLI must be in PATH.

### Running directly with environment variables

Alternatively, you can set up the environment manually:

    eval "$(cub worker get-envs --space $SPACE my-kyverno-worker)"
    ./kyverno

### Installing in a Kubernetes cluster

To deploy the worker in a Kubernetes cluster, first build and push a container image:

    docker build -f Dockerfile -t my-registry/kyverno-worker:latest .
    docker push my-registry/kyverno-worker:latest

Then install using `cub worker install`:

    # Create the worker unit in ConfigHub
    cub worker install --space $SPACE \
      --unit kyverno-worker-unit \
      --target $TARGET \
      --image my-registry/kyverno-worker:latest \
      my-kyverno-worker

    # Apply the worker unit to the cluster
    cub unit apply --space $SPACE kyverno-worker-unit

    # Wait for the namespace and deployment, then install the secret
    kubectl -n confighub wait --for=create deployment/my-kyverno-worker --timeout=120s
    cub worker install --space $SPACE \
      --export-secret-only \
      -n confighub \
      my-kyverno-worker 2>/dev/null | kubectl apply -f -

    # Wait for the worker to be ready
    kubectl -n confighub rollout status deployment/my-kyverno-worker --timeout=120s

The worker connects to ConfigHub and registers the `vet-kyverno` function alongside the standard built-in functions.

## Usage

The `vet-kyverno` function takes a single parameter: a YAML document containing one or more Kyverno policies (ValidatingPolicy, ClusterPolicy, or Policy resources) or Kubernetes [ValidatingAdmissionPolicy](https://kubernetes.io/docs/reference/access-authn-authz/validating-admission-policy/) resources.

    cub function do vet-kyverno '<policy-yaml>' --where "Slug='my-unit'" --worker "my-space/my-worker"

Policies from https://kyverno.io/policies/ can be used directly. Kubernetes native ValidatingAdmissionPolicy resources are also supported, allowing pre-deployment validation with the same CEL-based policies used for admission control.

## How It Works

1. The function writes the policy YAML and resource YAML to temporary files.
2. It executes `kyverno apply <policy> --resource <resources> --policy-report --output-format=json`.
3. It parses the JSON policy report to extract policy/rule failures and field paths (converted from JSON Pointer to dot notation).
4. It returns a `ValidationResult` with details, failed attributes, and paths where available.

## End-to-End Demo

For a complete end-to-end demo using Kind, see [demo.sh](demo.sh). Run from the `public/` directory:

    bash examples/kyverno/demo.sh

The demo creates a Kind cluster, deploys a kyverno CLI worker, and validates test resources against both Kyverno ValidatingPolicy and Kubernetes ValidatingAdmissionPolicy.

## Running Tests

Unit tests (require `kyverno` CLI in PATH):

    go test -v ./...

Tests will skip automatically if the kyverno CLI is not found.
