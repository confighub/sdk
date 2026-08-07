// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
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
  4. By performing a 3-way merge with another unit (using --merge-source, --merge-base, --merge-end)

Examples:
` + "```" + `
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

  # Restore a unit to a tagged revision (supports space/tag syntax)
  cub unit update --space my-space myunit --restore Tag:release-v1.0
  cub unit update --space my-space myunit --restore Tag:production/hotfix-patch

  # Restore a unit to the end of a changeset (supports space/changeset syntax)
  cub unit update --space my-space myunit --restore ChangeSet:feature-rollout
  cub unit update --space my-space myunit --restore ChangeSet:dev-space/bug-fixes

  # Upgrade a unit to match its upstream unit
  cub unit update --space my-space myunit --upgrade

  # Upgrade only as far as a point in the upstream's history, leaving later changes behind
  cub unit update --space my-space myunit --upgrade --merge-end ChangeSet:upstream-space/release-42
  cub unit update --space my-space myunit --upgrade --merge-end Tag:v1.0

  # Update with a change description
  cub unit update --space my-space myunit config.yaml --change-desc "Updated database configuration"

  # Perform a 3-way merge with another unit
  cub unit update --space my-space myunit --merge-source other-unit --merge-base LiveRevisionNum --merge-end HeadRevisionNum

  # Merge with specific revisions
  cub unit update --space my-space myunit --merge-source upstream-unit --merge-base Tag:v1.0 --merge-end 42

  # Merge with the unit itself (self-merge)
  cub unit update --space my-space myunit --merge-source Self --merge-base LiveRevisionNum --merge-end HeadRevisionNum

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
  echo '{"TargetID": null}' | cub unit update --patch --unit unit1,unit2,unit3 --from-stdin --change-desc "Cleared targets"
` + "```" + `
`

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

Restore using revision ID, tag, changeset, or special values:
  cub unit update --space SPACE my-unit --restore 550e8400-e29b-41d4-a716-446655440000
  cub unit update --space SPACE my-unit --restore Tag:release-v1.0
  cub unit update --space SPACE my-unit --restore ChangeSet:feature-deploy
  cub unit update --space SPACE my-unit --restore LiveRevisionNum

Upgrade from upstream:
  cub unit update --space SPACE my-unit --upgrade

Bulk patch operations:
  cub unit update --patch --where "Slug LIKE 'app-%'" --restore LiveRevisionNum --change-desc "Restored apps"
  cub unit update --patch --space "*" --where "Labels.tier = 'platform'" --label updated=true --change-desc "Updated platform units"

Key flags for agents:
- --wait: Wait for triggers and validation to complete (recommended)
- -o json: Get structured response with unit ID and details
- --verbose: Show detailed update information
- --from-stdin: Read additional metadata from stdin
- --replace-from-stdin: Replace entire metadata from stdin
- --restore: Restore to a revision using: revision number (positive/negative), revision ID (UUID), Tag:slug, ChangeSet:slug, or special values (LiveRevisionNum/LastAppliedRevisionNum/PreviousLiveRevisionNum)
- --upgrade: Upgrade to match the latest version of upstream unit
- --merge-source: Source unit for 3-way merge (slug, UUID, or "Self" for self-merge)
- --merge-base: Base revision for merge (uses same format as --restore)
- --merge-end: End revision for merge (uses same format as --restore). Also usable with --upgrade or --resolve to stop short of the source's head -- name the ChangeSet or Tag that ends the change you want, so a change still being made upstream is left behind
- --change-desc: Add a description for this change
- --label: Update labels for organization and filtering
- --patch: Use patch API for individual or bulk operations (enables --where and --unit flags for bulk mode)
- --where: Filter units for bulk patch operations (requires --patch with no unit argument)
- --unit: Target specific units by slug/UUID for bulk patch operations (requires --patch with no unit argument)

Post-update workflow:
1. Use 'function do get-placeholders' to check for placeholder values
2. Use 'function do' commands to modify configuration as needed
3. Use 'unit approve' if approval is required
4. Use 'release publish' to publish the space, which Argo CD / Flux pull

Important: Only one of config-file, --restore, --upgrade, or --merge-source (with --merge-base and --merge-end) should be specified per update operation.`

	return getCommandHelp(baseHelp, agentContext)
}

var (
	changeDescription      string
	restore                string
	resolve                string
	isUpgrade              bool
	isPatch                bool
	changesetSlug          string
	providerType           string
	mergeSource            string
	mergeBase              string
	mergeEnd               string
	mergeExternalSource    string
	mergeEnableSubtraction bool
	whereMutation          string
	filterMutation         string
	tag                    string
)

