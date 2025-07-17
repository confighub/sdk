# Custom Bridge Example

This guide explains how to create custom bridges for ConfigHub. Bridges are adapters that connect ConfigHub to external systems, allowing you to apply, refresh, import, and destroy configuration resources on various targets.

## Quick Start

Log into ConfigHub with the CLI:

    cub auth login

Once authenticated, create a Worker:

    cub worker create hello-bridge

Grab the Worker ID and Secret with:

    cub worker get hello-bridge --include-secret

Set the environment:

    export CONFIGHUB_WORKER_ID=...
    export CONFIGHUB_WORKER_SECRET=...
    export EXAMPLE_BRIDGE_DIR=/tmp/confighub-example-bridge  # Optional: defaults to /tmp/confighub-example-bridge

Create a the base directory and a test subdirectory (which will become a target):

    mkdir -p /tmp/confighub-example-bridge/dev

Build the example in this directory:

    go build

Run the bridge:

    ./hello-world-bridge

It should connect to ConfigHub and display:

    [INFO] Starting hello-world-bridge example...
    [INFO] Using base directory: /tmp/confighub-example-bridge
    [INFO] Starting connector...

Note: You may see warnings about uninitialized loggers - these can be safely ignored.

Create a unit with some Kubernetes compliant YAML content:

    cub unit create myapp test_input.yaml --target dev

Apply the unit to your bridge target:

    cub unit apply myapp --target dev

The bridge will write the configuration to `/tmp/confighub-example-bridge/dev/myapp.yaml`.

## Bridge Operations

Bridges implement five core operation. An operation is always performed on a config unit. The following properties apply:

* The unit in question must first be associated with a target made available by the bridge
* The unit and the target must have the same toolchain. E.g. you cannot create a properties file as a unit and associate it with a kubernetes/yaml target.

### 1. Apply

Applies configuration to the target system. In this example, it creates/updates files on the filesystem.

    cub unit apply myapp

### 2. Refresh  

Reads the current state from the target and detects drift. Returns whether the live state matches the desired state.

    cub unit refresh myapp

### 3. Import

Discover existing resources in the target system. In this example, it can theoretically discover files in a target subdir that it doesn't yet know about, but this feature is not implemented right now for this example.

### 4. Destroy

Removes configuration from the target system. In this example, it deletes files.

    cub unit destroy myapp

### 5. Finalize

Performs cleanup operations after other actions. Implementation-specific.

## Core Concepts

### Bridge Interface

Every bridge must implement the `Bridge` interface:

```go
type Bridge interface {
    Info(opts InfoOptions) BridgeInfo
    Apply(ctx BridgeContext, payload BridgePayload) error
    Refresh(ctx BridgeContext, payload BridgePayload) error
    Import(ctx BridgeContext, payload BridgePayload) error
    Destroy(ctx BridgeContext, payload BridgePayload) error
    Finalize(ctx BridgeContext, payload BridgePayload) error
}
```

### Target Discovery

The `Info()` method returns available targets. In this example it treats a sub-directory as a target. In other (more realistic) use cases, a target may be a Kubernetes cluster represented by a kubecontext, it may be a namespace in a kube cluster or it may be an IaaS cloud identity.

### Status Reporting

Bridges report operation progress using `SendStatus()`. For example:

```go
startTime := time.Now()
// Send initial status
if err := ctx.SendStatus(&api.ActionResult{
    UnitID:            payload.UnitID,
    SpaceID:           payload.SpaceID,
    QueuedOperationID: payload.QueuedOperationID,
    ActionResultBaseMeta: api.ActionResultMeta{
        Action:    api.ActionApply,
        Result:    api.ActionResultNone,
        Status:    api.ActionStatusProgressing,
        Message:   fmt.Sprintf("Starting apply operation for %s", eb.name),
        StartedAt: startTime,
    },
}); err != nil {
    return err
}

// ... perform operation ...

terminatedAt := time.Now()
// Send completion status
return ctx.SendStatus(&api.ActionResult{
    UnitID:            payload.UnitID,
    SpaceID:           payload.SpaceID,
    QueuedOperationID: payload.QueuedOperationID,
    ActionResultBaseMeta: api.ActionResultMeta{
        Action:       api.ActionApply,
        Result:       api.ActionResultApplyCompleted,
        Status:       api.ActionStatusCompleted,
        Message:      fmt.Sprintf("Successfully wrote configuration to %s at %s", filepath, time.Now().Format(time.RFC3339)),
        StartedAt:    startTime,
        TerminatedAt: &terminatedAt,
    },
    Data:      payload.Data,
    LiveState: payload.Data,
})
```

