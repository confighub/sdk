// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package yamlkit is TODO.
package yamlkit

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/join"
	"github.com/labstack/gommon/log"
	"github.com/mikefarah/yq/v4/pkg/yqlib"
	yqlogger "gopkg.in/op/go-logging.v1"

	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
)

// User data errors should not be logged here. They will be logged by the caller.
// Errors indicate that the operation could not be completed.
// Messages should be acceptable to return to the user, and should indicate the
// location of the problem in the configuration data.

// PlaceHolderBlockApply We will need placeholders for different data types and that fit with different validation rules
// The string value is all lowercase to comply with DNS label requirements.
const (
	PlaceHolderBlockApplyString = "replaceme"
	PlaceHolderBlockApplyInt    = 999999999
)

// This is not in a more general place because it is expected to be used after conversion of other
// formats to YAML.

// The ResourceProvider interface is used to perform toolchain-specific operations.
type ResourceProvider interface {
	DefaultResourceCategory() api.ResourceCategory
	ResourceCategoryGetter(doc *gaby.YamlDoc) (api.ResourceCategory, error)
	ResourceTypeGetter(doc *gaby.YamlDoc) (api.ResourceType, error)
	ResourceNameGetter(doc *gaby.YamlDoc) (api.ResourceName, error)
	RemoveScopeFromResourceName(resourceName api.ResourceName) api.ResourceName
	ScopelessResourceNamePath() api.ResolvedPath
	SetResourceName(doc *gaby.YamlDoc, name string) error
	ResourceTypesAreSimilar(resourceTypeA, resourceTypeB api.ResourceType) bool
	TypeDescription() string
	NormalizeName(name string) string
	NameSeparator() string
	ContextPath(contextField string) string
	GetPathRegistry() api.AttributeNameToResourceTypeToPathToVisitorInfoType
}

// LowerFirst lowercases the first character, which is useful for converting PascalCase to camelCase
func LowerFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

type ResourceTypeToPathPrefixSetType map[api.ResourceType]map[string]struct{}

var resourceTypeToPathPrefixToIsWildcarded = make(ResourceTypeToPathPrefixSetType)

// YamlSafePathGetDoc returns a document node at a fully resolved path and whether it was found.
// An error indicates a parsing error. An error is also returned if the path is expected to exist.
func YamlSafePathGetDoc(
	doc *gaby.YamlDoc,
	resolvedPath api.ResolvedPath,
	notFoundOk bool,
) (*gaby.YamlDoc, bool, error) {
	resolvedPathString := string(resolvedPath)
	if resolvedPathString == "" {
		return doc, true, nil
	}
	if !doc.ExistsP(resolvedPathString) {
		if notFoundOk {
			return doc, false, nil
		} else {
			return doc, false, fmt.Errorf("%s not found", resolvedPathString)
		}
	}
	subdoc := doc.Path(resolvedPathString)
	return subdoc, true, nil
}

// YamlSafePathGetValueAnyType returns a value at a fully resolved path and whether it was found.
// An error indicates a parsing error.
func YamlSafePathGetValueAnyType(
	doc *gaby.YamlDoc,
	resolvedPath api.ResolvedPath,
	notFoundOk bool,
) (any, bool, error) {
	subdoc, found, err := YamlSafePathGetDoc(doc, resolvedPath, notFoundOk)
	var result any
	if err != nil {
		return result, false, err
	}
	if !found {
		if notFoundOk {
			return result, false, nil
		} else {
			return result, false, fmt.Errorf("%s not found", string(resolvedPath))
		}
	}
	result = subdoc.Data()
	return result, found, nil
}

// YamlSafePathGetValue returns a value at a fully resolved path and whether it was found.
// An error indicates a parsing error or that the value was not of the expected type.
func YamlSafePathGetValue[T api.Scalar](
	doc *gaby.YamlDoc,
	resolvedPath api.ResolvedPath,
	notFoundOk bool,
) (T, bool, error) {
	subdoc, found, err := YamlSafePathGetDoc(doc, resolvedPath, notFoundOk)
	var result T
	if err != nil {
		return result, false, err
	}
	if !found {
		if notFoundOk {
			return result, false, nil
		} else {
			return result, false, fmt.Errorf("%s not found", string(resolvedPath))
		}
	}
	var ok bool
	result, ok = subdoc.Data().(T)
	if !ok {
		return result, found, fmt.Errorf("value %v cannot be converted to %T", subdoc.Data(), result)
	}
	return result, found, nil
}

func GetResourceCategoryTypeName(doc *gaby.YamlDoc, resourceProvider ResourceProvider) (api.ResourceCategory, api.ResourceType, api.ResourceName, error) {
	resourceInfo, err := GetResourceInfo(doc, resourceProvider)
	if err != nil {
		return "", "", "", err
	}
	return resourceInfo.ResourceCategory, resourceInfo.ResourceType, resourceInfo.ResourceName, nil
}

func GetResourceInfo(doc *gaby.YamlDoc, resourceProvider ResourceProvider) (*api.ResourceInfo, error) {
	var resourceCategory api.ResourceCategory
	var resourceType api.ResourceType
	var resourceName api.ResourceName
	var err error
	resourceCategory, err = resourceProvider.ResourceCategoryGetter(doc)
	if err != nil {
		return nil, err
	}
	resourceType, err = resourceProvider.ResourceTypeGetter(doc)
	if err != nil {
		return nil, err
	}
	resourceName, err = resourceProvider.ResourceNameGetter(doc)
	if err != nil {
		return nil, err
	}
	resourceInfo := &api.ResourceInfo{
		ResourceName:             resourceName,
		ResourceNameWithoutScope: resourceProvider.RemoveScopeFromResourceName(resourceName),
		ResourceType:             resourceType,
		ResourceCategory:         resourceCategory,
	}
	return resourceInfo, nil
}

// ResourceVisitorFunc defines the signature of functions invoked by the resource visitor function.
type ResourceVisitorFunc func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error)

