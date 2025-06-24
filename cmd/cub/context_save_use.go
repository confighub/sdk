// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var contextSaveCmd = &cobra.Command{
	Use:   "save <name>",
	Short: "Save current context",
	Long:  `Save current context`,
	Args:  cobra.ExactArgs(1),
	RunE:  contextSaveCmdRun,
}

var contextUseCmd = &cobra.Command{
	Use:   "use <name>",
	Short: "Make a previously saved context the active context",
	Long:  `Make a previously saved context the active context`,
	Args:  cobra.ExactArgs(1),
	RunE:  contextUseCmdRun,
}

var overwrite bool

func init() {
	contextSaveCmd.PersistentFlags().BoolVar(&overwrite, "overwrite", false, "allow overwriting existing context")
	contextCmd.AddCommand(contextSaveCmd)
	contextCmd.AddCommand(contextUseCmd)
}

func contextSaveCmdRun(cmd *cobra.Command, args []string) error {
	cubContext.ConfigHubURLSaved = cubContext.ConfigHubURL
	SaveCubContext(cubContext)
	err := copyContextFiles(args[0])
	if err != nil {
		return err
	}
	tprint("Current context saved as %s", args[0])
	return nil
}

func contextUseCmdRun(cmd *cobra.Command, args []string) error {
	err := useContext(args[0])
	if err != nil {
		return err
	}
	tprint("%s is now the current context", args[0])
	return nil
}

func useContext(name string) error {
	contextFile := filepath.Join(os.Getenv("HOME"), CONFIGHUB_DIR, fmt.Sprintf("context-%s.json", name))
	_, err := os.Stat(contextFile)
	if err != nil {
		return fmt.Errorf("context %s does not exist", name)
	}
	err = copyFile(contextFile, filepath.Join(os.Getenv("HOME"), CONFIGHUB_DIR, "context.json"))
	if err != nil {
		return err
	}
	sessionFile := filepath.Join(os.Getenv("HOME"), CONFIGHUB_DIR, fmt.Sprintf("session-%s.json", name))
	_, err = os.Stat(sessionFile)
	if err != nil {
		return fmt.Errorf("context %s does not exist", name)
	}
	err = copyFile(sessionFile, filepath.Join(os.Getenv("HOME"), CONFIGHUB_DIR, "session.json"))
	if err != nil {
		return err
	}
	return nil
}

func copyContextFiles(destName string) error {
	destContextFile := filepath.Join(os.Getenv("HOME"), CONFIGHUB_DIR, fmt.Sprintf("context-%s.json", destName))
	destSessionFile := filepath.Join(os.Getenv("HOME"), CONFIGHUB_DIR, fmt.Sprintf("session-%s.json", destName))
	if !overwrite {
		_, err := os.Stat(destContextFile)
		if err == nil {
			return fmt.Errorf("context %s already exists", destName)
		}
		_, err = os.Stat(destSessionFile)
		if err == nil {
			return fmt.Errorf("context (session) %s already exists", destName)
		}
	}
	contextFile := filepath.Join(os.Getenv("HOME"), CONFIGHUB_DIR, "context.json")
	err := copyFile(contextFile, destContextFile)
	if err != nil {
		return err
	}
	sessionFile := filepath.Join(os.Getenv("HOME"), CONFIGHUB_DIR, "session.json")
	err = copyFile(sessionFile, destSessionFile)
	if err != nil {
		return err
	}
	return nil
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()
	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()
	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}
	err = destFile.Sync()
	if err != nil {
		return err
	}
	return nil
}
