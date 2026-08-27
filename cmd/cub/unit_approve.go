// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitApproveCmd = &cobra.Command{
	Use:   "approve [<unit-slug>]",
	Short: "Approve a unit or multiple units",
	Long: getCommandHelp(`Approve a unit or multiple units using bulk operations.

Single unit approve:
`+"```"+`
  cub unit approve my-unit

  # Approve a specific revision
  cub unit approve my-unit --revision 5
  cub unit approve my-unit --revision LastReleasedRevisionNum
  cub unit approve my-unit --revision Tag:release-v1.0
  cub unit approve my-unit --revision ChangeSet:feature-rollout
`+"```"+`

Bulk approve with --where:

Approve multiple units at once based on search criteria.

Examples:
`+"```"+`
  # Approve all units with specific label
  cub unit approve --where "Labels.Tier = 'backend'"

  # Approve a specific revision for all matching units
  cub unit approve --where "Labels.Tier = 'backend'" --revision LastReleasedRevisionNum

  # Approve units across all spaces (requires --space "*")
  cub unit approve --space "*" --where "Slug = 'backend'"

  # Approve specific units by slug
  cub unit approve --unit my-unit,another-unit
`+"```"+`
`, ""),
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        unitApproveCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var approveRevision string

func init() {
	enableWhereFlag(unitApproveCmd)
	enableFilterFlag(unitApproveCmd)
	unitApproveCmd.Flags().StringSliceVar(&unitIdentifiers, "unit", []string{}, "target specific units by slug or UUID for bulk approve (can be repeated or comma-separated)")
	unitApproveCmd.Flags().StringVar(&approveRevision, "revision", "", "Revision to approve (defaults to HeadRevisionNum). Can be a revision number, 'LastReleasedRevisionNum', 'Tag:slug', 'ChangeSet:slug', etc.")
	enableWaitFlag(unitApproveCmd)
	addStandardDisplayFlags(unitApproveCmd)
	unitCmd.AddCommand(unitApproveCmd)
}

// parseApproveRevisionParameter parses the revision parameter for approve operations
// and returns the properly formatted revision string for the API.
func parseApproveRevisionParameter(revision string) (*string, error) {
	if revision == "" {
		return nil, nil
	}

	// Check for Before: prefix (not supported for approve)
	if strings.HasPrefix(revision, "Before:") {
		return nil, fmt.Errorf("'Before:' modifier is not supported for approve operations")
	}

	// Parse the revision parameter
	parts := strings.Split(revision, ":")
	var entityType, identifier string

	switch len(parts) {
	case 2:
		// EntityType:Identifier format
		entityType = parts[0]
		identifier = parts[1]
	case 1:
		// Simple identifier (LastReleasedRevisionNum, UUID, integer, etc.)
		identifier = parts[0]
	default:
		return nil, fmt.Errorf("invalid revision specification: %s", revision)
	}

	// Handle entity type-specific parsing
	if entityType == "Tag" {
		// Parse tag slug/ID and convert to UUID
		tagUUID, err := parseTagSlug(identifier)
		if err != nil {
			return nil, fmt.Errorf("failed to parse tag '%s': %w", identifier, err)
		}
		result := fmt.Sprintf("Tag:%s", tagUUID.String())
		return &result, nil

	} else if entityType == "ChangeSet" {
		// Parse changeset slug/ID and convert to UUID
		changesetUUID, err := parseChangeSetSlug(identifier)
		if err != nil {
			return nil, fmt.Errorf("failed to parse changeset '%s': %w", identifier, err)
		}
		result := fmt.Sprintf("ChangeSet:%s", changesetUUID)
		return &result, nil

	} else if entityType == "Revision" {
		// Handle Revision:uuid format
		if _, err := uuid.Parse(identifier); err != nil {
			return nil, fmt.Errorf("invalid revision UUID: %s", identifier)
		}
		result := fmt.Sprintf("Revision:%s", identifier)
		return &result, nil

	} else if entityType != "" {
		return nil, fmt.Errorf("unsupported entity type '%s': supported types are Tag, ChangeSet, and Revision", entityType)
	}

	// Handle simple identifiers (no entity type prefix)
	namedRevisions := map[string]bool{
		"LastReleasedRevisionNum": true,
		"HeadRevisionNum":         true,
	}

	if namedRevisions[identifier] {
		// Named revisions are passed as-is
		return &identifier, nil
	}

	// Check if it's a UUID
	if _, err := uuid.Parse(identifier); err == nil {
		// It's a UUID - format as Revision:uuid
		result := fmt.Sprintf("Revision:%s", identifier)
		return &result, nil
	}

	// Check if it's a number (revision number)
	if _, err := strconv.ParseInt(identifier, 10, 64); err == nil {
		// It's a number - pass as-is (the API will handle it)
		return &identifier, nil
	}

	// Not a valid revision specification
	return nil, fmt.Errorf("invalid revision specification: %s. Must be a revision number, named revision (LastReleasedRevisionNum, HeadRevisionNum), UUID, or EntityType:identifier format", revision)
}

