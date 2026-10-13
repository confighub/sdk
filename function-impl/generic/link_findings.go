// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// registerVetNoLinkFindings registers the validating function that turns what a Unit's Links
// could not do into a validation result.
//
// A Link that names a path its Units do not have -- a binding to a resource renamed upstream, a
// DownstreamPath into an element that is gone -- or whose resolve fails, writes nothing, and the
// Unit keeps the value it had. The resolve records that on the Unit as a LinkFinding. Whether a
// Unit holding a value its Link can no longer supply should be stopped from being applied is a
// policy question, so it belongs in a Trigger a Space opts into, as vet-no-merge-conflicts does
// for merges.
//
// The findings arrive in the FunctionContext: always for a Trigger, and for a direct invocation
// that asks (FunctionInvocationOptions.IncludeLinkFindings).
func registerVetNoLinkFindings(fh handler.FunctionRegistry, _ configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("vet-no-link-findings", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-no-link-findings",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "reasons",
					Required:      false,
					Description: "Comma-separated finding reasons to fail on: NeededNotFound, ProvidedNotFound, " +
						"ResolveFailed. Empty fails on any outstanding finding.",
					DataType: api.DataTypeString,
					Example:  "ProvidedNotFound,ResolveFailed",
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if the unit's links have no outstanding findings of the specified reasons",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns true if the resolves of the unit's links found everything the links name",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnVetNoLinkFindings(fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnVetNoLinkFindings(functionContext *api.FunctionContext, parsedData gaby.Container,
	args []api.FunctionArgument) (gaby.Container, any, error) {
	var reasons map[api.LinkFindingReason]struct{}
	if len(args) > 0 {
		if arg, ok := args[0].Value.(string); ok && strings.TrimSpace(arg) != "" {
			reasons = map[api.LinkFindingReason]struct{}{}
			for _, reason := range strings.Split(arg, ",") {
				if reason = strings.TrimSpace(reason); reason != "" {
					reasons[api.LinkFindingReason(reason)] = struct{}{}
				}
			}
		}
	}

	result := api.ValidationResult{Passed: true}
	for _, finding := range functionContext.LinkFindings {
		if reasons != nil {
			if _, selected := reasons[finding.Reason]; !selected {
				continue
			}
		}
		result.Passed = false
		result.Issues = append(result.Issues, api.Issue{
			Identifier: string(finding.Reason),
			Message:    describeLinkFinding(finding),
		})
	}
	return parsedData, result, nil
}

// describeLinkFinding says what a Link's resolve could not do, in one line.
func describeLinkFinding(finding api.LinkFinding) string {
	var where []string
	if finding.Entry != "" {
		where = append(where, finding.Entry)
	}
	if finding.Resource.ResourceName != "" {
		where = append(where, fmt.Sprintf("%s %s", finding.Resource.ResourceType, finding.Resource.ResourceName))
	}
	if finding.Path != "" {
		where = append(where, string(finding.Path))
	}
	message := fmt.Sprintf("link %s: %s", finding.LinkID, finding.Reason)
	if len(where) > 0 {
		message += " at " + strings.Join(where, " ")
	}
	if finding.Details != "" {
		message += ": " + finding.Details
	}
	return message
}