func init() {
	addStandardUpdateFlags(unitUpdateCmd)
	enableDestroyGateFlag(unitUpdateCmd)
	unitUpdateCmd.Flags().StringVar(&changeDescription, "change-desc", "", "change description")
	unitUpdateCmd.Flags().StringVar(&changesetSlug, "changeset", "", "changeset to associate the unit with (use '-' to remove in patch mode)")
	unitUpdateCmd.Flags().StringVar(&providerType, "provider", "", "provider type for the unit; None marks the unit as not applied and not included in releases")
	unitUpdateCmd.Flags().StringVar(&restore, "restore", "", "restore to a revision: UUID (revision ID), integer (revision number), Tag:slug, ChangeSet:slug, or one of LiveRevisionNum/LastAppliedRevisionNum/PreviousLiveRevisionNum")
	unitUpdateCmd.Flags().StringVar(&resolve, "resolve", "", "resolve non-automatically resolved links: Link:* for all, Link:<uuid> or Link:<slug> for a specific link, or just <slug> (e.g., space/link-name)")
	unitUpdateCmd.Flags().BoolVar(&dryRun, "dry-run", false, "dry run mode: return changed unit(s) but don't update configuration data")
	unitUpdateCmd.Flags().BoolVar(&isUpgrade, "upgrade", false, "upgrade the unit to the latest version of its upstream unit")
	unitUpdateCmd.Flags().BoolVar(&isPatch, "patch", false, "use patch API instead of update API")
	unitUpdateCmd.Flags().StringVar(&mergeSource, "merge-source", "", "source unit for 3-way merge (slug or UUID)")
	unitUpdateCmd.Flags().StringVar(&mergeBase, "merge-base", "", "base revision for 3-way merge (uses same format as --restore); with --merge-external-source, overrides the default selection of the latest MergeExternal revision")
	unitUpdateCmd.Flags().StringVar(&mergeEnd, "merge-end", "", "end revision of the source, for a 3-way merge, an --upgrade, or a --resolve (uses same format as --restore)")
	unitUpdateCmd.Flags().StringVar(&whereMutation, "where-mutation", "", "where expression to filter which mutations are affected during merge operations (only used with --merge-source)")
	unitUpdateCmd.Flags().StringVar(&filterMutation, "filter-mutation", "", "filter to select which mutations are affected during merge operations (only used with --merge-source)")
	unitUpdateCmd.Flags().StringVar(&mergeExternalSource, "merge-external-source", "", "external source identifier for merge-on-update")
	unitUpdateCmd.Flags().BoolVar(&mergeEnableSubtraction, "merge-enable-subtraction", false, "also subtract the target's local differences from the patch during --upgrade and --merge-source, on top of the stored mutation predicates that preserve overrides by default (no effect on --merge-source Self)")
	unitUpdateCmd.Flags().StringVar(&tag, "tag", "", "UUID of tag to attach to (new) head revision")
	enableOptionFlag(unitUpdateCmd)
	enableWhereFlag(unitUpdateCmd)
	enableFilterFlag(unitUpdateCmd)
	unitUpdateCmd.Flags().StringSliceVar(&unitIdentifiers, "unit", []string{}, "target specific units by slug or UUID (can be repeated or comma-separated)")
	enableWaitFlag(unitUpdateCmd)
	enableDisplayMutationsFlag(unitUpdateCmd)
	unitCmd.AddCommand(unitUpdateCmd)
}

// TODO: Add a --target flag, similar to cub unit create

var restoreValues = map[string]struct{}{
	"LiveRevisionNum":         struct{}{},
	"LastAppliedRevisionNum":  struct{}{},
	"PreviousLiveRevisionNum": struct{}{},
}

