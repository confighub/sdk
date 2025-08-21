// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"time"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitUpdateCmd = &cobra.Command{
	Use:         "update <slug or id> [config-file]",
	Short:       "Update a unit",
	Long:        getUnitUpdateHelp(),
	Args:        cobra.RangeArgs(0, 2), // Allow 0 args for bulk mode
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        unitUpdateCmdRun,
}

func getUnitUpdateHelp() string {
	baseHelp := `Update an existing unit in a space. Units can be updated with new configuration data, restored to previous revisions, or upgraded from upstream units.

Like other ConfigHub entities, Units have metadata, which can be partly set on the command line
and otherwise read from stdin using the flag --from-stdin or --replace-from-stdin.

Unit configuration data can be provided in multiple ways:
  1. From a local or remote configuration file, or from stdin (by specifying "-")
  2. By restoring to a previous revision (using --restore)
  3. By upgrading from the upstream unit (using --upgrade)

Examples:
  # Update a unit from a local YAML file
  cub unit update --space my-space myunit config.yaml

  # Update a unit from a file:// URL
  cub unit update --space my-space myunit file:///path/to/config.yaml

  # Update a unit from a remote HTTPS URL
  cub unit update --space my-space myunit https://example.com/config.yaml

  # Update a unit with config from stdin
  cub unit update --space my-space myunit -

  # Combine Unit JSON metadata from stdin with config data from file
  cub unit update --space my-space myunit config.yaml --from-stdin

  # Restore a unit to revision 5
  cub unit update --space my-space myunit --restore 5

  # Restore a unit to 2 revisions ago (relative to head)
  cub unit update --space my-space myunit --restore -2

  # Restore a unit using a specific revision ID
  cub unit update --space my-space myunit --restore 550e8400-e29b-41d4-a716-446655440000

  # Restore a unit to the live revision
  cub unit update --space my-space myunit --restore LiveRevisionNum

  # Restore a unit to the last applied revision
  cub unit update --space my-space myunit --restore LastAppliedRevisionNum

  # Upgrade a unit to match its upstream unit
  cub unit update --space my-space myunit --upgrade

  # Update with a change description
  cub unit update --space my-space myunit config.yaml --change-desc "Updated database configuration"

Patch Mode Examples:
  # Individual patch with labels
  cub unit update --patch --space my-space myunit --label version=1.2

  # Patch with data from file plus metadata changes
  cub unit update --patch --space my-space myunit --filename patch.json --change-desc "Updated annotations" --label patched=true

  # Bulk patch with change description and labels
  cub unit update --patch --where "Slug LIKE 'app-%'" --change-desc "Metadata review" --label reviewed=2024-01

  # Bulk patch across all spaces with metadata
  cub unit update --patch --space "*" --where "UpstreamRevisionNum > 0" --change-desc "Upgrade all" --upgrade

  # Bulk restore with change description
  cub unit update --patch --where "Slug IN ('unit1', 'unit2')" --restore LiveRevisionNum --change-desc "Restored to live revision"

  # Bulk patch with data from stdin plus metadata (just an example; use cub unit set-target for this case)
  echo '{"TargetID": null}' | cub unit update --patch --unit unit1,unit2,unit3 --from-stdin --change-desc "Cleared targets"`

	agentContext := `Essential for maintaining and evolving configuration in ConfigHub.

Agent update workflow:
1. Identify the unit to update by slug or ID
2. Choose update method: new config, restore, or upgrade
3. Update unit and wait for triggers to complete validation
4. Check for any validation issues or apply gates

Update methods:

From local file:
  cub unit update --space SPACE my-unit config.yaml

From stdin (useful for programmatic updates):
  cat config.yaml | cub unit update --space SPACE my-unit -

Restore to previous revision:
  cub unit update --space SPACE my-unit --restore 3

Restore using revision ID or special values:
  cub unit update --space SPACE my-unit --restore 550e8400-e29b-41d4-a716-446655440000
  cub unit update --space SPACE my-unit --restore LiveRevisionNum

Upgrade from upstream:
  cub unit update --space SPACE my-unit --upgrade

Bulk patch operations:
  cub unit update --patch --where "Slug LIKE 'app-%'" --restore LiveRevisionNum --change-desc "Restored apps"
  cub unit update --patch --space "*" --where "Labels.tier = 'platform'" --label updated=true --change-desc "Updated platform units"

Key flags for agents:
- --wait: Wait for triggers and validation to complete (recommended)
- --json: Get structured response with unit ID and details
- --verbose: Show detailed update information
- --from-stdin: Read additional metadata from stdin
- --replace-from-stdin: Replace entire metadata from stdin
- --restore: Restore to a revision using: revision number (positive/negative), revision ID (UUID), or special values (LiveRevisionNum/LastAppliedRevisionNum/PreviousLiveRevisionNum)
- --upgrade: Upgrade to match the latest version of upstream unit
- --change-desc: Add a description for this change
- --label: Update labels for organization and filtering
- --patch: Use patch API for individual or bulk operations (enables --where and --unit flags for bulk mode)
- --where: Filter units for bulk patch operations (requires --patch with no unit argument)
- --unit: Target specific units by slug/UUID for bulk patch operations (requires --patch with no unit argument)

Post-update workflow:
1. Use 'function do get-placeholders' to check for placeholder values
2. Use 'function do' commands to modify configuration as needed
3. Use 'unit approve' if approval is required
4. Use 'unit apply' to deploy to live infrastructure

Important: Only one of config-file, --restore, or --upgrade should be specified per update operation.`

	return getCommandHelp(baseHelp, agentContext)
}

