# cub CLI Overview

`cub` is the command-line tool for using ConfigHub.

## Getting started

To get credentials:

```
cub auth login
```

And login using your browser.

To set the default space, where SPACE is set to the slug of a space you have access to within the organization you are logged into:

```
cub context set --space $SPACE
```

To get your current context:

```
cub context get
```

To change the default confighub host (you probably won't ever need to do this), set the CONFIGHUB_URL environment variable prior to executing `cub auth login`.

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
- `--json`: Print formatted JSON of the response payload, suppressing default output. Applies to most commands.
- `--jq`: Print the result of applying the specified `jq` expression to the response payload, suppressing default output. Applies to most commands.
- `--yaml`: Print formatted YAML of the response payload, suppressing default output. Applies to most commands.
- `--yq`: Print the result of applying the specified `yq` expression to the response payload, suppressing default output. Applies to most commands.
- `--names`: Print only names, suppressing default output. Applies to `list`.
- `--no-header`: Omit the header line. Applies to `list`.

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
cub space create --json --from-stdin space-slug < spacemetadata.json
```

### Triggers

Create a trigger that validates that all Kubernetes Deployments have more than one replica:

```
cub trigger create --space $SPACE --verbose replicated Mutation "Kubernetes/YAML" vet-celexpr 'r.kind != "Deployment" || r.spec.replicas > 1'
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
cub unit list --space $SPACE --where 'LiveRevisionNum = 0'
```

Find units with unapplied changes within a space:

```
cub unit list --space $SPACE --where 'HeadRevisionNum > LiveRevisionNum'
```

Find units created after a specific time within a space:

```
cub unit list --space $SPACE --where "CreatedAt > '2025-02-18T23:16:34'"
```

Find units approved by a specific user by ID:

```
cub unit list --no-header --space $SPACE --where "ApprovedBy ? 'c9369257-0d7b-40d0-9127-454d90f5dcf8'"
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
cub unit list --space $SPACE --where "ApplyGates.complete/vet-placeholders = true" --jq '.[].ApplyGates'
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

Set an image for the `nginx` container of a Kubernetes Deployment:

```
cub function do --space $SPACE --where "Slug = 'mydeployment'" set-image nginx nginx:mainline-otel
```

Get the image attribute using `yq`:

```
cub function do --space $SPACE --where "Slug = 'mydeployment'" --output-only yq '.spec.template.spec.containers[0].image'
```

Get the replica counts of all units in a space that contain resources with replicas:

```
cub function do --space $SPACE get-replicas --output-values-only
```

Get the IDs of all units with more than one replica in a space:

```
cub function do --space $SPACE where-filter apps/v1/Deployment 'spec.replicas > 1' --quiet --output-jq '.[].Passed' --jq '.[].UnitID'
```

Get all resource types in all units within a space

```
cub function do --space $SPACE --quiet --output-jq '.[].ResourceType' get-resources
```

## Command help

Use `--help` with any of the subcommands for more details.
