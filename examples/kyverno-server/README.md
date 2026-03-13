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

Set up environment and run:

    export CONFIGHUB_WORKER_ID=...
    export CONFIGHUB_WORKER_SECRET=...
    export CONFIGHUB_URL=...
    export KYVERNO_URL=https://kyverno-svc.kyverno.svc:443
    export KYVERNO_SKIP_TLS_VERIFY=true  # for development
    ./kyverno-server

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
