// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var filterListCmd = &cobra.Command{
	Use:   "list",
	Short: "List filters",
	Long: getCommandHelp(`List filters you have access to in a space or across all spaces.

Examples:
`+"```"+`
  # List all filters in a space with headers
  cub filter list --space my-space

  # List filters across all spaces (requires --space "*")
  cub filter list --space "*" --where "From = 'Unit'"

  # List filters without headers for scripting
  cub filter list --space my-space --no-header

  # List filters in JSON format
  cub filter list --space my-space --json

  # List only filter names
  cub filter list --space my-space --no-header --names

  # List filters with a specific From type
  cub filter list --space my-space --where "From = 'Unit'"

  # List filters with resource type
  cub filter list --space my-space --where "ResourceType LIKE 'apps/v1/%'"

  # List filters that match a specific entity
  cub filter list --space my-space --entity-type Unit --entity-id my-unit

  # List filters that match a space (works across all spaces)
  cub filter list --space "*" --entity-type Space --entity-id my-space
`+"```"+`
`, ""),
	Args:        cobra.ExactArgs(0),
	RunE:        filterListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
var defaultFilterColumns = []string{"Filter.Slug", "Space.Slug", "Filter.From", "Filter.Where", "Filter.WhereData", "Filter.ResourceType", "FromSpace.Slug"}

// Filter-specific aliases
var filterAliases = map[string]string{
	"Name": "Filter.Slug",
	"ID":   "Filter.FilterID",
}

// Filter custom column dependencies
var filterCustomColumnDependencies = map[string][]string{}

var (
	entityType string
	entityID   string
)

func init() {
	addStandardListFlags(filterListCmd)

	// Add entity-type and entity-id flags for filtering
	filterListCmd.Flags().StringVar(&entityType, "entity-type", "", "Entity type to filter for (e.g., Space). Must be specified together with --entity-id.")
	filterListCmd.Flags().StringVar(&entityID, "entity-id", "", "Entity ID or slug to filter for. Must be specified together with --entity-type.")

	filterCmd.AddCommand(filterListCmd)
}

func filterListCmdRun(cmd *cobra.Command, args []string) error {
	// Validate entity parameters
	if err := validateEntityParameters(); err != nil {
		return err
	}

	var extendedFilters []*goclientnew.ExtendedFilter
	var err error

	if selectedSpaceID == "*" {
		extendedFilters, err = apiSearchFilters(where, selectFields)
		if err != nil {
			return err
		}
	} else {
		extendedFilters, err = apiListFilters(selectedSpaceID, where, selectFields)
		if err != nil {
			return err
		}
	}

	displayListResults(extendedFilters, getFilterSlug, displayFilterList)
	return nil
}

func getFilterSlug(filter *goclientnew.ExtendedFilter) string {
	return filter.Filter.Slug
}

func displayFilterList(filters []*goclientnew.ExtendedFilter) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Space", "From", "Where", "Where-Data", "Resource-Type", "From-Space"})
	}
	for _, f := range filters {
		filter := f.Filter
		spaceSlug := f.Filter.FilterID.String()
		if f.Space != nil {
			spaceSlug = f.Space.Slug
		} else if selectedSpaceID != "*" {
			spaceSlug = selectedSpaceSlug
		}

		fromSpaceSlug := ""
		if f.FromSpace != nil {
			fromSpaceSlug = f.FromSpace.Slug
		}

		// Truncate long where clauses for display
		whereDisplay := filter.Where
		if len(whereDisplay) > 50 {
			whereDisplay = whereDisplay[:47] + "..."
		}

		whereDataDisplay := filter.WhereData
		if len(whereDataDisplay) > 30 {
			whereDataDisplay = whereDataDisplay[:27] + "..."
		}

		table.Append([]string{
			filter.Slug,
			spaceSlug,
			filter.From,
			whereDisplay,
			whereDataDisplay,
			filter.ResourceType,
			fromSpaceSlug,
		})
	}
	table.Render()
}

