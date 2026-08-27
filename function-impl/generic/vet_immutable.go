// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"fmt"

	"log/slog"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerVetImmutable(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("vet-immutable", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-immutable",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "attribute-name",
					Required:      false,
					Description:   "Name of the attribute whose registered paths to check for immutability. Defaults to 'immutable'.",
					DataType:      api.DataTypeString,
				},
			},
			OtherDataExpected: []api.OtherDataSource{"LastReleasedRevisionNum"},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if no immutable fields were changed, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Validates that immutable fields have not been changed compared to the baseline revision data provided via OtherData. If no OtherData is present (e.g., the unit has never been published), validation passes.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			attributeName := api.AttributeName("immutable")
			if len(fArgs.Arguments) > 0 {
				if name, ok := fArgs.Arguments[0].Value.(string); ok && name != "" {
					attributeName = api.AttributeName(name)
				}
			}
			notLive := false
			if fArgs.FunctionContext != nil {
				notLive = fArgs.FunctionContext.NotLive
			}
			result, err := GenericVetImmutable(resourceProvider, fArgs.ParsedData, fArgs.ParsedOtherData, attributeName, notLive, fArgs.Options)
			return fArgs.ParsedData, result, err
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// baselineFromOtherData picks the revision to compare against out of the OtherData
// the caller supplied. The caller names the reference point itself —
// LastReleasedRevisionNum (which publishing a Release advances) or
// Before:HeadRevisionNum. Anything the caller names is honoured; the known keys are
// only a preference order for the ambiguous multi-source case.
func baselineFromOtherData(parsedOtherData map[api.OtherDataSource]gaby.Container) (gaby.Container, bool) {
	if len(parsedOtherData) == 1 {
		for _, data := range parsedOtherData {
			return data, true
		}
	}
	for _, source := range []api.OtherDataSource{"LastReleasedRevisionNum", "HeadRevisionNum"} {
		if data, ok := parsedOtherData[source]; ok {
			return data, true
		}
		if data, ok := parsedOtherData[api.OtherDataSource("Before:"+source)]; ok {
			return data, true
		}
	}
	return nil, false
}

// GenericVetImmutable validates that immutable fields have not been changed between the
// current data and the baseline revision data provided via OtherData. It compares values at
// all paths registered under the specified attribute name.
func GenericVetImmutable(
	resourceProvider yamlkit.ResourceProvider,
	parsedData gaby.Container,
	parsedOtherData map[api.OtherDataSource]gaby.Container,
	attributeName api.AttributeName,
	notLive bool,
	options *api.FunctionOptions,
) (api.ValidationResult, error) {
	// If no OtherData is present, there's nothing to compare against -- pass.
	// TODO: If not live, also pass?
	if len(parsedOtherData) == 0 {
		return api.ValidationResultTrue, nil
	}
	liveData, ok := baselineFromOtherData(parsedOtherData)
	if !ok || liveData == nil {
		return api.ValidationResultTrue, nil
	}

	resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, attributeName)
	if len(resourceTypeToPaths) == 0 {
		// No immutable paths registered for this attribute name
		return api.ValidationResultTrue, nil
	}

	// Get immutable field values from the current data
	currentValues, err := yamlkit.GetPathsAnyType(parsedData, resourceTypeToPaths, nil, resourceProvider, api.DataTypeNone, false, false, options)
	if err != nil {
		return api.ValidationResult{}, fmt.Errorf("failed to get immutable field values from current data: %w", err)
	}

	// Get immutable field values from the live revision data
	liveValues, err := yamlkit.GetPathsAnyType(liveData, resourceTypeToPaths, nil, resourceProvider, api.DataTypeNone, false, false, options)
	if err != nil {
		return api.ValidationResult{}, fmt.Errorf("failed to get immutable field values from live data: %w", err)
	}

	// TODO: Deal with associative lists by looking up merge keys

	// Build a lookup map from the live values: (ResourceType, ResourceName, Path) -> value
	// NOTE: Once live, the resource names should not change
	type resourcePathKey struct {
		ResourceType api.ResourceType
		ResourceName api.ResourceName
		Path         api.ResolvedPath
	}
	liveValueMap := make(map[resourcePathKey]any, len(liveValues))
	liveResources := make(map[api.ResourceName]api.ResourceType)
	for _, lv := range liveValues {
		key := resourcePathKey{
			ResourceType: lv.ResourceType,
			ResourceName: lv.ResourceName,
			Path:         lv.Path,
		}
		liveValueMap[key] = lv.Value
		liveResources[lv.ResourceName] = lv.ResourceType
	}

	// Compare: for each immutable field value in the current data, check if it
	// matches the corresponding value in the live data.
	var failedAttributes api.AttributeValueList
	for _, cv := range currentValues {
		// Only check resources that exist in the live data (skip new resources)
		if _, exists := liveResources[cv.ResourceName]; !exists {
			continue
		}

		key := resourcePathKey{
			ResourceType: cv.ResourceType,
			ResourceName: cv.ResourceName,
			Path:         cv.Path,
		}
		liveValue, liveHasPath := liveValueMap[key]
		if !liveHasPath {
			// Path didn't exist in live data -- this is a new field, not a change
			continue
		}

		// Compare values using string representation to handle all types uniformly
		currentStr := fmt.Sprintf("%v", cv.Value)
		liveStr := fmt.Sprintf("%v", liveValue)
		if currentStr != liveStr {
			cv.Issues = []api.Issue{{
				Identifier: "immutable-field-changed",
				Message:    fmt.Sprintf("immutable field %s was changed", cv.Path),
			}}
			failedAttributes = append(failedAttributes, cv)
		}
	}

	result := api.ValidationResult{
		Passed:           len(failedAttributes) == 0,
		FailedAttributes: failedAttributes,
	}
	return result, nil
}
