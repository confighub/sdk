// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var channelUpdateArgs struct {
	permissions   []string
	commands      string
	subscriptions string
	eventPosition int64
}

var channelUpdateCmd = &cobra.Command{
	Use:   "update [<name or id>]",
	Short: "Update a channel or multiple channels",
	Args:  cobra.MaximumNArgs(1),
	Long: getCommandHelp(`Update a Channel.

Changing who may claim it requires Claim on the channel. Its commands and subscriptions are fields
like any other: --commands and --subscriptions each take a JSON file or - for stdin; --commands
replaces the whole set, so a command left out is dropped.
--event-position moves its bookmark, to replay events or to skip them.

Examples:
`+"```"+`
  # Replace what the channel declares with what its bot prints
  preview-bot --print-commands | cub channel update --patch previews/preview-bot --commands -

  # Let a service account claim it
  cub channel update --patch previews/preview-bot --permission Claim:serviceaccount:preview-bot

  # Replay every event since position 1200
  cub channel update --patch previews/preview-bot --event-position 1200
`+"```"+`

With no name, --patch updates every channel --where selects.
`, ""),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        channelUpdateCmdRun,
}

func init() {
	addStandardUpdateFlags(channelUpdateCmd)
	enableOptionalSpace(channelUpdateCmd)
	channelUpdateCmd.Flags().BoolVar(&isPatch, "patch", false, "use patch API")
	channelUpdateCmd.Flags().StringSliceVar(&channelUpdateArgs.permissions, "permission", []string{}, permissionUpdateHelp)
	channelUpdateCmd.Flags().StringVar(&channelUpdateArgs.commands, "commands", "", "the commands the channel handles, as a JSON object by name, in a file or - for stdin; replaces the set")
	channelUpdateCmd.Flags().StringVar(&channelUpdateArgs.subscriptions, "subscriptions", "", "the events the channel watches, as a JSON file or - for stdin")
	channelUpdateCmd.Flags().Int64Var(&channelUpdateArgs.eventPosition, "event-position", 0, "move the channel's bookmark into the event log")
	enableWhereFlag(channelUpdateCmd)
	channelCmd.AddCommand(channelUpdateCmd)
}

// readJSONFlag decodes the JSON in the named file, or on stdin for "-", into target.
func readJSONFlag(source string, target any) error {
	var data []byte
	var err error
	if source == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(source)
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", source, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("parsing %s: %w", source, err)
	}
	return nil
}

