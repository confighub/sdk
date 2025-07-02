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

- https://github.com/confighub/sdk/tree/main/configkit

That library is built upon another Go library that provides traversal of and access to YAML configuration elements, here:

- https://github.com/confighub/sdk/tree/main/third_party/gaby

YAML paths in gaby and yamlkit are dot-separated, including for array indices. For example:

```
spec.template.spec.containers.0.image
```

Use `yamlkit.EscapeDotsInPathSegment` to escape map keys, such as Kubernetes annotations, which may have dots in them and `gaby.DotPathToSlice` to split a path into path segments to traverse them with gaby. Dots are escaped with `~1`. Functions you are likely to use include:

- YamlDoc.ExistsP(path string): Returns true if the specified path exists within the YAML document. Path segments containing dots must be escaped when in this form.
- YamlDoc.Path(path string): Returns the YAML document at the specified path if it exists.
- YamlDoc.SetP(value any, path string): Sets the value at the specified path, creating intermediate objects as necessary.
- YamlDoc.DeleteP(path string): Deletes the element at the specified path.
- YamlDoc.ChildrenMap(): Returns a map of YAML documents if the YAML document is an object.
- YamlDoc.Children(): Returns a slice of YAML documents if the YAML document is an array or object.
- YamlDoc.Data(): Returns the parsed data value. Useful for getting primitive leaf values.
- YamlDoc.Bytes(): Marshals the YAML document.
- ParseYAML(): Unmarshals a YAML document into a YamlDoc.

