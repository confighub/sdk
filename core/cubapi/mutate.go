// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Mutations (mutate.go) are a dry-run/commit harness for ConfigHub data changes.
// It encodes one policy consistently:
//
//   - An empty [Change.Description] means dry-run: the server previews the
//     change and writes nothing.
//   - A non-empty Description means commit: the change is written and the
//     description is recorded with it.
//
// It wraps the org-level function-invocation and bulk-patch endpoints and
// returns a structured [Result] (per-unit success, error, mutation diffs, and
// function outputs) instead of leaving callers to parse a CLI's text output.
//
// Function arguments use the canonical [api.FunctionArgument] / [api.FunctionInvocation]
// types from core/function/api (with Evaluator constants api.EvaluatorTemplate /
// api.EvaluatorCEL); [Arguments] converts them to the generated client's form.
package cubapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	api "github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// DefaultToolchainType is used when a Selector leaves ToolchainType empty.
const DefaultToolchainType = "Kubernetes/YAML"

// Change describes whether a mutation is a dry-run preview or a commit. The zero
// value is a dry-run.
type Change struct {
	// Description is recorded with the change on commit. Empty means dry-run.
	Description string
}

// DryRun reports whether this is a preview (no Description).
func (ch Change) DryRun() bool { return ch.Description == "" }

// Selector scopes a mutation to a set of units using the org-level endpoints.
// Where is required for org-wide invocation and bulk patch.
type Selector struct {
	Where         string // unit filter, e.g. "Slug = 'checkout'" or "SpaceID = '...'"
	WhereData     string // filter on config data
	WhereResource string // filter on resources within the config data
	ResourceType  string // e.g. "apps/v1/Deployment"
	ToolchainType string // defaults to DefaultToolchainType
	ExecutorSpace string // optional space whose worker/context executes functions
}

func (s Selector) toolchain() string {
	if s.ToolchainType == "" {
		return DefaultToolchainType
	}
	return s.ToolchainType
}

// Arguments converts canonical function arguments into the generated
// FunctionArgument list, mirroring how the cub CLI builds them (positional vs
// named, optional evaluator, scalar value).
func Arguments(args []api.FunctionArgument) []goclientnew.FunctionArgument {
	out := make([]goclientnew.FunctionArgument, 0, len(args))
	for _, a := range args {
		value := &goclientnew.FunctionArgument_Value{}
		switch v := a.Value.(type) {
		case string:
			_ = value.FromFunctionArgumentValue0(v)
		case int:
			_ = value.FromFunctionArgumentValue1(int64(v))
		case int64:
			_ = value.FromFunctionArgumentValue1(v)
		case bool:
			_ = value.FromFunctionArgumentValue2(v)
		default:
			_ = value.FromFunctionArgumentValue0(fmt.Sprintf("%v", v))
		}
		fa := goclientnew.FunctionArgument{Value: value}
		if a.Evaluator != "" {
			evaluator := a.Evaluator
			fa.Evaluator = &evaluator
		}
		if a.ParameterName != "" {
			name := a.ParameterName
			fa.ParameterName = &name
		}
		out = append(out, fa)
	}
	return out
}

// FunctionInvocations converts canonical function invocations into the generated
// FunctionInvocationList, for the fields that hold one -- notably an Invocation's
// FunctionInvocations, which names the functions it calls in the order they execute.
func FunctionInvocations(invocations ...api.FunctionInvocation) *goclientnew.FunctionInvocationList {
	out := make(goclientnew.FunctionInvocationList, 0, len(invocations))
	for _, invocation := range invocations {
		out = append(out, goclientnew.FunctionInvocation{
			FunctionName:  invocation.FunctionName,
			Arguments:     Arguments(invocation.Arguments),
			WhereResource: invocation.WhereResource,
		})
	}
	return &out
}

// InvocationFunctionNames names the functions an Invocation calls, in the order it executes
// them. An Invocation usually calls one function, in which case this is just its name.
func InvocationFunctionNames(invocation *goclientnew.Invocation) []string {
	if invocation == nil || invocation.FunctionInvocations == nil {
		return nil
	}
	functions := *invocation.FunctionInvocations
	names := make([]string, 0, len(functions))
	for _, function := range functions {
		names = append(names, function.FunctionName)
	}
	return names
}

// UnitOutcome is the per-unit result of a mutation or function invocation.
type UnitOutcome struct {
	UnitID       string
	SpaceID      string
	SpaceSlug    string
	UnitSlug     string
	Success      bool
	Error        string
	HasMutations bool
	// Mutations carries the (old -> new) diffs for rendering, when present.
	Mutations *goclientnew.ResourceMutationList
	// Outputs holds function output types mapped to embedded-JSON output data
	// (e.g. the result of a read-only function like get-resources).
	Outputs map[string]string
}

// Result aggregates the per-unit outcomes of a mutation.
type Result struct {
	DryRun   bool
	Outcomes []UnitOutcome
}

