// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var workerGetCmd = &cobra.Command{
	Use:   "get <worker-slug>",
	Args:  cobra.ExactArgs(1),
	Short: "Get Bridge Worker environment variables",
	Long: `Get Bridge Worker environment variables.

# Get the environment variables for a Bridge Worker
cub worker get-envs <worker-slug>`,
	RunE: workerGetCmdRun,
}

var workerGetInput struct {
	slug          string
	includeSecret bool
}

func init() {
	enableJsonFlag(workerGetCmd)
	workerGetCmd.Flags().BoolVar(&workerGetInput.includeSecret, "include-secret", false, "Include worker secret in putput")
	workerCmd.AddCommand(workerGetCmd)
}

func workerGetCmdRun(_ *cobra.Command, args []string) error {
	workerGetInput.slug = args[0]
	worker, err := apiGetBridgeWorkerFromSlug(workerGetInput.slug)
	if err != nil {
		return err
	}
	if !workerGetInput.includeSecret {
		worker.Secret = "********"
	}
	if jsonOutput {
		displayJSON(worker)
	} else {
		// TODO WorkerInfo is too large to display in the CLI
		// workerInfo, err := json.Marshal(worker.ProvidedInfo)
		// if err != nil {
		//	return errors.Wrap(err, "failed to marshal worker info")
		// }
		detail := detailView()
		detail.Append([]string{"ID", worker.BridgeWorkerID.String()})
		detail.Append([]string{"Name", worker.Slug})
		detail.Append([]string{"Space ID", worker.SpaceID.String()})
		detail.Append([]string{"Created At", worker.CreatedAt.String()})
		detail.Append([]string{"Updated At", worker.UpdatedAt.String()})
		// detail.Append([]string{"Provided Info", string(workerInfo)})
		detail.Append([]string{"Secret", worker.Secret})
		detail.Append([]string{"Condition", worker.Condition})
		detail.Append([]string{"Last Message", worker.LastMessage})
		detail.Append([]string{"Last Seen At", worker.LastSeenAt.String()})
		detail.Append([]string{"IP Address", worker.IPAddress})
		detail.Render()
	}
	return nil
}
