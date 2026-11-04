// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/xeipuuv/gojsonschema"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

const configMapResourceType = api.ResourceType("v1/ConfigMap")

func registerVetEnv(fh handler.FunctionRegistry, rp *k8skit.K8sResourceProviderType) {
	resourceTypes := append([]api.ResourceType{configMapResourceType}, k8skit.ContainerResourceTypes()...)
	if err := fh.RegisterFunction("vet-env", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-env",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "schema",
					Required:      true,
					Description:   "JSON Schema of the environment variables, an object whose properties are the variable names, as vet-jsonschema takes for an AppConfig/Env Unit",
					DataType:      api.DataTypeString,
					Example:       `{"type": "object", "properties": {"LOG_LEVEL": {"enum": ["debug", "info"]}}}`,
				},
				{
					ParameterName:    "container-name",
					Required:         false,
					Description:      "Name of the container whose env vars to check; * or omitted checks every container",
					DataType:         api.DataTypeString,
					Example:          "main",
					ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(containerNameRegexpString)},
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if every ConfigMap's data and every container's env vars pass the schema, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Validates the data of each ConfigMap, and the literal env var values of each container, against a JSON Schema of environment variables. ConfigMaps annotated with confighub.com/ConfigMapFormat: File are skipped. The schema's top-level required properties are not checked, since one ConfigMap or container need not carry every variable; only the names of env vars set with valueFrom are checked, since their values are elsewhere.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: resourceTypes,
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return k8sFnVetEnv(rp, fArgs.Options, fArgs.ParsedData, fArgs.Arguments)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// envVars is one set of variables validated together: a ConfigMap's data or a container's env.
type envVars struct {
	// basePath is where the variables are, reported for a violation no single variable has.
	basePath string
	// valueAt returns the path of one variable's value, and the value.
	valueAt func(name string) (string, any)
	// nameAt returns the path of one variable's name, and what is there, for a violation of
	// the name rather than the value, such as a variable the schema does not allow.
	nameAt func(name string) (string, any)
	// values holds the variables whose values are here.
	values map[string]any
	// valueFrom holds the variables whose values are elsewhere. Only their names are checked.
	valueFrom map[string]bool
}

func k8sFnVetEnv(rp *k8skit.K8sResourceProviderType, options *api.FunctionOptions, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	schemaJSON, ok := args[0].Value.(string)
	if !ok {
		return parsedData, api.ValidationResultFalse, errors.New("schema must be a string")
	}
	containerName := "*"
	if len(args) > 1 {
		if name, ok := args[1].Value.(string); ok && name != "" {
			containerName = name
		}
	}
	schema, err := envSchema(schemaJSON)
	if err != nil {
		return parsedData, api.ValidationResultFalse, err
	}

	details := []string{}
	failedAttributes := api.AttributeValueList{}
	_, err = yamlkit.VisitResourcesFiltered(parsedData, nil, rp, options, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		var sets []envVars
		if resourceInfo.ResourceType == configMapResourceType {
			if isFileConfigMap(doc) {
				return output, nil
			}
			sets = []envVars{configMapEnvVars(doc)}
		} else {
			sets = containerEnvVars(doc, resourceInfo.ResourceType, containerName)
		}
		var errs []error
		for _, vars := range sets {
			setDetails, setFailures, err := validateEnvVars(schema, vars, resourceInfo)
			if err != nil {
				errs = append(errs, errors.Wrapf(err, "validation error for resource %s/%s", resourceInfo.ResourceType, resourceInfo.ResourceName))
				continue
			}
			details = append(details, setDetails...)
			failedAttributes = append(failedAttributes, setFailures...)
		}
		return output, errs
	})
	if err != nil {
		return parsedData, api.ValidationResultFalse, err
	}
	if len(failedAttributes) == 0 {
		return parsedData, api.ValidationResultTrue, nil
	}
	result := api.ValidationResultFalse
	result.Details = details
	result.FailedAttributes = failedAttributes
	return parsedData, result, nil
}

// envSchema compiles the schema without its top-level required properties: a ConfigMap or a
// container usually carries some of an application's variables, with the rest coming from a
// Secret, another ConfigMap or the image. Nested required keywords are left in place.
func envSchema(schemaJSON string) (*gojsonschema.Schema, error) {
	var schemaObject map[string]any
	if err := json.Unmarshal([]byte(schemaJSON), &schemaObject); err != nil {
		return nil, errors.Wrap(err, "failed to parse schema JSON")
	}
	delete(schemaObject, "required")
	schema, err := gojsonschema.NewSchema(gojsonschema.NewGoLoader(schemaObject))
	if err != nil {
		return nil, errors.Wrap(err, "invalid schema")
	}
	return schema, nil
}

// isFileConfigMap reports whether a ConfigMap is annotated as holding files, such as one
// rendered from an AppConfig Unit to be mounted, rather than variables. Those are not checked.
func isFileConfigMap(doc *gaby.YamlDoc) bool {
	formatPath := k8skit.K8sContextPath(constants.ConfigMapFormatKeySuffix)
	format, found, _ := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(formatPath), true)
	return found && format == constants.ConfigMapFormatFile
}

// configMapEnvVars returns a ConfigMap's data. binaryData is left out: it does not hold
// variables a schema of strings describes.
func configMapEnvVars(doc *gaby.YamlDoc) envVars {
	values := map[string]any{}
	if data, ok := doc.Path("data").Data().(map[string]any); ok {
		maps.Copy(values, data)
	}
	keyAt := func(name string) (string, any) {
		return "data." + yamlkit.EscapeDotsInPathSegment(name), values[name]
	}
	return envVars{
		basePath: "data",
		valueAt:  keyAt,
		nameAt:   keyAt,
		values:   values,
	}
}

