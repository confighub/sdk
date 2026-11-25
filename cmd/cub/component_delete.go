// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var componentDeleteCmd = &cobra.Command{
	Use:   "delete [<name or id>]",
	Short: "Delete a component or multiple components",
	Args:  cobra.MaximumNArgs(1),
	Long: getCommandHelp(`Delete a Component entity, or the Components --where selects.

A Component is not deleted while spaces name it as their variants. --recursive deletes
those first, with their units, in one request, so that a deployment and the base it was
cloned from go together; a unit with a delete gate still stops it unless --force is given,
and a reference from outside the component stops it unless --detach is given.

--archive keeps the component rather than deleting it: it stays, hidden with the reason
Archived and named by a slug made from its ID, with its backing unit archived with it,
until it is unarchived with cub component unarchive. With --recursive, its variants are
archived first, recursively, as cub space delete --recursive --archive archives a space;
without it, a component that live spaces name is not archived.

Examples:
`+"```"+`
  cub component delete my-app

  # Delete a component together with every one of its variants
  cub component delete --recursive my-app

  # Archive a component and its variants, and unarchive them
  cub component delete --recursive --archive my-app
  cub component unarchive --recursive id5f0c1d2e-0a4b-4c3d-9e8f-7a6b5c4d3e2f

  # Delete the components whose backing Units are empty, keeping the Units
  cub component delete --from-backing-units --where "Labels.team = 'platform'"
`+"```"+`
`, ""),
	RunE: componentDeleteCmdRun,
}

var componentDeleteArgs struct {
	recursive bool
	force     bool
}

func init() {
	addStandardDeleteFlags(componentDeleteCmd)
	enableWhereFlag(componentDeleteCmd)
	addPruneFlag(componentDeleteCmd, "Component")
	componentDeleteCmd.Flags().BoolVar(&componentDeleteArgs.recursive, "recursive", false,
		"also delete the component's variants, with everything in them")
	componentDeleteCmd.Flags().BoolVar(&componentDeleteArgs.force, "force", false,
		"with --recursive, delete the variants' units even if some have delete gates")
	addDetachFlag(componentDeleteCmd)
	addArchiveFlag(componentDeleteCmd)
	componentCmd.AddCommand(componentDeleteCmd)
}

// deleteComponentVariants deletes every space naming the component, recursively and in
// one request, so that references between them do not stop the delete. The variants are
// named as they go, since a count alone does not say which were still there.
func deleteComponentVariants(componentSlug string, componentID goclientnew.UUID) error {
	spaces, err := listVariantSpaces(ctx, componentID, "")
	if err != nil {
		return err
	}
	if len(spaces) == 0 {
		if !quiet {
			tprintRaw("Component " + componentSlug + " has no variants")
		}
		return nil
	}
	if !quiet {
		verb := "Deleting"
		if deleteArchive {
			verb = "Archiving"
		}
		for _, name := range variantNames(spaces) {
			tprintRaw(verb + " variant " + componentSlug + "/" + name + "...")
		}
	}
	whereClause := "ComponentID = '" + componentID.String() + "'"
	include := "OrganizationID"
	recursiveParam := "true"
	params := &goclientnew.BulkDeleteSpacesParams{
		Where:     &whereClause,
		Include:   &include,
		Recursive: &recursiveParam,
	}
	if componentDeleteArgs.force {
		params.RecursiveForce = &recursiveParam
	}
	params.Detach = detachParam()
	params.Archive = archiveParam()
	bulkRes, err := cubClientNew.BulkDeleteSpacesWithResponse(ctx, params)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}
	return displayBulkDeleteResults(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "variant", deleteOperationName(), whereClause)
}

func runBulkComponentDelete() error {
	params := &goclientnew.BulkDeleteComponentsParams{Where: &where}
	params.IncludeHidden = includeHiddenParam()
	params.FromBackingUnits = fromBackingUnitsParam()
	params.Archive = archiveParam()
	bulkRes, err := cubClientNew.BulkDeleteComponentsWithResponse(ctx, params)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}
	return displayBulkDeleteResults(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "component", deleteOperationName(), where)
}

func componentDeleteCmdRun(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		if where == "" && !backingUnitArgs.fromBackingUnits {
			return errors.New("name a component, or select components with --where")
		}
		return runBulkComponentDelete()
	}
	if where != "" {
		return errors.New("--where can only be specified with no positional arguments")
	}
	componentDetails, err := resolveComponent(args[0], "ComponentID,Slug")
	if err != nil {
		return err
	}
	componentID := componentDetails.Component.ComponentID
	if componentDeleteArgs.recursive {
		if err := deleteComponentVariants(componentDetails.Component.Slug, componentID); err != nil {
			return err
		}
	}
	deleteRes, err := cubClientNew.DeleteComponentWithResponse(ctx, componentID,
		&goclientnew.DeleteComponentParams{Archive: archiveParam()})
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}
	displayDeleteOrArchiveResults("component", args[0], componentID.String(), deleteRes)
	return nil
}
