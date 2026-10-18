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

A context names a server, an organization and a user. A command says which space it means with `--space`, or by naming the entity as `<space>/<slug>`; lists and bulk operations span the organization unless `--space` narrows them.

To get your current context:

```
cub context get
```

### Shell completion

`cub completion zsh` (or `bash`, `fish`, `powershell`) prints a completion script; `cub completion zsh --help` says where to install it. With it loaded, the tab key completes commands, flags, and the entities a command names: spaces first, as `prod/`, then the entities in that space, as `prod/frontend`. With `--space` on the line, the slugs in that space complete directly. Completion asks the server, so it needs a logged-in context, and it gives up quietly after a few seconds if the server does not answer.

## General CLI Usage patterns

The `cub` CLI follows the pattern of:

```
cub <entity/area> <verb> [<flags>] [<arguments>]
```

For example:

```
cub unit create --space prod-eu deployment deployment.yaml
```

An entity that lives in a space is named by its slug, by `<space>/<slug>`, or by its UUID, and every command accepts all three wherever it takes a reference: positional arguments, and flags such as `--target`, `--unit`, `--filter` and `--with-unit`. A bare slug is looked up in the space `--space` names, and searched for across the organization when there is none, which is an error if more than one space holds it. `<space>/<slug>` names the space itself and wins over `--space`, so `cub unit get prod/frontend` reads the same unit whatever `--space` says. A UUID needs no space. Creating an entity always takes `--space`.

### Entities / areas (command groups)

The entities are:

- `organization`, `organization-member`, `user`, `group`, `oauthclient`
- `space`, `component`, `unit`, `revision`, `mutation`, `link`, `tag`
- `trigger`, `filter`, `view`, `invocation`, `attribute`
- `changeset`, `changeorder`, `changeworkflow`, `attestation`, `review-comment`
- `target`, `release`, `resource`
- `serviceaccount`, `channel`, `command`, `commandrun`
- `worker`, `unit-action`, `unit-event`, which are for external functions

Other functional areas include:

- `auth`, `context`, `completion`, `version`, and `upgrade`, which replaces cub with its latest release
- `function` and `run`, which invoke functions
- `variant`, which works on the variants of a Component as a whole
- `apply`, which creates and updates entities from YAML documents
- `k8s`, which reads the Kubernetes resources in Units and talks to clusters
- `cluster`, which wires a cluster to ConfigHub through Argo CD, Flux or Sveltos
- `plugin`, which installs and manages plugins such as `helm`
- `api`, for a request to an API endpoint no other command exposes

`cub --help` will list all of the supported entities/areas.

### Verbs

The standard entity verbs are:

- `create`, `list`, `get`, `update` and `delete`
- `explain`, which describes the entity's fields from the API's schema
- `rename`, `move` (to another space), `edit` (in an editor, as a YAML document) and `open` (in the web UI), where they apply

Some entities, such as `user`, `revision`, `mutation`, `unit-action`, and `unit-event`, are read-only apart from `user key`, and support `list`, `get` and `explain`, and `data` for revisions and unit actions.

### Flags

There are also some common flags that affect the output, input, or operation.

#### Selection/filtering flags

