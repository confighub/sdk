// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// A command is what a Channel declares and its client executes; running one creates a run on
// the Channel, which carries the request and, once the client answers, the outcome.

var commandCmd = &cobra.Command{
	Use:   "command",
	Short: "List the commands channels declare, and run them",
	Long: getCommandHelp(`List the commands channels declare, and run them.

A command is declared on a Channel and executed by the client that holds the channel. Running
one creates a run: the request, delivered to the client over the channel, and the outcome the
client reports. "cub commandrun" reads the runs back.`, ""),
	PersistentPreRunE: spacePreRunE,
}

var commandListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the commands channels declare",
	Long: getCommandHelp(`List the commands channels declare, with the channel declaring each: across the
organization, or in one space with --space. A command two channels declare is listed under both.

Examples:
`+"```"+`
  # Every command, organization-wide
  cub command list

  # The commands runnable in a space
  cub command list --space previews
`+"```"+`
`, ""),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        commandListCmdRun,
}

var commandGetCmd = &cobra.Command{
	Use:   "get <command>",
	Short: "Get a command's signature, and who declares it",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get a command: its options and what it returns, as each channel declaring it declares them.

Examples:
`+"```"+`
  cub command get preview.deploy
  cub command get --space previews preview.deploy -o json
`+"```"+`
`, ""),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        commandGetCmdRun,
}

var commandRunCmd = &cobra.Command{
	Use:   "run <command> [option=value ...]",
	Short: "Run a command on a channel",
	Args:  cobra.MinimumNArgs(1),
	Long: getCommandHelp(`Run a command, and by default wait for its outcome.

The command is resolved to the one channel that declares it, across the organization, or in one
space with --space; a command several channels declare has to name its channel with --channel.
Options are given as option=value and typed by the channel's declaration: an int option is
parsed as a number, a bool as true/false, a json option as JSON, and an entity option (UnitID,
TargetID, SpaceID, ChannelID) takes a slug or a UUID. --params supplies a JSON object of options
instead of, or as well as, option=value pairs.

The run fails if the client reports Failed or Rejected, or if it expires or times out before the
client answers. A channel whose client is not connected holds the run until its deadline.

Examples:
`+"```"+`
  # Deploy a preview, waiting for the outcome
  cub command run preview.deploy pr=42 sha=0123abcd

  # Name the channel, and come back for the outcome later
  cub command run preview.deploy pr=42 sha=0123abcd --channel previews/preview-bot --wait=false

  # Options as JSON, with a two-minute deadline
  cub command run preview.delete --params '{"pr": 42}' --deadline 2m
`+"```"+`
`, ""),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        commandRunCmdRun,
}

var commandRunArgs struct {
	channel  string
	target   string
	unit     string
	params   string
	deadline string
	wait     bool
	timeout  string
	getWait  bool
	getTime  string
	cancelID string
}

func init() {
	addSpaceFlags(commandCmd)
	rootCmd.AddCommand(commandCmd)

	addStandardListDisplayFlags(commandListCmd)
	commandCmd.AddCommand(commandListCmd)

	addStandardDisplayFlags(commandGetCmd)
	commandCmd.AddCommand(commandGetCmd)

	commandRunCmd.Flags().StringVar(&commandRunArgs.channel, "channel", "", "the channel to run it on, when several declare the command")
	commandRunCmd.Flags().StringVar(&commandRunArgs.target, "target", "", "the Target the run is about, recorded on it")
	commandRunCmd.Flags().StringVar(&commandRunArgs.unit, "unit", "", "the Unit the run is about, recorded on it")
	commandRunCmd.Flags().StringVar(&commandRunArgs.params, "params", "", "options as a JSON object; option=value arguments override its keys")
	commandRunCmd.Flags().StringVar(&commandRunArgs.deadline, "deadline", "", "how long the run waits for the client, as a duration such as 30s or 2m (server default if unset)")
	commandRunCmd.Flags().BoolVar(&commandRunArgs.wait, "wait", true, "wait for the outcome")
	commandRunCmd.Flags().StringVar(&commandRunArgs.timeout, "timeout", DefaultTimeoutDuration.String(), "how long to wait for the outcome, as a duration such as 30s or 2m")
	addStandardDisplayFlags(commandRunCmd)
	commandCmd.AddCommand(commandRunCmd)
}

// listDeclaredCommands lists the commands in the selected space, or the organization.
func listDeclaredCommands(command string) ([]*goclientnew.DeclaredCommand, error) {
	var spaceID *goclientnew.UUID
	if selectedSpaceID != "" && selectedSpaceID != "*" {
		id := goclientnew.UUID(uuid.MustParse(selectedSpaceID))
		spaceID = &id
	}
	return cubapi.ListDeclaredCommands(ctx, cubClient, spaceID, command)
}

func commandListCmdRun(_ *cobra.Command, _ []string) error {
	commands, err := listDeclaredCommands("")
	if err != nil {
		return err
	}
	displayListResults(commands, func(c *goclientnew.DeclaredCommand) string { return c.Command }, displayDeclaredCommandList)
	return nil
}

func commandGetCmdRun(_ *cobra.Command, args []string) error {
	commands, err := listDeclaredCommands(args[0])
	if err != nil {
		return err
	}
	if len(commands) == 0 {
		return fmt.Errorf("no channel declares command %q", args[0])
	}
	if renderPayload(commands) {
		return nil
	}
	for i, command := range commands {
		if i > 0 {
			fmt.Println()
		}
		displayDeclaredCommandDetails(command)
	}
	return nil
}

func displayDeclaredCommandList(commands []*goclientnew.DeclaredCommand) {
	if displayRequestedColumns(commands, nil, nil) {
		return
	}
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Command", "Channel", "Space", "Options", "Description"})
	}
	for _, command := range commands {
		table.Append([]string{command.Command, command.ChannelSlug, command.SpaceSlug, optionsSummary(command.Signature), signatureDescription(command.Signature)})
	}
	table.Render()
}

func displayDeclaredCommandDetails(command *goclientnew.DeclaredCommand) {
	view := tableView()
	view.Append([]string{"Command", command.Command})
	view.Append([]string{"Channel", prefixedSlug(command.SpaceSlug, command.ChannelSlug)})
	view.Append([]string{"Channel ID", command.ChannelID.String()})
	view.Append([]string{"Description", signatureDescription(command.Signature)})
	if command.Signature != nil {
		for _, option := range command.Signature.Options {
			text := option.DataType
			if option.Required {
				text += ", required"
			}
			if len(option.Choices) > 0 {
				text += ", one of " + strings.Join(option.Choices, "|")
			}
			if option.Description != "" {
				text += ": " + option.Description
			}
			view.Append([]string{"Option " + option.Name, text})
		}
		if command.Signature.Returns != nil {
			view.Append([]string{"Returns", compactJSON(command.Signature.Returns)})
		}
	}
	view.Render()
}

func signatureDescription(signature *goclientnew.CommandSignature) string {
	if signature == nil {
		return ""
	}
	return signature.Description
}

func optionsSummary(signature *goclientnew.CommandSignature) string {
	if signature == nil {
		return ""
	}
	options := make([]string, 0, len(signature.Options))
	for _, option := range signature.Options {
		text := option.Name + ":" + option.DataType
		if option.Required {
			text += "!"
		}
		options = append(options, text)
	}
	return strings.Join(options, " ")
}

func compactJSON(value any) string {
	if value == nil {
		return ""
	}
	out, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(out)
}

func commandRunCmdRun(_ *cobra.Command, args []string) error {
	command := args[0]
	target, err := resolveRunChannel(command)
	if err != nil {
		return err
	}
	params, err := buildCommandParams(target.signature, commandRunArgs.params, args[1:])
	if err != nil {
		return err
	}
	body := goclientnew.CommandRun{
		Command:   command,
		ChannelID: target.channelID,
		Params:    params,
	}
	if commandRunArgs.deadline != "" {
		d, err := time.ParseDuration(commandRunArgs.deadline)
		if err != nil {
			return fmt.Errorf("invalid --deadline: %w", err)
		}
		body.Deadline = time.Now().Add(d)
	}
	if commandRunArgs.target != "" {
		id, err := resolveTargetID(commandRunArgs.target)
		if err != nil {
			return err
		}
		body.TargetID = &id
	}
	if commandRunArgs.unit != "" {
		id, err := resolveUnitID(commandRunArgs.unit)
		if err != nil {
			return err
		}
		body.UnitID = &id
	}
	res, err := cubClientNew.CreateCommandRunWithResponse(ctx, target.spaceID, &goclientnew.CreateCommandRunParams{}, body)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	run := res.JSON200
	if commandRunArgs.wait {
		waitFor, err := time.ParseDuration(commandRunArgs.timeout)
		if err != nil {
			return fmt.Errorf("invalid --timeout: %w", err)
		}
		run, err = awaitCommandRun(run.SpaceID, run.CommandRunID, waitFor)
		if err != nil {
			return err
		}
	}
	displayGetResults(run, displayCommandRunDetails)
	if commandRunArgs.wait {
		return commandRunOutcomeError(run)
	}
	return nil
}

// runTarget is where a run goes: the channel, its space, and the signature to type the options by.
type runTarget struct {
	spaceID   goclientnew.UUID
	channelID goclientnew.UUID
	signature *goclientnew.CommandSignature
}

// resolveRunChannel finds the channel a run of the command goes to: the one --channel names,
// which has to declare it, or the one channel declaring it in the selected space or across the
// organization. Several is refused, naming them, so the caller picks one.
func resolveRunChannel(command string) (*runTarget, error) {
	if commandRunArgs.channel != "" {
		channel, err := resolveChannel(commandRunArgs.channel, selectedSpaceID, "*")
		if err != nil {
			return nil, err
		}
		signature, declared := channel.Channel.Commands[command]
		if !declared {
			names := make([]string, 0, len(channel.Channel.Commands))
			for name := range channel.Channel.Commands {
				names = append(names, name)
			}
			sort.Strings(names)
			return nil, fmt.Errorf("channel %s does not declare command %q; it declares: %s", channel.Channel.Slug, command, strings.Join(names, ", "))
		}
		return &runTarget{spaceID: channel.Channel.SpaceID, channelID: channel.Channel.ChannelID, signature: &signature}, nil
	}
	candidates, err := listDeclaredCommands(command)
	if err != nil {
		return nil, err
	}
	switch len(candidates) {
	case 0:
		if selectedSpaceID != "" && selectedSpaceID != "*" {
			return nil, fmt.Errorf("no channel in the space declares command %q", command)
		}
		return nil, fmt.Errorf("no channel declares command %q", command)
	case 1:
		return &runTarget{spaceID: candidates[0].SpaceID, channelID: candidates[0].ChannelID, signature: candidates[0].Signature}, nil
	}
	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		names = append(names, prefixedSlug(candidate.SpaceSlug, candidate.ChannelSlug))
	}
	sort.Strings(names)
	return nil, fmt.Errorf("command %q is declared by %d channels (%s); name one with --channel", command, len(candidates), strings.Join(names, ", "))
}

// buildCommandParams merges --params and option=value arguments into the params object,
// typing each option=value by the declared signature.
func buildCommandParams(signature *goclientnew.CommandSignature, rawParams string, pairs []string) (map[string]any, error) {
	params := map[string]any{}
	if rawParams != "" {
		if err := json.Unmarshal([]byte(rawParams), &params); err != nil {
			return nil, fmt.Errorf("--params must be a JSON object: %w", err)
		}
	}
	options := map[string]goclientnew.CommandOption{}
	if signature != nil {
		for _, option := range signature.Options {
			options[option.Name] = option
		}
	}
	for _, pair := range pairs {
		name, raw, found := strings.Cut(pair, "=")
		if !found || name == "" {
			return nil, fmt.Errorf("option %q must be option=value", pair)
		}
		option, declared := options[name]
		if !declared {
			return nil, fmt.Errorf("option %q is not declared for this command", name)
		}
		value, err := coerceCommandOption(option, raw)
		if err != nil {
			return nil, fmt.Errorf("option %q: %w", name, err)
		}
		params[name] = value
	}
	return params, nil
}

// coerceCommandOption turns the text of one option=value into the typed value the channel
// declared for it.
func coerceCommandOption(option goclientnew.CommandOption, raw string) (any, error) {
	switch option.DataType {
	case "int":
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("expected an integer: %w", err)
		}
		return n, nil
	case "bool":
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("expected true or false: %w", err)
		}
		return b, nil
	case "json":
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return nil, fmt.Errorf("expected JSON: %w", err)
		}
		return value, nil
	case "UnitID", "TargetID", "SpaceID", "ChannelID":
		if id, err := uuid.Parse(raw); err == nil {
			return id.String(), nil
		}
		id, err := resolveCommandEntityOption(option.DataType, raw)
		if err != nil {
			return nil, err
		}
		return id.String(), nil
	default:
		if len(option.Choices) > 0 && !slices.Contains(option.Choices, raw) {
			return nil, fmt.Errorf("expected one of %s", strings.Join(option.Choices, ", "))
		}
		return raw, nil
	}
}

func resolveCommandEntityOption(dataType string, ref string) (uuid.UUID, error) {
	switch dataType {
	case "UnitID":
		return resolveUnitID(ref)
	case "TargetID":
		return resolveTargetID(ref)
	case "SpaceID":
		space, err := resolveSpace(ref, idSelect("SpaceID"))
		if err != nil {
			return uuid.Nil, err
		}
		return space.Space.SpaceID, nil
	case "ChannelID":
		channel, err := resolveChannel(ref, selectedSpaceID, idSelect("ChannelID"))
		if err != nil {
			return uuid.Nil, err
		}
		return channel.Channel.ChannelID, nil
	}
	return uuid.Nil, fmt.Errorf("unknown entity option type %q", dataType)
}
