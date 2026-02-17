// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/join"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
	"github.com/labstack/gommon/log"
)

// ResourceVisitorFunc defines the signature of functions invoked by the resource visitor function.
type ResourceVisitorFunc func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error)

// VisitResources iterates over all of the resources/elements in a configuration unit
// and passes metadata about the resource as well as the document itself to a visitor function.
func VisitResources(parsedData gaby.Container, output any, resourceProvider ResourceProvider, visitor ResourceVisitorFunc) (any, error) {
	multiErrs := []error{}
	for index, doc := range parsedData {
		resourceInfo, err := GetResourceInfo(doc, resourceProvider)
		if err != nil {
			multiErrs = append(multiErrs, errors.Wrap(err, fmt.Sprintf("error in resource/element %d", index)))
			continue
		}
		newOutput, errs := visitor(doc, output, index, resourceInfo)
		if len(errs) != 0 {
			multiErrs = append(multiErrs, errs...)
		} else {
			output = newOutput
		}
	}
	if len(multiErrs) != 0 {
		err := errors.WithStack(join.Join(multiErrs...))
		log.Debugf("VisitResources errors: %v", err)
		return output, err
	}
	return output, nil
}

// VisitorContext contains information passed to visitor functions for each path traversed.
type VisitorContext struct {
	api.AttributeInfo // includes Path and Info
	Arguments         []api.FunctionArgument
	EmbeddedPath      string
	Accessor          EmbeddedAccessor
}

func attributeValueCompareFunction(attributeValue []api.AttributeValue) func(int, int) bool {
	return func(i int, j int) bool {
		return attributeValue[i].ResourceType < attributeValue[j].ResourceType ||
			(attributeValue[i].ResourceType == attributeValue[j].ResourceType &&
				(attributeValue[i].ResourceName < attributeValue[j].ResourceName ||
					(attributeValue[i].ResourceName == attributeValue[j].ResourceName &&
						attributeValue[i].Path < attributeValue[j].Path)))
	}
}

// VisitorFunc defines the signature of functions invoked by the visitor functions.
type VisitorFunc[T api.Scalar] func(doc *gaby.YamlDoc, output any, context VisitorContext, currentValue T) (any, error)

// VisitPaths is a simple wrapper of the base visitor function. It traverses the
// specified path patterns of the specified resource types within the parsed configuration
// YAML document list.
func VisitPaths[T api.Scalar](
	parsedData gaby.Container,
	resourceTypeToPaths api.ResourceTypeToPathToVisitorInfoType,
	keys []any,
	output any,
	resourceProvider ResourceProvider,
	visitor VisitorFunc[T],
	upsert bool,
) (any, error) {
	docVisitor := func(doc *gaby.YamlDoc, output any, context VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		if currentDoc == nil {
			// Handle nil currentDoc in upsert mode by providing default values
			var defaultValue T
			switch any(defaultValue).(type) {
			case string:
				// PlaceHolderBlockApplyString is not passed because some functions may want to set that as a value
				return visitor(doc, output, context, any("").(T))
			case int:
				// Use negative value to distinguish from 0
				return visitor(doc, output, context, any(-PlaceHolderBlockApplyInt).(T))
			case bool:
				// Use false since there's not a better option
				return visitor(doc, output, context, any(false).(T))
			default:
				return output, fmt.Errorf("unsupported type %T for upsert with nil currentDoc at path %s", defaultValue, string(context.Path))
			}
		}
		currentValue, ok := currentDoc.Data().(T)
		if ok {
			return visitor(doc, output, context, currentValue)
		}
		return output, fmt.Errorf("value %v at path %s cannot be converted to %T", currentDoc.Data(), string(context.Path), currentValue)
	}
	return VisitPathsDoc(parsedData, resourceTypeToPaths, keys, output, resourceProvider, docVisitor, upsert)
}

// VisitorFuncAnyType defines the signature of functions invoked by the visitor functions.
type VisitorFuncAnyType func(doc *gaby.YamlDoc, output any, context VisitorContext, currentValue any) (any, error)

