// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var attributeUpdateCmd = &cobra.Command{
	Use:   "update [<slug or id>]",
	Short: "Update an attribute or multiple attributes",
	Long: getCommandHelp(`Update an attribute or multiple attributes using bulk operations.

Single attribute update:
`+"```"+`
  # Update an attribute from a YAML file
  cub attribute update --space my-space my-attr --filename attr.yaml

  # Patch an attribute's description
  cub attribute update --space my-space my-attr --patch --description "new description"
`+"```"+`

Bulk update with --patch:

Update multiple attributes at once based on search criteria. Requires --patch flag with no positional arguments.

Examples:
`+"```"+`
  # Update description of all attributes matching a filter
  cub attribute update --patch --where "ToolchainType = 'Kubernetes/YAML'" --description "Updated"

  # Update specific attributes by slug
  cub attribute update --patch --attribute my-attr,another-attr --description "Updated"
`+"```"+`
`, ""),
	Args:        cobra.MinimumNArgs(0),
	RunE:        attributeUpdateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	attributePatch       bool
	attributeIdentifiers []string
)

func init() {
	addStandardUpdateFlags(attributeUpdateCmd)
	attributeUpdateCmd.Flags().StringVar(&attributeDescription, "description", "", "Description for the attribute")
	attributeUpdateCmd.Flags().BoolVar(&attributePatch, "patch", false, "use patch API for individual or bulk operations")
	enableWhereFlag(attributeUpdateCmd)
	enableFilterFlag(attributeUpdateCmd)
	attributeUpdateCmd.Flags().StringSliceVar(&attributeIdentifiers, "attribute", []string{}, "target specific attributes by slug or UUID for bulk patch (can be repeated or comma-separated)")
	attributeCmd.AddCommand(attributeUpdateCmd)
}

func checkAttributeUpdateConflictingArgs(args []string) bool {
	isBulkPatchMode := len(args) == 0

	if isBulkPatchMode {
		if !attributePatch {
			failOnError(errors.New("--patch is required in bulk mode"))
		}

		if len(attributeIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--attribute and --where flags are mutually exclusive"))
		}
	} else {
		if len(args) != 1 {
			failOnError(errors.New("single attribute update requires: <slug or id>"))
		}

		if filter != "" || where != "" || len(attributeIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --attribute can only be specified with --patch and no positional arguments"))
		}
	}

	if attributePatch && flagReplace {
		failOnError(fmt.Errorf("only one of --patch and --replace should be specified"))
	}

	if err := validateSpaceFlag(isBulkPatchMode); err != nil {
		failOnError(err)
	}

	if err := validateStdinFlags(); err != nil {
		failOnError(err)
	}

	if err := ValidateLabelRemoval(label, attributePatch); err != nil {
		failOnError(err)
	}
	if err := ValidateDeleteGateRemoval(deleteGate, attributePatch); err != nil {
		failOnError(err)
	}

	return isBulkPatchMode
}

func runBulkAttributeUpdate() error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	var effectiveWhere string
	if len(attributeIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromAttributes(attributeIdentifiers)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	enhancer := func(patchMap map[string]interface{}) {
		if attributeDescription != "" {
			patchMap["Description"] = attributeDescription
		}
	}

	patchJSON, err := BuildPatchData(enhancer)
	if err != nil {
		return err
	}

	include := "SpaceID"
	params := &goclientnew.BulkPatchAttributesParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	bulkRes, err := cubClientNew.BulkPatchAttributesWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	return handleBulkAttributeCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "update", effectiveWhere)
}

func attributeUpdateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkPatchMode := checkAttributeUpdateConflictingArgs(args)

	if isBulkPatchMode {
		return runBulkAttributeUpdate()
	}

	currentAttr, err := apiGetAttributeFromSlug(args[0], "*")
	if err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)

	if attributePatch {
		attrEnhancer := func(patchData map[string]interface{}) {
			if attributeDescription != "" {
				patchData["Description"] = attributeDescription
			}
		}

		patchData, err := BuildPatchData(attrEnhancer)
		if err != nil {
			return fmt.Errorf("failed to build patch data: %w", err)
		}

		attrDetails, err := patchAttribute(spaceID, currentAttr.Attribute.AttributeID, patchData)
		if err != nil {
			return err
		}

		displayUpdateResults(attrDetails, "attribute", args[0], attrDetails.AttributeID.String(), displayAttributeDetails)
		return nil
	}

	// Traditional update mode
	if flagPopulateModelFromStdin || flagFilename != "" {
		existingAttr := currentAttr.Attribute
		if flagReplace {
			newAttr := new(goclientnew.Attribute)
			newAttr.Version = existingAttr.Version
			currentAttr.Attribute = newAttr
		}

		if err := populateModelFromFlags(currentAttr.Attribute); err != nil {
			return err
		}

		currentAttr.Attribute.OrganizationID = existingAttr.OrganizationID
		currentAttr.Attribute.SpaceID = existingAttr.SpaceID
		currentAttr.Attribute.AttributeID = existingAttr.AttributeID
	}
	err = setAnnotations(&currentAttr.Attribute.Annotations)
	if err != nil {
		return err
	}
	err = setLabels(&currentAttr.Attribute.Labels)
	if err != nil {
		return err
	}

	currentAttr.Attribute.SpaceID = spaceID

	if attributeDescription != "" {
		currentAttr.Attribute.Description = attributeDescription
	}

	attrRes, err := cubClientNew.UpdateAttributeWithResponse(ctx, spaceID, currentAttr.Attribute.AttributeID, *currentAttr.Attribute)
	if cubapi.IsAPIError(err, attrRes) {
		return cubapi.InterpretErrorGeneric(err, attrRes)
	}

	attrDetails := attrRes.JSON200
	displayUpdateResults(attrDetails, "attribute", args[0], attrDetails.AttributeID.String(), displayAttributeDetails)
	return nil
}

func handleBulkAttributeCreateOrUpdateResponse(responses200 *[]goclientnew.AttributeCreateOrUpdateResponse, responses207 *[]goclientnew.AttributeCreateOrUpdateResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkGenericCreateOrUpdateResults(
		responses200, responses207, statusCode, "attribute", operationName, contextInfo,
		func(r *goclientnew.AttributeCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
		func(r *goclientnew.AttributeCreateOrUpdateResponse) string {
			if r.Attribute != nil {
				return fmt.Sprintf("%s (ID: %s)", r.Attribute.Slug, r.Attribute.AttributeID)
			}
			return ""
		},
	)
}

func patchAttribute(spaceID uuid.UUID, attrID uuid.UUID, patchData []byte) (*goclientnew.Attribute, error) {
	attrRes, err := cubClientNew.PatchAttributeWithBodyWithResponse(
		ctx,
		spaceID,
		attrID,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if cubapi.IsAPIError(err, attrRes) {
		return nil, cubapi.InterpretErrorGeneric(err, attrRes)
	}

	return attrRes.JSON200, nil
}