func checkUnitApproveConflictingArgs(args []string) bool {
	// Check for bulk approve mode: no positional args
	isBulkApproveMode := len(args) == 0

	if isBulkApproveMode {
		// Check for mutual exclusivity between --unit and --where flags
		if len(unitIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--unit and --where flags are mutually exclusive"))
		}

	} else {
		// Single approve mode validation
		if len(args) != 1 {
			failOnError(fmt.Errorf("single unit approve requires exactly one argument: <slug or id>"))
		}

		if filter != "" || where != "" || len(unitIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --unit can only be specified with no positional arguments"))
		}
	}

	if err := validateSpaceFlag(isBulkApproveMode); err != nil {
		failOnError(err)
	}

	return isBulkApproveMode
}

func runBulkUnitApprove() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Parse revision parameter
	revisionParam, err := parseApproveRevisionParameter(approveRevision)
	if err != nil {
		return err
	}

	// Build WHERE clause from unit identifiers or use provided where clause
	var effectiveWhere string
	if len(unitIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromUnits(unitIdentifiers)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Build bulk approve parameters
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	params := &goclientnew.BulkApproveUnitsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}
	if revisionParam != nil {
		params.Revision = revisionParam
	}

	// Call the bulk approve API
	bulkRes, err := cubClientNew.BulkApproveUnitsWithResponse(ctx, params)
	if err != nil {
		return err
	}

	// Handle the response
	return handleBulkUnitApproveResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "approve", effectiveWhere)
}

func unitApproveCmdRun(cmd *cobra.Command, args []string) error {
	isBulkApproveMode := checkUnitApproveConflictingArgs(args)

	if isBulkApproveMode {
		return runBulkUnitApprove()
	}

	// Single unit approve logic
	configUnit, err := apiGetUnitFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}

	// Parse revision parameter
	revisionParam, err := parseApproveRevisionParameter(approveRevision)
	if err != nil {
		return err
	}

	// Build approve parameters
	params := &goclientnew.ApproveUnitParams{}
	if revisionParam != nil {
		params.Revision = revisionParam
	}

	approveRes, err := cubClientNew.ApproveUnitWithResponse(ctx, uuid.MustParse(selectedSpaceID), configUnit.UnitID, params)
	if cubapi.IsAPIError(err, approveRes) {
		return cubapi.InterpretErrorGeneric(err, approveRes)
	}

	if !quiet {
		fmt.Printf("Unit %s (%s) has been approved\n", args[0], configUnit.UnitID.String())
	}

	// Wait for triggers to complete if --wait is specified
	if wait {
		var approvedUnit *goclientnew.Unit
		if approveRes.JSON200 != nil && approveRes.JSON200.Unit != nil {
			approvedUnit = approveRes.JSON200.Unit
		} else {
			// Fallback: re-fetch the unit
			approvedUnit, err = apiGetUnitInSpace(configUnit.UnitID.String(), selectedSpaceID, "*")
			if err != nil {
				return err
			}
		}
		tprint("Awaiting triggers...")
		return awaitTriggersRemoval(approvedUnit)
	}
	return nil
}

func handleBulkUnitApproveResponse(responses200 *[]goclientnew.ApproveResponse, responses207 *[]goclientnew.ApproveResponse, statusCode int, operationName, contextInfo string) error {
	var responses *[]goclientnew.ApproveResponse
	if statusCode == 200 && responses200 != nil {
		responses = responses200
	} else if statusCode == 207 && responses207 != nil {
		responses = responses207
	} else {
		return fmt.Errorf("unexpected status code %d or no response data", statusCode)
	}

	if responses == nil {
		return fmt.Errorf("no response data received")
	}

	successCount := 0
	failureCount := 0
	var failures []string

	for _, resp := range *responses {
		if resp.Error == nil {
			successCount++
			if verbose {
				fmt.Printf("Successfully %sd unit: %s\n", operationName, resp.Message)
			}
		} else {
			failureCount++
			errorMsg := "unknown error"
			if resp.Error != nil && resp.Error.Message != "" {
				errorMsg = resp.Error.Message
			}
			failures = append(failures, fmt.Sprintf("  - %s", errorMsg))
		}
	}

	// Display summary
	if !isAlternativeOutput() {
		fmt.Printf("\nBulk %s operation completed:\n", operationName)
		fmt.Printf("  Success: %d unit(s)\n", successCount)
		if failureCount > 0 {
			fmt.Printf("  Failed: %d unit(s)\n", failureCount)
			if verbose && len(failures) > 0 {
				fmt.Println("\nFailures:")
				for _, failure := range failures {
					fmt.Println(failure)
				}
			}
		}
		if contextInfo != "" {
			fmt.Printf("  Context: %s\n", contextInfo)
		}
	}

	// Return success only if all operations succeeded
	if statusCode == 207 || failureCount > 0 {
		return fmt.Errorf("bulk %s partially failed: %d succeeded, %d failed", operationName, successCount, failureCount)
	}

	return nil
}