// VisitPathsAnyType is a simple wrapper of the base visitor function. It traverses the
// specified path patterns of the specified resource types within the parsed configuration
// YAML document list.
func VisitPathsAnyType(
	parsedData gaby.Container,
	resourceTypeToPaths api.ResourceTypeToPathToVisitorInfoType,
	keys []any,
	output any,
	resourceProvider ResourceProvider,
	visitor VisitorFuncAnyType,
	upsert bool,
) (any, error) {
	docVisitor := func(doc *gaby.YamlDoc, output any, context VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		return visitor(doc, output, context, currentDoc.Data())
	}
	return VisitPathsDoc(parsedData, resourceTypeToPaths, keys, output, resourceProvider, docVisitor, upsert)
}

// VisitorFuncDoc defines the signature of functions invoked by the visitor function.
type VisitorFuncDoc func(doc *gaby.YamlDoc, output any, context VisitorContext, currentDoc *gaby.YamlDoc) (any, error)

// VisitPathsDoc is the base visitor function. It traverses the
// specified path patterns of the specified resource types within the parsed configuration
// YAML document list.
func VisitPathsDoc(
	parsedData gaby.Container,
	resourceTypeToPaths api.ResourceTypeToPathToVisitorInfoType,
	keys []any,
	output any,
	resourceProvider ResourceProvider,
	visitor VisitorFuncDoc,
	upsert bool,
) (any, error) {

	resourceVisitor := func(doc *gaby.YamlDoc, output any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
		multiErrs := []error{}
		unresolvedPaths := api.PathToVisitorInfoType{}
		formatPaths, ok := resourceTypeToPaths[resourceInfo.ResourceType]
		if ok {
			for k, pathInfo := range formatPaths {
				unresolvedPaths[k] = pathInfo
			}
		}
		formatPaths, ok = resourceTypeToPaths[api.ResourceTypeAny]
		if ok {
			for k, pathInfo := range formatPaths {
				exception := false
				if pathInfo.TypeExceptions != nil {
					_, exception = pathInfo.TypeExceptions[resourceInfo.ResourceType]
				}
				if !exception {
					unresolvedPaths[k] = pathInfo
				}
			}
		}
		if len(unresolvedPaths) == 0 {
			// Skip resource types with no paths
			return output, multiErrs
		}
		for _, unresolvedPathInfo := range unresolvedPaths {
			unresolvedPath := unresolvedPathInfo.Path
			if len(keys) > 0 {
				unresolvedPath = api.UnresolvedPath(fmt.Sprintf(string(unresolvedPath), keys...))
				if strings.Contains(string(unresolvedPath), "EXTRA") {
					log.Debugf("path %s resolved to %s with excess keys", string(unresolvedPathInfo.Path), string(unresolvedPath))
				}
			}
			unresolvedPathSegments := strings.Split(string(unresolvedPath), "#")
			embeddedPath := ""
			if len(unresolvedPathSegments) > 1 {
				embeddedPath = strings.Join(unresolvedPathSegments[1:], "#")
			}
			pathConstraint := strings.Split(string(unresolvedPathInfo.ResolvedPath), "#")
			// If there's an embedded accessor (#), upsert should be passed as false to ResolveAssociativePaths
			resolveUpsert := upsert && embeddedPath == ""
			resolvedPaths, err := ResolveAssociativePaths(doc, api.UnresolvedPath(unresolvedPathSegments[0]), api.ResolvedPath(pathConstraint[0]), resolveUpsert)
			if err != nil {
				// Don't report the error. Not found is expected.
				continue // Skip if an error
			}
			for _, resolvedPath := range resolvedPaths {
				// log.Infof("resolved path %s args %v in resource %s of type %s", resolvedPath.Path, resolvedPath.PathArguments, string(resourceName), string(resourceType))
				currentDoc, found, err := YamlSafePathGetDoc(doc, resolvedPath.Path, true)
				if err != nil || (!found && !upsert) {
					// Don't report the error. Not found is expected.
					continue // Skip if not found or an error, unless in upsert mode
				}
				// In upsert mode, currentDoc will already be nil if !found
				context := VisitorContext{
					AttributeInfo: api.AttributeInfo{
						AttributeIdentifier: api.AttributeIdentifier{
							ResourceInfo: *resourceInfo,
							Path:         resolvedPath.Path,
						},
						AttributeMetadata: api.AttributeMetadata{
							AttributeName: unresolvedPathInfo.AttributeName,
							DataType:      unresolvedPathInfo.DataType,
							Info:          unresolvedPathInfo.Info,
						},
					},
					Arguments:    resolvedPath.PathArguments,
					EmbeddedPath: embeddedPath,
				}
				if unresolvedPathInfo.EmbeddedAccessorType != "" {
					context.Accessor, err = GetEmbeddedAccessor(unresolvedPathInfo.EmbeddedAccessorType,
						unresolvedPathInfo.EmbeddedAccessorConfig)
					if err != nil {
						multiErrs = append(multiErrs, err)
						// The same error will occur for all resolved paths
						break
					}
				}
				newOutput, err := visitor(doc, output, context, currentDoc)
				if err != nil {
					multiErrs = append(multiErrs, err)
				} else {
					output = newOutput
					// log.Infof("VisitPaths output for path %s of resource %s of type %s is %v", string(resolvedPath.Path), string(resourceName), string(resourceType), output)
				}
			}
		}
		return output, multiErrs
	}
	newOutput, err := VisitResources(parsedData, output, resourceProvider, resourceVisitor)
	return newOutput, err
}

