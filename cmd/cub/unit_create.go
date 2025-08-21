// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/confighub/sdk/workerapi"
)

var unitCreateCmd = &cobra.Command{
	Use:         "create [<name> [config-file]]",
	Short:       "Create a unit or bulk create units",
	Long:        getUnitCreateHelp(),
	Args:        cobra.RangeArgs(0, 2), // Allow 0 args for bulk mode
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        unitCreateCmdRun,
}

func getUnitCreateHelp() string {
	baseHelp := `Create a new unit or bulk create multiple units by cloning existing ones.

SINGLE UNIT CREATION:
Like other ConfigHub entities, Units have metadata, which can be partly set on the command line
and otherwise read from stdin using the flag --from-stdin. 

Unlike other ConfigHub entities, Units also contain configuration data, which is read from another
source.

Unit configuration data can be provided in multiple ways:
  1. From a local or remote configuration file, or from stdin (by specifying "-")
  2. By cloning an existing upstream unit (using --upstream-unit)

BULK UNIT CREATION:
When no positional arguments are provided, bulk create mode is activated. This mode clones existing
units based on filters and creates multiple new units with optional modifications.

Single Unit Examples:
  # Create a unit from a local YAML file
  cub unit create --space my-space myunit config.yaml

  # Create a unit from a file:// URL
  cub unit create --space my-space myunit file:///path/to/config.yaml

  # Create a unit from a remote HTTPS URL
  cub unit create --space my-space myunit https://example.com/config.yaml

  # Create a unit with config from stdin
  cub unit create --space my-space myunit -

  # Combine Unit JSON metadata from stdin with config data from file
  cub unit create --space my-space myunit config.yaml --from-stdin

  # Clone an existing unit
  cub unit create --space my-space --json --from-stdin myclone --upstream-unit sample-deployment

Bulk Create Examples:
  # Clone all units matching a pattern with name prefixes
  cub unit create --where "Slug LIKE 'app-%'" --name-prefix dev-,staging- --dest-space dev-space

  # Clone specific units to multiple spaces
  cub unit create --unit app1,app2 --dest-space dev-space,test-space

  # Clone units using a where expression for destination spaces
  cub unit create --where "Slug LIKE 'app-%'" --where-space "Labels.Environment IN ('dev', 'staging')"

  # Clone units with target assignment and labels
  cub unit create --where "Labels.Tier = 'backend'" --name-prefix canary- --target my-target --label "Rollout=canary"

  # Clone units with JSON patch modifications
  echo '{"DisplayName": "Updated Name"}' | cub unit create --where "Slug = 'myapp'" --name-prefix v2- --from-stdin`

	agentContext := `Essential for adding new configuration to ConfigHub.

Agent creation workflow:
1. Prepare configuration files locally (YAML, HCL, properties, etc.)
2. Choose appropriate unit slug (used for referencing the unit)
3. Create unit and wait for triggers to complete validation
4. Check for any validation issues or apply gates

Creation methods:

From local file:
  cub unit create --space SPACE my-unit config.yaml --wait

From stdin (useful for programmatic creation):
  cat config.yaml | cub unit create --space SPACE my-unit - --wait

Clone existing unit:
  cub unit create --space SPACE my-variant --upstream-unit SOURCE_UNIT --upstream-space SOURCE_SPACE --from-stdin < metadata.json

Key flags for agents:
- --wait: Wait for triggers and validation to complete (recommended)
- --json: Get structured response with unit ID and details
- --verbose: Show detailed creation information
- --from-stdin: Read additional metadata from stdin (for cloning)
- --label: Add labels for organization and filtering

Post-creation workflow:
1. Use 'function do get-placeholders' to check for placeholder values
2. Use 'function do' commands to modify configuration as needed
3. Use 'unit approve' if approval is required
4. Use 'unit apply' to deploy to live infrastructure

Important: Unit slugs must be unique within a space and follow naming conventions (lowercase, hyphens allowed).`

	return getCommandHelp(baseHelp, agentContext)
}

var unitCreateArgs struct {
	upstreamUnitSlug  string
	upstreamSpaceSlug string
	importUnitSlug    string
	toolchainType     string
	targetSlug        string
	// Bulk create specific flags
	destSpaces   []string
	whereSpace   string
	namePrefixes []string
}