var (
	changeDescription string
	restore           string
	isUpgrade         bool
	isPatch           bool
)

func init() {
	addStandardUpdateFlags(unitUpdateCmd)
	unitUpdateCmd.Flags().StringVar(&changeDescription, "change-desc", "", "change description")
	unitUpdateCmd.Flags().StringVar(&restore, "restore", "", "restore to a revision: UUID (revision ID), integer (revision number), or one of LiveRevisionNum/LastAppliedRevisionNum/PreviousLiveRevisionNum")
	unitUpdateCmd.Flags().BoolVar(&isUpgrade, "upgrade", false, "upgrade the unit to the latest version of its upstream unit")
	unitUpdateCmd.Flags().BoolVar(&isPatch, "patch", false, "use patch API instead of update API")
	enableWhereFlag(unitUpdateCmd)
	unitUpdateCmd.Flags().StringSliceVar(&unitIdentifiers, "unit", []string{}, "target specific units by slug or UUID (can be repeated or comma-separated)")
	enableWaitFlag(unitUpdateCmd)
	unitCmd.AddCommand(unitUpdateCmd)
}

// TODO: Add a --target flag, similar to cub unit create

// addSpaceIDToWhereClause adds space constraint to where clause, for reuse across commands
func addSpaceIDToWhereClause(whereClause, spaceID string) string {
	if spaceID == "*" {
		return whereClause
	}
	spaceConstraint := fmt.Sprintf("SpaceID = '%s'", spaceID)
	if whereClause != "" {
		return fmt.Sprintf("%s AND %s", whereClause, spaceConstraint)
	}
	return spaceConstraint
}

var restoreValues = map[string]struct{}{
	"LiveRevisionNum":         struct{}{},
	"LastAppliedRevisionNum":  struct{}{},
	"PreviousLiveRevisionNum": struct{}{},
}

