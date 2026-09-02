// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// Configuration data and mutation sources are not fields of a Unit or a Revision: they are
// the two bulk columns, and a list of Units should not carry a copy of every document. They
// are read and written through their own endpoints, and these are the calls that do it. See
// docs/design/large-field-access.md sections 6 and 7.

// rawBodyErr reports failure for an endpoint whose success body is the configuration
// itself. cubapi.IsAPIError cannot be used for these: it treats a nil JSON200 field as a
// failure, and a raw-body response has no JSON200 to be non-nil.
func rawBodyErr(err error, resp interface {
	StatusCode() int
	Status() string
}) error {
	if err != nil {
		return err
	}
	if resp == nil {
		return errors.New("no response from server")
	}
	if resp.StatusCode() != http.StatusOK {
		return errors.Errorf("request failed: %s", resp.Status())
	}
	return nil
}

// fetchUnitData returns a Unit's configuration.
func fetchUnitData(spaceID, unitID uuid.UUID) (string, error) {
	res, err := cubClientNew.DownloadUnitDataWithResponse(ctx, spaceID, unitID)
	if err := rawBodyErr(err, res); err != nil {
		return "", err
	}
	return string(res.Body), nil
}

// putUnitData writes a Unit's configuration, and returns the operation's result so a dry run
// can report what it produced.
//
// params carries everything that describes *how* the configuration should land. A create or an
// update that was given a file is two calls -- the metadata, then the configuration -- and the
// metadata call changes no data, so protect, the external-merge trio, tag and subgroup have
// nothing to act on there. Sending them only on the first call silently drops them: an external
// merge becomes a plain overwrite that ignores protected paths, and dry_run leaves the
// configuration write to happen for real. See docs/design/large-field-access.md section 7.
func putUnitData(spaceID, unitID uuid.UUID, data string, params *goclientnew.UploadUnitDataParams) (*goclientnew.UnitCreateOrUpdateResponse, error) {
	if params == nil {
		params = &goclientnew.UploadUnitDataParams{}
	}
	res, err := cubClientNew.UploadUnitDataWithBodyWithResponse(ctx, spaceID, unitID, params,
		"application/octet-stream", strings.NewReader(data))
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	return res.JSON200, nil
}

// unitDataParams builds the parameters for a plain configuration write -- one that only records
// what the change was and which ChangeSet it belongs to.
func unitDataParams(changeDescription, changeSetID string) (*goclientnew.UploadUnitDataParams, error) {
	params := &goclientnew.UploadUnitDataParams{}
	if changeDescription != "" {
		params.LastChangeDescription = &changeDescription
	}
	if changeSetID != "" {
		id, err := uuid.Parse(changeSetID)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid change set id %s", changeSetID)
		}
		params.ChangeSetId = &id
	}
	return params, nil
}

// unitDataParamsFromUpdate carries an update's parameters onto the configuration write that
// follows it. Copied field by field from what the metadata call was given so that adding a
// parameter to one cannot quietly leave it off the other.
func unitDataParamsFromUpdate(p *goclientnew.UpdateUnitParams, changeDescription string) *goclientnew.UploadUnitDataParams {
	params := &goclientnew.UploadUnitDataParams{
		DryRun:                 p.DryRun,
		Protect:                p.Protect,
		Clearance:              p.Clearance,
		Guards:                 p.Guards,
		MergeBase:              p.MergeBase,
		MergeExternalSource:    p.MergeExternalSource,
		MergeEnableSubtraction: p.MergeEnableSubtraction,
		Tag:                    p.Tag,
		ChangeSetId:            p.ChangeSetId,
		Subgroup:               p.Subgroup,
		Include:                p.Include,
	}
	if changeDescription != "" {
		params.LastChangeDescription = &changeDescription
	}
	return params
}

// unitDataParamsFromCreate is the create counterpart. A create takes far fewer of these --
// merge_external_source is the one that describes how the configuration lands.
func unitDataParamsFromCreate(p *goclientnew.CreateUnitParams, changeDescription, changeSetID string) (*goclientnew.UploadUnitDataParams, error) {
	params, err := unitDataParams(changeDescription, changeSetID)
	if err != nil {
		return nil, err
	}
	params.MergeExternalSource = p.MergeExternalSource
	params.Include = p.Include
	return params, nil
}

// fetchRevisionData returns a Revision's configuration.
func fetchRevisionData(spaceID, unitID, revisionID uuid.UUID) (string, error) {
	res, err := cubClientNew.DownloadRevisionDataWithResponse(ctx, spaceID, unitID, revisionID)
	if err := rawBodyErr(err, res); err != nil {
		return "", err
	}
	return string(res.Body), nil
}

// fetchUnitMutationSources returns what set each value in a Unit's configuration.
func fetchUnitMutationSources(spaceID, unitID uuid.UUID) (*goclientnew.ResourceMutationList, error) {
	res, err := cubClientNew.GetUnitMutationSourcesWithResponse(ctx, spaceID, unitID)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, nil
	}
	return res.JSON200.MutationSources, nil
}

