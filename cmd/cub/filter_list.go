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
  cub filter list --space my-space --no-headers

  # List filters in JSON format
  cub filter list --space my-space -o json

  # List only filter names
  cub filter list --space my-space --no-headers -o name

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

// filterListInclude is the Include parameter for filter list queries (the related
// entities expanded into each ExtendedFilter).
const filterListInclude = "SpaceID,FromSpaceID"

// filterBaseSelectFields are the fields always returned by filter list queries,
// regardless of the requested columns.
var filterBaseSelectFields = []string{"Slug", "FilterID", "SpaceID", "OrganizationID"}

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

	extendedFilters, err := apiListFilters(selectedSpaceID, where, selectFields)
	if err != nil {
		return err
	}

	displayListResults(extendedFilters, getFilterSlug, displayFilterList)
	return nil
}

func getFilterSlug(filter *goclientnew.ExtendedFilter) string {
	space := ""
	if filter.Space != nil {
		space = filter.Space.Slug
	}
	return prefixedSlug(space, filter.Filter.Slug)
}

func displayFilterList(filters []*goclientnew.ExtendedFilter) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Space", "From", "Where", "Where-Data", "Resource-Type", "From-Space"})
	}
	for _, f := range filters {
		filter := f.Filter
		spaceSlug := f.Filter.SpaceID.String()
		if f.Space != nil {
			spaceSlug = f.Space.Slug
		} else if selectedSpaceID != "*" {
			spaceSlug = selectedSpaceSlug
		}

		fromSpaceSlug := ""
		if f.FromSpace != nil {
			fromSpaceSlug = f.FromSpace.Slug
		}

		// The data clause gets a narrower column than the metadata one, which is the one
		// people read first.
		const maxWhereDataWidth = 30
		whereDisplay := truncateWithEllipsis(filter.Where, defaultColumnWidth)
		whereDataDisplay := truncateWithEllipsis(filter.WhereData, maxWhereDataWidth)

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

// apiListFilters lists filters via the org-level endpoint, scoped to a single
// space by a SpaceID clause unless spaceID is "*" (list across all spaces).
func apiListFilters(spaceID string, whereFilter string, selectParam string) ([]*goclientnew.ExtendedFilter, error) {
	where := cubapi.NewWhere(whereFilter)
	if spaceID != "*" {
		where = where.SpaceID(goclientnew.UUID(uuid.MustParse(spaceID)))
	}
	return apiListAllFilters(where, selectParam)
}

func apiListAllFilters(where cubapi.Where, selectParam string) ([]*goclientnew.ExtendedFilter, error) {
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		return buildSelectList("Filter", nil, filterListInclude, defaultFilterColumns, filterAliases, filterCustomColumnDependencies, filterBaseSelectFields)
	})

	// Resolve the --entity-type/--entity-id options up front (resolution can
	// fail); the mutator only assigns the resolved values onto the params.
	var with []func(*goclientnew.ListAllFiltersParams)
	if entityType != "" && entityID != "" {
		resolvedEntityType, resolvedEntityID, err := parseEntityIdentifierForFilter(entityID, entityType, "")
		if err != nil {
			return nil, err
		}
		with = append(with, func(p *goclientnew.ListAllFiltersParams) {
			p.Entity = &resolvedEntityType
			p.Id = &resolvedEntityID
		})
	}

	return cubapi.ListFilters(ctx, cubClient, where, cubapi.ListOpts{
		Select:   cubapi.SelectFields(selectValue),
		Include:  filterListInclude,
		Contains: contains,
	}, with...)
}

// validateEntityParameters validates that both or neither of entity-type and entity-id are specified
func validateEntityParameters() error {
	if (entityType == "") != (entityID == "") {
		return fmt.Errorf("both --entity-type and --entity-id must be specified together, or neither")
	}

	if entityType != "" {
		supportedTypes := []string{"Space", "Filter", "View", "Invocation", "Trigger", "Tag", "ChangeSet", "ChangeOrder", "ChangeWorkflow", "Target", "BridgeWorker", "Unit", "Link", "Set"}
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
		space, err := resolveSpace(identifier, selectParam)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve Space %s: %w", identifier, err)
		}
		return entityType, space.Space.SpaceID.String(), nil

	case "Filter":
		filterUUID, err := resolveFilterID(identifier)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve Filter %s: %w", identifier, err)
		}
		return entityType, filterUUID.String(), nil

	case "View":
		viewUUID, err := resolveViewID(identifier)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve View %s: %w", identifier, err)
		}
		return entityType, viewUUID.String(), nil

	case "Invocation":
		invocationUUID, err := resolveInvocationID(identifier)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve Invocation %s: %w", identifier, err)
		}
		return entityType, invocationUUID.String(), nil

	case "Trigger":
		triggerUUID, err := resolveTriggerID(identifier)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve Trigger %s: %w", identifier, err)
		}
		return entityType, triggerUUID.String(), nil

	case "Tag":
		tagUUID, err := resolveTagID(identifier)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve Tag %s: %w", identifier, err)
		}
		return entityType, tagUUID.String(), nil

	case "ChangeSet":
		changeSetUUID, err := resolveChangeSetID(identifier)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve ChangeSet %s: %w", identifier, err)
		}
		return entityType, changeSetUUID.String(), nil

	case "ChangeOrder":
		changeOrderUUID, err := resolveChangeOrderID(identifier)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve ChangeOrder %s: %w", identifier, err)
		}
		return entityType, changeOrderUUID.String(), nil

	case "ChangeWorkflow":
		changeWorkflowUUID, err := resolveChangeWorkflowID(identifier)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve ChangeWorkflow %s: %w", identifier, err)
		}
		return entityType, changeWorkflowUUID.String(), nil

	case "Target":
		targetUUID, err := resolveTargetID(identifier)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve Target %s: %w", identifier, err)
		}
		return entityType, targetUUID.String(), nil

	case "BridgeWorker":
		bridgeWorkerUUID, err := resolveWorkerID(identifier)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve BridgeWorker %s: %w", identifier, err)
		}
		return entityType, bridgeWorkerUUID.String(), nil

	case "Unit":
		unitUUID, err := resolveUnitID(identifier)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve Unit %s: %w", identifier, err)
		}
		return entityType, unitUUID.String(), nil

	case "Link":
		linkUUID, err := resolveLinkID(identifier)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve Link %s: %w", identifier, err)
		}
		return entityType, linkUUID.String(), nil

	case "Attribute":
		attributeUUID, err := resolveAttributeID(identifier)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve Attribute %s: %w", identifier, err)
		}
		return entityType, attributeUUID.String(), nil

	default:
		return "", "", fmt.Errorf("unsupported entity type: %s", entityType)
	}
}
