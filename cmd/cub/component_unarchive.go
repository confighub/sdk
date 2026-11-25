// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var componentUnarchiveCmd = &cobra.Command{
	Use:   "unarchive [<slug or id>]",
	Short: "Unarchive a component or multiple components",
	Args:  cobra.MaximumNArgs(1),
	Long: getCommandHelp(`Unarchive components archived with cub component delete --archive.

An archived component is named by a slug made from its ID, id followed by the UUID, or by its UUID.
Its slug is restored unless another component has taken it since, in which case it keeps the one
made from its ID, and can be renamed. Its backing unit is unarchived with it. --recursive then
unarchives every archived space that names it as its variant, recursively, as cub space unarchive
--recursive does: that includes a variant archived on its own before the component was, which cub
component delete --recursive --archive left as it was. A variant is not unarchived while its
component is archived.

Examples:
`+"```"+`
  # Unarchive a component and its variants
  cub component unarchive --recursive id5f0c1d2e-0a4b-4c3d-9e8f-7a6b5c4d3e2f

  # Unarchive the components archived whose slug was my-app
  cub component unarchive --where "Labels.ArchivedSlug = 'my-app'"
`+"```"+`
`, ""),
	RunE:        componentUnarchiveCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var componentUnarchiveRecursive bool

func init() {
	addStandardDisplayFlags(componentUnarchiveCmd)
	componentUnarchiveCmd.Flags().StringVar(&where, "where", "", "where filter to select the archived components to unarchive")
	componentUnarchiveCmd.Flags().BoolVar(&componentUnarchiveRecursive, "recursive", false,
		"also unarchive every archived space that names the component as its variant, recursively, including any archived before it")
	componentCmd.AddCommand(componentUnarchiveCmd)
}

func componentUnarchiveCmdRun(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		if where == "" {
			return errors.New("name a component, or select archived components with --where")
		}
		if componentUnarchiveRecursive {
			return errors.New("--recursive unarchives the variants of one component; name it")
		}
		res, err := cubClientNew.BulkUnarchiveComponentsWithResponse(ctx, &goclientnew.BulkUnarchiveComponentsParams{Where: &where})
		if cubapi.IsAPIError(err, res) {
			return cubapi.InterpretErrorGeneric(err, res)
		}
		responses, err := bulkUnarchiveResponses(res.StatusCode(), res.JSON200, res.JSON207)
		if err != nil {
			return err
		}
		return displayBulkUnarchiveResults("component", responses)
	}
	if where != "" {
		return errors.New("--where can only be specified with no positional arguments")
	}
	componentDetails, err := resolveComponent(args[0], "ComponentID,Slug")
	if err != nil {
		return err
	}
	componentID := componentDetails.Component.ComponentID
	res, err := cubClientNew.UnarchiveComponentWithResponse(ctx, componentID)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	if !quiet && !isAlternativeOutput() {
		tprint("Successfully unarchived component %s (%s)%s", args[0], componentID, keptSlugNote(*res.JSON200))
	}
	renderPayload(res.JSON200)
	if !componentUnarchiveRecursive {
		return nil
	}
	variants := "ComponentID = '" + componentID.String() + "'"
	recursive := true
	spacesRes, err := cubClientNew.BulkUnarchiveSpacesWithResponse(ctx, &goclientnew.BulkUnarchiveSpacesParams{Where: &variants, Recursive: &recursive})
	if cubapi.IsAPIError(err, spacesRes) {
		return cubapi.InterpretErrorGeneric(err, spacesRes)
	}
	responses, err := bulkUnarchiveResponses(spacesRes.StatusCode(), spacesRes.JSON200, spacesRes.JSON207)
	if err != nil {
		return err
	}
	return displayBulkUnarchiveResults("variant", responses)
}