func checkConflictingArgs(args []string) bool {
	// Check for bulk patch mode (no positional args with --patch)
	isBulkPatchMode := isPatch && len(args) == 0

	// Validate label removal only works with patch
	if err := ValidateLabelRemoval(label, isPatch); err != nil {
		failOnError(err)
	}

	if !isBulkPatchMode && (where != "" || len(unitIdentifiers) > 0) {
		failOnError(fmt.Errorf("--where or --unit can only be specified with --patch and no unit positional argument"))
	}

	// Check for mutual exclusivity between --unit and --where flags
	if len(unitIdentifiers) > 0 && where != "" {
		failOnError(fmt.Errorf("--unit and --where flags are mutually exclusive"))
	}

	if restore != "" && isUpgrade {
		failOnError(fmt.Errorf("only one of --restore and --upgrade should be specified"))
	}

	dataFromEntity := restore != "" || isUpgrade
	if dataFromEntity && len(args) > 1 {
		failOnError(fmt.Errorf("only one of --restore, --upgrade, or config-file should be specified"))
	}

	if isPatch && flagReplace {
		failOnError(fmt.Errorf("only one of --patch and --replace should be specified"))
	}

	if isPatch && !isBulkPatchMode && !flagPopulateModelFromStdin && flagFilename == "" && restore == "" && !isUpgrade && len(label) == 0 {
		failOnError(fmt.Errorf("--patch requires one of: --from-stdin, --filename, --restore, --upgrade, or --label"))
	}

	if isBulkPatchMode && restore != "" {
		// In bulk mode, restore parameter can't be UUID or integer (only special strings)
		if _, isValid := restoreValues[restore]; !isValid {
			failOnError(fmt.Errorf("bulk patch mode doesn't support revision UUID or number restore values, only unit revision fields like LiveRevisionNum"))
		}
	}

	if err := validateSpaceFlag(isBulkPatchMode); err != nil {
		failOnError(err)
	}

	if err := validateStdinFlags(); err != nil {
		failOnError(err)
	}

	return isBulkPatchMode
}

func unitUpdateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkPatchMode := checkConflictingArgs(args)

	if isBulkPatchMode {
		return runBulkUnitUpdate()
	}

	spaceID := uuid.MustParse(selectedSpaceID)
	currentUnit, err := apiGetUnitFromSlug(args[0], "*") // get all fields for RMW
	if err != nil {
		return err
	}

	newParams := &goclientnew.UpdateUnitParams{}

	// Prepare Unit metadata

	var patchData []byte
	if isPatch {
		// Create enhancer for unit-specific fields
		var enhancer PatchEnhancer
		if changeDescription != "" {
			enhancer = func(patchMap map[string]interface{}) {
				patchMap["LastChangeDescription"] = changeDescription
			}
		}
		// Build patch data using consolidated function. It reads from stdin/file and sets labels, if any.
		patchData, err = BuildPatchData(enhancer)
		if err != nil {
			return err
		}
	} else {
		// Handle --from-stdin or --filename with optional --replace
		if flagPopulateModelFromStdin || flagFilename != "" {
			existingUnit := currentUnit
			if flagReplace {
				// Replace mode - create new entity, allow Version to be overwritten
				currentUnit = new(goclientnew.Unit)
				currentUnit.Version = existingUnit.Version
			}

			if err := populateModelFromFlags(currentUnit); err != nil {
				return err
			}

			// Ensure essential fields can't be clobbered
			currentUnit.OrganizationID = existingUnit.OrganizationID
			currentUnit.SpaceID = existingUnit.SpaceID
			currentUnit.UnitID = existingUnit.UnitID

		}
		// For non-patch operations, handle labels in the traditional way
		err = setLabels(&currentUnit.Labels)
		if err != nil {
			return err
		}
		// For non-patch operations, handle change description in the traditional way
		if changeDescription != "" {
			currentUnit.LastChangeDescription = changeDescription
		}
	}

	// Prepare Unit Data. These 3 alternatives are ensured to be mutually exclusve by checkConflictingArgs above.

	if isUpgrade {
		newParams.Upgrade = &isUpgrade
	}

	if restore != "" {
		// Parse restore parameter - could be UUID (revision ID), int64 (revision number), or special string
		if revisionUUID, err := uuid.Parse(restore); err == nil {
			// It's a UUID - use as revision ID directly
			newParams.RevisionId = &revisionUUID
		} else if revisionNum, err := strconv.ParseInt(restore, 10, 64); err == nil {
			// It's an integer - treat as revision number
			if revisionNum < 0 {
				// A negative value means it's relative to head revision num
				revisionNum = int64(currentUnit.HeadRevisionNum) + revisionNum
			}
			rev, err := apiGetRevisionFromNumber(revisionNum, currentUnit.UnitID.String(), "*") // get all fields for now
			failOnError(err)
			// TODO: this should read RevisionID, but stays revision_id in the query parameter call
			newParams.RevisionId = &rev.RevisionID
		} else if _, isValid := restoreValues[restore]; isValid {
			// It's one of the special restore parameter values - use restore parameter instead of revision_id
			newParams.Restore = &restore
		} else {
			return fmt.Errorf("invalid restore value '%s': must be a UUID (revision ID), integer (revision number), or one of LiveRevisionNum/LastAppliedRevisionNum/PreviousLiveRevisionNum", restore)
		}
	}

	// Read data payload
	if len(args) > 1 {
		if args[1] == "-" && flagPopulateModelFromStdin {
			return errors.New("can't read both entity attributes and config data from stdin")
		}
		content, err := fetchContent(args[1])
		if err != nil {
			return fmt.Errorf("failed to read config: %w", err)
		}
		var base64Content strfmt.Base64 = content
		currentUnit.Data = base64Content.String()
	}

	// Perform the update

	var unitDetails *goclientnew.Unit
	if isPatch {
		unitDetails, err = patchUnit(spaceID, currentUnit.UnitID, newParams, patchData)
	} else {
		unitDetails, err = updateUnit(spaceID, currentUnit, newParams)
	}
	if err != nil {
		return err
	}

	// Wait for trigger+resolve completion

	if wait {
		err = awaitTriggersRemoval(unitDetails)
		if err != nil {
			return err
		}
	}

	// Display results

	displayUpdateResults(unitDetails, "unit", args[0], unitDetails.UnitID.String(), displayUnitDetails)
	return nil
}