### Drift Detection

The Refresh operation compares the live state as expected by ConfigHub with the actual live state in the infrastructure. In this example, it simply does a byte comparison between the unit data in ConfigHub and the file contents in the corresponding file.

## Bridge Registration

Register your bridge with the dispatcher and pass the dispatcher to ConfighubConnector:

```go
bridgeDispatcher := worker.NewBridgeDispatcher()
bridgeDispatcher.RegisterBridge(NewExampleBridge("example-bridge", baseDir))

connector, err := worker.NewConnector(worker.ConnectorOptions{
    WorkerID:         os.Getenv("CONFIGHUB_WORKER_ID"),
    WorkerSecret:     os.Getenv("CONFIGHUB_WORKER_SECRET"),
    ConfigHubURL:     os.Getenv("CONFIGHUB_URL"),
    BridgeDispatcher: &bridgeDispatcher,
})
```

## Target Parameters

Bridges can accept parameters from targets. This example uses `dir_name` to determine which subdirectory to use:

```go
func parseTargetParams(payload api.BridgeWorkerPayload) (string, error) {
    var params map[string]interface{}
    if len(payload.TargetParams) > 0 {
        if err := json.Unmarshal(payload.TargetParams, &params); err != nil {
            return "", fmt.Errorf("failed to parse target params: %v", err)
        }
    }

    // Get directory name from the parameter I set in Info()
    if dirName, ok := params["dir_name"].(string); ok && dirName != "" {
        return dirName, nil
    }

    // Default to "default" if no directory name found
    return "default", nil
}
```

## Data Types

### BridgePayload

Contains all information about the operation:

- `UnitID`, `SpaceID`, `QueuedOperationID` - Identifiers
- `UnitSlug` - Human-readable unit name
- `Data` - Desired configuration (YAML/JSON)
- `LiveState` - Current state from previous operations
- `TargetParams` - Target-specific parameters
- `ToolchainType`, `ProviderType` - Configuration type info

### ActionResult

Reports operation status and results:

- `ActionResultBaseMeta` - Action type, status, messages, timing
- `Data` - Updated configuration (for Import)
- `LiveState` - Current state after operation

## Best Practices

### Error Handling

Always report errors with appropriate status:

```go
if err != nil {
    return fmt.Errorf("failed to write file %s: %w", filepath, err)
}
```

For more complex error handling, you can send status updates before returning errors:

```go
if err != nil {
    terminatedAt := time.Now()
    ctx.SendStatus(&api.ActionResult{
        UnitID:            payload.UnitID,
        SpaceID:           payload.SpaceID,
        QueuedOperationID: payload.QueuedOperationID,
        ActionResultBaseMeta: api.ActionResultMeta{
            Action:       api.ActionApply,
            Result:       api.ActionResultApplyFailed,
            Status:       api.ActionStatusFailed,
            Message:      fmt.Sprintf("Failed to write file: %v", err),
            StartedAt:    startTime,
            TerminatedAt: &terminatedAt,
        },
    })
    return fmt.Errorf("failed to write file %s: %w", filepath, err)
}
```

### Progress Updates

For long-running operations, send periodic status updates:

1. Initial "progressing" status when starting
2. Intermediate updates for multi-step operations  
3. Final "completed" or "failed" status

### Target Management

- Targets are owned by the bridge and advertised to ConfigHub
- Targets can in some cases be manually created in ConfigHub
- Right now, the bridge is responsible for given each target a unique name within a space. This is not ideal and will be reconsidered.

## Example Implementations

This filesystem bridge demonstrates basic concepts. Examples of real-world bridges might be:

- **Kubernetes Bridge**: Apply YAML manifests to clusters
- **AWS Bridge**: Manage cloud resources via APIs
- **Database Bridge**: Execute schema migrations

