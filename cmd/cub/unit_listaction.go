// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"net/url"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
)

var unitListEventCmd = &cobra.Command{
	Use:     "list-events <unit-slug>",
	Aliases: []string{"list-event"},
	Short:   "List events",
	Args:    cobra.ExactArgs(1),
	RunE:    unitListEventRun,
}

func init() {
	addStandardListFlags(unitListEventCmd)
	unitCmd.AddCommand(unitListEventCmd)
}

func unitListEventRun(cmd *cobra.Command, args []string) error {
	slug := args[0]
	u, err := apiGetUnitFromSlug(slug)
	if err != nil {
		return err
	}

	actions, err := apiListUnitEvents(uuid.MustParse(selectedSpaceID), u.UnitID, where)
	if err != nil {
		return err
	}

	displayListResults(actions, getUnitEventSlug, displayUnitEventList)
	return nil
}

func getUnitEventSlug(entity *goclientnew.UnitEvent) string {
	return "-"
}

func displayUnitEventList(events []*goclientnew.UnitEvent) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Action", "Result", "Status", "Created-At", "Terminated-At", "Message"})
	}
	for _, action := range events {
		act := ""
		result := ""
		if action.Action != nil {
			act = string(*action.Action)
		}
		if action.Result != nil {
			result = string(*action.Result)
		}
		table.Append([]string{
			act,
			result,
			string(actionStatus(action.Status)),
			action.CreatedAt.String(),
			action.TerminatedAt.String(),
			action.Message,
		})
	}
	table.Render()
}

func apiListUnitEvents(spaceID uuid.UUID, unitID uuid.UUID, whereFilter string) ([]*goclientnew.UnitEvent, error) {
	newParams := &goclientnew.ListUnitEventsParams{}
	if whereFilter != "" {
		whereFilter = url.QueryEscape(whereFilter)
		newParams.Where = &whereFilter
	}
	eventsRes, err := cubClientNew.ListUnitEventsWithResponse(ctx, spaceID, unitID, newParams)
	if IsAPIError(err, eventsRes) {
		return nil, InterpretErrorGeneric(err, eventsRes)
	}
	events := make([]*goclientnew.UnitEvent, 0, len(*eventsRes.JSON200))
	for _, event := range *eventsRes.JSON200 {
		events = append(events, &event)
	}
	return events, nil
}
