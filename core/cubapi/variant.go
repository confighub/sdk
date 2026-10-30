// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	api "github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// AnnotationUpstreamSpaceID is the annotation a variant Space carries naming the Space it was
// cloned from, which promotion reads to find where to promote from. Space has an
// UpstreamSpaceID field too, but a Space clone does not fill it yet.
const AnnotationUpstreamSpaceID = "UpstreamSpaceID"

// LabelNamespace is the Space label recording the namespace a Kubernetes variant lands in,
// which promotion reads to place the units it clones later.
const LabelNamespace = "Namespace"

// VariantSpec describes a variant to create from an upstream Space, as `cub variant create`
// does: the Space cloned with its labels, annotations and permissions, and every unit cloned
// into it with an UpgradeUnit link back to its upstream. A clone has only the gates its patch
// names; the upstream's are not copied.
type VariantSpec struct {
	// Upstream is the Space to clone.
	Upstream goclientnew.UUID
	// Name is the variant's name, recorded as the Space's Variant label.
	Name string
	// VariantLabels are further key=value labels for the new Space, applied after Variant and
	// Namespace so they win.
	VariantLabels []string
	// SpacePattern names the new Space, as a template over the upstream's fields, such as
	// "template:hub-preview-pr-123". Empty means <component>-<variant> when the upstream
	// belongs to a Component, and the server's default otherwise.
	SpacePattern string
	// SpacePatch is a merge patch layered onto the cloned Space, and UnitPatch one layered onto
	// every cloned unit. Nil for none.
	SpacePatch json.RawMessage
	UnitPatch  json.RawMessage
	// Target is every cloned unit's Target, and the Space's release target unless
	// ReleaseTarget names another.
	Target        *goclientnew.UUID
	ReleaseTarget *goclientnew.UUID
	// Namespace, when given, runs set-namespace over the clones, so the variant lands in a
	// namespace of its own, and records it as the Space's Namespace label. When the upstream
	// records its own namespace, only what is in that namespace moves.
	Namespace string
	// ChangeSetID puts the cloned units in a ChangeSet.
	ChangeSetID *goclientnew.UUID
	// ChangeDescription is recorded on every unit the variant writes.
	ChangeDescription string
	// Syncback also links each clone back to its upstream unit with a MergeUnits link.
	Syncback bool
	// IncludeOutgoingLinksWhere selects which of the upstream units' links to units in other
	// Spaces are copied onto the clones.
	IncludeOutgoingLinksWhere string
	// IncludeHidden also clones hidden units: these HiddenReasons, comma-separated, or "*".
	IncludeHidden string
	// AllowExists accepts a Space or unit that is already there instead of failing.
	AllowExists bool
	// DryRun sends every request as a dry run.
	DryRun bool
}

// VariantResult is what CreateVariant made. Space is set as soon as the Space exists, so a
// caller can say what was left behind when a later step fails.
type VariantResult struct {
	Space *goclientnew.Space
	// Units are the per-unit responses of the unit clone, and UnitsStatus its HTTP status:
	// 200 when every unit was cloned, 207 when some failed.
	Units       []goclientnew.UnitCreateOrUpdateResponse
	UnitsStatus int
}

// UnitErrors returns the errors of the units that failed to clone.
func (r *VariantResult) UnitErrors() []string {
	var errs []string
	for _, u := range r.Units {
		if u.Error != nil {
			errs = append(errs, u.Error.Message)
		}
	}
	return errs
}

