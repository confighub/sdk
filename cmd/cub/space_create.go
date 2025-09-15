// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var spaceCreateArgs struct {
	namePrefixes []string
}

var spaceCreateCmd = &cobra.Command{
	Use:   "create [space]",
	Short: "Create a space",
	Args:  cobra.RangeArgs(0, 1),
	Long: `Create a new space as a collaborative context for a project or team.

Single space creation examples:
  # Create a new space named "my-space" with verbose output, reading configuration from stdin
  # Verbose output prints the details of the created entity
  cub space create --verbose --json --from-stdin my-space

  # Create a new space with minimal output
  cub space create my-space

Bulk creation examples:
  # Bulk create spaces by cloning existing spaces with name prefixes
  cub space create --where "Slug IN ('prod', 'staging')" --name-prefix "backup-,test-"

  # Clone specific spaces by identifier with prefixes
  cub space create --space "space1,space2" --name-prefix "new-"`,
	RunE: spaceCreateCmdRun,
}

func init() {
	addStandardCreateFlags(spaceCreateCmd)
	// Bulk create specific flags
	spaceCreateCmd.Flags().StringSliceVar(&spaceCreateArgs.namePrefixes, "name-prefix", []string{}, "name prefixes for bulk create (can be repeated or comma-separated)")
	spaceCreateCmd.Flags().StringSliceVar(&spaceIdentifiers, "space", []string{}, "target specific spaces by slug or UUID for bulk create (can be repeated or comma-separated)")
	enableWhereFlag(spaceCreateCmd)
	enableFilterFlag(spaceCreateCmd)
	spaceCmd.AddCommand(spaceCreateCmd)
}

func checkSpaceCreateConflictingArgs(args []string) (bool, error) {
	// Determine if bulk create mode: no positional args
	isBulkCreateMode := len(args) == 0

	if isBulkCreateMode {
		// Validate bulk create requirements
		if len(spaceIdentifiers) > 0 && where != "" {
			return false, errors.New("--space and --where flags are mutually exclusive")
		}

		if len(spaceCreateArgs.namePrefixes) == 0 {
			return false, errors.New("bulk create mode requires --name-prefix")
		}
	} else {
		// Single create mode validation
		if len(args) != 1 {
			return false, errors.New("space name is required for single space creation")
		}

		if filter != "" || where != "" || len(spaceIdentifiers) > 0 || len(spaceCreateArgs.namePrefixes) > 0 {
			return false, errors.New("bulk create flags (--filter, --where, --space, --name-prefix) can only be used without positional arguments")
		}
	}

	if err := validateStdinFlags(); err != nil {
		return isBulkCreateMode, err
	}

	// Validate no label removal
	if err := ValidateLabelRemoval(label, false); err != nil {
		return isBulkCreateMode, err
	}
	// Validate no delete gate removal
	if err := ValidateDeleteGateRemoval(deleteGate, false); err != nil {
		return isBulkCreateMode, err
	}

	return isBulkCreateMode, nil
}

func spaceCreateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkCreateMode, err := checkSpaceCreateConflictingArgs(args)
	if err != nil {
		return err
	}

	if isBulkCreateMode {
		return runBulkSpaceCreate()
	}
	return runSingleSpaceCreate(args)
}

func runSingleSpaceCreate(args []string) error {
	newBody := &goclientnew.Space{}
	if flagPopulateModelFromStdin || flagFilename != "" {
		if err := populateModelFromFlags(newBody); err != nil {
			return err
		}
	}
	err := setLabels(&newBody.Labels)
	if err != nil {
		return err
	}
	err = setDeleteGates(&newBody.DeleteGates)
	if err != nil {
		return err
	}

	// Even if slug was set in stdin, we override it with the one from args
	newBody.Slug = makeSlug(args[0])

	// Create params with AllowExists if needed
	params := &goclientnew.CreateSpaceParams{}
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}

	spaceRes, err := cubClientNew.CreateSpaceWithResponse(ctx, params, *newBody)
	if cubapi.IsAPIError(err, spaceRes) {
		return cubapi.InterpretErrorGeneric(err, spaceRes)
	}

	spaceDetails := spaceRes.JSON200
	displayCreateResults(spaceDetails, "space", args[0], spaceDetails.SpaceID.String(), displaySpaceDetails)
	return nil
}

// createBulkCreatePatch creates a JSON patch for bulk create operations
func createBulkSpaceCreatePatch() ([]byte, error) {
	// Build patch data using consolidated function (no entity-specific fields for space)
	return BuildPatchData(nil)
}

func runBulkSpaceCreate() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build the where clause
	var effectiveWhere string
	if len(spaceIdentifiers) > 0 {
		// Convert space identifiers to where clause
		whereClause, err := buildWhereClauseFromIdentifiers(spaceIdentifiers, "SpaceID", "Slug")
		if err != nil {
			return fmt.Errorf("error building where clause from space identifiers: %w", err)
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Create JSON patch for customizing cloned spaces
	patchJSON, err := createBulkSpaceCreatePatch()
	if err != nil {
		return err
	}

	// Build bulk create parameters
	params := &goclientnew.BulkCreateSpacesParams{
		Where: &effectiveWhere,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// Set allow_exists parameter if flag is set
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}

	// Set include parameter for filtering if needed
	include := "OrganizationID"
	params.Include = &include

	// Set name prefixes parameter if specified
	if len(spaceCreateArgs.namePrefixes) > 0 {
		namePrefixesStr := strings.Join(spaceCreateArgs.namePrefixes, ",")
		params.NamePrefixes = &namePrefixesStr
	}

	// Call the bulk create API
	bulkRes, err := cubClientNew.BulkCreateSpacesWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}

	// Handle response based on status code
	var responses []goclientnew.SpaceCreateOrUpdateResponse
	var statusCode int

	if bulkRes.JSON200 != nil {
		responses = *bulkRes.JSON200
		statusCode = 200
	} else if bulkRes.JSON207 != nil {
		responses = *bulkRes.JSON207
		statusCode = 207
	} else {
		return fmt.Errorf("unexpected response from bulk create API")
	}

	return handleBulkSpaceCreateOrUpdateResponse(responses, statusCode, "create", "")
}

func handleBulkSpaceCreateOrUpdateResponse(responses []goclientnew.SpaceCreateOrUpdateResponse, statusCode int, operation, changeDescription string) error {
	if len(responses) == 0 {
		fmt.Printf("No spaces found to %s\n", operation)
		return nil
	}

	// Convert slice to pointer for generic function
	var responses200 *[]goclientnew.SpaceCreateOrUpdateResponse
	var responses207 *[]goclientnew.SpaceCreateOrUpdateResponse
	if statusCode == 200 {
		responses200 = &responses
	} else if statusCode == 207 {
		responses207 = &responses
	}

	return displayBulkGenericCreateOrUpdateResults(
		responses200, responses207, statusCode, "space", operation, changeDescription,
		func(r *goclientnew.SpaceCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
		func(r *goclientnew.SpaceCreateOrUpdateResponse) string {
			if r.Space != nil {
				return fmt.Sprintf("%s (ID: %s)", r.Space.Slug, r.Space.SpaceID)
			}
			return ""
		},
	)
}

// UnmarshalBinary interface implementation
func UnmarshalBinary(m *goclientnew.Space, b []byte) error {
	var res goclientnew.Space
	if err := json.Unmarshal(b, res); err != nil {
		return err
	}
	*m = res
	return nil
}
