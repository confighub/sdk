// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	orderedmap "github.com/wk8/go-ordered-map/v2"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// registerListPaths registers list-paths, which enumerates the paths present in the
// configuration data.
//
// The other getters answer "what is the value at this path" and need the path up front.
// This one answers "what paths are there", which is the question anyone who cannot
// remember the associative-list syntax is actually asking. The paths it reports are the
// ones the setters accept, merge keys and all, so a row can be pasted into set-path
// without translation.
func registerListPaths(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("list-paths", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "list-paths",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "path-prefix",
					Required:         false,
					Description:      "Report only paths at or below this one. Matched segment by segment against both the indexed and the associative spelling of each path, so either form of a list element selects it. See https://docs.confighub.com/guide/functions/#configuration-path-syntax for path syntax.",
					DataType:         api.DataTypeString,
					Example:          "spec.template.spec.containers",
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
				},
				{
					ParameterName:    "depth",
					Required:         false,
					Description:      "Report only paths within this many segments of the resource root. 0, the default, reports every depth. An associative segment counts as one segment, as does a list index.",
					DataType:         api.DataTypeInt,
					Example:          "2",
					ValueConstraints: api.ValueConstraints{Min: &depthMinimum},
				},
				{
					ParameterName: "include-subtrees",
					Required:      false,
					Description:   "Also report the paths of maps and lists, which set-path can target whole. Subtree rows carry a YAML DataType and no Value; only leaf paths have values.",
					DataType:      api.DataTypeBool,
				},
				{
					ParameterName: "attributes-only",
					Required:      false,
					Description:   "Report only paths bound to a registered attribute, which is the set reachable through named setters such as set-image. See https://docs.confighub.com/guide/functions/#getters-and-setters-attributes-and-the-path-registry.",
					DataType:      api.DataTypeBool,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "attribute-list",
				Description: "Paths present in the configuration data",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &api.AttributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns every path present in the configuration data, in the syntax the set-* functions accept, together with its value and the registered attribute it belongs to, if any. Reports what the data contains, not what its schema permits. Restrict the output with path-prefix, depth, and attributes-only; WhereResource restricts which resources are walked.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnListPaths(resourceProvider, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// depthMinimum bounds the depth parameter. A negative depth has no reading: the walk
// treats any non-positive depth as unlimited, so accepting one would silently give the
// caller the opposite of the restriction they asked for.
var depthMinimum = 0

func genericFnListPaths(resourceProvider yamlkit.ResourceProvider, parsedData gaby.Container, args []api.FunctionArgument, options *api.FunctionOptions) (gaby.Container, any, error) {
	var (
		pathPrefix      string
		maxDepth        int
		includeSubtrees bool
		attributesOnly  bool
	)
	// Every parameter is optional, so the argument slice holds only the ones supplied and
	// its indices say nothing. The handler names positional arguments before they get here.
	for _, arg := range args {
		switch arg.ParameterName {
		case "path-prefix":
			pathPrefix, _ = arg.Value.(string)
		case "depth":
			maxDepth, _ = arg.Value.(int)
		case "include-subtrees":
			includeSubtrees, _ = arg.Value.(bool)
		case "attributes-only":
			attributesOnly, _ = arg.Value.(bool)
		}
	}
	// The handler enforces depthMinimum for invocations that arrive through it; this
	// covers callers that reach the implementation directly.
	if maxDepth < 0 {
		return parsedData, nil, errors.Newf("depth must not be negative, got %d", maxDepth)
	}

	paths := api.AttributeValueList{}
	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, resourceProvider, options,
		func(doc *gaby.YamlDoc, output any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
			walker := &pathWalker{
				resourceProvider: resourceProvider,
				resourceDoc:      doc,
				resourceInfo:     resourceInfo,
				pathPrefix:       pathPrefix,
				maxDepth:         maxDepth,
				includeSubtrees:  includeSubtrees,
				attributesOnly:   attributesOnly,
				paths:            &paths,
			}
			walker.walk(doc.DataOrdered(), nil)
			return output, nil
		})
	if err != nil {
		return parsedData, nil, err
	}
	return parsedData, paths, nil
}

// pathWalker accumulates the paths of one resource. Output is in document order, which
// is what makes the result readable next to the YAML it describes; the other getters sort
// because they report one attribute across many units, where document order means nothing.
type pathWalker struct {
	resourceProvider yamlkit.ResourceProvider
	resourceDoc      *gaby.YamlDoc
	resourceInfo     *api.ResourceInfo
	pathPrefix       string
	maxDepth         int
	includeSubtrees  bool
	attributesOnly   bool
	paths            *api.AttributeValueList
}

// walk descends the decoded resource, carrying the escaped segments of the path so far.
// Segments are list indices; emit rewrites them to merge keys, because the rewrite has to
// resolve each index against the document and that is cheaper once per reported path than
// once per node.
func (w *pathWalker) walk(node any, segments []string) {
	// Each child extends a copy, so a sibling's segment cannot land in a slot the
	// parent's slice still reaches.
	child := func(segment string) []string {
		return append(segments[:len(segments):len(segments)], segment)
	}
	switch typed := node.(type) {
	case *orderedmap.OrderedMap[string, any]:
		w.emit(segments, nil, true)
		if w.maxDepth > 0 && len(segments) >= w.maxDepth {
			return
		}
		for pair := typed.Oldest(); pair != nil; pair = pair.Next() {
			// $comment$ keys hold comments lifted into the data by ExtractCommentsToKeys.
			// They are not fields of the resource and reporting them would offer the user
			// a path that writes a comment.
			if gaby.IsCommentKey(pair.Key) {
				continue
			}
			w.walk(pair.Value, child(yamlkit.EscapeDotsInPathSegment(pair.Key)))
		}
	case []any:
		w.emit(segments, nil, true)
		if w.maxDepth > 0 && len(segments) >= w.maxDepth {
			return
		}
		for index, element := range typed {
			w.walk(element, child(strconv.Itoa(index)))
		}
	default:
		w.emit(segments, typed, false)
	}
}

// emit records one path, unless a filter excludes it. The segments address the node by
// list index, which is the form the document-keyed lookups below need; the path reported
// to the caller names list elements by merge key instead.
func (w *pathWalker) emit(segments []string, value any, isSubtree bool) {
	// The resource root is the resource, not a path within it.
	if len(segments) == 0 {
		return
	}
	if isSubtree && !w.includeSubtrees {
		return
	}
	indexedPath := strings.Join(segments, ".")

	reportedPath := indexedPath
	resourceType := w.resourceInfo.ResourceType
	if named, renamed := yamlkit.NameArrayElementsByMergeKey(w.resourceDoc, api.ResolvedPath(indexedPath),
		func(arrayPath string) ([]string, bool) {
			return w.resourceProvider.MergeKeysForPath(resourceType, arrayPath)
		}); renamed {
		reportedPath = string(named)
	}

	// Either spelling satisfies the prefix. A user who copied a path out of an earlier
	// result has the associative form; one who is reading raw YAML has the indexed form,
	// and refusing the second would be refusing the more likely of the two. The separator
	// goes on both sides so that "spec.selector" does not also select "spec.selectorPolicy".
	if w.pathPrefix != "" {
		prefix := w.pathPrefix + "."
		if !strings.HasPrefix(reportedPath+".", prefix) && !strings.HasPrefix(indexedPath+".", prefix) {
			return
		}
	}

	attributeName := api.AttributeNameNone
	if visitorInfo := yamlkit.GetPathVisitorInfo(w.resourceProvider, resourceType, api.UnresolvedPath(indexedPath)); visitorInfo != nil &&
		visitorInfo.AttributeName != "" {
		attributeName = visitorInfo.AttributeName
	}
	if w.attributesOnly && attributeName == api.AttributeNameNone {
		return
	}

	dataType := api.DataTypeYAML
	if !isSubtree {
		dataType = api.DataTypeOfValue(value)
	}

	*w.paths = append(*w.paths, api.AttributeValue{
		AttributeInfo: api.AttributeInfo{
			AttributeIdentifier: api.AttributeIdentifier{
				ResourceInfo: *w.resourceInfo,
				Path:         api.ResolvedPath(reportedPath),
			},
			AttributeMetadata: api.AttributeMetadata{
				AttributeName: attributeName,
				DataType:      dataType,
			},
		},
		Value:   value,
		Comment: w.commentAt(indexedPath),
	})
}

func (w *pathWalker) commentAt(indexedPath string) string {
	head, line, foot := w.resourceDoc.GetCommentKeys(indexedPath)
	parts := make([]string, 0, 3)
	for _, part := range []string{head, line, foot} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "\n")
}
