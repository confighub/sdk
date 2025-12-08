// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var workerGetCmd = &cobra.Command{
	Use:   "get <worker-slug>",
	Args:  cobra.ExactArgs(1),
	Short: "Get details about a bridge worker",
	Long: getCommandHelp(`Get detailed information about a bridge worker including its configuration, status, and metadata.

Examples:
`+"```"+`
  # Get details about a bridge worker
  cub worker get --space my-space my-worker

  # Get details in JSON format
  cub worker get --space my-space --json my-worker

  # Include worker secret in output
  cub worker get --space my-space --include-secret my-worker
`+"```"+`
`, ""),
	RunE: workerGetCmdRun,
}

var includeSecret bool

func init() {
	addStandardGetFlags(workerGetCmd)
	workerGetCmd.Flags().BoolVar(&includeSecret, "include-secret", false, "Include worker secret in output")
	workerCmd.AddCommand(workerGetCmd)
}

func workerGetCmdRun(_ *cobra.Command, args []string) error {
	extendedWorker, err := apiGetExtendedBridgeWorkerFromSlug(args[0], selectFields)
	if err != nil {
		return err
	}

	displayGetResults(extendedWorker, displayExtendedBridgeWorkerDetails)
	return nil
}

func displayExtendedBridgeWorkerDetails(extendedWorker *goclientnew.ExtendedBridgeWorker) {
	worker := extendedWorker.BridgeWorker

	// Handle secret masking
	displayWorker := *worker
	if !includeSecret {
		displayWorker.Secret = "********"
	}

	view := tableView()
	view.Append([]string{"ID", displayWorker.BridgeWorkerID.String()})
	view.Append([]string{"Name", displayWorker.Slug})

	// Show Space slug instead of Space ID when available
	if extendedWorker.Space != nil {
		view.Append([]string{"Space", extendedWorker.Space.Slug})
	} else {
		view.Append([]string{"Space ID", displayWorker.SpaceID.String()})
	}

	view.Append([]string{"Created At", displayWorker.CreatedAt.String()})
	view.Append([]string{"Updated At", displayWorker.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(displayWorker.Labels)})
	view.Append([]string{"Delete Gates", deleteGatesToString(displayWorker.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(displayWorker.Annotations)})
	view.Append([]string{"Permissions", permissionsToString(displayWorker.Permissions)})
	view.Append([]string{"UserID", displayWorker.UserID.String()})
	view.Append([]string{"Secret", displayWorker.Secret})
	view.Append([]string{"Condition", displayWorker.Condition})
	view.Append([]string{"Last Message", displayWorker.LastMessage})
	view.Append([]string{"Last Seen At", displayWorker.LastSeenAt.String()})
	view.Append([]string{"IP Address", displayWorker.IPAddress})
	view.Render()
}
