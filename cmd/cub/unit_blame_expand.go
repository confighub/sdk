// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"sort"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/workerapi"
	funcimpl "github.com/confighub/sdk/function-impl"
)

// Flattening stops where a subtree arrived at once.
//
// compute-mutations reports a change per *leaf* only where the two sides have
// something to align, and blame diffs against an empty document, so each resource
// comes back whole: one Add carrying the resource as YAML. Expanding that is this
// file's job.
//
// The subtree is already in hand, so this is a walk over it rather than another
// round trip, and it produces the paths the rest of ConfigHub uses: merge-keyed
// array elements as "?name=server", everything else by index. The merge keys come
// from the resource provider for the Unit's own toolchain -- Kubernetes knows that
// containers are keyed by name, and a TOML file has no merge keys at all, so its
// arrays are addressed positionally, which is correct for it.

// blameProviders caches one resource provider per toolchain. Building one registers
// that toolchain's path registry, which is worth doing once rather than per field.
var blameProviders = map[string]yamlkit.ResourceProvider{}

// blameResourceProvider returns the resource provider for a Unit's toolchain.
func blameResourceProvider(toolchainType string) (yamlkit.ResourceProvider, error) {
	if provider, ok := blameProviders[toolchainType]; ok {
		return provider, nil
	}
	// The executor is the registry of which provider serves which toolchain; asking
	// it keeps that list in one place rather than repeating it here, where it would
	// silently fall behind as toolchains are added.
	provider, ok := funcimpl.NewStandardExecutor(nil, false).
		GetResourceProvider(workerapi.ToolchainType(toolchainType))
	if !ok {
		return nil, errors.Newf("no resource provider for toolchain %q", toolchainType)
	}
	blameProviders[toolchainType] = provider
	return provider, nil
}

// expandBlameValue turns one field's value into the leaves under it. A scalar is
// itself, one entry; a mapping or sequence is one entry per leaf beneath it, keyed
// by the path a function or link would use to address it.
func expandBlameValue(provider yamlkit.ResourceProvider, resourceType, basePath, value string) []blameLeaf {
	parsed, err := gaby.ParseYAML([]byte(value))
	if err != nil || parsed == nil {
		return []blameLeaf{{Path: basePath, Value: strings.TrimRight(value, "\n")}}
	}
	leaves := make([]blameLeaf, 0, 8)
	appendBlameLeaves(&leaves, provider, api.ResourceType(resourceType), basePath, parsed)
	if len(leaves) == 0 {
		return []blameLeaf{{Path: basePath, Value: strings.TrimRight(value, "\n")}}
	}
	return leaves
}

// blameLeaf is one addressable field and the value at it.
type blameLeaf struct {
	Path  string
	Value string
}

func appendBlameLeaves(leaves *[]blameLeaf, provider yamlkit.ResourceProvider, resourceType api.ResourceType, path string, doc *gaby.YamlDoc) {
	if doc == nil || doc.YNode() == nil {
		return
	}
	// Dispatch on the decoded shape rather than the YAML node kind: gaby already
	// answers that, and reaching for the node's Kind would make the CLI depend on
	// the YAML library directly to name three constants.
	switch doc.Data().(type) {
	case map[string]interface{}:
		children := doc.ChildrenMap()
		// An empty mapping is a value in its own right ("{}"), not an absence:
		// reporting nothing would lose the field.
		if len(children) == 0 {
			*leaves = append(*leaves, blameLeaf{Path: path, Value: "{}"})
			return
		}
		for _, key := range sortedKeys(children) {
			appendBlameLeaves(leaves, provider, resourceType,
				joinBlamePath(path, yamlkit.EscapeDotsInPathSegment(key)), children[key])
		}
	case []interface{}:
		elements := doc.Children()
		if len(elements) == 0 {
			*leaves = append(*leaves, blameLeaf{Path: path, Value: "[]"})
			return
		}
		mergeKeys, hasMergeKeys := provider.MergeKeysForPath(resourceType, path)
		for i, element := range elements {
			*leaves = appendSequenceLeaves(*leaves, provider, resourceType, path, element, i, mergeKeys, hasMergeKeys)
		}
	default:
		// A scalar reads from the node rather than from Data() so it keeps the text
		// as written -- "8080" stays a string of digits, not a formatted int.
		*leaves = append(*leaves, blameLeaf{
			Path:  path,
			Value: strings.TrimRight(doc.YNode().Value, "\n"),
		})
	}
}

// appendSequenceLeaves addresses one array element the way the rest of ConfigHub
// does: by its merge key where the array has one and the element carries it, and by
// index otherwise.
func appendSequenceLeaves(leaves []blameLeaf, provider yamlkit.ResourceProvider, resourceType api.ResourceType, path string,
	element *gaby.YamlDoc, index int, mergeKeys []string, hasMergeKeys bool,
) []blameLeaf {
	segment := strconv.Itoa(index)
	if hasMergeKeys {
		if values, ok := yamlkit.MergeKeyValues(element, mergeKeys); ok {
			segment = yamlkit.AssociativePathSegment(mergeKeys, values, index)
		}
	}
	appendBlameLeaves(&leaves, provider, resourceType, joinBlamePath(path, segment), element)
	return leaves
}

func joinBlamePath(base, segment string) string {
	if base == "" {
		return segment
	}
	return base + "." + segment
}

func sortedKeys(m map[string]*gaby.YamlDoc) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
