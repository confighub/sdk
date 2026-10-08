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

const groupMemberAuthorization = `An organization admin or manager may change a group's service accounts. So may a member of the
group who has Manage permission on the service account. An admin or manager who is not a member
names the group by ID, since only members can look a group up by slug.`

var groupAddCmd = &cobra.Command{
	Use:   "add <group> <member>",
	Short: "Add a service account to a group",
	Args:  cobra.ExactArgs(2),
	Long: getCommandHelp(`Add a member to a group, so that the group's grants in entities' Permissions apply to it.
The member is named as in --permission: serviceaccount:<name> names a service account. People's
groups are managed in the identity provider, so a user cannot be added here. A service account
picks up the change the next time it authenticates.

`+groupMemberAuthorization+`

Examples:
`+"```"+`
  # Add a service account to a group
  cub group add my-group serviceaccount:deploy-bot
`+"```"+`
`, ""),
	RunE: func(_ *cobra.Command, args []string) error {
		return changeGroupMember(args, func(ctx context.Context, groupID, userID uuid.UUID) (*goclientnew.User, error) {
			res, err := cubClientNew.AddGroupBotUserWithResponse(ctx, groupID, userID)
			if cubapi.IsAPIError(err, res) {
				return nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return res.JSON200, nil
		})
	},
}

var groupRemoveCmd = &cobra.Command{
	Use:   "remove <group> <member>",
	Short: "Remove a service account from a group",
	Args:  cobra.ExactArgs(2),
	Long: getCommandHelp(`Remove a member from a group. The member is named as in --permission: serviceaccount:<name>
names a service account. A service account keeps the group until it next authenticates.

`+groupMemberAuthorization+`

Examples:
`+"```"+`
  # Remove a service account from a group
  cub group remove my-group serviceaccount:deploy-bot
`+"```"+`
`, ""),
	RunE: func(_ *cobra.Command, args []string) error {
		return changeGroupMember(args, func(ctx context.Context, groupID, userID uuid.UUID) (*goclientnew.User, error) {
			res, err := cubClientNew.RemoveGroupBotUserWithResponse(ctx, groupID, userID)
			if cubapi.IsAPIError(err, res) {
				return nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return res.JSON200, nil
		})
	},
}

func init() {
	addStandardDisplayFlags(groupAddCmd)
	addStandardDisplayFlags(groupRemoveCmd)
	groupCmd.AddCommand(groupAddCmd)
	groupCmd.AddCommand(groupRemoveCmd)
}

// groupMemberUserID resolves the member named by a group add or remove to the user it adds or
// removes. The member is named with the subject syntax of --permission, of which only a service
// account can be a member added in ConfigHub.
func groupMemberUserID(member string) (uuid.UUID, error) {
	if ref, ok := strings.CutPrefix(member, permissionServiceAccountPrefix); ok {
		if ref == "" {
			return uuid.Nil, fmt.Errorf("no service account after %q", permissionServiceAccountPrefix)
		}
		serviceAccount, err := resolveServiceAccount(ref, "ServiceAccountID,Slug,UserID")
		if err != nil {
			return uuid.Nil, err
		}
		return serviceAccount.ServiceAccount.UserID, nil
	}
	if strings.HasPrefix(member, permissionGroupPrefix) {
		return uuid.Nil, fmt.Errorf("%q names a group, and a group cannot be a member of a group", member)
	}
	return uuid.Nil, fmt.Errorf("%q names a user; people's groups are managed in the identity provider. "+
		"Name a service account as %s<name>", member, permissionServiceAccountPrefix)
}

// changeGroupMember resolves the group and the member named by args and applies change,
// displaying the member's user it returns.
func changeGroupMember(args []string, change func(context.Context, uuid.UUID, uuid.UUID) (*goclientnew.User, error)) error {
	userID, err := groupMemberUserID(args[1])
	if err != nil {
		return err
	}
	groupID, err := groupIDForMembership(args[0])
	if err != nil {
		return err
	}
	user, err := change(ctx, groupID, userID)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("the response has no user")
	}
	displayGetResults(user, displayBotUserGroups)
	return nil
}
