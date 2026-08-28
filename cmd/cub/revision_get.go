// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var revisionGetCmd = &cobra.Command{
	Use:   "get <unit-slug> <revision-num>",
	Short: "Get details about a revision",
	Args:  cobra.ExactArgs(2),
	Long: getCommandHelp(`Get detailed information about a specific revision of a unit including its configuration data and metadata.

Examples:
`+"```"+`
  # Get details about a specific revision in JSON format
  cub revision get --space my-space -o json my-deployment 3

  # Get only the configuration data of a revision (use the dedicated subcommand)
  cub revision data --space my-space my-ns 2

  # Show the mutations recorded on a revision
  cub revision get --space my-space -o mutations my-deployment 3
`+"```"+`
`, ""),
	RunE: revisionGetCmdRun,
}

func init() {
	addStandardGetFlags(revisionGetCmd)
	enableDisplayMutationsFlag(revisionGetCmd)
	revisionGetCmd.Flags().BoolVar(&dataOnly, "data-only", false, "show config data without other response details")
	_ = revisionGetCmd.Flags().MarkDeprecated("data-only", "use 'cub revision data <unit> <revision-num>'")
	revisionCmd.AddCommand(revisionGetCmd)
}

func revisionGetCmdRun(cmd *cobra.Command, args []string) error {
	unit, err := apiGetUnitFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}
	num, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return err
	}
	rev, err := apiGetExtendedRevisionFromNumber(num, unit.UnitID.String(), selectFields)
	if err != nil {
		return err
	}

	// -o mutations replaces the default summary with the mutations recorded on the
	// revision. The deprecated --display-mutations flag remains additive and is
	// handled inside displayExtendedRevisionDetails.
	if effectiveOutput().Kind == OutputMutations {
		displayMutationsForRevision(rev.Revision)
		return nil
	}

	displayGetResults(rev, displayExtendedRevisionDetails)
	return nil
}

func displayRevisionDetails(rev *goclientnew.Revision) {
	// Create an ExtendedRevision wrapper with just the Revision set
	extendedRevision := &goclientnew.ExtendedRevision{
		Revision: rev,
		// All other fields (Unit, Space, etc.) will be nil, causing Extended display to show IDs
	}
	displayExtendedRevisionDetails(extendedRevision)
}

func displayExtendedRevisionDetails(extendedRev *goclientnew.ExtendedRevision) {
	rev := extendedRev.Revision
	if !dataOnly {
		view := tableView()
		view.Append([]string{"ID", rev.RevisionID.String()})
		view.Append([]string{"Revision Num", fmt.Sprintf("%d", rev.RevisionNum)})

		// Show Unit slug instead of Unit ID when available
		if extendedRev.Unit != nil {
			view.Append([]string{"Unit", extendedRev.Unit.Slug})
		} else if rev.UnitSlug != "" {
			view.Append([]string{"Unit", rev.UnitSlug})
		} else {
			view.Append([]string{"Unit ID", rev.UnitID.String()})
		}

		// Show Space slug instead of Space ID when available
		if revSpace := revisionSpaceSlug(extendedRev); revSpace != "" {
			view.Append([]string{"Space", revSpace})
		} else {
			view.Append([]string{"Space ID", rev.SpaceID.String()})
		}

		view.Append([]string{"Source", rev.Source})
		view.Append([]string{"Description", rev.Description})
		view.Append([]string{"Created At", rev.CreatedAt.String()})
		view.Append([]string{"Updated At", rev.UpdatedAt.String()})

		// Show the username instead of the User ID when available. Automated changes -- triggers,
		// resolve -- are recorded with the nil UserID, which names nobody, so neither row is shown.
		if extendedRev.User != nil {
			view.Append([]string{"User", extendedRev.User.Username})
		} else if rev.UserID != uuid.Nil {
			view.Append([]string{"User ID", rev.UserID.String()})
		}
		if rev.UserAgent != "" {
			view.Append([]string{"User Agent", rev.UserAgent})
		}

		if changeSet := revisionChangeSet(extendedRev); changeSet != "" {
			view.Append([]string{"ChangeSet", changeSet})
		}
		if changeOrders := revisionChangeOrders(extendedRev); changeOrders != "" {
			view.Append([]string{"ChangeOrders", changeOrders})
		}
		if tags := revisionTags(extendedRev); tags != "" {
			view.Append([]string{"Tags", tags})
		}
		if releases := revisionReleases(extendedRev); releases != "" {
			view.Append([]string{"Releases", releases})
		}

		if rev.DataHash != "" {
			view.Append([]string{"Data Hash", rev.DataHash})
		}
		if rev.DataSize != 0 {
			view.Append([]string{"Data Size", fmt.Sprintf("%d", rev.DataSize)})
		}

		if len(rev.ApplyGates) != 0 {
			view.Append([]string{"Apply Gates", applyGatesToString(rev.ApplyGates)})
		}
		if len(rev.ApplyWarnings) != 0 {
			view.Append([]string{"Apply Warnings", applyGatesToString(rev.ApplyWarnings)})
		}
		if len(rev.ApprovedBy) != 0 {
			view.Append([]string{"Approved By", strings.Join(resolveUsernames(rev.ApprovedBy), ", ")})
		}

		// The annotations and the withheld mutations are lists, too long for a row apiece: the
		// count says whether there is anything to look at, and --verbose prints it.
		pathAnnotations := 0
		if rev.PathAnnotations != nil {
			pathAnnotations = len(*rev.PathAnnotations)
		}
		if pathAnnotations > 0 {
			view.Append([]string{"Path Annotations", fmt.Sprintf("%d %s", pathAnnotations, plural("resource", pathAnnotations))})
		}
		conflicts := 0
		if rev.Conflicts != nil {
			conflicts = len(*rev.Conflicts)
		}
		if conflicts > 0 {
			view.Append([]string{"Conflicts", fmt.Sprintf("%d %s", conflicts, plural("conflict", conflicts))})
		}

		view.Append([]string{"Organization ID", rev.OrganizationID.String()})
		view.Render()

		if conflicts > 0 {
			tprintRaw("")
			tprintRaw("Conflicts:")
			tprintRaw("----------")
			displayConflicts(*rev.Conflicts)
		}
		if pathAnnotations > 0 && verbose {
			tprintRaw("")
			tprintRaw("Path Annotations:")
			tprintRaw("-----------------")
			displayJSON(rev.PathAnnotations)
		}

		tprintRaw("---")
		mutationSources, msErr := fetchRevisionMutationSources(rev.SpaceID, rev.UnitID, rev.RevisionID)
		failOnError(msErr)
		if mutationSources != nil && len(*mutationSources) != 0 {
			tprintRaw("Mutation Sources:")
			if shouldDisplayMutations() {
				lookupMutationsUnitID = rev.UnitID.String()
				displayResourceMutationList(mutationSources, true, 0, "", "")
			} else {
				displayJSON(mutationSources)
			}
			tprintRaw("---")
		}
	}
	revData, dataErr := fetchRevisionData(rev.SpaceID, rev.UnitID, rev.RevisionID)
	failOnError(dataErr)
	tprintRaw(revData)
}