func init() {
	addStandardCreateFlags(unitCreateCmd) // This already includes verbose, json, jq flags
	enableWaitFlag(unitCreateCmd)
	enableWhereFlag(unitCreateCmd)

	// Single unit create flags
	unitCreateCmd.Flags().StringVar(&unitCreateArgs.targetSlug, "target", "", "target for the unit")
	unitCreateCmd.Flags().StringVar(&unitCreateArgs.upstreamUnitSlug, "upstream-unit", "", "upstream unit slug to clone (single mode only)")
	unitCreateCmd.Flags().StringVar(&unitCreateArgs.upstreamSpaceSlug, "upstream-space", "", "space slug of upstream unit to clone (single mode only)")
	unitCreateCmd.Flags().StringVar(&unitCreateArgs.importUnitSlug, "import", "", "source unit slug (single mode only)")
	// default to ToolchainKubernetesYAML
	unitCreateCmd.Flags().StringVarP(&unitCreateArgs.toolchainType, "toolchain", "t", string(workerapi.ToolchainKubernetesYAML), "toolchain type (single mode only)")

	// Bulk create specific flags
	unitCreateCmd.Flags().StringSliceVar(&unitCreateArgs.destSpaces, "dest-space", []string{}, "destination spaces for bulk create (can be repeated or comma-separated)")
	unitCreateCmd.Flags().StringVar(&unitCreateArgs.whereSpace, "where-space", "", "where expression to select destination spaces for bulk create")
	unitCreateCmd.Flags().StringSliceVar(&unitCreateArgs.namePrefixes, "name-prefix", []string{}, "name prefixes for bulk create (can be repeated or comma-separated)")
	unitCreateCmd.Flags().StringSliceVar(&unitIdentifiers, "unit", []string{}, "target specific units by slug or UUID for bulk create (can be repeated or comma-separated)")

	unitCmd.AddCommand(unitCreateCmd)
}

// buildWhereClauseForSpaces converts space identifiers to a where clause
func buildWhereClauseForSpaces(identifiers []string) (string, error) {
	return buildWhereClauseFromIdentifiers(identifiers, "SpaceID", "Slug")
}

