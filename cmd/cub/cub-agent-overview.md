# Agent instructions

This file provides guidance to AI agents when working with code in this repository.

## ConfigHub Overview

ConfigHub is a centralized, versioned database for software infrastructure, Kubernetes, and application configuration. ConfigHub replaces local file systems and git for storing configuration.

It implements a "Configuration as Data" approach where configuration is stored as structured data rather than code. Key concepts:

- **Config Units**: Core configuration units containing data in single formats (YAML, properties, etc.)
- **Functions**: Reusable operations that inspect, modify, or validate configuration data
- **Revisions**: Sequential versioning of config data with complete change history
- **Spaces**: Organizational contexts for collaboration and access control
- **Workers**: Processes that execute functions and interface with live infrastructure
- **Triggers**: Automatic function execution based on lifecycle events

## CLI Tool: `cub`

The `cub` CLI is the primary interface for interacting with ConfigHub. It follows the pattern:

```
cub <entity/area> <verb> [flags] [arguments]
```

### Authentication & Context

```bash
# Login to ConfigHub
cub auth login

# Check authentication status (contacts the server to verify the token)
cub auth status

# Set default space context
cub context set --space SPACE_SLUG

# Get current context (local only; does not contact the server)
cub context get
```

Before running commands that require authentication, verify the session with `cub auth status`.
It contacts the server and exits non-zero if the token is missing or expired. If it reports the
session is not authenticated, ask the user to run `cub auth login` — re-authentication is an
interactive browser sign-in that an agent cannot complete on the user's behalf. `cub context get`
shows a local "Token Status" but does not contact the server, so prefer `cub auth status` to
confirm the token is actually accepted.

### Common Flags

