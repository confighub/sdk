// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"fmt"
	"strconv"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerVetMergeKeys(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("vet-merge-keys", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-merge-keys",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if no duplicate merge keys found, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns true if no arrays contain duplicate strategic merge patch keys. Duplicate merge keys in Kubernetes resources (such as duplicate container names or environment variable names) cause apply errors.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return GenericFnVetMergeKeys(resourceProvider, fArgs.ParsedData)
		},
	})
}

// GenericFnVetMergeKeys checks for duplicate merge keys across all resources.
func GenericFnVetMergeKeys(resourceProvider yamlkit.ResourceProvider, parsedData gaby.Container) (gaby.Container, any, error) {
	var failedAttributes api.AttributeValueList

	visitor := func(doc *gaby.YamlDoc, _ any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
		findDuplicateMergeKeys(doc, "", resourceInfo, resourceProvider, &failedAttributes)
		return nil, nil
	}
	yamlkit.VisitResources(parsedData, nil, resourceProvider, visitor)

	result := api.ValidationResult{
		Passed:           len(failedAttributes) == 0,
		FailedAttributes: failedAttributes,
	}
	return parsedData, result, nil
}

// findDuplicateMergeKeys recursively walks the YAML tree looking for arrays
// with merge keys, and reports any duplicates.
func findDuplicateMergeKeys(doc *gaby.YamlDoc, path string, resourceInfo *api.ResourceInfo, resourceProvider yamlkit.ResourceProvider, failedAttributes *api.AttributeValueList) {
	if children := doc.ChildrenMap(); len(children) > 0 {
		for key, child := range children {
			escapedKey := yamlkit.EscapeDotsInPathSegment(key)
			var childPath string
			if path != "" {
				childPath = path + "." + escapedKey
			} else {
				childPath = escapedKey
			}
			findDuplicateMergeKeys(child, childPath, resourceInfo, resourceProvider, failedAttributes)
		}
		return
	}

	arrayChildren := doc.Children()
	if arrayChildren == nil {
		return
	}

	mergeKey, hasMergeKey := resourceProvider.MergeKeyForPath(resourceInfo.ResourceType, path)
	if hasMergeKey {
		seen := make(map[string]bool)
		for i, child := range arrayChildren {
			childMap := child.ChildrenMap()
			if childMap == nil {
				continue
			}
			keyChild, ok := childMap[mergeKey]
			if !ok {
				continue
			}
			keyValue := fmt.Sprintf("%v", keyChild.Data())
			if seen[keyValue] {
				duplicatePath := path + "." + strconv.Itoa(i) + "." + yamlkit.EscapeDotsInPathSegment(mergeKey)
				*failedAttributes = append(*failedAttributes, api.AttributeValue{
					AttributeInfo: api.AttributeInfo{
						AttributeIdentifier: api.AttributeIdentifier{
							ResourceInfo: *resourceInfo,
							Path:         api.ResolvedPath(duplicatePath),
						},
						AttributeMetadata: api.AttributeMetadata{
							AttributeName: api.AttributeNameNone,
						},
					},
					Value: keyValue,
				})
			}
			seen[keyValue] = true
		}
	}

	// Recurse into array elements
	for i, child := range arrayChildren {
		childPath := path + "." + strconv.Itoa(i)
		findDuplicateMergeKeys(child, childPath, resourceInfo, resourceProvider, failedAttributes)
	}
}
