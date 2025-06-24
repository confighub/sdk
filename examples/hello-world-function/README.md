# Custom Function Example

This guide explains how to create custom functions for ConfigHub. Functions are executable pieces of code that operate on configuration data, allowing you to read, modify, or validate configuration units.

## Quick Start

Log into ConfigHub with the CLI:

    cub auth login

Once authenticated, create a Worker:

    cub worker create helloworld

Grab the Worker ID and Secret with:

    cub worker get helloworld --include-secret

Set the environment:

    export CONFIGHUB_WORKER_ID=...
    export CONFIGHUB_WORKER_SECRET=...
    export CONFIGHUB_URL=...

The last one is only necessary if you're not using https://hub.confighub.com. Now build the app in this directory with

    go build

and run it with:

    ./hello-world-function

It should connect to ConfigHub and get ready for action. Now create a target:

    cub target create helloworld '{}' helloworld

and create a unit associated with this target:

    cub unit create helloworld test_input.yaml --target helloworld

Now you can run the hello-world function on this unit:

    cub function do hello-world someargument --where "Slug='helloworld'" --use-worker

It should print something like:

    SUCCESS    true
    CONFIGDATA
    ---------
    ...
    Awaiting triggers...

and in the terminal where your worker is running, you should see some log lines indicating that the function was executed.

(text below here written by Claude. Review needed)

## Function Types

ConfigHub supports three types of functions:

### 1. Readonly Functions

Extract specific data from configuration units without modifying them.

- Return extracted data as output
- Set `Mutating: false` in the function signature
- Example: Extract container image names from Kubernetes deployments

### 2. Mutating Functions

Modify configuration data and return the updated configuration.

- Set `Mutating: true` in the function signature
- Can also return additional output data
- Example: Update image tags, set replicas, add labels

### 3. Validating Functions

Check configuration data against rules and return pass/fail results.

- Set `Validating: true` in the function signature
- Return `ValidationResult` as output
- Example: Ensure resources have required labels, validate security policies

## Core Concepts

### Function Signature

Every function must have a signature that describes:

- Name (kebab-case, e.g., "hello-world")
- Parameters (name, type, required/optional)
- Output type and description
- Function properties (mutating, validating, hermetic, idempotent)

```go
api.FunctionSignature{
    FunctionName: "my-function",
    Parameters: []api.FunctionParameter{
        {
            ParameterName: "my-param",
            Description:   "Description of the parameter",
            Required:      true,
            DataType:      api.DataTypeString,
        },
    },
    RequiredParameters: 1,
    Mutating:          true,
    Validating:        false,
    Hermetic:          true,
    Idempotent:        true,
    Description:       "What this function does",
}
```

### Function Implementation

Functions follow this signature pattern:

```go
func MyFunction(
    ctx *api.FunctionContext,
    parsedData gaby.Container,
    args []api.FunctionArgument,
    liveState []byte,
) (gaby.Container, any, error)
```

**Parameters:**

- `ctx` - Context with unit/space/org metadata
- `parsedData` - Parsed YAML/JSON documents to operate on
- `args` - Function arguments from the caller
- `liveState` - Current live state from the target system (optional)

**Returns:**

- Modified configuration data (or original if readonly)
- Function output (validation results, extracted data, etc.)
- Error if something went wrong

## Data Types

### Parameter Types

- `DataTypeString` - String values
- `DataTypeInt` - Integer values
- `DataTypeBool` - Boolean values
- `DataTypeYAML` - YAML content
- `DataTypeJSON` - JSON content
- Custom types for specific use cases

### Output Types

- `OutputTypeYAML` - YAML configuration data
- `OutputTypeValidationResult` - Pass/fail with details
- `OutputTypeAttributeValueList` - Extracted attribute values
- `OutputTypeResourceInfoList` - Resource metadata
- `OutputTypeCustomJSON` - Custom JSON output

## Working with Configuration Data

ConfigHub uses the `gaby` library for YAML/JSON traversal:

```go
// Access a field
value, err := doc.GetP("spec.replicas")

// Set a field
_, err := doc.SetP(newValue, "spec.replicas")

// Use dot-separated paths with array indices
image, err := doc.GetP("spec.template.spec.containers.0.image")
```

