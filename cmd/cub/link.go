// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var linkCmd = &cobra.Command{
	Use:   "link",
	Short: "Link commands",
	Long: getCommandHelp(`The link subcommands are used to manage links.

Links are explained at https://docs.confighub.com/background/entities/link/.
A guide for how to use links is at https://docs.confighub.com/guide/dependencies/.`, ""),
	PersistentPreRunE: spacePreRunE,
}

var (
	linkUpdateType     string
	linkAutoUpdate     bool
	linkNoAutoUpdate   bool
	linkUseLiveState   bool
	linkNoUseLiveState bool
	linkWhereMutation  string
	linkWhereResource  string
	linkMakeCurrent    bool
)

func addLinkFieldFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&linkUpdateType, "update-type", "", "link update type (NeedsProvides or MergeUnits)")
	cmd.Flags().BoolVar(&linkAutoUpdate, "auto-update", false, "enable automatic downstream unit updates when upstream changes")
	cmd.Flags().BoolVar(&linkNoAutoUpdate, "no-auto-update", false, "disable automatic downstream unit updates")
	cmd.Flags().BoolVar(&linkUseLiveState, "use-live-state", false, "use LiveState of upstream unit instead of Data")
	cmd.Flags().BoolVar(&linkNoUseLiveState, "no-use-live-state", false, "use Data of upstream unit instead of LiveState")
	cmd.Flags().StringVar(&linkWhereMutation, "where-mutation", "", "where expression to filter mutations during merge")
	cmd.Flags().StringVar(&linkWhereResource, "where-resource", "", "where expression to select upstream resources for propagation")
	cmd.Flags().BoolVar(&linkMakeCurrent, "make-current", false, "set link revision numbers to current unit revisions (skips initial merge)")
}

func validateLinkFieldFlags() error {
	if linkAutoUpdate && linkNoAutoUpdate {
		return fmt.Errorf("--auto-update and --no-auto-update are mutually exclusive")
	}
	if linkUseLiveState && linkNoUseLiveState {
		return fmt.Errorf("--use-live-state and --no-use-live-state are mutually exclusive")
	}
	if linkUpdateType != "" && linkUpdateType != "NeedsProvides" && linkUpdateType != "MergeUnits" {
		return fmt.Errorf("--update-type must be NeedsProvides or MergeUnits, got %q", linkUpdateType)
	}
	return nil
}

// setLinkFieldsOnCreate sets link-specific fields on a new Link for create operations.
func setLinkFieldsOnCreate(link *goclientnew.Link) {
	link.UpdateType = linkUpdateType
	if linkAutoUpdate {
		link.AutoUpdate = true
	}
	if linkUseLiveState {
		link.UseLiveState = true
	}
	link.WhereMutation = linkWhereMutation
	link.WhereResource = linkWhereResource
}

// setLinkFieldsOnUpdate sets link-specific fields on an existing Link for update operations.
// Only sets fields that were explicitly changed via flags.
func setLinkFieldsOnUpdate(link *goclientnew.Link, cmd *cobra.Command) {
	if cmd.Flags().Changed("update-type") {
		link.UpdateType = linkUpdateType
	}
	if linkAutoUpdate {
		link.AutoUpdate = true
	} else if linkNoAutoUpdate {
		link.AutoUpdate = false
	}
	if linkUseLiveState {
		link.UseLiveState = true
	} else if linkNoUseLiveState {
		link.UseLiveState = false
	}
	if cmd.Flags().Changed("where-mutation") {
		link.WhereMutation = linkWhereMutation
	}
	if cmd.Flags().Changed("where-resource") {
		link.WhereResource = linkWhereResource
	}
}

// linkFieldsEnhancer creates a PatchEnhancer for link-specific fields in patch/bulk operations.
func linkFieldsEnhancer(cmd *cobra.Command) PatchEnhancer {
	if !hasLinkFieldFlags(cmd) {
		return nil
	}
	return func(patchMap map[string]interface{}) {
		if cmd.Flags().Changed("update-type") {
			patchMap["UpdateType"] = linkUpdateType
		}
		if linkAutoUpdate {
			patchMap["AutoUpdate"] = true
		} else if linkNoAutoUpdate {
			patchMap["AutoUpdate"] = false
		}
		if linkUseLiveState {
			patchMap["UseLiveState"] = true
		} else if linkNoUseLiveState {
			patchMap["UseLiveState"] = false
		}
		if cmd.Flags().Changed("where-mutation") {
			patchMap["WhereMutation"] = linkWhereMutation
		}
		if cmd.Flags().Changed("where-resource") {
			patchMap["WhereResource"] = linkWhereResource
		}
	}
}

// hasLinkFieldFlags returns true if any link-specific field flags were explicitly set.
func hasLinkFieldFlags(cmd *cobra.Command) bool {
	return cmd.Flags().Changed("update-type") || linkAutoUpdate || linkNoAutoUpdate ||
		linkUseLiveState || linkNoUseLiveState ||
		cmd.Flags().Changed("where-mutation") || cmd.Flags().Changed("where-resource")
}

func buildWhereClauseFromLinks(linkIds []string) (string, error) {
	return buildWhereClauseFromIdentifiers(linkIds, "LinkID", "Slug")
}

func init() {
	addSpaceFlags(linkCmd)
	rootCmd.AddCommand(linkCmd)
}
