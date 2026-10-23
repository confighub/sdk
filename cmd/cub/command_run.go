// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var commandRunRootCmd = &cobra.Command{
	Use:   "commandrun",
	Short: "Read and cancel command runs",
	Long: getCommandHelp(`Read and cancel command runs: the record of what channels were asked and what happened.

A run is Pending until the channel's client takes it, Delivered while the client executes it,
and then Completed, Failed or Rejected as the client reports; Expired if nothing took it by its
deadline, TimedOut if the client never answered, or Canceled while it was still Pending.`, ""),
	PersistentPreRunE: spacePreRunE,
}

var commandRunListCmd = &cobra.Command{
	Use:   "list",
	Short: "List command runs",
	Long: getCommandHelp(`List command runs across the organization, or in one space with --space.

Examples:
`+"```"+`
  # Runs still waiting for a client
  cub commandrun list --where "Status = 'Pending'"

  # What one space asked for today
  cub commandrun list --space previews --where "CreatedAt > '2026-10-12'"
`+"```"+`
`, ""),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        commandRunListCmdRun,
}

var commandRunGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a command run and its outcome",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get a command run: its request and, once the client has answered, its outcome. With --wait,
hold until it reaches a terminal status.

Examples:
`+"```"+`
  cub commandrun get 3f1c6b2e-8d4a-4f7e-9a21-5c0d7e4b9ab2 --wait
`+"```"+`
`, ""),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        commandRunGetCmdRun,
}

var commandRunCancelCmd = &cobra.Command{
	Use:   "cancel <id>",
	Short: "Cancel a Pending command run",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Cancel a command run that is still Pending. A run the client has already taken cannot be
canceled: it completes or times out.`, ""),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        commandRunCancelCmdRun,
}

var defaultCommandRunColumns = []string{"CommandRun.CommandRunID", "CommandRun.Command", "Space.Slug", "Channel.Slug", "CommandRun.Status", "CommandRun.CreatedAt", "CommandRun.CompletedAt", "CommandRun.ErrorMessage"}

const commandRunListInclude = "SpaceID,ChannelID"

var commandRunAliases = map[string]string{
	"ID": "CommandRun.CommandRunID",
}

func init() {
	addSpaceFlags(commandRunRootCmd)
	rootCmd.AddCommand(commandRunRootCmd)

	addStandardListFlags(commandRunListCmd)
	enableListPagingFlags(commandRunListCmd)
	commandRunRootCmd.AddCommand(commandRunListCmd)

	commandRunGetCmd.Flags().BoolVar(&commandRunArgs.getWait, "wait", false, "wait for the run to reach a terminal status")
	commandRunGetCmd.Flags().StringVar(&commandRunArgs.getTime, "timeout", DefaultTimeoutDuration.String(), "how long to wait, as a duration such as 30s or 2m")
	addStandardGetFlags(commandRunGetCmd)
	commandRunRootCmd.AddCommand(commandRunGetCmd)

	addStandardDisplayFlags(commandRunCancelCmd)
	commandRunRootCmd.AddCommand(commandRunCancelCmd)
}

func commandRunListCmdRun(_ *cobra.Command, _ []string) error {
	whereClause := cubapi.NewWhere(where)
	if selectedSpaceID != "*" && selectedSpaceID != "" {
		whereClause = whereClause.SpaceID(goclientnew.UUID(uuid.MustParse(selectedSpaceID)))
	}
	selectValue := handleSelectParameter("", selectFields, func() string {
		return buildSelectList("CommandRun", listColumnsFor("cub commandrun list"), commandRunListInclude, defaultCommandRunColumns, commandRunAliases, nil, []string{"CommandRunID", "SpaceID", "OrganizationID", "ChannelID"})
	})
	orderBy := listOrderBy
	if orderBy == "" {
		orderBy = "DESC:CreatedAt"
	}
	runs, err := cubapi.ListCommandRuns(ctx, cubClient, whereClause, cubapi.ListOpts{
		Limit:   listLimit,
		OrderBy: orderBy,
		Select:  cubapi.SelectFields(selectValue),
		Include: commandRunListInclude,
		View:    listViewFor("cub commandrun list"),
	})
	if err != nil {
		return err
	}
	displayListResults(runs, func(run *goclientnew.ExtendedCommandRun) string { return run.CommandRun.CommandRunID.String() }, displayCommandRunList)
	return nil
}

func commandRunGetCmdRun(_ *cobra.Command, args []string) error {
	run, err := cubapi.ResolveCommandRun(ctx, cubClient, cubapi.ParseRef(args[0]), cubapi.ResolveOpts{Select: cubapi.SelectFields(handleSelectParameter("", selectFields, nil))})
	if err != nil {
		return err
	}
	if commandRunArgs.getWait {
		waitFor, err := time.ParseDuration(commandRunArgs.getTime)
		if err != nil {
			return fmt.Errorf("invalid --timeout: %w", err)
		}
		run, err = awaitCommandRun(run.CommandRun.SpaceID, run.CommandRun.CommandRunID, waitFor)
		if err != nil {
			return err
		}
	}
	displayGetResults(run, displayExtendedCommandRunDetails)
	return nil
}

func commandRunCancelCmdRun(_ *cobra.Command, args []string) error {
	run, err := cubapi.ResolveCommandRun(ctx, cubClient, cubapi.ParseRef(args[0]), cubapi.ResolveOpts{Select: "CommandRunID,SpaceID"})
	if err != nil {
		return err
	}
	res, err := cubClientNew.CancelCommandRunWithResponse(ctx, run.CommandRun.SpaceID, run.CommandRun.CommandRunID)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	displayGetResults(res.JSON200, displayCommandRunDetails)
	return nil
}

