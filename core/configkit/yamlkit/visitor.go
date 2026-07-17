// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"log/slog"

	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/join"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
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
		slog.Debug("VisitResources errors", "error", err)
		return output, err
	}
	return output, nil
}

// ResolveConfigHubPath resolves a ConfigHub.* metadata path against a ResourceInfo struct.
// Returns the resolved value, or an error if the path is unsupported.
// Keep consistent with ValidWhereResourcePaths in api.go.
func ResolveConfigHubPath(path string, resourceInfo *api.ResourceInfo) (any, error) {
	switch path {
	case "ConfigHub.ResourceName":
		return string(resourceInfo.ResourceName), nil
	case "ConfigHub.ResourceNameWithoutScope":
		return string(resourceInfo.ResourceNameWithoutScope), nil
	case "ConfigHub.ResourceType":
		return string(resourceInfo.ResourceType), nil
	case "ConfigHub.ResourceCategory":
		return string(resourceInfo.ResourceCategory), nil
	case "ConfigHub.ResourceNameStableCore":
		return string(resourceInfo.ResourceNameStableCore), nil
	default:
		return nil, fmt.Errorf("unsupported ConfigHub path: %s", path)
	}
}

// MatchesWhereResourceExpressions evaluates each expression against a resource.
// For paths with the "ConfigHub." prefix, values are resolved from ResourceInfo metadata.
// For other paths, values are resolved from the resource's YAML document using YamlSafePathGetValueAnyType.
// Returns false if any expression doesn't match.
// Keep consistent with ValidWhereResourcePaths.
func MatchesWhereResourceExpressions(doc *gaby.YamlDoc, resourceInfo *api.ResourceInfo, expressions []*api.VisitorRelationalExpression) (bool, error) {
	for _, expr := range expressions {
		var leftValue any
		if strings.HasPrefix(expr.Path, "ConfigHub.") {
			var err error
			leftValue, err = ResolveConfigHubPath(expr.Path, resourceInfo)
			if err != nil {
				return false, err
			}
		} else {
			// Resolve the value from the YAML document
			value, found, err := YamlSafePathGetValueAnyType(doc, api.ResolvedPath(expr.Path), true)
			if err != nil {
				return false, err
			}
			if !found {
				return false, nil
			}
			leftValue = value
		}
		matched, err := api.EvaluateExpression(&expr.RelationalExpression, leftValue, nil, nil)
		if err != nil {
			return false, err
		}
		if !matched {
			return false, nil
		}
	}
	return true, nil
}

// VisitResourcesFiltered iterates over resources, skipping those that don't match the
// WhereResourceExpressions in options or that fall outside its ResourceIndexes. The two
// restrictions are AND-ed. When options has neither, it behaves identically to
// VisitResources.
func VisitResourcesFiltered(parsedData gaby.Container, output any, resourceProvider ResourceProvider, options *api.FunctionOptions, visitor ResourceVisitorFunc) (any, error) {
	whereExpressions := api.GetWhereResourceExpressions(options)
	resourceIndexes := api.GetResourceIndexes(options)
	if len(whereExpressions) == 0 && resourceIndexes == nil {
		return VisitResources(parsedData, output, resourceProvider, visitor)
	}
	filteredVisitor := func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		if resourceIndexes != nil && !slices.Contains(resourceIndexes, index) {
			return output, nil
		}
		if len(whereExpressions) != 0 {
			matched, err := MatchesWhereResourceExpressions(doc, resourceInfo, whereExpressions)
			if err != nil {
				return output, []error{err}
			}
			if !matched {
				return output, nil
			}
		}
		return visitor(doc, output, index, resourceInfo)
	}
	return VisitResources(parsedData, output, resourceProvider, filteredVisitor)
}

