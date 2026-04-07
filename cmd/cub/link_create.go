// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

var linkCreateArgs struct {
	linkSlugs           []string
	reverse             bool
	fromDownstreamWhere string
	toDownstreamWhere   string
}

var linkCreateCmd = &cobra.Command{
	Use:   "create [<link slug> <from unit slug> <to unit slug> [<to space slug>]]",
	Short: "Create a new link or bulk create (copy) links",
	Long: getCommandHelp(`Create a new link between two units or bulk create links by copying existing links.

SINGLE LINK CREATION:

Create a single link between two units. Links define relationships between units and can be used to establish dependencies or connections between resources.

A link can be created:

  1. Between units in the same space
  2. Between units across different spaces (by specifying the target space)

BULK LINK CREATION (COPY):

When no positional arguments are provided with --where or --filter plus at least --reverse or
--from-downstream-where, bulk create mode is activated. This mode copies existing links and
retargets their From and/or To units using downstream UpgradeUnit links.

Single Link Examples:
`+"```"+`
  # Create a link between a deployment and its namespace in the same space
  cub link create --space my-space --json to-ns my-deployment my-ns --wait

  # Create a link for a complex application to its namespace
  cub link create --space my-space --json headlamp-to-ns headlamp my-ns --wait

  # Create a link between a cloned unit and a namespace
  cub link create --space my-space --json clone-to-ns my-clone my-ns --wait
`+"```"+`

Bulk Create (Copy) Examples:
`+"```"+`
  # Copy outgoing links from prod-v1 units to their downstream copies in prod-v2
  cub link create --where "Space.Slug = 'prod-v1' AND ToSpaceID != SpaceID AND UpdateType != 'UpgradeUnit'" \
    --from-downstream-where "UpdateType = 'UpgradeUnit' AND Space.Slug = 'prod-v2'"

  # Also retarget the To units to their downstream copies
  cub link create --where "Space.Slug = 'prod-v1' AND ToSpaceID != SpaceID" \
    --from-downstream-where "UpdateType = 'UpgradeUnit' AND Space.Slug = 'prod-v2'" \
    --to-downstream-where "UpdateType = 'UpgradeUnit' AND Space.Slug = 'prod-v2'"

  # Reverse cross-space UpgradeUnit links (create the link in the To unit's space)
  cub link create --where "Space.Slug = 'prod-v2' AND UpdateType = 'UpgradeUnit'" --reverse
`+"```"+`
`, ""),
	Args:        cobra.MaximumNArgs(4),
	RunE:        linkCreateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func init() {
	addStandardCreateFlags(linkCreateCmd)
	enableWaitFlag(linkCreateCmd)
	addLinkFieldFlags(linkCreateCmd)

	// Bulk create specific flags
	enableWhereFlag(linkCreateCmd)
	enableFilterFlag(linkCreateCmd)
	linkCreateCmd.Flags().StringSliceVar(&linkCreateArgs.linkSlugs, "link", []string{}, "target specific links by slug or UUID for bulk create (can be repeated or comma-separated)")
	linkCreateCmd.Flags().BoolVar(&linkCreateArgs.reverse, "reverse", false, "swap FromUnit and ToUnit directions of copied links (for cross-space link reversal)")
	linkCreateCmd.Flags().StringVar(&linkCreateArgs.fromDownstreamWhere, "from-downstream-where", "", "where expression to find downstream UpgradeUnit links from each source link's FromUnit; creates one copy per match")
	linkCreateCmd.Flags().StringVar(&linkCreateArgs.toDownstreamWhere, "to-downstream-where", "", "where expression to find downstream UpgradeUnit link from each source link's ToUnit; exactly one match required")

	linkCmd.AddCommand(linkCreateCmd)
}

func checkLinkCreateConflictingArgs(args []string) (bool, error) {
	// Determine if bulk create mode: no positional args and has bulk-specific flags
	hasBulkFlags := where != "" || filter != "" || len(linkCreateArgs.linkSlugs) > 0 || linkCreateArgs.reverse || linkCreateArgs.fromDownstreamWhere != "" || linkCreateArgs.toDownstreamWhere != ""
	isBulkCreateMode := len(args) == 0 && hasBulkFlags

	if isBulkCreateMode {
		// Validate bulk create requirements
		if len(linkCreateArgs.linkSlugs) > 0 && (where != "" || filter != "") {
			return false, errors.New("--link and --where/--filter flags are mutually exclusive")
		}

		if !linkCreateArgs.reverse && linkCreateArgs.fromDownstreamWhere == "" {
			return false, errors.New("bulk create mode requires either --reverse or --from-downstream-where")
		}
	} else if len(args) > 0 {
		// Single create mode validation
		if len(args) < 3 || len(args) > 4 {
			return false, errors.New("single link creation requires: <slug> <from unit> <to unit> [to space]")
		}

		if linkCreateArgs.reverse || linkCreateArgs.fromDownstreamWhere != "" || linkCreateArgs.toDownstreamWhere != "" {
			return false, errors.New("bulk create flags (--reverse, --from-downstream-where, --to-downstream-where) can only be used without positional arguments")
		}
	} else {
		// No args and no bulk flags
		return false, errors.New("provide positional arguments for single create, or --where/--filter with --reverse/--from-downstream-where for bulk create")
	}

	if err := validateLinkFieldFlags(); err != nil {
		return isBulkCreateMode, err
	}

	if err := validateSpaceFlag(isBulkCreateMode); err != nil {
		return isBulkCreateMode, err
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

func linkCreateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkCreateMode, err := checkLinkCreateConflictingArgs(args)
	if err != nil {
		return err
	}

	if isBulkCreateMode {
		return runBulkLinkCreate(cmd)
	}

	return runSingleLinkCreate(args)
}

func runSingleLinkCreate(args []string) error {
	newLink := &goclientnew.Link{}
	if flagPopulateModelFromStdin || flagFilename != "" {
		if err := populateModelFromFlags(newLink); err != nil {
			return err
		}
	}
	err := setAnnotations(&newLink.Annotations)
	if err != nil {
		return err
	}
	err = setLabels(&newLink.Labels)
	if err != nil {
		return err
	}
	err = setDeleteGates(&newLink.DeleteGates)
	if err != nil {
		return err
	}
	newLink.SpaceID = uuid.MustParse(selectedSpaceID)
	if args[0] == "-" {
		// Allow the slug to be autogenerated by the server
		newLink.Slug = ""
	} else {
		newLink.Slug = makeSlug(args[0])
		if newLink.DisplayName == "" {
			newLink.DisplayName = args[0]
		}
	}

	fromUnit, err := apiGetUnitFromSlugInSpace(args[1], selectedSpaceID, "*") // get all fields for now
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

	newLink.FromUnitID = fromUnitID
	newLink.ToUnitID = toUnitID
	newLink.ToSpaceID = uuid.MustParse(toSpaceID)
	setLinkFieldsOnCreate(newLink)

	// If --make-current is set, initialize revision numbers to current unit revisions
	if linkMakeCurrent {
		if newLink.UseLiveState {
			// When UseLiveState is true, UpstreamLastMergedRevisionNum stores a UnitActionNum
			newLink.UpstreamLastMergedRevisionNum = toUnit.HeadUnitActionNum
		} else {
			newLink.UpstreamLastMergedRevisionNum = toUnit.HeadRevisionNum
		}
		newLink.DownstreamLastMergedRevisionNum = fromUnit.HeadRevisionNum
	}

	// Create params with AllowExists if needed
	params := &goclientnew.CreateLinkParams{}
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}

	linkRes, err := cubClientNew.CreateLinkWithResponse(ctx, uuid.MustParse(selectedSpaceID), params, *newLink)
	if cubapi.IsAPIError(err, linkRes) {
		return cubapi.InterpretErrorGeneric(err, linkRes)
	}
	linkDetails := linkRes.JSON200
	displayCreateResults(linkDetails, "link", linkDetails.Slug, linkDetails.LinkID.String(), displayLinkDetails)
	if wait {
		if !quiet {
			tprint("Awaiting triggers...")
		}
		unitDetails, err := apiGetUnitInSpace(fromUnitID.String(), selectedSpaceID, "*") // get all fields for now
		if err != nil {
			return err
		}
		err = awaitTriggersRemoval(unitDetails)
		if err != nil {
			return err
		}
	}
	return err
}

func runBulkLinkCreate(cmd *cobra.Command) error {
	// Build WHERE clause from view identifiers or use provided where clause
	var effectiveWhere string
	if len(linkCreateArgs.linkSlugs) > 0 {
		whereClause, err := buildWhereClauseFromLinks(linkCreateArgs.linkSlugs)
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

	// Build bulk create parameters
	params := &goclientnew.BulkCreateLinksParams{}

	// Set allow_exists parameter if flag is set
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}

	// Set where/filter parameters for selecting source links
	if effectiveWhere != "" {
		params.Where = &effectiveWhere
	}
	if filter != "" {
		filterID, parseErr := parseFilterFlag(filter)
		if parseErr != nil {
			return errors.Wrapf(parseErr, "error parsing filter")
		}
		params.Filter = &filterID
	}

	// Set retargeting parameters
	if linkCreateArgs.reverse {
		reverseVal := true
		params.Reverse = &reverseVal
	}
	if linkCreateArgs.fromDownstreamWhere != "" {
		params.FromDownstreamWhere = &linkCreateArgs.fromDownstreamWhere
	}
	if linkCreateArgs.toDownstreamWhere != "" {
		params.ToDownstreamWhere = &linkCreateArgs.toDownstreamWhere
	}

	// Call the bulk create API
	bulkRes, err := cubClientNew.BulkCreateLinksWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	// Handle the response
	return handleBulkLinkUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "create",
		fmt.Sprintf("where: %s, reverse: %v, from_downstream_where: %s", where, linkCreateArgs.reverse, linkCreateArgs.fromDownstreamWhere))
}
