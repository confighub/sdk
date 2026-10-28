// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var changeorderRefreshStageCmd = &cobra.Command{
	Use:   "refresh-stage [<slug or id>]",
	Short: "Work out the stage of a change order, or of multiple change orders, again",
	Long: getCommandHelp(`Work out the stage of a change order again, from the releases, live status,
attestations and validation results it depends on.

The stage a change order has reached in its ChangeWorkflow is stored, and is worked out again when
the change order is promoted or updated, when a release is published for it or its live status is
reported, and when an attestation is recorded in a space it is headed for. Refresh it when what it
depends on changed some other way: a unit validated again, or a space taking the change by
"cub unit update --upgrade" or a link resolving rather than by "cub variant promote".

Refreshing requires Use permission on the change order, not the Edit permission an update does.

Examples:
`+"```"+`
  # Refresh one change order
  cub changeorder refresh-stage --space my-base release-42

  # Refresh every change order of a space that has not completed its workflow
  cub changeorder refresh-stage --space my-base --where "Stage != 'Completed'"

  # Refresh specific change orders by slug
  cub changeorder refresh-stage --space my-base --changeorder release-42,release-43
`+"```"+`
`, ""),
	Args:        cobra.MaximumNArgs(1),
	RunE:        changeorderRefreshStageCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func init() {
	addStandardDisplayFlags(changeorderRefreshStageCmd)
	enableWhereFlag(changeorderRefreshStageCmd)
	enableFilterFlag(changeorderRefreshStageCmd)
	enableDryRunFlag(changeorderRefreshStageCmd)
	changeorderRefreshStageCmd.Flags().StringSliceVar(&changeorderIdentifiers, "changeorder", []string{},
		"target specific changeorders by slug or UUID (can be repeated or comma-separated)")
	changeorderCmd.AddCommand(changeorderRefreshStageCmd)
}

func changeorderRefreshStageCmdRun(cmd *cobra.Command, args []string) error {
	bulk := len(args) == 0
	if bulk {
		if len(changeorderIdentifiers) > 0 && where != "" {
			return errors.New("--changeorder and --where flags are mutually exclusive")
		}
		if len(changeorderIdentifiers) == 0 && where == "" && filter == "" {
			return errors.New("name a change order, or select change orders with --where, --filter or --changeorder")
		}
	} else if filter != "" || where != "" || len(changeorderIdentifiers) > 0 {
		return errors.New("--filter, --where, or --changeorder can only be specified with no positional arguments")
	}
	if err := validateSpaceFlag(bulk); err != nil {
		return err
	}
	if bulk {
		return runBulkChangeOrderRefreshStage()
	}

	changeorder, err := resolveChangeOrder(args[0], selectedSpaceID, "ChangeOrderID,SpaceID,Slug")
	if err != nil {
		return err
	}
	res, err := cubClientNew.RefreshChangeOrderStageWithResponse(ctx, changeorder.ChangeOrder.SpaceID,
		changeorder.ChangeOrder.ChangeOrderID, &goclientnew.RefreshChangeOrderStageParams{DryRun: dryRunParam()})
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	refreshed := res.JSON200
	if !quiet && !isAlternativeOutput() {
		stage := refreshed.Stage
		if stage == "" {
			stage = "none: it has not finished the first stage"
		}
		if dryRun {
			tprint("Dry run: changeorder %s would be at stage %s", refreshed.Slug, stage)
		} else {
			tprint("Changeorder %s is at stage %s", refreshed.Slug, stage)
		}
	}
	if verbose {
		displayChangeOrderDetails(refreshed)
	}
	renderPayload(refreshed)
	return nil
}

func runBulkChangeOrderRefreshStage() error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}
	effectiveWhere := where
	if len(changeorderIdentifiers) > 0 {
		if effectiveWhere, err = buildWhereClauseFromChangeOrders(changeorderIdentifiers); err != nil {
			return err
		}
	}
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	params := &goclientnew.BulkRefreshChangeOrderStagesParams{Where: &effectiveWhere, DryRun: dryRunParam()}
	params.IncludeHidden = includeHiddenParam()
	if filterID != "" {
		params.Filter = &filterID
	}
	res, err := cubClientNew.BulkRefreshChangeOrderStagesWithResponse(ctx, params)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	return displayBulkGenericCreateOrUpdateResults(
		res.JSON200, res.JSON207, res.StatusCode(), "changeorder", "refresh", effectiveWhere,
		func(r *goclientnew.ChangeOrderCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
		func(r *goclientnew.ChangeOrderCreateOrUpdateResponse) string {
			if r.ChangeOrder != nil {
				return fmt.Sprintf("%s (stage %s)", r.ChangeOrder.Slug, r.ChangeOrder.Stage)
			}
			return ""
		},
	)
}
