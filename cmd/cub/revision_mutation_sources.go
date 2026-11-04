// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// The Revision counterpart of `cub unit mutation-sources`: a Revision's MutationSources as
// JSON, for a script, where `cub revision get -o mutations` renders them as a table for a
// person. A Unit has one MutationSources; a Revision is one point in its history, so the
// bulk form here also reads a Unit's history, which the Unit command cannot.
var revisionMutationSourcesCmd = &cobra.Command{
	Use:   "mutation-sources [<unit> [<revision-num>]]",
	Short: "Show what set each value in a revision's config data",
	Long: getCommandHelp(`Display a revision's MutationSources: for each resource and each path
within it, what set the value that is there as of that revision.

MutationSources is not part of the Revision entity, so 'cub revision get -o json' does not
include it. Use -o json / -o yaml / -o jq= to shape the output; with a unit and a revision number,
the payload is the list of resources, so a path is addressed as '.[0].PathMutationMap'.

For one revision, 'cub revision get -o mutations' shows the same information as a table.

With a unit and no revision number, reads every revision of the unit, newest first, in one
request rather than one per revision; --where narrows them. Each row carries the revision it
belongs to, so the payload is addressed as '.[0].MutationSources' rather than '.[0]'.

With --where and no unit, reads the revisions the clause selects across units. By default that
is at most one revision per unit, the newest that matches; --distinct-on Off reads every one.
Scoped to --space when it is given.

The clause can select on MutationSources itself. Its PathMutationMap is keyed by path, and a *
there iterates the paths. A dot in a path is written ~1 when naming one.

Examples:
`+"```"+`
  # What set each value in revision 3 of a unit
  cub revision mutation-sources --space my-space my-deployment 3

  # Every revision of a unit, with the revision each row belongs to
  cub revision mutation-sources --space my-space my-deployment -o jq='.[].RevisionNum'

  # The revisions of a unit in which spec.replicas was protected
  cub revision mutation-sources --space my-space my-deployment \
    --where "MutationSources.*.PathMutationMap.spec~1replicas.Protected = true"

  # For each unit in a space, the newest revision a change set made
  cub revision mutation-sources --space my-space --where "ChangeSetID = '<changeset-id>'"

  # Every revision in the organization with a protected path, not only the newest per unit
  cub revision mutation-sources --distinct-on Off \
    --where "MutationSources.*.PathMutationMap.*.Protected = true"
`+"```"+`
`, ""),
	Args:        cobra.RangeArgs(0, 2),
	RunE:        runRevisionMutationSources,
	Annotations: map[string]string{"OrgLevel": ""},
}

// revisionMutationSourcesDistinctOn is --distinct-on: what a read across units returns at most
// one revision per.
var revisionMutationSourcesDistinctOn string

func init() {
	addStandardGetFlags(revisionMutationSourcesCmd)
	enableWhereFlag(revisionMutationSourcesCmd)
	enableListPagingFlags(revisionMutationSourcesCmd)
	revisionMutationSourcesCmd.Flags().StringVar(&revisionMutationSourcesDistinctOn, "distinct-on", "",
		`without a unit, what to return at most one revision per: Unit, the default, or Off for every revision that matches`)
	enableOptionalSpace(revisionMutationSourcesCmd)
	revisionCmd.AddCommand(revisionMutationSourcesCmd)
}

func runRevisionMutationSources(cmd *cobra.Command, args []string) error {
	if len(args) > 0 && revisionMutationSourcesDistinctOn != "" {
		return errors.New("--distinct-on applies only without a unit; a unit's revisions are each read")
	}
	switch len(args) {
	case 2:
		if where != "" {
			return errors.New("name a revision or pass --where, not both")
		}
		return showRevisionMutationSources(args[0], args[1])
	case 1:
		unit, err := resolveUnit(args[0], selectedSpaceID, "UnitID,SpaceID,Slug")
		if err != nil {
			return err
		}
		whereClause := fmt.Sprintf("UnitID = '%s'", unit.Unit.UnitID)
		if where != "" {
			whereClause = where + " AND " + whereClause
		}
		return displayRevisionMutationSources(searchRevisionMutationSources(whereClause, goclientnew.SearchRevisionMutationSourcesParamsDistinctOnOff))
	default:
		if where == "" {
			return errors.New("name a unit, or pass --where to read revisions across units")
		}
		distinctOn := goclientnew.SearchRevisionMutationSourcesParamsDistinctOn(revisionMutationSourcesDistinctOn)
		return displayRevisionMutationSources(searchRevisionMutationSources(bulkDataWhere(where), distinctOn))
	}
}

func showRevisionMutationSources(unitArg, revisionNumArg string) error {
	unit, err := resolveUnit(unitArg, selectedSpaceID, "UnitID,SpaceID,Slug")
	if err != nil {
		return err
	}
	num, err := strconv.ParseInt(revisionNumArg, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid revision number %q: %w", revisionNumArg, err)
	}
	rev, err := apiGetRevisionFromNumberInSpace(num, unit.Unit.UnitID.String(), unit.Unit.SpaceID.String(), "RevisionID")
	if err != nil {
		return err
	}
	mutationSources, err := fetchRevisionMutationSources(unit.Unit.SpaceID, unit.Unit.UnitID, rev.RevisionID)
	if err != nil {
		return err
	}
	// An empty list is the honest answer for a Revision with no configuration, such as a
	// Unit's first, so it is rendered rather than reported as a failure.
	payload := goclientnew.ResourceMutationList{}
	if mutationSources != nil {
		payload = *mutationSources
	}
	if renderPayload(payload) {
		return nil
	}
	displayJSON(payload)
	return nil
}

func displayRevisionMutationSources(rows []*goclientnew.RevisionMutationSources, err error) error {
	if err != nil {
		return err
	}
	if renderPayload(rows) {
		return nil
	}
	displayJSON(rows)
	return nil
}

// searchRevisionMutationSources reads the MutationSources of the Revisions a where clause
// selects, newest first unless --order-by says otherwise. distinctOn empty is the server's
// default, the newest matching Revision per Unit.
func searchRevisionMutationSources(whereClause string, distinctOn goclientnew.SearchRevisionMutationSourcesParamsDistinctOn) ([]*goclientnew.RevisionMutationSources, error) {
	params := &goclientnew.SearchRevisionMutationSourcesParams{Where: &whereClause}
	if includeHidden != "" {
		params.IncludeHidden = &includeHidden
	}
	opts := listPageOpts("")
	if opts.OrderBy == "" {
		opts.OrderBy = "DESC:CreatedAt"
	}
	params.OrderBy = &opts.OrderBy
	if distinctOn != "" {
		params.DistinctOn = &distinctOn
		// Without the dedupe the server requires a limit, so read every page of matches.
		if distinctOn == goclientnew.SearchRevisionMutationSourcesParamsDistinctOnOff && opts.Limit == 0 {
			opts.Limit = math.MaxInt
		}
	}
	return cubapi.ReadPages(opts, func(limit *int, token *string) (*http.Response, *[]goclientnew.RevisionMutationSources, error) {
		params.Limit, params.Continue = limit, token
		res, err := cubClientNew.SearchRevisionMutationSourcesWithResponse(ctx, params)
		if cubapi.IsAPIError(err, res) {
			return nil, nil, cubapi.InterpretErrorGeneric(err, res)
		}
		return res.HTTPResponse, res.JSON200, nil
	})
}
