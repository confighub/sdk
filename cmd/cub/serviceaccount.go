// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

// serviceAccountCmd is organization-level, so it does not use spacePreRunE. It only needs
// globalPreRun to initialize the API client.
var serviceAccountCmd = &cobra.Command{
	Use:               "serviceaccount",
	Short:             "Service account commands",
	Long:              getServiceAccountCommandGroupHelp(),
	PersistentPreRunE: globalPreRun,
}

func getServiceAccountCommandGroupHelp() string {
	baseHelp := `The serviceaccount subcommands manage ServiceAccount entities.

A service account is an identity for something that is not a person, such as a CI pipeline or an
agent. It has an identity of its own in ConfigHub, created with it, which is not linked to any person
or to any account in the identity provider; the only way to authenticate as it is with one of its
keys. The identity's ID is the service account's UserID.

Grant a service account access by naming it in an entity's permissions
(--permission View:serviceaccount:<name>), by adding it to a group
(cub group add <group> serviceaccount:<name>), or by giving it an organization role with
--org-role. Who may manage the service account is set by its own permissions.

Anyone in the organization may create a service account and becomes its manager. The organization
role given to it cannot be above the caller's own, and changing the role requires Manage permission
on the service account.

Deleting a service account deletes its keys and disables its identity, which is kept so that the
changes it made stay recorded under its UserID.`

	agentContext := `Service accounts are organization-level, so these commands take no --space.

To grant a service account access to an entity, name it in the entity's permissions, which adds
its user:
  cub space update --patch my-space --permission View:serviceaccount:my-bot`

	return getCommandHelp(baseHelp, agentContext)
}

func init() {
	rootCmd.AddCommand(serviceAccountCmd)
	addExplainCmd(serviceAccountCmd, "ServiceAccount")
}
