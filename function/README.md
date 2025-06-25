# Function executor

In ConfigHub, code is separate from configuration data. The code reads and/or writes configuration data.

A Function is an executable piece of code that operates on Config Units.

There are 3 types of Functions: Readonly, Mutating and Validating. A Readonly Function extracts specific pieces of data from a Config Unit and returns them. For example it may extract the container image URI from a Unit containing a Kubernetes Deployment. A Mutating function takes Config Units as input, makes changes to them and writes and returns the mutated Units. For example, a Mutating function may update the image of a Deployment. A Validating function takes Config Units as input and returns simple pass/fail for each Unit.

Functions most commonly run synchronously, hermetically, and idempotently, with no side effects. But it is possible for functions to interact with external systems. The main constraint is that functions are expected to return synchronously and therefore must complete without long delays.

The intent is that, like command-line tools and other API-based automation, ConfigHub functions should be reusable across teams, organizations, and use cases. They should be composable and interoperable.

Many kinds of configuration data, such as Kubernetes resources and cloud resources serialized in some Infrastructure as Code format, have well defined, stable schemas, like APIs. Such data is well suited to be operated on by stable functions. Kustomize operates on Kubernetes resources based on the same principle.

At the moment, we've only implemented Kubernetes functions. Other configuration formats of other ToolchainTypes will be implemented in the future.

## Authoring functions

Currently the function executor is written in Go, and all of the functions we've written have been written in Go.

We provide a Go library, called ConfigKit, to assist with authoring functions using common patterns. The library is here:

- https://github.com/confighubai/public/tree/main/pkg/configkit

That library is built upon another Go library that provides traversal of and access to YAML configuration elements, here:

- https://github.com/confighubai/public/tree/main/third_party/gaby

YAML paths in gaby and yamlkit are dot-separated, including for array indices. For example:

```
spec.template.spec.containers.0.image
```

Use `yamlkit.EscapeDotsInPathSegment` to escape map keys, such as Kubernetes annotations, which may have dots in them and `gaby.DotPathToSlice` to split a path into path segments to traverse them with gaby. Dots are escaped with `~1`.

The Kubernetes API and cloud APIs contain a number of associative lists, such as container lists and environment variable lists, where the keys identifying the array elements are map elements of the array element, such as `name`. yamlkit has a function for resolving associative paths to array index syntax, `ResolveAssociativePaths`. The syntax for an associative list path lookup is `.?<map key>:<parameter name>=<map value>`, as in `spec.template.spec.containers.?name:container-name=%s.image` (using a Sprintf placeholder). The `:<parameter name>` is optional, but is used to match corresponding values.

`ResolveAssociativePaths` also supports wildcards. `*` is the simplest form of wildcard. As with associative matches, matched segments may be bound to parameter names. The syntax is `.*?<map key>:<getter parameter name>`, as in `spec.template.spec.containers.*?name:container-name`.

`ResolveAssociativePaths` can also bind map keys to parameters, using `.@<map key>:<parameter name>`, for a specific key, or `.*@:<parameter name>` for any key.

`ResolveAssociativePaths` supports path existence checking using the `.|` syntax. When a path segment is prefixed with `|`, the path resolution requires that the preceding path exists up to that point, but allows the current segment to be created if it doesn't exist. For example, `spec.template.spec.containers.0.|securityContext` will resolve if the `containers.0` path exists, regardless of whether `securityContext` exists. This is useful for conditional operations where you want to ensure a parent structure exists before creating or modifying child elements.

### Resource traversal

Kubernetes resources are currently stored in Units as lists of YAML documents. Kubernetes functions mostly work the same as functions on any arbitrary YAML. k8skit implements an interface to enable extracting the resource type and name from each document:
https://github.com/confighubai/public/blob/main/pkg/configkit/k8skit/k8skit.go#L42

The resource type is in the format group/version/kind. The resource name is in the format namespace/name, where the namespace segment is empty if the namespace isn't present, including in the case of cluster-scoped resources.

