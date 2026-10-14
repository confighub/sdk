// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var channelCreateArgs struct {
	permissions   []string
	commands      string
	subscriptions string
}

var channelCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a channel",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Create a Channel in a space.

Its commands, the ones it handles, and its subscriptions, the events it watches, are fields
of the channel: give them in the JSON --from-stdin or --filename takes, or with --commands
and --subscriptions, each a JSON file or - for stdin. The commands are a JSON object keyed by
command name, each value its signature. The caller is given Manage.
Nobody is given Claim; grant it to the service account that will run the bot.

Examples:
`+"```"+`
  # Create a channel and let a service account claim it
  cub channel create --space previews preview-bot --permission Claim:serviceaccount:preview-bot

  # Create a channel declaring what its bot handles, from the commands the bot prints
  preview-bot --print-commands | cub channel create --space previews preview-bot --commands -
`+"```"+`
`, ""),
	RunE: channelCreateCmdRun,
}

func init() {
	addStandardCreateFlags(channelCreateCmd)
	channelCreateCmd.Flags().StringSliceVar(&channelCreateArgs.permissions, "permission", []string{}, permissionCreateHelp)
	channelCreateCmd.Flags().StringVar(&channelCreateArgs.commands, "commands", "", "the commands the channel handles, as a JSON object by name, in a file or - for stdin")
	channelCreateCmd.Flags().StringVar(&channelCreateArgs.subscriptions, "subscriptions", "", "the events the channel watches, as a JSON file or - for stdin")
	channelCmd.AddCommand(channelCreateCmd)
}

func channelCreateCmdRun(_ *cobra.Command, args []string) error {
	if err := validateSpaceFlag(false); err != nil {
		return err
	}
	if err := validateStdinFlags(); err != nil {
		return err
	}
	if err := ValidateLabelRemoval(label, false); err != nil {
		return err
	}
	if err := ValidateDeleteGateRemoval(deleteGate, false); err != nil {
		return err
	}

	newBody := &goclientnew.Channel{}
	if flagPopulateModelFromStdin || flagFilename != "" {
		if err := populateModelFromFlags(newBody); err != nil {
			return err
		}
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
	if err := applyPermissions(channelCreateArgs.permissions, &newBody.Permissions); err != nil {
		return err
	}
	if channelCreateArgs.commands != "" {
		if err := readJSONFlag(channelCreateArgs.commands, &newBody.Commands); err != nil {
			return err
		}
	}
	if channelCreateArgs.subscriptions != "" {
		if err := readJSONFlag(channelCreateArgs.subscriptions, &newBody.Subscriptions); err != nil {
			return err
		}
	}

	if args[0] == "-" {
		newBody.Slug = randomUnusedSlug()
	} else {
		newBody.Slug = makeSlug(args[0])
	}
	newBody.SpaceID = uuid.MustParse(selectedSpaceID)

	params := &goclientnew.CreateChannelParams{}
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}
	params.DryRun = dryRunParam()
	channelRes, err := cubClientNew.CreateChannelWithResponse(ctx, newBody.SpaceID, params, *newBody)
	if cubapi.IsAPIError(err, channelRes) {
		return cubapi.InterpretErrorGeneric(err, channelRes)
	}
	channelDetails := channelRes.JSON200
	displayCreateResults(channelDetails, "channel", newBody.Slug, channelDetails.ChannelID.String(), displayChannelDetails)
	return nil
}
