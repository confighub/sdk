// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import "github.com/spf13/cobra"

var changeworkflowCmd = &cobra.Command{
	Use:   "changeworkflow",
	Short: "ChangeWorkflow commands",
	Long: getCommandHelp(`The changeworkflow subcommands manage ChangeWorkflow definitions.

A ChangeWorkflow says how a change is promoted: the ordered stages it moves through, which Spaces
each stage selects, and the gates that have to pass before it enters one.

It is not an entity of its own. A definition is a YAML document held in a Kubernetes/YAML Unit, so
it is versioned, labeled, cloned and diffed like any other configuration, and the unit subcommands
read and write it:

  cub unit data --space workflows myapp-main-line
  cub unit update --space workflows myapp-main-line workflow.yaml

"cub changeorder create --change-workflow" names the Unit and pins the Revision in force when the
change order is created, so a workflow edited part way through a rollout cannot change the rules a
change already started under.
`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(changeworkflowCmd)
	rootCmd.AddCommand(changeworkflowCmd)
}
