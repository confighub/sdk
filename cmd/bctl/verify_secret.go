// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/confighub/sdk/bridge-worker/token"
)

var verifySecretCmd = &cobra.Command{
	Use:   "secret [secret]",
	Short: "Verify authentication secret",
	Long:  `Verify an authentication secret used for accessing the bridge worker API`,
	Args:  cobra.ExactArgs(1),
	RunE:  verifySecretCmdRunE,
}

func init() {
	verifyCmd.AddCommand(verifySecretCmd)
}

func verifySecretCmdRunE(cmd *cobra.Command, args []string) error {
	err := token.Verify(token.DefaultSpec(), args[0])
	if err != nil {
		return err
	}

	fmt.Println("Secret is valid")
	return nil
}
