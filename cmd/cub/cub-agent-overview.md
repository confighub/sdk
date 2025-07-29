# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

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

# Set default space context
cub context set --space SPACE_SLUG

# Get current context
cub context get
```

### Common Flags

- `--space SPACE_SLUG`: Override default space context; specify `*` to indicate all spaces
- `--json`: Output formatted JSON, suppressing default output
- `--jq EXPRESSION`: Apply jq expression to response, suppressing default output
- `--where "EXPRESSION"`: Filter results using simple relational expressions. The specified string is an expression for the purpose of filtering the list of entities returned. The expression syntax was inspired by SQL, but does not support full SQL syntax currently. It supports conjunctions using `AND` of relational expressions of the form _attribute_ _operator_ _attribute_or_literal_. The attribute names are case-sensitive and PascalCase, as in the JSON encoding. Supported attributes for each entity are allow-listed, and documented in swagger. All entities that include the attributes support `CreatedAt`, `UpdatedAt`, `DisplayName`, `Slug`, and ID fields. `Labels` are supported, using a dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`. Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`. String pattern operators include `LIKE` and `~~` for pattern matching with `%` and `_` wildcards, `ILIKE` for case-insensitive pattern matching, and `!~~` for NOT LIKE. String regex operators include `~` for regex matching, `~*` for case-insensitive regex, and `!~`/`!~*` for regex not matching. Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`. UUIDs and boolean attributes support equality and inequality only. String literals are quoted with single quotes, such as `'string'`. UUID and time literals must be quoted as string literals, as in `'7c61626f-ddbe-41af-93f6-b69f4ab6d308'`. Time literals use the same form as when serialized as JSON, such as: `CreatedAt > '2025-02-18T23:16:34'`. Integer and boolean literals are also supported for attributes of those types. An example conjunction is: `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`. See the [Query Language Grammar](#query-language-grammar) section for the formal syntax specification.
- `--from-stdin`: Read JSON input from stdin for passing to the ConfigHub API
- `--verbose`: Show detailed output, additive with default output
- `--debug`: Show API calls

#### Query Language Grammar

The `--where` flag accepts expressions in a simple query language. The formal EBNF grammar is:

```ebnf
(* ConfigHub Query Language EBNF Grammar *)

query_expression    ::= binary_expression ( whitespace 'AND' whitespace binary_expression )*

binary_expression   ::= left_operand whitespace operator whitespace right_operand

left_operand        ::= length_expression | map_access | attribute_name
right_operand       ::= attribute_name | literal

length_expression   ::= 'LEN' '(' attribute_name ')'

map_access          ::= labels_access | apply_gates_access
labels_access       ::= 'Labels' '.' label_key
apply_gates_access  ::= 'ApplyGates' '.' slug '/' function_name

operator            ::= '<=' | '>=' | '<' | '>' | '=' | '!=' | '?' | 'LIKE' | 'ILIKE' | '~~' | '!~~' | '~' | '~*' | '!~' | '!~*'

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
--where "ApplyGates.low-cost/cel-validate = true"

# String pattern matching
--where "Slug LIKE 'app-%'"

# Case-insensitive pattern matching
--where "Slug ILIKE '%BACKEND%'"

# Regex matching
--where "Slug ~ '^app-[0-9]+$'"

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
cub space create --json --from-stdin SPACE_SLUG < metadata.json
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

#### Functions

Functions can operate on configuration data stored in ConfigHub without retrieving it locally.

```bash
# List available functions to discover what functions are available to fit your tasks
cub function list

# Get function details to understand how to correctly invoke a function
cub function explain FUNCTION_NAME

# Invoke a function on specific units
cub function do --space SPACE_SLUG --where "Slug = 'myunit'" FUNCTION_NAME [args]

# Invoke a function across all units in space
cub function do --space SPACE_SLUG FUNCTION_NAME [args]
```

To discover what functions are available, use `cub function list`. Before executing a function
you are not familiar with, use `cub function explain FUNCTION_NAME`.

### Function Categories

#### Inspection Functions (Read-only)

- `get-placeholders`: Find placeholder values ("confighubplaceholder" or 999999999) that need replacement
- `get-image`: Extract container image information
- `get-attributes`: List significant configuration attributes
- `get-resources`: List all resources and their types
- `get-needed`/`get-provided`: Show needs/provides relationships
- `yq EXPRESSION`: Apply yq queries to YAML configuration

#### Modification Functions (Mutating)

- `set-image CONTAINER_NAME IMAGE`: Update container images
- `set-image-reference CONTAINER_NAME REFERENCE`: Update container tags (prefix the reference with `:`) and digests (prefix the reference with `@`)
- `set-replicas COUNT`: Set replica counts for workloads
- `set-namespace NAMESPACE`: Set namespace for resources
- `set-annotation KEY VALUE`: Add/update annotations
- `set-label KEY VALUE`: Add/update labels
- `search-replace SEARCH REPLACE`: Text replacement across configuration
- `ensure-context true|false`: Add/remove ConfigHub context metadata

#### Validation Functions (Validating)

- `no-placeholders`: Verify no placeholder values remain
- `cel-validate EXPRESSION`: Custom CEL validation expressions
- `is-approved COUNT`: Check if sufficient approvals exist
- `validate`: Schema validation
- `where-filter RESOURCE_TYPE EXPRESSION`: Filter resources by criteria

### Advanced Usage Patterns

#### Bulk Operations

```bash
# Update images across multiple units across all spaces
cub function do --space "*" --where "Labels.app = 'myapp'" \
  set-image nginx nginx:1.25-alpine

# Find all units with placeholders across all spaces
cub function do --space "*" get-placeholders --output-values-only

# Get resource types across all units across all spaces
cub function do --space "*" get-resources --output-jq '.[].ResourceType'
```

#### Queries and Filtering

```bash
# Find unapplied units
cub unit list --space SPACE_SLUG --where 'LiveRevisionNum = 0'

# Find units with pending changes
cub unit list --space SPACE_SLUG --where 'HeadRevisionNum > LiveRevisionNum'

# Find units created after specific time
cub unit list --space SPACE_SLUG --where "CreatedAt > '2025-01-01T00:00:00'"

# Find approved units
cub unit list --space SPACE_SLUG --where 'LEN(ApprovedBy) > 0'

# Find units with Kubernetes Deployments that could run as root (--resource-type must be specified when --where-data is specified)
cub unit list --space "*" --resource-type apps/v1/Deployment --where-data "spec.template.spec.containers.*.|securityContext.runAsNonRoot != true"
```

#### Triggers (Policy Enforcement)

```bash
# Require approval before apply
cub trigger create --space SPACE_SLUG require-approval Mutation \
  "Kubernetes/YAML" is-approved 1

# Validate no placeholders remain
cub trigger create --space SPACE_SLUG no-placeholders Mutation \
  "Kubernetes/YAML" no-placeholders

# Custom CEL validation. "r." refers to the current resource.
cub trigger create --space SPACE_SLUG replicated Mutation \
  "Kubernetes/YAML" cel-validate 'r.kind != "Deployment" || r.spec.template.spec.containers.all(container, container.securityContext.runAsNonRoot == true)'
```

## Function Selection Guide

### To Find Issues in Configuration:

1. **Check for placeholders**: `cub function do --space SPACE get-placeholders`
2. **Validate schema**: `cub function do --space SPACE validate`
3. **Custom validation**: `cub function do --space SPACE cel-validate 'YOUR_EXPRESSION'`

### To Modify Configuration:

1. **Container images**: `cub function do --space SPACE set-image CONTAINER IMAGE`
2. **Scaling**: `cub function do --space SPACE set-replicas COUNT`
3. **Namespaces**: `cub function do --space SPACE set-namespace NAMESPACE`
4. **Labels/Annotations**: `cub function do --space SPACE set-label KEY VALUE`
5. **Text replacement**: `cub function do --space SPACE search-replace OLD NEW`

### To Inspect Configuration:

1. **List resources**: `cub function do --space SPACE get-resources`
2. **Extract values**: `cub function do --space SPACE yq '.spec.replicas'`
3. **Get specific attributes**: `cub function do --space SPACE get-image CONTAINER`
4. **Check dependencies**: `cub function do --space SPACE get-needed`

### To Validate Configuration:

1. **No placeholders**: `cub function do --space SPACE no-placeholders`
2. **Approval status**: `cub function do --space SPACE is-approved MIN_COUNT`
3. **Resource filtering**: `cub function do --space SPACE where-filter RESOURCE_TYPE 'EXPRESSION'`

## Supported Configuration Formats

- **Kubernetes/YAML**: Kubernetes resources in YAML format
- **AppConfig/Properties**: Java-style properties files
- **OpenTofu/HCL**: OpenTofu/Terraform HCL configurations

Functions are toolchain-specific, so ensure you're using the right function for your configuration type.

## Common Workflows

### 1. Creating and Configuring a Unit

```bash
# Create unit from file
cub unit create --space myspace --verbose myapp app.yaml

# Check for placeholders
cub function do --space myspace --where "Slug = 'myapp'" get-placeholders

# Replace placeholders
cub function do --space myspace --where "Slug = 'myapp'" set-namespace production

# Validate configuration
cub function do --space myspace --where "Slug = 'myapp'" no-placeholders
```

### 2. Updating Images Across Multiple Units

```bash
# Find units with specific app label
cub unit list --space myspace --where "Labels.app = 'myapp'"

# Update all matching units
cub function do --space myspace --where "Labels.app = 'myapp'" \
  set-image-reference-by-uri nginx ":1.25-alpine"
```

### 3. Validation and Approval Workflow

```bash
# Check unit status
cub unit get --space myspace myapp

# Validate configuration
cub function do --space myspace --where "Slug = 'myapp'" validate

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
cub unit get myapp --space myspace --data-only > myapp.yaml

# 2. Edit the file locally
# (make your changes to myapp.yaml)

# 3. Upload the updated config
cub unit update myapp myapp.yaml --space myspace --change-desc "Added resource limits"

# Other useful options:
# --timeout: Set completion timeout (default "2m")
# --restore <revision>: Restore to a specific revision number
```
