// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var revisionGetCmd = &cobra.Command{
	Use:   "get <unit-slug> <revision-num>",
	Short: "Get details about a revision",
	Args:  cobra.ExactArgs(2),
	Long: `Get detailed information about a specific revision of a unit including its configuration data and metadata.

Examples:
  # Get details about a specific revision in JSON format
  cub revision get --space my-space --json my-deployment 3

  # Get only the configuration data of a revision
  cub revision get --space my-space --data-only my-ns 2

  # Get extended information about a revision
  cub revision get --space my-space --json --extended my-ns 1`,
	RunE: revisionGetCmdRun,
}

func init() {
	addStandardGetFlags(revisionGetCmd)
	revisionGetCmd.Flags().BoolVar(&dataOnly, "data-only", false, "show config data without other response details")
	revisionCmd.AddCommand(revisionGetCmd)
}

func revisionGetCmdRun(cmd *cobra.Command, args []string) error {
	unit, err := apiGetUnitFromSlug(args[0])
	if err != nil {
		return err
	}
	num, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return err
	}
	rev, err := apiGetRevisionFromNumber(num, unit.UnitID.String())
	if err != nil {
		return err
	}
	if extended {
		revisionExtended, err := apiGetRevisionExtended(unit.UnitID.String(), rev.RevisionID.String())
		if err != nil {
			return err
		}
		displayGetResults(revisionExtended, displayRevisionExtendedDetails)
		return nil
	}

	displayGetResults(rev, displayRevisionDetails)
	return nil
}

func displayRevisionDetails(rev *goclientnew.Revision) {
	if !dataOnly {
		view := tableView()
		view.Append([]string{"ID", rev.RevisionID.String()})
		view.Append([]string{"Unit ID", rev.UnitID.String()})
		view.Append([]string{"Revision Num", fmt.Sprintf("%d", rev.RevisionNum)})
		view.Append([]string{"Source", rev.Source})
		view.Append([]string{"Description", rev.Description})
		view.Append([]string{"Created At", rev.CreatedAt.String()})
		view.Append([]string{"Live At", rev.LiveAt.String()})
		view.Append([]string{"User ID", rev.UserID.String()})
		view.Append([]string{"Space ID", rev.SpaceID.String()})
		view.Append([]string{"Organization ID", rev.OrganizationID.String()})
		if rev.ApplyGates != nil && len(rev.ApplyGates) != 0 {
			gates := ""
			for gate, failed := range rev.ApplyGates {
				if failed {
					gates += gate + " "
				}
			}
			view.Append([]string{"Apply Gates", strings.TrimSpace(gates)})
		}
		view.Render()
		tprint("---")
		if len(*rev.MutationSources) != 0 {
			tprint("Mutation Sources:")
			// TODO: Make this prettier
			displayJSON(rev.MutationSources)
			tprint("---")
		}
	}
	data, err := base64.StdEncoding.DecodeString(rev.Data)
	failOnError(err)
	tprint(string(data))
}

func displayRevisionExtendedDetails(revisionExtendedDetails *goclientnew.RevisionExtended) {
	displayRevisionDetails(revisionExtendedDetails.Revision)
	view := tableView()
	if revisionExtendedDetails.Username != "" {
		view.Append([]string{"Username", revisionExtendedDetails.Username})
	}
	if len(revisionExtendedDetails.ApprovedByUsers) != 0 {
		approvers := ""
		for _, approver := range revisionExtendedDetails.ApprovedByUsers {
			approvers += " " + approver
		}
		view.Append([]string{"Approved By", strings.TrimSpace(approvers)})
	}
	view.Render()
}

func apiGetRevision(revisionID string, unitID string) (*goclientnew.Revision, error) {
	revRes, err := cubClientNew.GetExtendedRevisionWithResponse(ctx,
		uuid.MustParse(selectedSpaceID),
		uuid.MustParse(unitID),
		uuid.MustParse(revisionID),
		&goclientnew.GetExtendedRevisionParams{},
	)
	if IsAPIError(err, revRes) {
		return nil, InterpretErrorGeneric(err, revRes)
	}
	if revRes.JSON200.Revision.SpaceID.String() != selectedSpaceID {
		return nil, fmt.Errorf("SERVER DIDN'T CHECK: revision %s not found", revisionID)
	}

	return revRes.JSON200.Revision, nil
}

func apiGetRevisionExtended(unitID string, revisionID string) (*goclientnew.RevisionExtended, error) {
	revRes, err := cubClientNew.GetRevisionExtendedWithResponse(context.Background(),
		uuid.MustParse(selectedSpaceID),
		uuid.MustParse(unitID),
		uuid.MustParse(revisionID),
	)
	if IsAPIError(err, revRes) {
		return nil, InterpretErrorGeneric(err, revRes)
	}
	return revRes.JSON200, nil
}

func apiGetRevisionFromNumber(revNo int64, unitID string) (*goclientnew.Revision, error) {
	revisions, err := apiListRevisions(selectedSpaceID, unitID, fmt.Sprintf("RevisionNum = %d", revNo))
	if err != nil {
		return nil, err
	}
	for _, rev := range revisions {
		if int64(rev.RevisionNum) == revNo {
			return rev, nil
		}
	}
	return nil, fmt.Errorf("rev %d of unit %s not found in space %s", revNo, unitID, selectedSpaceSlug)
}