func checkConflictingArgs(args []string) bool {
	// Check for bulk patch mode (no positional args)
	isBulkPatchMode := len(args) == 0

	if isBulkPatchMode {
		if !isPatch {
			failOnError(errors.New("--patch is required in bulk mode"))
		}

		// Check for mutual exclusivity between --unit and --where flags
		if len(unitIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--unit and --where flags are mutually exclusive"))
		}

		if restore != "" {
			// In bulk mode, restore parameter can't be UUID or integer (only special strings and prefixed values)
			if _, isValid := restoreValues[restore]; !isValid {
				// Check for Before: prefix and remove it to validate the underlying value
				checkRestore := restore
				if strings.HasPrefix(restore, "Before:") {
					checkRestore = strings.TrimPrefix(restore, "Before:")
				}

				// Check for Tag:, ChangeSet:, or Revision: prefixed values, or special values
				parts := strings.Split(checkRestore, ":")
				var isValidPrefix bool

				switch len(parts) {
				case 2:
					// EntityType:Identifier format
					isValidPrefix = parts[0] == "Tag" || parts[0] == "ChangeSet" || parts[0] == "Revision"
				case 1:
					// Simple identifier - check if it's a valid restore value
					_, isValidPrefix = restoreValues[parts[0]]
				default:
					isValidPrefix = false
				}

				if !isValidPrefix {
					failOnError(fmt.Errorf("bulk patch mode doesn't support revision UUID or number restore values, only unit revision fields like LiveRevisionNum, Tag:slug, ChangeSet:slug, Revision:uuid, or Before:value"))
				}
			}
		}

	} else {
		if filter != "" || where != "" || len(unitIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --unit can only be specified with --patch and no unit positional argument"))
		}

		if isPatch && !flagPopulateModelFromStdin && flagFilename == "" && restore == "" && resolve == "" && !isUpgrade && mergeSource == "" && mergeExternalSource == "" && len(label) == 0 && len(deleteGate) == 0 && len(destroyGate) == 0 && changesetSlug == "" {
			failOnError(fmt.Errorf("--patch requires one of: --from-stdin, --filename, --restore, --resolve, --upgrade, --merge-source, --merge-external-source, --label, --delete-gate, --destroy-gate, or --changeset"))
		}
	}

	// Validate label removal only works with patch
	if err := ValidateLabelRemoval(label, isPatch); err != nil {
		failOnError(err)
	}
	// Validate delete gate removal only works with patch
	if err := ValidateDeleteGateRemoval(deleteGate, isPatch); err != nil {
		failOnError(err)
	}
	// Validate destroy gate removal only works with patch
	if err := ValidateDestroyGateRemoval(destroyGate, isPatch); err != nil {
		failOnError(err)
	}

	// Check for mutually exclusive options
	optionsSet := 0
	if restore != "" {
		optionsSet++
	}
	if resolve != "" {
		optionsSet++
	}
	if isUpgrade {
		optionsSet++
	}
	if mergeSource != "" {
		optionsSet++
		if mergeBase == "" || mergeEnd == "" {
			failOnError(fmt.Errorf("--merge-base and --merge-end must be provided with --merge-source"))
		}
	} else if mergeExternalSource != "" {
		if mergeEnd != "" {
			failOnError(fmt.Errorf("--merge-end cannot be used with --merge-external-source"))
		}
		if whereMutation != "" || filterMutation != "" {
			failOnError(fmt.Errorf("--where-mutation and --filter-mutation can only be used with --merge-source"))
		}
	} else if isUpgrade || resolve != "" {
		// An upgrade or a resolve takes its base from what was last merged, so --merge-base is
		// not theirs to set. --merge-end is: it says how far along the source to take this
		// propagation, which is how a change that is still being made is left behind.
		if mergeBase != "" {
			failOnError(fmt.Errorf("--merge-base cannot be used with --upgrade or --resolve; the base is the last revision merged from the source"))
		}
		if whereMutation != "" || filterMutation != "" {
			failOnError(fmt.Errorf("--where-mutation and --filter-mutation can only be used with --merge-source"))
		}
	} else {
		if mergeBase != "" || mergeEnd != "" {
			failOnError(fmt.Errorf("--merge-source, --merge-external-source, --upgrade, or --resolve must be provided with --merge-base or --merge-end"))
		}
		if whereMutation != "" || filterMutation != "" {
			failOnError(fmt.Errorf("--where-mutation and --filter-mutation can only be used with --merge-source"))
		}
	}
	if mergeExternalSource != "" {
		optionsSet++
	}

	if optionsSet > 1 {
		failOnError(fmt.Errorf("only one of --restore, --resolve, --upgrade, --merge-source, or --merge-external-source should be specified"))
	}

	dataFromEntity := restore != "" || resolve != "" || isUpgrade || mergeSource != ""
	if dataFromEntity && len(args) > 1 {
		failOnError(fmt.Errorf("only one of --restore, --resolve, --upgrade, --merge-source, or config-file should be specified"))
	}

	if isPatch && flagReplace {
		failOnError(fmt.Errorf("only one of --patch and --replace should be specified"))
	}

	if err := validateSpaceFlag(isBulkPatchMode); err != nil {
		failOnError(err)
	}

	if err := validateStdinFlags(); err != nil {
		failOnError(err)
	}

	// Validate label removal only works with patch
	if err := ValidateLabelRemoval(label, isPatch); err != nil {
		failOnError(err)
	}
	// Validate delete gate removal only works with patch
	if err := ValidateDeleteGateRemoval(deleteGate, isPatch); err != nil {
		failOnError(err)
	}
	// Validate destroy gate removal only works with patch
	if err := ValidateDestroyGateRemoval(destroyGate, isPatch); err != nil {
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

	// Save prior state for distinguishing new vs prior mutations and fetching old values
	priorHeadMutationNum := currentUnit.HeadMutationNum
	priorRevisionNum := currentUnit.HeadRevisionNum
	unitSlug := currentUnit.Slug

	newParams := &goclientnew.UpdateUnitParams{}

	// Prepare Unit metadata

	var patchData []byte
	if isPatch {
		// Create enhancer for unit-specific fields
		var enhancer PatchEnhancer = func(patchMap map[string]interface{}) {
			// Handle destroy gates for units
			err := setDestroyGatesInPatch(patchMap)
			if err != nil {
				failOnError(err)
			}
			// Handle TargetOptions
			if len(option) > 0 {
				optionMap := make(map[string]interface{})
				if existing, ok := patchMap["TargetOptions"]; ok {
					if m, ok := existing.(map[string]interface{}); ok {
						for k, v := range m {
							optionMap[k] = v
						}
					}
				}
				_ = patchKeyValues(optionMap, splitOptionsBySemicolon(option))
				patchMap["TargetOptions"] = optionMap
			}
			if providerType != "" {
				patchMap["ProviderType"] = providerType
			}
			if changeDescription != "" {
				patchMap["LastChangeDescription"] = changeDescription
			}
			if changesetSlug != "" {
				if changesetSlug == "-" {
					// Special value to remove the changeset
					patchMap["ChangeSetID"] = nil
				} else {
					changesetUUID, err := parseChangeSetSlug(changesetSlug)
					if err != nil {
						failOnError(fmt.Errorf("failed to get changeset: %w", err))
						return
					}
					patchMap["ChangeSetID"] = changesetUUID
					newParams.ChangeSetId = &changesetUUID
				}
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
		// For non-patch operations, handle annotations and labels in the traditional way
		err = setAnnotations(&currentUnit.Annotations)
		if err != nil {
			return err
		}
		err = setLabels(&currentUnit.Labels)
		if err != nil {
			return err
		}
		err = setDeleteGates(&currentUnit.DeleteGates)
		if err != nil {
			return err
		}
		err = setDestroyGatesField(&currentUnit.DestroyGates)
		if err != nil {
			return err
		}
		err = setOptions(&currentUnit.TargetOptions)
		if err != nil {
			return err
		}
		if providerType != "" {
			currentUnit.ProviderType = providerType
		}
		// For non-patch operations, handle change description in the traditional way
		if changeDescription != "" {
			currentUnit.LastChangeDescription = changeDescription
		}
		// For non-patch operations, handle changeset in the traditional way
		if changesetSlug != "" {
			if changesetSlug == "-" {
				// Special value to remove the changeset (only valid in patch mode)
				return errors.New("use --patch mode to remove a changeset (--changeset -)")
			}
			changesetUUID, err := parseChangeSetSlug(changesetSlug)
			if err != nil {
				return err
			}
			currentUnit.ChangeSetID = &changesetUUID
			newParams.ChangeSetId = &changesetUUID
		}
	}

	// Prepare Unit Data. These alternatives are ensured to be mutually exclusive by checkConflictingArgs above.

	if dryRun {
		newParams.DryRun = &dryRun
	}
	if isUpgrade {
		newParams.Upgrade = &isUpgrade
	}
	if mergeEnableSubtraction {
		newParams.MergeEnableSubtraction = &mergeEnableSubtraction
	}

	if restore != "" {
		// Parse restore parameter - enhanced to support Tag:slug, ChangeSet:slug formats
		restoreFormatted, restoreIsUUID, err := parseSelectedRevisionParameter(restore, currentUnit.UnitID, currentUnit.SpaceID.String(), currentUnit.HeadRevisionNum)
		if err != nil {
			return err
		}
		if restoreIsUUID {
			// It's a revision ID - set RevisionId parameter
			revisionUUID, _ := uuid.Parse(restoreFormatted)
			newParams.RevisionId = &revisionUUID
		} else {
			// It's a formatted restore specification
			newParams.Restore = &restoreFormatted
		}
	}

	if resolve != "" {
		resolveFormatted, err := formatResolveParameter(resolve)
		if err != nil {
			return err
		}
		newParams.Resolve = &resolveFormatted
	}

	if mergeEnd != "" && (isUpgrade || resolve != "") {
		// The end revision names a Revision of the *source* -- the upstream Unit for an
		// upgrade, the Link's target for a resolve -- so a delta relative to head has to be
		// resolved against the source's head, not this Unit's.
		mergeEndFormatted, err := parseSourceEndRevision(mergeEnd, currentUnit, isUpgrade)
		if err != nil {
			return err
		}
		newParams.MergeEnd = &mergeEndFormatted
	}

	if mergeSource != "" {
		var mergeSourceUnit *goclientnew.Unit
		var mergeSourceStr string

		// Check if merge source is "Self"
		if mergeSource == "Self" {
			// Pass "Self" directly to the API
			mergeSourceStr = "Self"
			mergeSourceUnit = currentUnit
		} else {
			// Parse merge source unit
			mergeSourceUnit, err = parseEntityIdentifierSingleAsEntity[goclientnew.Unit](
				mergeSource,
				"unit",
				"UnitID,SpaceID,HeadRevisionNum",
				apiGetUnitFromSlugInSpace,
				func(u *goclientnew.Unit) string { return u.UnitID.String() },
			)
			if err != nil {
				return fmt.Errorf("failed to get merge source unit: %w", err)
			}
			mergeSourceStr = mergeSourceUnit.UnitID.String()
		}
		newParams.MergeSource = &mergeSourceStr

		mergeBaseFormatted, _, err := parseSelectedRevisionParameter(mergeBase, mergeSourceUnit.UnitID, mergeSourceUnit.SpaceID.String(), mergeSourceUnit.HeadRevisionNum)
		if err != nil {
			return fmt.Errorf("invalid merge base specification: %w", err)
		}
		newParams.MergeBase = &mergeBaseFormatted

		mergeEndFormatted, _, err := parseSelectedRevisionParameter(mergeEnd, mergeSourceUnit.UnitID, mergeSourceUnit.SpaceID.String(), mergeSourceUnit.HeadRevisionNum)
		if err != nil {
			return fmt.Errorf("invalid merge end specification: %w", err)
		}
		newParams.MergeEnd = &mergeEndFormatted

		// Add mutation filtering parameters if provided
		if whereMutation != "" {
			newParams.WhereMutation = &whereMutation
		}
		if filterMutation != "" {
			filterMutationUUID, err := parseFilterFlag(filterMutation)
			if err != nil {
				return fmt.Errorf("failed to parse filter-mutation: %w", err)
			}
			newParams.FilterMutation = &filterMutationUUID
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

		// We don't set the external config data source automatically because there isn't a reliable
		// way to determine whether this is an external upload or read+modify+write implicitly.
		// if mergeExternalSource == "" {
		// 	if args[1] == "-" {
		// 		mergeExternalSource = "stdin"
		// 	} else {
		// 		mergeExternalSource = args[1]
		// 	}
		// }
	}

	if mergeExternalSource != "" {
		newParams.MergeExternalSource = &mergeExternalSource
		if mergeBase != "" {
			mergeBaseFormatted, _, err := parseSelectedRevisionParameter(mergeBase, currentUnit.UnitID, currentUnit.SpaceID.String(), currentUnit.HeadRevisionNum)
			if err != nil {
				return fmt.Errorf("invalid merge base specification: %w", err)
			}
			newParams.MergeBase = &mergeBaseFormatted
		}
	}

	if tag != "" {
		tagID, err := parseTagSlug(tag)
		failOnError(err)
		newParams.Tag = &tagID
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

	// Display mutations if requested
	if shouldDisplayMutations() {
		tprintRaw("")
		configFile := ""
		if len(args) > 1 {
			configFile = args[1]
		}
		updateDesc := unitUpdateChangeDescription(configFile)
		if restore != "" {
			// Restore snapshots the revision's MutationSources onto the unit
			// (same indices as the restored revision), so the usual split
			// display can't tell new vs prior. Compute the diff on the fly
			// to show the user what changed because of this restore.
			lookupMutationsUnitID = unitDetails.UnitID.String()
			priorRevision := fmt.Sprintf("%s/%d", unitSlug, priorRevisionNum)
			displayMutationsForRestore(currentUnit.Data, unitDetails.Data, unitDetails.SpaceID.String(), dryRun, priorRevision, updateDesc)
		} else if dryRun {
			// Dry-run returns the unit as it would look after the update,
			// including MutationSources with the proposed mutations.
			displayMutationsForUnit(unitDetails, priorHeadMutationNum, updateDesc, "dry-run")
		} else {
			// Fetch updated unit to get the latest MutationSources
			updatedUnit, err := apiGetUnitInSpace(unitDetails.UnitID.String(), unitDetails.SpaceID.String(), "*")
			if err != nil {
				return err
			}
			priorRevision := fmt.Sprintf("%s/%d", unitSlug, priorRevisionNum)
			displayMutationsForUnit(updatedUnit, priorHeadMutationNum, updateDesc, priorRevision)
		}
	}

	return nil
}

// unitUpdateChangeDescription describes the update operation that produced the
// new mutations, for the "New changes from ..." header. configFile is the
// positional config-file argument, if any.
func unitUpdateChangeDescription(configFile string) string {
	switch {
	case restore != "":
		return "restore to " + restore
	case isUpgrade:
		return "upgrade"
	case mergeSource != "":
		return "merge from " + mergeSource
	case resolve != "":
		return "resolve " + resolve
	case configFile != "":
		return "update from " + configFile
	default:
		return "PatchUnit"
	}
}

func runBulkUnitUpdate() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

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

	// Build bulk patch parameters
	params := &goclientnew.BulkPatchUnitsParams{
		Where: &effectiveWhere,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// Create enhancer for unit-specific fields
	var enhancer PatchEnhancer = func(patchMap map[string]interface{}) {
		// Handle destroy gates for units
		err := setDestroyGatesInPatch(patchMap)
		if err != nil {
			failOnError(err)
		}
		// Handle TargetOptions
		if len(option) > 0 {
			optionMap := make(map[string]interface{})
			if existing, ok := patchMap["TargetOptions"]; ok {
				if m, ok := existing.(map[string]interface{}); ok {
					for k, v := range m {
						optionMap[k] = v
					}
				}
			}
			_ = patchKeyValues(optionMap, splitOptionsBySemicolon(option))
			patchMap["TargetOptions"] = optionMap
		}
		if providerType != "" {
			patchMap["ProviderType"] = providerType
		}
		if changeDescription != "" {
			patchMap["LastChangeDescription"] = changeDescription
		}
		if changesetSlug != "" {
			if changesetSlug == "-" {
				// Special value to remove the changeset
				patchMap["ChangeSetID"] = nil
			} else {
				changesetUUID, err := parseChangeSetSlug(changesetSlug)
				if err != nil {
					failOnError(fmt.Errorf("failed to get changeset: %w", err))
					return
				}
				patchMap["ChangeSetID"] = changesetUUID
				params.ChangeSetId = &changesetUUID
			}
		}
	}

	// Build patch data using consolidated function
	patchData, err := BuildPatchData(enhancer)
	if err != nil {
		return err
	}

	// Set include parameter to expand UpstreamUnitID
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	params.Include = &include

	// Add merge parameters if specified
	if mergeSource != "" {
		var mergeSourceStr string
		var mergeUnitID uuid.UUID
		var mergeSpaceIDStr string
		var mergeHeadRevisionNum int64

		// Note: For bulk operations, "Self" means different units for each item.
		// Pass dummy values.
		if mergeSource == "Self" {
			mergeSourceStr = "Self"
			mergeUnitID = uuid.Nil
			mergeSpaceIDStr = "*"
			mergeHeadRevisionNum = 0
		} else {
			// Parse merge source unit
			mergeSourceUnit, err := parseEntityIdentifierSingleAsEntity[goclientnew.Unit](
				mergeSource,
				"unit",
				"UnitID,SpaceID,HeadRevisionNum",
				apiGetUnitFromSlugInSpace,
				func(u *goclientnew.Unit) string { return u.UnitID.String() },
			)
			if err != nil {
				return fmt.Errorf("failed to get merge source unit: %w", err)
			}

			mergeSourceStr = mergeSourceUnit.UnitID.String()
			mergeUnitID = mergeSourceUnit.UnitID
			mergeSpaceIDStr = mergeSourceUnit.SpaceID.String()
			mergeHeadRevisionNum = mergeSourceUnit.HeadRevisionNum
		}
		params.MergeSource = &mergeSourceStr
		mergeBaseFormatted, mergeBaseIsUUID, err := parseSelectedRevisionParameter(mergeBase, mergeUnitID, mergeSpaceIDStr, mergeHeadRevisionNum)
		if err != nil {
			return fmt.Errorf("invalid merge base specification: %w", err)
		}
		if mergeBaseIsUUID {
			// Convert UUID back to Revision:UUID format for bulk API
			mergeBaseFormatted = fmt.Sprintf("Revision:%s", mergeBaseFormatted)
		}
		params.MergeBase = &mergeBaseFormatted

		mergeEndFormatted, mergeEndIsUUID, err := parseSelectedRevisionParameter(mergeEnd, mergeUnitID, mergeSpaceIDStr, mergeHeadRevisionNum)
		if err != nil {
			return fmt.Errorf("invalid merge end specification: %w", err)
		}
		if mergeEndIsUUID {
			// Convert UUID back to Revision:UUID format for bulk API
			mergeEndFormatted = fmt.Sprintf("Revision:%s", mergeEndFormatted)
		}
		params.MergeEnd = &mergeEndFormatted

		// Add mutation filtering parameters if provided
		if whereMutation != "" {
			params.WhereMutation = &whereMutation
		}
		if filterMutation != "" {
			filterMutationUUID, err := parseFilterFlag(filterMutation)
			if err != nil {
				return fmt.Errorf("failed to parse filter-mutation: %w", err)
			}
			params.FilterMutation = &filterMutationUUID
		}
	}

	// Add restore parameter if specified
	if restore != "" {
		// Parse restore using consolidated logic
		// For bulk operations, use "*" as spaceID to prevent revision number resolution
		restoreFormatted, restoreIsUUID, err := parseSelectedRevisionParameter(restore, uuid.Nil, "*", 0)
		if err != nil {
			return fmt.Errorf("invalid restore specification: %w", err)
		}
		if restoreIsUUID {
			// Convert UUID back to Revision:UUID format for bulk API
			restoreFormatted = fmt.Sprintf("Revision:%s", restoreFormatted)
		}
		params.Restore = &restoreFormatted
	}

	if mergeEnd != "" && (isUpgrade || resolve != "") {
		mergeEndFormatted, err := parseSourceEndRevision(mergeEnd, nil, isUpgrade)
		if err != nil {
			return err
		}
		params.MergeEnd = &mergeEndFormatted
	}

	// Add resolve parameter if specified
	if resolve != "" {
		resolveFormatted, err := formatResolveParameter(resolve)
		if err != nil {
			return err
		}
		params.Resolve = &resolveFormatted
	}

	if dryRun {
		params.DryRun = &dryRun
	}
	// Add upgrade parameter if specified
	if isUpgrade {
		params.Upgrade = &isUpgrade
	}
	if mergeEnableSubtraction {
		params.MergeEnableSubtraction = &mergeEnableSubtraction
	}

	if tag != "" {
		tagID, err := parseTagSlug(tag)
		failOnError(err)
		params.Tag = &tagID
	}

	// Save prior unit state so mutation display can tell new changes from prior ones.
	// Restore additionally needs the pre-update data to compute its diff.
	var priorUnits map[string]priorUnitInfo
	if shouldDisplayMutations() {
		priorUnits = savePriorUnitInfoFromWhereWithData(effectiveWhere, restore != "")
	}

	// Call the bulk patch API (organization-level API that can be constrained by SpaceID in WHERE clause)
	bulkRes, err := cubClientNew.BulkPatchUnitsWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)

	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
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

	bulkErr := handleBulkCreateOrUpdateResponse(responses, statusCode, "update", "")

	// Display mutations if requested, for the units that were updated successfully
	if shouldDisplayMutations() {
		displayMutationsForBulkUnitUpdate(responses, priorUnits, restore != "", dryRun, unitUpdateChangeDescription(""))
	}

	return bulkErr
}

func updateUnit(spaceID uuid.UUID, currentUnit *goclientnew.Unit, params *goclientnew.UpdateUnitParams) (*goclientnew.Unit, error) {
	updatedRes, err := cubClientNew.UpdateUnitWithResponse(ctx, spaceID, currentUnit.UnitID, params, *currentUnit)
	if cubapi.IsAPIError(err, updatedRes) {
		return nil, cubapi.InterpretErrorGeneric(err, updatedRes)
	}

	return updatedRes.JSON200, nil
}

func patchUnit(spaceID uuid.UUID, unitID uuid.UUID, updateParams *goclientnew.UpdateUnitParams, patchData []byte) (*goclientnew.Unit, error) {
	// Convert UpdateUnitParams to PatchUnitParams
	patchParams := &goclientnew.PatchUnitParams{}
	patchParams.RevisionId = updateParams.RevisionId
	patchParams.Restore = updateParams.Restore
	patchParams.Resolve = updateParams.Resolve
	patchParams.DryRun = updateParams.DryRun
	patchParams.Upgrade = updateParams.Upgrade
	patchParams.MergeSource = updateParams.MergeSource
	patchParams.MergeBase = updateParams.MergeBase
	patchParams.MergeEnd = updateParams.MergeEnd
	patchParams.MergeExternalSource = updateParams.MergeExternalSource
	patchParams.MergeEnableSubtraction = updateParams.MergeEnableSubtraction
	patchParams.WhereMutation = updateParams.WhereMutation
	patchParams.FilterMutation = updateParams.FilterMutation
	patchParams.Tag = updateParams.Tag
	patchParams.ChangeSetId = updateParams.ChangeSetId

	unitRes, err := cubClientNew.PatchUnitWithBodyWithResponse(
		ctx,
		spaceID,
		unitID,
		patchParams,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if cubapi.IsAPIError(err, unitRes) {
		return nil, cubapi.InterpretErrorGeneric(err, unitRes)
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
	if len(unitDetails.ApplyGates) > 0 && !quiet && !isAlternativeOutput() && !hasAlternativeFunctionOutput() {
		tprint("Unit %s (%s) has apply gates: %s", unitDetails.Slug, unitDetails.UnitID.String(), applyGatesToString(unitDetails.ApplyGates))
	}
	if len(unitDetails.ApplyWarnings) > 0 && !quiet && !isAlternativeOutput() && !hasAlternativeFunctionOutput() {
		tprint("Unit %s (%s) has apply warnings: %s", unitDetails.Slug, unitDetails.UnitID.String(), applyGatesToString(unitDetails.ApplyWarnings))
	}
	return nil
}

func handleBulkCreateOrUpdateResponse(responses *[]goclientnew.UnitCreateOrUpdateResponse, statusCode int, operationName, contextInfo string) error {
	if responses == nil {
		return fmt.Errorf("no response data received")
	}

	// Wait for triggers BEFORE calling the generic display function
	if wait {
		successfulUnits := []*goclientnew.Unit{}
		for _, resp := range *responses {
			if resp.Error == nil && resp.Unit != nil {
				successfulUnits = append(successfulUnits, resp.Unit)
			}
		}

		if len(successfulUnits) > 0 {
			if !quiet && !isAlternativeOutput() {
				tprintRaw("Awaiting triggers...")
			}
			// Wait for each successfully updated unit
			for _, unit := range successfulUnits {
				// The units returned don't have the extended information, so we re-fetch them with that information.
				unitExtended, err := apiGetExtendedUnitInSpace(unit.UnitID.String(), unit.SpaceID.String(), "*")
				if err != nil {
					return err
				}
				err = awaitTriggersRemoval(unitExtended.Unit)
				if err != nil {
					return err
				}
				// Update the unit in the response with the latest state
				// Note: We can't easily update the original response, but the triggers have been awaited
			}
		}
	}

	// Convert responses to the format expected by the generic function
	// For status code 207, we need both parameters
	var responses200 *[]goclientnew.UnitCreateOrUpdateResponse
	var responses207 *[]goclientnew.UnitCreateOrUpdateResponse
	if statusCode == 200 {
		responses200 = responses
	} else if statusCode == 207 {
		responses207 = responses
	}

	// Call the generic display function
	return displayBulkGenericCreateOrUpdateResults(
		responses200, responses207, statusCode, "unit", operationName, contextInfo,
		func(r *goclientnew.UnitCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
		func(r *goclientnew.UnitCreateOrUpdateResponse) string {
			if r.Unit != nil {
				return r.Unit.Slug
			}
			return ""
		},
	)
}

// parseSourceEndRevision formats a --merge-end value for an upgrade or a resolve, where the
// Revision named belongs to the source rather than to the Unit being updated.
//
// For a single Unit's upgrade the source is known -- the Unit's upstream -- so it is fetched and
// a delta relative to head resolves against it. For a resolve the source is whichever Unit each
// Link points at, and in a bulk update each Unit has its own upstream, so there is no single head
// to count back from; a delta is rejected there rather than silently measured against the wrong
// Unit. Everything else -- a Tag, a ChangeSet, an absolute or named revision -- is resolved
// per-Unit by the server.
func parseSourceEndRevision(revisionSpec string, currentUnit *goclientnew.Unit, isUpgrade bool) (string, error) {
	sourceUnitID := uuid.Nil
	sourceSpaceID := ""
	var sourceHeadRevisionNum int64
	if isUpgrade && currentUnit != nil {
		if currentUnit.UpstreamUnitID == nil || currentUnit.UpstreamSpaceID == nil {
			return "", fmt.Errorf("--merge-end requires an upstream unit to upgrade from")
		}
		upstreamUnit, err := apiGetUnitInSpace(currentUnit.UpstreamUnitID.String(),
			currentUnit.UpstreamSpaceID.String(), "UnitID,SpaceID,HeadRevisionNum")
		if err != nil {
			return "", fmt.Errorf("failed to get upstream unit for --merge-end: %w", err)
		}
		sourceUnitID = upstreamUnit.UnitID
		sourceSpaceID = upstreamUnit.SpaceID.String()
		sourceHeadRevisionNum = upstreamUnit.HeadRevisionNum
	} else if strings.HasPrefix(revisionSpec, "-") {
		return "", fmt.Errorf("--merge-end relative to head is not supported here, since the source is not a single unit; name a Tag, a ChangeSet, or an absolute revision")
	}
	formatted, isUUID, err := parseSelectedRevisionParameter(revisionSpec, sourceUnitID, sourceSpaceID, sourceHeadRevisionNum)
	if err != nil {
		return "", fmt.Errorf("invalid merge end specification: %w", err)
	}
	if isUUID {
		// The bulk and single APIs both take the end revision as a string.
		formatted = fmt.Sprintf("Revision:%s", formatted)
	}
	return formatted, nil
}

// parseSelectedRevisionParameter parses various revision formats and returns the formatted value
// This renamed function can be used for restore, merge-base, merge-end and other revision specifications
// Returns: (formatted string, isUUID bool, error)
// If isUUID is true, the formatted string is a revision UUID that should be used as RevisionId
// Otherwise, it's a formatted restore/revision specification string
func parseSelectedRevisionParameter(revisionSpec string, unitID uuid.UUID, spaceID string, headRevisionNum int64) (string, bool, error) {
	// Check for Before: prefix and remove it
	var isBeforeModifier bool
	originalSpec := revisionSpec
	if strings.HasPrefix(revisionSpec, "Before:") {
		isBeforeModifier = true
		revisionSpec = strings.TrimPrefix(revisionSpec, "Before:")
	}

	// Parse the remaining revision specification
	parts := strings.Split(revisionSpec, ":")
	var entityType, identifier string

	switch len(parts) {
	case 2:
		// EntityType:Identifier format
		entityType = parts[0]
		identifier = parts[1]
	case 1:
		// Simple identifier (LiveRevisionNum, UUID, integer, etc.)
		identifier = parts[0]
	default:
		return "", false, fmt.Errorf("invalid revision specification: %s", originalSpec)
	}

	// Handle entity type-specific parsing
	if entityType == "Tag" {
		// Parse tag slug/ID and convert to UUID
		tagUUID, err := parseTagSlug(identifier)
		if err != nil {
			return "", false, fmt.Errorf("failed to parse tag '%s': %w", identifier, err)
		}
		// Return the formatted value
		if isBeforeModifier {
			return fmt.Sprintf("Before:Tag:%s", tagUUID), false, nil
		}
		return fmt.Sprintf("Tag:%s", tagUUID), false, nil

	} else if entityType == "ChangeSet" {
		// Parse changeset slug/ID and convert to UUID
		changesetUUID, err := parseChangeSetSlug(identifier)
		if err != nil {
			return "", false, fmt.Errorf("failed to parse changeset '%s': %w", identifier, err)
		}
		// Return the formatted value
		if isBeforeModifier {
			return fmt.Sprintf("Before:ChangeSet:%s", changesetUUID), false, nil
		}
		return fmt.Sprintf("ChangeSet:%s", changesetUUID), false, nil

	} else if entityType == "Revision" {
		// Handle Revision:uuid format
		if revisionUUID, err := uuid.Parse(identifier); err == nil {
			// It's a UUID - return it to be used as revision ID
			return revisionUUID.String(), true, nil
		} else {
			return "", false, fmt.Errorf("invalid revision identifier '%s': must be a UUID", identifier)
		}

	} else if entityType != "" {
		return "", false, fmt.Errorf("unsupported entity type '%s': supported types are Tag, ChangeSet, and Revision", entityType)
	}

	// Handle simple identifiers (no entity type prefix)
	if identifier == "LiveRevisionNum" || identifier == "LastAppliedRevisionNum" ||
		identifier == "PreviousLiveRevisionNum" || identifier == "HeadRevisionNum" {
		// Special revision values
		if isBeforeModifier {
			return fmt.Sprintf("Before:%s", identifier), false, nil
		}
		return identifier, false, nil
	}

	// Fall back to original parsing logic for UUIDs and integers
	if revisionUUID, err := uuid.Parse(identifier); err == nil {
		// It's a UUID - return it to be used as revision ID
		return revisionUUID.String(), true, nil
	} else if revisionNum, err := strconv.ParseInt(identifier, 10, 64); err == nil {
		// It's an integer - treat as revision number
		if revisionNum < 0 {
			// A negative value means it's relative to head revision num
			subtracted := headRevisionNum + revisionNum
			if subtracted < 1 {
				return "", false, fmt.Errorf("revision delta %d must be less than HeadRevisionNum %d", revisionNum, headRevisionNum)
			}
			revisionNum = subtracted
			identifier = fmt.Sprintf("%d", revisionNum)
		}
		// We don't actually need to pass a UUID. We can pass the number.
		// // Check if this is a bulk operation (spaceID is "*")
		// if spaceID == "*" {
		// 	return "", false, fmt.Errorf("revision numbers not supported in bulk operations (use revision UUID, named revision, Tag:slug, or ChangeSet:slug instead): %s", originalSpec)
		// }
		// // Use the provided spaceID to resolve revision number
		// rev, err := apiGetRevisionFromNumberInSpace(revisionNum, unitID.String(), spaceID, "RevisionID")
		// if err != nil {
		// 	return "", false, err
		// }
		// // Return the revision ID to be used
		// return rev.RevisionID.String(), true, nil
		return identifier, false, nil
	} else {
		return "", false, fmt.Errorf("invalid revision value '%s': must be a UUID (revision ID), integer (revision number), Tag:slug, ChangeSet:slug, Before:value, or one of LiveRevisionNum/LastAppliedRevisionNum/PreviousLiveRevisionNum/HeadRevisionNum", revisionSpec)
	}
}

// formatResolveParameter parses the resolve parameter and returns the formatted value.
// Accepts:
// - "Link:*" - resolves all non-auto-update links
// - "Link:<uuid>" - resolves a specific link by UUID
// - "<slug>" or "<space>/<slug>" - resolves a link by slug, converted to "Link:<uuid>"
func formatResolveParameter(resolve string) (string, error) {
	// Check for "Link:*" wildcard
	if resolve == "Link:*" {
		return resolve, nil
	}

	// Check for "Link:<identifier>" format
	if strings.HasPrefix(resolve, "Link:") {
		identifier := strings.TrimPrefix(resolve, "Link:")
		// If it's already a UUID, pass as-is
		if _, err := uuid.Parse(identifier); err == nil {
			return resolve, nil
		}
		// Otherwise, try to parse the identifier as a link slug
		linkUUID, err := parseEntityIdentifierSingle[goclientnew.Link](
			identifier,
			EntityTypeLink,
			apiGetLinkFromSlugInSpace,
			func(l *goclientnew.Link) string { return l.LinkID.String() },
		)
		if err != nil {
			return "", fmt.Errorf("failed to resolve link '%s': %w", identifier, err)
		}
		return fmt.Sprintf("Link:%s", linkUUID), nil
	}

	// No "Link:" prefix - treat the whole string as a link slug
	linkUUID, err := parseEntityIdentifierSingle[goclientnew.Link](
		resolve,
		EntityTypeLink,
		apiGetLinkFromSlugInSpace,
		func(l *goclientnew.Link) string { return l.LinkID.String() },
	)
	if err != nil {
		return "", fmt.Errorf("failed to resolve link '%s': %w", resolve, err)
	}
	return fmt.Sprintf("Link:%s", linkUUID), nil
}
