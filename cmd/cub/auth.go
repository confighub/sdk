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

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authentication commands",
	Long:  `The auth subcommands are used to manage authentication`,
}

func init() {
	rootCmd.AddCommand(authCmd)
}

func LoadSession() (AuthSession, error) {
	var session AuthSession

	// Define the config file path
	configFile := filepath.Join(os.Getenv("HOME"), ".confighub", "session.json")

	// Read the JSON data from the file
	data, err := os.ReadFile(configFile)
	if err != nil {
		return session, fmt.Errorf("failed to read config file: %w", err)
	}

	// Unmarshal the JSON data into the session struct
	if err := json.Unmarshal(data, &session); err != nil {
		return session, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	return session, nil
}

func SaveSession(session AuthSession) error {
	// Define the config directory and file path
	configDir := filepath.Join(os.Getenv("HOME"), ".confighub")
	configFile := filepath.Join(configDir, "session.json")

	// Ensure the config directory exists
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal the session struct to JSON
	data, err := json.MarshalIndent(session, "", "  ") // Prettify JSON output
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	// Write the JSON data to the file
	if err := os.WriteFile(configFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write session to file: %w", err)
	}

	return nil
}
