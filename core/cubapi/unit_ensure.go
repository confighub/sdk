// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/cockroachdb/errors"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// EnsureUnitData makes the Unit unit.Slug in unit.SpaceID hold data. When there is no such
// Unit, it creates one from unit, holding data. When there is one and replace is set, it merges
// unit's Labels and Annotations into it and replaces its configuration with data, recording
// description, unless that would change nothing. When replace is not set, an existing Unit is
// left as it is, so one created from defaults keeps the edits made to it since.
func EnsureUnitData(ctx context.Context, c *Client, unit goclientnew.Unit, data, description string, replace bool) (*goclientnew.Unit, error) {
	existing, err := ResolveUnit(ctx, c, NewRef("", unit.Slug), ResolveOpts{Space: unit.SpaceID, Select: "UnitID,SpaceID,Slug"})
	switch {
	case err != nil && !IsNotFoundError(err):
		return nil, err
	case err != nil:
		res, err := c.API.CreateUnitWithResponse(ctx, unit.SpaceID, &goclientnew.CreateUnitParams{}, unit)
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		if res.JSON200 == nil || res.JSON200.Unit == nil {
			return nil, errors.Errorf("create unit %s returned no unit", unit.Slug)
		}
		return uploadUnitData(ctx, c, res.JSON200.Unit.SpaceID, res.JSON200.Unit.UnitID, data, description, false)
	case !replace:
		return existing.Unit, nil
	}
	spaceID, unitID := existing.Unit.SpaceID, existing.Unit.UnitID
	// A merge patch's null deletes, so only what is set is sent.
	fields := map[string]any{}
	if len(unit.Labels) > 0 {
		fields["Labels"] = unit.Labels
	}
	if len(unit.Annotations) > 0 {
		fields["Annotations"] = unit.Annotations
	}
	if len(fields) > 0 {
		patch, err := json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		res, err := c.API.PatchUnitWithBodyWithResponse(ctx, spaceID, unitID, &goclientnew.PatchUnitParams{},
			mergePatchContentType, bytes.NewReader(patch))
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
	}
	// The stored configuration is not byte for byte what was uploaded, so whether data changes
	// it is the server's to say.
	dryRun, err := uploadUnitDataResponse(ctx, c, spaceID, unitID, data, description, true)
	if err != nil {
		return nil, err
	}
	if dryRun.Diff == nil || len(dryRun.Diff.Resources) == 0 {
		return existing.Unit, nil
	}
	return uploadUnitData(ctx, c, spaceID, unitID, data, description, false)
}

func uploadUnitData(ctx context.Context, c *Client, spaceID, unitID goclientnew.UUID, data, description string, dryRun bool) (*goclientnew.Unit, error) {
	res, err := uploadUnitDataResponse(ctx, c, spaceID, unitID, data, description, dryRun)
	if err != nil {
		return nil, err
	}
	return res.Unit, nil
}

func uploadUnitDataResponse(ctx context.Context, c *Client, spaceID, unitID goclientnew.UUID, data, description string, dryRun bool) (*goclientnew.UnitCreateOrUpdateResponse, error) {
	params := &goclientnew.UploadUnitDataParams{}
	if dryRun {
		include := "Diff"
		params.DryRun, params.Include = &dryRun, &include
	} else if description != "" {
		params.LastChangeDescription = &description
	}
	res, err := c.API.UploadUnitDataWithBodyWithResponse(ctx, spaceID, unitID, params,
		"application/octet-stream", strings.NewReader(data))
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil || res.JSON200.Unit == nil {
		return nil, errors.Errorf("upload unit data returned no unit")
	}
	return res.JSON200, nil
}
