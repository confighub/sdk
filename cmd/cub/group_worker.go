// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

const groupWorkerAuthorization = `An organization admin or manager may change a group's workers. So may a member of the group
who has Manage permission on the worker. An admin or manager who is not a member names the
group by ID, since only members can look a group up by slug.`

var groupAddWorkerCmd = &cobra.Command{
	Use:   "add-worker <group> <worker>",
	Short: "Add a worker to a group",
	Args:  cobra.ExactArgs(2),
	Long: getCommandHelp(`Add a worker's bot user to a group, so that the group's grants in entities' Permissions
apply to the worker. Other users' groups are managed in the identity provider. The worker is
named as <space>/<slug>, by ID, or by a slug only one space holds. The worker picks up the
change the next time it authenticates.

`+groupWorkerAuthorization+`

Examples:
`+"```"+`
  # Add a worker to a group
  cub group add-worker my-group my-space/my-worker
`+"```"+`
`, ""),
	RunE: func(_ *cobra.Command, args []string) error {
		return changeGroupWorker(args, func(ctx context.Context, groupID, userID uuid.UUID) (*goclientnew.User, error) {
			res, err := cubClientNew.AddGroupBotUserWithResponse(ctx, groupID, userID)
			if cubapi.IsAPIError(err, res) {
				return nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return res.JSON200, nil
		})
	},
}

var groupRemoveWorkerCmd = &cobra.Command{
	Use:   "remove-worker <group> <worker>",
	Short: "Remove a worker from a group",
	Args:  cobra.ExactArgs(2),
	Long: getCommandHelp(`Remove a worker's bot user from a group. The worker is named as <space>/<slug>, by ID, or
by a slug only one space holds. The worker keeps the group until it next authenticates.

`+groupWorkerAuthorization+`

Examples:
`+"```"+`
  # Remove a worker from a group
  cub group remove-worker my-group my-space/my-worker
`+"```"+`
`, ""),
	RunE: func(_ *cobra.Command, args []string) error {
		return changeGroupWorker(args, func(ctx context.Context, groupID, userID uuid.UUID) (*goclientnew.User, error) {
			res, err := cubClientNew.RemoveGroupBotUserWithResponse(ctx, groupID, userID)
			if cubapi.IsAPIError(err, res) {
				return nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return res.JSON200, nil
		})
	},
}

func init() {
	addStandardDisplayFlags(groupAddWorkerCmd)
	addStandardDisplayFlags(groupRemoveWorkerCmd)
	groupCmd.AddCommand(groupAddWorkerCmd)
	groupCmd.AddCommand(groupRemoveWorkerCmd)
}

// changeGroupWorker resolves the group and the worker's bot user named by args and applies change,
// displaying the bot user it returns.
func changeGroupWorker(args []string, change func(context.Context, uuid.UUID, uuid.UUID) (*goclientnew.User, error)) error {
	groupID, err := groupIDForMembership(args[0])
	if err != nil {
		return err
	}
	worker, err := resolveWorker(args[1], "", idSelect("BridgeWorkerID")+",UserID")
	if err != nil {
		return err
	}
	if worker.BridgeWorker == nil || worker.BridgeWorker.UserID == nil || *worker.BridgeWorker.UserID == uuid.Nil {
		return fmt.Errorf("worker %s has no bot user", args[1])
	}
	user, err := change(ctx, groupID, *worker.BridgeWorker.UserID)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("the response has no user")
	}
	displayGetResults(user, displayBotUserGroups)
	return nil
}

func displayBotUserGroups(user *goclientnew.User) {
	view := tableView()
	view.Append([]string{"User ID", user.UserID.String()})
	view.Append([]string{"Display Name", user.DisplayName})
	groupIDs := make([]string, 0, len(user.GroupIDs))
	for _, groupID := range user.GroupIDs {
		groupIDs = append(groupIDs, groupID.String())
	}
	view.Append([]string{"Group IDs", strings.Join(groupIDs, ", ")})
	view.Render()
}

// groupIDForMembership takes a group ID as it is and resolves anything else as a slug. A slug
// resolves only for a member of the group, but an admin or manager need not be one.
func groupIDForMembership(ref string) (uuid.UUID, error) {
	if groupID, err := uuid.Parse(ref); err == nil {
		return groupID, nil
	}
	group, err := resolveGroup(ref, "GroupID")
	if err != nil {
		return uuid.Nil, err
	}
	return group.Group.GroupID, nil
}
