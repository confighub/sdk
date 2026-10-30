// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package profiles is the profile library manager tools share: named, reusable
// edits stored as ConfigHub Invocations, applied to one Unit or to a selector of
// them.
//
// A profile bundles a function with preset arguments -- a resource tier, a
// hardening pass, a spread rule, an autoscaling target -- and exposes whatever
// has to vary as a parameter the caller supplies at apply time. What differs
// between tools is only which profiles are seeded and what the help text calls
// them; the Space, the Invocations, the parameter binding, and the three
// commands are the same each time.
package profiles

import (
	"context"
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	api "github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// DescriptionAnnotation is where a profile's human description is kept: a stored
// Invocation has no description field.
//
// One key for every tool, not one per tool. The library Space is shared -- that
// is the point of one place to look -- so a per-tool key would mean each tool
// listing every profile it did not install with a blank description, and nothing
// about a description is tool-specific enough to earn that.
const DescriptionAnnotation = "Description"

// Param declares one profile parameter the caller supplies at apply time.
type Param struct {
	// Name is the parameter's own name. It has to be an identifier
	// (^[A-Za-z_][A-Za-z0-9_]*$), which is why a profile parameter binding a
	// function's kebab-case argument is named differently from it.
	Name string
	// DataType defaults to "string".
	DataType string
}

// Spec is one profile: a function, its fixed arguments, and the parameters left
// for the caller.
type Spec struct {
	Slug        string
	Description string
	Function    string
	Args        []api.FunctionArgument
	Params      []Param
}

// Arg is a function argument with a fixed value.
func Arg(fnParam string, value any) api.FunctionArgument {
	return api.FunctionArgument{ParameterName: fnParam, Value: value}
}

// TmplArg binds a function parameter to a profile parameter through a
// Go-template ref, so the value is substituted when the profile is applied.
func TmplArg(fnParam, profileParam string) api.FunctionArgument {
	return api.FunctionArgument{
		ParameterName: fnParam,
		Value:         "{{ .Params." + profileParam + " }}",
		Evaluator:     api.EvaluatorTemplate,
	}
}

// YQParamArg binds set-yq's `param` varArg to a profile parameter, so the yq
// expression can read $params.<name>. set-yq takes its parameters this way
// rather than as named arguments, so TmplArg does not apply to it.
func YQParamArg(name string) api.FunctionArgument {
	return api.FunctionArgument{
		ParameterName: "param",
		Value:         name + "={{ .Params." + name + " }}",
		Evaluator:     api.EvaluatorTemplate,
	}
}

// Library is one tool's profile library.
type Library struct {
	// Tool is the tool's module name, e.g. "autoscale-manager". It becomes the
	// profiles Space's `app` label.
	Tool string
	// Noun is what this tool calls a profile in help text: "autoscaling
	// profile", "placement profile", or just "profile".
	Noun string
	// Target names what `apply` writes to, with its article: "an HPA Unit",
	// "a workload".
	Target string
	// ParamExample is a worked --param for the apply help, e.g.
	// "--param min=3 --param max=10 for hpa-range". Optional.
	ParamExample string

	// Profiles are what `profile install` seeds.
	Profiles []Spec

	// Preflight verifies the tool's ConfigHub session and returns the client.
	Preflight func(context.Context) (*cubapi.Client, error)
	// InvocationName is how the tool is spelled in help text, e.g.
	// "cub-autoscale" or "cub autoscale" when run as a plugin.
	InvocationName func() string

	// FleetEdit configures the bulk command. Leave WhereData empty to omit the
	// command entirely.
	FleetEdit FleetEdit
}

// FleetEdit is the bulk half: one profile applied to every Unit a selector
// matches, in one server-side operation.
type FleetEdit struct {
	// WhereData scopes the edit to the resource kinds a profile can act on, so a
	// profile never reaches a Unit it has nothing to say about. Required.
	WhereData string
	// Scope names those kinds in the help text, e.g.
	// "HorizontalPodAutoscaler / ScaledObject Units".
	Scope string
	// Example is one worked invocation for the help text, without the command
	// name. Optional.
	Example string
}

// buildInvocation turns a Spec into a stored Invocation, declaring its
// parameters and carrying its description.
func (l Library) buildInvocation(spaceID goclientnew.UUID, spec Spec) goclientnew.Invocation {
	params := make([]goclientnew.FunctionParameter, 0, len(spec.Params))
	for _, p := range spec.Params {
		dt := p.DataType
		if dt == "" {
			dt = "string"
		}
		params = append(params, goclientnew.FunctionParameter{
			ParameterName: p.Name,
			DataType:      dt,
			Required:      true,
		})
	}
	return goclientnew.Invocation{
		SpaceID:       spaceID,
		Slug:          spec.Slug,
		DisplayName:   spec.Slug,
		ToolchainType: cubapi.DefaultToolchainType,
		FunctionInvocations: cubapi.FunctionInvocations(api.FunctionInvocation{
			FunctionName: spec.Function,
			Arguments:    spec.Args,
		}),
		Parameters:  params,
		Annotations: map[string]string{DescriptionAnnotation: spec.Description},
	}
}

// noun is the Library's Noun, defaulted.
func (l Library) noun() string {
	if l.Noun != "" {
		return l.Noun
	}
	return "profile"
}

// installHint is the remediation for a library that is not there yet.
func (l Library) installHint(space string) string {
	return fmt.Sprintf("resolve %s (run `%s profile install`)", space, l.InvocationName())
}

// ParseParams turns --param name=value flags into the map a stored Invocation is
// applied with.
func ParseParams(params []string) (map[string]any, error) {
	out := map[string]any{}
	for _, p := range params {
		k, v, found := strings.Cut(p, "=")
		if !found || k == "" {
			return nil, fmt.Errorf("bad --param %q, want name=value", p)
		}
		out[k] = v
	}
	return out, nil
}
