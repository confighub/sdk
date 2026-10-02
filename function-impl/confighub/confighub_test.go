// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package confighub

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
)

func newTestHandler(t *testing.T) *handler.FunctionHandler {
	t.Helper()
	rp := cubkit.NewConfigHubResourceProvider()
	fh := handler.NewFunctionHandler(rp)
	RegisterFunctions(rp, fh)
	return fh
}

func invoke(t *testing.T, fh *handler.FunctionHandler, data string, invocation api.FunctionInvocation) *api.FunctionInvocationResponse {
	t.Helper()
	resp, err := fh.InvokeCore(context.Background(), &api.FunctionInvocationRequest{
		ConfigData:          data,
		FunctionInvocations: []api.FunctionInvocation{invocation},
	})
	require.NoError(t, err)
	require.True(t, resp.Success, "errors: %v", resp.ErrorMessages)
	return resp
}

func attributeValues(t *testing.T, resp *api.FunctionInvocationResponse) api.AttributeValueList {
	t.Helper()
	var values api.AttributeValueList
	require.NoError(t, json.Unmarshal(resp.Outputs[api.OutputTypeAttributeValueList], &values))
	return values
}

const triggerAndSpace = `EntityType: Trigger
Slug: standard-vets
Event: Mutation
ToolchainType: Kubernetes/YAML
FunctionName: vet-schemas
UnitFilter: all-k8s-units
BridgeWorker: default/server-worker
Labels:
  Purpose: validation
---
EntityType: Space
Slug: platform-dev
TriggerFilter: platform/triggers
Labels:
  Environment: dev
`

// get-references finds the names an entity document holds, with the type each one names, from
// the references the generated specs declare.
func TestGetReferences(t *testing.T) {
	fh := newTestHandler(t)
	resp := invoke(t, fh, triggerAndSpace, api.FunctionInvocation{FunctionName: "get-references"})

	references := map[string]string{}
	for _, value := range attributeValues(t, resp) {
		require.NotNil(t, value.Details)
		references[string(value.ResourceType)+" "+string(value.Path)+" = "+value.Value.(string)] =
			value.Details.NeededRequired[api.PropertyKeyResourceType]
	}
	assert.Equal(t, map[string]string{
		"Trigger UnitFilter = all-k8s-units":           "Filter",
		"Trigger BridgeWorker = default/server-worker": "BridgeWorker",
		"Space TriggerFilter = platform/triggers":      "Filter",
	}, references)
}

// A list of references is a list of names, each one a reference.
func TestGetReferencesInAList(t *testing.T) {
	fh := newTestHandler(t)
	component := `EntityType: Component
Slug: checkout
AllowedChangeWorkflows:
- platform/release
- platform/hotfix
`
	resp := invoke(t, fh, component, api.FunctionInvocation{FunctionName: "get-references"})
	var paths []string
	for _, value := range attributeValues(t, resp) {
		assert.Equal(t, "ChangeWorkflow", value.Details.NeededRequired[api.PropertyKeyResourceType])
		paths = append(paths, string(value.Path)+" = "+value.Value.(string))
	}
	assert.ElementsMatch(t, []string{
		"AllowedChangeWorkflows.0 = platform/release",
		"AllowedChangeWorkflows.1 = platform/hotfix",
	}, paths)
}

// An entity's slug is what a reference to it names, so the slug of every type a reference can
// name is a provided name of that type.
func TestGetProvided(t *testing.T) {
	fh := newTestHandler(t)
	filter := `EntityType: Filter
Slug: all-k8s-units
From: Unit
Where: "ToolchainType = 'Kubernetes/YAML'"
`
	// Unlike a needed path, which is reported only while it holds a placeholder, a provided path
	// is reported whatever its value.
	resp := invoke(t, fh, filter, api.FunctionInvocation{FunctionName: "get-provided"})
	values := attributeValues(t, resp)
	require.Len(t, values, 1)
	assert.Equal(t, api.ResolvedPath("Slug"), values[0].Path)
	assert.Equal(t, "Filter", values[0].Details.ProvidedProperties[api.PropertyKeyResourceType])
}

