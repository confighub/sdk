// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var variantDeleteCmd = &cobra.Command{
	Use:         "delete <variant>",
	Short:       "Delete a variant and its configuration",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Delete a variant: the space that holds it, with every unit and other entity in it.

The variant is named as <component>/<variant>, or by its space slug. Unlike "cub space
delete", which refuses a space with entities in it unless --recursive is given, a variant
is its configuration, so deleting it always deletes the units; a unit with a delete gate
still stops it unless --force is given.

A base is not deleted while deployments cloned from it remain: their units still point
at its units as their upstream. The error names them. Delete the deployments first, or
pass --detach to delete the base anyway and leave the clones without an upstream.

Examples:
`+"```"+`
  # Delete a deployment
  cub variant delete web/dev

  # Delete the base after its deployments are gone
  cub variant delete web/base

  # Delete a base whose deployments stay, detaching them from it
  cub variant delete --detach web/base
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: variantDeleteCmdRun,
}

var variantDeleteForce bool

func init() {
	addStandardDeleteFlags(variantDeleteCmd)
	variantDeleteCmd.Flags().BoolVar(&variantDeleteForce, "force", false,
		"delete the variant's units even if some have delete gates")
	addDetachFlag(variantDeleteCmd)
	variantCmd.AddCommand(variantDeleteCmd)
}

func variantDeleteCmdRun(_ *cobra.Command, args []string) error {
	space, err := resolveVariantOrSpace(args[0], "SpaceID,Slug")
	if err != nil {
		return err
	}
	params := &goclientnew.DeleteSpaceParams{}
	recursiveParam := "true"
	params.Recursive = &recursiveParam
	if variantDeleteForce {
		params.RecursiveForce = &recursiveParam
	}
	params.Detach = detachParam()
	deleteRes, err := cubClientNew.DeleteSpaceWithResponse(ctx, space.Space.SpaceID, params)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}
	displayDeleteResults("variant", args[0], space.Space.SpaceID.String(), deleteRes)
	return nil
}
