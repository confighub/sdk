// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var attributeCreateCmd = &cobra.Command{
	Use:   "create [<slug> <toolchain-type> <data-type>]",
	Short: "Create a new attribute or bulk create attributes",
	Long: getCommandHelp(`Create a new attribute or bulk create multiple attributes by cloning existing ones.

SINGLE ATTRIBUTE CREATION:

Create a new attribute to register getter and setter functions for configuration paths.

ToolchainTypes:

  - Kubernetes/YAML: For Kubernetes YAML configurations

DataTypes:

  - string: String values
  - int: Integer values
  - bool: Boolean values

For complex attributes with ResourceTypePaths, use --filename to provide a YAML/JSON definition.

BULK ATTRIBUTE CREATION:

When no positional arguments are provided, bulk create mode is activated. This mode clones existing
attributes based on filters and creates multiple new attributes with optional modifications.

Single Attribute Examples:
`+"```"+`
  # Create a simple attribute from a YAML file
  cub attribute create --space my-space --filename attr.yaml

  # Create a basic attribute with args
  cub attribute create --space my-space my-attr Kubernetes/YAML string --description "My attribute"

  # Create with allow-exists
  cub attribute create --space my-space --filename attr.yaml --allow-exists
`+"```"+`

Bulk Create Examples:
`+"```"+`
  # Clone all attributes matching a pattern with name prefixes
  cub attribute create --where "Slug LIKE 'app-%'" --name-prefix dev-,staging- --dest-space dev-space

  # Clone specific attributes to multiple spaces
  cub attribute create --attribute my-attr --dest-space dev-space,staging-space
`+"```"+`
`, ""),
	Args:        cobra.MinimumNArgs(0),
	RunE:        attributeCreateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var attributeCreateArgs struct {
	destSpaces     []string
	whereSpace     string
	namePrefixes   []string
	attributeSlugs []string
	filterSpace    string
}

func init() {
	addStandardCreateFlags(attributeCreateCmd)
	attributeCreateCmd.Flags().StringVar(&attributeDescription, "description", "", "Description for the attribute")
	enableWhereFlag(attributeCreateCmd)
	enableFilterFlag(attributeCreateCmd)

	// Bulk create specific flags
	attributeCreateCmd.Flags().StringSliceVar(&attributeCreateArgs.destSpaces, "dest-space", []string{}, "destination spaces for bulk create (can be repeated or comma-separated)")
	attributeCreateCmd.Flags().StringVar(&attributeCreateArgs.whereSpace, "where-space", "", "where expression to select destination spaces for bulk create")
	attributeCreateCmd.Flags().StringSliceVar(&attributeCreateArgs.namePrefixes, "name-prefix", []string{}, "name prefixes for bulk create (can be repeated or comma-separated)")
	attributeCreateCmd.Flags().StringSliceVar(&attributeCreateArgs.attributeSlugs, "attribute", []string{}, "target specific attributes by slug or UUID for bulk create (can be repeated or comma-separated)")
	attributeCreateCmd.Flags().StringVar(&attributeCreateArgs.filterSpace, "filter-space", "", "filter entity containing WHERE expression to select destination spaces for bulk create (slug or UUID)")

	attributeCmd.AddCommand(attributeCreateCmd)
}

func checkAttributeCreateConflictingArgs(args []string) (bool, error) {
	isBulkCreateMode := len(args) == 0 && !flagPopulateModelFromStdin && flagFilename == ""

	if isBulkCreateMode {
		if len(attributeCreateArgs.attributeSlugs) > 0 && where != "" {
			return false, errors.New("--attribute and --where flags are mutually exclusive")
		}

		if len(attributeCreateArgs.destSpaces) > 0 && attributeCreateArgs.whereSpace != "" {
			return false, errors.New("--dest-space and --where-space flags are mutually exclusive")
		}

		if len(attributeCreateArgs.destSpaces) == 0 && attributeCreateArgs.whereSpace == "" && len(attributeCreateArgs.namePrefixes) == 0 {
			return false, errors.New("bulk create mode requires at least one of --dest-space, --where-space, or --name-prefix")
		}
	} else if len(args) > 0 && len(args) < 3 && flagFilename == "" && !flagPopulateModelFromStdin {
		return false, errors.New("single attribute creation requires: <slug> <toolchain-type> <data-type> or --filename")
	}

	if err := validateSpaceFlag(isBulkCreateMode); err != nil {
		return isBulkCreateMode, err
	}

	if err := validateStdinFlags(); err != nil {
		return isBulkCreateMode, err
	}

	if err := ValidateLabelRemoval(label, false); err != nil {
		return isBulkCreateMode, err
	}
	if err := ValidateDeleteGateRemoval(deleteGate, false); err != nil {
		return isBulkCreateMode, err
	}

	return isBulkCreateMode, nil
}

func attributeCreateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkCreateMode, err := checkAttributeCreateConflictingArgs(args)
	if err != nil {
		return err
	}

	if isBulkCreateMode {
		return runBulkAttributeCreate()
	}

	return runSingleAttributeCreate(args)
}

func runSingleAttributeCreate(args []string) error {
	spaceID := uuid.MustParse(selectedSpaceID)
	newBody := goclientnew.Attribute{}
	if flagPopulateModelFromStdin || flagFilename != "" {
		if err := populateModelFromFlags(&newBody); err != nil {
			return err
		}
	}
	err := setAnnotations(&newBody.Annotations)
	if err != nil {
		return err
	}
	err = setLabels(&newBody.Labels)
	if err != nil {
		return err
	}
	err = setDeleteGates(&newBody.DeleteGates)
	if err != nil {
		return err
	}
	newBody.SpaceID = spaceID

	if len(args) >= 3 {
		newBody.Slug = makeSlug(args[0])
		if newBody.DisplayName == "" {
			newBody.DisplayName = args[0]
		}
		newBody.ToolchainType = args[1]
		newBody.DataType = args[2]
	}

	if attributeDescription != "" {
		newBody.Description = attributeDescription
	}

	params := &goclientnew.CreateAttributeParams{}
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}

	attrRes, err := cubClientNew.CreateAttributeWithResponse(ctx, spaceID, params, newBody)
	if cubapi.IsAPIError(err, attrRes) {
		return cubapi.InterpretErrorGeneric(err, attrRes)
	}

	attrDetails := attrRes.JSON200
	displayCreateResults(attrDetails, "attribute", attrDetails.Slug, attrDetails.AttributeID.String(), displayAttributeDetails)
	return nil
}

func runBulkAttributeCreate() error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	var effectiveWhere string
	if len(attributeCreateArgs.attributeSlugs) > 0 {
		whereClause, err := buildWhereClauseFromAttributes(attributeCreateArgs.attributeSlugs)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	patchJSON, err := BuildPatchData(nil)
	if err != nil {
		return err
	}

	include := "SpaceID"
	params := &goclientnew.BulkCreateAttributesParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}

	if len(attributeCreateArgs.namePrefixes) > 0 {
		namePrefixesStr := strings.Join(attributeCreateArgs.namePrefixes, ",")
		params.NamePrefixes = &namePrefixesStr
	}

	var whereSpaceExpr string
	if attributeCreateArgs.whereSpace != "" {
		whereSpaceExpr = attributeCreateArgs.whereSpace
	} else if len(attributeCreateArgs.destSpaces) > 0 {
		whereSpaceExpr, err = buildWhereClauseForSpaces(attributeCreateArgs.destSpaces)
		if err != nil {
			return errors.Wrapf(err, "error converting destination spaces to where expression")
		}
	}

	if whereSpaceExpr != "" {
		params.WhereSpace = &whereSpaceExpr
	}

	if attributeCreateArgs.filterSpace != "" {
		filterSpaceID, err := parseFilterFlag(attributeCreateArgs.filterSpace)
		if err != nil {
			return errors.Wrapf(err, "error parsing filter-space")
		}
		params.FilterSpace = &filterSpaceID
	}

	bulkRes, err := cubClientNew.BulkCreateAttributesWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	return handleBulkAttributeCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "create", effectiveWhere)
}
