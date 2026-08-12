// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var unitGetCmd = &cobra.Command{
	Use:   "get <name or id>",
	Short: "Get details about an unit",
	Args:  cobra.ExactArgs(1),
	Long:  getUnitGetHelp(),
	RunE:  unitGetCmdRun,
}

func getUnitGetHelp() string {
	baseHelp := `Get metadata and status information about a unit in a space.

Examples:
` + "```" + `
  # Get details about a namespace unit
  cub unit get --space my-space -o json my-ns

  # Get details about a deployment unit
  cub unit get --space my-space -o json my-deployment

  # Get details about a headlamp application unit
  cub unit get --space my-space -o json headlamp

  # Get only the configuration data of a unit (use the dedicated subcommand)
  cub unit data --space my-space my-deployment
` + "```" + `
`

	agentContext := `Critical for inspecting unit configuration and state before making changes.

Agent inspection workflow:
1. Use 'unit get UNIT_SLUG' to understand unit metadata and current state
2. Check revision numbers to understand change history
3. Use 'cub unit data UNIT_SLUG' to get raw configuration for local processing

Key information provided:
- Unit metadata: ID, slug, display name, creation/update times
- Revision tracking: HeadRevisionNum vs LiveRevisionNum shows pending changes
- Approval state: ApprovedBy list and ApplyGates status
- Configuration data: Actual YAML/HCL content via 'cub unit data'

Important flags for agents:
- -o json: Get full metadata in structured format
- -o jq=<expr>: Drill into a specific field
- -o mutations: Show the mutation diff for the current head revision

For the raw configuration bytes, use the dedicated subcommand:
- cub unit data <unit> [--filename <path>]  — prints or writes the decoded Data

Common agent patterns:
  # Download unit for local editing
  cub unit data my-app --space prod > my-app.yaml

  # Check if unit has pending changes
  cub unit get my-app --space prod -o jq='.Unit.HeadRevisionNum > .Unit.LiveRevisionNum'

  # Get approval status
  cub unit get my-app --space prod -o jq='.Unit.ApprovedBy | length'

Use the slug or UUID to identify the unit. Slugs are more human-readable and typically preferred.`

	return getCommandHelp(baseHelp, agentContext)
}

func init() {
	addStandardGetFlags(unitGetCmd)
	enableDisplayMutationsFlag(unitGetCmd)
	unitGetCmd.Flags().BoolVar(&dataOnly, "data-only", false, "show config data without other response details")
	_ = unitGetCmd.Flags().MarkDeprecated("data-only", "use 'cub unit data <unit>'")
	unitGetCmd.Flags().StringVar(&flagFilename, "filename", "", "write config data to file instead of stdout (only works with --data-only)")
	_ = unitGetCmd.Flags().MarkDeprecated("filename", "use 'cub unit data --filename'")
	unitCmd.AddCommand(unitGetCmd)
}

func unitGetCmdRun(cmd *cobra.Command, args []string) error {
	unitDetails, err := apiGetExtendedUnitFromSlug(args[0], selectFields)
	if err != nil {
		return err
	}

	// -o mutations replaces the default summary with the mutations diff for
	// the current head revision. The deprecated --display-mutations flag
	// remains additive and is handled inside displayExtendedUnitDetails.
	if effectiveOutput().Kind == OutputMutations {
		displayMutationsForUnit(unitDetails.Unit, 0, "", "")
		return nil
	}

	displayGetResults(unitDetails, displayExtendedUnitDetails)
	return nil
}

func countResourcesFromExtended(unitDetails *goclientnew.ExtendedUnit) int {
	return countResources(unitDetails.Unit)
}

func countResources(unitDetails *goclientnew.Unit) int {
	if len(*unitDetails.MutationSources) == 0 {
		return 0
	}
	count := 0
	for i := range *unitDetails.MutationSources {
		if *(*unitDetails.MutationSources)[i].ResourceMutationInfo.MutationType == goclientnew.Delete {
			continue
		}
		count++
	}
	return count
}

func applyGatesToString(applyGates map[string]bool) string {
	gates := make([]string, 0, len(applyGates))
	for gate, failed := range applyGates {
		if failed {
			gates = append(gates, gate)
		}
	}
	return strings.Join(gates, ", ")
}