- `--space`: The space to operate in, by slug or UUID. Applies to all verbs, for entities/areas contained within spaces. Omitted, a list or bulk operation spans every accessible space and a single entity is named as `<space>/<slug>`, which overrides `--space`. A value of "\*" is accepted and means the same as omitting it.
- `--variant`: The same scope as `--space`, named as `<component>/<variant>`: the space naming that Component whose `Variant` label is the given one. `cub unit list --variant web/prod` lists the units of the web component's prod variant whatever its space is called. It cannot be combined with `--space`.
- `--resource`: On the function verbs, the one resource to operate on, as kubectl names it: `<kind>/<name>`, or `<kind>/<namespace>/<name>` when the name is held in several namespaces. `cub function set --variant web/base --resource Deployment/backend set-replicas 3` finds the unit holding that Deployment and runs on that resource alone. It is an error when it matches no resource or more than one.
- `--where`: The specified string is an expression for the purpose of filtering the list of entities returned or operated upon. The expression syntax was inspired by SQL. See [Where expressions](#where-expressions) below, and [filters](https://docs.confighub.com/background/concepts/filters/) in the documentation. Applies to `list` and to all [bulk operations](https://docs.confighub.com/background/concepts/bulk-operations/).
- `--contains`: Free text search for entities containing the specified text. Searches across string fields (like Slug, DisplayName) and map fields (like Labels, Annotations). Case-insensitive matching. Can be combined with `--where` using AND logic. Example: `--contains backend` to find entities with "backend" in any searchable field. Applies to `list`.
- `--filter`: Use a saved Filter entity in `<space>/<filter>` syntax to filter the operation.
- `--order-by`: Order the entities by the named fields, as comma-separated `ASC:Field`, `DESC:Field` or `Field` terms, such as `--order-by DESC:CreatedAt`. Entities with equal values are ordered by ID. Applies to `list`.
- `--limit`: Return at most this many entities. cub reads them from the server in pages of at most 1000, so the limit can be larger than that. With `--order-by`, it decides which entities are returned; without it, a list displayed newest first returns the newest. Applies to `list`.

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

| Deprecated                                                | Replacement                            |
| --------------------------------------------------------- | -------------------------------------- |
| `--json`                                                  | `-o json`                              |
| `--yaml`                                                  | `-o yaml`                              |
| `--jq <expr>`                                             | `-o jq=<expr>`                         |
| `--yq <expr>`                                             | `-o yq=<expr>`                         |
| `--names`                                                 | `-o name`                              |
| `--no-header` (singular)                                  | `--no-headers`                         |
| `--display-mutations`                                     | `-o mutations`                         |
| On `function do` / `exec` / `run`: `--output-only`        | `--show output`                        |
| On `function do` / `exec` / `run`: `--output-json`        | `--show output -o json`                |
| On `function do` / `exec` / `run`: `--output-jq <expr>`   | `--show output -o jq=<expr>`           |
| On `function do` / `exec` / `run`: `--output-values-only` | `--show values`                        |
| On `function do` / `exec` / `run`: `--data-only`          | `--show data`                          |
| On `cub unit get`: `--data-only`                          | `cub unit data <unit>`                 |
| On `cub revision get`: `--data-only`                      | `cub revision data <unit> <rev>`       |
| On `cub unit-action get`: `--data`                        | `cub unit-action data <unit> <action>` |

#### Other common flags

- `--label`: Add a label or list of labels, comma-separated, using key=value syntax. Applies to `create` and `update`.
- `--filename`: Read the JSON or YAML entity body from a file, URL, or standard input. Applies to `create` and `update`.
- `--patch`: Use the PATCH API rather than PUT (Update). Applies to `update`.
- `--dry-run`: Report what the command would write, and write nothing. The server makes the write, with every check it involves, and then rolls it back, so a dry run fails where the write would. Most commands that write take it: create and update, single and bulk, and commands such as `rename`, `move`, `apply`, `release publish`, `variant promote`, `variant upload` and the function verbs. Delete commands and a few others do not; each command's help lists its flags. A dry-run `unit create` with configuration tries only the create, since the configuration is written to the unit afterwards.
- `--wait`: Wait for asynchronous work a write starts, such as the Triggers that run after a change to a unit's configuration. The commands that start such work take it, most of them waiting by default; each one's help gives its default.

## Sample commands

### Spaces

Get the names of all spaces to which you have access within the organization you are logged into:

```
cub space list
```

Create a space as a variant of a Component, with the standard labels, and show the result as JSON:

```
cub space create web-prod --component web --variant prod --environment Prod -o json
```

### Triggers

Create a trigger that validates that all Kubernetes Deployments have more than one replica:

```
cub trigger create --space $SPACE --verbose replicated Mutation "Kubernetes/YAML" vet-cel 'r.kind != "Deployment" || r.spec.replicas > 1'
```

Create a trigger to ensure that no placeholder values remain before a unit is published:

```
cub trigger create --space $SPACE complete Mutation "Kubernetes/YAML" vet-placeholders
```

Create a trigger that checks every resource against its Kubernetes schema:

```
cub trigger create --space $SPACE schemas Mutation "Kubernetes/YAML" vet-schemas
```

### Units

Create a unit from a configuration file and wait for triggers and resolve to execute asynchronously:

```
cub unit create --space $SPACE --verbose myunit config.yaml
```

Restore a prior revision, previewing it first. A unit created since v0.2.16 begins with an empty revision 1, so its first configuration is revision 2; `cub revision list` shows which:

```
cub unit update --space $SPACE myunit --restore 2 --dry-run -o mutations
cub unit update --space $SPACE myunit --restore 2 --change-desc "Back to the first configuration"
```

Clone a unit into another space, downstream of the original:

```
cub unit create --space $SPACE myvariant --upstream-unit sample-deployment --upstream-space sample-space
```

Approve the units in a space, recording an Attestation that a change workflow can require:

```
cub variant approve $SPACE
```

Publish a Release of the space:

```
cub release publish $SPACE
```

### Variants

Clone a whole space and its units into a new downstream variant space in one step. The new space's
`Variant` label is set to the variant name; other labels are inherited from the upstream space, and
`--stage`, `--environment`, and `--region` can add or change the `Stage`, `Environment`, and
`Region` labels. The slug is derived from `--space-pattern` (here, the Component's slug as the prefix
and the `Variant` label suffix). The cloned units can be retargeted:

```
cub variant create test website/prod --space-pattern "template:{{.Component.Slug}}-{{.Labels.Variant}}" --target infra/test-cluster
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

Find units never released within a space:

```
cub unit list --space $SPACE --where 'LastReleasedRevisionNum = 0'
```

Find units with unreleased changes within a space:

```
cub unit list --space $SPACE --where 'HeadRevisionNum > LastReleasedRevisionNum AND TargetID IS NOT NULL'
```

Find units created after a specific time within a space:

```
cub unit list --space $SPACE --where "CreatedAt > '2025-02-18T23:16:34'"
```

Find units with validation errors:

```
cub unit list --space $SPACE --where 'LEN(ValidationErrors) > 0'
```

Get all validation errors of units with a specific one:

```
cub unit list --space $SPACE --where "ValidationErrors.complete/vet-placeholders = true" -o jq='.[].Unit.ValidationErrors'
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

`cub k8s source`, `cub k8s refresh`, and `cub k8s collect` are the other half of the group:
they talk to a Kubernetes cluster directly. The kubeconfig is loaded with the usual precedence
(`--kubeconfig` flag, then `KUBECONFIG`, then `~/.kube/config`), and `--kube-context`
selects the context.

```
# Trace a live resource back to its ConfigHub Unit
cub k8s source deployment my-app --namespace my-namespace

# Bring a resource's cluster-side changes back into its Unit
cub k8s refresh deployment my-app --namespace my-namespace --dry-run

# Collect cluster facts (version, CRDs, storage/ingress classes) onto a Target
cub k8s collect --space $SPACE --kube-context kind-kind my-target

# Preview the facts without updating anything
cub k8s collect --kube-context kind-kind --dry-run
```

`cub k8s refresh` finds the Unit from the ConfigHub annotations on the live resource, strips
everything that is cluster state rather than configuration, diffs what is left against the
Unit's `LastReleasedRevisionNum` — the revision last published in a Release — and patches
just that difference onto the Unit's head. Only the named resource is touched, and paths the
Unit protects as local overrides are reported as conflicts rather than overwritten.

`cub k8s collect` stores the facts in the Target's `Facts` map under `Cluster.*` keys, which
can then be used in `where` queries and parameter expansion.

### Raw payload subcommands

The configuration of a unit, a revision, or a unit-action is read with its own subcommand, which
prints the data as stored:

- `cub unit data <unit>`, which writes to a file with `-O <file>`
- `cub unit-action data <unit> <id-or-num>` and `cub revision data <unit> <revision-num>`, which write to a file with `--filename <file>`

## Where expressions

Filter results using simple relational expressions. The specified string is an expression for the purpose of filtering the list of entities returned. The expression syntax was inspired by SQL, but does not support full SQL syntax currently. It supports conjunctions using `AND` of relational expressions of the form _attribute_ _operator_ _attribute_or_literal_. The attribute names are case-sensitive and PascalCase, as in the JSON encoding. Supported attributes for each entity are allow-listed: `cub <entity> explain --where` lists them for an entity, with their types and how each is written (`Labels.<key>`, or `Space.<attribute>` for an attribute of a referenced entity), and `cub <entity> explain --where <attribute>` describes one, as in `cub unit explain --where UpstreamUnitID`. All entities that include the attributes support `CreatedAt`, `UpdatedAt`, `DisplayName`, `Slug`, and ID fields. `Labels` are supported, using a dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`. Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`. String pattern operators include `LIKE` and `~~` for pattern matching with `%` and `_` wildcards, `ILIKE` for case-insensitive pattern matching, and `NOT LIKE` and `!~~` for negated pattern matching. String regex operators include `~` for regex matching, `~*` for case-insensitive regex, and `!~`/`!~*` for regex not matching. Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`. UUIDs and boolean attributes support equality and inequality only. Pointer types support `IS NULL` and `IS NOT NULL` to check for presence. String literals are quoted with single quotes, such as `'string'`. UUID and time literals must be quoted as string literals, as in `'7c61626f-ddbe-41af-93f6-b69f4ab6d308'`. Time literals use the same form as when serialized as JSON, such as: `CreatedAt > '2025-02-18T23:16:34'`. Integer and boolean literals are also supported for attributes of those types. Arrays support the `?` operator to to match any element of the array, as in `FromLinkID ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`. Arrays can perform LEN() to check for length, as in `LEN(FromLinkID) > 0`. An attribute naming a list of other entities is filtered on their attributes with a `*` segment, as in `FromLink.*.Slug = 'upgrade-app'` or `Triggers.*.Slug LIKE 'validate-%'`, which holds when any element satisfies it; without the `*` such a reference is an error, since it names no single value to compare. Maps support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`. Maps also support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence, as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists). The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses, such as `Slug IN ('slugone', 'slugtwo')` or `Labels.Environment IN ('production', 'staging')`. An example conjunction is: `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`. There is no `OR` and there are no parentheses: use `IN (...)` for alternatives of one attribute, or run separate queries.

