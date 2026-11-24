// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var unitUnarchiveCmd = &cobra.Command{
	Use:   "unarchive [<slug or id>]",
	Short: "Unarchive a unit or multiple units",
	Long: getCommandHelp(`Unarchive units archived with cub unit delete --archive.

An archived unit is named by a slug made from its ID, id followed by the UUID, or by its UUID. Its
slug is restored unless another unit in its space has taken it since, in which case it keeps the
one made from its ID, and can be renamed. Its links, and the links to it, are unarchived with it,
except a link whose other unit is still archived, which comes back when that unit does.

Examples:
`+"```"+`
  # Unarchive one unit
  cub unit unarchive id5f0c1d2e-0a4b-4c3d-9e8f-7a6b5c4d3e2f

  # Unarchive the units archived from a space whose slug was my-unit
  cub unit unarchive --space my-space --where "Labels.ArchivedSlug = 'my-unit'"
`+"```"+`
`, ""),
	Args:        cobra.MaximumNArgs(1),
	RunE:        unitUnarchiveCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func init() {
	addStandardDisplayFlags(unitUnarchiveCmd)
	unitUnarchiveCmd.Flags().StringVar(&where, "where", "", "where filter to select the archived units to unarchive")
	enableFilterFlag(unitUnarchiveCmd)
	unitCmd.AddCommand(unitUnarchiveCmd)
}

func unitUnarchiveCmdRun(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return runBulkUnitUnarchive()
	}
	if filter != "" || where != "" {
		return fmt.Errorf("--filter and --where can only be specified with no positional arguments")
	}
	unitDetails, err := resolveUnit(args[0], selectedSpaceID, "UnitID,SpaceID,Slug")
	if err != nil {
		return err
	}
	res, err := cubClientNew.UnarchiveUnitWithResponse(ctx, unitDetails.Unit.SpaceID, unitDetails.Unit.UnitID)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	if !quiet && !isAlternativeOutput() {
		tprint("Successfully unarchived unit %s (%s)%s", args[0], unitDetails.Unit.UnitID, keptSlugNote(*res.JSON200))
	}
	renderPayload(res.JSON200)
	return nil
}

func runBulkUnitUnarchive() error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}
	effectiveWhere := addSpaceIDToWhereClause(where, selectedSpaceID)
	params := &goclientnew.BulkUnarchiveUnitsParams{Where: &effectiveWhere}
	if filterID != "" {
		params.Filter = &filterID
	}
	res, err := cubClientNew.BulkUnarchiveUnitsWithResponse(ctx, params)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	responses := res.JSON200
	if res.StatusCode() == 207 {
		responses = res.JSON207
	}
	if responses == nil {
		return fmt.Errorf("unexpected status code %d or no response data", res.StatusCode())
	}
	renderPayload(responses)
	failed := 0
	for _, response := range *responses {
		if response.Error != nil {
			failed++
			if !quiet && !isAlternativeOutput() {
				displayBulkFailure("unarchive", "unit", response.Error)
			}
			continue
		}
		if !quiet && !isAlternativeOutput() && (verbose || response.KeptSlug != "") {
			fmt.Printf("Successfully unarchived unit: %s%s\n", response.Message, keptSlugNote(response))
		}
	}
	if !quiet && !isAlternativeOutput() {
		fmt.Printf("\nBulk unarchive operation completed:\n  Success: %d unit(s)\n", len(*responses)-failed)
		if failed > 0 {
			fmt.Printf("  Failed: %d unit(s)\n", failed)
		}
	}
	if failed > 0 {
		return fmt.Errorf("bulk unarchive partially failed: %d succeeded, %d failed", len(*responses)-failed, failed)
	}
	return nil
}

// keptSlugNote says that an unarchived entity kept the slug the archive gave it, because another
// entity has taken the one it had.
func keptSlugNote(response goclientnew.UnarchiveResponse) string {
	if response.KeptSlug == "" {
		return ""
	}
	return fmt.Sprintf("; slug %s is in use, so it keeps the slug made from its ID", response.KeptSlug)
}
