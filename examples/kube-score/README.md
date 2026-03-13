# kube-score Validation Example

This example demonstrates a custom ConfigHub function that validates Kubernetes resources using [kube-score](https://kube-score.com/) best-practice checks. It uses the `kube-score` CLI for scoring, avoiding heavy build dependencies.

## Prerequisites

- The `kube-score` CLI must be installed and available in `PATH`. See [kube-score installation](https://github.com/zegl/kube-score#installation).
- A running ConfigHub server (for the worker mode).

## Quick Start

Build the kube-score CLI (if not already installed):

    # From a kube-score source checkout:
    go build -o /usr/local/bin/kube-score ./cmd/kube-score/

    # Or install from release:
    # See https://github.com/zegl/kube-score#installation

Build the example worker:

    go build

### Running locally with `cub worker run`

The simplest way to run the example is with `cub worker run`, which automatically creates the worker and sets up the environment:

    cub worker run --space $SPACE --executable ./kube-score my-kube-score-worker

This will create the worker if it doesn't exist, set the required environment variables (`CONFIGHUB_WORKER_ID`, `CONFIGHUB_WORKER_SECRET`, `CONFIGHUB_URL`), and start the executable. The `kube-score` CLI must be in PATH.

### Running directly with environment variables

Alternatively, you can set up the environment manually:

    eval "$(cub worker get-envs --space $SPACE my-kube-score-worker)"
    ./kube-score

### Installing in a Kubernetes cluster

To deploy the worker in a Kubernetes cluster, first build and push a container image:

    docker build -f Dockerfile -t my-registry/kube-score-worker:latest .
    docker push my-registry/kube-score-worker:latest

Then install using `cub worker install`:

    # Create the worker unit in ConfigHub
    cub worker install --space $SPACE \
      --unit kube-score-worker-unit \
      --target $TARGET \
      --image my-registry/kube-score-worker:latest \
      my-kube-score-worker

    # Apply the worker unit to the cluster
    cub unit apply --space $SPACE kube-score-worker-unit

    # Wait for the namespace and deployment, then install the secret
    kubectl -n confighub wait --for=create deployment/my-kube-score-worker --timeout=120s
    cub worker install --space $SPACE \
      --export-secret-only \
      -n confighub \
      my-kube-score-worker 2>/dev/null | kubectl apply -f -

    # Wait for the worker to be ready
    kubectl -n confighub rollout status deployment/my-kube-score-worker --timeout=120s

The worker connects to ConfigHub and registers the `vet-kube-score` function alongside the standard built-in functions.

## Usage

The `vet-kube-score` function takes a single parameter: a score threshold that determines when validation fails.

    cub function do vet-kube-score 'Critical' --where "Slug='my-unit'" --worker "my-space/my-worker"

Possible threshold values: `Critical`, `High`, `Medium`, `Low`. If any finding has a score at or above the threshold, validation fails.

## Score Mapping

kube-score grades are mapped to ConfigHub scores:

| kube-score Grade | ConfigHub Score |
|------------------|-----------------|
| CRITICAL (1)     | Critical        |
| WARNING (5)      | Medium          |
| AlmostOK (7)     | Low             |
| AllOK (10)       | None (ignored)  |

## How It Works

1. The function writes the resource YAML to a temporary file.
2. It executes `kube-score score --output-format json <resources>`.
3. It parses the JSON output, maps grades to scores, and resolves container paths to gaby dot notation.
4. It returns a `ValidationResult` with `MaxScore`, failed attributes (each with a score), and details.
5. If `MaxScore >= threshold`, `Passed` is false; otherwise true.

## Running Tests

Unit tests (require `kube-score` CLI in PATH):

    go test -v ./...

Tests will skip automatically if the kube-score CLI is not found. Several unit tests for helper functions run without the binary.
