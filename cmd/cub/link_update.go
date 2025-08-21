// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var linkUpdateCmd = &cobra.Command{
	Use:   "update [<link slug or id> <from unit slug> <to unit slug> [<to space slug>]]",
	Short: "Update a link or multiple links",
	Long: `Update a link or multiple links using bulk operations.

Single link update:
  cub link update my-link from-unit to-unit [to-space]

Individual patch with --patch:
Update a single link using JSON merge patch. Requires --patch flag with link slug.

  # Patch individual link with JSON
  echo '{"Labels": {"env": "prod"}}' | cub link update my-link --patch --from-stdin
  
  # Patch individual link with labels
  cub link update my-link --patch --label env=prod,team=backend

Bulk update with --patch:
Update multiple links at once based on search criteria. Requires --patch flag with no positional arguments.

Examples:
  # Update labels for multiple links using JSON patch
  echo '{"Labels": {"env": "prod"}}' | cub link update --patch --where "DisplayName LIKE 'app-%'" --from-stdin

  # Update labels for multiple links using --label flag
  cub link update --patch --where "DisplayName LIKE 'app-%'" --label env=prod,team=backend

  # Update links across all spaces (requires --space "*")
  cub link update --patch --space "*" --where "ToSpaceID = 'old-space-id'" --from-stdin

  # Update specific links by slug
  echo '{"Labels": {"updated": "true"}}' | cub link update --patch --link my-link,another-link --from-stdin`,
	Args:        cobra.MinimumNArgs(0), // Allow 0 args for bulk mode
	RunE:        linkUpdateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	linkPatch       bool
	linkIdentifiers []string
)

func init() {
	addStandardUpdateFlags(linkUpdateCmd)
	enableWaitFlag(linkUpdateCmd)
	linkUpdateCmd.Flags().BoolVar(&linkPatch, "patch", false, "use patch API for individual or bulk operations")
	enableWhereFlag(linkUpdateCmd)
	linkUpdateCmd.Flags().StringSliceVar(&linkIdentifiers, "link", []string{}, "target specific links by slug or UUID for bulk patch (can be repeated or comma-separated)")
	linkCmd.AddCommand(linkUpdateCmd)
}

