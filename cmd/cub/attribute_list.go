// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var attributeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List attributes",
	Long: getCommandHelp(`List attributes you have access to in a space or across all spaces. The output includes slugs, toolchain types, data types, and descriptions.

Examples:
`+"```"+`
  # List all attributes in a space
  cub attribute list --space my-space

  # List attributes across all spaces (requires --space "*")
  cub attribute list --space "*"

  # List attributes in JSON format
  cub attribute list --space my-space -o json

  # List only attribute names
  cub attribute list --space my-space --no-headers -o name

  # List attributes for a specific toolchain
  cub attribute list --space my-space --where "ToolchainType = 'Kubernetes/YAML'"
`+"```"+`
`, ""),
	Args:        cobra.ExactArgs(0),
	RunE:        attributeListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
var defaultAttributeColumns = []string{"Attribute.Slug", "Space.Slug", "Attribute.ToolchainType", "Attribute.DataType", "Attribute.Description"}

// Attribute-specific aliases
var attributeAliases = map[string]string{
	"Name": "Attribute.Slug",
	"ID":   "Attribute.AttributeID",
}

// Attribute custom column dependencies
var attributeCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(attributeListCmd)
	attributeCmd.AddCommand(attributeListCmd)
}

func attributeListCmdRun(cmd *cobra.Command, args []string) error {
	var extendedAttributes []*goclientnew.ExtendedAttribute
	var err error

	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	if selectedSpaceID == "*" {
		extendedAttributes, err = apiSearchAttributes(where, selectFields, filterID)
		if err != nil {
			return err
		}
	} else {
		extendedAttributes, err = apiListAttributes(selectedSpaceID, where, selectFields, filterID)
		if err != nil {
			return err
		}
	}

	displayListResults(extendedAttributes, getAttributeSlug, displayAttributeList)
	return nil
}

func getAttributeSlug(attr *goclientnew.ExtendedAttribute) string {
	space := ""
	if attr.Space != nil {
		space = attr.Space.Slug
	}
	return prefixedSlug(space, attr.Attribute.Slug)
}

func displayAttributeList(attrs []*goclientnew.ExtendedAttribute) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Space", "Toolchain-Type", "Data-Type", "Description"})
	}
	for _, a := range attrs {
		attr := a.Attribute
		spaceSlug := attr.AttributeID.String()
		if a.Space != nil {
			spaceSlug = a.Space.Slug
		} else if selectedSpaceID != "*" {
			spaceSlug = selectedSpaceSlug
		}
		desc := attr.Description
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}
		table.Append([]string{
			attr.Slug,
			spaceSlug,
			attr.ToolchainType,
			attr.DataType,
			desc,
		})
	}
	table.Render()
}

func apiListAttributes(spaceID string, whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedAttribute, error) {
	newParams := &goclientnew.ListAttributesParams{}
	include := "SpaceID"
	newParams.Include = &include
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	if filterParam != "" {
		newParams.Filter = &filterParam
	}
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "AttributeID", "SpaceID", "OrganizationID"}
		return buildSelectList("Attribute", nil, include, defaultAttributeColumns, attributeAliases, attributeCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	attrsRes, err := cubClientNew.ListAttributesWithResponse(ctx, uuid.MustParse(spaceID), newParams)
	if cubapi.IsAPIError(err, attrsRes) {
		return nil, cubapi.InterpretErrorGeneric(err, attrsRes)
	}

	attrs := make([]*goclientnew.ExtendedAttribute, 0, len(*attrsRes.JSON200))
	for _, attr := range *attrsRes.JSON200 {
		attrs = append(attrs, &attr)
	}

	return attrs, nil
}

func apiSearchAttributes(whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedAttribute, error) {
	newParams := &goclientnew.ListAllAttributesParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	if filterParam != "" {
		newParams.Filter = &filterParam
	}

	include := "SpaceID"
	newParams.Include = &include

	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "AttributeID", "SpaceID", "OrganizationID"}
		return buildSelectList("Attribute", nil, include, defaultAttributeColumns, attributeAliases, attributeCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}

	res, err := cubClientNew.ListAllAttributes(ctx, newParams)
	if err != nil {
		return nil, err
	}
	attrsRes, err := goclientnew.ParseListAllAttributesResponse(res)
	if cubapi.IsAPIError(err, attrsRes) {
		return nil, cubapi.InterpretErrorGeneric(err, attrsRes)
	}

	extendedAttrs := make([]*goclientnew.ExtendedAttribute, 0, len(*attrsRes.JSON200))
	for _, attr := range *attrsRes.JSON200 {
		extendedAttrs = append(extendedAttrs, &attr)
	}

	return extendedAttrs, nil
}
