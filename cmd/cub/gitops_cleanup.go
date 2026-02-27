// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var gitopsCleanupCmd = &cobra.Command{
	Use:   "cleanup <target-slug>",
	Short: "Clean up the discover unit for a target",
	Long: getCommandHelp(`Clean up the discover unit created by the gitops discover command for a target.

Examples:
`+"```"+`
  cub gitops cleanup --space my-space my-k8s-target
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: gitopsCleanupCmdRun,
}

func init() {
	addStandardDisplayFlags(gitopsCleanupCmd)
	gitopsCmd.AddCommand(gitopsCleanupCmd)
}

func gitopsCleanupCmdRun(cmd *cobra.Command, args []string) error {
	// Parse target
	target, err := parseEntityIdentifierSingleAsEntity[goclientnew.Target](
		args[0],
		EntityTypeTarget,
		"*",
		apiGetTargetFromSlugInSpaceCore,
		func(t *goclientnew.Target) string { return t.TargetID.String() },
	)
	if err != nil {
		return fmt.Errorf("failed to resolve target: %w", err)
	}

	spaceID := uuid.MustParse(selectedSpaceID)

	// Generate the discover unit slug
	slug := discoverUnitSlug(target.Slug, selectedSpaceID)

	// Try to get the discover unit
	existingUnit, err := apiGetUnitFromSlug(slug, "*")
	if err != nil {
		tprint("No discover unit found for target %s", target.Slug)
		return nil
	}

	// Delete the discover unit
	deleteRes, err := cubClientNew.DeleteUnitWithResponse(ctx, spaceID, existingUnit.UnitID)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("unit", slug, existingUnit.UnitID.String(), deleteRes)
	return nil
}
