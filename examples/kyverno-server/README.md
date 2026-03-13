# Kyverno Server Validation Example

This example demonstrates a custom ConfigHub function that validates Kubernetes resources against [Kyverno](https://kyverno.io/) policies by sending [AdmissionReview](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/) requests to a running Kyverno server. Unlike the [CLI-based kyverno example](../kyverno/), this approach calls the Kyverno webhook directly, avoiding the overhead of spawning a CLI process for each invocation.

## How It Works

1. For each Kubernetes resource in the configuration data, the function converts it to JSON and wraps it in a Kubernetes `AdmissionReview` request.
2. The request is POSTed to the Kyverno webhook's `/validate` endpoint.
3. The Kyverno server evaluates the resource against all deployed policies and returns an `AdmissionResponse`.
4. The function aggregates results across all resources into a `ValidationResult` with details and failed attributes.

Policies are not passed as parameters — they must be deployed in the Kyverno cluster. This means the same policies used for admission control are also used for pre-deployment validation via ConfigHub.

## Prerequisites

- Kyverno deployed in a Kubernetes cluster with policies configured. See [Kyverno installation](https://kyverno.io/docs/installation/).
- Network access from the worker to the Kyverno webhook service.
- A running ConfigHub server (for the worker mode).

## Configuration

The function uses environment variables to connect to the Kyverno server:

| Variable | Required | Description |
|----------|----------|-------------|
| `KYVERNO_URL` | Yes | Base URL of the Kyverno webhook (e.g., `https://kyverno-svc.kyverno.svc:443`) |
| `KYVERNO_CA_CERT_PATH` | No | Path to a CA certificate file for TLS verification (for Kyverno's self-signed cert) |
| `KYVERNO_SKIP_TLS_VERIFY` | No | Set to `true` to skip TLS certificate verification (development only) |

## Quick Start

Build the example worker:

    go build

### Running locally with `cub worker run`

The simplest way to run the example is with `cub worker run`, which automatically creates the worker and sets up the environment:

    cub worker run --space $SPACE \
      --executable ./kyverno-server \
      -e "KYVERNO_URL=https://localhost:9443" \
      -e "KYVERNO_SKIP_TLS_VERIFY=true" \
      my-kyverno-server

This will create the worker if it doesn't exist, set the required environment variables (`CONFIGHUB_WORKER_ID`, `CONFIGHUB_WORKER_SECRET`, `CONFIGHUB_URL`), and start the executable.

### Running directly with environment variables

Alternatively, you can set up the environment manually:

    eval "$(cub worker get-envs --space $SPACE my-kyverno-server)"
    export KYVERNO_URL=https://kyverno-svc.kyverno.svc:443
    export KYVERNO_SKIP_TLS_VERIFY=true  # for development
    ./kyverno-server

### Installing in a Kubernetes cluster

To deploy the worker in a Kubernetes cluster, first build and push a container image:

    docker build -f Dockerfile -t my-registry/kyverno-server-worker:latest .
    docker push my-registry/kyverno-server-worker:latest

Then install using `cub worker install`:

    # Create the worker unit in ConfigHub
    cub worker install --space $SPACE \
      --unit kyverno-server-unit \
      --target $TARGET \
      -n kyverno-worker \
      --image my-registry/kyverno-server-worker:latest \
      -e "KYVERNO_URL=https://kyverno-svc.kyverno.svc:443" \
      -e "KYVERNO_SKIP_TLS_VERIFY=true" \
      my-kyverno-server

    # Apply the worker unit to the cluster
    cub unit apply --space $SPACE kyverno-server-unit

    # Wait for the namespace and deployment, then install the secret
    kubectl -n kyverno-worker wait --for=create deployment/my-kyverno-server --timeout=120s
    cub worker install --space $SPACE \
      --export-secret-only \
      -n kyverno-worker \
      my-kyverno-server 2>/dev/null | kubectl apply -f -

    # Wait for the worker to be ready
    kubectl -n kyverno-worker rollout status deployment/my-kyverno-server --timeout=120s

For a complete end-to-end demo, see [demo.sh](demo.sh).

The worker connects to ConfigHub and registers the `vet-kyverno-server` function.

### Accessing Kyverno from outside the cluster

If the worker runs outside the Kubernetes cluster, you can use `kubectl port-forward` to reach the Kyverno webhook:

    kubectl port-forward -n kyverno svc/kyverno-svc 9443:443

Then set:

    export KYVERNO_URL=https://localhost:9443
    export KYVERNO_SKIP_TLS_VERIFY=true

## Usage

The `vet-kyverno-server` function takes no parameters — it validates resources against all policies deployed in the Kyverno cluster.

    cub function do vet-kyverno-server --where "Slug='my-unit'" --worker "my-space/my-worker"

## Comparison with the CLI Example

| | `kyverno` (CLI) | `kyverno-server` |
|---|---|---|
| Policy source | Passed as function parameter | Deployed in Kyverno cluster |
| Kyverno dependency | CLI binary in PATH | Kyverno running in a cluster |
| Performance | Process spawn per invocation | HTTP request per resource |
| Policy management | Ad-hoc, per invocation | Centralized in cluster |

## Running Tests

Unit tests use a mock HTTP server and do not require a running Kyverno instance:

    go test -v ./...