// UpdatePathsFunction traverses the specified path patterns of the specified resource types.
// The updater function simply needs to return the new attribute value, which must be of the
// type of the generic type parameter.
func UpdatePathsFunction[T api.Scalar](
	parsedData gaby.Container,
	resourceTypeToPaths api.ResourceTypeToPathToVisitorInfoType,
	keys []any,
	resourceProvider ResourceProvider,
	updater func(T) T,
	upsert bool,
) error {

	visitor := func(doc *gaby.YamlDoc, output any, context VisitorContext, currentValue T) (any, error) {
		originalValue := currentValue
		newValue := updater(currentValue)
		var err error
		if newValue != originalValue || upsert {
			_, err = doc.SetP(newValue, string(context.Path))
		}
		return output, err
	}
	_, err := VisitPaths[T](parsedData, resourceTypeToPaths, keys, nil, resourceProvider, visitor, upsert)
	return err
}

// UpdatePathsValue traverses the specified path patterns of the specified resource types and
// updates the attributes with the provided value.
func UpdatePathsValue[T api.Scalar](
	parsedData gaby.Container,
	resourceTypeToPaths api.ResourceTypeToPathToVisitorInfoType,
	keys []any,
	resourceProvider ResourceProvider,
	newValue T,
	upsert bool,
) error {

	updater := func(_ T) T {
		return newValue
	}
	err := UpdatePathsFunction[T](parsedData, resourceTypeToPaths, keys, resourceProvider, updater, upsert)
	return err
}

// UpdatePathsFunctionDoc traverses the specified path patterns of the specified resource types.
// The updater function simply needs to return the new attribute value, which must be a YamlDoc.
func UpdatePathsFunctionDoc(
	parsedData gaby.Container,
	resourceTypeToPaths api.ResourceTypeToPathToVisitorInfoType,
	keys []any,
	resourceProvider ResourceProvider,
	updater func(*gaby.YamlDoc) *gaby.YamlDoc,
	upsert bool,
) error {

	visitor := func(doc *gaby.YamlDoc, output any, context VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		originalDoc := currentDoc
		newDoc := updater(currentDoc)
		var err error
		if upsert || (originalDoc != nil && newDoc.String() != originalDoc.String()) {
			_, err = doc.SetDocP(newDoc, string(context.Path))
		}
		return output, err
	}
	_, err := VisitPathsDoc(parsedData, resourceTypeToPaths, keys, nil, resourceProvider, visitor, upsert)
	return err
}

