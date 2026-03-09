# Kubernetes Resource Type Metadata

This package contains per-resource-type metadata used by ConfigHub's Kubernetes functions. When adding support for new CRDs or custom resource types, update the relevant files described below.

## Files to update

### `immutable_fields.go`

Maps resource types to field paths that cannot be changed after creation (require delete + recreate). Sources of truth for immutability:

- **Built-in K8s types**: `pkg/apis/*/validation/validation.go` in the Kubernetes source
- **ACK CRDs**: `is_immutable: true` in `generator.yaml`, or `x-kubernetes-validations` rules with `self == oldSelf`
- **Other CRDs**: look for "immutable", "cannot be updated", or "cannot be changed" in CRD field descriptions, and for immutability checks in controller reconciliation code

### `merge_key_fields.go`

Maps resource types to strategic merge patch keys for array fields. These determine how list items are matched during merges (e.g., containers matched by `name`). Sources:

- **Built-in K8s types**: `x-kubernetes-patch-merge-key` in the Kubernetes OpenAPI/Swagger spec, or `patchMergeKey` struct tags in Go types
- **CRDs**: `x-kubernetes-list-map-keys` and `x-kubernetes-list-type: map` in the CRD schema

Pod-spec merge keys shared across workloads are in `PodSpecMergeKeyFields`; per-type prefixes are in `WorkloadMergeKeyFields`.

### `reference_fields.go`

Maps resource types to fields that reference other Kubernetes resources. This enables cross-resource dependency tracking. The `Target` field uses `group/version/Kind` format.

- **ACK CRDs**: references follow the `spec.<field>Ref.from.name` pattern (single) or `spec.<field>Refs.*.from.name` (list)
- **Other CRDs**: look for fields named `*Ref`, `*SecretName`, `*ServiceAccountName`, `*ConfigMapRef`, etc.

### `public/configkit/k8skit/cluster_resource_types.go`

Lists cluster-scoped (non-namespaced) resource types. Check the `scope` field in a CRD's spec. Most CRDs are `Namespaced`; only add types here if `scope: Cluster`.

## Resource type format

All resource types use `group/version/Kind` format throughout:

```
v1/Pod                                    # core API (no group)
apps/v1/Deployment                        # grouped API
eks.services.k8s.aws/v1alpha1/Cluster     # CRD
```

## Adding a new controller's types

1. Read the CRD YAML files and controller source code
2. Check `scope:` for cluster-scoped types and add to `cluster_resource_types.go`
3. Search for immutability markers and add to `immutable_fields.go`
4. Check for strategic merge keys and add to `merge_key_fields.go`
5. Identify cross-resource reference fields and add to `reference_fields.go`
6. Run `make build-funcexec && make test-public` to verify
