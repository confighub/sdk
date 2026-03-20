// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var linkUpdateCmd = &cobra.Command{
	Use:   "update [<link slug or id> <from unit slug> <to unit slug> [<to space slug>]]",
	Short: "Update a link or multiple links",
	Long: getCommandHelp(`Update a link or multiple links using bulk operations.

Single link update:
`+"```"+`
  cub link update my-link from-unit to-unit [to-space]
`+"```"+`

Individual patch with --patch:

Update a single link using JSON merge patch. Requires --patch flag with link slug.

Examples:
`+"```"+`
  # Patch individual link with JSON
  echo '{"Labels": {"env": "prod"}}' | cub link update my-link --patch --from-stdin

  # Patch individual link with labels
  cub link update my-link --patch --label env=prod,team=backend
`+"```"+`

Bulk update with --patch:

Update multiple links at once based on search criteria. Requires --patch flag with no positional arguments.

Examples:
`+"```"+`
  # Update labels for multiple links using JSON patch
  echo '{"Labels": {"env": "prod"}}' | cub link update --patch --where "DisplayName LIKE 'app-%'" --from-stdin

  # Update labels for multiple links using --label flag
  cub link update --patch --where "DisplayName LIKE 'app-%'" --label env=prod,team=backend

  # Update links across all spaces (requires --space "*")
  cub link update --patch --space "*" --where "ToSpaceID = 'old-space-id'" --from-stdin

  # Update specific links by slug
  echo '{"Labels": {"updated": "true"}}' | cub link update --patch --link my-link,another-link --from-stdin
`+"```"+`
`, ""),
	Args:        cobra.MinimumNArgs(0), // Allow 0 args for bulk mode
	RunE:        linkUpdateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	linkPatch       bool
	linkReverse     bool
	linkIdentifiers []string
)