An example of a function that iterates over resources is `record-resource-names`, here:
https://github.com/confighubai/public/blob/main/plugin/functions/internal/handlers/kubernetes/standard_functions.go#L2511

```
func k8sFnRecordResourceNames(_ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
    // Iterate over YAML documents
	for _, doc := range parsedData {
        // Extract the resource name
		resourceName, err := k8skit.K8sResourceProvider.ResourceNameGetter(doc)
		if err != nil {
			return parsedData, nil, err
		}

        // Upsert an annotation
		safeKey := yamlkit.EscapeDotsInPathSegment(OriginalNameAnnotation)
		_, err = doc.SetP(string(resourceName), "metadata.annotations."+safeKey)
		if err != nil {
			return parsedData, nil, err
		}
	}
	return parsedData, nil, nil
}
```

### Path traversal

As in Kustomize, many mutating and readonly functions operate on specific paths in specific Kubernetes resource types. We're working on making the definition of paths extensible.

yamlkit provides a set of visitor functions to make these path traversals straightforward. VisitPaths ane VisitPathsAnyType are the main such functions. There are some more specific convenience functions layered on top of them: UpdatePathsFunction, UpdatePathsValue, GetPaths, GetPathsAnyType, UpdateStringPathsFunction, UpdateStringPaths, GetStringPaths, etc.

Here's the code for `set-string-path`, which sets a string value at the specified YAML path:

```
func k8sFnSetStringPath(_ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	gvk := args[0].Value.(string)
	resolvedPath := args[1].Value.(string)
	value := args[2].Value.(string)

	resourceTypeToPaths := getVisitorMapForPath(api.ResourceType(gvk), api.ResolvedPath(resolvedPath))
	err := yamlkit.UpdateStringPaths(parsedData, resourceTypeToPaths, []any{}, k8skit.K8sResourceProvider, value)
	return parsedData, nil, err
}
```

### Path registry

A number of functions are as simple as just registering the right YAML paths for the visitor functions. You could use a function like `set-int-path` and specify the YAML path as a function argument, but if you operate on the same path(s) frequently, it can be worthwhile to define a function.

As an example, here's the code for `set-replicas`:

```
func k8sFnSetReplicas(_ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	newReplicas := args[0].Value.(int)

	resourceTypeToReplicasPaths := yamlkit.GetPathRegistryForAttributeName(attributeNameReplicas)
	err := yamlkit.UpdatePathsValue[int](parsedData, resourceTypeToReplicasPaths, []any{}, k8skit.K8sResourceProvider, newReplicas)
	return parsedData, nil, err
}
```

Look at this line:

```
	resourceTypeToReplicasPaths := yamlkit.GetPathRegistryForAttributeName(attributeNameReplicas)
```

There's a path registry that associates a set of paths and path metadata with a named attribute used to identify that set of semantically related paths. `attributeNameReplicas` is currently defined to have the value `"replicas"`.

Paths are typically only relevant to specific resource types. Here's how the types are currently defined for which `attributeNameReplicas` is applicable:

```
var replicatedControllerResourceTypes = []api.ResourceType{
	api.ResourceType("apps/v1/Deployment"),
	api.ResourceType("apps/v1/ReplicaSet"),
	api.ResourceType("apps/v1/StatefulSet"),
}
```

The paths are registered before the function executor server is started, like this:

```
	replicasGetterFunctionInvocation := &api.FunctionInvocation{
		FunctionName: "get-replicas",
	}
	replicasSetterFunctionInvocation := &api.FunctionInvocation{
		FunctionName: "set-replicas",
	}

	for _, resourceType := range replicatedControllerResourceTypes {
		attributePath := api.UnresolvedPath("spec.replicas")
		pathInfos := api.PathToVisitorInfoType{
			attributePath: {
				Path:          attributePath,
				AttributeName: attributeNameReplicas,
				DataType:      api.DataTypeInt,
			},
		}
		yamlkit.RegisterPathsByAttributeName(
			attributeNameReplicas,
			resourceType,
			pathInfos,
			replicasGetterFunctionInvocation,
			replicasSetterFunctionInvocation,
			false,
		)
	}
```