// CreateVariant clones spec.Upstream into a new variant Space. A unit that fails to clone is
// reported in the result rather than as an error, as the bulk clone reports it.
func CreateVariant(ctx context.Context, c *Client, spec VariantSpec) (*VariantResult, error) {
	if spec.Name == "" {
		return nil, fmt.Errorf("cubapi: CreateVariant needs a variant name")
	}
	upstreamRes, err := c.API.GetSpaceWithResponse(ctx, spec.Upstream, &goclientnew.GetSpaceParams{})
	if IsAPIError(err, upstreamRes) {
		return nil, InterpretErrorGeneric(err, upstreamRes)
	}
	upstream := upstreamRes.JSON200.Space

	result := &VariantResult{}
	result.Space, err = cloneVariantSpace(ctx, c, spec, upstream)
	if err != nil {
		return nil, err
	}
	space := result.Space

	releaseTarget := spec.ReleaseTarget
	if releaseTarget == nil {
		releaseTarget = spec.Target
	}
	if releaseTarget != nil {
		if err := patchSpace(ctx, c, space.SpaceID, map[string]any{"ReleaseTargetID": releaseTarget.String()}, spec.DryRun); err != nil {
			return result, fmt.Errorf("setting the release target of %s: %w", space.Slug, err)
		}
	}

	result.Units, result.UnitsStatus, err = cloneVariantUnits(ctx, c, spec, upstream.SpaceID, space.SpaceID)
	if err != nil {
		return result, err
	}

	if spec.Namespace != "" {
		if err := setVariantNamespace(ctx, c, spec, space, upstream.Labels[LabelNamespace]); err != nil {
			return result, err
		}
	}
	return result, nil
}

func cloneVariantSpace(ctx context.Context, c *Client, spec VariantSpec, upstream *goclientnew.Space) (*goclientnew.Space, error) {
	variantLabels := []string{"Variant=" + spec.Name}
	if spec.Namespace != "" {
		variantLabels = append(variantLabels, LabelNamespace+"="+spec.Namespace)
	}
	variantLabels = append(variantLabels, spec.VariantLabels...)
	labels := strings.Join(variantLabels, ",")
	where := NewWhere("").SpaceID(upstream.SpaceID).String()
	include := "OrganizationID"
	params := &goclientnew.BulkCreateSpacesParams{Where: &where, Include: &include, VariantLabels: &labels, DryRun: dryRun(spec.DryRun)}
	pattern := spec.SpacePattern
	if pattern == "" && upstream.ComponentID != nil {
		pattern = "template:{{.Component.Slug}}-{{.Labels.Variant}}"
	}
	if pattern != "" {
		params.NamePattern = &pattern
	}
	if spec.AllowExists {
		params.AllowExists = allowExists()
	}
	if spec.IncludeHidden != "" {
		params.IncludeHidden = &spec.IncludeHidden
	}
	// The upstream's ID goes on the clone so promotion can find where to promote from.
	patch, err := layerPatch(spec.SpacePatch, nil, map[string]string{AnnotationUpstreamSpaceID: upstream.SpaceID.String()})
	if err != nil {
		return nil, fmt.Errorf("space patch: %w", err)
	}
	res, err := c.API.BulkCreateSpacesWithBodyWithResponse(ctx, params, mergePatchContentType, bytes.NewReader(patch))
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	responses := res.JSON200
	if responses == nil {
		responses = res.JSON207
	}
	if responses != nil {
		for i := range *responses {
			r := &(*responses)[i]
			if r.Error != nil {
				return nil, fmt.Errorf("failed to clone space: %s", r.Error.Message)
			}
			if r.Space != nil {
				return r.Space, nil
			}
		}
	}
	return nil, fmt.Errorf("space clone returned no space")
}

func cloneVariantUnits(ctx context.Context, c *Client, spec VariantSpec, upstreamID, spaceID goclientnew.UUID) ([]goclientnew.UnitCreateOrUpdateResponse, int, error) {
	where := NewWhere("").SpaceID(upstreamID).String()
	whereSpace := NewWhere("").SpaceID(spaceID).String()
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	params := &goclientnew.BulkCreateUnitsParams{Where: &where, WhereSpace: &whereSpace, Include: &include, DryRun: dryRun(spec.DryRun)}
	if spec.AllowExists {
		params.AllowExists = allowExists()
	}
	if spec.IncludeHidden != "" {
		params.IncludeHidden = &spec.IncludeHidden
	}
	if spec.Syncback {
		params.Syncback = &spec.Syncback
	}
	if spec.IncludeOutgoingLinksWhere != "" {
		params.IncludeOutgoingLinksWhere = &spec.IncludeOutgoingLinksWhere
	}
	set := map[string]any{}
	if spec.Target != nil {
		set["TargetID"] = spec.Target.String()
	}
	if spec.ChangeDescription != "" {
		set["LastChangeDescription"] = spec.ChangeDescription
	}
	if spec.ChangeSetID != nil {
		set["ChangeSetID"] = spec.ChangeSetID.String()
	}
	patch, err := layerPatch(spec.UnitPatch, set, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("unit patch: %w", err)
	}
	res, err := c.API.BulkCreateUnitsWithBodyWithResponse(ctx, params, mergePatchContentType, bytes.NewReader(patch))
	if IsAPIError(err, res) {
		return nil, 0, InterpretErrorGeneric(err, res)
	}
	if res.JSON200 != nil {
		return *res.JSON200, 200, nil
	}
	if res.JSON207 != nil {
		return *res.JSON207, 207, nil
	}
	return nil, 0, fmt.Errorf("unexpected response from bulk create API")
}