func runBulkUnitUpdate() error {
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

	// Add space constraint to the where clause only if not org level
	if selectedSpaceID != "*" {
		effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)
	}

	// Create enhancer for unit-specific fields
	var enhancer PatchEnhancer
	if changeDescription != "" {
		enhancer = func(patchMap map[string]interface{}) {
			patchMap["LastChangeDescription"] = changeDescription
		}
	}

	// Build patch data using consolidated function
	patchData, err := BuildPatchData(enhancer)
	if err != nil {
		return err
	}

	// Build bulk patch parameters
	params := &goclientnew.BulkPatchUnitsParams{
		Where: &effectiveWhere,
	}

	// Set include parameter to expand UpstreamUnitID
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	params.Include = &include

	// Add restore parameter if specified
	if restore != "" {
		params.Restore = &restore
	}

	// Add upgrade parameter if specified
	if isUpgrade {
		params.Upgrade = &isUpgrade
	}

	// Call the bulk patch API (organization-level API that can be constrained by SpaceID in WHERE clause)
	bulkRes, err := cubClientNew.BulkPatchUnitsWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
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
		return fmt.Errorf("unexpected response from bulk patch API")
	}

	return handleBulkCreateOrUpdateResponse(responses, statusCode, "update", "")
}

func updateUnit(spaceID uuid.UUID, currentUnit *goclientnew.Unit, params *goclientnew.UpdateUnitParams) (*goclientnew.Unit, error) {
	updatedRes, err := cubClientNew.UpdateUnitWithResponse(ctx, spaceID, currentUnit.UnitID, params, *currentUnit)
	if IsAPIError(err, updatedRes) {
		return nil, InterpretErrorGeneric(err, updatedRes)
	}

	return updatedRes.JSON200, nil
}

