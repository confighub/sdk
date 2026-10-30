// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

import (
	"context"
	"sort"

	"github.com/confighub/sdk/core/cubapi"
	api "github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// StatusRow is one Unit carrying a ValidationWarning or a ValidationError.
type StatusRow struct {
	Space    string `json:"space"`
	Unit     string `json:"unit"`
	Warnings int    `json:"warnings"`
	Errors   int    `json:"errors"`
}

// Status lists the Units guardrail Triggers have marked, warnings and gates
// alike, sorted by space and unit.
//
// It reports what any Trigger attached, not only this pack's: a Unit does not
// record which rule warned it, and a tool that filtered to its own rules would
// under-report a Unit that is blocked for some other reason entirely.
func Status(ctx context.Context, client *cubapi.Client) ([]StatusRow, error) {
	marked, err := cubapi.ListMarkedUnits(ctx, client, cubapi.DefaultToolchainType)
	if err != nil {
		return nil, err
	}
	rows := make([]StatusRow, 0, len(marked))
	for _, eu := range marked {
		space := ""
		if eu.Space != nil {
			space = eu.Space.Slug
		}
		rows = append(rows, StatusRow{
			Space: space, Unit: eu.Unit.Slug,
			Warnings: len(eu.Unit.ValidationWarnings), Errors: len(eu.Unit.ValidationErrors),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Space != rows[j].Space {
			return rows[i].Space < rows[j].Space
		}
		return rows[i].Unit < rows[j].Unit
	})
	return rows, nil
}

// Annotate writes one annotation onto a Unit's resources.
//
// It is the producing half of annotate-then-validate: a manager cannot attach an
// ValidationWarning itself -- only a failed Trigger can -- so a finding that no
// single resource can express is written as an annotation, and a rule in the
// pack warns for as long as the annotation is there.
func Annotate(ctx context.Context, client *cubapi.Client, spaceID, unitSlug, key, value, changeDesc string) error {
	where := cubapi.Where{}.Eq("SpaceID", spaceID).Slug(unitSlug)
	if err := where.Err(); err != nil {
		return err
	}
	_, err := cubapi.InvokeFunction(ctx, client,
		api.FunctionInvocation{
			FunctionName: "set-annotation",
			Arguments: []api.FunctionArgument{
				{ParameterName: "annotation-key", Value: key},
				{ParameterName: "annotation-value", Value: value},
			},
		},
		cubapi.Selector{Where: where.String()},
		cubapi.Change{Description: changeDesc})
	return err
}

// KubernetesSpaces is the set of Space slugs holding at least one
// Kubernetes/YAML Unit. Wiring a Space that holds none would attach Triggers
// that can never run.
func KubernetesSpaces(ctx context.Context, client *cubapi.Client) (map[string]bool, error) {
	return cubapi.SpacesWithToolchain(ctx, client, cubapi.DefaultToolchainType)
}

// listSpaces returns the Space metadata the plan reasons about, optionally
// narrowed by a Space filter.
func listSpaces(ctx context.Context, client *cubapi.Client, whereSpace string) ([]spaceInfo, error) {
	where := cubapi.Where{}
	if whereSpace != "" {
		where = cubapi.NewWhere(whereSpace)
	}
	spaces, err := cubapi.ListSpaces(ctx, client, where,
		cubapi.ListOpts{Select: "Slug,SpaceID,WhereTrigger,TriggerFilterID"})
	if err != nil {
		return nil, err
	}
	infos := make([]spaceInfo, 0, len(spaces))
	for _, es := range spaces {
		if es.Space == nil {
			continue
		}
		triggerFilterID := ""
		if es.Space.TriggerFilterID != nil {
			triggerFilterID = es.Space.TriggerFilterID.String()
		}
		infos = append(infos, spaceInfo{
			SpaceID: es.Space.SpaceID, Slug: es.Space.Slug,
			WhereTrigger: es.Space.WhereTrigger, TriggerFilterID: triggerFilterID,
		})
	}
	return infos, nil
}

// spaceInfo is the Space metadata the plan reasons about.
type spaceInfo struct {
	SpaceID         goclientnew.UUID
	Slug            string
	WhereTrigger    string
	TriggerFilterID string
}

// targetInfo is the Target metadata the plan reasons about.
type targetInfo struct {
	SpaceID         goclientnew.UUID
	TargetID        goclientnew.UUID
	Space           string
	Slug            string
	WhereTrigger    string
	TriggerFilterID string
}

// listTargets returns the Target metadata the plan reasons about, optionally
// narrowed by a Target filter.
func listTargets(ctx context.Context, client *cubapi.Client, whereTarget string) ([]targetInfo, error) {
	targets, err := cubapi.ListTargets(ctx, client, cubapi.NewWhere(whereTarget),
		cubapi.ListOpts{Select: "Slug,SpaceID,SpaceSlug,TargetID,WhereTrigger,TriggerFilterID"})
	if err != nil {
		return nil, err
	}
	infos := make([]targetInfo, 0, len(targets))
	for _, et := range targets {
		if et.Target == nil {
			continue
		}
		triggerFilterID := ""
		if et.Target.TriggerFilterID != nil {
			triggerFilterID = et.Target.TriggerFilterID.String()
		}
		infos = append(infos, targetInfo{
			SpaceID: et.Target.SpaceID, TargetID: et.Target.TargetID,
			Space: et.Target.SpaceSlug, Slug: et.Target.Slug,
			WhereTrigger: et.Target.WhereTrigger, TriggerFilterID: triggerFilterID,
		})
	}
	return infos, nil
}