// VisitorContext contains information passed to visitor functions for each path traversed.
type VisitorContext struct {
	api.AttributeInfo // includes Path and Info
	Arguments         []api.FunctionArgument
	EmbeddedPath      string
	Accessor          EmbeddedAccessor
	PathVisitorInfo   *api.PathVisitorInfo
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
	options *api.FunctionOptions,
) (any, error) {
	docVisitor := func(doc *gaby.YamlDoc, output any, context VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		if currentDoc == nil || currentDoc.Data() == nil {
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
				return output, fmt.Errorf("unsupported type %T for upsert or no/null value at path %s", defaultValue, string(context.Path))
			}
		}
		currentValue, ok := currentDoc.Data().(T)
		if ok {
			return visitor(doc, output, context, currentValue)
		}
		return output, fmt.Errorf("value %v at path %s cannot be converted to %T", currentDoc.Data(), string(context.Path), currentValue)
	}
	return VisitPathsDoc(parsedData, resourceTypeToPaths, keys, output, resourceProvider, docVisitor, upsert, options)
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
	options *api.FunctionOptions,
) (any, error) {
	docVisitor := func(doc *gaby.YamlDoc, output any, context VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		// currentDoc.Data() could be nil
		return visitor(doc, output, context, currentDoc.Data())
	}
	return VisitPathsDoc(parsedData, resourceTypeToPaths, keys, output, resourceProvider, docVisitor, upsert, options)
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
	options *api.FunctionOptions,
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
		// Sort paths for deterministic iteration order
		sortedPaths := make([]api.UnresolvedPath, 0, len(unresolvedPaths))
		for k := range unresolvedPaths {
			sortedPaths = append(sortedPaths, k)
		}
		sort.Slice(sortedPaths, func(i, j int) bool { return sortedPaths[i] < sortedPaths[j] })
		for _, sortedPath := range sortedPaths {
			unresolvedPathInfo := unresolvedPaths[sortedPath]
			unresolvedPath := unresolvedPathInfo.Path
			if len(keys) > 0 {
				unresolvedPath = api.UnresolvedPath(fmt.Sprintf(string(unresolvedPath), keys...))
				if strings.Contains(string(unresolvedPath), "EXTRA") {
					slog.Debug("path resolved with excess keys", "unresolvedPath", string(unresolvedPathInfo.Path), "resolvedPath", string(unresolvedPath))
				}
			}
			unresolvedPathSegments := strings.Split(string(unresolvedPath), EmbeddedAccessorSeparator)
			embeddedPath := ""
			if len(unresolvedPathSegments) > 1 {
				embeddedPath = strings.Join(unresolvedPathSegments[1:], EmbeddedAccessorSeparator)
			}
			pathConstraint := strings.Split(string(unresolvedPathInfo.ResolvedPath), EmbeddedAccessorSeparator)
			// Create accessor early so it can be used for scalar-array associative lookups
			var accessor EmbeddedAccessor
			if unresolvedPathInfo.EmbeddedAccessorType != "" {
				var accErr error
				accessor, accErr = GetEmbeddedAccessor(unresolvedPathInfo.EmbeddedAccessorType,
					unresolvedPathInfo.EmbeddedAccessorConfig)
				if accErr != nil {
					multiErrs = append(multiErrs, accErr)
					continue
				}
			}
			// If there's an embedded accessor (#), upsert should be passed as false to ResolveAssociativePaths
			resolveUpsert := upsert && embeddedPath == ""
			resolvedPaths, err := ResolveAssociativePaths(doc, api.UnresolvedPath(unresolvedPathSegments[0]), api.ResolvedPath(pathConstraint[0]), resolveUpsert, accessor)
			if err != nil {
				// Don't report the error. Not found is expected.
				continue // Skip if an error
			}
			for _, resolvedPath := range resolvedPaths {
				// log.Infof("resolved path %s args %v in resource %s of type %s", resolvedPath.Path, resolvedPath.PathArguments, string(resourceName), string(resourceType))
				currentDoc, found, err := YamlSafePathGetDoc(doc, resolvedPath.Path, true)
				if err != nil || ((!found || currentDoc == nil) && !upsert) {
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
							Details:       unresolvedPathInfo.Details,
						},
					},
					Arguments:       resolvedPath.PathArguments,
					EmbeddedPath:    embeddedPath,
					Accessor:        accessor,
					PathVisitorInfo: unresolvedPathInfo,
				}
				// currentDoc.Data() could be nil
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
	newOutput, err := VisitResourcesFiltered(parsedData, output, resourceProvider, options, resourceVisitor)
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
	updater func(T, VisitorContext) T,
	upsert bool,
	options *api.FunctionOptions,
) error {

	visitor := func(doc *gaby.YamlDoc, output any, context VisitorContext, currentValue T) (any, error) {
		originalValue := currentValue
		newValue := updater(currentValue, context)
		var err error
		if newValue != originalValue || upsert {
			_, err = doc.SetP(newValue, string(context.Path))
		}
		return output, err
	}
	_, err := VisitPaths[T](parsedData, resourceTypeToPaths, keys, nil, resourceProvider, visitor, upsert, options)
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
	options *api.FunctionOptions,
) error {

	updater := func(_ T, _ VisitorContext) T {
		return newValue
	}
	err := UpdatePathsFunction[T](parsedData, resourceTypeToPaths, keys, resourceProvider, updater, upsert, options)
	return err
}