gaby is layered on top of the [kustomize kyaml library](https://github.com/kubernetes-sigs/kustomize/tree/master/kyaml), so it preserves field order and comments.

The Kubernetes API and cloud APIs contain a number of associative lists, such as container lists and environment variable lists, where the keys identifying the array elements are map elements of the array element, such as `name`. yamlkit has a function for resolving associative paths to array index syntax, `ResolveAssociativePaths`. The syntax for an associative list path lookup is `.?<map key>:<parameter name>=<map value>`, as in `spec.template.spec.containers.?name:container-name=%s.image` (using a Sprintf placeholder). The `:<parameter name>` is optional, but is used to match corresponding values.

`ResolveAssociativePaths` also supports wildcards. `*` is the simplest form of wildcard. As with associative matches, matched segments may be bound to parameter names. The syntax is `.*?<map key>:<getter parameter name>`, as in `spec.template.spec.containers.*?name:container-name`. When the value substituted into an associative list lookup is `*`, `ResolveAssociativePaths` automatically converts the path expression into the wildcard form.

`ResolveAssociativePaths` can also bind map keys to parameters, using `.@<map key>:<parameter name>`, for a specific key, or `.*@:<parameter name>` for any key.

`ResolveAssociativePaths` supports path existence checking using the `.|` syntax. When a path segment is prefixed with `|`, the path resolution requires that the preceding path exists up to that point, but allows the current segment to be created if it doesn't exist. For example, `spec.template.spec.containers.0.|securityContext` will resolve if the `containers.0` path exists, regardless of whether `securityContext` exists. This is useful for conditional operations where you want to ensure a parent structure exists before creating or modifying child elements.

### Resource traversal

Kubernetes resources are currently stored in Units as lists of YAML documents. Kubernetes functions mostly work the same as functions on any arbitrary YAML. k8skit implements an interface to enable extracting the resource type and name from each document, `K8sResourceProvider`.

https://github.com/confighub/sdk/tree/main/configkit/k8skit

The resource type is in the format group/version/kind. The resource name is in the format namespace/name, where the namespace segment is empty if the namespace isn't present, including in the case of cluster-scoped resources.

The yamlkit function VisitResources iterates over resources in a unit.

An example of a function that iterates over resources is ResourceToDocMap:

```
// ResourceToDocMap returns a map of all resources in the provided list of parsed YAML
// documents to their document index.
func ResourceToDocMap(parsedData gaby.Container, resourceProvider ResourceProvider) (resourceMap ResourceInfoToDocMap, err error) {
	resourceMap = make(ResourceInfoToDocMap)
	if len(parsedData) == 0 {
		return resourceMap, nil
	}
	visitor := func(_ *gaby.YamlDoc, _ any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		resourceMap[*resourceInfo] = index
		return nil, []error{}
	}
	_, err = VisitResources(parsedData, nil, resourceProvider, visitor)
	return resourceMap, err
}
```

### Path traversal

As in Kustomize, many mutating and readonly functions operate on specific paths in specific Kubernetes resource types. We're working on making the definition of paths dynamically extensible.

yamlkit provides a set of visitor functions to make these path traversals straightforward. VisitPathsDoc is the main path visitor function. There are some more specific convenience functions layered on top of them: UpdatePathsFunction, UpdatePathsValue, GetPaths, GetPathsAnyType, UpdateStringPathsFunction, UpdateStringPaths, GetStringPaths, etc.

Here's the code for `set-string-path`, which sets a string value at the specified YAML path:

```
func GenericFnSetStringPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte, upsert bool) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)
	value := args[2].Value.(string)

	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	err := yamlkit.UpdateStringPaths(parsedData, resourceTypeToPaths, []any{}, resourceProvider, value, upsert)
	return parsedData, nil, err
}
```

### Path registry

A number of functions are as simple as just registering the right YAML paths for the visitor functions. You could use a function like `set-int-path` and specify the YAML path as a function argument, but if you operate on the same path(s) frequently, it can be worthwhile to define a function so that your users don't need to type or remember the paths.

`generic.RegisterPathSetterAndGetter` will create setter and getter functions corresponding to a set of paths.

As an example, here's the code for `set-replicas`:

```
	minValue := 0
	replicasParameters := []api.FunctionParameter{
		{
			ParameterName:    "replicas",
			Required:         true,
			Description:      "Number of replicas of workload controllers",
			DataType:         api.DataTypeInt,
			Example:          "3",
			ValueConstraints: api.ValueConstraints{Min: &minValue},
		},
	}
	generic.RegisterPathSetterAndGetter(fh, "replicas", replicasParameters,
		" the replicas for workload controllers", attributeNameReplicas, k8skit.K8sResourceProvider, true, false)
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
		// Arguments will be added during traversal
	}
	replicasSetterFunctionInvocation := &api.FunctionInvocation{
		FunctionName: "set-replicas",
		// Arguments will be added during traversal
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
			k8skit.K8sResourceProvider,
			attributeNameReplicas,
			resourceType,
			pathInfos,
			replicasGetterFunctionInvocation,
			replicasSetterFunctionInvocation,
			false,
		)
	}
```

In the future we expect to provide a way to define these sets of paths dynamically.

## Registering functions

Functions you add here can be built into the worker in this repo, or built into your own worker.

There's an example here:
https://github.com/confighub/sdk/tree/main/examples/hello-world-function

```
	executor.RegisterFunction(workerapi.ToolchainKubernetesYAML, handler.FunctionRegistration{
		FunctionSignature: GetHelloWorldFunctionSignature(),
		Function:          HelloWorldFunction,
	})
...
func GetHelloWorldFunctionSignature() api.FunctionSignature {
	return api.FunctionSignature{
		FunctionName: "hello-world",
		Parameters: []api.FunctionParameter{
			{
				ParameterName: "greeting",
				Description:   "The greeting message to add to the configuration",
				Required:      true,
				DataType:      api.DataTypeString,
				Example:       "Hello from ConfigHub!",
			},
		},
		RequiredParameters: 1,
		VarArgs:            false, // This function doesn't accept variable arguments
		OutputInfo: &api.FunctionOutput{
			ResultName:  "modified-config",
			Description: "Configuration with greeting annotation added",
			OutputType:  api.OutputTypeYAML,
		},
		Mutating:              true,  // This function modifies the configuration
		Validating:            false, // This function doesn't validate (return pass/fail)
		Hermetic:              true,  // This function doesn't call external systems
		Idempotent:            true,  // Running this function multiple times has the same effect
		Description:           "Adds a greeting message as an annotation to the first Kubernetes resource",
		FunctionType:          api.FunctionTypeCustom,
		AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny}, // Works on any resource type
	}
}
```

There are many more examples here:
https://github.com/confighub/sdk/tree/main/function/internal/handlers/kubernetes

Function names, parameter names, and result names are expected to be in `kabob-case`, for consistency with the CLI.

This is the FunctionSignature type, defined as part of the [function API](https://github.com/confighub/sdk/tree/main/function/api):

```
// FunctionSignature specifies the parameter names and values, required and optional parameters,
// OutputType, kind of function (mutating/readonly or validating), and description of the function.
type FunctionSignature struct {
	FunctionName          string              `description:"Name of the function in kabob-case"`
	Parameters            []FunctionParameter `description:"Function parameters, in order"`
	RequiredParameters    int                 `description:"Number of required parameters"`
	VarArgs               bool                `description:"Last parameter may be repeated"`
	OutputInfo            *FunctionOutput     `description:"Output description"`
	Mutating              bool                `description:"May change the configuration data"`
	Validating            bool                `description:"Returns ValidationResult"`
	Hermetic              bool                `description:"Does not call other systems"`
	Idempotent            bool                `description:"Will return the same result if invoked again"`
	Description           string              `description:"Description of the function"`
	FunctionType          FunctionType        `swaggertype:"string" description:"Implementation pattern of the function: PathVisitor or Custom"`
	AttributeName         AttributeName       `json:",omitempty" swaggertype:"string" description:"Attribute corresponding to registered paths, if a path visitor; optional"`
	AffectedResourceTypes []ResourceType      `json:",omitempty" description:"Resource types the function applies to; * if all"`
}

// FunctionParameter specifies the parameter name, description, required vs optional, data type, and example.
type FunctionParameter struct {
	ParameterName string   `description:"Name of the parameter in kabob-case"`
	Description   string   `description:"Description of the parameter"`
	Required      bool     `description:"Whether the parameter is required"`
	DataType      DataType `swaggertype:"string" description:"Data type of the parameter"`
	Example       string   `json:",omitempty" description:"Example value"`
	ValueConstraints
}

// ValueConstraints specifies constraints on a parameter's value.
type ValueConstraints struct {
	Regexp     string   `json:",omitempty" description:"Regular expression matching valid values; applies to string parameters"`
	Min        *int     `json:",omitempty" description:"Minimum allowed value; applies to int parameters"`
	Max        *int     `json:",omitempty" description:"Maximum allowed value; applies to int parameters"`
	EnumValues []string `json:",omitempty" description:"List of valid enum values; applies to enum parameters"`
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
const (
	DataTypeNone                 = DataType("")
	DataTypeString               = DataType("string")
	DataTypeInt                  = DataType("int")
	DataTypeBool                 = DataType("bool")
	DataTypeEnum                 = DataType("enum")
	DataTypeAttributeValueList   = DataType("AttributeValueList")
	DataTypePatchMap             = DataType("PatchMap")
	DataTypeJSON                 = DataType("JSON")
	DataTypeYAML                 = DataType("YAML")
	DataTypeProperties           = DataType("Properties")
	DataTypeTOML                 = DataType("TOML")
	DataTypeINI                  = DataType("INI")
	DataTypeEnv                  = DataType("Env")
	DataTypeHCL                  = DataType("HCL")
	DataTypeCEL                  = DataType("CEL")
	DataTypeResourceMutationList = DataType("ResourceMutationList")
	DataTypeResourceList         = DataType("ResourceList")
)
```

Outputs, on the other hand, are expected to be embedded JSON. Several well known types are defined in the function API. Some of these are also supported as parameter types above.

```
const (
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
)
```

## The function API

The function API was mentioned several times:
https://github.com/confighub/sdk/tree/main/function/api

The invocation request is currently:

```
type FunctionInvocationRequest struct {
	FunctionContext
	ConfigData               []byte                 `swaggertype:"string" format:"byte" description:"Configuration data of the Unit to operate on"`
	LiveState                []byte                 `swaggertype:"string" format:"byte" description:"The most recent live state of the Unit as reported by the bridge worker associated with the Target attached to the Unit."`
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
https://github.com/confighub/sdk/tree/main/cmd/fctl

For example:

```
fctl do deployment.yaml "MyDeploymentUnit" set-replicas 5
```

And a test script that exercises all of the currently implemented functions:
https://github.com/confighub/sdk/blob/main/cmd/fctl/manual-test.sh

That set of tests can be run with `make manual-test`.

Any new functions added to the SDK should have at least one test case added. Correctness is currently verified manually by reviewing the output. The output is then captured in the `golden-output` subdirectory for comparison with test runs. The golden outputs can be updated by `DIR=golden-output make manual-test`. The `diff` commands only report mismatches by default, but `QUIET=no make manual-test` will cause them to output any diffs. In addition to new test cases, test-data changes, and function behavioral changes, the addition of new functions, paths, and/or output structure fields require the golden outputs to be updated.

## Configuration formats other than Kubernetes/YAML

There is nascent support for other configuration formats:

- Java Properties files: AppConfig/Properties
- OpenTofu: OpenTofu/HCL

Other formats are converted to and from YAML documents using the `configkit.ConfigConverter` interface so that the `yamlkit` and `gaby` libraries may be used to traverse and manipulate the configuration data, and so that a set of common / standard functions may be implemented in a generic way for all configuration formats. These functions are here:

https://github.com/confighub/sdk/tree/main/function/internal/handlers/generic

They are registered with a converter and a `ResourceProvider`, which may be implemented using the same receiver type, as done for Kubernetes:

```
	generic.RegisterStandardFunctions(fh, k8skit.K8sResourceProvider, k8skit.K8sResourceProvider)
```

The `ResourceProvider` interface is used to interact with configuration element metadata, such as resource categories, types, and names. The `ResourceCategory` identifies what kind of configuration element it is. Kubernetes only contains elements of category `api.ResourceCategoryResource`. Java Properties only contains elements of category `api.ResourceCategoryAppConfig`. OpenTofu contains elements of categories `api.ResourceCategoryResource` and `api.ResourceCategoryDyanmicData`, which corresponds to [data sources](https://opentofu.org/docs/language/data-sources/).