// fetchRevisionMutationSources is the Revision counterpart of fetchUnitMutationSources.
func fetchRevisionMutationSources(spaceID, unitID, revisionID uuid.UUID) (*goclientnew.ResourceMutationList, error) {
	res, err := cubClientNew.GetRevisionMutationSourcesWithResponse(ctx, spaceID, unitID, revisionID)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, nil
	}
	return res.JSON200.MutationSources, nil
}

// changeSetIDForDataWrite returns the ChangeSet a Unit belongs to as a string, for the data
// endpoint's query parameter, or "" when it belongs to none.
func changeSetIDForDataWrite(unit *goclientnew.Unit) string {
	if unit.ChangeSetID == nil || *unit.ChangeSetID == uuid.Nil {
		return ""
	}
	return unit.ChangeSetID.String()
}

// fetchUnitDataBulk returns the configuration of every Unit the where clause selects, across
// Spaces, keyed by Unit ID, in one request. It is the bulk counterpart of fetchUnitData and
// the reason the bulk endpoint exists: a caller that needs many Units' configuration should
// not make one request per Unit. Scope it to a Space with a where clause.
func fetchUnitDataBulk(whereClause string) (map[string]string, error) {
	params := &goclientnew.SearchUnitDataParams{}
	if whereClause != "" {
		params.Where = &whereClause
	}
	res, err := cubClientNew.SearchUnitDataWithResponse(ctx, params)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return map[string]string{}, nil
	}
	byUnitID := make(map[string]string, len(*res.JSON200))
	for _, row := range *res.JSON200 {
		byUnitID[row.UnitID.String()] = row.Data
	}
	return byUnitID, nil
}

// A write to a Unit -- create, update or patch, single or bulk -- answers with the operation's
// result rather than the entity. The Unit is inside it, along with the parts of the result that
// are not fields of a Unit and are returned only when `include` names them: the configuration
// the operation produced, and what set each value in it. Those are how a dry run reports what
// it computed, since a dry run stores nothing for a later read to find.

// unitFromWrite returns the Unit a write produced.
func unitFromWrite(resp *goclientnew.UnitCreateOrUpdateResponse) (*goclientnew.Unit, error) {
	if resp == nil || resp.Unit == nil {
		return nil, errors.New("the server returned no unit")
	}
	return resp.Unit, nil
}

// includeWriteResult asks a write to return the configuration it produced and what set each
// value in it. Only a dry run has no other source for them -- a real write stores what it made,
// and the endpoints serve that -- but asking either way costs one parameter and means no caller
// has to know which kind of write it is making.
func includeWriteResult() *string {
	s := "ConfigData,MutationSources"
	return &s
}

// bulkDataWhere scopes a caller's where clause to the Space the command is pointed at. The
// bulk endpoints are organization-level -- "the configuration of everything matching this"
// rarely stops at a Space boundary -- so a Space is a where clause like any other rather than
// part of the path. `--space "*"` means the whole organization and adds nothing.
func bulkDataWhere(whereClause string) string {
	if selectedSpaceID == "" || selectedSpaceID == "*" {
		return whereClause
	}
	scoped := fmt.Sprintf("SpaceID = '%s'", selectedSpaceID)
	if whereClause == "" {
		return scoped
	}
	// Concatenated rather than parenthesised: the filter grammar has no grouping, and a
	// leading "(" is rejected as an attribute name. It conjoins with AND and nothing else,
	// so there is no precedence for parentheses to disambiguate.
	return whereClause + " AND " + scoped
}

// searchUnitData returns the configuration of every Unit a where clause selects, as the rows
// the endpoint serves: identity, DataHash, DataSize and the document. This is the shape a
// caller wants when reading many Units -- one request rather than one per Unit -- and the
// reason a single-Unit read cannot serve it is that a stream of documents has no way to say
// which Unit each came from.
func searchUnitData(whereClause string) ([]goclientnew.UnitData, error) {
	params := &goclientnew.SearchUnitDataParams{}
	if w := bulkDataWhere(whereClause); w != "" {
		params.Where = &w
	}
	res, err := cubClientNew.SearchUnitDataWithResponse(ctx, params)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return []goclientnew.UnitData{}, nil
	}
	return *res.JSON200, nil
}

// searchUnitMutationSources is the mutation-sources counterpart of searchUnitData.
func searchUnitMutationSources(whereClause string) ([]goclientnew.UnitMutationSources, error) {
	params := &goclientnew.SearchUnitMutationSourcesParams{}
	if w := bulkDataWhere(whereClause); w != "" {
		params.Where = &w
	}
	res, err := cubClientNew.SearchUnitMutationSourcesWithResponse(ctx, params)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return []goclientnew.UnitMutationSources{}, nil
	}
	return *res.JSON200, nil
}
