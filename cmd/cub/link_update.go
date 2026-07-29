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

Resetting merged-revision pointers with --make-current:

--make-current sets a link's UpstreamLastMergedRevisionNum and
DownstreamLastMergedRevisionNum to the current head revisions of the units it
connects, declaring the link caught up without merging anything. Use it to repair
a link whose pointers name a revision that no longer exists, which otherwise fails
every write to that link. Because the values come from each link's own units, a
bulk --make-current patches the matched links one at a time.

Examples:
`+"```"+`
  # Repair one link
  cub link update upgrade-my-unit --space my-space --patch --make-current

  # Repair every UpgradeUnit link in a space
  cub link update --patch --space my-space --where "UpdateType = 'UpgradeUnit'" --make-current
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
	linkUpdateCmd.Flags().BoolVar(&linkReverse, "reverse", false, "swap FromUnit and ToUnit directions (requires --patch); cross-space links are reversed by creating reversed copies and deleting the originals")
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

	// Wait for triggers BEFORE calling the generic display function
	if wait {
		successfulLinks := []*goclientnew.Link{}
		for _, resp := range *responses {
			if resp.Error == nil && resp.Link != nil {
				successfulLinks = append(successfulLinks, resp.Link)
			}
		}

		if len(successfulLinks) > 0 {
			if !quiet && !isAlternativeOutput() {
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

	// --reverse swaps the From and To units, which swaps which unit each merged-revision
	// pointer refers to. --make-current computes those pointers from the current
	// direction, so the combination is ambiguous.
	if linkReverse && linkMakeCurrent {
		failOnError(errors.New("--reverse and --make-current are mutually exclusive"))
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

	effectiveWhere, err := buildLinkBulkEffectiveWhere(linkIdentifiers, where, selectedSpaceID)
	if err != nil {
		return err
	}

	// Build patch data using consolidated function with link-specific field enhancer
	patchJSON, err := BuildPatchData(linkFieldsEnhancer(cmd))
	if err != nil {
		return err
	}

	if linkReverse {
		return runBulkLinkUpdateReverse(effectiveWhere, filterID, patchJSON)
	}

	if linkMakeCurrent {
		return runBulkLinkMakeCurrent(cmd, effectiveWhere, filterID)
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

// runBulkLinkMakeCurrent applies --make-current across every matched link.
// The pointers depend on the Units each link connects, so a single merge patch
// cannot express them; each link is patched individually and the results are
// reported together, the same way the bulk paths report theirs.
func runBulkLinkMakeCurrent(cmd *cobra.Command, effectiveWhere, filterID string) error {
	extLinks, err := apiListAllLinks(cubapi.NewWhere(effectiveWhere), "*", filterID)
	if err != nil {
		return err
	}

	responses := make([]goclientnew.LinkCreateOrUpdateResponse, 0, len(extLinks))
	anyFailed := false
	for _, el := range extLinks {
		if el.Link == nil {
			continue
		}
		link := el.Link

		// The list join supplies both Units. Fall back to fetching them if the
		// server did not expand one.
		var upstream, downstream int64
		if el.FromUnit != nil && el.ToUnit != nil {
			upstream, downstream = makeCurrentPointers(el.FromUnit, el.ToUnit, link.UseLiveState)
		} else {
			var resolveErr error
			upstream, downstream, resolveErr = resolveMakeCurrentPointers(link)
			if resolveErr != nil {
				responses = append(responses, goclientnew.LinkCreateOrUpdateResponse{
					Link:  link,
					Error: &goclientnew.ResponseError{Message: resolveErr.Error()},
				})
				anyFailed = true
				continue
			}
		}

		patchData, err := BuildPatchData(withMakeCurrentPointers(linkFieldsEnhancer(cmd), upstream, downstream))
		if err != nil {
			return err
		}

		res, err := cubClientNew.PatchLinkWithBodyWithResponse(
			ctx,
			link.SpaceID,
			link.LinkID,
			&goclientnew.PatchLinkParams{},
			"application/merge-patch+json",
			bytes.NewReader(patchData),
		)
		if cubapi.IsAPIError(err, res) {
			responses = append(responses, goclientnew.LinkCreateOrUpdateResponse{
				Link:  link,
				Error: &goclientnew.ResponseError{Message: cubapi.InterpretErrorGeneric(err, res).Error()},
			})
			anyFailed = true
			continue
		}
		responses = append(responses, goclientnew.LinkCreateOrUpdateResponse{Link: res.JSON200})
	}

	if len(responses) == 0 {
		if !quiet && !isAlternativeOutput() {
			tprint("No links matched")
		}
		return nil
	}

	if anyFailed {
		return handleBulkLinkUpdateResponse(nil, &responses, 207, "update", effectiveWhere)
	}
	return handleBulkLinkUpdateResponse(&responses, nil, 200, "update", effectiveWhere)
}

// runBulkLinkUpdateReverse handles bulk --reverse for both same-space and
// cross-space links. The server can only reverse same-space links in place
// via PATCH; cross-space links are reversed by creating reversed copies (in
// the new from-unit's space) and deleting the originals.
func runBulkLinkUpdateReverse(effectiveWhere, filterID string, patchJSON []byte) error {
	// Pre-fetch matched links so we can split same-space from cross-space and
	// drive the two paths separately. The single search call avoids surfacing
	// spurious "no links found" errors from the bulk-create endpoint when no
	// cross-space links match.
	extLinks, err := apiListAllLinks(cubapi.NewWhere(effectiveWhere), "*", filterID)
	if err != nil {
		return err
	}

	var sameSpaceIDs, crossSpaceIDs []uuid.UUID
	for _, el := range extLinks {
		if el.Link == nil {
			continue
		}
		if el.Link.SpaceID == el.Link.ToSpaceID {
			sameSpaceIDs = append(sameSpaceIDs, el.Link.LinkID)
		} else {
			crossSpaceIDs = append(crossSpaceIDs, el.Link.LinkID)
		}
	}

	if len(sameSpaceIDs) == 0 && len(crossSpaceIDs) == 0 {
		if !quiet && !isAlternativeOutput() {
			tprint("No links matched")
		}
		return nil
	}

	var firstErr error

	// Same-space subset: in-place reversal via bulk patch.
	if len(sameSpaceIDs) > 0 {
		if err := runSameSpaceLinkPatchReverse(sameSpaceIDs, patchJSON); err != nil {
			firstErr = err
		}
	}

	// Cross-space subset: copy with reverse, then delete originals whose
	// copies were created successfully.
	if len(crossSpaceIDs) > 0 {
		if err := runCrossSpaceLinkReverse(crossSpaceIDs, patchJSON); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// runSameSpaceLinkPatchReverse reverses same-space links in place by issuing a
// BulkPatchLinks call with reverse=true scoped to the given link IDs.
func runSameSpaceLinkPatchReverse(linkIDs []uuid.UUID, patchJSON []byte) error {
	ssWhere := buildLinkIDsWhere(linkIDs)
	include := "SpaceID,FromUnitID,ToUnitID,ToSpaceID"
	rev := true
	params := &goclientnew.BulkPatchLinksParams{
		Where:   &ssWhere,
		Include: &include,
		Reverse: &rev,
	}
	res, err := cubClientNew.BulkPatchLinksWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}
	return handleBulkLinkUpdateResponse(res.JSON200, res.JSON207, res.StatusCode(), "update", ssWhere)
}

// runCrossSpaceLinkReverse reverses cross-space links by creating reversed
// copies in the new from-unit's space, then deleting only those originals
// whose copies were created successfully.
func runCrossSpaceLinkReverse(linkIDs []uuid.UUID, patchJSON []byte) error {
	csWhere := buildLinkIDsWhere(linkIDs)

	createRes, err := callBulkCreateLinks(csWhere, "", patchJSON, true, "", "", false)
	if err != nil {
		return err
	}

	var firstErr error
	if err := handleBulkLinkUpdateResponse(createRes.JSON200, createRes.JSON207, createRes.StatusCode(), "create", csWhere); err != nil {
		firstErr = err
	}

	// Only delete originals whose reversed copies were successfully created.
	sourceIDs := successfullyReversedSourceIDs(createRes)
	if len(sourceIDs) == 0 {
		return firstErr
	}

	delWhere := buildLinkIDsWhere(sourceIDs)

	// If wait was requested, await triggers on the reversed-copy from-units
	// (handled by handleBulkLinkUpdateResponse above) AND on the original
	// from-units that the delete will affect.
	var fromUnits []linkFromUnit
	if wait {
		links, err := apiListAllLinks(cubapi.NewWhere(delWhere), "*", "")
		if err != nil {
			return err
		}
		fromUnits = uniqueFromUnitsFromLinks(links)
	}

	delRes, err := callBulkDeleteLinks(delWhere, "", "")
	if cubapi.IsAPIError(err, delRes) {
		return cubapi.InterpretErrorGeneric(err, delRes)
	}

	if wait && len(fromUnits) > 0 && delRes.StatusCode() == 200 {
		awaitTriggersOnLinkFromUnits(fromUnits)
	}

	if err := handleBulkLinkDeleteResponse(delRes.JSON200, delRes.JSON207, delRes.StatusCode(), "delete", delWhere); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// successfullyReversedSourceIDs returns the LinkIDs of the source links whose
// reversed copies were created successfully (tracked via UpstreamLinkID on
// the new links).
func successfullyReversedSourceIDs(res *goclientnew.BulkCreateLinksResponse) []uuid.UUID {
	var responses *[]goclientnew.LinkCreateOrUpdateResponse
	switch res.StatusCode() {
	case 200:
		responses = res.JSON200
	case 207:
		responses = res.JSON207
	default:
		return nil
	}
	if responses == nil {
		return nil
	}
	var ids []uuid.UUID
	for _, r := range *responses {
		if r.Error != nil || r.Link == nil || r.Link.UpstreamLinkID == nil {
			continue
		}
		ids = append(ids, *r.Link.UpstreamLinkID)
	}
	return ids
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

	// Build patch data using consolidated function with link-specific field enhancer
	enhancer := linkFieldsEnhancer(cmd)
	if linkMakeCurrent {
		upstream, downstream, err := resolveMakeCurrentPointers(currentLink)
		if err != nil {
			return err
		}
		enhancer = withMakeCurrentPointers(enhancer, upstream, downstream)
	}
	patchData, err := BuildPatchData(enhancer)
	if err != nil {
		return err
	}

	// Cross-space links can't be reversed in place; create a reversed copy in
	// the new from-unit's space and delete the original.
	if linkReverse && currentLink.SpaceID != currentLink.ToSpaceID {
		return runCrossSpaceLinkReverse([]uuid.UUID{linkID}, patchData)
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
	if err := setLinkFieldsOnUpdate(currentLink, cmd); err != nil {
		return err
	}

	// Applied after setLinkFieldsOnUpdate so that a --use-live-state/--no-use-live-state
	// in the same command decides which upstream counter is read.
	if linkMakeCurrent {
		currentLink.UpstreamLastMergedRevisionNum, currentLink.DownstreamLastMergedRevisionNum =
			makeCurrentPointers(fromUnit, toUnit, currentLink.UseLiveState)
	}

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