func handleBulkLinkUpdateResponse(responses200 *[]goclientnew.LinkCreateOrUpdateResponse, responses207 *[]goclientnew.LinkCreateOrUpdateResponse, statusCode int, operationName, contextInfo string) error {
	var responses *[]goclientnew.LinkCreateOrUpdateResponse
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
			if verbose && resp.Link != nil {
				fmt.Printf("Successfully %sd link: %s\n", operationName, resp.Link.Slug)
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
	if !jsonOutput {
		fmt.Printf("\nBulk %s operation completed:\n", operationName)
		fmt.Printf("  Success: %d link(s)\n", successCount)
		if failureCount > 0 {
			fmt.Printf("  Failed: %d link(s)\n", failureCount)
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

	if wait {
		if !quiet {
			tprint("Awaiting triggers...")
		}
		for _, resp := range *responses {
			if resp.Error == nil && resp.Link != nil {
				unitDetails, err := apiGetUnitInSpace(resp.Link.FromUnitID.String(), resp.Link.SpaceID.String(), "*") // get all fields for now
				if err != nil {
					return err
				}
				err = awaitTriggersRemoval(unitDetails)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func checkLinkConflictingArgs(args []string) bool {
	// Check for bulk patch mode (no positional args with --patch)
	isBulkPatchMode := linkPatch && len(args) == 0

	// Validate label removal only works with patch
	if err := ValidateLabelRemoval(label, linkPatch); err != nil {
		fmt.Printf("Error: %v\n", err)
		return false
	}

	if !isBulkPatchMode && (where != "" || len(linkIdentifiers) > 0) {
		fmt.Printf("Error: --where and --link flags can only be used in bulk mode (with --patch and without positional arguments)\n")
		return false
	}

	if err := validateSpaceFlag(isBulkPatchMode); err != nil {
		failOnError(err)
	}

	return isBulkPatchMode
}

func runBulkLinkUpdate() error {
	if !flagPopulateModelFromStdin && flagFilename == "" && len(label) == 0 {
		return fmt.Errorf("bulk patch requires one of: --from-stdin, --filename, or --label")
	}

	var effectiveWhere string
	if len(linkIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromLinks(linkIdentifiers)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Build patch data using consolidated function (no entity-specific fields for link)
	patchJSON, err := BuildPatchData(nil)
	if err != nil {
		return err
	}

	// Build bulk patch parameters
	include := "SpaceID,FromUnitID,ToUnitID,ToSpaceID"
	params := &goclientnew.BulkPatchLinksParams{
		Where:   &effectiveWhere,
		Include: &include,
	}

	// Call the bulk patch API
	res, err := cubClientNew.BulkPatchLinksWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	// Handle the response
	return handleBulkLinkUpdateResponse(res.JSON200, res.JSONDefault, res.StatusCode(), "update", effectiveWhere)
}

func runIndividualLinkPatch(linkSlug string) error {
	if !flagPopulateModelFromStdin && flagFilename == "" && len(label) == 0 {
		return fmt.Errorf("--patch requires one of: --from-stdin, --filename, or --label")
	}

	// Get the current link for space and link ID
	currentLink, err := apiGetLinkFromSlug(linkSlug, "*")
	if err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)
	linkID := currentLink.LinkID

	// Get patch data from stdin/filename or use empty patch
	var patchData []byte
	if flagPopulateModelFromStdin || flagFilename != "" {
		patchData, err = getBytesFromFlags()
		if err != nil {
			return fmt.Errorf("failed to read patch data: %w", err)
		}
	}
	if patchData == nil {
		// Null patch for operations with labels only
		patchData = []byte("null")
	}

	// Build patch data using consolidated function
	patchData, err = BuildPatchData(nil)
	if err != nil {
		return err
	}

	// Call the individual patch API
	res, err := cubClientNew.PatchLinkWithBodyWithResponse(
		ctx,
		spaceID,
		linkID,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if IsAPIError(err, res) {
		return InterpretErrorGeneric(err, res)
	}

	linkDetails := res.JSON200
	displayUpdateResults(linkDetails, "link", linkSlug, linkDetails.LinkID.String(), displayLinkDetails)
	if wait {
		if !quiet {
			tprint("Awaiting triggers...")
		}
		unitDetails, err := apiGetUnit(linkDetails.FromUnitID.String(), "*") // get all fields for now
		if err != nil {
			return err
		}
		err = awaitTriggersRemoval(unitDetails)
		if err != nil {
			return err
		}
	}
	return nil
}

func linkUpdateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkPatchMode := checkLinkConflictingArgs(args)

	if isBulkPatchMode {
		return runBulkLinkUpdate()
	}

	// Single link update logic
	if len(args) < 1 {
		return fmt.Errorf("specify link slug/id for single update, or use --patch with --where/--link for bulk update")
	}
	if err := validateStdinFlags(); err != nil {
		return err
	}

	// Handle individual patch mode
	if linkPatch {
		return runIndividualLinkPatch(args[0])
	}

	// Traditional update mode requires unit arguments
	if len(args) < 3 {
		return fmt.Errorf("specify link slug/id and unit slugs for single update, or use --patch for individual patch")
	}

	currentLink, err := apiGetLinkFromSlug(args[0], "*") // get all fields for RMW
	if err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)
	currentLink.SpaceID = spaceID
	// Handle --from-stdin or --filename with optional --replace
	if flagPopulateModelFromStdin || flagFilename != "" {
		existingLink := currentLink
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			currentLink = new(goclientnew.Link)
			currentLink.Version = existingLink.Version
		}

		if err := populateModelFromFlags(currentLink); err != nil {
			return err
		}

		// Ensure essential fields can't be clobbered
		currentLink.OrganizationID = existingLink.OrganizationID
		currentLink.SpaceID = existingLink.SpaceID
		currentLink.LinkID = existingLink.LinkID
	}
	err = setLabels(&currentLink.Labels)
	if err != nil {
		return err
	}

	// If this was set from stdin, it will be overridden
	currentLink.SpaceID = spaceID

	fromUnit, err := apiGetUnitFromSlug(args[1], "*") // get all fields for now
	if err != nil {
		return err
	}
	fromUnitID := fromUnit.UnitID
	toSpaceID := selectedSpaceID
	if len(args) == 4 {
		toSpace, err := apiGetSpaceFromSlug(args[3], "*") // get all fields for now
		if err != nil {
			return err
		}
		toSpaceID = toSpace.SpaceID.String()
	}
	toUnit, err := apiGetUnitFromSlugInSpace(args[2], toSpaceID, "*") // get all fields for now
	if err != nil {
		return err
	}
	toUnitID := toUnit.UnitID

	currentLink.FromUnitID = fromUnitID
	currentLink.ToUnitID = toUnitID
	currentLink.ToSpaceID = uuid.MustParse(toSpaceID)

	linkRes, err := cubClientNew.UpdateLinkWithResponse(ctx, spaceID, currentLink.LinkID, *currentLink)
	if IsAPIError(err, linkRes) {
		return InterpretErrorGeneric(err, linkRes)
	}

	linkDetails := linkRes.JSON200
	displayUpdateResults(linkDetails, "link", args[0], linkDetails.LinkID.String(), displayLinkDetails)
	if wait {
		if !quiet {
			tprint("Awaiting triggers...")
		}
		unitDetails, err := apiGetUnit(fromUnitID.String(), "*") // get all fields for now
		if err != nil {
			return err
		}
		err = awaitTriggersRemoval(unitDetails)
		if err != nil {
			return err
		}
	}
	return nil
}