func patchUnit(spaceID uuid.UUID, unitID uuid.UUID, updateParams *goclientnew.UpdateUnitParams, patchData []byte) (*goclientnew.Unit, error) {
	// Convert UpdateUnitParams to PatchUnitParams
	patchParams := &goclientnew.PatchUnitParams{}
	if updateParams.RevisionId != nil {
		patchParams.RevisionId = updateParams.RevisionId
	}
	if updateParams.Restore != nil {
		patchParams.Restore = updateParams.Restore
	}
	if updateParams.Upgrade != nil {
		patchParams.Upgrade = updateParams.Upgrade
	}

	unitRes, err := cubClientNew.PatchUnitWithBodyWithResponse(
		ctx,
		spaceID,
		unitID,
		patchParams,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if IsAPIError(err, unitRes) {
		return nil, InterpretErrorGeneric(err, unitRes)
	}

	return unitRes.JSON200, nil
}

func awaitTriggersRemoval(unitDetails *goclientnew.Unit) error {
	// TODO: Implement configurable timeout, similar to awaitCompletion
	var err error
	unitID := unitDetails.UnitID
	tries := 0
	numTries := 100
	ms := 25
	maxMs := 250
	done := false
	for tries < numTries {
		if unitDetails.ApplyGates == nil {
			done = true
			break
		}
		_, awaitingTriggers := unitDetails.ApplyGates["awaiting/triggers"]
		if !awaitingTriggers {
			done = true
			break
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
		ms *= 2
		if ms > maxMs {
			ms = maxMs
		}
		tries++
		unitDetails, err = apiGetUnitInSpace(unitID.String(), unitDetails.SpaceID.String(), "*") // get all fields for now
		if err != nil {
			return err
		}
	}
	if !done {
		return errors.New("triggers didn't execute on unit " + unitDetails.Slug)
	}
	return nil
}

func handleBulkCreateOrUpdateResponse(responses *[]goclientnew.UnitCreateOrUpdateResponse, statusCode int, operationName, contextInfo string) error {
	if responses == nil {
		return fmt.Errorf("no response data received")
	}

	successCount := 0
	errorCount := 0
	var successfulUnits []*goclientnew.ExtendedUnit
	var failedErrors []*goclientnew.ResponseError

	for _, resp := range *responses {
		if resp.Error != nil {
			errorCount++
			failedErrors = append(failedErrors, resp.Error)
		} else if resp.Unit != nil {
			successCount++
			// Convert Unit to ExtendedUnit for display
			extendedUnit := &goclientnew.ExtendedUnit{
				Unit: resp.Unit,
			}
			successfulUnits = append(successfulUnits, extendedUnit)
		}
	}

	// Check if any alternative output format is specified
	hasAlternativeOutput := jsonOutput || jq != ""

	// Wait for triggers to complete if requested. Do it before displaying the units with apply gates.
	if wait && successCount > 0 {
		if !quiet && !hasAlternativeOutput {
			tprintRaw("Awaiting triggers...")
		}
		// Wait for each successfully updated unit
		for i := range successfulUnits {
			// The units returned don't have the extended information, so we re-fetch them with that information.
			unitExtended, err := apiGetExtendedUnitInSpace(successfulUnits[i].Unit.UnitID.String(), successfulUnits[i].Unit.SpaceID.String(), "*") // get all fields for now
			if err != nil {
				return err
			}
			err = awaitTriggersRemoval(unitExtended.Unit)
			if err != nil {
				return err
			}
			// TODO: We should change awaitTriggersRemoval to return the latest state. This will show one unit with triggers.
			successfulUnits[i] = unitExtended
		}
	}

	// Summary message
	if !quiet && !hasAlternativeOutput {
		// Display successful units using standard display function.
		if verbose && len(successfulUnits) > 0 {
			displayListResults(successfulUnits, getExtendedUnitSlug, displayExtendedUnitList)
		}

		// Display failed units if any
		if len(failedErrors) > 0 {
			tprintRaw(fmt.Sprintf("Failed to %s units:", operationName))
			displayResponseErrorTable(failedErrors)
		}

		totalCount := len(*responses)
		if statusCode == 207 {
			tprint("Bulk %s completed with mixed results: %d succeeded, %d failed out of %d total units",
				operationName, successCount, errorCount, totalCount)
		} else if statusCode == 200 {
			if contextInfo != "" {
				tprint("Bulk %s completed successfully: %d units %s from %s",
					operationName, successCount, operationName, contextInfo)
			} else {
				tprint("Bulk %s completed successfully: %d units %s",
					operationName, successCount, operationName)
			}
		}
	}

	// Output JSON if requested
	if jsonOutput {
		displayJSON(responses)
	}
	if jq != "" {
		displayJQ(responses)
	}

	// Return error if all operations failed
	if errorCount > 0 && successCount == 0 {
		return fmt.Errorf("all bulk %s operations failed", operationName)
	}

	return nil
}
