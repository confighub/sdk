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

// attributeListInclude is the Include parameter for attribute list queries.
const attributeListInclude = "SpaceID"

// attributeBaseSelectFields are the fields always returned by attribute list queries.
var attributeBaseSelectFields = []string{"Slug", "AttributeID", "SpaceID", "OrganizationID"}

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
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	extendedAttributes, err := apiListAttributes(selectedSpaceID, where, selectFields, filterID)
	if err != nil {
		return err
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
		spaceSlug := attr.SpaceID.String()
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

// apiListAttributes lists attributes via the org-level endpoint, scoped to a
// single space by a SpaceID clause unless spaceID is "*" (list across all spaces).
func apiListAttributes(spaceID string, whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedAttribute, error) {
	where := cubapi.NewWhere(whereFilter)
	if spaceID != "*" {
		where = where.SpaceID(goclientnew.UUID(uuid.MustParse(spaceID)))
	}
	return apiListAllAttributes(where, selectParam, filterParam)
}

func apiListAllAttributes(where cubapi.Where, selectParam string, filterParam string) ([]*goclientnew.ExtendedAttribute, error) {
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		return buildSelectList("Attribute", nil, attributeListInclude, defaultAttributeColumns, attributeAliases, attributeCustomColumnDependencies, attributeBaseSelectFields)
	})
	return cubapi.ListAttributes(ctx, cubClient, where, cubapi.ListOpts{
		Select:   cubapi.SelectFields(selectValue),
		Include:  attributeListInclude,
		Filter:   filterParam,
		Contains: contains,
	})
}