func apiListFilters(spaceID string, whereFilter string, selectParam string) ([]*goclientnew.ExtendedFilter, error) {
	newParams := &goclientnew.ListFiltersParams{}
	include := "SpaceID,FromSpaceID"
	newParams.Include = &include
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}

	// Add entity parameters if specified
	if entityType != "" && entityID != "" {
		resolvedEntityType, resolvedEntityID, err := parseEntityIdentifierForFilter(entityID, entityType, "")
		if err != nil {
			return nil, err
		}
		newParams.Entity = &resolvedEntityType
		newParams.Id = &resolvedEntityID
	}

	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "FilterID", "SpaceID", "OrganizationID"}
		return buildSelectList("Filter", "", include, defaultFilterColumns, filterAliases, filterCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	filtersRes, err := cubClientNew.ListFiltersWithResponse(ctx, uuid.MustParse(spaceID), newParams)
	if cubapi.IsAPIError(err, filtersRes) {
		return nil, cubapi.InterpretErrorGeneric(err, filtersRes)
	}

	filters := make([]*goclientnew.ExtendedFilter, 0, len(*filtersRes.JSON200))
	for _, filter := range *filtersRes.JSON200 {
		filters = append(filters, &filter)
	}

	return filters, nil
}

func apiSearchFilters(whereFilter string, selectParam string) ([]*goclientnew.ExtendedFilter, error) {
	newParams := &goclientnew.ListAllFiltersParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}

	// Add entity parameters if specified
	if entityType != "" && entityID != "" {
		resolvedEntityType, resolvedEntityID, err := parseEntityIdentifierForFilter(entityID, entityType, "")
		if err != nil {
			return nil, err
		}
		newParams.Entity = &resolvedEntityType
		newParams.Id = &resolvedEntityID
	}

	include := "SpaceID,FromSpaceID"
	newParams.Include = &include

	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "FilterID", "SpaceID", "OrganizationID"}
		return buildSelectList("Filter", "", include, defaultFilterColumns, filterAliases, filterCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}

	res, err := cubClientNew.ListAllFilters(ctx, newParams)
	if err != nil {
		return nil, err
	}
	filtersRes, err := goclientnew.ParseListAllFiltersResponse(res)
	if cubapi.IsAPIError(err, filtersRes) {
		return nil, cubapi.InterpretErrorGeneric(err, filtersRes)
	}

	extendedFilters := make([]*goclientnew.ExtendedFilter, 0, len(*filtersRes.JSON200))
	for _, filter := range *filtersRes.JSON200 {
		extendedFilters = append(extendedFilters, &filter)
	}

	return extendedFilters, nil
}

