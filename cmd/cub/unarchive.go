// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

// unarchiveCommand describes the unarchive command of an entity type whose deletes take --archive.
type unarchiveCommand struct {
	// entity names the entity type in output, as in "trigger".
	entity string
	// long is the command's help, after its first line.
	long string
	// unarchiveOne unarchives the entity named by slug or ID, returning its ID and the response.
	unarchiveOne func(nameOrID string) (string, *goclientnew.UnarchiveResponse, error)
	// unarchiveSelected unarchives the archived entities the where filter, with the space added,
	// and the filter select.
	unarchiveSelected func(where, filterID string) ([]goclientnew.UnarchiveResponse, error)
}

// newUnarchiveCmd makes the unarchive command an unarchiveCommand describes: by slug or ID, or the
// archived entities --where and --filter select.
func newUnarchiveCmd(u unarchiveCommand) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "unarchive [<slug or id>]",
		Short:       fmt.Sprintf("Unarchive a %s or multiple %ss", u.entity, u.entity),
		Long:        getCommandHelp(u.long, ""),
		Args:        cobra.MaximumNArgs(1),
		Annotations: map[string]string{"OrgLevel": ""},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				filterID, err := parseFilterFlag(filter)
				if err != nil {
					return err
				}
				responses, err := u.unarchiveSelected(addSpaceIDToWhereClause(where, selectedSpaceID), filterID)
				if err != nil {
					return err
				}
				return displayBulkUnarchiveResults(u.entity, responses)
			}
			if filter != "" || where != "" {
				return fmt.Errorf("--filter and --where can only be specified with no positional arguments")
			}
			id, response, err := u.unarchiveOne(args[0])
			if err != nil {
				return err
			}
			if !quiet && !isAlternativeOutput() {
				tprint("Successfully unarchived %s %s (%s)%s", u.entity, args[0], id, keptSlugNote(*response))
			}
			renderPayload(response)
			return nil
		},
	}
	addStandardDisplayFlags(cmd)
	cmd.Flags().StringVar(&where, "where", "", fmt.Sprintf("where filter to select the archived %ss to unarchive", u.entity))
	enableFilterFlag(cmd)
	return cmd
}

// bulkUnarchiveResponses are the responses of a bulk unarchive, whether all succeeded or some failed.
func bulkUnarchiveResponses(statusCode int, responses200, responses207 *[]goclientnew.UnarchiveResponse) ([]goclientnew.UnarchiveResponse, error) {
	responses := responses200
	if statusCode == 207 {
		responses = responses207
	}
	if responses == nil {
		return nil, fmt.Errorf("unexpected status code %d or no response data", statusCode)
	}
	return *responses, nil
}