func channelUpdateCmdRun(cmd *cobra.Command, args []string) error {
	if isPatch && flagReplace {
		return errors.New("only one of --patch and --replace should be specified")
	}
	if err := ValidateLabelRemoval(label, isPatch); err != nil {
		return err
	}
	if err := ValidateDeleteGateRemoval(deleteGate, isPatch); err != nil {
		return err
	}
	if err := validateStdinFlags(); err != nil {
		return err
	}
	if channelUpdateArgs.commands == "-" && channelUpdateArgs.subscriptions == "-" {
		return errors.New("only one of --commands and --subscriptions can read stdin")
	}

	commandsGiven := channelUpdateArgs.commands != ""
	var commands map[string]goclientnew.CommandSignature
	if commandsGiven {
		if err := readJSONFlag(channelUpdateArgs.commands, &commands); err != nil {
			return err
		}
		if commands == nil {
			commands = map[string]goclientnew.CommandSignature{}
		}
	}
	var subscriptions []goclientnew.EventSubscription
	if channelUpdateArgs.subscriptions != "" {
		if err := readJSONFlag(channelUpdateArgs.subscriptions, &subscriptions); err != nil {
			return err
		}
		if subscriptions == nil {
			subscriptions = []goclientnew.EventSubscription{}
		}
	}
	positionChanged := cmd.Flags().Changed("event-position")
	enhancer := func(patchMap map[string]interface{}) {
		if commandsGiven {
			patchMap["Commands"] = commands
		}
		if channelUpdateArgs.subscriptions != "" {
			patchMap["Subscriptions"] = subscriptions
		}
		if positionChanged {
			patchMap["EventPosition"] = channelUpdateArgs.eventPosition
		}
	}
	if len(args) == 0 {
		return runBulkChannelUpdate(enhancer)
	}
	if where != "" {
		return errors.New("--where selects the channels to update, so it takes no channel name")
	}

	currentEnvelope, err := resolveChannel(args[0], selectedSpaceID, "*") // get all fields for RMW
	if err != nil {
		return err
	}
	currentChannel := currentEnvelope.Channel

	if isPatch {
		// A merge patch merges maps, so a patch that replaces the commands has to name every
		// command it drops as null; the same for subscriptions, which the server replaces as a
		// list anyway.
		if commandsGiven {
			merged := map[string]any{}
			for name, signature := range commands {
				merged[name] = signature
			}
			for name := range currentChannel.Commands {
				if _, kept := commands[name]; !kept {
					merged[name] = nil
				}
			}
			enhancer = func(patchMap map[string]interface{}) {
				patchMap["Commands"] = merged
				if channelUpdateArgs.subscriptions != "" {
					patchMap["Subscriptions"] = subscriptions
				}
				if positionChanged {
					patchMap["EventPosition"] = channelUpdateArgs.eventPosition
				}
			}
		}
		patchData, err := BuildPatchDataWithPermissions(enhancer, channelUpdateArgs.permissions)
		if err != nil {
			return fmt.Errorf("failed to build patch data: %w", err)
		}
		channelRes, err := cubClientNew.PatchChannelWithBodyWithResponse(
			ctx,
			currentChannel.SpaceID,
			currentChannel.ChannelID,
			&goclientnew.PatchChannelParams{DryRun: dryRunParam()},
			"application/merge-patch+json",
			bytes.NewReader(patchData),
		)
		if cubapi.IsAPIError(err, channelRes) {
			return cubapi.InterpretErrorGeneric(err, channelRes)
		}
		channelDetails := channelRes.JSON200
		displayUpdateResults(channelDetails, "channel", args[0], channelDetails.ChannelID.String(), displayChannelDetails)
		return nil
	}

	newBody := currentChannel
	if flagPopulateModelFromStdin || flagFilename != "" {
		if flagReplace {
			newBody = new(goclientnew.Channel)
			newBody.Version = currentChannel.Version
		}
		if err := populateModelFromFlags(newBody); err != nil {
			return err
		}
		newBody.OrganizationID = currentChannel.OrganizationID
		newBody.SpaceID = currentChannel.SpaceID
		newBody.ChannelID = currentChannel.ChannelID
	}
	setDisplayNameAndHiddenReason(&newBody.DisplayName, &newBody.HiddenReason)
	if err := setAnnotations(&newBody.Annotations); err != nil {
		return err
	}
	if err := setLabels(&newBody.Labels); err != nil {
		return err
	}
	if err := setDeleteGates(&newBody.DeleteGates); err != nil {
		return err
	}
	if err := applyPermissions(channelUpdateArgs.permissions, &newBody.Permissions); err != nil {
		return err
	}
	if commandsGiven {
		newBody.Commands = commands
	}
	if channelUpdateArgs.subscriptions != "" {
		newBody.Subscriptions = subscriptions
	}
	if positionChanged {
		newBody.EventPosition = channelUpdateArgs.eventPosition
	}

	channelRes, err := cubClientNew.UpdateChannelWithResponse(ctx, currentChannel.SpaceID, currentChannel.ChannelID, &goclientnew.UpdateChannelParams{DryRun: dryRunParam()}, *newBody)
	if cubapi.IsAPIError(err, channelRes) {
		return cubapi.InterpretErrorGeneric(err, channelRes)
	}
	channelDetails := channelRes.JSON200
	displayUpdateResults(channelDetails, "channel", args[0], channelDetails.ChannelID.String(), displayChannelDetails)
	return nil
}

func runBulkChannelUpdate(enhancer func(map[string]interface{})) error {
	if !isPatch {
		return errors.New("--patch is required to update multiple channels")
	}
	if where == "" {
		return errors.New("--where is required to update multiple channels")
	}
	patchData, err := BuildPatchDataWithPermissions(enhancer, channelUpdateArgs.permissions)
	if err != nil {
		return fmt.Errorf("failed to build patch data: %w", err)
	}
	params := &goclientnew.BulkPatchChannelsParams{Where: &where}
	params.IncludeHidden = includeHiddenParam()
	params.DryRun = dryRunParam()
	bulkRes, err := cubClientNew.BulkPatchChannelsWithBodyWithResponse(ctx, params,
		"application/merge-patch+json", bytes.NewReader(patchData))
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}
	return displayBulkGenericCreateOrUpdateResults(
		bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "channel", "update", where,
		func(r *goclientnew.ChannelCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
		func(r *goclientnew.ChannelCreateOrUpdateResponse) string {
			if r.Channel != nil {
				return fmt.Sprintf("%s (ID: %s)", r.Channel.Slug, r.Channel.ChannelID)
			}
			return ""
		},
	)
}
