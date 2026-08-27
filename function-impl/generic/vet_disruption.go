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

// DefaultDisruptionAttributePrefix is the attribute-name prefix whose per-severity registrations
// vet-disruption consults: disruption-critical, disruption-high, disruption-medium, disruption-low.
const DefaultDisruptionAttributePrefix = "disruption"

// disruptionTiers pairs each severity attribute suffix with the Score it reports, ordered worst
// first so a path registered under two tiers is attributed to the more severe one.
var disruptionTiers = []struct {
	suffix string
	score  api.Score
}{
	{"critical", api.ScoreCritical},
	{"high", api.ScoreHigh},
	{"medium", api.ScoreMedium},
	{"low", api.ScoreLow},
}

// disruptionOtherDataPreference is the order in which baselines are accepted.
//
// LastReleasedRevisionNum is "what the target was last told", and it is what the GitOps release
// path maintains. A validator runs *before* apply, so it is the correct comparison point.
var disruptionOtherDataPreference = []api.OtherDataSource{"LastReleasedRevisionNum"}

func registerVetDisruption(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("vet-disruption", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-disruption",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "score-threshold",
					Required:      true,
					Description:   `Fail when a change's disruption reaches this severity or worse. Possible values: "Critical", "High", "Medium", "Low".`,
					DataType:      api.DataTypeString,
					ValueConstraints: api.ValueConstraints{
						EnumValues: []string{
							string(api.ScoreCritical), string(api.ScoreHigh),
							string(api.ScoreMedium), string(api.ScoreLow),
						},
					},
				},
				{
					ParameterName: "attribute-prefix",
					Required:      false,
					Description:   `Prefix of the per-severity attributes whose registered paths are checked; "` + DefaultDisruptionAttributePrefix + `" by default, meaning disruption-critical, disruption-high, disruption-medium and disruption-low.`,
					DataType:      api.DataTypeString,
				},
			},
			RequiredParameters: 1,
			OtherDataExpected:  disruptionOtherDataPreference,
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if no change reaches the disruption threshold, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:   false,
			Validating: true,
			Hermetic:   true,
			Idempotent: true,
			Description: "Validates how disruptive a pending change is, by comparing registered per-severity " +
				"paths against the last-released revision. Registering a path under disruption-critical / -high / " +
				"-medium / -low grades a change to it; the score-threshold decides which severities fail. " +
				"Unlike vet-immutable this is graded rather than binary, so one rule can block a destructive " +
				"change while merely reporting a benign one. Validation passes when there is no baseline, " +
				"because creating a resource is not replacing one.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			threshold := api.ScoreNone
			prefix := DefaultDisruptionAttributePrefix
			for _, arg := range fArgs.Arguments {
				switch arg.ParameterName {
				case "score-threshold", "":
					if s, ok := arg.Value.(string); ok && threshold == api.ScoreNone {
						parsed, err := api.ValidateScore(s)
						if err != nil {
							return fArgs.ParsedData, nil, fmt.Errorf("vet-disruption: %w", err)
						}
						threshold = parsed
					}
				case "attribute-prefix":
					if s, ok := arg.Value.(string); ok && s != "" {
						prefix = s
					}
				}
			}
			if threshold == api.ScoreNone {
				return fArgs.ParsedData, nil, fmt.Errorf("vet-disruption: score-threshold is required")
			}
			result, err := GenericVetDisruption(
				resourceProvider, fArgs.ParsedData, fArgs.ParsedOtherData, prefix, threshold, fArgs.Options)
			return fArgs.ParsedData, result, err
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// GenericVetDisruption grades the changes between the current data and a baseline revision, by
// severity tier, and fails when the worst change reaches the threshold.
//
// Two situations look superficially alike and are handled differently on purpose:
//
//   - No baseline at all (parsedOtherData empty). The unit has never been applied. This must pass:
//     with nothing applied there is nothing to disrupt — creating a resource is not replacing one —
//     and failing here would deadlock the unit, since it could never be applied to acquire the
//     baseline that would let it pass.
//   - A baseline was supplied, but under a source this function cannot interpret. That is a
//     misconfiguration (a Trigger whose OtherDataSource does not match), and returning "passed"
//     would credit a validator that never actually ran. It is an error.
func GenericVetDisruption(
	resourceProvider yamlkit.ResourceProvider,
	parsedData gaby.Container,
	parsedOtherData map[api.OtherDataSource]gaby.Container,
	attributePrefix string,
	threshold api.Score,
	options *api.FunctionOptions,
) (api.ValidationResult, error) {
	// No baseline: nothing has been applied, so nothing can be disrupted.
	if len(parsedOtherData) == 0 {
		return api.ValidationResultTrue, nil
	}
	baseline, ok := selectBaseline(parsedOtherData)
	if !ok {
		return api.ValidationResult{}, fmt.Errorf(
			"vet-disruption: other data was supplied but none of %v is present (found %v); "+
				"set the Trigger's OtherDataSource to LastReleasedRevisionNum",
			disruptionOtherDataPreference, otherDataKeys(parsedOtherData))
	}

	var (
		failedAttributes api.AttributeValueList
		maxScore         = api.ScoreNone
		// A path registered under two tiers must be reported once, at the worse severity.
		seen = map[resourcePathKey]bool{}
		// anyRegistered guards against a silently-inert validator: if no tier has any
		// registered paths, the function would always pass without checking anything.
		anyRegistered bool
	)

	for _, tier := range disruptionTiers {
		attributeName := api.AttributeName(attributePrefix + "-" + tier.suffix)
		resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, attributeName)
		if len(resourceTypeToPaths) == 0 {
			continue
		}
		anyRegistered = true

		currentValues, err := yamlkit.GetPathsAnyType(parsedData, resourceTypeToPaths, nil, resourceProvider, api.DataTypeNone, false, false, options)
		if err != nil {
			return api.ValidationResult{}, fmt.Errorf("vet-disruption: read %s paths from current data: %w", attributeName, err)
		}
		baselineValues, err := yamlkit.GetPathsAnyType(baseline, resourceTypeToPaths, nil, resourceProvider, api.DataTypeNone, false, false, options)
		if err != nil {
			return api.ValidationResult{}, fmt.Errorf("vet-disruption: read %s paths from baseline: %w", attributeName, err)
		}

		baselineMap := make(map[resourcePathKey]any, len(baselineValues))
		baselineResources := make(map[api.ResourceName]struct{}, len(baselineValues))
		for _, bv := range baselineValues {
			baselineMap[resourcePathKey{bv.ResourceType, bv.ResourceName, bv.Path}] = bv.Value
			baselineResources[bv.ResourceName] = struct{}{}
		}

		for _, cv := range currentValues {
			// A resource absent from the baseline is being created, not changed.
			if _, exists := baselineResources[cv.ResourceName]; !exists {
				continue
			}
			key := resourcePathKey{cv.ResourceType, cv.ResourceName, cv.Path}
			if seen[key] {
				continue
			}
			baseValue, hasPath := baselineMap[key]
			if !hasPath {
				// The path is newly set rather than changed.
				continue
			}
			if fmt.Sprintf("%v", cv.Value) == fmt.Sprintf("%v", baseValue) {
				continue
			}
			seen[key] = true
			cv.Score = tier.score
			cv.Issues = []api.Issue{{
				Identifier: string(attributeName),
				Message: fmt.Sprintf("%s is %s disruption: changing it from %v to %v cannot be reconciled in place",
					cv.Path, tier.score, baseValue, cv.Value),
			}}
			failedAttributes = append(failedAttributes, cv)
			maxScore = api.ScoreMax(maxScore, tier.score)
		}
	}

	if !anyRegistered {
		return api.ValidationResult{}, fmt.Errorf(
			"vet-disruption: no paths registered under %s-critical/-high/-medium/-low; "+
				"register them with an Attribute before using this function", attributePrefix)
	}

	// Fail when the worst change reaches the threshold. ScoreMax(max, threshold) == max means
	// max >= threshold; this is the same rule the scored validators in custom-workers use.
	passed := maxScore == api.ScoreNone || api.ScoreMax(maxScore, threshold) != maxScore
	return api.ValidationResult{
		Passed:           passed,
		MaxScore:         maxScore,
		FailedAttributes: failedAttributes,
	}, nil
}

// resourcePathKey identifies one attribute value across revisions. Resource names do not change
// once a resource is live, so they are stable join keys.
type resourcePathKey struct {
	ResourceType api.ResourceType
	ResourceName api.ResourceName
	Path         api.ResolvedPath
}

// selectBaseline picks the baseline revision by preference order, so a Trigger configured with
// either supported source works and no key is hardcoded.
func selectBaseline(parsedOtherData map[api.OtherDataSource]gaby.Container) (gaby.Container, bool) {
	for _, source := range disruptionOtherDataPreference {
		if data, ok := parsedOtherData[source]; ok && data != nil {
			return data, true
		}
	}
	return nil, false
}

func otherDataKeys(parsedOtherData map[api.OtherDataSource]gaby.Container) []api.OtherDataSource {
	keys := make([]api.OtherDataSource, 0, len(parsedOtherData))
	for k := range parsedOtherData {
		keys = append(keys, k)
	}
	return keys
}