### Path Patterns

- Simple paths: `metadata.name`
- Array indices: `spec.containers.0.image`
- Escaped dots: Use `yamlkit.EscapeDotsInPathSegment()` for keys with dots
- Associative paths: `spec.containers.?name:my-container.image`

## Function Properties

### Hermetic vs Non-Hermetic

- **Hermetic**: Function doesn't call external systems (recommended)
- **Non-Hermetic**: Function may call APIs, databases, etc.

### Idempotent vs Non-Idempotent

- **Idempotent**: Running multiple times produces same result (recommended)
- **Non-Idempotent**: Each run may produce different results

## Registration and Deployment

### 1. Register with Function Handler

```go
fh := handler.NewFunctionHandler()
fh.RegisterFunction("my-function", &handler.FunctionRegistration{
    FunctionSignature: mySignature,
    Function:         myFunction,
})
```

### 2. Deploy as Bridge Worker

Create a bridge worker that includes your functions and deploy it to your target environment.

### 3. Use with ConfigHub

Once deployed, your functions can be invoked via:

- ConfigHub UI
- CLI commands
- API calls
- Triggers and automation

## Best Practices

### Error Handling

```go
if len(args) != expectedCount {
    return parsedData, nil, fmt.Errorf("expected %d arguments, got %d", expectedCount, len(args))
}

value, ok := args[0].Value.(string)
if !ok {
    return parsedData, nil, fmt.Errorf("parameter must be string, got %T", args[0].Value)
}
```

### Parameter Validation

```go
// Validate parameter types and values
if len(greeting) == 0 {
    return parsedData, nil, fmt.Errorf("greeting cannot be empty")
}

// Use parameter names for clarity
for _, arg := range args {
    switch arg.ParameterName {
    case "greeting":
        greeting = arg.Value.(string)
    case "target":
        target = arg.Value.(string)
    }
}
```

### Resource Iteration

```go
// Process all resources
for _, doc := range parsedData {
    resourceType, err := k8skit.K8sResourceProvider.ResourceTypeGetter(doc)
    if err != nil {
        continue // Skip invalid resources
    }

    // Process specific resource types
    if resourceType == "apps/v1/Deployment" {
        // Handle deployment
    }
}
```

## Testing

### Local Testing with fctl

```bash
# Test your function locally
fctl do config.yaml "MyUnit" my-function "parameter-value"
```

### Unit Tests

```go
func TestMyFunction(t *testing.T) {
    // Parse test YAML
    parsedData, err := gaby.ParseAll([]byte(testYAML))
    require.NoError(t, err)

    // Create test arguments
    args := []api.FunctionArgument{
        {Value: "test-value"},
    }https://github.com/confighubai/confighub/pull/2270

    // Call function
    result, output, err := MyFunction(nil, parsedData, args, nil)

    // Verify results
    require.NoError(t, err)
    // Add assertions...
}
```

## Common Patterns

### Path-Based Functions

For functions that operate on specific YAML paths, consider using the path registry system for consistency with built-in functions.

### Validation Functions

```go
func ValidateFunction(ctx *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
    // Validation logic here...

    if validationPassed {
        return parsedData, api.ValidationResult{Passed: true}, nil
    } else {
        return parsedData, api.ValidationResult{
            Passed: false,
            Details: []string{"Validation failed because..."},
        }, nil
    }
}
```

### Readonly Functions

```go
func ReadonlyFunction(ctx *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
    var results api.AttributeValueList

    // Extract data from parsedData
    for _, doc := range parsedData {
        value, err := doc.GetP("some.path")
        if err == nil {
            results = append(results, api.AttributeValue{
                Value: value,
                // ... other fields
            })
        }
    }

    return parsedData, results, nil
}
```

## Examples and References

- [Hello World Function](./hello-world-function/) - Complete working example
- [Built-in Kubernetes Functions](../function/internal/handlers/kubernetes/) - Advanced examples
- [Function API Documentation](../function/api/function.go) - Complete API reference
- [ConfigKit Library](../configkit/) - Utilities for configuration manipulation

## Getting Help

- Check the built-in function implementations for patterns
- Review the function API documentation
- Test functions locally with `fctl` before deployment
- Start with simple readonly functions before building complex mutating functions