### Query Language Grammar

The `--where` flag accepts expressions in a simple query language. The formal EBNF grammar is:

```ebnf
(* ConfigHub Query Language EBNF Grammar *)

query_expression    ::= binary_expression ( required_whitespace 'AND' required_whitespace binary_expression )*

binary_expression   ::= left_operand whitespace operator ( whitespace right_operand )? ( whitespace truth_test )?

left_operand        ::= length_expression | map_access | entity_ref | attribute_name
right_operand       ::= in_list | entity_ref | attribute_name | literal

(* A reference to a related entity fetched alongside the one being filtered. The '*' form is *)
(* for a reference naming a list of them, and holds when any element satisfies the           *)
(* expression; it is not allowed as a right_operand.                                         *)
entity_ref          ::= entity_name '.' ( '*' '.' )? attribute_name
entity_name         ::= letter ( letter | digit )*

(* Note: IS NULL and IS NOT NULL operators do not require a right_operand *)
(* Truth-value tests are post-fix modifiers applied to the result of a comparison *)
truth_test          ::= 'IS' whitespace 'TRUE' | 'IS' whitespace 'FALSE' | 'IS' whitespace 'NOT' whitespace 'TRUE' | 'IS' whitespace 'NOT' whitespace 'FALSE'

length_expression   ::= 'LEN' '(' attribute_name ')'

map_access          ::= labels_access | validation_access
labels_access       ::= 'Labels' '.' label_key
validation_access   ::= ('ValidationErrors' | 'ValidationWarnings') '.' slug '/' function_name

operator            ::= '<=' | '>=' | '<' | '>' | '=' | '!=' | '?' | 'NOT' whitespace 'LIKE' | 'LIKE' | 'ILIKE' | '~~' | '!~~' | '~' | '~*' | '!~' | '!~*' | 'IN' | 'NOT' whitespace 'IN' | 'IS' whitespace 'NULL' | 'IS' whitespace 'NOT' whitespace 'NULL'

(* Note: truth_test modifiers above are separate from operators; they follow a complete binary_expression *)

in_list             ::= '(' whitespace? literal ( whitespace? ',' whitespace? literal )* whitespace? ')'

literal             ::= string_literal | integer_literal | boolean_literal

(* Lexical rules *)
attribute_name      ::= letter ( letter )*
label_key           ::= label_key_char ( label_key_mid_char* label_key_char )?
slug                ::= slug_char ( slug_mid_char* slug_char )?
function_name       ::= alnum ( function_name_char )*

string_literal      ::= "'" string_char* "'"
string_char         ::= [^'"\\] (* and not a control character *)
integer_literal     ::= digit ( digit )*
boolean_literal     ::= 'true' | 'false'

whitespace          ::= ( ' ' | '\t' )*
required_whitespace ::= ( ' ' | '\t' )+

(* Character classes *)
letter              ::= [A-Za-z]
digit               ::= [0-9]
alnum               ::= [A-Za-z0-9]

(* Label key: ^[A-Za-z0-9]([\-_\./A-Za-z0-9]*[A-Za-z0-9])? *)
label_key_char      ::= [A-Za-z0-9]
label_key_mid_char  ::= [A-Za-z0-9\-_\./]

(* Slug: ^[A-Za-z0-9]([\-_A-Za-z0-9]*[A-Za-z0-9])? *)
slug_char           ::= [A-Za-z0-9]
slug_mid_char       ::= [A-Za-z0-9\-_]

(* Function name: ^[A-Za-z0-9]([\-_A-Za-z0-9]{0,127})? *)
function_name_char  ::= [A-Za-z0-9\-_]
```