// setVariantNamespace runs set-namespace over the variant's units. The clones joined the
// ChangeSet, if any, and a change to a unit in an open ChangeSet has to name it.
//
// The namespace is the variant's own value, so the paths it writes are protected: a merge from
// the upstream does not write the upstream's value back over them.
func setVariantNamespace(ctx context.Context, c *Client, spec VariantSpec, space *goclientnew.Space, upstreamNamespace string) error {
	args := []api.FunctionArgument{{Value: spec.Namespace}}
	if upstreamNamespace != "" {
		args = append(args, api.FunctionArgument{ParameterName: "old-namespace", Value: upstreamNamespace})
	}
	where := NewWhere("").SpaceID(space.SpaceID).String()
	protect := true
	params := &goclientnew.InvokeFunctionsOnOrgParams{Where: &where, ChangeSetId: spec.ChangeSetID, Protect: &protect}
	if spec.DryRun {
		value := "true"
		params.DryRun = &value
	}
	req := goclientnew.FunctionInvocationsRequest{
		ToolchainType:       DefaultToolchainType,
		ChangeDescription:   spec.ChangeDescription,
		FunctionInvocations: FunctionInvocations(api.FunctionInvocation{FunctionName: "set-namespace", Arguments: args}),
	}
	res, err := c.API.InvokeFunctionsOnOrgWithResponse(ctx, params, req)
	if IsAPIError(err, res) {
		return fmt.Errorf("set-namespace on %s: %w", space.Slug, InterpretErrorGeneric(err, res))
	}
	responses := res.JSON200
	if responses == nil {
		responses = res.JSON207
	}
	if responses != nil {
		for _, r := range *responses {
			if !r.Success {
				return fmt.Errorf("set-namespace on %s/%s: %s", space.Slug, r.UnitSlug, errString(r.Error))
			}
		}
	}
	return nil
}

func patchSpace(ctx context.Context, c *Client, spaceID goclientnew.UUID, patch map[string]any, dryRunRequest bool) error {
	body, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	res, err := c.API.PatchSpaceWithBodyWithResponse(ctx, spaceID, &goclientnew.PatchSpaceParams{DryRun: dryRun(dryRunRequest)}, mergePatchContentType, bytes.NewReader(body))
	if IsAPIError(err, res) {
		return InterpretErrorGeneric(err, res)
	}
	return nil
}

func dryRun(on bool) *bool {
	if !on {
		return nil
	}
	return &on
}

// layerPatch adds set's fields and annotations' entries to a merge patch. A patch that ends up
// empty stays "null", the clone APIs' no-op.
func layerPatch(base json.RawMessage, set map[string]any, annotations map[string]string) ([]byte, error) {
	trimmed := bytes.TrimSpace(base)
	empty := len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
	if empty && len(set) == 0 && len(annotations) == 0 {
		return []byte("null"), nil
	}
	patch := map[string]any{}
	if !empty {
		if err := json.Unmarshal(trimmed, &patch); err != nil {
			return nil, err
		}
	}
	for k, v := range set {
		patch[k] = v
	}
	if len(annotations) > 0 {
		merged, _ := patch["Annotations"].(map[string]any)
		if merged == nil {
			merged = map[string]any{}
		}
		for k, v := range annotations {
			merged[k] = v
		}
		patch["Annotations"] = merged
	}
	return json.Marshal(patch)
}