// UpdatePathsFunctionDoc traverses the specified path patterns of the specified resource types.
// The updater function simply needs to return the new attribute value, which must be a YamlDoc.
func UpdatePathsFunctionDoc(
	parsedData gaby.Container,
	resourceTypeToPaths api.ResourceTypeToPathToVisitorInfoType,
	keys []any,
	resourceProvider ResourceProvider,
	updater func(*gaby.YamlDoc, VisitorContext) *gaby.YamlDoc,
	upsert bool,
	options *api.FunctionOptions,
) error {

	visitor := func(doc *gaby.YamlDoc, output any, context VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		// currentDoc.Data() could be nil
		originalDoc := currentDoc
		newDoc := updater(currentDoc, context)
		var err error
		if upsert || (originalDoc != nil && newDoc.String() != originalDoc.String()) {
			// SetDocExpandP (not SetDocP) so an appended array index — resolved to
			// len(array) for a terminal ?key=value that matched nothing under upsert
			// (find-or-append) — expands the array instead of failing.
			_, err = doc.SetDocExpandP(newDoc, string(context.Path))
		}
		return output, err
	}
	_, err := VisitPathsDoc(parsedData, resourceTypeToPaths, keys, nil, resourceProvider, visitor, upsert, options)
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

func appendGetterAndSetterArguments(details *api.AttributeDetails, arguments []api.FunctionArgument) {
	if details == nil || len(arguments) == 0 {
		return
	}
	if details.GetterInvocation == nil && len(details.SetterInvocations) == 0 {
		return
	}
	if details.GetterInvocation != nil {
		details.GetterInvocation = appendFunctionInvocationArguments(details.GetterInvocation, arguments)
	}
	for i := range details.SetterInvocations {
		details.SetterInvocations[i] = *appendFunctionInvocationArguments(&details.SetterInvocations[i], arguments)
	}
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
	options *api.FunctionOptions,
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

	return GetPathsAnyType(parsedData, resourceTypeToPaths, keys, resourceProvider, dataType, false, false, options)
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
	providedValuesOnly bool,
	options *api.FunctionOptions,
) (api.AttributeValueList, error) {

	visitor := func(resourceDoc *gaby.YamlDoc, output any, context VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		attr := context.AttributeInfo
		var currentDataType api.DataType
		currentValue := currentDoc.Data()
		if currentValue == nil || resourceDoc == nil {
			return output, nil // skip if there's no value or no resource
		}

		// Filter resources with IgnoreNeeded/IgnoreProvided options
		if neededValuesOnly {
			ignorePath := slices.Contains(GetVisitorOptions(resourceDoc, resourceProvider), constants.VisitorOptionIgnoreNeeded)
			if ignorePath {
				return output, nil
			}
		}
		if providedValuesOnly {
			ignorePath := slices.Contains(GetVisitorOptions(resourceDoc, resourceProvider), constants.VisitorOptionIgnoreProvided)
			if ignorePath {
				return output, nil
			}
		}

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
				attr.Path = api.ResolvedPath(string(attr.Path) + EmbeddedAccessorSeparator + context.EmbeddedPath)
			}
		case int:
			currentDataType = api.DataTypeInt
		case bool:
			currentDataType = api.DataTypeBool
		default:
			// Non-scalar value (map, slice, etc.) - serialize as YAML
			currentDataType = api.DataTypeYAML
			currentValueString := strings.TrimRight(currentDoc.String(), "\n")
			currentValue = currentValueString
		}

		// Apply type filtering based on dataType parameter
		if dataType != api.DataTypeNone && dataType != currentDataType {
			slog.Debug(fmt.Sprintf("value %v at path %s in resource %s %s is of type %s but expected %s", currentValue, string(context.Path), string(context.ResourceType), string(context.ResourceName), currentDataType, dataType))
			return output, nil
		}

		// Apply needed values filtering if requested
		if neededValuesOnly {
			switch currentDataType {
			case api.DataTypeString:
				if stringVal, ok := currentValue.(string); ok && !IsStringPlaceHolderValue(stringVal) {
					return output, nil // skip if there's already a value
				}
			case api.DataTypeInt:
				if intVal, ok := currentValue.(int); ok && !IsIntPlaceHolderValue(intVal) {
					return output, nil // skip if there's already a value
				}
			case api.DataTypeBool:
				// No placeholder for bool
			}
		}

		attr.DataType = currentDataType

		visitorValues, ok := output.([]api.AttributeValue)
		if !ok {
			slog.Debug("couldn't convert output to []api.AttributeValue{}")
			return output, errors.New("could not convert visitor output to AttributeValueList")
		}
		var attributeValue api.AttributeValue
		// Read comments from $comment$ sibling keys in the parent map
		head, line, foot := resourceDoc.GetCommentKeys(string(context.Path))
		var commentParts []string
		if head != "" {
			commentParts = append(commentParts, head)
		}
		if line != "" {
			commentParts = append(commentParts, line)
		}
		if foot != "" {
			commentParts = append(commentParts, foot)
		}
		comment := strings.Join(commentParts, "\n")

		// Deep copy the AttributeInfo so that maps, slices, and pointers in Details
		// are not shared across AttributeValues from different visitor invocations.
		if neededValuesOnly || providedValuesOnly || api.GetIncludeDetails(options) {
			deepCopiedAttr := api.DeepCopyAttributeInfo(attr)
			attributeValue = api.AttributeValue{AttributeInfo: deepCopiedAttr, Value: currentValue, Comment: comment}

			// Append getter and setter arguments to the deep-copied details.
			appendGetterAndSetterArguments(deepCopiedAttr.Details, context.Arguments)

			// TODO
			// For needed values, auto-extract merge keys from the resolved path as preferred properties.
			// Resolved paths use numeric indices (e.g., spec.template.spec.volumes.1.configMap.name).
			// We walk the path, find numeric segments, look up the merge key for that array via
			// MergeKeyForPath, and read the merge key value from the resource document.
			// if neededValuesOnly && resourceDoc != nil {
			// 	EnrichMergeKeysFromDoc(resourceDoc, resourceProvider, &attributeValue)
			// }

			// Invoke the Enricher function if registered for this path
			if context.PathVisitorInfo != nil {
				if enricher, ok := context.PathVisitorInfo.Enricher.(AttributeEnricher); ok && enricher != nil {
					_ = enricher(resourceDoc, &attributeValue, providedValuesOnly)
				}
			}
		} else {
			// Don't include Details. Shallow copy, clear Details.
			attributeValue = api.AttributeValue{AttributeInfo: attr, Value: currentValue, Comment: comment}
			attributeValue.Details = nil
		}

		visitorValues = append(visitorValues, attributeValue)
		return visitorValues, nil
	}
	values := []api.AttributeValue{}
	output, err := VisitPathsDoc(parsedData, resourceTypeToPaths, keys, values, resourceProvider, visitor, false, options)
	if err != nil {
		return values, err
	}
	values, ok := output.([]api.AttributeValue)
	if !ok {
		slog.Debug("couldn't convert output to []api.AttributeValue{}")
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
	options *api.FunctionOptions,
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

	return GetPathsAnyType(parsedData, resourceTypeToPaths, keys, resourceProvider, dataType, true, false, options)
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
	options *api.FunctionOptions,
) (api.AttributeValueList, error) {
	return GetPathsAnyType(parsedData, resourceTypeToPaths, keys, resourceProvider, api.DataTypeString, false, false, options)
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
	options *api.FunctionOptions,
) (api.AttributeValueList, error) {
	return GetPathsAnyType(parsedData, resourceTypeToPaths, keys, resourceProvider, api.DataTypeString, true, false, options)
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
	options *api.FunctionOptions,
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
	_, err := VisitPaths[string](parsedData, resourceTypeToPaths, keys, nil, resourceProvider, visitor, upsert, options)
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
	options *api.FunctionOptions,
) error {

	updater := func(_ string) string {
		return newValue
	}
	err := UpdateStringPathsFunction(parsedData, resourceTypeToPaths, keys, resourceProvider, updater, upsert, options)
	return err
}

