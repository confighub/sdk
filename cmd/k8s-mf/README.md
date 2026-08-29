# k8s-mf

`k8s-mf` inspects and repairs Kubernetes [server-side-apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/)
**managed fields**. Managed fields record which *field manager* owns each field
of a resource. Leftover or competing managers are the usual cause of apply
surprises — fields silently retained, deletions blocked, or apply conflicts —
especially after a `kubectl` "break glass" edit or when transitioning a resource
between tools (`kubectl apply`, ArgoCD, Flux, Sveltos).

The goal is to make apply operations less surprising and to debug and fix
managed-field problems.

## Build

```
make build-k8s-mf      # from the repo root; produces bin/k8s-mf
```

## Field-manager categories

Rather than reasoning about raw manager strings, `k8s-mf` classifies each manager
into a category via the shared [`mfclass`](../../k8sutil/mfclass) package — the
same package `cub k8s refresh` uses to decide which fields are cluster state
rather than authored configuration, so the tool mirrors what a refresh stores:

- **Applier** — whole-resource owners you manage config with: kubectl, ArgoCD,
  Flux, Sveltos, Helm, … Tools that deploy by shelling out to kubectl (Spinnaker,
  for one) are indistinguishable from kubectl itself here, because they do not
  set their own `--field-manager`.
- **AsyncController** — controllers that write out of band rather than as the
  config's owner: HPA/VPA, the built-in workload controllers, cert-manager,
  ingress controllers, the mesh control planes, and the API server's own
  built-in admission plugins (`kube-apiserver`).
- **Default fields** — present on the object but owned by no manager (API-server
  defaults). Computed, not a real manager.

There is no admission-controller category. A mutating dynamic admission
controller rewrites the object inside the API server's write path, and the API
server credits the resulting fields to whichever client made the call — a
sidecar injector's own name never reaches `managedFields`, so there is nothing
to classify.

## Commands

All read commands accept a live resource as `TYPE NAME` (`--kubeconfig`,
`--context`, `-n/--namespace`) **or** an object from a file with `-f` (use `-`
for stdin), e.g. the output of `kubectl get … -o yaml --show-managed-fields`.

### Schema awareness

When reading from a **live cluster**, `categories` and `values` fetch the
resource's OpenAPI (v3, group-version-scoped) and project field ownership
schema-aware: atomic fields (e.g. a Deployment's `spec.selector`) are treated as
whole-subtree ownership, and associative lists use their real keys. This makes
the DEFAULT-fields and `values` output exact.

Reading from a **file** (`-f`) has no cluster to fetch a schema from, so it falls
back to schemaless heuristics. These are close, but can't tell an atomic
`f:selector: {}` from a granular key-only ownership, so a few subfields of
atomic objects (notably `spec.selector.matchLabels`) may show up under DEFAULT
fields. Read from the live cluster for exact results.

### `categories` — who owns what

```
k8s-mf categories deployment my-app -n prod
k8s-mf categories deployment my-app --by-manager
kubectl get deploy my-app -o yaml --show-managed-fields | k8s-mf categories -f -
```

Groups owned fields by category, and highlights **co-owned** fields (owned by
more than one manager — likely apply conflicts) and **default** fields.

### `values` — what an applier sees

```
k8s-mf values deployment my-app                       # all appliers (default)
k8s-mf values deployment my-app --manager argocd-controller
k8s-mf values deployment my-app --category Applier --include-defaults
```

Projects the resource down to just the values owned by the selected
manager/category.

### `cleanup` — what a refresh would keep

```
k8s-mf cleanup namespace test-ns
kubectl get deploy my-app -o yaml --show-managed-fields | k8s-mf cleanup -f -
```

Runs the same `ExtraCleanupObjects` pass `cub k8s refresh` runs, and prints the
result: fields owned only by async controllers dropped, status / managedFields /
internal metadata removed, cluster-internal annotations and labels stripped, and
resource quantities normalized. Use it to see why an unexpected field is being
carried into configuration data.

### `takeover` — consolidate ownership (mutating)

```
k8s-mf takeover deployment my-app --manager flux --dry-run
k8s-mf takeover deployment my-app --manager argocd-controller --yes
```

Removes the managedFields entries of competing appliers so the keeper can own
the resource on its next apply (controllers are preserved). Prints the JSON
patch and asks for confirmation unless `--yes`; `--dry-run` only prints. Scope
with `--remove-manager` / `--remove-category`. Removing an entry makes its fields
*unowned* — the keeper claims (and may prune) them on its next apply; values are
not transferred.

### `conflicts` — predict apply conflicts (read-only)

```
k8s-mf conflicts -f deploy.yaml --manager kustomize-controller -n prod
k8s-mf conflicts -f deploy.yaml --manager argocd-controller -o json
```

Server-side dry-runs the apply as the given manager with force disabled and
reports, as data (not an error), which fields another manager owns and would
block the apply — each annotated with the owner and its category. Nothing is
written. This is the focused "what will I fight over, and who owns it?" check;
`dry-run-apply` is the broader "show me the merged result" command.

### `dry-run-apply` — preview an apply as a manager

```
k8s-mf dry-run-apply -f deploy.yaml --manager kustomize-controller
k8s-mf dry-run-apply -f deploy.yaml --manager argocd-controller --show-diff
k8s-mf dry-run-apply -f deploy.yaml --manager kustomize-controller --force --commit
```

Server-side applies the manifest as the given manager and shows the merged
result. By default it is a server dry-run (nothing is persisted); pass `--commit`
to actually apply. When the apply would conflict with fields owned by another
manager (and `--force` is not set), the conflicting fields and their owners are
listed — the answer to "what will I fight over if I apply this?".
