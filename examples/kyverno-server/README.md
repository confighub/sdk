# Kyverno Server Validation Example

This example demonstrates a custom ConfigHub function that validates Kubernetes resources against [Kyverno](https://kyverno.io/) policies by sending [AdmissionReview](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/) requests to a running Kyverno server. Unlike the [CLI-based kyverno example](../kyverno/), this approach calls the Kyverno webhook directly, avoiding the overhead of spawning a CLI process for each invocation.

## How It Works

1. For each Kubernetes resource in the configuration data, the function converts it to JSON and wraps it in a Kubernetes `AdmissionReview` request.
2. The request is POSTed to the Kyverno kyverno-resource-validating-webhook-cfg webhook's endpoint.
3. The Kyverno server evaluates the resource against all deployed policies and returns an `AdmissionResponse`.
4. The function aggregates results across all resources into a `ValidationResult` with details and failed attributes.

Policies are not passed as parameters — they must be deployed in the Kyverno cluster. This means the same policies used for admission control are also used for pre-deployment validation via ConfigHub.

## Prerequisites

- Kyverno deployed in a Kubernetes cluster with policies configured. See [Kyverno installation](https://kyverno.io/docs/installation/).
- Network access from the worker to the Kyverno webhook service.
- A running ConfigHub server (for the worker mode).

## Configuration

The function uses environment variables to connect to the Kyverno server:

| Variable                  | Required | Description                                                                         |
| ------------------------- | -------- | ----------------------------------------------------------------------------------- |
| `KYVERNO_URL`             | Yes      | Base URL of the Kyverno webhook (e.g., `https://kyverno-svc.kyverno.svc:443`)       |
| `KYVERNO_CA_CERT_PATH`    | No       | Path to a CA certificate file for TLS verification (for Kyverno's self-signed cert) |
| `KYVERNO_SKIP_TLS_VERIFY` | No       | Set to `true` to skip TLS certificate verification (development only)               |

## Quick Start

### Installing in a Kubernetes cluster

The kyverno-server function expects to run in a Kubernetes cluster so that it can call Kyverno's admission webhook.

To deploy the worker in a cluster, first build and push a container image:

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

For a complete end-to-end demo using Kind, see [demo.sh](demo.sh).

The worker connects to ConfigHub and registers the `vet-kyverno-server` function.

### Running out-of-cluster

You can also run the worker outside the cluster using `kubectl port-forward` to access the Kyverno webhook:

    # Port-forward the Kyverno webhook service
    kubectl -n kyverno port-forward svc/kyverno-svc 8443:443 &

    # Set up ConfigHub worker environment
    eval "$(cub worker get-envs --space $SPACE my-kyverno-worker)"

    # Set Kyverno environment
    export KYVERNO_URL=https://localhost:8443
    export KYVERNO_SKIP_TLS_VERIFY=true

    # Run the worker
    ./kyverno-server

Or use `cub worker run`:

    cub worker run --space $SPACE --executable ./kyverno-server \
      -e "KYVERNO_URL=https://localhost:8443" \
      -e "KYVERNO_SKIP_TLS_VERIFY=true" \
      my-kyverno-worker

## Usage

The `vet-kyverno-server` function takes no parameters — it validates resources against all policies deployed in the Kyverno cluster.

    cub function do vet-kyverno-server --where "Slug='my-unit'" --worker "my-space/my-worker"

## Comparison with the CLI Example

|                    | `kyverno` (CLI)              | `kyverno-server`             |
| ------------------ | ---------------------------- | ---------------------------- |
| Policy source      | Passed as function parameter | Deployed in Kyverno cluster  |
| Kyverno dependency | CLI binary in PATH           | Kyverno running in a cluster |
| Performance        | Process spawn per invocation | HTTP request per resource    |
| Policy management  | Ad-hoc, per invocation       | Centralized in cluster       |

## Running Tests

Unit tests use a mock HTTP server and do not require a running Kyverno instance:

    go test -v ./...
