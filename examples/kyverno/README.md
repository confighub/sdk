# Kyverno Policy Validation Example

This example demonstrates a custom ConfigHub function that validates Kubernetes resources against [Kyverno](https://kyverno.io/) policies. It uses the `kyverno` CLI for offline validation, avoiding heavy build dependencies.

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

Set up environment and run:

    export CONFIGHUB_WORKER_ID=...
    export CONFIGHUB_WORKER_SECRET=...
    export CONFIGHUB_URL=...
    ./kyverno

The worker connects to ConfigHub and registers the `vet-kyverno` function alongside the standard built-in functions.

## Usage

The `vet-kyverno` function takes a single parameter: a YAML document containing one or more Kyverno policies (ClusterPolicy or Policy resources).

    cub function do vet-kyverno '<policy-yaml>' --where "Slug='my-unit'" --worker "my-space/my-worker"

Policies from https://kyverno.io/policies/ can be used directly.

## How It Works

1. The function writes the policy YAML and resource YAML to temporary files.
2. It executes `kyverno apply <policy> --resource <resources> --detailed-results`.
3. It parses the CLI output to extract policy/rule failures and field paths.
4. It returns a `ValidationResult` with details and failed attributes.

## Running Tests

Unit tests (require `kyverno` CLI in PATH):

    go test -v ./...

Tests will skip automatically if the kyverno CLI is not found.
