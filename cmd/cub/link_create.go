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
	destSpaces    []string
	whereSpace    string
	whereFrom     string
	whereTo       string
	whereToSpace  string
	filterSpace   string
	filterFrom    string
	filterTo      string
	filterToSpace string
}

var linkCreateCmd = &cobra.Command{
	Use:   "create [<link slug> <from unit slug> <to unit slug> [<to space slug>]]",
	Short: "Create a new link or bulk create links",
	Long: getCommandHelp(`Create a new link between two units or bulk create multiple links based on filters.

SINGLE LINK CREATION:

Create a single link between two units. Links define relationships between units and can be used to establish dependencies or connections between resources.

A link can be created:

  1. Between units in the same space
  2. Between units across different spaces (by specifying the target space)

BULK LINK CREATION:

When no positional arguments are provided, bulk create mode is activated. This mode creates
links between units matching the filters specified.

Single Link Examples:
`+"```"+`
  # Create a link between a deployment and its namespace in the same space
  cub link create --space my-space --json to-ns my-deployment my-ns --wait

  # Create a link for a complex application to its namespace
  cub link create --space my-space --json headlamp-to-ns headlamp my-ns --wait

  # Create a link between a cloned unit and a namespace
  cub link create --space my-space --json clone-to-ns my-clone my-ns --wait
`+"```"+`

Bulk Create Examples:
`+"```"+`
  # Create links between all deployments and a namespace in a space
  cub link create --where-space "Slug = 'my-space'" --where-from "Labels.type = 'deployment'" --where-to "Slug = 'my-ns'"

  # Create links using filter entities to select spaces and units
  cub link create --filter-space deployment-spaces --filter-from frontend-units --filter-to backend-units

  # Combine where and filter expressions for complex selections
  cub link create --where-space "Labels.env = 'prod'" --filter-from prod-deployments --where-to "Slug LIKE 'ns-%'"

  # Create links between units across different spaces
  cub link create --dest-space dev-space,staging-space --where-from "Labels.app = 'frontend'" --where-to "Labels.app = 'backend'" --where-to-space "Slug = 'services-space'"

  # Create links with custom labels via JSON patch
  echo '{"Labels": {"relationship": "dependency"}}' | cub link create --where-space "Slug LIKE 'app-%'" --where-from "Labels.tier = 'web'" --where-to "Labels.tier = 'db'" --from-stdin
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
	linkCreateCmd.Flags().StringSliceVar(&linkCreateArgs.destSpaces, "dest-space", []string{}, "destination spaces for bulk create (can be repeated or comma-separated)")
	linkCreateCmd.Flags().StringVar(&linkCreateArgs.whereSpace, "where-space", "", "where expression to select spaces for bulk create")
	linkCreateCmd.Flags().StringVar(&linkCreateArgs.whereFrom, "where-from", "", "where expression to select from units within each space")
	linkCreateCmd.Flags().StringVar(&linkCreateArgs.whereTo, "where-to", "", "where expression to select to units within each space")
	linkCreateCmd.Flags().StringVar(&linkCreateArgs.whereToSpace, "where-to-space", "", "where expression to select to spaces for bulk create (optional)")
	linkCreateCmd.Flags().StringVar(&linkCreateArgs.filterSpace, "filter-space", "", "filter entity containing WHERE expression to select spaces for bulk create (slug or UUID)")
	linkCreateCmd.Flags().StringVar(&linkCreateArgs.filterFrom, "filter-from", "", "filter entity containing WHERE expression to select from units (slug or UUID)")
	linkCreateCmd.Flags().StringVar(&linkCreateArgs.filterTo, "filter-to", "", "filter entity containing WHERE expression to select to units (slug or UUID)")
	linkCreateCmd.Flags().StringVar(&linkCreateArgs.filterToSpace, "filter-to-space", "", "filter entity containing WHERE expression to select to spaces (slug or UUID)")

	linkCmd.AddCommand(linkCreateCmd)
}

func checkLinkCreateConflictingArgs(args []string) (bool, error) {
	// Determine if bulk create mode: no positional args and has bulk-specific flags
	isBulkCreateMode := len(args) == 0

	if isBulkCreateMode {
		// Validate bulk create requirements - require at least one from, to, and space selection
		if linkCreateArgs.whereFrom == "" && linkCreateArgs.filterFrom == "" {
			return false, errors.New("bulk create mode requires --where-from and/or --filter-from flags")
		}

		if linkCreateArgs.whereTo == "" && linkCreateArgs.filterTo == "" {
			return false, errors.New("bulk create mode requires --where-to and/or --filter-to flags")
		}

		if linkCreateArgs.whereSpace == "" && linkCreateArgs.filterSpace == "" && len(linkCreateArgs.destSpaces) == 0 {
			return false, errors.New("bulk create mode requires at least one of --where-space, --filter-space, or --dest-space flags")
		}

		if linkCreateArgs.whereSpace != "" && len(linkCreateArgs.destSpaces) > 0 {
			return false, errors.New("--where-space and --dest-space flags are mutually exclusive")
		}

	} else {
		// Single create mode validation
		if len(args) < 3 || len(args) > 4 {
			return false, errors.New("single link creation requires: <slug> <from unit> <to unit> [to space]")
		}

		if linkCreateArgs.whereFrom != "" || linkCreateArgs.whereTo != "" || linkCreateArgs.whereSpace != "" ||
			linkCreateArgs.whereToSpace != "" || len(linkCreateArgs.destSpaces) > 0 ||
			linkCreateArgs.filterFrom != "" || linkCreateArgs.filterTo != "" || linkCreateArgs.filterSpace != "" ||
			linkCreateArgs.filterToSpace != "" {
			return false, errors.New("bulk create flags (--where-from, --where-to, --where-space, --where-to-space, --dest-space, --filter-from, --filter-to, --filter-space, --filter-to-space) can only be used without positional arguments")
		}
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

	// Set where parameters if specified
	if linkCreateArgs.whereFrom != "" {
		params.WhereFrom = &linkCreateArgs.whereFrom
	}
	if linkCreateArgs.whereTo != "" {
		params.WhereTo = &linkCreateArgs.whereTo
	}
	if linkCreateArgs.whereToSpace != "" {
		params.WhereToSpace = &linkCreateArgs.whereToSpace
	}

	// Set where_space parameter - either from direct where-space flag or converted from dest-space
	var whereSpaceExpr string
	if linkCreateArgs.whereSpace != "" {
		whereSpaceExpr = linkCreateArgs.whereSpace
	} else if len(linkCreateArgs.destSpaces) > 0 {
		// Convert dest-space identifiers to a where expression
		whereSpaceExpr, err = buildWhereClauseForSpaces(linkCreateArgs.destSpaces)
		if err != nil {
			return errors.Wrapf(err, "error converting destination spaces to where expression")
		}
	}
	if whereSpaceExpr != "" {
		params.WhereSpace = &whereSpaceExpr
	}

	// Parse and set filter parameters if specified
	if linkCreateArgs.filterSpace != "" {
		filterSpaceID, err := parseFilterFlag(linkCreateArgs.filterSpace)
		if err != nil {
			return errors.Wrapf(err, "error parsing filter-space")
		}
		params.FilterSpace = &filterSpaceID
	}
	if linkCreateArgs.filterFrom != "" {
		filterFromID, err := parseFilterFlag(linkCreateArgs.filterFrom)
		if err != nil {
			return errors.Wrapf(err, "error parsing filter-from")
		}
		params.FilterFrom = &filterFromID
	}
	if linkCreateArgs.filterTo != "" {
		filterToID, err := parseFilterFlag(linkCreateArgs.filterTo)
		if err != nil {
			return errors.Wrapf(err, "error parsing filter-to")
		}
		params.FilterTo = &filterToID
	}
	if linkCreateArgs.filterToSpace != "" {
		filterToSpaceID, err := parseFilterFlag(linkCreateArgs.filterToSpace)
		if err != nil {
			return errors.Wrapf(err, "error parsing filter-to-space")
		}
		params.FilterToSpace = &filterToSpaceID
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

	// Handle the response using the existing handler from link_update.go
	return handleBulkLinkUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "create",
		fmt.Sprintf("where_from: %s, where_to: %s", linkCreateArgs.whereFrom, linkCreateArgs.whereTo))
}
