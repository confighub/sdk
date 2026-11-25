// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var spaceUnarchiveCmd = &cobra.Command{
	Use:   "unarchive [<slug or id>]",
	Short: "Unarchive a space or multiple spaces",
	Long: getCommandHelp(`Unarchive spaces archived with cub space delete --archive.

An archived space is named by a slug made from its ID, id followed by the UUID, or by its UUID. Its
slug is restored unless another space has taken it since, in which case it keeps the one made from
its ID, and can be renamed. A space archived with --recursive is unarchived only with --recursive,
which unarchives what its archive archived: anything in it archived before or since stays archived.
The change orders the archive aborted stay aborted.

Unarchiving is refused while what it would bring back names an entity that is still archived, such
as a trigger running an invocation in another archived space. Unarchive that one first, or in the
same request.

Examples:
`+"```"+`
  # Unarchive one space and what was archived with it
  cub space unarchive --recursive id5f0c1d2e-0a4b-4c3d-9e8f-7a6b5c4d3e2f

  # Unarchive the archived space whose slug was my-space
  cub space unarchive --recursive --where "Labels.ArchivedSlug = 'my-space'"
`+"```"+`
`, ""),
	Args: cobra.MaximumNArgs(1),
	RunE: spaceUnarchiveCmdRun,
}

var spaceUnarchiveRecursive bool

func init() {
	addStandardDisplayFlags(spaceUnarchiveCmd)
	spaceUnarchiveCmd.Flags().StringVar(&where, "where", "", "where filter to select the archived spaces to unarchive")
	enableFilterFlag(spaceUnarchiveCmd)
	spaceUnarchiveCmd.Flags().BoolVar(&spaceUnarchiveRecursive, "recursive", false,
		"Also unarchive what was archived with each space, which a space archived with --recursive requires")
	spaceCmd.AddCommand(spaceUnarchiveCmd)
}

func spaceUnarchiveCmdRun(cmd *cobra.Command, args []string) error {
	var recursiveParam *bool
	if spaceUnarchiveRecursive {
		recursiveParam = &spaceUnarchiveRecursive
	}
	if len(args) == 0 {
		return runBulkSpaceUnarchive(recursiveParam)
	}
	if filter != "" || where != "" {
		return fmt.Errorf("--filter and --where can only be specified with no positional arguments")
	}
	spaceDetails, err := resolveSpace(args[0], "SpaceID,Slug")
	if err != nil {
		return err
	}
	spaceID := spaceDetails.Space.SpaceID
	res, err := cubClientNew.UnarchiveSpaceWithResponse(ctx, spaceID, &goclientnew.UnarchiveSpaceParams{Recursive: recursiveParam})
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	if !quiet && !isAlternativeOutput() {
		tprint("Successfully unarchived space %s (%s)%s", args[0], spaceID, keptSlugNote(*res.JSON200))
	}
	renderPayload(res.JSON200)
	return nil
}

func runBulkSpaceUnarchive(recursiveParam *bool) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}
	params := &goclientnew.BulkUnarchiveSpacesParams{Where: &where, Recursive: recursiveParam}
	if filterID != "" {
		params.Filter = &filterID
	}
	res, err := cubClientNew.BulkUnarchiveSpacesWithResponse(ctx, params)
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
	return displayBulkUnarchiveResults("space", *responses)
}
