// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/confighub/sdk/cubapi"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authentication commands",
	Long:  getCommandHelp(`The auth subcommands are used to manage authentication`, ""),
}

func init() {
	rootCmd.AddCommand(authCmd)
}

// LoginURL returns the login URL used to start an authentication flow for a context coordinate
// It respects the CONFIGHUB_URL environment variable if set.
// If the server URL is not set in this context, it defaults to https://hub.confighub.com.
func LoginURL(coordinate Coordinate) string {
	serverURL := coordinate.ServerURL
	// Parse the server URL
	parsedURL, err := url.Parse(serverURL)
	if err != nil {
		// Fallback to default if parsing fails
		parsedURL = &url.URL{
			Scheme: "https",
			Host:   "hub.confighub.com",
		}
	}

	loginURL := fmt.Sprintf("%s://%s/auth/login", parsedURL.Scheme, parsedURL.Host)

	params := url.Values{}
	params.Set("client_type", "api")
	params.Set("redirect_uri", "http://127.0.0.1:3000/") // TODO: This should probably be adjustable

	if coordinate.OrganizationID != "" {
		params.Set("org", coordinate.OrganizationID)
	}

	return loginURL + "?" + params.Encode()
}

// LogoutURL returns the logout URL used to log out of a session for a context coordinate.
func LogoutURL(coordinate Coordinate) string {
	serverURL := coordinate.ServerURL

	parsedURL, err := url.Parse(serverURL)
	if err != nil {
		// Fallback to default if parsing fails
		parsedURL = &url.URL{
			Scheme: "https",
			Host:   "hub.confighub.com",
		}
	}

	return fmt.Sprintf("%s://%s/auth/logout", parsedURL.Scheme, parsedURL.Host)
}

func LoadSession() (cubapi.AuthSession, error) {
	var session cubapi.AuthSession

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

func SaveSession(session cubapi.AuthSession) error {
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