// resolveUsernames names the users that approved a Revision, falling back to the id for a user
// that cannot be read -- an approver removed from the organization since approving, for instance.
func resolveUsernames(userIDs []goclientnew.UUID) []string {
	names := make([]string, 0, len(userIDs))
	for _, userID := range userIDs {
		user, err := apiGetUser(userID.String())
		if err != nil || user == nil || user.Username == "" {
			names = append(names, userID.String())
			continue
		}
		names = append(names, user.Username)
	}
	sort.Strings(names)
	return names
}

func apiGetRevision(revisionID string, unitID string, selectParam string) (*goclientnew.Revision, error) {
	extendedRev, err := apiGetExtendedRevision(revisionID, unitID, selectParam)
	if err != nil {
		return nil, err
	}
	return extendedRev.Revision, nil
}

func apiGetExtendedRevision(revisionID string, unitID string, selectParam string) (*goclientnew.ExtendedRevision, error) {
	newParams := &goclientnew.GetExtendedRevisionParams{}
	include := "UserID,UnitID,SpaceID,ChangeSetID,Tags,ChangeOrders,Releases"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	revRes, err := cubClientNew.GetExtendedRevisionWithResponse(ctx,
		uuid.MustParse(selectedSpaceID),
		uuid.MustParse(unitID),
		uuid.MustParse(revisionID),
		newParams,
	)
	if cubapi.IsAPIError(err, revRes) {
		return nil, cubapi.InterpretErrorGeneric(err, revRes)
	}
	if revRes.JSON200.Revision.SpaceID.String() != selectedSpaceID {
		return nil, fmt.Errorf("SERVER DIDN'T CHECK: revision %s not found", revisionID)
	}

	return revRes.JSON200, nil
}

func apiGetRevisionFromNumber(revNo int64, unitID string, selectParam string) (*goclientnew.Revision, error) {
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	revisions, err := apiListRevisions(selectedSpaceID, unitID, fmt.Sprintf("RevisionNum = %d", revNo), selectParam, "")
	if err != nil {
		return nil, err
	}
	for _, extendedRev := range revisions {
		if int64(extendedRev.Revision.RevisionNum) == revNo {
			return extendedRev.Revision, nil
		}
	}
	return nil, fmt.Errorf("rev %d of unit %s not found in space %s", revNo, unitID, selectedSpaceSlug)
}

func apiGetRevisionFromNumberInSpace(revNo int64, unitID string, spaceID string, selectParam string) (*goclientnew.Revision, error) {
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	revisions, err := apiListRevisions(spaceID, unitID, fmt.Sprintf("RevisionNum = %d", revNo), selectParam, "")
	if err != nil {
		return nil, err
	}
	for _, extendedRev := range revisions {
		if int64(extendedRev.Revision.RevisionNum) == revNo {
			return extendedRev.Revision, nil
		}
	}
	return nil, fmt.Errorf("rev %d of unit %s not found in space %s", revNo, unitID, spaceID)
}

func apiGetExtendedRevisionFromNumber(revNo int64, unitID string, selectParam string) (*goclientnew.ExtendedRevision, error) {
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	revisions, err := apiListRevisions(selectedSpaceID, unitID, fmt.Sprintf("RevisionNum = %d", revNo), selectParam, "")
	if err != nil {
		return nil, err
	}
	for _, extendedRev := range revisions {
		if int64(extendedRev.Revision.RevisionNum) == revNo {
			return extendedRev, nil
		}
	}
	return nil, fmt.Errorf("rev %d of unit %s not found in space %s", revNo, unitID, selectedSpaceSlug)
}