func appendFunctionInvocationArguments(sharedFunctionInvocation *api.FunctionInvocation, arguments []api.FunctionArgument) *api.FunctionInvocation {
	// Deep copy so that we don't append to the args repeatedly
	functionInvocation := *sharedFunctionInvocation
	functionInvocation.Arguments = make([]api.FunctionArgument,
		len(sharedFunctionInvocation.Arguments),
		len(sharedFunctionInvocation.Arguments)+len(arguments))
	copy(functionInvocation.Arguments, sharedFunctionInvocation.Arguments)
	functionInvocation.Arguments = append(functionInvocation.Arguments, arguments...)
	return &functionInvocation
}

func appendGetterAndSetterArguments(details *api.AttributeDetails, arguments []api.FunctionArgument) *api.AttributeDetails {
	if details == nil {
		return nil
	}
	if len(arguments) == 0 {
		return details
	}
	if details.GetterInvocation == nil && len(details.SetterInvocations) == 0 {
		return details
	}
	newDetails := *details
	if details.GetterInvocation != nil {
		newDetails.GetterInvocation = appendFunctionInvocationArguments(details.GetterInvocation, arguments)
	}
	if len(details.SetterInvocations) != 0 {
		newDetails.SetterInvocations = make([]api.FunctionInvocation, len(details.SetterInvocations))
		for i, _ := range details.SetterInvocations {
			newDetails.SetterInvocations[i] = *appendFunctionInvocationArguments(&details.SetterInvocations[i], arguments)
		}
	}
	return &newDetails
}

// TODO: Refactor the layer on top of the base visitors

// GetPaths traverses the specified path patterns of the specified resource types and returns
// an api.AttributeValueList containing the values and registered information about all of
// the found attributes matching the path patterns. Use only for int and bool attributes.
// Use GetStringPaths for string attributes.
func GetPaths[T api.Scalar](
	parsedData gaby.Container,
	resourceTypeToPaths api.ResourceTypeToPathToVisitorInfoType,
	keys []any,
	resourceProvider ResourceProvider,
) (api.AttributeValueList, error) {
	// Determine the data type based on the generic type parameter
	var dataType api.DataType
	var zero T
	switch any(zero).(type) {
	case int:
		dataType = api.DataTypeInt
	case bool:
		dataType = api.DataTypeBool
	default:
		// Invalid; strings supported in a dedicated function
		return nil, fmt.Errorf("type %T not supported", zero)
	}

	return GetPathsAnyType(parsedData, resourceTypeToPaths, keys, resourceProvider, dataType, false)
}

// GetPathsAnyType traverses the specified path patterns of the specified resource types and returns
// an api.AttributeValueList containing the values and registered information about all of
// the found attributes matching the path patterns.
func GetPathsAnyType(
	parsedData gaby.Container,
	resourceTypeToPaths api.ResourceTypeToPathToVisitorInfoType,
	keys []any,
	resourceProvider ResourceProvider,
	dataType api.DataType,
	neededValuesOnly bool,
) (api.AttributeValueList, error) {

	visitor := func(_ *gaby.YamlDoc, output any, context VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		attr := context.AttributeInfo
		var currentDataType api.DataType
		currentValue := currentDoc.Data()
		switch v := any(currentValue).(type) {
		case string:
			currentDataType = api.DataTypeString
			if context.EmbeddedPath != "" && context.Accessor != nil {
				embeddedValue, err := context.Accessor.Extract(v, context.EmbeddedPath)
				if err != nil {
					// Skip this path if the embedded field isn't found
					return output, nil
				}
				embeddedStringValue, ok := embeddedValue.(string)
				if !ok {
					// Skip this path if not a string
					return output, nil
				}
				// If the data isn't a string or the pattern wasn't matched, embeddedValue should be empty
				currentValue = embeddedStringValue
				attr.Path = api.ResolvedPath(string(attr.Path) + "#" + context.EmbeddedPath)
			}
		case int:
			currentDataType = api.DataTypeInt
		case bool:
			currentDataType = api.DataTypeBool
		default:
			// Invalid; strings supported in a dedicated function
			return output, fmt.Errorf("type %T not supported", v)
		}

		// Apply type filtering based on dataType parameter
		if dataType != api.DataTypeNone && dataType != currentDataType {
			return output, fmt.Errorf("value %v at path %s is of type %s but expected %s", currentValue, string(context.Path), currentDataType, dataType)
		}

		// Apply needed values filtering if requested
		if neededValuesOnly {
			switch currentDataType {
			case api.DataTypeString:
				if stringVal, ok := currentValue.(string); ok &&
					!strings.Contains(stringVal, PlaceHolderBlockApplyString) {
					return output, nil // skip if there's already a value
				}
			case api.DataTypeInt:
				if intVal, ok := currentValue.(int); ok && intVal != PlaceHolderBlockApplyInt {
					return output, nil // skip if there's already a value
				}
			case api.DataTypeBool:
				// No placeholder for bool
			}
		}

		attr.DataType = currentDataType

		visitorValues, ok := output.([]api.AttributeValue)
		if !ok {
			log.Debugf("couldn't convert output to []api.AttributeValue{}")
			return output, fmt.Errorf("internal error") // TODO: define an error type
		}
		var attributeValue api.AttributeValue
		comment := currentDoc.GetComments()
		attributeValue = api.AttributeValue{AttributeInfo: attr, Value: currentValue, Comment: comment}
		attributeValue.Info = appendGetterAndSetterArguments(attributeValue.Info, context.Arguments)
		visitorValues = append(visitorValues, attributeValue)
		return visitorValues, nil
	}
	values := []api.AttributeValue{}
	output, err := VisitPathsDoc(parsedData, resourceTypeToPaths, keys, values, resourceProvider, visitor, false)
	if err != nil {
		return values, err
	}
	values, ok := output.([]api.AttributeValue)
	if !ok {
		log.Debugf("couldn't convert output to []api.AttributeValue{}")
		return values, fmt.Errorf("internal error") // TODO: define an error type
	}
	// TODO: Revisit. Did this for predictable order.
	sort.Slice(values, attributeValueCompareFunction(values))
	return values, nil
}

