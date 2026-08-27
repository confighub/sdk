# cub CLI Overview

`cub` is the command-line tool for using ConfigHub.

## Getting started

To get credentials:

```
cub auth login
```

And login using your browser.

To check whether you are authenticated, use:

```
cub auth status
```

This contacts the server to verify the access token and exits non-zero if it is missing or expired, in which case run `cub auth login` again. (`cub context get` reports a local token status but does not contact the server.)

To set the default space, where SPACE is set to the slug of a space you have access to within the organization you are logged into:

```
cub context set --space $SPACE
```

To get your current context:

```
cub context get
```

## General CLI Usage patterns

The `cub` CLI follows the pattern of:

```
cub <entity/area> <verb> [<flags>] [<arguments>]
```

For example:

```
cub unit create --space prod-eu deployment deployment.yaml
```

### Entities / areas (command groups)

The supported entities are:

- `organization`
- `organization-member`
- `user`
- `space`
- `unit`
- `unit-action`
- `unit-event`
- `revision`
- `mutation`
- `link`
- `target`
- `worker`
- `trigger`
- `invocation`
- `filter`
- `changeset`
- `tag`
- `attribute`

In general, the CLI identifies entities using names.

Other functional areas include:

- `auth`
- `completion`
- `context`
- `upgrade`
- `version`
- `function`
- `run`
- `helm`
- `k8s`
- `variant`

`cub --help` will list all of the supported entities/areas.

### Verbs

The standard entity verbs are:

- `create`
- `list`
- `get`
- `update`
- `delete`

Some entities, such as `user`, `revision`, `mutation`, `unit-action`, and `unit-event`, are readonly and only support `list` and `get`.

### Flags

There are also some common flags that affect the output, input, or operation.

#### Selection/filtering flags