// awaitCommandRun reads the run until it reaches a terminal status or the timeout elapses,
// letting the server hold each read open with ?wait=.
func awaitCommandRun(spaceID, runID goclientnew.UUID, timeout time.Duration) (*goclientnew.ExtendedCommandRun, error) {
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		waitSeconds := max(1, min(25, int(remaining.Seconds())))
		run, err := getCommandRun(spaceID, runID, waitSeconds)
		if err != nil {
			return nil, err
		}
		if isTerminalRunStatus(run.CommandRun.Status) {
			return run, nil
		}
	}
	run, err := getCommandRun(spaceID, runID, 0)
	if err != nil {
		return nil, err
	}
	if !isTerminalRunStatus(run.CommandRun.Status) {
		return run, fmt.Errorf("run %s is still %s after %s", runID, run.CommandRun.Status, timeout)
	}
	return run, nil
}

// getCommandRun reads the run with its Space and Channel, holding for up to waitSeconds for a
// terminal status. The response always carries the run.
func getCommandRun(spaceID, runID goclientnew.UUID, waitSeconds int) (*goclientnew.ExtendedCommandRun, error) {
	include := commandRunListInclude
	params := &goclientnew.GetCommandRunParams{Include: &include}
	if waitSeconds > 0 {
		params.Wait = &waitSeconds
	}
	res, err := cubClientNew.GetCommandRunWithResponse(ctx, spaceID, runID, params)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil || res.JSON200.CommandRun == nil {
		return nil, fmt.Errorf("the server returned no run for %s", runID)
	}
	return res.JSON200, nil
}

func isTerminalRunStatus(status string) bool {
	switch status {
	case "Completed", "Failed", "Rejected", "Canceled", "Expired", "TimedOut":
		return true
	}
	return false
}

// commandRunOutcomeError is the error a waited-for run ends in when the client did not
// complete it.
func commandRunOutcomeError(run *goclientnew.CommandRun) error {
	switch run.Status {
	case "Completed":
		return nil
	case "Expired":
		return fmt.Errorf("run %s expired before a client took it", run.CommandRunID)
	case "TimedOut":
		return fmt.Errorf("run %s timed out: the client did not answer", run.CommandRunID)
	default:
		if run.ErrorMessage != "" {
			return fmt.Errorf("run %s %s: %s", run.CommandRunID, strings.ToLower(run.Status), run.ErrorMessage)
		}
		return fmt.Errorf("run %s %s", run.CommandRunID, strings.ToLower(run.Status))
	}
}

func displayExtendedCommandRunDetails(extended *goclientnew.ExtendedCommandRun) {
	channel := ""
	if extended.Channel != nil {
		channel = extended.Channel.Slug
	}
	displayCommandRunTable(extended.CommandRun, channel)
}

func displayCommandRunDetails(run *goclientnew.CommandRun) {
	displayCommandRunTable(run, "")
}

// displayCommandRunTable shows a run; channel is its Channel's slug, when the response carried it.
func displayCommandRunTable(run *goclientnew.CommandRun, channel string) {
	view := tableView()
	view.Append([]string{"ID", run.CommandRunID.String()})
	view.Append([]string{"Command", run.Command})
	view.Append([]string{"Status", run.Status})
	view.Append([]string{"Space", run.SpaceSlug})
	if channel != "" {
		view.Append([]string{"Channel", channel})
	}
	view.Append([]string{"Channel ID", run.ChannelID.String()})
	view.Append([]string{"Params", compactJSON(run.Params)})
	if run.Result != nil {
		view.Append([]string{"Result", compactJSON(run.Result)})
	}
	if run.ErrorMessage != "" {
		view.Append([]string{"Error", run.ErrorMessage})
	}
	view.Append([]string{"Created At", run.CreatedAt.String()})
	view.Append([]string{"Created By", run.CreatedBy.String()})
	view.Append([]string{"Deadline", run.Deadline.String()})
	if !run.DeliveredAt.IsZero() {
		view.Append([]string{"Delivered At", run.DeliveredAt.String()})
	}
	if !run.TimeoutAt.IsZero() {
		view.Append([]string{"Timeout At", run.TimeoutAt.String()})
	}
	if !run.CompletedAt.IsZero() {
		view.Append([]string{"Completed At", run.CompletedAt.String()})
	}
	view.Render()
}

func displayCommandRunList(extended []*goclientnew.ExtendedCommandRun) {
	if displayRequestedColumns(extended, commandRunAliases, nil) {
		return
	}
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"ID", "Command", "Space", "Channel", "Status", "Created", "Completed", "Error"})
	}
	for _, e := range extended {
		run := e.CommandRun
		if run == nil {
			continue
		}
		space, channel := run.SpaceSlug, ""
		if e.Space != nil {
			space = e.Space.Slug
		}
		if e.Channel != nil {
			channel = e.Channel.Slug
		}
		completed := ""
		if !run.CompletedAt.IsZero() {
			completed = run.CompletedAt.Format("2006-01-02 15:04:05")
		}
		table.Append([]string{
			run.CommandRunID.String(),
			run.Command,
			space,
			channel,
			run.Status,
			run.CreatedAt.Format("2006-01-02 15:04:05"),
			completed,
			run.ErrorMessage,
		})
	}
	table.Render()
}