// GetNeededPaths traverses the specified path patterns of the specified resource types and returns
// an api.AttributeValueList containing the values and registered information about all of
// the found attributes matching the path patterns that Need values. Currently "Need" is determined
// using placeholder values, 999999999 (9 9s) for integers. Use only for ints. Bools have no
// placeholder value.
// Use GetNeededStringPaths for strings.
func GetNeededPaths[T api.Scalar](
	parsedData gaby.Container,
	resourceTypeToPaths api.ResourceTypeToPathToVisitorInfoType,
	keys []any,
	resourceProvider ResourceProvider,
) (api.AttributeValueList, error) {
	// Determine the data type based on the generic type parameter
	var dataType api.DataType
	var zero T
	switch any(zero).(type) {
	case int:
		dataType = api.DataTypeInt
	case bool:
		dataType = api.DataTypeBool
	default:
		// Invalid; strings supported in a dedicated function
		return nil, fmt.Errorf("type %T not supported", zero)
	}

	return GetPathsAnyType(parsedData, resourceTypeToPaths, keys, resourceProvider, dataType, true)
}

// GetStringPaths traverses the specified path patterns of the specified resource types and returns
// an api.AttributeValueList containing the values and registered information about all of
// the found string attributes matching the path patterns. It can also extract fields embedded
// in strings using registered embedded accessors.
func GetStringPaths(
	parsedData gaby.Container,
	resourceTypeToPaths api.ResourceTypeToPathToVisitorInfoType,
	keys []any,
	resourceProvider ResourceProvider,
) (api.AttributeValueList, error) {
	return GetPathsAnyType(parsedData, resourceTypeToPaths, keys, resourceProvider, api.DataTypeString, false)
}

// GetNeededStringPaths traverses the specified path patterns of the specified resource types and returns
// an api.AttributeValueList containing the values and registered information about all of
// the found string attributes matching the path patterns that Need values. Currently "Need" is determined
// using placeholder values, "confighubplaceholder" for strings. It can also extract fields embedded
// in strings using registered embedded accessors.
func GetNeededStringPaths(
	parsedData gaby.Container,
	resourceTypeToPaths api.ResourceTypeToPathToVisitorInfoType,
	keys []any,
	resourceProvider ResourceProvider,
) (api.AttributeValueList, error) {
	return GetPathsAnyType(parsedData, resourceTypeToPaths, keys, resourceProvider, api.DataTypeString, true)
}