There's an even easier way to create simple path-based getter and setter functions. Take a look at `registerPathSetterAndGetter`.

In the future we expect to provide a way to define these sets of paths dynamically.

## Registering functions

Functions you add here can be built into the worker in this repo.

There's an example here:
https://github.com/confighubai/public/blob/main/plugin/bridge-workers/impl/custom_functions.go

```
	fh.RegisterFunction("echo", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "echo",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "resource",
				Description: "Return the same data as input",
				OutputType:  api.OutputTypeYAML,
			},
			Mutating:    true,
			Validating:  false,
			Hermetic:    true,
			Idempotent:  true,
			Description: "Echo is to demonstrate that a custom function can be registered via the worker model.",
		},
		Function: k8sFnEcho,
	})
```

There are many more examples here:
https://github.com/confighubai/public/tree/main/plugin/functions/internal/handlers/kubernetes

Function names, parameter names, and result names are expected to be in `kabob-case`, for consistency with the CLI.

This is the FunctionSignature type, defined as part of the [function API](https://github.com/confighubai/public/blob/main/plugin/functions/pkg/api/function.go):

```
// FunctionSignature specifies the parameter names and values, required and optional parameters,
// OutputType, kind of function (mutating/readonly or validating), and description of the function.
type FunctionSignature struct {
	FunctionName       string              `description:"Name of the function in kabob-case"`
	Parameters         []FunctionParameter `description:"Function parameters, in order"`
	RequiredParameters int                 `description:"Number of required parameters"`
	VarArgs            bool                `description:"Last parameter may be repeated"`
	OutputInfo         *FunctionOutput     `description:"Output description"`
	Mutating           bool                `description:"May change the configuration data"`
	Validating         bool                `description:"Returns ValidationResult"`
	Hermetic           bool                `description:"Does not call other systems"`
	Idempotent         bool                `description:"Will return the same result if invoked again"`
	Description        string              `description:"Description of the function"`
}

// FunctionParameter specifies the parameter name, description, required vs optional, and DataType.
type FunctionParameter struct {
	ParameterName string   `description:"Name of the parameter in kabob-case"`
	Description   string   `description:"Description of the parameter"`
	Required      bool     `description:"Whether the parameter is required"`
	DataType      DataType `swaggertype:"string" description:"Data type of the parameter"`
}

// FunctionOutput specifies the name and description of the result and its OutputType.
type FunctionOutput struct {
	ResultName  string     `description:"Name of the result in kabob-case"`
	Description string     `description:"Description of the result"`
	OutputType  OutputType `swaggertype:"string" description:"Data type of the JSON embedded in the output"`
}
```

Parameter types may be scalar types, some JSON types defined in the [function API](https://github.com/confighubai/public/blob/main/plugin/functions/pkg/api/function.go), YAML, and selected other well defined types. The type enables proper decoding and validation by the handler. Rather than pass int and bool parameter values as the corresponding JSON types, the CLI passes them as strings and specifies `CastStringArgsToScalars: true` so that the function executor handler knows it should convert them prior to invoking the functions.

```
	DataTypeNone                 = DataType("")
	DataTypeString               = DataType("string")
	DataTypeInt                  = DataType("int")
	DataTypeBool                 = DataType("bool")
	DataTypeAttributeValueList   = DataType("AttributeValueList")
	DataTypePatchMap             = DataType("PatchMap")
	DataTypeJSON                 = DataType("JSON")
	DataTypeYAML                 = DataType("YAML")
	DataTypeCEL                  = DataType("CEL")
	DataTypeResourceMutationList = DataType("ResourceMutationList")
```

Outputs, on the other hand, are expected to be embedded JSON. Several well known types are defined in the function API. Some of these are also supported as parameter types above.

```
	OutputTypeValidationResult     = OutputType("ValidationResult")
	OutputTypeValidationResultList = OutputType("ValidationResultList")
	OutputTypeAttributeValueList   = OutputType("AttributeValueList")
	OutputTypeResourceInfoList     = OutputType("ResourceInfoList")
	OutputTypeResourceList         = OutputType("ResourceList")
	OutputTypePatchMap             = OutputType("PatchMap")
	OutputTypeCustomJSON           = OutputType("JSON")
	OutputTypeYAML                 = OutputType("YAML")
	OutputTypeOpaque               = OutputType("Opaque")
	OutputTypeResourceMutationList = OutputType("ResourceMutationList")
```

## The function API

The function API was mentioned several times:
https://github.com/confighubai/public/blob/main/plugin/functions/pkg/api/function.go

The invocation request is currently:

```
type FunctionInvocationRequest struct {
	FunctionContext
	ConfigData               []byte                 `swaggertype:"string" format:"byte" description:"Configuration data to operate on"`
	CastStringArgsToScalars  bool                   `description:"If true, expect integer and boolean arguments to be passed as strings"`
	NumFilters               int                    `description:"Number of validating functions to treat as filters: stop, but don't report errors"`
	StopOnError              bool                   `description:"If true, stop executing functions on the first error"`
	CombineValidationResults bool                   `description:"If true, return a single ValidationResult for validating functions rather than a ValidationResultList"`
	FunctionInvocations      FunctionInvocationList `description:"List of functions to invoke and their arguments"`
}

type FunctionInvocation struct {
	FunctionName string             `description:"Function name"`
	Arguments    []FunctionArgument `description:"Function arguments"`
}

type FunctionArgument struct {
	ParameterName string `json:",omitempty" description:"Name of parameter corresponding to this argument; optional: if not specified, expected to be in order"`
	Value         any    `description:"Argument value; must be a Scalar type, currently string, int, or bool"`
	// DataType is not needed here because it's in the function signature
}
```

The FunctionContext contains the UnitID, SpaceID, etc.

The response generated by the handler is currently:

```
// A FunctionInvocationResponse is returned by the function executor in response to a
// FunctionInvocationRequest. It contains the potentially modified configuration data,
// any output produced by read-only and/or validation functions, whether the function
// sequence executed successfully, and any error messages returned.
// Output of compatible OutputTypes is combined, and otherwise the first output is
// returned. For instance, a sequence of validation functions will have their outputs
// combined into a single ValidationResult, multiple AttributeValueLists will be appended
// together, and multiple ResourceInfoLists will be appended together.
type FunctionInvocationResponse struct {
	OrganizationID uuid.UUID            `description:"ID of the Unit's Organization"`
	SpaceID        uuid.UUID            `description:"ID of the Unit's Space"`
	UnitID         uuid.UUID            `description:"ID of the Unit the configuration data is associated with"`
	RevisionID     uuid.UUID            `description:"ID of the Revision the configuration data is associated with"`
	ConfigData     []byte               `swaggertype:"string" format:"byte" description:"The resulting configuration data, potentially mutated"`
	Output         []byte               `swaggertype:"string" format:"byte" description:"Output other than config data, as embedded JSON"`
	OutputType     OutputType           `swaggertype:"string" description:"Type of structured function output, if any"`
	Success        bool                 `description:"True if all functions executed successfully"`
	Mutations      ResourceMutationList `description:"List of mutations in the same order as the resources in ConfigData"`
	Mutators       []int                `description:"List of function invocation indices that resulted in mutations"`
	ErrorMessages  []string             `description:"Error messages from function execution; will be empty if Success is true"`
}
```

Functions just need to return the configuration data and output of the right format, and the handler takes care of the rest.

## Local testing

To test your functions locally, there's a simple CLI, fctl:
https://github.com/confighubai/public/tree/main/plugin/functions/cmd/fctl

For example:

```
fctl do deployment.yaml "MyDeploymentUnit" set-replicas 5
```

And a test script that exercises all of the currently implemented functions:
https://github.com/confighubai/public/blob/main/plugin/functions/cmd/fctl/manual-test.sh