// GetRegisteredNeededStringPaths retrieves Needed values by scanning all attributes
// for paths marked as IsNeeded.
func GetRegisteredNeededStringPaths(
	parsedData gaby.Container,
	resourceProvider ResourceProvider,
	options *api.FunctionOptions,
) (api.AttributeValueList, error) {
	resourceTypeToNeededPaths := GetRegisteredNeededPaths(resourceProvider)
	return GetNeededStringPaths(parsedData, resourceTypeToNeededPaths, []any{}, resourceProvider, options)
}

// GetRegisteredProvidedStringPaths retrieves Provided values by scanning all attributes
// for paths marked as IsProvided. Resources with IgnoreProvided annotation are skipped.
func GetRegisteredProvidedStringPaths(
	parsedData gaby.Container,
	resourceProvider ResourceProvider,
	options *api.FunctionOptions,
) (api.AttributeValueList, error) {
	resourceTypeToProvidedPaths := GetRegisteredProvidedPaths(resourceProvider)
	return GetPathsAnyType(parsedData, resourceTypeToProvidedPaths, []any{}, resourceProvider, api.DataTypeString, false, true, options)
}

// VisitorSetterInvocationFunctionName is a special function name used to indicate that the
// setter invocation value should be used directly by the visitor, rather than invoking a
// real function. It is not a valid function name.
const VisitorSetterInvocationFunctionName = "$visitor"

