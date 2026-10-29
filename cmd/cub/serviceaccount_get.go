// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var serviceAccountGetCmd = &cobra.Command{
	Use:   "get <name or id>",
	Short: "Get details about a service account",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a ServiceAccount entity, including the user it acts as.

With --verbose, it also shows what the service account can reach: its Groups, and every entity
whose Permissions name it or one of its Groups, with the actions each grants and which come
through a Group. Only the entities you can see are searched. Permissions on the service account
itself are its Permissions above, not a grant to it, and are left out. Of more than four grants
on entities of one type, in one Space, that give the same permissions -- the Tags an account that
publishes is granted Manage on, for one -- the newest three are listed and the rest counted.

Examples:
`+"```"+`
  # Get service account details in table format
  cub serviceaccount get deploy-bot

  # Get the ID of the user the service account acts as, to grant it access
  cub serviceaccount get deploy-bot -o jq=.ServiceAccount.UserID

  # Also show its Groups and every entity that grants it, or one of its Groups, permissions
  cub serviceaccount get deploy-bot --verbose
`+"```"+`
`, ""),
	RunE: serviceAccountGetCmdRun,
}

func init() {
	addStandardGetFlags(serviceAccountGetCmd)
	serviceAccountCmd.AddCommand(serviceAccountGetCmd)
}

func serviceAccountGetCmdRun(cmd *cobra.Command, args []string) error {
	extendedServiceAccount, err := resolveServiceAccount(args[0], selectFields)
	if err != nil {
		return err
	}
	displayGetResults(extendedServiceAccount, displayExtendedServiceAccountDetails)
	if verbose && !quiet && !isAlternativeOutput() && extendedServiceAccount.ServiceAccount != nil {
		return displayServiceAccountGrants(extendedServiceAccount.ServiceAccount)
	}
	return nil
}

// displayServiceAccountGrants prints the service account's Groups and the entities whose
// Permissions name it or one of them.
func displayServiceAccountGrants(serviceAccount *goclientnew.ServiceAccount) error {
	if serviceAccount.UserID == (goclientnew.UUID{}) {
		return fmt.Errorf("the service account's UserID is needed to find what it has been granted; add it to --select")
	}
	user, err := cubapi.ResolveUser(ctx, cubClient, cubapi.NewRef("", serviceAccount.UserID.String()), cubapi.ResolveOpts{Select: "UserID,GroupIDs"})
	if err != nil {
		return fmt.Errorf("reading the service account's user: %w", err)
	}
	var groupIDs []goclientnew.UUID
	if user.User != nil {
		groupIDs = user.User.GroupIDs
	}
	grants := cubapi.ListGrantsTo(ctx, cubClient, serviceAccount.UserID, groupIDs)

	groupNames := map[goclientnew.UUID]string{}
	if len(groupIDs) > 0 {
		groups, err := cubapi.ListGroups(ctx, cubClient, cubapi.Where{}.In("GroupID", groupIDs), cubapi.ListOpts{Select: "GroupID,Slug"})
		if err != nil {
			return fmt.Errorf("reading the service account's groups: %w", err)
		}
		for _, group := range groups {
			if group.Group != nil {
				groupNames[group.Group.GroupID] = group.Group.Slug
			}
		}
	}
	groupName := func(id goclientnew.UUID) string {
		if name, ok := groupNames[id]; ok {
			return name
		}
		return id.String()
	}

	var spaceIDs []goclientnew.UUID
	seenSpaces := map[goclientnew.UUID]bool{}
	for _, grant := range grants.Grants {
		if grant.SpaceID != (goclientnew.UUID{}) && !seenSpaces[grant.SpaceID] {
			seenSpaces[grant.SpaceID] = true
			spaceIDs = append(spaceIDs, grant.SpaceID)
		}
	}
	spaceSlugs := map[goclientnew.UUID]string{}
	if len(spaceIDs) > 0 {
		spaces, err := cubapi.ListSpaces(ctx, cubClient, cubapi.Where{}.In("SpaceID", spaceIDs), cubapi.ListOpts{Select: "SpaceID,Slug"})
		if err != nil {
			return fmt.Errorf("reading the spaces of the entities that grant the service account permissions: %w", err)
		}
		for _, space := range spaces {
			if space.Space != nil {
				spaceSlugs[space.Space.SpaceID] = space.Space.Slug
			}
		}
	}

	tprintRaw("")
	groupList := make([]string, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		groupList = append(groupList, groupName(groupID))
	}
	if len(groupList) == 0 {
		tprint("Groups: none")
	} else {
		tprint("Groups: %s", strings.Join(groupList, ", "))
	}

	tprintRaw("")
	tprintRaw("Granted On:")
	tprintRaw("-----------")
	permissionsOf := func(grant cubapi.Grant) string {
		actions := make([]string, 0, len(grant.Actions))
		for _, action := range grant.Actions {
			if action.Direct {
				actions = append(actions, action.Action)
				continue
			}
			via := make([]string, 0, len(action.GroupIDs))
			for _, groupID := range action.GroupIDs {
				via = append(via, groupName(groupID))
			}
			actions = append(actions, fmt.Sprintf("%s (via %s)", action.Action, strings.Join(via, ", ")))
		}
		return strings.Join(actions, ", ")
	}

	// Listed by entity type in the order the search returns the types, and then by Space and name.
	typeRank := map[string]int{}
	others := make([]cubapi.Grant, 0, len(grants.Grants))
	for _, grant := range grants.Grants {
		if _, ok := typeRank[grant.EntityType]; !ok {
			typeRank[grant.EntityType] = len(typeRank)
		}
		if grant.EntityType != "ServiceAccount" || grant.ID != serviceAccount.ServiceAccountID {
			others = append(others, grant)
		}
	}
	slices.SortStableFunc(others, func(a, b cubapi.Grant) int {
		if rank := typeRank[a.EntityType] - typeRank[b.EntityType]; rank != 0 {
			return rank
		}
		return strings.Compare(spaceSlugs[a.SpaceID], spaceSlugs[b.SpaceID])
	})
	shown := 0
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Type", "Entity", "Permissions"})
	}
	for _, group := range groupSimilarGrants(others) {
		visible, hidden := newestGrants(group)
		for _, grant := range visible {
			name := grant.Name
			if grant.SpaceID != (goclientnew.UUID{}) {
				name = spaceSlugs[grant.SpaceID] + "/" + name
			}
			table.Append([]string{grant.EntityType, name, permissionsOf(grant)})
			shown++
		}
		if hidden > 0 {
			more := fmt.Sprintf("%d more", hidden)
			if group[0].SpaceID != (goclientnew.UUID{}) {
				more += " in " + spaceSlugs[group[0].SpaceID]
			}
			table.Append([]string{group[0].EntityType, more, permissionsOf(group[0])})
		}
	}
	if shown == 0 {
		tprint("Nothing grants it or its Groups permissions.")
	} else {
		table.Render()
	}
	for _, unreadable := range grants.Unreadable {
		fmt.Fprintf(os.Stderr, "Could not search %s: %v\n", unreadable.EntityType, unreadable.Err)
	}
	return nil
}

// grantsShownPerGroup is how many grants of a group of similar ones are listed before the
// rest are counted instead.
const grantsShownPerGroup = 3

// groupSimilarGrants gathers grants on entities of one type, in one Space, that give the same
// actions through the same subjects, each group where its first grant was. An account is granted
// Manage on whatever it creates, so one that publishes has a Release Tag for every release, which
// would otherwise crowd out the grants made to it on purpose; those give something else, and so
// are grouped apart.
func groupSimilarGrants(grants []cubapi.Grant) [][]cubapi.Grant {
	var groups [][]cubapi.Grant
	index := map[string]int{}
	for _, grant := range grants {
		key := grant.EntityType + "\x00" + grant.SpaceID.String()
		for _, action := range grant.Actions {
			key += fmt.Sprintf("\x00%s:%t:%v", action.Action, action.Direct, action.GroupIDs)
		}
		if i, ok := index[key]; ok {
			groups[i] = append(groups[i], grant)
			continue
		}
		index[key] = len(groups)
		groups = append(groups, []cubapi.Grant{grant})
	}
	return groups
}

// newestGrants is the grants of a group to list, the newest in the group's order, and how many
// are left out. A single grant left out takes the line its count would, so it is listed instead.
func newestGrants(group []cubapi.Grant) ([]cubapi.Grant, int) {
	if len(group) <= grantsShownPerGroup+1 {
		return group, 0
	}
	byAge := slices.Clone(group)
	slices.SortStableFunc(byAge, func(a, b cubapi.Grant) int { return b.CreatedAt.Compare(a.CreatedAt) })
	newest := map[goclientnew.UUID]bool{}
	for _, grant := range byAge[:grantsShownPerGroup] {
		newest[grant.ID] = true
	}
	shown := make([]cubapi.Grant, 0, grantsShownPerGroup)
	for _, grant := range group {
		if newest[grant.ID] {
			shown = append(shown, grant)
		}
	}
	return shown, len(group) - grantsShownPerGroup
}

func displayExtendedServiceAccountDetails(extendedServiceAccount *goclientnew.ExtendedServiceAccount) {
	renderServiceAccountDetails(extendedServiceAccount.ServiceAccount, extendedServiceAccount.Permissions)
}

func displayServiceAccountDetails(serviceAccount *goclientnew.ServiceAccount) {
	renderServiceAccountDetails(serviceAccount, nil)
}

func renderServiceAccountDetails(serviceAccount *goclientnew.ServiceAccount, expanded *goclientnew.ExtendedPermissions) {
	view := tableView()
	view.Append([]string{"ID", serviceAccount.ServiceAccountID.String()})
	view.Append([]string{"Name", serviceAccount.Slug})
	view.Append([]string{"Display Name", serviceAccount.DisplayName})
	view.Append([]string{"User ID", serviceAccount.UserID.String()})
	view.Append([]string{"Org Role", serviceAccount.OrgRole})
	view.Append([]string{"Created At", serviceAccount.CreatedAt.String()})
	view.Append([]string{"Updated At", serviceAccount.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(serviceAccount.Labels)})
	view.Append([]string{"Delete Gates", deleteGatesToString(serviceAccount.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(serviceAccount.Annotations)})
	view.Append([]string{"Permissions", permissionsToString(serviceAccount.Permissions, expanded)})
	view.Append([]string{"Organization ID", serviceAccount.OrganizationID.String()})
	view.Render()
}
