// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var spaceCreateCmd = &cobra.Command{
	Use:   "create <space>",
	Short: "Create a space",
	Args:  cobra.ExactArgs(1),
	Long: `Create a new space as a collaborative context for a project or team.

Examples:
  # Create a new space named "my-space" with verbose output, reading configuration from stdin
  # Verbose output prints the details of the created entity
  cub space create --verbose --json --from-stdin my-space

  # Create a new space with minimal output
  cub space create my-space`,
	RunE: spaceCreateCmdRun,
}

func init() {
	addStandardCreateFlags(spaceCreateCmd)
	spaceCmd.AddCommand(spaceCreateCmd)
}

func spaceCreateCmdRun(cmd *cobra.Command, args []string) error {
	newBody := &goclientnew.Space{}
	if flagPopulateModelFromStdin {
		if err := populateNewModelFromStdin(newBody); err != nil {
			return err
		}
	}
	err := setLabels(&newBody.Labels)
	if err != nil {
		return err
	}

	// Even if slug was set in stdin, we override it with the one from args
	newBody.Slug = makeSlug(args[0])

	spaceRes, err := cubClientNew.CreateSpaceWithResponse(ctx, *newBody)
	if IsAPIError(err, spaceRes) {
		return InterpretErrorGeneric(err, spaceRes)
	}

	spaceDetails := spaceRes.JSON200
	displayCreateResults(spaceDetails, "space", args[0], spaceDetails.SpaceID.String(), displaySpaceDetails)
	return nil
}

// UnmarshalBinary interface implementation
func UnmarshalBinary(m *goclientnew.Space, b []byte) error {
	var res goclientnew.Space
	if err := json.Unmarshal(b, res); err != nil {
		return err
	}
	*m = res
	return nil
}