// containerEnvVars returns the env of each selected container that has one. A variable with
// neither value nor valueFrom is the empty string, as it is to Kubernetes, and when a name
// repeats, the last entry wins, again as it does to Kubernetes.
func containerEnvVars(doc *gaby.YamlDoc, resourceType api.ResourceType, containerName string) []envVars {
	var sets []envVars
	for _, containersPath := range k8skit.ContainerArrayPaths(resourceType) {
		containers, found, err := yamlkit.YamlSafePathGetDoc(doc, api.ResolvedPath(containersPath), true)
		if !found || err != nil {
			continue
		}
		for _, container := range containers.Children() {
			name, ok := container.Path("name").Data().(string)
			if !ok || (containerName != "*" && name != containerName) {
				continue
			}
			env := container.Path("env")
			if env == nil || len(env.Children()) == 0 {
				continue
			}
			containerPath := containersPath + ".?name=" + yamlkit.EscapeDotsInPathSegment(name)
			values := map[string]any{}
			valueFrom := map[string]bool{}
			for _, entry := range env.Children() {
				varName, ok := entry.Path("name").Data().(string)
				if !ok {
					continue
				}
				if entry.Exists("valueFrom") {
					delete(values, varName)
					valueFrom[varName] = true
					continue
				}
				value := entry.Path("value").Data()
				if value == nil {
					value = ""
				}
				values[varName] = value
				delete(valueFrom, varName)
			}
			entryPath := func(varName string) string {
				return containerPath + ".env.?name=" + yamlkit.EscapeDotsInPathSegment(varName)
			}
			sets = append(sets, envVars{
				basePath:  containerPath + ".env",
				valueAt:   func(varName string) (string, any) { return entryPath(varName) + ".value", values[varName] },
				nameAt:    func(varName string) (string, any) { return entryPath(varName) + ".name", varName },
				values:    values,
				valueFrom: valueFrom,
			})
		}
	}
	return sets
}

// contextDelimiter separates the segments of a violation's location. Variable names may
// contain dots, which gojsonschema otherwise uses.
const contextDelimiter = "\x00"

// validateEnvVars checks the variables whose values are here against the whole schema, and
// the names of the ones set with valueFrom against what the schema says of names. A valueFrom
// variable is otherwise absent: any placeholder for its value could fail a keyword that looks
// at values, such as an anyOf at the root, which reports at the root rather than at the
// variable.
func validateEnvVars(schema *gojsonschema.Schema, vars envVars, resourceInfo *api.ResourceInfo) ([]string, api.AttributeValueList, error) {
	report := &envViolations{vars: vars, resourceInfo: resourceInfo}
	result, err := schema.Validate(gojsonschema.NewGoLoader(vars.values))
	if err != nil {
		return nil, nil, err
	}
	report.add(result.Errors(), nil)

	if len(vars.valueFrom) > 0 {
		names := map[string]any{}
		for name := range vars.valueFrom {
			names[name] = ""
		}
		result, err := schema.Validate(gojsonschema.NewGoLoader(names))
		if err != nil {
			return nil, nil, err
		}
		report.add(result.Errors(), func(violation gojsonschema.ResultError) bool {
			switch violation.Type() {
			case "additional_property_not_allowed", "invalid_property_name":
				return true
			}
			return false
		})
	}
	return report.details, report.failed, nil
}

// envViolations collects the violations of one set of variables.
type envViolations struct {
	vars         envVars
	resourceInfo *api.ResourceInfo
	details      []string
	failed       api.AttributeValueList
	// nameDetail is the index in details of the last invalid_property_name violation, which
	// the propertyNames subschema's own violations that follow it explain.
	nameDetail int
}

// add reports each violation that keep, if not nil, keeps.
func (r *envViolations) add(violations []gojsonschema.ResultError, keep func(gojsonschema.ResultError) bool) {
	r.nameDetail = -1
	for _, violation := range violations {
		// The first segment is gojsonschema's (root); the second, if any, is the variable
		// whose value is in violation.
		segments := strings.Split(violation.Context().String(contextDelimiter), contextDelimiter)
		property, hasProperty := violation.Details()["property"].(string)
		if len(segments) == 1 && !hasProperty {
			if _, isName := violation.Value().(string); isName {
				// propertyNames validates each name at the root, so the subschema's own
				// violations are there too, with the name as their value. The
				// invalid_property_name violation before them already reports the name; they
				// say why.
				if r.nameDetail >= 0 {
					r.details[r.nameDetail] += ": " + violation.Description()
				}
				continue
			}
		}
		if keep != nil && !keep(violation) {
			continue
		}

		path := r.vars.basePath
		var value any
		// A violation of no one variable, such as minProperties, is at the variables' map.
		dataType := api.DataTypeNone
		if len(segments) > 1 {
			path, value = r.vars.valueAt(segments[1])
			dataType = api.DataTypeString
		} else if hasProperty {
			// additionalProperties and propertyNames report a variable's name at the root.
			path, value = r.vars.nameAt(property)
			dataType = api.DataTypeString
		}
		if violation.Type() == "invalid_property_name" {
			r.nameDetail = len(r.details)
		}
		r.details = append(r.details, fmt.Sprintf("Resource %s/%s: %s: %s", r.resourceInfo.ResourceType, r.resourceInfo.ResourceName, path, violation.Description()))
		r.failed = append(r.failed, api.AttributeValue{
			AttributeInfo: api.AttributeInfo{
				AttributeIdentifier: api.AttributeIdentifier{
					ResourceInfo: *r.resourceInfo,
					Path:         api.ResolvedPath(path),
				},
				AttributeMetadata: api.AttributeMetadata{AttributeName: api.AttributeNameNone, DataType: dataType},
			},
			Value: value,
		})
	}
}