// UpdatePathsSetterArgument traverses the specified path patterns of the specified resource types.
// For each path, if the visitor context has Details with a SetterInvocation using
// VisitorSetterInvocationFunctionName, the value at the path is set to the first argument's Value.
// Supports string, int, and bool values. Skips paths where the current value is already set to
// a non-placeholder value. Otherwise, the path is not updated.
func UpdatePathsSetterArgument(
	parsedData gaby.Container,
	resourceTypeToPaths api.ResourceTypeToPathToVisitorInfoType,
	keys []any,
	resourceProvider ResourceProvider,
	upsert bool,
	options *api.FunctionOptions,
) error {
	visitor := func(doc *gaby.YamlDoc, output any, context VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		if context.Details == nil ||
			len(context.Details.SetterInvocations) == 0 ||
			context.Details.SetterInvocations[0].FunctionName != VisitorSetterInvocationFunctionName ||
			len(context.Details.SetterInvocations[0].Arguments) == 0 ||
			context.Details.SetterInvocations[0].Arguments[0].Value == nil {
			return output, nil
		}
		newValue := context.Details.SetterInvocations[0].Arguments[0].Value
		switch newValue.(type) {
		case string, int, bool, []any:
			// valid scalar or array type (gaby's setValue requires []interface{})
		default:
			return output, fmt.Errorf("unsupported value type %T at path %s", newValue, string(context.Path))
		}
		// Skip if the current value is already set to a non-placeholder value
		if currentDoc != nil && !IsPlaceholderValue(currentDoc.Data()) {
			return output, nil
		}
		_, err := doc.SetP(newValue, string(context.Path))
		return output, err
	}
	_, err := VisitPathsDoc(parsedData, resourceTypeToPaths, keys, nil, resourceProvider, visitor, upsert, options)
	return err
}