func init() {
	addStandardUpdateFlags(linkUpdateCmd)
	enableWaitFlag(linkUpdateCmd)
	addLinkFieldFlags(linkUpdateCmd)
	linkUpdateCmd.Flags().BoolVar(&linkPatch, "patch", false, "use patch API for individual or bulk operations")
	linkUpdateCmd.Flags().BoolVar(&linkReverse, "reverse", false, "swap FromUnit and ToUnit directions (requires --patch)")
	enableWhereFlag(linkUpdateCmd)
	enableFilterFlag(linkUpdateCmd)
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

	// Check if any alternative output format is specified
	hasAlternativeOutput := jsonOutput || jq != ""

	// Wait for triggers BEFORE calling the generic display function
	if wait {
		successfulLinks := []*goclientnew.Link{}
		for _, resp := range *responses {
			if resp.Error == nil && resp.Link != nil {
				successfulLinks = append(successfulLinks, resp.Link)
			}
		}

		if len(successfulLinks) > 0 {
			if !quiet && !hasAlternativeOutput {
				tprintRaw("Awaiting triggers...")
			}
			// Wait for triggers on each affected unit
			for _, link := range successfulLinks {
				unitDetails, err := apiGetUnitInSpace(link.FromUnitID.String(), link.SpaceID.String(), "*")
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

	// Call the generic display function
	return displayBulkGenericCreateOrUpdateResults(
		responses200, responses207, statusCode, "link", operationName, contextInfo,
		func(r *goclientnew.LinkCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
		func(r *goclientnew.LinkCreateOrUpdateResponse) string {
			if r.Link != nil {
				return r.Link.Slug
			}
			return ""
		},
	)
}

func checkLinkConflictingArgs(args []string) bool {
	// Check for bulk patch mode: no positional args
	isBulkPatchMode := len(args) == 0

	if isBulkPatchMode {
		if !linkPatch {
			failOnError(errors.New("--patch is required in bulk mode"))
		}

		// Check for mutual exclusivity between --invocation and --where flags
		if len(linkIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--link and --where flags are mutually exclusive"))
		}

	} else {
		// Single update mode validation
		if linkPatch {
			// Individual patch mode: only slug required
			if len(args) != 1 {
				failOnError(errors.New("individual patch requires exactly one argument: <slug>"))
			}
		} else if len(args) < 3 || len(args) > 4 {
			failOnError(errors.New("single link update requires: <slug> <from unit> <to unit> [to space]"))
		}

		if filter != "" || where != "" || len(linkIdentifiers) > 0 {
			failOnError(errors.New("--filter, --where, and --link flags can only be used in bulk mode (without positional arguments)"))
		}
	}

	if linkReverse && !linkPatch {
		failOnError(errors.New("--reverse requires --patch"))
	}

	if err := validateLinkFieldFlags(); err != nil {
		failOnError(err)
	}

	// Validate label removal only works with patch
	if err := ValidateLabelRemoval(label, linkPatch); err != nil {
		failOnError(err)
	}
	// Validate delete gate removal only works with patch
	if err := ValidateDeleteGateRemoval(deleteGate, linkPatch); err != nil {
		failOnError(err)
	}

	if err := validateSpaceFlag(isBulkPatchMode); err != nil {
		failOnError(err)
	}

	return isBulkPatchMode
}

func runBulkLinkUpdate(cmd *cobra.Command) error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	if !flagPopulateModelFromStdin && flagFilename == "" && len(label) == 0 && len(deleteGate) == 0 && !hasLinkFieldFlags(cmd) && !linkReverse {
		return fmt.Errorf("bulk patch requires one of: --from-stdin, --filename, --label, --delete-gate, --reverse, or link field flags")
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

	// Build patch data using consolidated function with link-specific field enhancer
	patchJSON, err := BuildPatchData(linkFieldsEnhancer(cmd))
	if err != nil {
		return err
	}

	// Build bulk patch parameters
	include := "SpaceID,FromUnitID,ToUnitID,ToSpaceID"
	params := &goclientnew.BulkPatchLinksParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}
	if linkReverse {
		params.Reverse = &linkReverse
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
	return handleBulkLinkUpdateResponse(res.JSON200, res.JSON207, res.StatusCode(), "update", effectiveWhere)
}

func runIndividualLinkPatch(cmd *cobra.Command, linkSlug string) error {
	if !flagPopulateModelFromStdin && flagFilename == "" && len(label) == 0 && len(deleteGate) == 0 && !hasLinkFieldFlags(cmd) && !linkReverse {
		return fmt.Errorf("--patch requires one of: --from-stdin, --filename, --label, --delete-gate, --reverse, or link field flags")
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

	// Build patch data using consolidated function with link-specific field enhancer
	patchData, err = BuildPatchData(linkFieldsEnhancer(cmd))
	if err != nil {
		return err
	}

	// Call the individual patch API
	params := &goclientnew.PatchLinkParams{}
	if linkReverse {
		params.Reverse = &linkReverse
	}
	res, err := cubClientNew.PatchLinkWithBodyWithResponse(
		ctx,
		spaceID,
		linkID,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}

	linkDetails := res.JSON200
	displayUpdateResults(linkDetails, "link", linkSlug, linkDetails.LinkID.String(), displayLinkDetails)
	if wait {
		if !quiet {
			tprint("Awaiting triggers...")
		}
		unitDetails, err := apiGetUnitInSpace(linkDetails.FromUnitID.String(), linkDetails.SpaceID.String(), "*") // get all fields for now
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
		return runBulkLinkUpdate(cmd)
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
		return runIndividualLinkPatch(cmd, args[0])
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
	err = setAnnotations(&currentLink.Annotations)
	if err != nil {
		return err
	}
	err = setLabels(&currentLink.Labels)
	if err != nil {
		return err
	}
	err = setDeleteGates(&currentLink.DeleteGates)
	if err != nil {
		return err
	}

	// If this was set from stdin, it will be overridden
	currentLink.SpaceID = spaceID

	fromUnit, err := apiGetUnitFromSlugInSpace(args[1], spaceID.String(), "*") // get all fields for now
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
	setLinkFieldsOnUpdate(currentLink, cmd)

	linkRes, err := cubClientNew.UpdateLinkWithResponse(ctx, spaceID, currentLink.LinkID, *currentLink)
	if cubapi.IsAPIError(err, linkRes) {
		return cubapi.InterpretErrorGeneric(err, linkRes)
	}

	linkDetails := linkRes.JSON200
	displayUpdateResults(linkDetails, "link", args[0], linkDetails.LinkID.String(), displayLinkDetails)
	if wait {
		if !quiet {
			tprint("Awaiting triggers...")
		}
		unitDetails, err := apiGetUnitInSpace(fromUnitID.String(), spaceID.String(), "*") // get all fields for now
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