- `--space SPACE_SLUG`: Override default space context; specify `*` to indicate all spaces
- `-o, --output <format>`: Select output format (kubectl-style). Values: `json`, `yaml`, `name`, `wide`, `jq=<expr>`, `yq=<expr>`, `custom-columns=<spec>`, `mutations`. For space-resident entities, `-o name` prints `<space-slug>/<slug>` — the same identifier syntax accepted by other commands.
- `--show <section>`: Function commands only (`function do|exec|vet|get|set`). Selects which part of the response is the subject: `output`, `values`, `data`. Combine with `-o` to format the selected section, e.g. `--show output -o json`.
- `-O, --output-file <path>`: Write raw payload to a file. Accepts `{space}`, `{unit}`, `{section}` placeholders for per-unit file paths in bulk operations.
- `--no-headers`: Omit header rows on list commands.
- `--columns <fields>` or `-o custom-columns=<spec>`: Select columns on list commands that support dynamic columns.
- `--where "EXPRESSION"`: Filter results using simple relational expressions. The specified string is an expression for the purpose of filtering the list of entities returned. The expression syntax was inspired by SQL, but does not support full SQL syntax currently. It supports conjunctions using `AND` of relational expressions of the form _attribute_ _operator_ _attribute_or_literal_. The attribute names are case-sensitive and PascalCase, as in the JSON encoding. Supported attributes for each entity are allow-listed, and documented in swagger. All entities that include the attributes support `CreatedAt`, `UpdatedAt`, `DisplayName`, `Slug`, and ID fields. `Labels` are supported, using a dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`. Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`. String pattern operators include `LIKE` and `~~` for pattern matching with `%` and `_` wildcards, `ILIKE` for case-insensitive pattern matching, and `NOT LIKE` and `!~~` for negated pattern matching. String regex operators include `~` for regex matching, `~*` for case-insensitive regex, and `!~`/`!~*` for regex not matching. Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`. UUIDs and boolean attributes support equality and inequality only. Pointer types support `IS NULL` and `IS NOT NULL` to check for presence. String literals are quoted with single quotes, such as `'string'`. UUID and time literals must be quoted as string literals, as in `'7c61626f-ddbe-41af-93f6-b69f4ab6d308'`. Time literals use the same form as when serialized as JSON, such as: `CreatedAt > '2025-02-18T23:16:34'`. Integer and boolean literals are also supported for attributes of those types. Arrays support the `?` operator to to match any element of the array, as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`. Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`. An attribute naming a list of other entities is filtered on their attributes with a `*` segment, as in `FromLink.*.Slug = 'upgrade-app'` or `Triggers.*.Slug LIKE 'validate-%'`, which holds when any element satisfies it; without the `*` such a reference is an error, since it names no single value to compare. Maps support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`. Maps also support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence, as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists). The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses, such as `Slug IN ('slugone', 'slugtwo')` or `Labels.Environment IN ('production', 'staging')`. An example conjunction is: `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`. See the [Query Language Grammar](#query-language-grammar) section for the formal syntax specification.
- `--from-stdin`: Read JSON input from stdin for passing to the ConfigHub API
- `--verbose`: Show detailed output, additive with default output
- `--debug`: Show API calls

Deprecated flags retained as aliases (using them prints a one-line migration hint; see the mapping in `cub-overview.md`): `--json`, `--yaml`, `--jq`, `--yq`, `--names`, `--no-header` (singular), `--display-mutations`, `--output-only`, `--output-json`, `--output-jq`, `--output-values-only`, `--data-only`, and the additive `--data` / `--livedata` / `--livestate` / `--bridgestate` flags on `unit-action get`. Prefer `-o`, `--show`, and the dedicated subcommands instead.

#### Query Language Grammar

The `--where` flag accepts expressions in a simple query language. The formal EBNF grammar is:

```ebnf
(* ConfigHub Query Language EBNF Grammar *)

query_expression    ::= binary_expression ( whitespace 'AND' whitespace binary_expression )*

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

map_access          ::= labels_access | apply_gates_access
labels_access       ::= 'Labels' '.' label_key
apply_gates_access  ::= 'ApplyGates' '.' slug '/' function_name

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
string_char         ::= [^'"\\]
integer_literal     ::= digit ( digit )*
boolean_literal     ::= 'true' | 'false'

whitespace          ::= ( ' ' | '\t' )*

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

#### Grammar Constraints

The following constraints apply but are not expressible in pure EBNF:

- **attribute_name**: 1-41 characters total
- **label_key**: max 128 characters, matches `^[A-Za-z0-9]([\-_\./A-Za-z0-9]*[A-Za-z0-9])?$`
- **slug**: max 128 characters, matches `^[A-Za-z0-9]([\-_A-Za-z0-9]*[A-Za-z0-9])?$`
- **function_name**: max 128 characters, matches `^[A-Za-z0-9]([\-_A-Za-z0-9]{0,127})?$`
- **string_char**: max 255 characters in string_literal content
- **integer_literal**: max 10 digits total
- **whitespace**: max 256 characters total
- **Overall query length**: max 4096 characters

#### Query Examples

```bash
# Simple attribute comparison
--where "Slug = 'myapp'"

# Time comparison
--where "CreatedAt > '2025-01-01T00:00:00'"

# Label access
--where "Labels.tier = 'Backend'"

# Array containment
--where "ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'"

# Array length
--where "LEN(ApprovedBy) > 0"

# ApplyGates map access
--where "ApplyGates.low-cost/vet-cel = true"

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

#### Configuration Data Query Grammar

The `--where-data` flag (available only with `cub unit list`) accepts expressions that filter based on configuration data content rather than entity metadata. This uses a different query language for traversing YAML/JSON configuration paths. The formal EBNF grammar is:

```ebnf
(* ConfigHub Where-Data Query Language EBNF Grammar *)

query_expression       ::= binary_expression ( whitespace 'AND' whitespace binary_expression )*

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

operator               ::= '<=' | '>=' | '<' | '>' | '=' | '!='

literal                ::= string_literal | integer_literal | boolean_literal

string_literal         ::= "'" string_char* "'"
string_char            ::= [^']
integer_literal        ::= digit ( digit )*
boolean_literal        ::= 'true' | 'false'

whitespace             ::= ( ' ' | '\t' )*

(* Character classes *)
letter                 ::= [A-Za-z]
digit                  ::= [0-9]
map_char               ::= [A-Za-z0-9/_\-]
escaped_char           ::= '~1' | '~2'  (* ~1 for '.', ~2 for '/' *)
param_char             ::= [A-Za-z0-9_\-]
assoc_char             ::= [^.]
```

#### Configuration Data Grammar Constraints

The following constraints apply but are not expressible in pure EBNF:

- **map_segment**: max 128 characters total, starts with letter
- **parameter_name**: max 128 characters total, starts with letter
- **index_segment**: max 10 digits total
- **associative_value**: any characters except '.'
- **string_char**: any characters except single quote
- **escaped_char**: `~1` represents '.', `~2` represents '/'
- **Overall query length**: limits apply

#### Configuration Data Path Syntax

Configuration data paths are dot-separated and support several advanced features:

- **Basic paths**: `spec.replicas`, `metadata.name`
- **Array indices**: `spec.template.spec.containers.0.image`
- **Wildcards**: `spec.containers.*.image` (matches any container)
- **Associative matching**: `spec.template.spec.containers.?name:container-name=nginx.image` (find container named "nginx")
- **Split paths**: `spec.containers.*.|securityContext.runAsNonRoot` (check if any container has this security setting)
- **Escaped keys**: Use `~1` for `.` in map keys (e.g., `metadata.annotations.example~1com/annotation`)

#### Configuration Data Query Examples

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

### Core Entity Operations

#### Spaces

```bash
# List all accessible spaces
cub space list

# Get space details
cub space get SPACE_SLUG

# Create new space
cub space create -o json --from-stdin SPACE_SLUG < metadata.json
```

#### Config Units

```bash
# Create unit from configuration file
cub unit create --space SPACE_SLUG --verbose UNIT_SLUG config.yaml

# List units with filtering
cub unit list --space SPACE_SLUG --where "Labels.tier = 'Backend'"

# Get unit details
cub unit get --space SPACE_SLUG UNIT_SLUG

# Edit unit configuration
cub unit edit --space SPACE_SLUG UNIT_SLUG

# Clone unit from another space
cub unit create --space SPACE_SLUG --from-stdin VARIANT_SLUG \
  --upstream-unit SOURCE_UNIT --upstream-space SOURCE_SPACE < metadata.json

# Apply unit to live infrastructure
cub unit apply --space SPACE_SLUG UNIT_SLUG

# Approve unit for deployment
cub unit approve --space SPACE_SLUG UNIT_SLUG
```

#### Kubernetes

`cub k8s get` and `cub k8s types` read the Kubernetes resources inside Units — configuration,
not live cluster state — using kubectl's names for resource types.

```bash
# Survey what resource types exist before querying them
cub k8s types --space "*"

# List resources of a type; "all" means every type except CustomResourceDefinition
cub k8s get deploy --space SPACE_SLUG
cub k8s get all --target SPACE_SLUG/TARGET_SLUG

# Describe a resource, or print the YAML as stored
cub k8s get deploy APP --space SPACE_SLUG --show detail
cub k8s get deploy APP --space SPACE_SLUG --show data --quiet

# Filter on resource content and on Unit metadata independently
cub k8s get deploy --space "*" --where-resource "spec.replicas > 1" --where "Labels.Tier = 'Backend'"
```

Narrow with `--space`, `--target`, or `--where`: those bound how many Units are read, which
is what determines how long a query takes. Naming a `--target` searches all spaces by default,
since a target's Units usually live elsewhere.

```bash
# Trace a live resource back to its ConfigHub Unit
cub k8s source deployment APP --namespace NAMESPACE

# Bring one resource's cluster-side changes back into its Unit (diffed against
# LastReleasedRevisionNum, so it isolates drift from ConfigHub-side changes)
cub k8s refresh deployment APP --namespace NAMESPACE --dry-run

# Collect cluster facts onto a Target (stored under Cluster.* keys in the Target's Facts map)
cub k8s collect --space SPACE_SLUG --kube-context KUBE_CONTEXT TARGET_SLUG

# Preview the facts without updating any Target
cub k8s collect --kube-context KUBE_CONTEXT --dry-run
```

#### Functions

Functions can operate on configuration data stored in ConfigHub without retrieving it locally.

```bash
# List available functions to discover what functions are available to fit your tasks
cub function list

# Get function details to understand how to correctly invoke a function
cub function explain FUNCTION_NAME

# Verb-scoped commands — preferred for known function kinds:
#   vet  — validating functions only (Validating=true)
#   get  — non-mutating functions (Mutating=false, includes validating)
#   set  — mutating functions only (Mutating=true)
#
# Each verb rejects functions whose kind doesn't match, using the cached
# function signatures refreshed by `cub function list`. This also makes
# permission scopes predictable: agents can be granted only `function get`
# to safely inspect configurations.
cub function vet --space SPACE_SLUG --where "Slug = 'myunit'" VALIDATING_FN [args]
cub function get --space SPACE_SLUG --where "Slug = 'myunit'" READONLY_FN [args]
cub function set --space SPACE_SLUG --where "Slug = 'myunit'" MUTATING_FN [args]

# Mixed escape hatch — accepts any kind:
cub function do --space SPACE_SLUG --where "Slug = 'myunit'" FUNCTION_NAME [args]
```

To discover what functions are available, use `cub function list`. Before executing a function
you are not familiar with, use `cub function explain FUNCTION_NAME`.

### Function Categories

The kind decides the verb. `cub function list` prints Mutating and Validating
for every function, and marks deprecated names in their descriptions.

#### Inspection Functions (Read-only) -- `cub function get`

- `get-placeholders`: Find placeholder values ("confighubplaceholder" or 999999999) that need replacement
- `get-container-image CONTAINER_NAME`: Extract container image information
- `get-container-image-reference CONTAINER_NAME`: Just the tag/digest portion
- `get-replicas`: Replica counts for workloads
- `get-attribute`: Read a registered attribute
- `get-resources`: List all resources and their types
- `get-needed`/`get-provided`: Show needs/provides relationships
- `get-yq EXPRESSION`: Apply yq queries to YAML configuration

#### Modification Functions (Mutating) -- `cub function set`

- `set-container-image CONTAINER_NAME IMAGE`: Update container images
- `set-container-image-reference CONTAINER_NAME REFERENCE`: Update container tags (prefix the reference with `:`) and digests (prefix the reference with `@`)
- `set-image-reference-by-uri REPOSITORY_URI REFERENCE`: Retag every image matching a repository URI, wherever it sits
- `set-replicas COUNT`: Set replica counts for workloads
- `set-namespace NAMESPACE`: Set namespace for resources
- `set-annotation KEY VALUE`: Add/update annotations
- `set-label KEY VALUE`: Add/update labels
- `search-replace SEARCH REPLACE`: Text replacement across configuration
- `set-yq EXPRESSION`: Apply an in-place yq expression; the escape hatch when no purpose-built setter fits
- `ensure-context true|false`: Add/remove ConfigHub context metadata
- Defaults family, all idempotent and parameterless: `set-container-resources-defaults`,
  `set-container-probe-defaults`, `set-pod-container-security-context-defaults`,
  `set-pod-security-defaults`, `ensure-namespaces`

#### Validation Functions (Validating) -- `cub function vet`

- `vet-placeholders`: Verify no placeholder values remain
- `vet-schemas`: OpenAPI schema validation
- `vet-cel EXPRESSION`: Custom CEL validation, with the Kubernetes CEL libraries and structured per-path failures
- `vet-approvedby COUNT`: Check if sufficient approvals exist
- `vet-format`, `vet-merge-keys`: YAML hygiene and duplicate merge keys
- `vet-no-merge-conflicts`: No merge left changes withheld on this unit
- `where-filter RESOURCE_TYPE EXPRESSION`: Filter resources by criteria

#### Deprecated aliases -- do not use in new work

`yq` (use `get-yq`), `yq-i` (use `set-yq`), `set-image`/`get-image` (use
`set-container-image`/`get-container-image`), `set-image-reference` (use
`set-container-image-reference`), `cel-validate` (use `vet-cel`),
`no-placeholders` (use `vet-placeholders`), `is-approved` (use `vet-approvedby`).

### Advanced Usage Patterns

#### Bulk Operations

```bash
# Update images across multiple units across all spaces
cub function set --space "*" --where "Labels.app = 'myapp'" -o mutations \
  --change-desc "Retag nginx to 1.25-alpine" \
  set-container-image nginx nginx:1.25-alpine

# Find all units with placeholders across all spaces
cub function get --space "*" --show values get-placeholders

# Get resource types across all units across all spaces
cub function get --space "*" --show output -o jq='.[].ResourceType' get-resources
```

#### Queries and Filtering

```bash
# Find unapplied units
cub unit list --space SPACE_SLUG --where 'LastReleasedRevisionNum = 0'

# Find units with pending changes
cub unit list --space SPACE_SLUG --where 'HeadRevisionNum > LastReleasedRevisionNum'

# Find units created after specific time
cub unit list --space SPACE_SLUG --where "CreatedAt > '2025-01-01T00:00:00'"

# Find approved units
cub unit list --space SPACE_SLUG --where 'LEN(ApprovedBy) > 0'

# Find units with Kubernetes Deployments that could run as root (--resource-type is optional; omitting it searches all resource types)
cub unit list --space "*" --resource-type apps/v1/Deployment --where-data "spec.template.spec.containers.*.|securityContext.runAsNonRoot != true"
```

#### Triggers (Policy Enforcement)

```bash
# Require approval before apply
cub trigger create --space SPACE_SLUG require-approval Mutation \
  "Kubernetes/YAML" vet-approvedby 1

# Validate no placeholders remain
cub trigger create --space SPACE_SLUG vet-placeholders Mutation \
  "Kubernetes/YAML" vet-placeholders

# Custom CEL validation. "r." refers to the current resource.
cub trigger create --space SPACE_SLUG replicated Mutation \
  "Kubernetes/YAML" vet-cel 'r.kind != "Deployment" || r.spec.template.spec.containers.all(container, container.securityContext.runAsNonRoot == true)'
```

## Function Selection Guide

Every mutating invocation should carry `--change-desc` and `-o mutations`.

### To Find Issues in Configuration:

1. **Check for placeholders**: `cub function get --space SPACE get-placeholders`
2. **Validate schema**: `cub function vet --space SPACE vet-schemas`
3. **Custom validation**: `cub function vet --space SPACE vet-cel 'YOUR_EXPRESSION'`

### To Modify Configuration:

1. **Container images**: `cub function set --space SPACE -o mutations --change-desc "..." set-container-image CONTAINER IMAGE`
2. **Scaling**: `cub function set --space SPACE -o mutations --change-desc "..." set-replicas COUNT`
3. **Namespaces**: `cub function set --space SPACE -o mutations --change-desc "..." set-namespace NAMESPACE`
4. **Labels/Annotations**: `cub function set --space SPACE -o mutations --change-desc "..." set-label KEY VALUE`
5. **Text replacement**: `cub function set --space SPACE -o mutations --change-desc "..." search-replace OLD NEW`

### To Inspect Configuration:

1. **List resources**: `cub function get --space SPACE get-resources`
2. **Extract values**: `cub function get --space SPACE get-yq '.spec.replicas'`
3. **Get specific attributes**: `cub function get --space SPACE get-container-image CONTAINER`
4. **Check dependencies**: `cub function get --space SPACE get-needed`

### To Validate Configuration:

1. **No placeholders**: `cub function vet --space SPACE vet-placeholders`
2. **Approval status**: `cub function vet --space SPACE vet-approvedby MIN_COUNT`
3. **Resource filtering**: `cub function vet --space SPACE where-filter RESOURCE_TYPE 'EXPRESSION'`

## Supported Configuration Formats

- **Kubernetes/YAML**: Kubernetes resources in YAML format
- **AppConfig/Properties**: Application Java Properties configurations
- **AppConfig/YAML**: Application YAML configurations
- **AppConfig/TOML**: Application TOML configurations
- **AppConfig/INI**: Application INI configurations
- **AppConfig/Env**: Application Env configurations
- **AppConfig/YAML**: Application YAML configurations
- **AppConfig/JSON**: Application JSON configurations

Functions are toolchain-specific, so ensure you're using the right function for your configuration type.

## Common Workflows

### 1. Creating and Configuring a Unit

```bash
# Create unit from file
cub unit create --space myspace --verbose myapp app.yaml

# Check for placeholders
cub function get --space myspace --unit myapp get-placeholders

# Replace placeholders
cub function set --space myspace --unit myapp -o mutations \
  --change-desc "Set namespace to production" set-namespace production

# Validate configuration
cub function vet --space myspace --unit myapp vet-placeholders
```

### 2. Updating Images Across Multiple Units

```bash
# Find units with specific app label
cub unit list --space myspace --where "Labels.app = 'myapp'"

# Update all matching units
cub function set --space myspace --where "Labels.app = 'myapp'" -o mutations \
  --change-desc "Retag nginx to 1.25-alpine" \
  set-image-reference-by-uri nginx ":1.25-alpine"
```

### 3. Validation and Approval Workflow

```bash
# Check unit status
cub unit get --space myspace myapp

# Validate configuration
cub function vet --space myspace --unit myapp vet-schemas

# Approve unit
cub unit approve --space myspace myapp

# Apply to live infrastructure
cub unit apply --space myspace myapp
```

### 4. Editing Units Locally

While configuration can be operated on in ConfigHub using functions, it can also be operated upon locally by retrieving the configuration, editing it, and writing it back.

Typical workflow for editing:

```bash
# 1. Download the current config data (e.g., Kubernetes YAML)
cub unit data myapp --space myspace > myapp.yaml

# 2. Edit the file locally
# (make your changes to myapp.yaml)

# 3. Upload the updated config
cub unit update myapp myapp.yaml --space myspace --change-desc "Added resource limits"

# Other useful options:
# --timeout: Set completion timeout (default "2m")
# --restore <revision>: Restore to a specific revision number
```