// validateEntityParameters validates that both or neither of entity-type and entity-id are specified
func validateEntityParameters() error {
	if (entityType == "") != (entityID == "") {
		return fmt.Errorf("both --entity-type and --entity-id must be specified together, or neither")
	}

	if entityType != "" {
		supportedTypes := []string{"Space", "Filter", "View", "Invocation", "Trigger", "Tag", "ChangeSet", "Target", "BridgeWorker", "Unit", "Link", "Set"}
		found := false
		for _, supported := range supportedTypes {
			if entityType == supported {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unsupported entity type: %s. Supported types: %v", entityType, supportedTypes)
		}
	}
	return nil
}

// parseEntityIdentifierForFilter parses a single entity identifier for both organization-level and space-resident entities
// Supports: Space (organization-level) and all space-resident entities
func parseEntityIdentifierForFilter(
	identifier string,
	entityType string,
	selectParam string,
) (string, string, error) {
	if identifier == "" {
		return "", "", fmt.Errorf("%s value cannot be empty", entityType)
	}

	switch entityType {
	case "Space":
		// Organization-level entity
		space, err := apiGetSpaceFromSlug(identifier, selectParam)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve Space %s: %w", identifier, err)
		}
		return entityType, space.SpaceID.String(), nil

	case "Filter":
		filterUUID, err := parseEntityIdentifierSingle[goclientnew.Filter](
			identifier,
			EntityTypeFilter,
			apiGetFilterFromSlugInSpace,
			func(f *goclientnew.Filter) string { return f.FilterID.String() },
		)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve Filter %s: %w", identifier, err)
		}
		return entityType, filterUUID.String(), nil

	case "View":
		viewUUID, err := parseEntityIdentifierSingle[goclientnew.View](
			identifier,
			EntityTypeView,
			apiGetViewFromSlugInSpace,
			func(v *goclientnew.View) string { return v.ViewID.String() },
		)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve View %s: %w", identifier, err)
		}
		return entityType, viewUUID.String(), nil

	case "Invocation":
		invocationUUID, err := parseEntityIdentifierSingle[goclientnew.Invocation](
			identifier,
			EntityTypeInvocation,
			apiGetInvocationFromSlugInSpace,
			func(i *goclientnew.Invocation) string { return i.InvocationID.String() },
		)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve Invocation %s: %w", identifier, err)
		}
		return entityType, invocationUUID.String(), nil

	case "Trigger":
		triggerUUID, err := parseEntityIdentifierSingle[goclientnew.Trigger](
			identifier,
			EntityTypeTrigger,
			apiGetTriggerFromSlugInSpaceCore,
			func(t *goclientnew.Trigger) string { return t.TriggerID.String() },
		)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve Trigger %s: %w", identifier, err)
		}
		return entityType, triggerUUID.String(), nil

	case "Tag":
		tagUUID, err := parseEntityIdentifierSingle[goclientnew.Tag](
			identifier,
			EntityTypeTag,
			apiGetTagFromSlugInSpace,
			func(t *goclientnew.Tag) string { return t.TagID.String() },
		)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve Tag %s: %w", identifier, err)
		}
		return entityType, tagUUID.String(), nil

	case "ChangeSet":
		changeSetUUID, err := parseEntityIdentifierSingle[goclientnew.ChangeSet](
			identifier,
			EntityTypeChangeSet,
			apiGetChangeSetFromSlugInSpace,
			func(c *goclientnew.ChangeSet) string { return c.ChangeSetID.String() },
		)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve ChangeSet %s: %w", identifier, err)
		}
		return entityType, changeSetUUID.String(), nil

	case "Target":
		targetUUID, err := parseEntityIdentifierSingle[goclientnew.Target](
			identifier,
			EntityTypeTarget,
			apiGetTargetFromSlugInSpaceCore,
			func(t *goclientnew.Target) string { return t.TargetID.String() },
		)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve Target %s: %w", identifier, err)
		}
		return entityType, targetUUID.String(), nil

	case "BridgeWorker":
		bridgeWorkerUUID, err := parseEntityIdentifierSingle[goclientnew.BridgeWorker](
			identifier,
			EntityTypeBridgeWorker,
			apiGetBridgeWorkerFromSlugInSpace,
			func(w *goclientnew.BridgeWorker) string { return w.BridgeWorkerID.String() },
		)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve BridgeWorker %s: %w", identifier, err)
		}
		return entityType, bridgeWorkerUUID.String(), nil

	case "Unit":
		unitUUID, err := parseEntityIdentifierSingle[goclientnew.Unit](
			identifier,
			EntityTypeUnit,
			apiGetUnitFromSlugInSpace,
			func(u *goclientnew.Unit) string { return u.UnitID.String() },
		)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve Unit %s: %w", identifier, err)
		}
		return entityType, unitUUID.String(), nil

	case "Link":
		linkUUID, err := parseEntityIdentifierSingle[goclientnew.Link](
			identifier,
			EntityTypeLink,
			apiGetLinkFromSlugInSpace,
			func(l *goclientnew.Link) string { return l.LinkID.String() },
		)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve Link %s: %w", identifier, err)
		}
		return entityType, linkUUID.String(), nil

	// Not implemented yet
	// case "Set":
	// 	setUUID, err := parseEntityIdentifierSingle[goclientnew.Set](
	// 		identifier,
	// 		EntityTypeSet,
	// 		apiGetSetFromSlugInSpace,
	// 		func(s *goclientnew.Set) string { return s.SetID.String() },
	// 	)
	// 	if err != nil {
	// 		return "", "", fmt.Errorf("failed to resolve Set %s: %w", identifier, err)
	// 	}
	// 	return entityType, setUUID.String(), nil

	default:
		return "", "", fmt.Errorf("unsupported entity type: %s", entityType)
	}
}