// Failed returns the outcomes that did not succeed.
func (r *Result) Failed() []UnitOutcome {
	var out []UnitOutcome
	for _, o := range r.Outcomes {
		if !o.Success {
			out = append(out, o)
		}
	}
	return out
}

// InvokeFunction runs an ad-hoc function over the units matching sel, as a
// dry-run preview or a commit per ch. It also serves read-only functions (e.g.
// get-resources): inspect [UnitOutcome.Outputs] on the result.
func InvokeFunction(ctx context.Context, c *Client, fn api.FunctionInvocation, sel Selector, ch Change) (*Result, error) {
	req := baseRequest(sel, ch)
	req.FunctionInvocations = &[]goclientnew.FunctionInvocation{{
		FunctionName:  fn.FunctionName,
		Arguments:     Arguments(fn.Arguments),
		WhereResource: fn.WhereResource,
	}}
	return invoke(ctx, c, sel, ch, req)
}

// InvokeStoredInvocation runs a parameterized stored Invocation (by ID) over the
// units matching sel, as a dry-run preview or a commit per ch.
func InvokeStoredInvocation(ctx context.Context, c *Client, invocationID goclientnew.UUID, params map[string]any, sel Selector, ch Change) (*Result, error) {
	req := baseRequest(sel, ch)
	req.ParameterizedInvocations = []goclientnew.ParameterizedInvocationRef{{
		InvocationID: invocationID,
		Parameters:   params,
	}}
	return invoke(ctx, c, sel, ch, req)
}

func baseRequest(sel Selector, ch Change) goclientnew.FunctionInvocationsRequest {
	return goclientnew.FunctionInvocationsRequest{
		ToolchainType:     sel.toolchain(),
		WhereResource:     sel.WhereResource,
		ChangeDescription: ch.Description,
	}
}

func invoke(ctx context.Context, c *Client, sel Selector, ch Change, req goclientnew.FunctionInvocationsRequest) (*Result, error) {
	params := &goclientnew.InvokeFunctionsOnOrgParams{}
	if sel.Where != "" {
		params.Where = &sel.Where
	}
	if sel.WhereData != "" {
		params.WhereData = &sel.WhereData
	}
	if sel.ResourceType != "" {
		params.ResourceType = &sel.ResourceType
	}
	if sel.ExecutorSpace != "" {
		params.ExecutorSpace = &sel.ExecutorSpace
	}
	if ch.DryRun() {
		dryRun := "true"
		params.DryRun = &dryRun
	}

	res, err := c.API.InvokeFunctionsOnOrgWithResponse(ctx, params, req)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	responses := res.JSON200
	if responses == nil {
		responses = res.JSON207 // partial success/failure
	}
	out := &Result{DryRun: ch.DryRun()}
	if responses != nil {
		for i := range *responses {
			fr := &(*responses)[i]
			out.Outcomes = append(out.Outcomes, UnitOutcome{
				UnitID:       fr.UnitID.String(),
				SpaceID:      fr.SpaceID.String(),
				SpaceSlug:    fr.SpaceSlug,
				UnitSlug:     fr.UnitSlug,
				Success:      fr.Success,
				Error:        errString(fr.Error),
				HasMutations: fr.HasNewMutations,
				Mutations:    fr.Mutations,
				Outputs:      fr.Outputs,
			})
		}
	}
	return out, nil
}

// UpgradeUnits performs a bulk "--upgrade" patch over the units matching where:
// each unit is reconciled with its upstream. It is a dry-run preview or a commit
// per ch.
func UpgradeUnits(ctx context.Context, c *Client, where string, ch Change) (*Result, error) {
	params := &goclientnew.BulkPatchUnitsParams{}
	if where != "" {
		params.Where = &where
	}
	upgrade := true
	params.Upgrade = &upgrade
	if ch.DryRun() {
		dryRun := true
		params.DryRun = &dryRun
	}

	// On commit, record the change description via a merge patch; on dry-run the
	// patch is empty.
	patch := map[string]any{}
	if !ch.DryRun() {
		patch["LastChangeDescription"] = ch.Description
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("cubapi: marshal patch: %w", err)
	}

	res, err := c.API.BulkPatchUnitsWithBodyWithResponse(ctx, params, "application/merge-patch+json", bytes.NewReader(body))
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	responses := res.JSON200
	if responses == nil {
		responses = res.JSON207
	}
	out := &Result{DryRun: ch.DryRun()}
	if responses != nil {
		for i := range *responses {
			ur := &(*responses)[i]
			oc := UnitOutcome{Success: ur.Error == nil, Error: errString(ur.Error)}
			if ur.Unit != nil {
				oc.UnitID = ur.Unit.UnitID.String()
				oc.SpaceID = ur.Unit.SpaceID.String()
				oc.UnitSlug = ur.Unit.Slug
				oc.SpaceSlug = ur.Unit.SpaceSlug
			}
			out.Outcomes = append(out.Outcomes, oc)
		}
	}
	return out, nil
}

func errString(e *goclientnew.ResponseError) string {
	if e == nil {
		return ""
	}
	return e.Message
}