func displayExtendedUnitDetails(unitDetails *goclientnew.ExtendedUnit) {
	if !dataOnly {
		view := tableView()
		view.Append([]string{"Name", unitDetails.Unit.Slug})
		view.Append([]string{"Toolchain Type", unitDetails.Unit.ToolchainType})
		view.Append([]string{"Provider Type", unitDetails.Unit.ProviderType})

		// Not implemented yet
		// Show Set name if available
		// if unitDetails.Set != nil {
		// 	view.Append([]string{"Set", unitDetails.Set.Slug})
		// }

		// Show Target name if available
		if unitDetails.Target != nil {
			view.Append([]string{"Target", unitDetails.Target.Slug})
		}
		if len(unitDetails.Unit.TargetOptions) > 0 {
			view.Append([]string{"Target Options", mapToString(unitDetails.Unit.TargetOptions)})
		}

		// Show ChangeSet slug if available, or ChangeSetID if not nil and not uuid.Nil
		if unitDetails.ChangeSet != nil {
			view.Append([]string{"ChangeSet", unitDetails.ChangeSet.Slug})
		} else if unitDetails.Unit.ChangeSetID != nil && *unitDetails.Unit.ChangeSetID != uuid.Nil {
			view.Append([]string{"ChangeSet ID", unitDetails.Unit.ChangeSetID.String()})
		}

		view.Append([]string{"Created At", unitDetails.Unit.CreatedAt.String()})
		view.Append([]string{"Updated At", unitDetails.Unit.UpdatedAt.String()})
		view.Append([]string{"Labels", labelsToString(unitDetails.Unit.Labels)})
		view.Append([]string{"Delete Gates", deleteGatesToString(unitDetails.Unit.DeleteGates)})
		view.Append([]string{"Destroy Gates", destroyGatesToString(unitDetails.Unit.DestroyGates)})
		view.Append([]string{"Annotations", annotationsToString(unitDetails.Unit.Annotations)})
		view.Append([]string{"Last Change Description", unitDetails.Unit.LastChangeDescription})
		view.Append([]string{"Head Revision Num", fmt.Sprintf("%d", unitDetails.Unit.HeadRevisionNum)})
		view.Append([]string{"Last Applied Revision Num", fmt.Sprintf("%d", unitDetails.Unit.LastAppliedRevisionNum)})
		view.Append([]string{"Live Revision Num", fmt.Sprintf("%d", unitDetails.Unit.LiveRevisionNum)})
		view.Append([]string{"Previous Live Revision Num", fmt.Sprintf("%d", unitDetails.Unit.PreviousLiveRevisionNum)})

		// Show upstream unit info if available
		if unitDetails.Unit.UpstreamUnitID != nil && *unitDetails.Unit.UpstreamUnitID != uuid.Nil {
			view.Append([]string{"Upstream Organization ID", unitDetails.Unit.UpstreamOrganizationID.String()})
			view.Append([]string{"Upstream Space ID", unitDetails.Unit.UpstreamSpaceID.String()})
			view.Append([]string{"Upstream Unit ID", unitDetails.Unit.UpstreamUnitID.String()})
			view.Append([]string{"Upstream Revision Num", fmt.Sprintf("%d", unitDetails.Unit.UpstreamRevisionNum)})
		}

		if len(unitDetails.Unit.ApplyGates) != 0 {
			view.Append([]string{"Apply Gates", applyGatesToString(unitDetails.Unit.ApplyGates)})
		}

		if len(unitDetails.Unit.ApplyWarnings) != 0 {
			view.Append([]string{"Apply Warnings", applyGatesToString(unitDetails.Unit.ApplyWarnings)})
		}

		if len(unitDetails.Unit.ApprovedBy) != 0 {
			approverIDs := ""
			for _, approverID := range unitDetails.Unit.ApprovedBy {
				approverIDs += " " + approverID.String()
			}
			view.Append([]string{"Approved By", strings.TrimSpace(approverIDs)})
		}

		if len(unitDetails.Unit.Values) != 0 {
			view.Append([]string{"Values", mapToString(unitDetails.Unit.Values)})
		}

		// Show From Links if available (expanded)
		if unitDetails.FromLink != nil && len(unitDetails.FromLink) != 0 {
			linkSlugs := ""
			for _, link := range unitDetails.FromLink {
				linkSlugs += " " + link.Slug
			}
			view.Append([]string{"From Links", strings.TrimSpace(linkSlugs)})
		} else if unitDetails.Unit.FromLinkID != nil && len(unitDetails.Unit.FromLinkID) != 0 {
			// Fallback to IDs if expansion not available
			linkIDs := ""
			for _, linkID := range unitDetails.Unit.FromLinkID {
				linkIDs += " " + linkID.String()
			}
			view.Append([]string{"From Link IDs", strings.TrimSpace(linkIDs)})
		}

		// Show Bridge Worker if available (expanded)
		if unitDetails.BridgeWorker != nil {
			view.Append([]string{"Bridge Worker", unitDetails.BridgeWorker.Slug})
		} else if unitDetails.Unit.BridgeWorkerID != nil && *unitDetails.Unit.BridgeWorkerID != uuid.Nil {
			// Fallback to ID if expansion not available
			view.Append([]string{"Bridge Worker ID", unitDetails.Unit.BridgeWorkerID.String()})
		}

		if unitDetails.Unit.DataHash != "" {
			view.Append([]string{"Data Hash", unitDetails.Unit.DataHash})
		}
		view.Append([]string{"Head Mutation Num", fmt.Sprintf("%d", unitDetails.Unit.HeadMutationNum)})
		view.Append([]string{"Head Unit Action Num", fmt.Sprintf("%d", unitDetails.Unit.HeadUnitActionNum)})
		view.Append([]string{"Head Unit Event Num", fmt.Sprintf("%d", unitDetails.Unit.HeadUnitEventNum)})
		view.Append([]string{"Number of Resources", fmt.Sprintf("%d", countResourcesFromExtended(unitDetails))})

		if len(unitDetails.Unit.NeededPaths) > 0 {
			view.Append([]string{"Needed Paths", fmt.Sprintf("%d paths", len(unitDetails.Unit.NeededPaths))})
		}
		if len(unitDetails.Unit.ProvidedPaths) > 0 {
			view.Append([]string{"Provided Paths", fmt.Sprintf("%d paths", len(unitDetails.Unit.ProvidedPaths))})
		}

		view.Render()

		if len(*unitDetails.Unit.MutationSources) != 0 && (verbose || shouldDisplayMutations()) {
			tprintRaw("")
			tprintRaw("Mutation Sources:")
			tprintRaw("-----------------")
			if shouldDisplayMutations() {
				lookupMutationsUnitID = unitDetails.Unit.UnitID.String()
				displayResourceMutationList(unitDetails.Unit.MutationSources, true, 0, "", "")
			} else {
				displayJSON(unitDetails.Unit.MutationSources)
			}
		}

		if len(unitDetails.Unit.NeededPaths) > 0 && verbose {
			tprintRaw("")
			tprintRaw("Needed Paths:")
			tprintRaw("-------------")
			displayJSON(unitDetails.Unit.NeededPaths)
		}
		if len(unitDetails.Unit.ProvidedPaths) > 0 && verbose {
			tprintRaw("")
			tprintRaw("Provided Paths:")
			tprintRaw("---------------")
			displayJSON(unitDetails.Unit.ProvidedPaths)
		}
	}
	if dataOnly && flagFilename != "" {
		yamlBytes, err := base64.StdEncoding.DecodeString(unitDetails.Unit.Data)
		if err != nil {
			failOnError(err)
		}
		err = os.WriteFile(flagFilename, yamlBytes, 0644)
		if err != nil {
			failOnError(err)
		}
		tprintRaw(fmt.Sprintf("Config data written to %s", flagFilename))
	} else if dataOnly || verbose || dryRun {
		dataBytes, err := base64.StdEncoding.DecodeString(unitDetails.Unit.Data)
		if err != nil {
			failOnError(err)
		}
		tprintRaw(string(dataBytes))
	}
}

