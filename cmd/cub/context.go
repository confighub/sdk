// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var cubContext = CubContext{
	Space:          "",
	SpaceID:        "",
	OrganizationID: "",
}

type CubContext struct {
	ConfigHubURL      string `json:"-"`
	ConfigHubURLSaved string `json:"confighub_url"`
	Space             string `json:"space"`
	SpaceID           string `json:"space_id"`
	OrganizationID    string `json:"organization_id"`
}

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Context commands",
	Long:  `Context commands`,
}

func init() {
	rootCmd.AddCommand(contextCmd)
}

func LoadCubContext() {

	contextFile := filepath.Join(os.Getenv("HOME"), CONFIGHUB_DIR, "context.json")
	_, err := os.Stat(contextFile)
	if err != nil {
		if os.IsNotExist(err) {
			// no file, which is ok
			return
		}
		failOnError(fmt.Errorf("failed to stat context file: %w", err))
	}
	// File exists
	data, err := os.ReadFile(contextFile)
	failOnError(err)

	// Unmarshal the JSON data into the session struct
	err = json.Unmarshal(data, &cubContext)
	failOnError(err)
}

func SaveCubContext(context CubContext) error {
	// Define the config directory and file path
	configDir := filepath.Join(os.Getenv("HOME"), CONFIGHUB_DIR)
	configFile := filepath.Join(configDir, "context.json")

	// Ensure the config directory exists
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Marshal the session struct to JSON
	data, err := json.MarshalIndent(context, "", "  ") // Prettify JSON output
	if err != nil {
		return fmt.Errorf("failed to marshal context: %w", err)
	}

	// Write the JSON data to the file
	if err := os.WriteFile(configFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write context to file: %w", err)
	}

	return nil
}
