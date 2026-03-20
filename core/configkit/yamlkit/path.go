// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"fmt"
	"strconv"
	"strings"

	"log/slog"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

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
			return nil, false, nil
		} else {
			return nil, false, fmt.Errorf("%s not found", resolvedPathString)
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

func PathIsResolved(path string, includeAt bool) bool {
	if includeAt {
		return !strings.ContainsAny(path, "?*@|")
	}
	return !strings.ContainsAny(path, "?*|")
}

// parseParameterInfo parses a @ prefixed segment to extract parameter value and name
// Returns (segment, parameterValue, parameterName, error)
func parseParameterInfo(segment string) (string, string, string, error) {
	if !strings.HasPrefix(segment, "@") {
		return segment, "", "", nil
	}

	keyName := strings.TrimPrefix(segment, "@")
	keyNameParts := strings.Split(keyName, ":")
	switch len(keyNameParts) {
	case 1:
		// No parameter name
		return keyNameParts[0], "", "", nil
	case 2:
		parameterValue := keyNameParts[0]
		parameterName := keyNameParts[1]
		return parameterValue, parameterValue, parameterName, nil
	default:
		return "", "", "", fmt.Errorf("invalid map parameter expression '%s'", segment)
	}
}

// ResolveAssociativePaths resolves an associative path with associative lookups (?) and wildcards (*, *?, *@)
// into specific resolved paths and discovered path parameters.
// See the documentation for api.UnresolvedPath for more details.
func ResolveAssociativePaths(
	doc *gaby.YamlDoc,
	unresolvedPath api.UnresolvedPath,
	resolvedPath api.ResolvedPath,
	upsert bool,
) ([]ResolvedPathInfo, error) {

	path := string(unresolvedPath)
	if path == "" {
		return []ResolvedPathInfo{}, fmt.Errorf("path cannot be empty")
	}
	if PathIsResolved(path, true) {
		return []ResolvedPathInfo{{Path: api.ResolvedPath(path)}}, nil
	}
	// DotPathToSlice converts escaped dots back to unescaped dots, so we need to convert
	// them back when constructing the path
	segments := gaby.DotPathToSlice(path)
	var constraintSegments []string
	if resolvedPath != "" {
		constraintSegments = gaby.DotPathToSlice(string(resolvedPath))
		if len(constraintSegments) != len(segments) {
			slog.Debug("unresolved path and resolved path have different numbers of segments",
				"unresolvedPath", path, "resolvedPath", string(resolvedPath))
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

		// Transform associative lookup with wildcard value to wildcard form
		// Convert ?key=* to *?key
		if strings.HasPrefix(segment, "?") && strings.HasSuffix(segment, "=*") {
			segment = "*" + strings.TrimSuffix(segment, "=*")
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
			value := kvParts[1]
			directIndexString := ""
			fallbackIndexString := ""
			if strings.HasPrefix(value, "@") {
				// Support the syntax @<index> to specify an index even though the path was registered as associative
				directIndexString = strings.TrimPrefix(value, "@")
			} else if semiAt := strings.Index(value, ";@"); semiAt >= 0 {
				// Support ?key=value;@index — try key match first, fall back to index
				fallbackIndexString = value[semiAt+2:]
				value = value[:semiAt]
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
				if (indexString == directIndexString) ||
					(fieldValueNode != nil && (fieldValueNode.Data() == value || constraintSegment != "")) {
					// Found the matching element. Just update the head of the queue.
					workList[0].ResolvedSegments = append(workList[0].ResolvedSegments, indexString)
					workList[0].ParentNode = child
					workList[0].CurrentSegmentIndex++
					workList[0].PathArguments = append(workList[0].PathArguments, api.FunctionArgument{ParameterName: parameterName, Value: fieldValueNode.Data()})
					found = true
					break
				}
			}
			// Fall back to numeric index if key match failed and ;@index was provided
			if !found && fallbackIndexString != "" {
				fallbackIndex, err := strconv.Atoi(fallbackIndexString)
				if err == nil && fallbackIndex >= 0 && fallbackIndex < len(elements) {
					child := elements[fallbackIndex]
					workList[0].ResolvedSegments = append(workList[0].ResolvedSegments, fallbackIndexString)
					workList[0].ParentNode = child
					workList[0].CurrentSegmentIndex++
					fieldValueNode := child.S(parameterKey)
					if fieldValueNode != nil {
						workList[0].PathArguments = append(workList[0].PathArguments, api.FunctionArgument{ParameterName: parameterName, Value: fieldValueNode.Data()})
					}
					found = true
				}
			}
			if !found {
				// Not found
				// Dequeue and continue
				workList = workList[1:]
			}

		default:
			// Regular segment. Assume it matches the constraint, if any.
			var parameterName, parameterValue string
			var err error

			if strings.HasPrefix(segment, "|") {
				// Handle "|" syntax - path needs to exist up to this point
				segment = strings.TrimPrefix(segment, "|")
			}

			segment, parameterValue, parameterName, err = parseParameterInfo(segment)
			if err != nil {
				return []ResolvedPathInfo{}, err
			}

			// This segment traversal doesn't need to have dots escaped
			currentNode := workList[0].ParentNode.S(segment)
			if currentNode == nil && !upsert {
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

			// Handle upsert mode: if all remaining segments are resolved, append them and mark as complete
			if upsert {
				allRemainingResolved := true
				for i := workList[0].CurrentSegmentIndex; i < len(segments); i++ {
					if !PathIsResolved(segments[i], false) {
						allRemainingResolved = false
						break
					}
				}
				if allRemainingResolved {
					// TODO: Confirm that constraint segments match, if present
					// Append all remaining segments to ResolvedSegments
					for i := workList[0].CurrentSegmentIndex; i < len(segments); i++ {
						remainingSegment := segments[i]
						// Handle @ prefixed segments by extracting parameters and adding them to PathArguments
						segmentProcessed, parameterValue, parameterName, err := parseParameterInfo(remainingSegment)
						if err != nil {
							return []ResolvedPathInfo{}, err
						}
						workList[0].ResolvedSegments = append(workList[0].ResolvedSegments, EscapeDotsInPathSegment(segmentProcessed))
						if parameterName != "" {
							workList[0].PathArguments = append(workList[0].PathArguments, api.FunctionArgument{ParameterName: parameterName, Value: parameterValue})
						}
					}
					// Set CurrentSegmentIndex to len(segments) so path will be resolved on next iteration
					workList[0].CurrentSegmentIndex = len(segments)
				} else if currentNode == nil {
					// If currentNode is nil and not all remaining segments are resolved, dequeue
					workList = workList[1:]
				}
			}
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

// TODO: Fix and use this or remove it. If used, it needs to be local to a path registry / resource provider.
// It's also not threadsafe, though registerPathWildcards should only be called at initialization time.

// var resourceTypeToPathPrefixToIsWildcarded = make(ResourceTypeToPathPrefixSetType)

// func prefixIsWildcarded(resourceType api.ResourceType, prefix string) bool {
// 	_, present := resourceTypeToPathPrefixToIsWildcarded[resourceType]
// 	if !present {
// 		return false
// 	}
// 	_, present = resourceTypeToPathPrefixToIsWildcarded[resourceType][prefix]
// 	return present
// }

func normalizePath(resourceType api.ResourceType, path api.UnresolvedPath, preserveBinding bool) api.UnresolvedPath {
	segments := gaby.DotPathToSlice(string(path))
	prefix := ""
	for i, segment := range segments {
		// Associative lookups and array indices are treated as wildcards, but maps can be also
		if strings.ContainsAny(segment, "?*@%") || IsNumber(segment) /*|| prefixIsWildcarded(resourceType, prefix)*/ {
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

// func registerPathWildcards(resourceType api.ResourceType, path api.UnresolvedPath) {
// 	segments := gaby.DotPathToSlice(string(path))
// 	prefix := ""
// 	for i, segment := range segments {
// 		// Record all wildcards
// 		if strings.ContainsAny(segment, "*") {
// 			segments[i] = "*"
// 			_, present := resourceTypeToPathPrefixToIsWildcarded[resourceType]
// 			if !present {
// 				resourceTypeToPathPrefixToIsWildcarded[resourceType] = make(map[string]struct{})
// 			}
// 			resourceTypeToPathPrefixToIsWildcarded[resourceType][prefix] = struct{}{}
// 		}
// 		if prefix != "" {
// 			prefix += "."
// 		}
// 		prefix += segments[i]
// 	}
// }
