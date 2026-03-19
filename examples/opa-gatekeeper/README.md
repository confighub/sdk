# OPA Gatekeeper Validation Example

This example demonstrates a custom ConfigHub function that validates Kubernetes resources against [OPA Gatekeeper](https://open-policy-agent.github.io/gatekeeper/) constraints by sending [AdmissionReview](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/) requests to a running Gatekeeper server.

## How It Works

1. For each Kubernetes resource in the configuration data, the function converts it to JSON and wraps it in a Kubernetes `AdmissionReview` request.
2. The request is POSTed to the Gatekeeper webhook endpoint.
3. Gatekeeper evaluates the resource against all deployed constraint templates and constraints, and returns an `AdmissionResponse`.
4. The function aggregates results across all resources into a `ValidationResult` with details and failed attributes.

Constraints are not passed as parameters — they must be deployed in the Gatekeeper cluster. This means the same constraints used for admission control are also used for pre-deployment validation via ConfigHub.

## Prerequisites

- OPA Gatekeeper deployed in a Kubernetes cluster with constraint templates and constraints configured. See [Gatekeeper installation](https://open-policy-agent.github.io/gatekeeper/website/docs/install/).
- Network access from the worker to the Gatekeeper webhook service.
- A running ConfigHub server (for the worker mode).

## Configuration

The function uses environment variables to connect to the Gatekeeper server:

| Variable                     | Required | Description                                                                         |
| ---------------------------- | -------- | ----------------------------------------------------------------------------------- |
| `GATEKEEPER_URL`             | Yes      | Base URL of the Gatekeeper webhook (e.g., `https://gatekeeper-webhook-service.gatekeeper-system.svc:443`) |
| `GATEKEEPER_CA_CERT_PATH`    | No       | Path to a CA certificate file for TLS verification (for Gatekeeper's self-signed cert) |
| `GATEKEEPER_SKIP_TLS_VERIFY` | No       | Set to `true` to skip TLS certificate verification (development only)               |

## Quick Start

### Installing in a Kubernetes cluster

The opa-gatekeeper function expects to run in a Kubernetes cluster so that it can call Gatekeeper's admission webhook.

To deploy the worker in a cluster, first build and push a container image:

    docker build -f Dockerfile -t my-registry/opa-gatekeeper-worker:latest .
    docker push my-registry/opa-gatekeeper-worker:latest

Then install using `cub worker install`:

    # Create the worker unit in ConfigHub
    cub worker install --space $SPACE \
      --unit gatekeeper-worker-unit \
      --target $TARGET \
      -n gatekeeper-worker \
      --image my-registry/opa-gatekeeper-worker:latest \
      -e "GATEKEEPER_URL=https://gatekeeper-webhook-service.gatekeeper-system.svc:443" \
      -e "GATEKEEPER_SKIP_TLS_VERIFY=true" \
      my-gatekeeper-worker

    # Apply the worker unit to the cluster
    cub unit apply --space $SPACE gatekeeper-worker-unit

    # Wait for the namespace and deployment, then install the secret
    kubectl -n gatekeeper-worker wait --for=create deployment/my-gatekeeper-worker --timeout=120s
    cub worker install --space $SPACE \
      --export-secret-only \
      -n gatekeeper-worker \
      my-gatekeeper-worker 2>/dev/null | kubectl apply -f -

    # Wait for the worker to be ready
    kubectl -n gatekeeper-worker rollout status deployment/my-gatekeeper-worker --timeout=120s

For a complete end-to-end demo using Kind, see [demo.sh](demo.sh).

The worker connects to ConfigHub and registers the `vet-opa-gatekeeper` function.

### Running out-of-cluster

You can also run the worker outside the cluster using `kubectl port-forward` to access the Gatekeeper webhook:

    # Port-forward the Gatekeeper webhook service
    kubectl -n gatekeeper-system port-forward svc/gatekeeper-webhook-service 8443:443 &

    # Set up ConfigHub worker environment
    eval "$(cub worker get-envs --space $SPACE my-gatekeeper-worker)"

    # Set Gatekeeper environment
    export GATEKEEPER_URL=https://localhost:8443
    export GATEKEEPER_SKIP_TLS_VERIFY=true

    # Run the worker
    ./opa-gatekeeper

Or use `cub worker run`:

    cub worker run --space $SPACE --executable ./opa-gatekeeper \
      -e "GATEKEEPER_URL=https://localhost:8443" \
      -e "GATEKEEPER_SKIP_TLS_VERIFY=true" \
      my-gatekeeper-worker

## Usage

The `vet-opa-gatekeeper` function takes no parameters — it validates resources against all constraints deployed in the Gatekeeper cluster.

    cub function do vet-opa-gatekeeper --where "Slug='my-unit'" --worker "my-space/my-worker"

## Running Tests

Unit tests use a mock HTTP server and do not require a running Gatekeeper instance:

    go test -v ./...
