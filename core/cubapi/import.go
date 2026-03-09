// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
)

// ParseBoolOption extracts a boolean option from the options map
func ParseBoolOption(options *goclientnew.ImportOptions, key string, defaultValue bool) bool {
	if options == nil {
		return defaultValue
	}
	if val, ok := (*options)[key]; ok {
		if boolVal, ok := val.(bool); ok {
			return boolVal
		}
	}
	return defaultValue
}

// ParseStringOption extracts a string option from the options map
func ParseStringOption(options *goclientnew.ImportOptions, key string, defaultValue string) string {
	if options == nil {
		return defaultValue
	}
	if val, ok := (*options)[key]; ok {
		if stringVal, ok := val.(string); ok {
			return stringVal
		}
	}
	return defaultValue
}

// ListEntitiesForImport fetches entities of a specific type using the provided where filter.
// It returns the entities as a slice of interfaces that can be processed further.
func ListEntitiesForImport(ctx context.Context, client *goclientnew.ClientWithResponses, spaceID string, entityType string, whereFilter string) ([]interface{}, error) {
	spaceUUID := uuid.MustParse(spaceID)

	switch entityType {
	case EntityTypeSpace:
		params := &goclientnew.ListSpacesParams{
			Where: &whereFilter,
		}
		resp, err := client.ListSpacesWithResponse(ctx, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil {
			return nil, nil
		}
		result := make([]interface{}, len(*resp.JSON200))
		for i, item := range *resp.JSON200 {
			result[i] = item.Space
		}
		return result, nil

	case EntityTypeUnit:
		params := &goclientnew.ListUnitsParams{
			Where: &whereFilter,
		}
		resp, err := client.ListUnitsWithResponse(ctx, spaceUUID, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil {
			return nil, nil
		}
		result := make([]interface{}, len(*resp.JSON200))
		for i, item := range *resp.JSON200 {
			result[i] = item.Unit
		}
		return result, nil

	case EntityTypeLink:
		params := &goclientnew.ListLinksParams{
			Where: &whereFilter,
		}
		resp, err := client.ListLinksWithResponse(ctx, spaceUUID, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil {
			return nil, nil
		}
		result := make([]interface{}, len(*resp.JSON200))
		for i, item := range *resp.JSON200 {
			result[i] = item.Link
		}
		return result, nil

	case EntityTypeView:
		params := &goclientnew.ListViewsParams{
			Where: &whereFilter,
		}
		resp, err := client.ListViewsWithResponse(ctx, spaceUUID, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil {
			return nil, nil
		}
		result := make([]interface{}, len(*resp.JSON200))
		for i, item := range *resp.JSON200 {
			result[i] = item.View
		}
		return result, nil

	case EntityTypeFilter:
		params := &goclientnew.ListFiltersParams{
			Where: &whereFilter,
		}
		resp, err := client.ListFiltersWithResponse(ctx, spaceUUID, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil {
			return nil, nil
		}
		result := make([]interface{}, len(*resp.JSON200))
		for i, item := range *resp.JSON200 {
			result[i] = item.Filter
		}
		return result, nil

	case EntityTypeTrigger:
		params := &goclientnew.ListTriggersParams{
			Where: &whereFilter,
		}
		resp, err := client.ListTriggersWithResponse(ctx, spaceUUID, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil {
			return nil, nil
		}
		result := make([]interface{}, len(*resp.JSON200))
		for i, item := range *resp.JSON200 {
			result[i] = item.Trigger
		}
		return result, nil

	case EntityTypeInvocation:
		params := &goclientnew.ListInvocationsParams{
			Where: &whereFilter,
		}
		resp, err := client.ListInvocationsWithResponse(ctx, spaceUUID, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil {
			return nil, nil
		}
		result := make([]interface{}, len(*resp.JSON200))
		for i, item := range *resp.JSON200 {
			result[i] = item.Invocation
		}
		return result, nil

	case EntityTypeChangeSet:
		params := &goclientnew.ListChangeSetsParams{
			Where: &whereFilter,
		}
		resp, err := client.ListChangeSetsWithResponse(ctx, spaceUUID, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil {
			return nil, nil
		}
		result := make([]interface{}, len(*resp.JSON200))
		for i, item := range *resp.JSON200 {
			result[i] = item.ChangeSet
		}
		return result, nil

	case EntityTypeTag:
		params := &goclientnew.ListTagsParams{
			Where: &whereFilter,
		}
		resp, err := client.ListTagsWithResponse(ctx, spaceUUID, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil {
			return nil, nil
		}
		result := make([]interface{}, len(*resp.JSON200))
		for i, item := range *resp.JSON200 {
			result[i] = item.Tag
		}
		return result, nil

	case EntityTypeAttribute:
		params := &goclientnew.ListAttributesParams{
			Where: &whereFilter,
		}
		resp, err := client.ListAttributesWithResponse(ctx, spaceUUID, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil {
			return nil, nil
		}
		result := make([]interface{}, len(*resp.JSON200))
		for i, item := range *resp.JSON200 {
			result[i] = item.Attribute
		}
		return result, nil

	default:
		return nil, errors.Newf("unsupported entity type for import: %s", entityType)
	}
}

// BuildWhereFilterFromImportFilters constructs a where filter string from ImportFilters
func BuildWhereFilterFromImportFilters(filters []goclientnew.ImportFilter) (string, error) {
	if len(filters) == 0 {
		return "", nil
	}

	var filterParts []string
	for _, filter := range filters {
		if len(filter.Values) == 0 {
			continue
		}

		// Handle different operators
		switch filter.Operator {
		case "=", "!=", ">", "<", ">=", "<=":
			// For single value operators
			if len(filter.Values) != 1 {
				return "", fmt.Errorf("operator %s expects exactly one value", filter.Operator)
			}
			filterParts = append(filterParts, fmt.Sprintf("%s %s '%s'", filter.Type, filter.Operator, filter.Values[0]))

		case "IN", "NOT IN":
			// For multiple values
			quotedValues := make([]string, len(filter.Values))
			for i, v := range filter.Values {
				quotedValues[i] = fmt.Sprintf("'%s'", v)
			}
			filterParts = append(filterParts, fmt.Sprintf("%s %s (%s)", filter.Type, filter.Operator, strings.Join(quotedValues, ",")))

		case "LIKE", "ILIKE":
			// For pattern matching
			if len(filter.Values) != 1 {
				return "", fmt.Errorf("operator %s expects exactly one value", filter.Operator)
			}
			filterParts = append(filterParts, fmt.Sprintf("%s %s '%s'", filter.Type, filter.Operator, filter.Values[0]))

		default:
			// Default to equality if operator is empty
			if filter.Operator == "" && len(filter.Values) == 1 {
				filterParts = append(filterParts, fmt.Sprintf("%s = '%s'", filter.Type, filter.Values[0]))
			} else {
				return "", fmt.Errorf("unsupported operator: %s", filter.Operator)
			}
		}
	}

	// Join all filter parts with AND
	return strings.Join(filterParts, " AND "), nil
}