func checkUnitCreateConflictingArgs(args []string) (bool, error) {
	// Determine if bulk create mode: no positional args and has bulk-specific flags
	isBulkCreateMode := len(args) == 0 && (where != "" || len(unitIdentifiers) > 0 || len(unitCreateArgs.destSpaces) > 0 || unitCreateArgs.whereSpace != "" || len(unitCreateArgs.namePrefixes) > 0)

	if isBulkCreateMode {
		// Validate bulk create requirements
		if where == "" && len(unitIdentifiers) == 0 {
			return false, errors.New("bulk create mode requires --where or --unit flags")
		}

		if len(unitIdentifiers) > 0 && where != "" {
			return false, errors.New("--unit and --where flags are mutually exclusive")
		}

		if len(unitCreateArgs.destSpaces) > 0 && unitCreateArgs.whereSpace != "" {
			return false, errors.New("--dest-space and --where-space flags are mutually exclusive")
		}

		if len(unitCreateArgs.destSpaces) == 0 && unitCreateArgs.whereSpace == "" && len(unitCreateArgs.namePrefixes) == 0 {
			return false, errors.New("bulk create mode requires at least one of --dest-space, --where-space, or --name-prefix")
		}

		// Validate single-mode-only flags are not used in bulk mode
		if unitCreateArgs.upstreamUnitSlug != "" || unitCreateArgs.upstreamSpaceSlug != "" ||
			unitCreateArgs.importUnitSlug != "" || unitCreateArgs.toolchainType != string(workerapi.ToolchainKubernetesYAML) {
			return false, errors.New("--upstream-unit, --upstream-space, --import, and --toolchain flags cannot be used in bulk create mode")
		}
	} else {
		// Single create mode validation
		if len(args) == 0 {
			return false, errors.New("unit name is required for single unit creation")
		}

		if where != "" || len(unitIdentifiers) > 0 || len(unitCreateArgs.destSpaces) > 0 || unitCreateArgs.whereSpace != "" || len(unitCreateArgs.namePrefixes) > 0 {
			return false, errors.New("bulk create flags (--where, --unit, --dest-space, --where-space, --name-prefix) can only be used without positional arguments")
		}

		// Validate conflicting options - if 2nd arg is "-" (stdin for config), can't also read metadata from stdin
		if len(args) > 1 && args[1] == "-" && flagPopulateModelFromStdin {
			return false, errors.New("can't read both entity attributes and config data from stdin")
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

func unitCreateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkCreateMode, err := checkUnitCreateConflictingArgs(args)
	if err != nil {
		return err
	}

	if isBulkCreateMode {
		return runBulkUnitCreate()
	}

	return runSingleUnitCreate(args)
}

func runSingleUnitCreate(args []string) error {
	spaceID := uuid.MustParse(selectedSpaceID)
	newUnit := &goclientnew.Unit{}
	newParams := &goclientnew.CreateUnitParams{}

	// Handle --from-stdin or --filename
	if flagPopulateModelFromStdin || flagFilename != "" {
		if err := populateModelFromFlags(newUnit); err != nil {
			return err
		}
	}

	// Handle config data from 2nd positional argument
	if len(args) > 1 {
		if unitCreateArgs.upstreamUnitSlug != "" {
			return errors.New("shouldn't specify both an upstream to clone and config data")
		}
		content, err := fetchContent(args[1])
		if err != nil {
			return fmt.Errorf("failed to read config: %w", err)
		}
		var base64Content strfmt.Base64 = content
		newUnit.Data = base64Content.String()
	}

	err := setLabels(&newUnit.Labels)
	if err != nil {
		return err
	}
	var upstreamSpaceID, upstreamUnitID uuid.UUID
	if unitCreateArgs.upstreamSpaceSlug != "" {
		upstreamSpace, err := apiGetSpaceFromSlug(unitCreateArgs.upstreamSpaceSlug, "*") // get all fields for now
		if err != nil {
			return err
		}
		upstreamSpaceID = upstreamSpace.SpaceID
	}
	if unitCreateArgs.upstreamUnitSlug != "" {
		if unitCreateArgs.upstreamSpaceSlug == "" {
			upstreamSpaceID = spaceID
		}
		upstreamUnit, err := apiGetUnitFromSlugInSpace(unitCreateArgs.upstreamUnitSlug, upstreamSpaceID.String(), "*") // get all fields for now
		if err != nil {
			return err
		}
		upstreamUnitID = upstreamUnit.UnitID
	}
	if unitCreateArgs.targetSlug != "" {
		target, err := apiGetTargetFromSlug(unitCreateArgs.targetSlug, selectedSpaceID, "*") // get all fields for now
		if err != nil {
			return err
		}
		newUnit.TargetID = &target.Target.TargetID
	}

	// If these were set from stdin, they will be overridden
	newUnit.SpaceID = spaceID
	newUnit.Slug = makeSlug(args[0])
	newUnit.ToolchainType = unitCreateArgs.toolchainType

	if unitCreateArgs.upstreamUnitSlug != "" {
		newParams.UpstreamSpaceId = &upstreamSpaceID
		newParams.UpstreamUnitId = &upstreamUnitID
	}

	unitRes, err := cubClientNew.CreateUnitWithResponse(ctx, spaceID, newParams, *newUnit)
	if IsAPIError(err, unitRes) {
		return InterpretErrorGeneric(err, unitRes)
	}

	unitDetails := unitRes.JSON200
	if wait {
		err = awaitTriggersRemoval(unitDetails)
		if err != nil {
			return err
		}
	}
	displayCreateResults(unitDetails, "unit", args[0], unitDetails.UnitID.String(), displayUnitDetails)
	return nil
}

// createBulkCreatePatch creates a JSON patch for bulk create operations
func createBulkCreatePatch() ([]byte, error) {
	// Create enhancer for unit-specific fields
	var enhancer PatchEnhancer = func(patchMap map[string]interface{}) {
		// Add target if specified
		if unitCreateArgs.targetSlug != "" {
			var targetID uuid.UUID
			if unitCreateArgs.targetSlug == "-" {
				targetID = uuid.Nil
			} else {
				exTarget, err := apiGetTargetFromSlug(unitCreateArgs.targetSlug, selectedSpaceID, "*") // get all fields for now
				if err != nil {
					// Can't return error from enhancer, so log it
					fmt.Fprintf(os.Stderr, "Failed to get target: %v\n", err)
					return
				}
				targetID = exTarget.Target.TargetID
			}
			patchMap["TargetID"] = targetID
		}

		// Add change description if specified
		if changeDescription != "" {
			patchMap["LastChangeDescription"] = changeDescription
		}
	}

	// Build patch data using consolidated function
	return BuildPatchData(enhancer)
}

func runBulkUnitCreate() error {
	// Build WHERE clause from unit identifiers if provided
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

	// Append space constraint to the where clause
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Create JSON patch for customizing cloned units
	patchJSON, err := createBulkCreatePatch()
	if err != nil {
		return err
	}

	// Build bulk create parameters
	params := &goclientnew.BulkCreateUnitsParams{
		Where: &effectiveWhere,
	}

	// Set include parameter to expand UpstreamUnitID
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	params.Include = &include

	// Set name prefixes parameter if specified
	if len(unitCreateArgs.namePrefixes) > 0 {
		namePrefixesStr := strings.Join(unitCreateArgs.namePrefixes, ",")
		params.NamePrefixes = &namePrefixesStr
	}

	// Set where_space parameter - either from direct where-space flag or converted from dest-space
	var whereSpaceExpr string
	if unitCreateArgs.whereSpace != "" {
		whereSpaceExpr = unitCreateArgs.whereSpace
	} else if len(unitCreateArgs.destSpaces) > 0 {
		// Convert dest-space identifiers to a where expression
		whereSpaceExpr, err = buildWhereClauseForSpaces(unitCreateArgs.destSpaces)
		if err != nil {
			return fmt.Errorf("error converting destination spaces to where expression: %w", err)
		}
	}

	if whereSpaceExpr != "" {
		params.WhereSpace = &whereSpaceExpr
	}

	// Call the bulk create API
	bulkRes, err := cubClientNew.BulkCreateUnitsWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if IsAPIError(err, bulkRes) {
		return InterpretErrorGeneric(err, bulkRes)
	}

	// Handle response based on status code
	var responses *[]goclientnew.UnitCreateOrUpdateResponse
	var statusCode int

	if bulkRes.JSON200 != nil {
		responses = bulkRes.JSON200
		statusCode = 200
	} else if bulkRes.JSON207 != nil {
		responses = bulkRes.JSON207
		statusCode = 207
	} else {
		return fmt.Errorf("unexpected response from bulk create API")
	}

	return handleBulkCreateOrUpdateResponse(responses, statusCode, "create", "")
}