// VetPathsSetterArgument traverses the specified path patterns and validates that current values
// match the expected default values from $visitor setter invocations. Returns a ValidationResult
// with Passed=false and FailedAttributes listing any mismatched paths.
func VetPathsSetterArgument(
	parsedData gaby.Container,
	resourceTypeToPaths api.ResourceTypeToPathToVisitorInfoType,
	keys []any,
	resourceProvider ResourceProvider,
	options *api.FunctionOptions,
) (api.ValidationResult, error) {
	visitor := func(doc *gaby.YamlDoc, output any, context VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		if context.Details == nil ||
			len(context.Details.SetterInvocations) == 0 ||
			context.Details.SetterInvocations[0].FunctionName != VisitorSetterInvocationFunctionName ||
			len(context.Details.SetterInvocations[0].Arguments) == 0 ||
			context.Details.SetterInvocations[0].Arguments[0].Value == nil {
			return output, nil
		}
		expectedValue := context.Details.SetterInvocations[0].Arguments[0].Value
		var currentValue any
		if currentDoc != nil {
			currentValue = currentDoc.Data()
		}
		// currentValue could be nil
		if currentValue != expectedValue {
			failedAttributes, ok := output.(api.AttributeValueList)
			if !ok {
				return output, fmt.Errorf("internal error")
			}
			failedAttributes = append(failedAttributes, api.AttributeValue{
				AttributeInfo: context.AttributeInfo,
				Value:         currentValue,
			})
			return failedAttributes, nil
		}
		return output, nil
	}
	values := api.AttributeValueList{}
	output, err := VisitPathsDoc(parsedData, resourceTypeToPaths, keys, values, resourceProvider, visitor, false, options)
	if err != nil {
		return api.ValidationResult{Passed: false}, err
	}
	failedAttributes, ok := output.(api.AttributeValueList)
	if !ok {
		return api.ValidationResult{Passed: false}, fmt.Errorf("internal error")
	}
	return api.ValidationResult{
		Passed:           len(failedAttributes) == 0,
		FailedAttributes: failedAttributes,
	}, nil
}

func DeletePaths(
	parsedData gaby.Container,
	resourceTypeToPaths api.ResourceTypeToPathToVisitorInfoType,
	keys []any,
	resourceProvider ResourceProvider,
	options *api.FunctionOptions,
) error {
	docVisitor := func(doc *gaby.YamlDoc, output any, context VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		// Delete associated $comment$ sibling keys before deleting the data path
		doc.DeleteCommentKeysForPath(string(context.Path))
		err := doc.DeleteP(string(context.Path))
		return nil, err
	}
	_, err := VisitPathsDoc(parsedData, resourceTypeToPaths, keys, nil, resourceProvider, docVisitor, false, options)
	return err
}
