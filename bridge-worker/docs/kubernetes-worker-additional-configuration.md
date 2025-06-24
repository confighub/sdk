# Kubernetes Worker Additional Configuration

Among regular configuration in the Kubernetes worker,
the `IN_CLUSTER_TARGET_NAME` Environment Variable is available.

## Purpose

The `IN_CLUSTER_TARGET_NAME` environment variable is used by the ConfigHub Kubernetes bridge worker to determine the name of the default target when the worker is running inside a Kubernetes cluster (i.e., when using Kubernetes' in-cluster configuration).

## How It Works

When the bridge worker detects that it is running inside a Kubernetes cluster (using `rest.InClusterConfig()`), it attempts to set the name of the available target as follows:

1. **If `IN_CLUSTER_TARGET_NAME` is set:**
   - The value of this environment variable is used as the name for the default target.
2. **If `IN_CLUSTER_TARGET_NAME` is not set:**
   - The worker falls back to using the slug provided via the Go Options pattern (i.e., the `Slug` field in the options passed to `Info`).
   - If neither is set, a generic name such as `in-cluster` may be used (see implementation for details).

## Example

Suppose you want the default target to appear as `my-cluster` in the ConfigHub UI or API. You would start the worker with:

```sh
export IN_CLUSTER_TARGET_NAME=my-cluster
./cub-worker-run ...
```

If you do not set this variable, the worker will use the slug passed to it (if any), or a generic fallback.

## Why This Exists

This mechanism allows operators to control the display name of the in-cluster target, which can be useful for distinguishing between multiple clusters or for clarity in multi-tenant environments.

## Related Behavior

- When running outside a cluster, the worker enumerates all available kubeconfig contexts and creates a target for each.
- When running inside a cluster, only a single target is created, and its name is determined as described above.