- `--space`: Specify the slug of the space of the entity or functional area. Overrides the current context. Applies to all verbs, for entities/areas contained within spaces. A value of "\*" implies the operation should be performed over all accessible spaces; supported by unit list, function do, and function list.
- `--where`: The specified string is an expression for the purpose of filtering the list of entities returned or operated upon. The expression syntax was inspired by SQL. For syntax details, see [our documentation](https://docs.confighub.com/concepts/filters/). Applies to `list` and to all [bulk operations](https://docs.confighub.com/concepts/bulk-operations/).
- `--contains`: Free text search for entities containing the specified text. Searches across string fields (like Slug, DisplayName) and map fields (like Labels, Annotations). Case-insensitive matching. Can be combined with `--where` using AND logic. Example: `--contains backend` to find entities with "backend" in any searchable field. Applies to `list`.
- `--filter`: Use a saved Filter entity in `<space>/<filter>` syntax to filter the operation.

#### Display flags

- `--debug`: Print API calls. Applies to all verbs.
- `--quiet`: Do not print default output. Applies to all verbs.
- `--verbose`: Print details of the returned entity, additive with default output. Applies to `create` and `update`.
- `-o, --output <format>`: Select an alternative output format (kubectl-style). Accepts one of:
  - `json` — payload as indented JSON
  - `yaml` — payload as YAML
  - `name` — slugs only (space-resident entities print as `<space-slug>/<slug>`)
  - `wide` — full default columns (list commands)
  - `jq=<expr>` — apply `jq` expression to the payload
  - `yq=<expr>` — apply `yq` expression to the payload
  - `custom-columns=<spec>` — projection spec (list commands with dynamic columns)
  - `mutations` — colored diff of the resource mutations; works with or without `--dry-run`
- `--show <section>`: Function commands only (`do`, `exec`, `vet`, `get`, `set`). Selects which sub-payload of a FunctionInvocationsResponse is the subject. Valid values: `output`, `values`, `data`. `--show` and `-o` are orthogonal: `--show output -o json` renders the Outputs section as JSON.
- `-O, --output-file <path>`: Write raw payloads (such as `--show data` on function commands) to a file. Accepts `{space}`, `{unit}`, `{section}` placeholders for per-unit file paths. Parent directories are created as needed.
- `--no-headers`: Omit header lines on list output. Applies to `list`.
- `--columns <fields>`: Select columns to display on list commands that support dynamic columns. Comma-separated or repeated. Equivalent to `-o custom-columns=<spec>`.

The following flags are deprecated in favor of `-o` / `--show` / the new subcommands; they continue to work as aliases (using them prints a one-line migration hint):

| Deprecated                                                                       | Replacement                      |
| -------------------------------------------------------------------------------- | -------------------------------- | ----------------------------------- | ------------------------------------ | ---------------------------- |
| `--json`                                                                         | `-o json`                        |
| `--yaml`                                                                         | `-o yaml`                        |
| `--jq <expr>`                                                                    | `-o jq=<expr>`                   |
| `--yq <expr>`                                                                    | `-o yq=<expr>`                   |
| `--names`                                                                        | `-o name`                        |
| `--no-header` (singular)                                                         | `--no-headers`                   |
| `--display-mutations`                                                            | `-o mutations`                   |
| On `function do` / `exec` / `run`: `--output-only`                               | `--show output`                  |
| On `function do` / `exec` / `run`: `--output-json`                               | `--show output -o json`          |
| On `function do` / `exec` / `run`: `--output-jq <expr>`                          | `--show output -o jq=<expr>`     |
| On `function do` / `exec` / `run`: `--output-values-only`                        | `--show values`                  |
| On `function do` / `exec` / `run`: `--data-only`                                 | `--show data`                    |
| On `cub unit get`: `--data-only`                                                 | `cub unit data <unit>`           |
| On `cub revision get`: `--data-only`                                             | `cub revision data <unit> <rev>` |
| On `cub unit-action get`: `--data`, `--livedata`, `--livestate`, `--bridgestate` | `cub unit-action data            | livedata                            | livestate                            | bridgestate <unit> <action>` |
| On `cub unit livedata                                                            | livestate                        | bridgestate`: `-o <file>` (to file) | `-O <file>` / `--output-file <file>` |

#### Other common flags

- `--label`: Add a label or list of labels, comma-separated, using key=value syntax. Applies to `create` and `update`.
- `--filename`: Read the JSON or YAML entity body from a file, URL, or standard input. Applies to `create` and `update`.
- `--patch`: Use the PATCH API rather than PUT (Update). Applies to `update`.
- `--wait`: Wait for completion of asynchronous operations. Applies to unit and link create, update, apply, destroy, and refresh.

## Sample commands

### Spaces

Get the names of all spaces to which you have access within the organization you are logged into:

```
cub space list
```

Create a new space from JSON and show the resulting JSON:

```
cub space create -o json --from-stdin space-slug < spacemetadata.json
```

### Triggers

Create a trigger that validates that all Kubernetes Deployments have more than one replica:

```
cub trigger create --space $SPACE --verbose replicated Mutation "Kubernetes/YAML" vet-cel 'r.kind != "Deployment" || r.spec.replicas > 1'
```

Create a trigger to ensure that no placeholder values remain before you apply:

```
cub trigger create --space $SPACE complete Mutation "Kubernetes/YAML" vet-placeholders
```

Create a trigger to ensure that a unit has been reviewed and approved after any change by at least one person prior to apply:

```
cub trigger create --space $SPACE require-approval Mutation "Kubernetes/YAML" vet-approvedby 1
```

Create a trigger to ensure that all Kubernetes resources are annotated with unit metadata:

```
cub trigger create --space $SPACE annotate-resources Mutation "Kubernetes/YAML" ensure-context true
```

### Units

Create a unit from a configuration file and wait for triggers and resolve to execute asynchronously:

```
cub unit create --space $SPACE --verbose myunit config.yaml
```

Restore a prior revision:

```
cub unit update --space $SPACE --verbose myunit --restore 1
```

Clone a unit:

```
cub unit create --space $SPACE --verbose --from-stdin myvariant --upstream-unit sample-deployment --upstream-space sample-space < variantmetadata.json
```

Approve a unit:

```
cub unit approve --space $SPACE myunit
```

Apply a unit:

```
cub unit apply --space $SPACE myunit
```

### Variants

Clone a whole space and its units into a new downstream variant space in one step. The new space's
`Variant` label is set to the variant name; other labels are inherited from the upstream space, and
`--stage`, `--environment`, and `--region` can add or change the `Stage`, `Environment`, and
`Region` labels. The slug is derived from `--space-pattern` (here, the `Component` label prefix and
the `Variant` label suffix). The cloned units can be retargeted:

```
cub variant create test website-prod --space-pattern "template:{{.Labels.Component}}-{{.Labels.Variant}}" --target website-test/cluster
```

### Links

Link an application unit to a namespace unit:

```
cub link create --space $SPACE --verbose dep-to-ns mydeployment myns
```

### Where clauses

Find units with a specific label key and value:

```
cub unit list --space $SPACE --where "Labels.tier = 'Backend'"
```

Find all cloned units within a space:

```
cub unit list --space $SPACE --where 'UpstreamRevisionNum > 0'
```

Find unapplied units within a space:

```
cub unit list --space $SPACE --where 'LastReleasedRevisionNum = 0'
```

Find units with unapplied changes within a space:

```
cub unit list --space $SPACE --where 'HeadRevisionNum > LastReleasedRevisionNum'
```

Find units created after a specific time within a space:

```
cub unit list --space $SPACE --where "CreatedAt > '2025-02-18T23:16:34'"
```

Find units approved by a specific user by ID:

```
cub unit list --no-headers --space $SPACE --where "ApprovedBy ? 'c9369257-0d7b-40d0-9127-454d90f5dcf8'"
```

Find units that have been approved:

```
cub unit list --space $SPACE --where 'LEN(ApprovedBy) > 0'
```

Find units with apply gates:

```
cub unit list --space $SPACE --where 'LEN(ApplyGates) > 0'
```

Get all apply gates of units with a specific apply gate:

```
cub unit list --space $SPACE --where "ApplyGates.complete/vet-placeholders = true" -o jq='.[].ApplyGates'
```

Find units with names starting with "test":

```
cub unit list --space $SPACE --where "Slug LIKE 'test%'"
```

Find units with names containing "backend" (case-insensitive):

```
cub unit list --space $SPACE --where "Slug ILIKE '%backend%'"
```

Find units with names matching a regex pattern:

```
cub unit list --space $SPACE --where "Slug ~ '^app-[0-9]+$'"
```

Find units NOT starting with "temp":

```
cub unit list --space $SPACE --where "Slug !~~ 'temp%'"
```

Find units containing "backend" in any searchable field:

```
cub unit list --space $SPACE --contains "backend"
```

Combine text search with specific filtering:

```
cub unit list --space $SPACE --where "CreatedAt > '2025-01-01'" --contains "api"
```

Search for units with "prod" in labels or annotations:

```
cub unit list --space $SPACE --contains "prod"
```

### Functions

Functions are grouped into three verb-scoped commands that enforce the kind of
function they invoke. Each one validates its arguments against the cached
function signatures (refreshed at login and by `cub function list`):

- `cub function vet` — validating functions (Validating=true) only
- `cub function get` — non-mutating functions (Mutating=false; includes plain readonly and validating)
- `cub function set` — mutating functions (Mutating=true) only

`cub function do` (and `cub function exec`) remain available as a mixed escape
hatch that accepts any kind. Prefer a verb: it catches a wrong-kind call before
anything is written, and it scopes what a role or agent is permitted to run.

Each verb also supplies a missing prefix, so `cub function set replicas 3`
invokes `set-replicas`.

Set an image for the `nginx` container of a Kubernetes Deployment:

```
cub function set --space $SPACE --where "Slug = 'mydeployment'" --change-desc "Move nginx to mainline-otel" set-container-image nginx nginx:mainline-otel
```

Preview what would change without applying:

```
cub function set --space $SPACE --where "Slug = 'mydeployment'" --dry-run -o mutations set-container-image nginx nginx:1.25
```

Hold a value against the next merge from upstream, by recording the paths the
change writes as protected local overrides:

```
cub function set --space $SPACE --where "Slug = 'mydeployment'" --protect --change-desc "Prod owns its replica count" set-replicas 5
```

Get the image attribute using `get-yq`:

```
cub function get --space $SPACE --where "Slug = 'mydeployment'" --show output get-yq '.spec.template.spec.containers[0].image'
```

Get the replica counts of all units in a space:

```
cub function get --space $SPACE --show values get-replicas
```

Get the IDs of all units with more than one replica in a space:

```
cub function vet --space $SPACE where-filter apps/v1/Deployment 'spec.replicas > 1' --quiet --show output -o jq='. as $e | .Output[] | select(.Passed == true) | {space: $e.SpaceSlug, unit: $e.UnitSlug}'
```

### Kubernetes

`cub k8s get` and `cub k8s types` read the Kubernetes resources held in Units, naming
resource types the way kubectl does. They report configuration, not live cluster state.

```
# List resources of a type, in a space or across the fleet
cub k8s get deploy --space $SPACE
cub k8s get netpol --target prod-use2/prod-use2-oci
cub k8s get deploy,sts --space "*"

# Everything except CustomResourceDefinitions
cub k8s get all --space $SPACE

# Describe resources, like "kubectl describe"
cub k8s get deploy my-app --space $SPACE --show detail

# Print the stored YAML
cub k8s get cm my-config --space $SPACE --show data --quiet

# Which resource types exist, and how widely they are used
cub k8s types --space "*"
```

Resource-level conditions go in `--where-resource`, Unit-level ones in `--where`; the two
are independent and combine:

```
cub k8s get deploy --space "*" --where-resource "spec.replicas > 1" --where "Labels.Tier = 'Backend'"
```

`cub k8s source` and `cub k8s collect` are the other half of the group: they talk to a
Kubernetes cluster directly. The kubeconfig is loaded with the usual precedence
(`--kubeconfig` flag, then `KUBECONFIG`, then `~/.kube/config`), and `--kube-context`
selects the context.

```
# Trace a live resource back to its ConfigHub Unit
cub k8s source deployment my-app --namespace my-namespace

# Collect cluster facts (version, CRDs, storage/ingress classes) onto a Target
cub k8s collect --space $SPACE --kube-context kind-kind my-target

# Preview the facts without updating anything
cub k8s collect --kube-context kind-kind --dry-run
```

`cub k8s collect` stores the facts in the Target's `Facts` map under `Cluster.*` keys, which
can then be used in `where` queries and parameter expansion.

### Raw payload subcommands

For large decoded blobs on a unit, revision, or unit-action, use the dedicated
subcommands. They emit raw bytes (no `-o` formatting, by design):

- `cub unit data <unit>`, `cub unit livedata <unit>`, `cub unit livestate <unit>`, `cub unit bridgestate <unit>`
- `cub unit-action data|livedata|livestate|bridgestate <unit> <id-or-num>`
- `cub revision data <unit> <revision-num>`

Each supports `--filename` to write to a file instead of stdout.

## Plugins

The `cub` CLI supports third-party plugins. Plugins are standalone executables placed in `~/.confighub/plugins/` and are invoked as regular subcommands.

### Plugin formats

1. **Single executable**: `~/.confighub/plugins/<name>` — a single file (binary, shell script, etc.)
2. **Directory plugin**: `~/.confighub/plugins/<name>/main` — a directory containing a `main` entry point plus any supporting files

### Plugin resolution

`cub <name> [args...]` looks up the plugin by exact name in the plugin directory. All remaining arguments and flags are passed through to the plugin.

### Environment variables

Plugins receive context via environment variables:

- `CUB_PLUGIN=1` — signals the process was launched as a plugin
- `CUB_CONFIG` — config directory path (`~/.confighub`)
- `CUB_CONTEXT` — active context name
- `CUB_SERVER` — server URL from active context
- `CUB_TOKEN` — access token (if authenticated)
- `CUB_SPACE` — default space slug (if set)

### Managing plugins

List installed plugins:

```
cub plugin list
```

## Command help

Use `--help` with any of the subcommands for more details.
