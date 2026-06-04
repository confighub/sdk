// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

// AnnotationUpstreamSpaceID is the Space annotation "cub variant create" stamps
// on a new variant space, recording the UUID of the upstream space it was cloned
// from. "cub variant promote" reads it to find the space to promote from.
const AnnotationUpstreamSpaceID = "UpstreamSpaceID"

// variantCmd is inherently multi-space: it reads from an upstream space and
// writes to a freshly created downstream space. It therefore does NOT use the
// standard spacePreRunE (which resolves a single default space). It only needs
// globalPreRun to initialize the API client.
var variantCmd = &cobra.Command{
	Use:               "variant",
	Short:             "Variant commands",
	Long:              getVariantCommandGroupHelp(),
	PersistentPreRunE: globalPreRun,
}

func getVariantCommandGroupHelp() string {
	baseHelp := `The variant subcommands create and manage variants of a space.

A variant is a clone of an upstream space (and its units) into a new downstream space,
customized for a different environment, region, tenant, or experiment. The clone is linked
to the upstream so it can later be upgraded as the upstream changes.`

	agentContext := `A variant clones a whole space and its units in one step, equivalent to running
'cub space create' in bulk mode followed by 'cub unit create' in bulk mode.

The cloned space keeps its WhereTrigger, TriggerFilterID, Permissions, and DeleteGates from the
upstream space. To customize units automatically as they are cloned, define PostClone triggers
and select them via the upstream space's WhereTrigger or TriggerFilterID so they are copied to the
downstream space and run during the clone. Trigger arguments can reference space metadata in Go
templates, such as "template:{{.SpaceLabels.Region}}". Make any further changes after cloning.`

	return getCommandHelp(baseHelp, agentContext)
}

func init() {
	rootCmd.AddCommand(variantCmd)
}
