// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
)

var linkCreateArgs struct {
	destSpaces   []string
	whereSpace   string
	whereFrom    string
	whereTo      string
	whereToSpace string
}

var linkCreateCmd = &cobra.Command{
	Use:   "create [<link slug> <from unit slug> <to unit slug> [<to space slug>]]",
	Short: "Create a new link or bulk create links",
	Long: `Create a new link between two units or bulk create multiple links based on filters.

SINGLE LINK CREATION:
Create a single link between two units. Links define relationships between units and can be used to establish dependencies or connections between resources.

A link can be created:
  1. Between units in the same space
  2. Between units across different spaces (by specifying the target space)

BULK LINK CREATION:
When no positional arguments are provided, bulk create mode is activated. This mode creates
links between units matching the filters specified.

Single Link Examples:
  # Create a link between a deployment and its namespace in the same space
  cub link create --space my-space --json to-ns my-deployment my-ns --wait

  # Create a link for a complex application to its namespace
  cub link create --space my-space --json headlamp-to-ns headlamp my-ns --wait

  # Create a link between a cloned unit and a namespace
  cub link create --space my-space --json clone-to-ns my-clone my-ns --wait

Bulk Create Examples:
  # Create links between all deployments and a namespace in a space
  cub link create --where-space "Slug = 'my-space'" --where-from "Labels.type = 'deployment'" --where-to "Slug = 'my-ns'"

  # Create links between units across different spaces
  cub link create --dest-space dev-space,staging-space --where-from "Labels.app = 'frontend'" --where-to "Labels.app = 'backend'" --where-to-space "Slug = 'services-space'"

  # Create links with custom labels via JSON patch
  echo '{"Labels": {"relationship": "dependency"}}' | cub link create --where-space "Slug LIKE 'app-%'" --where-from "Labels.tier = 'web'" --where-to "Labels.tier = 'db'" --from-stdin`,
	Args:        cobra.MaximumNArgs(4),
	RunE:        linkCreateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func init() {
	addStandardCreateFlags(linkCreateCmd)
	enableWaitFlag(linkCreateCmd)

	// Bulk create specific flags
	linkCreateCmd.Flags().StringSliceVar(&linkCreateArgs.destSpaces, "dest-space", []string{}, "destination spaces for bulk create (can be repeated or comma-separated)")
	linkCreateCmd.Flags().StringVar(&linkCreateArgs.whereSpace, "where-space", "", "where expression to select spaces for bulk create")
	linkCreateCmd.Flags().StringVar(&linkCreateArgs.whereFrom, "where-from", "", "where expression to select from units within each space (required in bulk mode)")
	linkCreateCmd.Flags().StringVar(&linkCreateArgs.whereTo, "where-to", "", "where expression to select to units within each space (required in bulk mode)")
	linkCreateCmd.Flags().StringVar(&linkCreateArgs.whereToSpace, "where-to-space", "", "where expression to select to spaces for bulk create (optional)")

	linkCmd.AddCommand(linkCreateCmd)
}

func checkLinkCreateConflictingArgs(args []string) (bool, error) {
	// Determine if bulk create mode: no positional args and has bulk-specific flags
	isBulkCreateMode := len(args) == 0

	if isBulkCreateMode {
		// Validate bulk create requirements
		if linkCreateArgs.whereFrom == "" || linkCreateArgs.whereTo == "" {
			return false, errors.New("bulk create mode requires --where-from and --where-to flags")
		}

		if linkCreateArgs.whereSpace == "" && len(linkCreateArgs.destSpaces) == 0 {
			return false, errors.New("bulk create mode requires either --where-space or --dest-space flag")
		}

		if linkCreateArgs.whereSpace != "" && len(linkCreateArgs.destSpaces) > 0 {
			return false, errors.New("--where-space and --dest-space flags are mutually exclusive")
		}
	} else {
		// Single create mode validation
		if len(args) < 3 {
			return false, errors.New("single link creation requires: <slug> <from unit> <to unit> [to space]")
		}

		if linkCreateArgs.whereFrom != "" || linkCreateArgs.whereTo != "" || linkCreateArgs.whereSpace != "" ||
			linkCreateArgs.whereToSpace != "" || len(linkCreateArgs.destSpaces) > 0 {
			return false, errors.New("bulk create flags (--where-from, --where-to, --where-space, --where-to-space, --dest-space) can only be used without positional arguments")
		}
	}

	if err := validateSpaceFlag(isBulkCreateMode); err != nil {
		failOnError(err)
	}

	if err := validateStdinFlags(); err != nil {
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
		return runBulkLinkCreate()
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
	err := setLabels(&newLink.Labels)
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

	newLink.FromUnitID = fromUnitID
	newLink.ToUnitID = toUnitID
	newLink.ToSpaceID = uuid.MustParse(toSpaceID)

	linkRes, err := cubClientNew.CreateLinkWithResponse(ctx, uuid.MustParse(selectedSpaceID), *newLink)
	if IsAPIError(err, linkRes) {
		return InterpretErrorGeneric(err, linkRes)
	}
	linkDetails := linkRes.JSON200
	displayCreateResults(linkDetails, "link", linkDetails.Slug, linkDetails.LinkID.String(), displayLinkDetails)
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
	return err
}

func runBulkLinkCreate() error {
	// Build patch data using consolidated function (no entity-specific fields for link in bulk create)
	patchJSON, err := BuildPatchData(nil)
	if err != nil {
		return err
	}

	// Build bulk create parameters
	params := &goclientnew.BulkCreateLinksParams{
		WhereFrom: &linkCreateArgs.whereFrom,
		WhereTo:   &linkCreateArgs.whereTo,
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
	params.WhereSpace = &whereSpaceExpr

	// Set where_to_space if specified
	if linkCreateArgs.whereToSpace != "" {
		params.WhereToSpace = &linkCreateArgs.whereToSpace
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