### Grammar Constraints

The following constraints apply but are not expressible in pure EBNF:

- **attribute_name**: 1-41 characters total
- **label_key**: max 128 characters, matches `^[A-Za-z0-9]([\-_\./A-Za-z0-9]*[A-Za-z0-9])?$`
- **slug**: max 128 characters, matches `^[A-Za-z0-9]([\-_A-Za-z0-9]*[A-Za-z0-9])?$`
- **function_name**: max 128 characters, matches `^[A-Za-z0-9]([\-_A-Za-z0-9]{0,127})?$`
- **string_char**: valid UTF-8, and no control characters (tab and newline included)
- **integer_literal**: max 10 digits total
- **whitespace**: max 256 characters total
- **Terms**: max 100 binary expressions
- **in_list**: max 1000 values, all of one kind (strings, integers, or booleans)
- **Regular expressions** (`~`, `~*`, `!~`, `!~*`): literal characters, `.`, `^`, `$`, `*`, `+`, `?`, bounds `{n}`, `{n,}` and `{n,m}` up to 255, `|`, `( )`, and bracket expressions with ranges. Not supported: anything beginning `(?`, lazy quantifiers, backslashes, and `[:class:]`; write `[.]` or `[{]` for a literal character. `.` matches a newline. Max 1024 characters, as for LIKE patterns.
- **Overall query length**: max 8192 characters

