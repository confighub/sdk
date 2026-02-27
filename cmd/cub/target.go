// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var targetCmd = &cobra.Command{
	Use:   "target",
	Short: "Target commands",
	Long: getCommandHelp(`The target subcommands are used to manage targets.

Targets are explained at https://docs.confighub.com/background/entities/target/.`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(targetCmd)
	rootCmd.AddCommand(targetCmd)
}

// FindConfigType returns the TargetConfigType matching the given toolchain and provider.
// If the toolchain matches the inline ToolchainType, a TargetConfigType is constructed
// from the inline fields. Otherwise, the ConfigTypes list is searched.
func FindConfigType(t *goclientnew.Target, toolchain string, provider string) *goclientnew.TargetConfigType {
	if t.ToolchainType == toolchain && (provider == "" || t.ProviderType == provider) {
		return &goclientnew.TargetConfigType{
			ProviderType:  t.ProviderType,
			ToolchainType: t.ToolchainType,
			LiveStateType: t.LiveStateType,
			Options:       t.Options,
		}
	}
	for i := range t.ConfigTypes {
		if t.ConfigTypes[i].ToolchainType == toolchain && (provider == "" || t.ConfigTypes[i].ProviderType == provider) {
			return &t.ConfigTypes[i]
		}
	}
	return nil
}