// UpdateStringPathsFunction traverses the specified path patterns of the specified resource types.
// The updater function simply needs to return the new attribute value. It can also inject fields
// embedded in strings using registered embedded accessors.
func UpdateStringPathsFunction(
	parsedData gaby.Container,
	resourceTypeToPaths api.ResourceTypeToPathToVisitorInfoType,
	keys []any,
	resourceProvider ResourceProvider,
	updater func(string) string,
	upsert bool,
) error {

	visitor := func(doc *gaby.YamlDoc, output any, context VisitorContext, currentValue string) (any, error) {
		originalValue := currentValue
		if context.EmbeddedPath != "" && context.Accessor != nil {
			embeddedValue, err := context.Accessor.Extract(currentValue, context.EmbeddedPath)
			if err != nil {
				// Not found is not an error. For example, it could be an embedded YAML or JSON field. Skip this value.
				return output, nil
			}
			embeddedStringValue, ok := embeddedValue.(string)
			// If the data isn't a string, skip this value.
			if !ok {
				return output, nil
			}
			currentValue = embeddedStringValue
		}
		newValue := updater(currentValue)
		if context.EmbeddedPath != "" && context.Accessor != nil {
			replacedValue, err := context.Accessor.Replace(originalValue, newValue, context.EmbeddedPath)
			if err != nil {
				return output, errors.Wrap(err, fmt.Sprintf("embedded field %s not replaced at path %s", context.EmbeddedPath, string(context.Path)))
			}
			newValue = replacedValue
		}
		var err error
		if newValue != originalValue || upsert {
			_, err = doc.SetP(newValue, string(context.Path))
		}
		return output, err
	}
	_, err := VisitPaths[string](parsedData, resourceTypeToPaths, keys, nil, resourceProvider, visitor, upsert)
	return err
}

// UpdateStringPaths traverses the specified path patterns of the specified resource types and
// updates the attributes with the provided value. It can also inject fields
// embedded in strings using registered embedded accessors.
func UpdateStringPaths(
	parsedData gaby.Container,
	resourceTypeToPaths api.ResourceTypeToPathToVisitorInfoType,
	keys []any,
	resourceProvider ResourceProvider,
	newValue string,
	upsert bool,
) error {

	updater := func(_ string) string {
		return newValue
	}
	err := UpdateStringPathsFunction(parsedData, resourceTypeToPaths, keys, resourceProvider, updater, upsert)
	return err
}

// GetRegisteredNeededStringPaths retrieves Needed values specifically registered under
// api.AttributeNameNeededValue.
func GetRegisteredNeededStringPaths(
	parsedData gaby.Container,
	resourceProvider ResourceProvider,
) (api.AttributeValueList, error) {
	resourceTypeToNeededPaths := GetPathRegistryForAttributeName(resourceProvider, api.AttributeNameNeededValue)
	return GetNeededStringPaths(parsedData, resourceTypeToNeededPaths, []any{}, resourceProvider)
}

// GetRegisteredProvidedStringPaths retrieves Provided values registered under
// api.AttributeNameProvidedValue.
func GetRegisteredProvidedStringPaths(
	parsedData gaby.Container,
	resourceProvider ResourceProvider,
) (api.AttributeValueList, error) {
	resourceTypeToProvidedPaths := GetPathRegistryForAttributeName(resourceProvider, api.AttributeNameProvidedValue)
	return GetStringPaths(parsedData, resourceTypeToProvidedPaths, []any{}, resourceProvider)
}

func DeletePaths(
	parsedData gaby.Container,
	resourceTypeToPaths api.ResourceTypeToPathToVisitorInfoType,
	keys []any,
	resourceProvider ResourceProvider,
) error {
	docVisitor := func(doc *gaby.YamlDoc, output any, context VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		err := doc.DeleteP(string(context.Path))
		return nil, err
	}
	_, err := VisitPathsDoc(parsedData, resourceTypeToPaths, keys, nil, resourceProvider, docVisitor, false)
	return err
}
