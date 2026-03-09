// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var attributeDeleteCmd = &cobra.Command{
	Use:   "delete [<slug or id>]",
	Short: "Delete an attribute or multiple attributes",
	Long: getCommandHelp(`Delete an attribute or multiple attributes using bulk operations.

Single attribute delete:
`+"```"+`
  cub attribute delete my-attr
`+"```"+`

Bulk delete with --where:

Delete multiple attributes at once based on search criteria.

Examples:
`+"```"+`
  # Delete all attributes for a specific toolchain
  cub attribute delete --where "ToolchainType = 'Kubernetes/YAML'"

  # Delete specific attributes by slug
  cub attribute delete --attribute my-attr,another-attr

  # Delete attributes across all spaces (requires --space "*")
  cub attribute delete --space "*" --where "Labels.cleanup = 'true'"
`+"```"+`
`, ""),
	Args:        cobra.MaximumNArgs(1),
	RunE:        attributeDeleteCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	attributeDeleteIdentifiers []string
)

func init() {
	addStandardDeleteFlags(attributeDeleteCmd)
	enableWhereFlag(attributeDeleteCmd)
	enableFilterFlag(attributeDeleteCmd)
	attributeDeleteCmd.Flags().StringSliceVar(&attributeDeleteIdentifiers, "attribute", []string{}, "target specific attributes by slug or UUID for bulk delete (can be repeated or comma-separated)")
	attributeCmd.AddCommand(attributeDeleteCmd)
}

func checkAttributeDeleteConflictingArgs(args []string) bool {
	isBulkDeleteMode := len(args) == 0

	if isBulkDeleteMode {
		if len(attributeDeleteIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--attribute and --where flags are mutually exclusive"))
		}
	} else {
		if len(args) != 1 {
			failOnError(fmt.Errorf("single attribute delete requires exactly one argument: <slug or id>"))
		}

		if filter != "" || where != "" || len(attributeDeleteIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --attribute can only be specified with no positional arguments"))
		}
	}

	if err := validateSpaceFlag(isBulkDeleteMode); err != nil {
		failOnError(err)
	}

	return isBulkDeleteMode
}

func runBulkAttributeDelete() error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	var effectiveWhere string
	if len(attributeDeleteIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromAttributes(attributeDeleteIdentifiers)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	include := "SpaceID"
	params := &goclientnew.BulkDeleteAttributesParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	bulkRes, err := cubClientNew.BulkDeleteAttributesWithResponse(ctx, params)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}

	return handleBulkAttributeDeleteResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "delete", effectiveWhere)
}

func attributeDeleteCmdRun(cmd *cobra.Command, args []string) error {
	isBulkDeleteMode := checkAttributeDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkAttributeDelete()
	}

	attrDetails, err := apiGetAttributeFromSlug(args[0], "*")
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteAttributeWithResponse(ctx, uuid.MustParse(selectedSpaceID), attrDetails.Attribute.AttributeID)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("attribute", args[0], attrDetails.Attribute.AttributeID.String(), deleteRes)
	return nil
}

func handleBulkAttributeDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkDeleteResults(responses200, responses207, statusCode, "attribute", operationName, contextInfo)
}
