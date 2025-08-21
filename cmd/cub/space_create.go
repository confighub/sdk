// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
	spaceCmd.AddCommand(spaceCreateCmd)
}

func checkSpaceCreateConflictingArgs(args []string) (bool, error) {
	// Determine if bulk create mode: no positional args and has bulk-specific flags
	isBulkCreateMode := len(args) == 0 && (where != "" || len(spaceIdentifiers) > 0 || len(spaceCreateArgs.namePrefixes) > 0)

	if isBulkCreateMode {
		// Validate bulk create requirements
		if where == "" && len(spaceIdentifiers) == 0 {
			return false, errors.New("bulk create mode requires --where or --space flags")
		}
		if len(spaceIdentifiers) > 0 && where != "" {
			return false, errors.New("--space and --where flags are mutually exclusive")
		}
		if len(spaceCreateArgs.namePrefixes) == 0 {
			return false, errors.New("bulk create mode requires --name-prefix")
		}
	} else {
		// Single create mode validation
		if len(args) == 0 {
			return false, errors.New("space name is required for single space creation")
		}
		if where != "" || len(spaceIdentifiers) > 0 || len(spaceCreateArgs.namePrefixes) > 0 {
			return false, errors.New("bulk create flags (--where, --space, --name-prefix) can only be used without positional arguments")
		}
		// Validate conflicting options - if 2nd arg is "-" (stdin for config), can't also read metadata from stdin
		if len(args) > 1 && args[1] == "-" && flagPopulateModelFromStdin {
			return false, errors.New("can't read both entity attributes and config data from stdin")
		}
	}

	if err := validateStdinFlags(); err != nil {
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

	// Even if slug was set in stdin, we override it with the one from args
	newBody.Slug = makeSlug(args[0])

	spaceRes, err := cubClientNew.CreateSpaceWithResponse(ctx, *newBody)
	if IsAPIError(err, spaceRes) {
		return InterpretErrorGeneric(err, spaceRes)
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
	if IsAPIError(err, bulkRes) {
		return InterpretErrorGeneric(err, bulkRes)
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

	successCount := 0
	errorCount := 0

	for i, response := range responses {
		fmt.Printf("Space %d:\n", i+1)
		if response.Error != nil {
			errorCount++
			fmt.Printf("  Error: %s\n", response.Error.Message)
			if response.Space != nil {
				fmt.Printf("  Space ID: %s\n", response.Space.SpaceID.String())
				fmt.Printf("  Slug: %s\n", response.Space.Slug)
			}
		} else if response.Space != nil {
			successCount++
			fmt.Printf("  Successfully %sd space:\n", operation)
			fmt.Printf("  Space ID: %s\n", response.Space.SpaceID.String())
			fmt.Printf("  Slug: %s\n", response.Space.Slug)
			if response.Space.DisplayName != "" {
				fmt.Printf("  Display Name: %s\n", response.Space.DisplayName)
			}
		}
		fmt.Println()
	}

	fmt.Printf("Summary: %d succeeded, %d failed out of %d total spaces\n", successCount, errorCount, len(responses))

	// Return error for complete failure (status 400+) but not for partial success (207)
	if statusCode == 207 {
		if errorCount == len(responses) {
			return fmt.Errorf("all %s operations failed", operation)
		}
	} else if statusCode >= 400 {
		return fmt.Errorf("bulk %s operation failed", operation)
	}

	return nil
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