func TestLabelsAndAnnotations(t *testing.T) {
	fh := newTestHandler(t)
	resp := invoke(t, fh, triggerAndSpace, api.FunctionInvocation{
		FunctionName: "set-label",
		Arguments: []api.FunctionArgument{
			{ParameterName: "label-key", Value: "Owner"},
			{ParameterName: "label-value", Value: "platform"},
		},
	})
	data := resp.ResultData(triggerAndSpace)
	assert.Contains(t, data, "Purpose: validation\n  Owner: platform\n")
	assert.Contains(t, data, "Environment: dev\n  Owner: platform\n")

	resp = invoke(t, fh, data, api.FunctionInvocation{
		FunctionName: "set-annotation",
		Arguments: []api.FunctionArgument{
			{ParameterName: "annotation-key", Value: "example.com/reviewed-by"},
			{ParameterName: "annotation-value", Value: "alice"},
		},
	})
	data = resp.ResultData(data)

	resp = invoke(t, fh, data, api.FunctionInvocation{
		FunctionName: "get-annotation",
		Arguments:    []api.FunctionArgument{{ParameterName: "annotation-key", Value: "example.com/reviewed-by"}},
	})
	values := attributeValues(t, resp)
	require.Len(t, values, 2)
	for _, value := range values {
		assert.Equal(t, "alice", value.Value)
	}
}

// vet-schemas checks each document against its entity type's built-in schema: the fields it may
// set, their types, and the form of the names it holds.
func TestVetSchemas(t *testing.T) {
	fh := newTestHandler(t)
	result := func(data string) api.ValidationResult {
		t.Helper()
		resp := invoke(t, fh, data, api.FunctionInvocation{FunctionName: "vet-schemas"})
		var results api.ValidationResultList
		require.NoError(t, json.Unmarshal(resp.Outputs[api.OutputTypeValidationResult], &results))
		require.Len(t, results, 1)
		return results[0]
	}

	assert.True(t, result(triggerAndSpace).Passed)

	failed := func(data string, path string) {
		t.Helper()
		verdict := result(data)
		require.False(t, verdict.Passed, "%s", data)
		var paths []string
		for _, attribute := range verdict.FailedAttributes {
			paths = append(paths, string(attribute.Path))
		}
		assert.Contains(t, paths, path, "details: %v", verdict.Details)
	}
	// A field the type does not have, and a reference given as its ID field.
	failed("EntityType: Filter\nSlug: f\nFrom: Unit\nColour: blue\n", ".")
	failed("EntityType: Trigger\nSlug: t\nEvent: Mutation\nToolchainType: Kubernetes/YAML\nUnitFilterID: 9d5c8e64-0c4b-4a4e-9d9e-2d5d2b2f3a1c\n", ".")
	// A value of the wrong type.
	failed("EntityType: Trigger\nSlug: t\nEvent: Mutation\nToolchainType: Kubernetes/YAML\nDisabled: maybe\n", "Disabled")
	// A name that is neither <slug> nor <space>/<slug>.
	failed("EntityType: Trigger\nSlug: t\nEvent: Mutation\nToolchainType: Kubernetes/YAML\nUnitFilter: a/b/c\n", "UnitFilter")
	// A Component belongs to no Space, so its name has no Space in it.
	failed("EntityType: Space\nSlug: s\nComponent: platform/checkout\n", "Component")
	// A required field.
	failed("EntityType: Filter\nSlug: f\n", "From")
	// A type that has no schema fails rather than being skipped.
	failed("EntityType: Gadget\nSlug: g\n", ".")
	failed("Slug: g\n", ".")
}

func vetWhere(t *testing.T, fh *handler.FunctionHandler, data string) api.ValidationResult {
	t.Helper()
	resp := invoke(t, fh, data, api.FunctionInvocation{FunctionName: "vet-where-expressions"})
	var results api.ValidationResultList
	require.NoError(t, json.Unmarshal(resp.Outputs[api.OutputTypeValidationResult], &results))
	require.Len(t, results, 1)
	return results[0]
}

