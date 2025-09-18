// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/confighub/sdk/cubapi"
	"github.com/spf13/cobra"
)

var authTestLoginCmd = &cobra.Command{
	Use:   "test-login <username>",
	Short: "Log into ConfigHub with a test user",
	Long:  `Authenticate the CLI to ConfigHub for testing purposes`,
	Args:  cobra.ExactArgs(1),
	RunE:  authTestLoginCmdRun,
}

func init() {
	authTestLoginCmd.Flags().StringVar(&serverURL, "server", "", "Server URL to authenticate to (e.g., https://hub.confighub.com)")
	authCmd.AddCommand(authTestLoginCmd)
}

func authTestLoginCmdRun(cmd *cobra.Command, args []string) error {
	creds, err := loadTestUsers()
	if err != nil {
		return err
	}
	username := args[0]
	password, ok := creds[username]
	if !ok {
		return fmt.Errorf("no entry found for username %s in ~/.confighub/testusers.csv", username)
	}

	// Create request body for the password authentication endpoint
	requestBody := map[string]string{
		"username": username,
		"password": password,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	coordinate := Coordinate{}
	coordinate.OrganizationID = contextManager.ActiveContext().Coordinate.OrganizationID

	if os.Getenv("CONFIGHUB_URL") != "" {
		if serverURL != "" {
			return fmt.Errorf("cannot use both --server and CONFIGHUB_URL environment variable")
		}
		coordinate.ServerURL = os.Getenv("CONFIGHUB_URL")
	} else if serverURL != "" {
		coordinate.ServerURL = serverURL
	} else {
		coordinate.ServerURL = contextManager.ActiveContext().Coordinate.ServerURL
	}

	endpoint := fmt.Sprintf("%s/auth/password", coordinate.ServerURL)
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make authentication request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		return fmt.Errorf("authentication failed: %s", string(body))
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse the response into an cubapi.AuthSession
	session := cubapi.AuthSession{}
	session.AuthType = cubapi.AuthTypeJWT

	if err := json.Unmarshal(body, &session); err != nil {
		return fmt.Errorf("failed to parse authentication response: %w", err)
	}

	// Update context with authentication results
	if err := updateContextFromSession(coordinate, &session); err != nil {
		return fmt.Errorf("failed to update context: %w", err)
	}
	displayContextDetails(contextManager.ActiveContext())

	// Preload builtin functions
	if _, _, err := listAndSaveFunctions("", "", ""); err != nil {
		return err
	}

	return nil
}

func loadTestUsers() (map[string]string, error) {
	testUsersFile := filepath.Join(os.Getenv("HOME"), ".confighub", "testusers.csv")

	file, err := os.Open(testUsersFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)

	// Read all lines from CSV
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	// Create a map to store username-password pairs
	credentials := make(map[string]string)

	// Iterate over records, assuming no header row
	for _, record := range records {
		if len(record) < 2 {
			return nil, fmt.Errorf("invalid record format: %v", record)
		}
		username := record[0]
		password := record[1]
		credentials[username] = password
	}

	return credentials, nil
}
