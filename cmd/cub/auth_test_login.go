// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"

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
	session := AuthSession{
		User: User{
			Email: username,
		},
		BasicAuthPassword: password,
		AuthType:          "Basic",
	}
	err = SaveSession(session)
	if err != nil {
		return err
	}
	authSession = session
	authHeader = setAuthHeader(&authSession)
	cubClientNew, err = initializeClient()
	if err != nil {
		return err
	}
	err = setSpaceContext()
	if err != nil {
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