// VisitResources iterates over all of the resources/elements in a configuration unit
// and passes metadata about the resource as well as the document itself to a visitor function.
func VisitResources(parsedData gaby.Container, output any, resourceProvider ResourceProvider, visitor ResourceVisitorFunc) (any, error) {
	multiErrs := []error{}
	for index, doc := range parsedData {
		resourceInfo, err := GetResourceInfo(doc, resourceProvider)
		if err != nil {
			multiErrs = append(multiErrs, err)
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

type ResourceNameToCategoryTypesMap map[api.ResourceName][]api.ResourceCategoryType
type ResourceCategoryTypeToNamesMap map[api.ResourceCategoryType][]api.ResourceName
type ResourceInfoToDocMap map[api.ResourceInfo]int

// ResourceAndCategoryTypeMaps returns maps of all resources in the provided list of parsed YAML
// documents, from from names to categories+types and categories+types to names.
func ResourceAndCategoryTypeMaps(parsedData gaby.Container, resourceProvider ResourceProvider) (
	resourceMap ResourceNameToCategoryTypesMap,
	categoryTypeMap ResourceCategoryTypeToNamesMap,
	err error,
) {
	resourceMap = make(ResourceNameToCategoryTypesMap)
	categoryTypeMap = make(ResourceCategoryTypeToNamesMap)
	if len(parsedData) == 0 {
		return resourceMap, categoryTypeMap, nil
	}
	visitor := func(_ *gaby.YamlDoc, _ any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
		categoryType := api.ResourceCategoryType{
			ResourceCategory: resourceInfo.ResourceCategory,
			ResourceType:     resourceInfo.ResourceType,
		}
		resourceMap[resourceInfo.ResourceName] = append(resourceMap[resourceInfo.ResourceName], categoryType)
		categoryTypeMap[categoryType] = append(categoryTypeMap[categoryType], resourceInfo.ResourceName)
		return nil, []error{}
	}
	_, err = VisitResources(parsedData, nil, resourceProvider, visitor)
	return resourceMap, categoryTypeMap, err
}

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

// ResolvedPathInfo contains a fully resolved path and any named path parameters
// specified in the unresolved path expression (using ?, *?, or *@).
type ResolvedPathInfo struct {
	Path          api.ResolvedPath
	PathArguments []api.FunctionArgument
}

// EscapeDotsInPathSegment escapes any dots in a path segment for use in whole-path searches
// because path segments are separated by dots.
// TODO: Escape more special characters?
func EscapeDotsInPathSegment(segment string) string {
	return strings.ReplaceAll(segment, ".", "~1")
}

// JoinPathSegments escapes any dots in path segments and joins them for use in whole-path searches.
func JoinPathSegments(segments []string) string {
	for i := range segments {
		segments[i] = EscapeDotsInPathSegment(segments[i])
	}
	return strings.Join(segments, ".")
}

func PathIsResolved(path string) bool {
	return !strings.ContainsAny(path, "?*@|")
}

// ResolveAssociativePaths resolves an associative path with associative lookups (?) and wildcards (*, *?, *@)
// into specific resolved paths and discovered path parameters.
// See the documentation for api.UnresolvedPath for more details.
func ResolveAssociativePaths(
	doc *gaby.YamlDoc,
	unresolvedPath api.UnresolvedPath,
	resolvedPath api.ResolvedPath,
) ([]ResolvedPathInfo, error) {

	path := string(unresolvedPath)
	if path == "" {
		return []ResolvedPathInfo{}, fmt.Errorf("path cannot be empty")
	}
	if PathIsResolved(path) {
		return []ResolvedPathInfo{{Path: api.ResolvedPath(path)}}, nil
	}
	// DotPathToSlice converts escaped dots back to unescaped dots, so we need to convert
	// them back when constructing the path
	segments := gaby.DotPathToSlice(path)
	var constraintSegments []string
	if resolvedPath != "" {
		constraintSegments = gaby.DotPathToSlice(string(resolvedPath))
		if len(constraintSegments) != len(segments) {
			log.Debugf("unresolved path %s and resolved path %s have different numbers of segments",
				path, string(resolvedPath))
		}
	}
	resolvedPaths := []ResolvedPathInfo{}

	type currentPosition struct {
		ResolvedSegments    []string
		CurrentSegmentIndex int
		PathArguments       []api.FunctionArgument
		ParentNode          *gaby.YamlDoc
	}
	workList := []currentPosition{{CurrentSegmentIndex: 0, ParentNode: doc}}
	for len(workList) != 0 {
		if workList[0].CurrentSegmentIndex == len(segments) {
			// Success! Record the path and args, dequeue, and continue.
			resolvedPaths = append(resolvedPaths, ResolvedPathInfo{
				Path:          api.ResolvedPath(JoinPathSegments(workList[0].ResolvedSegments)),
				PathArguments: workList[0].PathArguments,
			})
			workList = workList[1:]
			continue
		}
		segment := segments[workList[0].CurrentSegmentIndex]
		var constraintSegment string
		if workList[0].CurrentSegmentIndex < len(constraintSegments) {
			constraintSegment = constraintSegments[workList[0].CurrentSegmentIndex]
		}
		switch {
		case strings.HasPrefix(segment, "*"):
			// Gaby Search supports wildcards, at least for array sequence nodes,
			// but I'm unsure how it returns multiple results. We resolve wildcards here.

			var parameterKey, parameterName string
			if strings.HasPrefix(segment, "*?") {
				keyName := strings.TrimPrefix(segment, "*?")
				keyNameParts := strings.Split(keyName, ":")
				parameterKey = keyNameParts[0]
				switch len(keyNameParts) {
				case 1:
					// No parameter name
				case 2:
					parameterName = keyNameParts[1]
				default:
					return []ResolvedPathInfo{}, fmt.Errorf("invalid parameter expression '%s'", segment)
				}
			} else if strings.HasPrefix(segment, "*@:") {
				parameterName = strings.TrimPrefix(segment, "*@:")
			}

			// Enqueue all children
			children := workList[0].ParentNode.ChildrenMap()
			if len(children) > 0 {
				for key, child := range children {
					// TODO: This could be more efficient.
					if constraintSegment != "" && key != constraintSegment {
						continue
					}
					newPos := currentPosition{
						ResolvedSegments:    append(workList[0].ResolvedSegments, EscapeDotsInPathSegment(key)),
						CurrentSegmentIndex: workList[0].CurrentSegmentIndex + 1,
						PathArguments:       workList[0].PathArguments,
						ParentNode:          child,
					}
					if parameterKey != "" {
						fieldValueNode := child.S(parameterKey)
						if fieldValueNode != nil {
							newPos.PathArguments = append(newPos.PathArguments, api.FunctionArgument{ParameterName: parameterName, Value: fieldValueNode.Data()})
						}
					} else if parameterName != "" {
						newPos.PathArguments = append(newPos.PathArguments, api.FunctionArgument{ParameterName: parameterName, Value: key})
					}
					workList = append(workList, newPos)
				}
			} else if arrayChildren := workList[0].ParentNode.Children(); arrayChildren != nil {
				// NOTE: An empty map will also land here.

				for index, child := range arrayChildren {
					indexString := strconv.Itoa(index)
					// TODO: This could be more efficient.
					if constraintSegment != "" && indexString != constraintSegment {
						continue
					}
					newPos := currentPosition{
						ResolvedSegments:    append(workList[0].ResolvedSegments, indexString),
						CurrentSegmentIndex: workList[0].CurrentSegmentIndex + 1,
						PathArguments:       workList[0].PathArguments,
						ParentNode:          child,
					}
					if parameterKey != "" {
						fieldValueNode := child.S(parameterKey)
						if fieldValueNode != nil {
							newPos.PathArguments = append(newPos.PathArguments, api.FunctionArgument{ParameterName: parameterName, Value: fieldValueNode.Data()})
						}
					}
					workList = append(workList, newPos)
				}
			} else {
				// No children found on this branch. Possibly went down an errant path.
			}
			// Dequeue
			workList = workList[1:]

		case strings.HasPrefix(segment, "?"):
			// Associative lookup
			currentNode := workList[0].ParentNode
			if currentNode == nil || currentNode.IsArray() == false {
				// Possibly we went down an errant path
				// Dequeue and continue
				workList = workList[1:]
				continue
			}
			// Parse the key and value
			kv := strings.TrimPrefix(segment, "?")
			kvParts := strings.SplitN(kv, "=", 2)
			if len(kvParts) != 2 {
				return []ResolvedPathInfo{}, fmt.Errorf("invalid associative lookup '%s'", segment)
			}
			var parameterKey, parameterName string
			keyName := kvParts[0]
			value := kvParts[1]
			keyNameParts := strings.Split(keyName, ":")
			parameterKey = keyNameParts[0]
			switch len(keyNameParts) {
			case 1:
				// No parameter name
			case 2:
				parameterName = keyNameParts[1]
			default:
				return []ResolvedPathInfo{}, fmt.Errorf("invalid associative parameter expression '%s'", segment)
			}

			// Search the sequence for an element where key == value
			elements := currentNode.Children()
			found := false
			for index, child := range elements {
				indexString := strconv.Itoa(index)
				if constraintSegment != "" && indexString != constraintSegment {
					continue
				}
				fieldValueNode := child.S(parameterKey)
				if fieldValueNode != nil && (fieldValueNode.Data() == value || constraintSegment != "") {
					// Found the matching element. Just update the head of the queue.
					workList[0].ResolvedSegments = append(workList[0].ResolvedSegments, indexString)
					workList[0].ParentNode = child
					workList[0].CurrentSegmentIndex++
					workList[0].PathArguments = append(workList[0].PathArguments, api.FunctionArgument{ParameterName: parameterName, Value: fieldValueNode.Data()})
					found = true
					break
				}
			}
			if !found {
				// Not found
				// Dequeue and continue
				workList = workList[1:]
			}

		default:
			// Regular segment. Assume it matches the constraint, if any.
			parameterName := ""
			parameterValue := ""
			if strings.HasPrefix(segment, "@") {
				keyName := strings.TrimPrefix(segment, "@")
				keyNameParts := strings.Split(keyName, ":")
				switch len(keyNameParts) {
				case 1:
					// No parameter name
				case 2:
					parameterValue = keyNameParts[0]
					parameterName = keyNameParts[1]
				default:
					return []ResolvedPathInfo{}, fmt.Errorf("invalid map parameter expression '%s'", segment)
				}
				segment = keyNameParts[0]
				// log.Infof("segment %s parameterValue %s parameterName %s", segment, parameterValue, parameterName)
			}

			// This segment traversal doesn't need to have dots escaped
			currentNode := workList[0].ParentNode.S(segment)
			if currentNode == nil {
				// Possibly we went down an errant path
				// Dequeue and continue
				workList = workList[1:]
				continue
			}

			// Update the head of the queue.
			workList[0].ResolvedSegments = append(workList[0].ResolvedSegments, EscapeDotsInPathSegment(segment))
			if parameterName != "" {
				workList[0].PathArguments = append(workList[0].PathArguments, api.FunctionArgument{ParameterName: parameterName, Value: parameterValue})
			}
			workList[0].ParentNode = currentNode
			workList[0].CurrentSegmentIndex++
		}
	}

	return resolvedPaths, nil
}

// IsNumeric reports whether a character is within ['0'-'9'].
func IsNumeric(c rune) bool {
	return (c >= '0' && c <= '9')
}

// IsNumber reports whether a strings characters are all within ['0'-'9'].
func IsNumber(s string) bool {
	for _, c := range s {
		if !IsNumeric(c) {
			return false
		}
	}
	return true
}

func prefixIsWildcarded(resourceType api.ResourceType, prefix string) bool {
	_, present := resourceTypeToPathPrefixToIsWildcarded[resourceType]
	if !present {
		return false
	}
	_, present = resourceTypeToPathPrefixToIsWildcarded[resourceType][prefix]
	return present
}

func normalizePath(resourceType api.ResourceType, path api.UnresolvedPath, preserveBinding bool) api.UnresolvedPath {
	segments := gaby.DotPathToSlice(string(path))
	prefix := ""
	for i, segment := range segments {
		// Associative lookups and array indices are treated as wildcards, but maps can be also
		if strings.ContainsAny(segment, "?*@%") || IsNumber(segment) || prefixIsWildcarded(resourceType, prefix) {
			if preserveBinding && strings.ContainsAny(segment, "?@") && strings.ContainsAny(segment, ":") {
				switch {
				case strings.HasPrefix(segment, "*?"):
					// just keep it as is
				case strings.HasPrefix(segment, "*@:"):
					// just keep it as is
				case strings.HasPrefix(segment, "?"):
					// convert to wildcard
					kvParts := strings.SplitN(segment, "=", 2)
					// Keep the part before the equal sign
					segments[i] = "*" + kvParts[0]
				case strings.HasPrefix(segment, "@"):
					parameterName, found := strings.CutPrefix(segments[i], ":")
					// Should be found due to the check above, but...
					if found {
						segments[i] = "*@:" + parameterName
					} else {
						segments[i] = "*"
					}

				default:
					segments[i] = "*"
				}
			} else {
				segments[i] = "*"
			}
		}
		if prefix != "" {
			prefix += "."
		}
		prefix += segments[i]
	}
	return api.UnresolvedPath(prefix)
}

func registerPathWildcards(resourceType api.ResourceType, path api.UnresolvedPath) {
	segments := gaby.DotPathToSlice(string(path))
	prefix := ""
	for i, segment := range segments {
		// Record all wildcards
		if strings.ContainsAny(segment, "*") {
			segments[i] = "*"
			_, present := resourceTypeToPathPrefixToIsWildcarded[resourceType]
			if !present {
				resourceTypeToPathPrefixToIsWildcarded[resourceType] = make(map[string]struct{})
			}
			resourceTypeToPathPrefixToIsWildcarded[resourceType][prefix] = struct{}{}
		}
		if prefix != "" {
			prefix += "."
		}
		prefix += segments[i]
	}
}

// FunctionInvocationsEqual reports whether two function invocations match.
func FunctionInvocationsEqual(fi1, fi2 *api.FunctionInvocation) bool {
	if (fi1 == nil) != (fi2 == nil) {
		return false
	}
	if fi1 == nil {
		return true
	}
	if fi1.FunctionName != fi2.FunctionName || len(fi1.Arguments) != len(fi2.Arguments) {
		return false
	}
	for i, _ := range fi1.Arguments {
		if fi1.Arguments[i].ParameterName != fi2.Arguments[i].ParameterName ||
			fi1.Arguments[i].Value != fi2.Arguments[i].Value {
			return false
		}
	}
	return true
}

// AttributeDetailsEqual reports whether two sets of attribute details, optionally including
// getter and setter invocations, match.
func AttributeDetailsEqual(details1, details2 *api.AttributeDetails, compareFunctions bool) bool {
	isGenerationTemplate1 := (details1 != nil && details1.GenerationTemplate != "")
	isGenerationTemplate2 := (details2 != nil && details2.GenerationTemplate != "")
	if isGenerationTemplate1 != isGenerationTemplate2 ||
		(isGenerationTemplate1 && isGenerationTemplate2 &&
			details1.GenerationTemplate != details2.GenerationTemplate) {
		return false
	}
	isDescription1 := (details1 != nil && details1.Description != "")
	isDescription2 := (details2 != nil && details2.Description != "")
	if isDescription1 != isDescription2 ||
		(isDescription1 && isDescription2 &&
			details1.Description != details2.Description) {
		return false
	}
	if !compareFunctions {
		return true
	}
	if !FunctionInvocationsEqual(details1.GetterInvocation, details2.GetterInvocation) {
		return false
	}
	if len(details1.SetterInvocations) != len(details2.SetterInvocations) {
		return false
	}
	for i, _ := range details1.SetterInvocations {
		if !FunctionInvocationsEqual(&details1.SetterInvocations[i], &details2.SetterInvocations[i]) {
			return false
		}
	}
	return true
}

// VisitorInfoEqual reports whether two path visitor specifications, optionally including
// getter and setter invocations, match.
func VisitorInfoEqual(pathVisitorInfo1, pathVisitorInfo2 *api.PathVisitorInfo, compareFunctions bool) bool {
	return pathVisitorInfo1.AttributeName == pathVisitorInfo2.AttributeName &&
		pathVisitorInfo1.DataType == pathVisitorInfo2.DataType &&
		pathVisitorInfo1.EmbeddedAccessorType == pathVisitorInfo2.EmbeddedAccessorType &&
		pathVisitorInfo1.EmbeddedAccessorConfig == pathVisitorInfo2.EmbeddedAccessorConfig &&
		AttributeDetailsEqual(pathVisitorInfo1.Info, pathVisitorInfo2.Info, compareFunctions)
}

func setFunctionInvocationsInVisitorPathInfo(
	pathInfo *api.PathVisitorInfo,
	getterFunctionInvocation *api.FunctionInvocation,
	setterFunctionInvocation *api.FunctionInvocation,
) {
	if getterFunctionInvocation == nil && setterFunctionInvocation == nil {
		return
	}
	if pathInfo.Info == nil {
		pathInfo.Info = &api.AttributeDetails{}
	}
	// The getter should be the same.
	if getterFunctionInvocation != nil {
		if pathInfo.Info.GetterInvocation != nil &&
			!FunctionInvocationsEqual(pathInfo.Info.GetterInvocation, getterFunctionInvocation) {
			log.Errorf("different getter function invocations registered: %v vs %v",
				pathInfo.Info.GetterInvocation, getterFunctionInvocation)
		}
		pathInfo.Info.GetterInvocation = getterFunctionInvocation
	}
	if setterFunctionInvocation != nil {
		found := false
		for _, setterInvocation := range pathInfo.Info.SetterInvocations {
			// The function name could be different and/or the argument values could be different.
			// Example: resource references that could refer to multiple resource types.
			if FunctionInvocationsEqual(&setterInvocation, setterFunctionInvocation) {
				found = true
				break
			}
		}
		if !found {
			pathInfo.Info.SetterInvocations = append(pathInfo.Info.SetterInvocations, *setterFunctionInvocation)
		}
	}
}

func registerPaths(
	registry api.ResourceTypeToPathToVisitorInfoType,
	resourceType api.ResourceType,
	pathInfos api.PathToVisitorInfoType,
	getterFunctionInvocation *api.FunctionInvocation,
	setterFunctionInvocation *api.FunctionInvocation,
) {
	_, ok := registry[resourceType]
	if !ok {
		registry[resourceType] = make(api.PathToVisitorInfoType)
		for path, pathInfo := range pathInfos {
			registry[resourceType][path] = pathInfo
			setFunctionInvocationsInVisitorPathInfo(pathInfo, getterFunctionInvocation, setterFunctionInvocation)
		}
		return
	}

	// Some paths could already be registered under the same attribute name.
	// Example: resource references that could refer to multiple resource types.
	for path, newPathInfo := range pathInfos {
		oldPathInfo, present := registry[resourceType][path]
		if present {
			if !VisitorInfoEqual(oldPathInfo, newPathInfo, false) {
				log.Errorf("info mismatch for path %s: %v vs %v", newPathInfo.Path, newPathInfo, oldPathInfo)
			}
			newPathInfo = oldPathInfo
		} else {
			registry[resourceType][path] = newPathInfo
		}
		setFunctionInvocationsInVisitorPathInfo(newPathInfo, getterFunctionInvocation, setterFunctionInvocation)
	}
}

// RegisterPathsByAttributeName registers the specified path visitor specifications under the
// designated attribute name and resource type, and adds the provided getter and setter invocations,
// merging with existing registrations at the same paths, if any. If requested, the registered paths
// will be normalized so that associative lookups and array indices will be converted to wildcards,
// which is desired when matching all paths to the attribute. api.AttributeNameGeneral is used for
// registrations for general attributes of significance. api.AttributeNameNeededValue is used for
// needed values. api.AttributeNameProvidedValue is used for provided values. AttributeNameResourceName
// is used for references to resource names. Other attribute names are used for specific setters and/or
// getters, especially for attributes that appear in multiple resource types and/or locations.
// Provided values are special in that they represent sources of values for attributes of the specified
// attribute name, though they are logically distinct kinds of attributes.
func RegisterPathsByAttributeName(
	resourceProvider ResourceProvider,
	attributeName api.AttributeName,
	resourceType api.ResourceType,
	pathInfos api.PathToVisitorInfoType,
	getterFunctionInvocation *api.FunctionInvocation,
	setterFunctionInvocation *api.FunctionInvocation,
	normalizePaths bool,
) {
	pathRegistry := resourceProvider.GetPathRegistry()
	_, present := pathRegistry[attributeName]
	if !present {
		pathRegistry[attributeName] = make(api.ResourceTypeToPathToVisitorInfoType)
	}
	newPathInfos := pathInfos
	if normalizePaths {
		newPathInfos = make(api.PathToVisitorInfoType)
		for path, pathInfo := range pathInfos {
			fullyNormalizedPath := normalizePath(resourceType, path, false)
			normalizedPathWithBindings := normalizePath(resourceType, path, true)
			newPathInfo := *pathInfo // deep copy so the path isn't clobbered
			newPathInfo.Path = normalizedPathWithBindings
			newPathInfos[fullyNormalizedPath] = &newPathInfo
		}
	}
	registerPaths(
		pathRegistry[attributeName],
		resourceType,
		newPathInfos,
		getterFunctionInvocation,
		setterFunctionInvocation,
	)
}

// GetPathRegistryForAttributeName returns the registry for the specified attribute to pass
// to a visitor function.
func GetPathRegistryForAttributeName(
	resourceProvider ResourceProvider,
	attributeName api.AttributeName,
) api.ResourceTypeToPathToVisitorInfoType {
	pathRegistry := resourceProvider.GetPathRegistry()
	var resourceTypeToPathToVisitorInfo api.ResourceTypeToPathToVisitorInfoType
	resourceTypeToPathToVisitorInfo, _ = pathRegistry[attributeName]
	return resourceTypeToPathToVisitorInfo
}

// ResourceTypesForAttribute returns a list of resource types associated with the specified attribute.
func ResourceTypesForAttribute(attributeName api.AttributeName, resourceProvider ResourceProvider) []api.ResourceType {
	resourceTypeToPaths := GetPathRegistryForAttributeName(resourceProvider, attributeName)
	resourceTypes := make([]api.ResourceType, 0, len(resourceTypeToPaths))
	for resourceType := range resourceTypeToPaths {
		resourceTypes = append(resourceTypes, resourceType)
		// Just report all resource types if a wildcard is present
		if resourceType == api.ResourceTypeAny {
			break
		}
	}
	return resourceTypes
}

// ResourceTypesForPathMap returns a list of resource types from a path map.
func ResourceTypesForPathMap(pathMap map[api.ResourceType][]string) []api.ResourceType {
	resourceTypes := make([]api.ResourceType, 0, len(pathMap))
	for resourceType := range pathMap {
		resourceTypes = append(resourceTypes, resourceType)
		// Just report all resource types if a wildcard is present
		if resourceType == api.ResourceTypeAny {
			break
		}
	}
	return resourceTypes
}

// GetPathVisitorInfo returns the path visitor specification for the specified path within the
// specified resource type to pass to a visitor function.
func GetPathVisitorInfo(resourceProvider ResourceProvider, resourceType api.ResourceType, path api.UnresolvedPath) *api.PathVisitorInfo {
	normalizedPath := normalizePath(resourceType, path, false)
	// log.Infof("looked up info for resourceType %s path %s\n", resourceType, normalizedPath)

	var visitorInfo *api.PathVisitorInfo
	var resourceTypeToPathToVisitorInfo api.ResourceTypeToPathToVisitorInfoType
	var present bool
	pathRegistry := resourceProvider.GetPathRegistry()
	resourceTypeToPathToVisitorInfo, present = pathRegistry[api.AttributeNameGeneral]
	if !present {
		// This shouldn't happen
		log.Error("no general attribute path registry")
		return nil
	}
	_, present = resourceTypeToPathToVisitorInfo[resourceType]
	if present {
		visitorInfo, present = resourceTypeToPathToVisitorInfo[resourceType][normalizedPath]
	}
	if !present {
		// Try wildcard
		_, present = resourceTypeToPathToVisitorInfo[api.ResourceTypeAny]
		if present {
			visitorInfo, present = resourceTypeToPathToVisitorInfo[api.ResourceTypeAny][normalizedPath]
		}
	}
	if !present {
		return nil
	}
	if visitorInfo.AttributeName == api.AttributeNameNone {
		log.Debugf("path %s registered with no AttributeName", normalizedPath)
		visitorInfo.AttributeName = api.AttributeNameGeneral
	}
	// log.Infof("found info for resourceType %s path %s\n", resourceType, normalizedPath)
	return visitorInfo
}

// RegisterNeededPaths registers paths in the api.AttributeNameNeededValue path registry.
// These are paths of attributes that generally need to be set based on values extracted from
// other resources or configuration Objects, as opposed to values that would be set by default
// in a configuration object sample, set automatically based on default conventions, set
// by other registered mutation functions, set by other automated processes, or set imperatively
// via functions, UI, other tool, or just by editing the configuration data manually.
func RegisterNeededPaths(
	resourceProvider ResourceProvider,
	resourceType api.ResourceType,
	pathInfos api.PathToVisitorInfoType,
	setterFunctionInvocation *api.FunctionInvocation,
) {
	RegisterPathsByAttributeName(resourceProvider, api.AttributeNameNeededValue, resourceType, pathInfos, nil, setterFunctionInvocation, false)
}

// RegisterProvidedPaths registers paths in the api.AttributeNameProvidedValue path registry.
// These are paths of attributes that may provide values that could satisfy needed values within
// or across configuration Objects. Provided values are matched with Needed values when they have
// attribute names in common and matching getter (for the Provided value) and setter (for the Needed value)
// function invocation argument values, as disambiguators. The getter and setter function names do not
// need to match, since the provided attributes are expected to be of a different kind (different attribute
// name), from a different resource type.
func RegisterProvidedPaths(
	resourceProvider ResourceProvider,
	resourceType api.ResourceType,
	pathInfos api.PathToVisitorInfoType,
	getterFunctionInvocation *api.FunctionInvocation,
) {
	RegisterPathsByAttributeName(resourceProvider, api.AttributeNameProvidedValue, resourceType, pathInfos, getterFunctionInvocation, nil, false)
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
) (any, error) {
	docVisitor := func(doc *gaby.YamlDoc, output any, context VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		currentValue, ok := currentDoc.Data().(T)
		if ok {
			return visitor(doc, output, context, currentValue)
		}
		return output, fmt.Errorf("value %v at path %s cannot be converted to %T", currentDoc.Data(), string(context.Path), currentValue)
	}
	return VisitPathsDoc(parsedData, resourceTypeToPaths, keys, output, resourceProvider, docVisitor)
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
) (any, error) {
	docVisitor := func(doc *gaby.YamlDoc, output any, context VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		return visitor(doc, output, context, currentDoc.Data())
	}
	return VisitPathsDoc(parsedData, resourceTypeToPaths, keys, output, resourceProvider, docVisitor)
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
			resolvedPaths, err := ResolveAssociativePaths(doc, api.UnresolvedPath(unresolvedPathSegments[0]), api.ResolvedPath(pathConstraint[0]))
			if err != nil {
				// Don't report the error. Not found is expected.
				continue // Skip if an error
			}
			for _, resolvedPath := range resolvedPaths {
				// log.Infof("resolved path %s args %v in resource %s of type %s", resolvedPath.Path, resolvedPath.PathArguments, string(resourceName), string(resourceType))
				currentDoc, found, err := YamlSafePathGetDoc(doc, resolvedPath.Path, true)
				if err != nil || !found {
					// Don't report the error. Not found is expected.
					continue // Skip if not found or an error
				}
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
) error {

	visitor := func(doc *gaby.YamlDoc, output any, context VisitorContext, currentValue T) (any, error) {
		originalValue := currentValue
		newValue := updater(currentValue)
		var err error
		if newValue != originalValue {
			_, err = doc.SetP(newValue, string(context.Path))
		}
		return output, err
	}
	_, err := VisitPaths[T](parsedData, resourceTypeToPaths, keys, nil, resourceProvider, visitor)
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
) error {

	updater := func(_ T) T {
		return newValue
	}
	err := UpdatePathsFunction[T](parsedData, resourceTypeToPaths, keys, resourceProvider, updater)
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
) error {

	visitor := func(doc *gaby.YamlDoc, output any, context VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		originalDoc := currentDoc
		newDoc := updater(currentDoc)
		var err error
		if newDoc.String() != originalDoc.String() {
			_, err = doc.SetDocP(newDoc, string(context.Path))
		}
		return output, err
	}
	_, err := VisitPathsDoc(parsedData, resourceTypeToPaths, keys, nil, resourceProvider, visitor)
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
				embeddedValue, _ := context.Accessor.Extract(v, context.EmbeddedPath).(string)
				// If the data isn't a string or the pattern wasn't matched, embeddedValue should be empty
				currentValue = embeddedValue
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
				if stringVal, ok := currentValue.(string); ok && !strings.Contains(stringVal, PlaceHolderBlockApplyString) {
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
	output, err := VisitPathsDoc(parsedData, resourceTypeToPaths, keys, values, resourceProvider, visitor)
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
// using placeholder values, "replaceme" for strings. It can also extract fields embedded
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
) error {

	visitor := func(doc *gaby.YamlDoc, output any, context VisitorContext, currentValue string) (any, error) {
		originalValue := currentValue
		if context.EmbeddedPath != "" && context.Accessor != nil {
			embeddedValue, ok := context.Accessor.Extract(currentValue, context.EmbeddedPath).(string)
			// If the data isn't a string or the pattern wasn't matched, embeddedValue should be empty
			if !ok || embeddedValue == "" {
				return output, fmt.Errorf("embedded field %s not found at path %s", context.EmbeddedPath, string(context.Path)) // TODO: create an error type
			}
			currentValue = embeddedValue
		}
		newValue := updater(currentValue)
		if context.EmbeddedPath != "" && context.Accessor != nil {
			replacedValue, err := context.Accessor.Replace(originalValue, newValue, context.EmbeddedPath)
			if err != nil {
				return output, fmt.Errorf("embedded field %s not replaced at path %s", context.EmbeddedPath, string(context.Path)) // TODO: create an error type
			}
			newValue = replacedValue
		}
		var err error
		if newValue != originalValue {
			_, err = doc.SetP(newValue, string(context.Path))
		}
		return output, err
	}
	_, err := VisitPaths[string](parsedData, resourceTypeToPaths, keys, nil, resourceProvider, visitor)
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
) error {

	updater := func(_ string) string {
		return newValue
	}
	err := UpdateStringPathsFunction(parsedData, resourceTypeToPaths, keys, resourceProvider, updater)
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

func attributeValueForPath(path api.ResolvedPath, resourceInfo *api.ResourceInfo, value any) api.AttributeValue {
	// TODO: attributeName, dataType, Info.GetterInvocation, Info.SetterInvocations, Comment
	var attributeValue api.AttributeValue
	attributeValue.ResourceInfo = *resourceInfo
	attributeValue.Path = path
	attributeValue.AttributeName = api.AttributeNameGeneral // TODO: look up by path once they are all registered
	attributeValue.Value = value
	switch value.(type) {
	case string:
		attributeValue.DataType = api.DataTypeString
	case int:
		attributeValue.DataType = api.DataTypeInt
	case float64:
		// Ints are represented as "numbers", which parse as float64s
		attributeValue.DataType = api.DataTypeInt
	case bool:
		attributeValue.DataType = api.DataTypeBool
	default:
		// TODO: This may not be the best choice
		attributeValue.DataType = api.DataTypeJSON
	}
	return attributeValue
}

// FindYAMLPathsByValue searches for all paths that match a specified value in a YAML structure
// and returns an api.AttributeValueList.
func FindYAMLPathsByValue(parsedData gaby.Container, resourceProvider ResourceProvider, searchValue any) api.AttributeValueList {
	var paths api.AttributeValueList

	searchStringValue, searchValueIsString := searchValue.(string)

	// Recursive function to traverse YAML structure
	// TODO: use a worklist instead of recursion so that we can't blow our stack
	var traverse func(path string, doc *gaby.YamlDoc, resourceInfo *api.ResourceInfo)
	traverse = func(path string, doc *gaby.YamlDoc, resourceInfo *api.ResourceInfo) {
		children := doc.ChildrenMap()
		if len(children) > 0 {
			// If the container is a map, traverse its children
			for key, child := range children {
				var currentPath string
				// The key needs to be escaped so that the path can be parsed when passed back into functions
				escapedKey := EscapeDotsInPathSegment(key)
				if path != "" {
					currentPath = path + "." + escapedKey
				} else {
					currentPath = escapedKey
				}
				// TODO: factor this out into a function
				// Check if the value of the current key matches the search value
				if child.Data() == searchValue {
					attributeValue := attributeValueForPath(api.ResolvedPath(currentPath), resourceInfo, searchValue)
					paths = append(paths, attributeValue)
					// Skip further traversal since the match is found
					continue
				} else if searchValueIsString {
					stringVal, isString := child.Data().(string)
					if isString && strings.Contains(stringVal, searchStringValue) {
						attributeValue := attributeValueForPath(api.ResolvedPath(currentPath), resourceInfo, stringVal)
						paths = append(paths, attributeValue)
						// Skip further traversal since the match is found
						continue
					}
				}
				// Recursively traverse the YAML structure
				traverse(currentPath, child, resourceInfo)
			}
		} else if arrayChildren := doc.Children(); arrayChildren != nil {
			// NOTE: We'll also land here in the case of an empty map.

			// If the doc is an array, traverse its elements
			for index, child := range arrayChildren {
				currentPath := path + "." + strconv.Itoa(index)
				// Check if the value of the current array element matches the search value
				if child.Data() == searchValue {
					attributeValue := attributeValueForPath(api.ResolvedPath(currentPath), resourceInfo, searchValue)
					paths = append(paths, attributeValue)
					// Skip further traversal since the match is found
					continue
				} else if searchValueIsString {
					stringVal, isString := child.Data().(string)
					if isString && strings.Contains(stringVal, searchStringValue) {
						attributeValue := attributeValueForPath(api.ResolvedPath(currentPath), resourceInfo, stringVal)
						paths = append(paths, attributeValue)
						// Skip further traversal since the match is found
						continue
					}
				}
				// Recursively traverse the YAML structure
				traverse(currentPath, child, resourceInfo)
			}
		} else {
			// If the doc is neither a map nor an array, it's a value; compare it
			if path != "" {
				if doc.Data() == searchValue {
					attributeValue := attributeValueForPath(api.ResolvedPath(path), resourceInfo, searchValue)
					paths = append(paths, attributeValue)
				} else if searchValueIsString {
					stringVal, isString := doc.Data().(string)
					if isString && strings.Contains(stringVal, searchStringValue) {
						attributeValue := attributeValueForPath(api.ResolvedPath(path), resourceInfo, stringVal)
						paths = append(paths, attributeValue)
					}
				}
			}
		}
	}

	visitor := func(doc *gaby.YamlDoc, _ any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
		// Start traversal from the root
		traverse("", doc, resourceInfo)
		return nil, []error{}
	}
	VisitResources(parsedData, nil, resourceProvider, visitor)

	// TODO: Revisit. Did this for predictable order.
	sort.Slice(paths, attributeValueCompareFunction(paths))

	return paths
}

func EvalYQExpression(expr string, yamlString string) (string, error) {
	yqlogger.SetLevel(yqlogger.WARNING, "yq-lib")
	encoder := yqlib.NewYamlEncoder(yqlib.ConfiguredYamlPreferences)
	decoder := yqlib.NewYamlDecoder(yqlib.ConfiguredYamlPreferences)
	result, err := yqlib.NewStringEvaluator().EvaluateAll(expr, yamlString, encoder, decoder)
	if err != nil {
		return "", err
	}
	return result, nil
}

// ComputeMutationsForDocs determines the edits that have been performed to transform the previousDoc
// into modifiedDoc. The resulting mutations are associated with the provided functionIndex.
// The pathMutationMap is modified in place.
func ComputeMutationsForDocs(rootPath string, previousDoc *gaby.YamlDoc, modifiedDoc *gaby.YamlDoc, functionIndex int64, pathMutationMap api.MutationMap) {
	// TODO: Determine whether there should be any error conditions.

	// TODO: Decide how to tombstone removed paths so they are not later re-added
	// by a patch. Example: a port in a Service is removed from a downstream unit and
	// some part of that port spec is modified in the upstream unit. The next PatchMutations
	// for upgrade would reinsert the port.

	// TODO: Handle associative lists using schema information from the ResourceProvider.

	// TODO: Decide what to do about embedded accessors

	// Define a traversal item for our stack
	type traversalItem struct {
		path        string
		previousDoc *gaby.YamlDoc
		modifiedDoc *gaby.YamlDoc
	}

	// Initialize the stack with the root traversal item
	stack := []traversalItem{{
		path:        rootPath,
		previousDoc: previousDoc,
		modifiedDoc: modifiedDoc,
	}}

	// Process items until the stack is empty
	for len(stack) > 0 {
		// Pop the top item from the stack
		last := len(stack) - 1
		item := stack[last]
		stack = stack[:last]

		path := item.path
		previousDoc := item.previousDoc
		modifiedDoc := item.modifiedDoc

		// Now process this item (similar logic to the recursive function)
		modifiedChildren := modifiedDoc.ChildrenMap()
		previousChildren := previousDoc.ChildrenMap()

		if len(modifiedChildren) > 0 {
			if len(previousChildren) == 0 {
				// modifiedDoc is a map, but previousDoc is not a map, though it exists.
				// The path's contents have completely changed in this case.
				pathMutationMap[api.ResolvedPath(path)] = api.MutationInfo{
					MutationType: api.MutationTypeUpdate,
					Index:        functionIndex,
					Predicate:    true,
					Value:        modifiedDoc.String(), // new data
				}
				continue // process next stack element
			}

			// Process all modified children
			for key, modifiedChild := range modifiedChildren {
				var currentPath string
				if path != "" {
					currentPath = path + "." + EscapeDotsInPathSegment(key)
				} else {
					currentPath = EscapeDotsInPathSegment(key)
				}

				previousChild, present := previousChildren[key]
				if !present {
					pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
						MutationType: api.MutationTypeAdd,
						Index:        functionIndex,
						Predicate:    true,
						Value:        modifiedChild.String(), // new data
					}
					continue // process next stack element
				}

				// Instead of recursion, push this item to the stack
				stack = append(stack, traversalItem{
					path:        currentPath,
					previousDoc: previousChild,
					modifiedDoc: modifiedChild,
				})

				delete(previousChildren, key)
			}

			// Remaining previousChildren must have been deleted
			for key, previousChild := range previousChildren {
				var currentPath string
				if path != "" {
					currentPath = path + "." + EscapeDotsInPathSegment(key)
				} else {
					currentPath = EscapeDotsInPathSegment(key)
				}
				pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
					MutationType: api.MutationTypeDelete,
					Index:        functionIndex,
					Predicate:    true,
					Value:        previousChild.String(), // deleted data
				}
			}
		} else if modifiedArrayChildren := modifiedDoc.Children(); modifiedArrayChildren != nil {
			// TODO: Handle associative arrays.
			// For now, compare arrays positionally, treating differences in length as
			// additions and deletions.
			// We'll also land here in the case of an empty map. Or empty arrays.
			previousArrayChildren := previousDoc.Children()
			if len(modifiedArrayChildren) == 0 && len(previousArrayChildren) == 0 {
				// Both are empty. No changes.
				continue // process next stack element
			}

			if !modifiedDoc.IsArray() {
				// modifiedDoc is an empty map.
				if len(previousChildren) != 0 {
					// The map children were deleted.
					for key, previousChild := range previousChildren {
						var currentPath string
						if path != "" {
							currentPath = path + "." + EscapeDotsInPathSegment(key)
						} else {
							currentPath = EscapeDotsInPathSegment(key)
						}
						pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
							MutationType: api.MutationTypeDelete,
							Index:        functionIndex,
							Predicate:    true,
							Value:        previousChild.String(), // deleted data
						}
					}
				} else {
					// The whole path was changed.
					pathMutationMap[api.ResolvedPath(path)] = api.MutationInfo{
						MutationType: api.MutationTypeUpdate,
						Index:        functionIndex,
						Predicate:    true,
						Value:        modifiedDoc.String(), // new data
					}
				}
				continue // process next stack element
			}

			if modifiedDoc.IsArray() && !previousDoc.IsArray() {
				// modifiedDoc is an array, but previousDoc is not an array, though it exists.
				// The path's contents have completely changed in this case.
				pathMutationMap[api.ResolvedPath(path)] = api.MutationInfo{
					MutationType: api.MutationTypeUpdate,
					Index:        functionIndex,
					Predicate:    true,
					Value:        modifiedDoc.String(), // new data
				}
				continue // process next stack element
			}

			// Process array children
			for index, modifiedChild := range modifiedArrayChildren {
				// Arrays have to have a preceding path.
				currentPath := path + "." + strconv.Itoa(index)
				if index >= len(previousArrayChildren) {
					pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
						MutationType: api.MutationTypeAdd,
						Index:        functionIndex,
						Predicate:    true,
						Value:        modifiedChild.String(), // new data
					}
					continue // process next stack element
				}

				previousChild := previousArrayChildren[index]

				// Push this comparison to the stack
				stack = append(stack, traversalItem{
					path:        currentPath,
					previousDoc: previousChild,
					modifiedDoc: modifiedChild,
				})
			}

			// Process array elements that were deleted
			index := len(modifiedArrayChildren)
			for index < len(previousArrayChildren) {
				currentPath := path + "." + strconv.Itoa(index)
				pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
					MutationType: api.MutationTypeDelete,
					Index:        functionIndex,
					Predicate:    true,
					Value:        previousArrayChildren[index].String(), // previous data
				}
				index++
			}
		} else {
			// modifiedDoc must be a value. Compare the contents.
			if modifiedDoc.String() != previousDoc.String() {
				pathMutationMap[api.ResolvedPath(path)] = api.MutationInfo{
					MutationType: api.MutationTypeUpdate,
					Index:        functionIndex,
					Predicate:    true,
					Value:        modifiedDoc.String(), // new data
				}
				// log.Infof("different values: '%s' vs '%s'", previousDoc.String(), modifiedDoc.String())
			}
		}
	}
}

// FIXME: Remove this once all existing clones are converted to Aliases/AliasesWithoutScopes.
const OriginalNameAnnotation = "confighub.com/OriginalName"

var originalNamePath = "metadata.annotations." + EscapeDotsInPathSegment(OriginalNameAnnotation)

// ComputeMutations performs a kind of diff between two configuration Units where it determines what
// modifications were made at the resource/element level and at the path level. They are recorded in a
// way that can be accumulated and updated over subsequent edits and transformations.
func ComputeMutations(previousParsedData, modifiedParsedData gaby.Container, functionIndex int64, resourceProvider ResourceProvider) (api.ResourceMutationList, error) {
	// There are limits in how accurately we can determine the correspondence between resources/elements
	// across revisions. Once resources/elements change too significantly, they will be determined to be
	// distinct. Some properties, such as the ResourceCategory, ResourceType, and ResourceName, carry more
	// significance than other attributes. Also, presence of paths (keys) should carry more weight than values.
	// Line diffs use surrounding lines for context to identify matches, which sometimes works well,
	// but also can be fragile, such as in the case of insertions of partially similar blocks, or minor
	// changes in syntax, such as presence or absence of trailing commas.
	// Since we don't expect a vast number of resources/elements per unit, an algorithm that is quadratic in
	// numbers of resources/elements, such as using Jaccard Similarity or Levenshtein Distance, is acceptable.
	// As opposed to some kind of higher-dimensional vector distance using embeddings.
	// https://www.geeksforgeeks.org/jaccard-similarity/ -- intersection size divided by union size
	// https://www.geeksforgeeks.org/introduction-to-levenshtein-distance/ -- number of edits
	// To compute Jaccard Similarity we'd need to enumerate all of the paths and values using a visitor.
	// To compute the Levenshtein Distance we can use ComputeMutationsForDocs, but it only provides
	// relatively similarity rather than absolute similarity, so we need to normalize the deltas similar to
	// Jaccard Similarity. For now I'll use the line count as the denominator.
	// Of course, we should optimize for the common case that resources are modified in their same positions
	// and are not renamed nor have types changed.
	// I decided not to impose a canonical order based on resource name because it would cause resources to
	// move when they are renamed, such as during cloning.

	// We could start with either the previous docs or the modified docs. I chose the latter, since the
	// modified docs represent the new/current content.

	mutations := api.ResourceMutationList{}
	previousDocMatched := make([]bool, len(previousParsedData))
	minUnmatchedPreviousDocIndex := 0
	modifiedDocIndex := 0
	for modifiedDocIndex < len(modifiedParsedData) {
		modifiedDoc := modifiedParsedData[modifiedDocIndex]
		modifiedResourceCategory, modifiedResourceType, modifiedResourceName, err := GetResourceCategoryTypeName(modifiedDoc, resourceProvider)
		if err != nil {
			return nil, err
		}
		modifiedResourceNameOnly := resourceProvider.RemoveScopeFromResourceName(modifiedResourceName)

		// Check whether the "next" resource obviously matches in the previous doc list.
		// If not, we need to search for it. We could make maps of type and name to index,
		// but that wouldn't work in the case of type changes, so I'm punting on that for
		// simplicity for now.

		// Search previousDocMatched starting with minUnmatchedPreviousDocIndex.
		matchIndex := -1
		bestMatchScore := math.MaxFloat64
		// TODO: Determine a reasonable threshold. If the name of a Namespace changes, that's one line in 4, or 0.25.
		// It's also possible that we should always consider another resource of the same type as the same resource
		// if there's only one.
		maxMatchScore := 1.0
		numDocLines := strings.Count(modifiedParsedData.String(), "\n")
		var pathMutationMap api.MutationMap
		minMutationLength := math.MaxInt
		aliases := map[api.ResourceName]struct{}{}
		aliasesWithoutScopes := map[api.ResourceName]struct{}{}
		for previousDocIndex := minUnmatchedPreviousDocIndex; previousDocIndex < len(previousDocMatched); previousDocIndex++ {
			previousDoc := previousParsedData[previousDocIndex]
			previousResourceCategory, previousResourceType, previousResourceName, err := GetResourceCategoryTypeName(previousDoc, resourceProvider)
			if err != nil {
				return nil, err
			}
			if previousResourceCategory != modifiedResourceCategory {
				continue
			}
			// TODO: favor exact match
			if !resourceProvider.ResourceTypesAreSimilar(previousResourceType, modifiedResourceType) {
				continue
			}

			// Do a deep diff
			tmpMutationMap := api.MutationMap{}
			ComputeMutationsForDocs("", previousDoc, modifiedDoc, functionIndex, tmpMutationMap)

			// TODO: favor exact match
			// TODO: special-case changes of a placeholder scope to a non-placeholder scope
			previousResourceNameOnly := resourceProvider.RemoveScopeFromResourceName(previousResourceName)
			if previousResourceName == modifiedResourceName || previousResourceNameOnly == modifiedResourceNameOnly {
				matchIndex = previousDocIndex
				bestMatchScore = 0.0
				pathMutationMap = tmpMutationMap
				// Re-initialize aliases and aliasesWithoutScopes
				aliases = map[api.ResourceName]struct{}{
					previousResourceName: {},
				}
				aliasesWithoutScopes = map[api.ResourceName]struct{}{
					previousResourceNameOnly: {},
				}
				break
			}
			// TODO: Figure out a better way to determine name changes.
			// Kustomize records name changes when they occur at the field mutation level, but
			// that doesn't work for out-of-band (non-filter) changes.
			// https://github.com/kubernetes-sigs/kustomize/blob/616c08480583c24b1828111a6e9e720735676979/api/filters/prefix/prefix.go#L29
			// https://github.com/kubernetes-sigs/kustomize/blob/616c08480583c24b1828111a6e9e720735676979/api/filters/suffix/suffix.go#L29
			// TODO: special-case changes of the placeholder name to a non-placeholder name
			// TODO: special-case matching indices and/or clones by setting the score to the minimum matching score
			// TODO: special-case the only resource of matching type
			// TODO: some attributes, like container names and images, are more important than others
			// TODO: Do we need a name kernel pattern to deal with common prefixes and suffixes?
			// TODO: take into account the number of subpaths (leaf values) of the paths in the map
			if len(tmpMutationMap) < minMutationLength {
				minMutationLength = len(tmpMutationMap)
				pathMutationMap = tmpMutationMap
				bestMatchScore = float64(minMutationLength) / float64(numDocLines)
				matchIndex = previousDocIndex
				// Re-initialize aliases and aliasesWithoutScopes
				aliases = map[api.ResourceName]struct{}{
					previousResourceName: {},
				}
				aliasesWithoutScopes = map[api.ResourceName]struct{}{
					previousResourceNameOnly: {},
				}
			}
		}

		// If no match was found, then we need to add this resource. During Create,
		// including cloning, the previous data should be empty.
		if matchIndex < 0 || bestMatchScore > maxMatchScore {
			mutations = append(mutations, api.ResourceMutation{
				Resource: api.ResourceInfo{
					ResourceType:             modifiedResourceType,
					ResourceName:             modifiedResourceName,
					ResourceNameWithoutScope: modifiedResourceNameOnly,
					ResourceCategory:         modifiedResourceCategory,
				},
				ResourceMutationInfo: api.MutationInfo{
					MutationType: api.MutationTypeAdd,
					Index:        functionIndex,
					Predicate:    true,
					Value:        modifiedDoc.String(), // new data
				},
				// Don't use pathMutationMap, if any
				PathMutationMap: make(api.MutationMap),
				// Don't use previous aliases, if any
				Aliases: map[api.ResourceName]struct{}{
					modifiedResourceName: {},
				},
				AliasesWithoutScopes: map[api.ResourceName]struct{}{
					modifiedResourceNameOnly: {},
				},
			})
			modifiedDocIndex++
			continue
		}

		// A match for the resource was found. It possibly was changed.

		// Add new aliases, if any
		aliases[modifiedResourceName] = struct{}{}
		aliasesWithoutScopes[modifiedResourceNameOnly] = struct{}{}
		mutation := api.ResourceMutation{
			Resource: api.ResourceInfo{
				ResourceType:             modifiedResourceType,
				ResourceName:             modifiedResourceName,
				ResourceNameWithoutScope: modifiedResourceNameOnly,
				ResourceCategory:         modifiedResourceCategory,
			},
			ResourceMutationInfo: api.MutationInfo{
				MutationType: api.MutationTypeUpdate, // assume changed
				Index:        functionIndex,
				Predicate:    true,
				// no Value at this level
			},
			PathMutationMap:      pathMutationMap,
			Aliases:              aliases,
			AliasesWithoutScopes: aliasesWithoutScopes,
		}
		if len(pathMutationMap) == 0 {
			mutation.ResourceMutationInfo.MutationType = api.MutationTypeNone
		}
		mutations = append(mutations, mutation)
		modifiedDocIndex++

		// This assumes matchIndex >= 0
		// Find the next unmatched index, if any
		previousDocMatched[matchIndex] = true
		if minUnmatchedPreviousDocIndex == matchIndex {
			minUnmatchedPreviousDocIndex++
			for minUnmatchedPreviousDocIndex < len(previousDocMatched) {
				if !previousDocMatched[minUnmatchedPreviousDocIndex] {
					break
				}
				minUnmatchedPreviousDocIndex++
			}
		}
	}

	// Any remaining unmatched resources were deletions.
	for minUnmatchedPreviousDocIndex < len(previousDocMatched) {
		// Skip matched resources
		if previousDocMatched[minUnmatchedPreviousDocIndex] {
			minUnmatchedPreviousDocIndex++
			continue
		}

		previousDoc := previousParsedData[minUnmatchedPreviousDocIndex]
		previousResourceCategory, previousResourceType, previousResourceName, err := GetResourceCategoryTypeName(previousDoc, resourceProvider)
		if err != nil {
			return nil, err
		}
		previousResourceNameOnly := resourceProvider.RemoveScopeFromResourceName(previousResourceName)
		mutations = append(mutations, api.ResourceMutation{
			Resource: api.ResourceInfo{
				ResourceType:             previousResourceType,
				ResourceName:             previousResourceName,
				ResourceNameWithoutScope: previousResourceNameOnly,
				ResourceCategory:         previousResourceCategory,
			},
			ResourceMutationInfo: api.MutationInfo{
				MutationType: api.MutationTypeDelete,
				Index:        functionIndex,
				Predicate:    true,
				Value:        previousDoc.String(), // previous data
			},
			PathMutationMap: make(api.MutationMap),
			Aliases: map[api.ResourceName]struct{}{
				previousResourceName: {},
			},
			AliasesWithoutScopes: map[api.ResourceName]struct{}{
				previousResourceNameOnly: {},
			},
		})
		minUnmatchedPreviousDocIndex++
	}

	return mutations, nil
}

// PatchMutations replays the mutations in mutationsPatch on the provided configuration data.
// mutationsPatch is sometimes generated from other configuration units, such as in the canonical
// case of upgrade from upstream. Or may be generated from past revisions or even live state.
// So it may not match the provided configuration data in some ways, such as resource names
// and whole resources.
// By default all resources and paths are patchable. Predicates are used to preserve existing changes.
// mutationsPredicates is expected to have been generated from the mutations corresponding to the
// configuration data being patched. So it is expected to match the contents of parsedData.
// It is acceptable for mutationsPredicates to be nil.
func PatchMutations(parsedData gaby.Container, mutationsPredicates, mutationsPatch api.ResourceMutationList, resourceProvider ResourceProvider) (gaby.Container, error) {
	// If mutationsPredicates is nil, then mutationPredicateMap will be empty.
	mutationPredicateMap := make(map[api.ResourceTypeAndName]int)
	for i := range mutationsPredicates {
		resourceInfo := mutationsPredicates[i].Resource
		// ResourceNameWithoutScope is a new field so it may not be present in all cases.
		if resourceInfo.ResourceNameWithoutScope == "" {
			resourceInfo.ResourceNameWithoutScope = resourceProvider.RemoveScopeFromResourceName(resourceInfo.ResourceName)
		}
		resourceInfoKey := api.ResourceTypeAndNameFromResourceInfo(resourceInfo)
		mutationPredicateMap[resourceInfoKey] = i
	}

	mutationPatchMap := make(map[api.ResourceTypeAndName]int)
	for i := range mutationsPatch {
		resourceInfo := mutationsPatch[i].Resource
		if resourceInfo.ResourceNameWithoutScope == "" {
			resourceInfo.ResourceNameWithoutScope = resourceProvider.RemoveScopeFromResourceName(resourceInfo.ResourceName)
		}
		resourceInfoKey := api.ResourceTypeAndNameFromResourceInfo(resourceInfo)
		mutationPatchMap[resourceInfoKey] = i
	}

	for docIndex, doc := range parsedData {
		resourceCategory, resourceType, resourceName, err := GetResourceCategoryTypeName(doc, resourceProvider)
		if err != nil {
			return parsedData, err
		}
		resourceInfo := api.ResourceInfo{
			ResourceName:             resourceName,
			ResourceNameWithoutScope: resourceProvider.RemoveScopeFromResourceName(resourceName),
			ResourceType:             resourceType,
			ResourceCategory:         resourceCategory,
		}
		resourceInfoKey := api.ResourceTypeAndNameFromResourceInfo(resourceInfo)

		mutationPredicateIndex, hasPredicate := mutationPredicateMap[resourceInfoKey]

		// Filter the patch.
		if hasPredicate && !mutationsPredicates[mutationPredicateIndex].ResourceMutationInfo.Predicate {
			log.Infof("patch filtered for %s", resourceInfoKey)
			continue
		}

		aliasInfo := api.ResourceInfo{
			// ResourceNameWithoutScope:     resourceProvider.RemoveScopeFromResourceName(resourceName),
			ResourceType:     resourceType,
			ResourceCategory: resourceCategory,
		}
		var aliasInfoKey api.ResourceTypeAndName

		// FIXME: Remove this once all existing clones are converted to AliasesWithoutScopes.
		// TODO: This assumes the unit may be a clone, in which case it may be
		// patched from upstream. If the patch were from a clone to be applied
		// upstream, we'd need to get this information and pass it in.
		originalName, found, err := YamlSafePathGetValue[string](doc, api.ResolvedPath(originalNamePath), true)
		if err != nil {
			return parsedData, err
		}
		if found {
			aliasInfo.ResourceName = api.ResourceName(originalName)
			aliasInfo.ResourceNameWithoutScope = resourceProvider.RemoveScopeFromResourceName(api.ResourceName(originalName))
			aliasInfoKey = api.ResourceTypeAndNameFromResourceInfo(aliasInfo)
		}

		mutationPatchIndex, ok := mutationPatchMap[resourceInfoKey]
		if !ok {
			// originalInfoKey might be "", but that's ok
			mutationPatchIndex, ok = mutationPatchMap[aliasInfoKey]
			if !ok {
				// If present, mutationsPredicates is expected to have been generated from the mutations
				// corresponding to the configuration data being patched. Therefore, it may
				// contain the aliases for resources present in the configuration.
				if hasPredicate {
					// We may have already checked a couple of these, but just check them all.
					for alias := range mutationsPredicates[mutationPredicateIndex].AliasesWithoutScopes {
						// TODO: This doesn't work for resource type changes, like Deployment -> StatefulSet
						// We don't need to set aliasInfo.ResourceName
						aliasInfo.ResourceNameWithoutScope = alias
						aliasInfoKey = api.ResourceTypeAndNameFromResourceInfo(aliasInfo)
						mutationPatchIndex, ok = mutationPatchMap[aliasInfoKey]
						if ok {
							break
						}
					}
				}
				if !ok {
					continue
				}
			}
		}

		resourcePatchMutation := &mutationsPatch[mutationPatchIndex].ResourceMutationInfo
		switch resourcePatchMutation.MutationType {
		case api.MutationTypeAdd, api.MutationTypeReplace:
			// Replace at the resource level means there was a delete then an add, so
			// treat it like add.
			valueString := resourcePatchMutation.Value
			valueDoc, err := gaby.ParseYAML([]byte(valueString))
			if err != nil {
				log.Infof("error parsing value for resource %s: %v", string(resourceInfoKey), err)
			}
			parsedData[docIndex] = valueDoc
			// Some paths also could have been modified
		case api.MutationTypeDelete:
			// TODO: Make sure this works
			// TODO: Probably should eliminate empty docs in gaby_multidoc.go
			err := doc.DeleteP(".")
			if err != nil {
				log.Infof("error deleting root path: %v", err)
			}
			// Shouldn't be any modified paths
			continue
		case api.MutationTypeNone:
			// None at the resource level means the resource wasn't modified.
			continue
		case api.MutationTypeUpdate:
			// Update at the resource level means some paths were modified.
		}

		// PathMutationMap is a map, which could be in arbitrary order.
		// We should process parents before children, so we copy the map into an array
		// and sort it.
		patches := make([]*api.MutationMapEntry, len(mutationsPatch[mutationPatchIndex].PathMutationMap))
		patchIndex := 0
		for patchPath, patchMutation := range mutationsPatch[mutationPatchIndex].PathMutationMap {
			patches[patchIndex] = &api.MutationMapEntry{
				Path:         patchPath,
				MutationInfo: &patchMutation,
			}
			patchIndex++
		}
		sort.Slice(patches, func(i, j int) bool {
			return patches[i].Path < patches[j].Path
		})

		for i := range patches {
			patchPath := patches[i].Path
			patchMutation := patches[i].MutationInfo
			// Check for patches that conflict with the patch.
			// TODO: Break down the patch.
			if hasPredicate {
				filtered := false
				// Check all path prefixes in the map bottom up. We use gaby.DotPathToSlice to handle
				// escaping and quoting, if any.
				pathSegments := gaby.DotPathToSlice(string(patchPath))
				for len(pathSegments) > 0 {
					filteredPath := JoinPathSegments(pathSegments)
					predicateMutation, hasFilter := mutationsPredicates[mutationPredicateIndex].PathMutationMap[api.ResolvedPath(filteredPath)]
					if hasFilter && !predicateMutation.Predicate {
						filtered = true
						break
					}
					pathSegments = pathSegments[:len(pathSegments)-1]
				}
				if filtered {
					log.Debugf("path %s filtered", string(patchPath))
					continue
				}
			}
			// TODO: what should we do about errors?
			switch patchMutation.MutationType {
			case api.MutationTypeAdd, api.MutationTypeUpdate, api.MutationTypeReplace:
				valueString := patchMutation.Value
				valueDoc, err := gaby.ParseYAML([]byte(valueString))
				if err != nil {
					log.Infof("error parsing value at path %s: %v", string(patchPath), err)
				}
				// Note: This doesn't preserve indentation nor field ordering.
				_, err = doc.SetDocP(valueDoc, string(patchPath))
				if err != nil {
					log.Infof("error setting value at path %s: %v", string(patchPath), err)
				}
			case api.MutationTypeDelete:
				err := doc.DeleteP(string(patchPath))
				if err != nil {
					log.Infof("error deleting path %s: %v", string(patchPath), err)
				}
			case api.MutationTypeNone:
				// Shouldn't happen for paths, but also shouldn't be anything to do
			}
		}
	}

	return parsedData, nil
}

func Reset(parsedData gaby.Container, mutationsPredicates api.ResourceMutationList, resourceProvider ResourceProvider) error {
	mutationPredicateMap := make(map[api.ResourceTypeAndName]int)
	for i := range mutationsPredicates {
		resourceInfo := mutationsPredicates[i].Resource
		if resourceInfo.ResourceNameWithoutScope == "" {
			resourceInfo.ResourceNameWithoutScope = resourceProvider.RemoveScopeFromResourceName(resourceInfo.ResourceName)
		}
		resourceInfoKey := api.ResourceTypeAndNameFromResourceInfo(resourceInfo)
		mutationPredicateMap[resourceInfoKey] = i
	}

	for _, doc := range parsedData {
		resourceCategory, resourceType, resourceName, err := GetResourceCategoryTypeName(doc, resourceProvider)
		if err != nil {
			return err
		}
		resourceInfoKey := api.ResourceTypeAndNameFromResourceInfo(api.ResourceInfo{
			ResourceName:             resourceName,
			ResourceNameWithoutScope: resourceProvider.RemoveScopeFromResourceName(resourceName),
			ResourceType:             resourceType,
			ResourceCategory:         resourceCategory,
		})

		mutationPredicateIndex, hasPredicate := mutationPredicateMap[resourceInfoKey]
		if !hasPredicate {
			// Nothing to reset
			continue
		}

		// TODO: The predicate for the resource could set the default, but would require traversing
		// all the paths, like FindYAMLPathsByValue.
		// shouldBeReset := hasPredicate && mutationsPredicates[mutationPredicateIndex].ResourceMutationInfo.Predicate
		// PathMutationMap is a map, which could be in arbitrary order.
		// We're only going to reset leaves, so that should be ok.

		for path, mutation := range mutationsPredicates[mutationPredicateIndex].PathMutationMap {
			if !mutation.Predicate {
				// Shouldn't be reset
				continue
			}
			value, found, err := YamlSafePathGetValueAnyType(doc, path, true)
			if err != nil {
				return err
			}
			if !found {
				continue
			}
			switch value.(type) {
			case string:
				_, err = doc.SetP(PlaceHolderBlockApplyString, string(path))
				if err != nil {
					log.Infof("error setting string value at path %s: %v", string(path), err)
				}
			case int:
				_, err = doc.SetP(PlaceHolderBlockApplyInt, string(path))
				if err != nil {
					log.Infof("error setting int value at path %s: %v", string(path), err)
				}
			default:
				// Not a leaf or no placeholder value. Skip.
			}
		}
	}
	return nil
}