// vet-where-expressions checks each where expression against the attributes of the type it
// selects, allowing the prefixes the server allows for that field.
func TestVetWhereExpressions(t *testing.T) {
	fh := newTestHandler(t)
	valid := `EntityType: Space
Slug: app
WhereTrigger: "Event = 'Mutation' AND Space.Slug = 'platform'"
WhereAttribute: "DataType = 'string'"
---
EntityType: Filter
Slug: derived
From: Unit
Where: "UpstreamUnit.Slug = 'base' AND Labels.tier = 'web'"
---
EntityType: ChangeWorkflow
Slug: release
Stages:
- Name: dev
  WhereSpace: "Labels.Environment = 'dev'"
---
EntityType: Trigger
Slug: t
Event: Mutation
ToolchainType: Kubernetes/YAML
FunctionName: vet-schemas
WhereUnit: "Slug LIKE 'app-%' AND UpstreamUnit.Slug = 'base'"
`
	result := vetWhere(t, fh, valid)
	assert.True(t, result.Passed, "%v", result.FailedAttributes)
	assert.Empty(t, result.FailedAttributes)

	failedAt := func(data string) []string {
		t.Helper()
		result := vetWhere(t, fh, data)
		assert.False(t, result.Passed)
		var paths []string
		for _, attribute := range result.FailedAttributes {
			paths = append(paths, string(attribute.Path))
		}
		return paths
	}
	// A Space's WhereTrigger is over Triggers, not Spaces.
	assert.Equal(t, []string{"WhereTrigger"}, failedAt("EntityType: Space\nSlug: s\nWhereTrigger: \"IsUnitSpace = true\"\n"))
	// A Filter's Where is over what its From names.
	assert.Equal(t, []string{"Where"}, failedAt("EntityType: Filter\nSlug: f\nFrom: Space\nWhere: \"UpstreamUnit.Slug = 'base'\"\n"))
	assert.Equal(t, []string{"Stages.?Name=dev.WhereSpace"}, failedAt("EntityType: ChangeWorkflow\nSlug: r\nStages:\n- Name: dev\n  WhereSpace: \"Slug ? 'x'\"\n"))
	// A ChangeWorkflow stage's WhereSpace is evaluated without expanding the Space's references.
	assert.Equal(t, []string{"Stages.?Name=dev.WhereSpace"}, failedAt("EntityType: ChangeWorkflow\nSlug: r\nStages:\n- Name: dev\n  WhereSpace: \"Organization.Slug = 'acme'\"\n"))
}

// A literal UUID compared with an ID is a finding, scored Low, that does not fail the check: it is
// sometimes what an expression has to say.
func TestVetWhereExpressionsLiteralUUID(t *testing.T) {
	fh := newTestHandler(t)
	result := vetWhere(t, fh, "EntityType: Space\nSlug: s\nWhereTrigger: \"SpaceID = '9d5c8e64-0c4b-4a4e-9d9e-2d5d2b2f3a1c'\"\n")
	assert.True(t, result.Passed)
	assert.Equal(t, api.ScoreLow, result.MaxScore)
	require.Len(t, result.FailedAttributes, 1)
	assert.Equal(t, api.ResolvedPath("WhereTrigger"), result.FailedAttributes[0].Path)
	assert.Equal(t, "literal-uuid", result.FailedAttributes[0].Issues[0].Identifier)

	// A label that happens to hold a UUID is not an ID.
	result = vetWhere(t, fh, "EntityType: Space\nSlug: s\nWhereTrigger: \"Labels.owner = '9d5c8e64-0c4b-4a4e-9d9e-2d5d2b2f3a1c'\"\n")
	assert.True(t, result.Passed)
	assert.Empty(t, result.FailedAttributes)
}

// get-where-expressions returns each expression with the type it selects, read from the sibling
// that names it for a Filter.
func TestGetWhereExpressions(t *testing.T) {
	fh := newTestHandler(t)
	data := `EntityType: Space
Slug: app
WhereTrigger: "Event = 'Mutation'"
---
EntityType: Filter
Slug: derived
From: Unit
Where: "Labels.tier = 'web'"
---
EntityType: ChangeWorkflow
Slug: release
Stages:
- Name: dev
  WhereSpace: "Labels.Environment = 'dev'"
`
	resp := invoke(t, fh, data, api.FunctionInvocation{FunctionName: "get-where-expressions"})
	selects := map[string]string{}
	for _, value := range attributeValues(t, resp) {
		require.NotNil(t, value.Details)
		selects[string(value.ResourceType)+" "+string(value.Path)+" = "+value.Value.(string)] =
			value.Details.NeededRequired[api.PropertyKeyResourceType]
	}
	assert.Equal(t, map[string]string{
		"Space WhereTrigger = Event = 'Mutation'":                                 "Trigger",
		"Filter Where = Labels.tier = 'web'":                                      "Unit",
		"ChangeWorkflow Stages.?Name=dev.WhereSpace = Labels.Environment = 'dev'": "Space",
	}, selects)
}