### Query Examples

```bash
# Simple attribute comparison
--where "Slug = 'myapp'"

# Time comparison
--where "CreatedAt > '2025-01-01T00:00:00'"

# Label access
--where "Labels.tier = 'Backend'"

# Array containment
--where "FromLinkID ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'"

# Array length
--where "LEN(FromLinkID) > 0"

# ValidationErrors map access
--where "ValidationErrors.low-cost/vet-cel = true"

# String pattern matching
--where "Slug LIKE 'app-%'"

# Case-insensitive pattern matching
--where "Slug ILIKE '%BACKEND%'"

# Regex matching
--where "Slug ~ '^app-[0-9]+$'"

# IN operator - match multiple values
--where "Slug IN ('slugone', 'slugtwo', 'slugthree')"

# NOT IN operator - exclude multiple values
--where "Labels.Environment NOT IN ('development', 'test')"

# IN with integers
--where "RevisionNum IN (1, 2, 3)"

# Check if a map key doesn't exist (IS NULL)
--where "Labels.Environment IS NULL"

# Check if a map key exists (IS NOT NULL)
--where "Labels.tier IS NOT NULL"

# Truth-value test: match where value equals OR is NULL (useful for nullable columns)
--where "MergeSourceID = '<uuid>' IS NOT FALSE"

# Truth-value test: match where comparison is false
--where "Slug = 'test' IS FALSE"

# Complex conjunction
--where "CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'"
```

