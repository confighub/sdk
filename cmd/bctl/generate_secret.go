// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/confighub/sdk/bridge-worker/token"
)

var generateSecretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Generate authentication secret",
	Long:  `Generate an authentication secret for accessing the bridge worker API`,
	RunE:  generateSecretCmdRunE,
}

func init() {
	generateCmd.AddCommand(generateSecretCmd)
}

func generateSecretCmdRunE(cmd *cobra.Command, args []string) error {
	tkn, err := token.Generate(token.DefaultSpec())
	if err != nil {
		return fmt.Errorf("error generate secret :%w", err)
	}

	fmt.Println(tkn)
	return nil
}