func displayUnitDetails(unitDetails *goclientnew.Unit) {
	// Create an ExtendedUnit wrapper with just the Unit set
	extendedUnit := &goclientnew.ExtendedUnit{
		Unit: unitDetails,
		// All other fields (Set, Target, Space, etc.) will be nil, causing Extended display to show IDs
	}
	displayExtendedUnitDetails(extendedUnit)
}

func apiGetUnitInSpace(unitID string, spaceID string, selectParam string) (*goclientnew.Unit, error) {
	extendedUnit, err := apiGetExtendedUnitInSpace(unitID, spaceID, selectParam)
	if err != nil {
		return nil, err
	}
	return extendedUnit.Unit, nil
}

func apiGetExtendedUnitInSpace(unitID string, spaceID string, selectParam string) (*goclientnew.ExtendedUnit, error) {
	newParams := &goclientnew.GetUnitParams{}
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID,FromLinkID,BridgeWorkerID,ChangeSetID"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	unitRes, err := cubClientNew.GetUnitWithResponse(ctx, uuid.MustParse(spaceID), uuid.MustParse(unitID), newParams)
	if cubapi.IsAPIError(err, unitRes) {
		return nil, cubapi.InterpretErrorGeneric(err, unitRes)
	}
	return unitRes.JSON200, nil
}

func apiGetUnitFromSlug(slug string, selectParam string) (*goclientnew.Unit, error) {
	return apiGetUnitFromSlugInSpace(slug, selectedSpaceID, selectParam)
}

func apiGetExtendedUnitFromSlug(slug string, selectParam string) (*goclientnew.ExtendedUnit, error) {
	return apiGetExtendedUnitFromSlugInSpace(slug, selectedSpaceID, selectParam)
}

func apiGetUnitFromSlugInSpace(slug string, spaceID string, selectParam string) (*goclientnew.Unit, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		extendedUnit, err := apiGetExtendedUnitInSpace(id.String(), spaceID, selectParam)
		if err != nil {
			return nil, err
		}
		return extendedUnit.Unit, nil
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	units, err := apiListUnits(spaceID, "Slug = '"+slug+"'", selectParam)
	if err != nil {
		return nil, err
	}
	for _, unit := range units {
		if unit.Slug == slug {
			return unit, nil
		}
	}
	return nil, fmt.Errorf("unit %s not found in space %s", slug, spaceID)
}

func apiGetExtendedUnitFromSlugInSpace(slug string, spaceID string, selectParam string) (*goclientnew.ExtendedUnit, error) {
	_, err := uuid.Parse(slug)
	var where string
	if err == nil {
		where = "UnitID='" + slug + "'"
	} else {
		where = "SpaceID='" + spaceID + "' AND Slug='" + slug + "'"
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	units, err := apiListAllUnits(cubapi.NewWhere(where), "", "", "", "", false, selectParam, "", "")
	if err != nil {
		return nil, err
	}
	if len(units) == 1 {
		return units[0], nil
	}
	return nil, fmt.Errorf("unit %s not found in space %s", slug, spaceID)
}