### Configuration Data Query Grammar

The `--where-data` flag (on `cub unit list`, `cub unit tree`, the function verbs, `cub filter create` and `update`, and other commands that select units by their configuration) accepts expressions that filter based on configuration data content rather than entity metadata. This uses a different query language for traversing YAML/JSON configuration paths. The formal EBNF grammar is:

```ebnf
(* ConfigHub Where-Data Query Language EBNF Grammar *)

query_expression       ::= binary_expression ( required_whitespace 'AND' required_whitespace binary_expression )*

binary_expression      ::= path_expression whitespace operator whitespace literal

path_expression        ::= config_path | split_path
config_path            ::= path_segment ( '.' path_segment )*
split_path             ::= config_path '.|' simple_path

path_segment           ::= map_segment | bound_parameter_segment | index_segment |
                          wildcard_segment | associative_match_segment

simple_path            ::= simple_segment ( '.' simple_segment )*
simple_segment         ::= map_segment | bound_parameter_segment | index_segment

(* Path segment types *)
map_segment            ::= letter ( map_char | escaped_char )*
bound_parameter_segment ::= '@' map_segment ':' parameter_name
index_segment          ::= digit ( digit )*
wildcard_segment       ::= '*' wildcard_binding?
associative_match_segment ::= '?' map_segment parameter_binding? '=' associative_value

(* Wildcard and parameter bindings *)
wildcard_binding       ::= '?' map_segment parameter_binding? | '@:' parameter_name
parameter_binding      ::= ':' parameter_name
parameter_name         ::= letter ( param_char )*

(* Associative match value - anything except '.' *)
associative_value      ::= assoc_char ( assoc_char )*

operator               ::= '<=' | '>=' | '<' | '>' | '=' | '!=' | 'IN' | 'NOT' whitespace 'IN' | 'NOT' whitespace 'LIKE' | 'LIKE' | 'ILIKE' | '~~' | '!~~' | '~' | '~*' | '!~' | '!~*'

literal                ::= string_literal | integer_literal | boolean_literal | in_list
in_list                ::= '(' whitespace literal ( whitespace ',' whitespace literal )* whitespace ')'  (* after IN or NOT IN *)

string_literal         ::= "'" string_char* "'"
string_char            ::= [^'"\\] (* and not a control character *)
integer_literal        ::= digit ( digit )*
boolean_literal        ::= 'true' | 'false'

whitespace             ::= ( ' ' | '\t' )*
required_whitespace    ::= ( ' ' | '\t' )+

(* Character classes *)
letter                 ::= [A-Za-z]
digit                  ::= [0-9]
map_char               ::= [A-Za-z0-9/_\-]
escaped_char           ::= '~1' | '~2'  (* ~1 for '.', ~2 for '/' *)
param_char             ::= [A-Za-z0-9_\-]
assoc_char             ::= [^.]
```

