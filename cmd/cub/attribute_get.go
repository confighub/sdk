// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var attributeGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about an attribute",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about an attribute in a space including its ID, slug, toolchain type, data type, description, resource type paths, and parameters.

Examples:
`+"```"+`
  # Get details about an attribute
  cub attribute get --space my-space --json my-attr

  # Get attribute details in YAML format
  cub attribute get --space my-space --yaml my-attr
`+"```"+`
`, ""),
	RunE: attributeGetCmdRun,
}

func init() {
	addStandardGetFlags(attributeGetCmd)
	attributeCmd.AddCommand(attributeGetCmd)
}

func attributeGetCmdRun(cmd *cobra.Command, args []string) error {
	attrDetails, err := apiGetAttributeFromSlug(args[0], selectFields)
	if err != nil {
		return err
	}

	displayGetResults(attrDetails, displayExtendedAttributeDetails)
	return nil
}

func displayExtendedAttributeDetails(extendedAttr *goclientnew.ExtendedAttribute) {
	attr := extendedAttr.Attribute
	view := tableView()
	view.Append([]string{"ID", attr.AttributeID.String()})
	view.Append([]string{"Name", attr.Slug})

	if extendedAttr.Space != nil {
		view.Append([]string{"Space", extendedAttr.Space.Slug})
	} else {
		view.Append([]string{"Space ID", attr.SpaceID.String()})
	}
	view.Append([]string{"Created At", attr.CreatedAt.String()})
	view.Append([]string{"Updated At", attr.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(attr.Labels)})
	view.Append([]string{"Delete Gates", deleteGatesToString(attr.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(attr.Annotations)})
	view.Append([]string{"Organization ID", attr.OrganizationID.String()})

	view.Append([]string{"Toolchain Type", attr.ToolchainType})
	view.Append([]string{"Data Type", attr.DataType})
	view.Append([]string{"Description", attr.Description})
	view.Append([]string{"Hash", attr.Hash})

	if len(attr.ResourceTypePaths) > 0 {
		var rtpLines []string
		for _, entry := range attr.ResourceTypePaths {
			rtpLines = append(rtpLines, fmt.Sprintf("  %s:", entry.ResourceType))
			if entry.Paths != nil {
				for path := range *entry.Paths {
					rtpLines = append(rtpLines, fmt.Sprintf("    - %s", path))
				}
			}
			if entry.GetterInvocation != nil {
				rtpLines = append(rtpLines, fmt.Sprintf("    getter: %s", entry.GetterInvocation.FunctionName))
			}
			if entry.SetterInvocation != nil {
				rtpLines = append(rtpLines, fmt.Sprintf("    setter: %s", entry.SetterInvocation.FunctionName))
			}
		}
		view.Append([]string{"Resource Type Paths", strings.Join(rtpLines, "\n")})
	}

	if len(attr.Parameters) > 0 {
		var paramLines []string
		for _, p := range attr.Parameters {
			paramLines = append(paramLines, fmt.Sprintf("  %s (%s)", p.ParameterName, p.DataType))
		}
		view.Append([]string{"Parameters", strings.Join(paramLines, "\n")})
	}

	view.Render()
}

func displayAttributeDetails(attr *goclientnew.Attribute) {
	view := tableView()
	view.Append([]string{"ID", attr.AttributeID.String()})
	view.Append([]string{"Name", attr.Slug})
	view.Append([]string{"Space ID", attr.SpaceID.String()})
	view.Append([]string{"Created At", attr.CreatedAt.String()})
	view.Append([]string{"Updated At", attr.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(attr.Labels)})
	view.Append([]string{"Delete Gates", deleteGatesToString(attr.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(attr.Annotations)})
	view.Append([]string{"Organization ID", attr.OrganizationID.String()})

	view.Append([]string{"Toolchain Type", attr.ToolchainType})
	view.Append([]string{"Data Type", attr.DataType})
	view.Append([]string{"Description", attr.Description})
	view.Append([]string{"Hash", attr.Hash})
	view.Render()
}

func apiGetAttribute(attrID string, selectParam string) (*goclientnew.ExtendedAttribute, error) {
	newParams := &goclientnew.GetAttributeParams{}
	include := "SpaceID"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	attrRes, err := cubClientNew.GetAttributeWithResponse(ctx, uuid.MustParse(selectedSpaceID), uuid.MustParse(attrID), newParams)
	if cubapi.IsAPIError(err, attrRes) {
		return nil, cubapi.InterpretErrorGeneric(err, attrRes)
	}
	return attrRes.JSON200, nil
}

func apiGetAttributeFromSlug(slug string, selectParam string) (*goclientnew.ExtendedAttribute, error) {
	return apiGetAttributeFromSlugInSpace(slug, selectedSpaceID, selectParam)
}

// apiGetAttributeFromSlugInSpaceCore returns just the Attribute, for use with parseEntityIdentifiers
func apiGetAttributeFromSlugInSpaceCore(slug string, spaceID string, selectParam string) (*goclientnew.Attribute, error) {
	extendedAttr, err := apiGetAttributeFromSlugInSpace(slug, spaceID, selectParam)
	if err != nil {
		return nil, err
	}
	if extendedAttr.Attribute == nil {
		return nil, fmt.Errorf("attribute data not found")
	}
	return extendedAttr.Attribute, nil
}

func apiGetAttributeFromSlugInSpace(slug string, spaceID string, selectParam string) (*goclientnew.ExtendedAttribute, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetAttribute(id.String(), selectParam)
	}
	if selectParam == "" {
		selectParam = "*"
	}
	attrs, err := apiListAttributes(spaceID, "Slug = '"+slug+"'", selectParam, "")
	if err != nil {
		return nil, err
	}
	for _, attr := range attrs {
		if attr.Attribute != nil && attr.Attribute.Slug == slug {
			return attr, nil
		}
	}
	return nil, fmt.Errorf("attribute %s not found in space %s", slug, spaceID)
}