### Configuration Data Grammar Constraints

The following constraints apply but are not expressible in pure EBNF:

- **map_segment**: max 128 characters total, starts with letter
- **parameter_name**: max 128 characters total, starts with letter
- **index_segment**: max 10 digits total
- **associative_value**: any characters except '.'
- **string_char**: any characters except quotes, backslashes and control characters
- **escaped_char**: `~1` represents '.', `~2` represents '/'
- **Regular expressions**: the same subset as `--where`
- **Overall query length**: max 8192 characters, and max 100 binary expressions

### Configuration Data Path Syntax

Configuration data paths are dot-separated and support several advanced features:

- **Basic paths**: `spec.replicas`, `metadata.name`
- **Array indices**: `spec.template.spec.containers.0.image`
- **Wildcards**: `spec.containers.*.image` (matches any container)
- **Associative matching**: `spec.template.spec.containers.?name:container-name=nginx.image` (find container named "nginx")
- **Split paths**: `spec.containers.*.|securityContext.runAsNonRoot` (check if any container has this security setting)
- **Escaped keys**: Use `~1` for `.` in map keys (e.g., `metadata.annotations.example~1com/annotation`)

### Configuration Data Query Examples

```bash
# Simple path comparison
--where-data "spec.replicas > 1"

# Array index access
--where-data "spec.template.spec.containers.0.image = 'nginx:latest'"

# Wildcard matching - any container with specific image
--where-data "spec.template.spec.containers.*.image = 'nginx:latest'"

# Associative matching - find specific container
--where-data "spec.template.spec.containers.?name:container-name=nginx.image = 'nginx:latest'"

# Split path - check if any container is missing security context
--where-data "spec.template.spec.containers.*.|securityContext.runAsNonRoot != true"

# Complex expression with AND
--where-data "spec.replicas > 1 AND metadata.labels.tier = 'frontend'"

# Check for existence (boolean values)
--where-data "spec.securityContext.runAsNonRoot = true"
```

## Plugins

The `cub` CLI supports third-party plugins. Plugins are standalone executables placed in `~/.confighub/plugins/` and are invoked as regular subcommands.

### Plugin formats

1. **Single executable**: `~/.confighub/plugins/<name>` — a single file (binary, shell script, etc.)
2. **Directory plugin**: `~/.confighub/plugins/<name>/main` — a directory containing a `main` entry point plus any supporting files

### Plugin resolution

`cub <name> [args...]` looks up the plugin by exact name in the plugin directory. cub's own global flags (`--context`, `--debug`) are cub's wherever they appear on the line, before or after the plugin name, exactly as for a built-in command; they select the context and debug mode the plugin inherits and are not passed on. Every other argument and flag is passed through to the plugin unchanged, and everything after a bare `--` is passed through even if it looks like one of cub's flags. A plugin therefore cannot define its own `--context` or `--debug`.

### Environment variables

Plugins receive context via environment variables:

- `CUB_PLUGIN=1` — signals the process was launched as a plugin
- `CUB_CONFIG` — config directory path (`~/.confighub`)
- `CUB_CONTEXT` — active context name
- `CUB_SERVER` — server URL from active context
- `CUB_TOKEN` — access token (if authenticated)

### Managing plugins

Install, list, upgrade and uninstall plugins:

```
cub plugin install confighub/cub-helm
cub plugin list
cub plugin upgrade <name>
cub plugin uninstall <name>
```

## Command help

Use `--help` with any of the subcommands for more details.
