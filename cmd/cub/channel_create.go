// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var channelCreateArgs struct {
	permissions   []string
	commands      string
	subscriptions string
	channelSlugs  []string
	destSpaces    []string
	whereSpace    string
	filterSpace   string
	namePrefixes  []string
	variantLabels []string
	namePattern   string
}

var channelCreateCmd = &cobra.Command{
	Use:   "create [<name>]",
	Short: "Create a channel or clone channels",
	Args:  cobra.MaximumNArgs(1),
	Long: getCommandHelp(`Create a Channel in a space, or clone channels.

Its commands, the ones it handles, and its subscriptions, the events it watches, are fields
of the channel: give them in the JSON --from-stdin or --filename takes, or with --commands
and --subscriptions, each a JSON file or - for stdin. The commands are a JSON object keyed by
command name, each value its signature. The caller is given Manage.
Nobody is given Claim; grant it to the service account that will run the bot.

With no name, clone the channels --channel, --where or --filter selects, to the spaces
--dest-space, --where-space or --filter-space selects, under the names --name-prefix gives, or one
per combination of --variant-labels, named by --name-pattern. A clone has its source's commands,
subscriptions and permissions, and none of its runs; it starts reading the event log where a new
channel does, at its tail.

Examples:
`+"```"+`
  # Create a channel and let a service account claim it
  cub channel create --space previews preview-bot --permission Claim:serviceaccount:preview-bot

  # Create a channel declaring what its bot handles, from the commands the bot prints
  preview-bot --print-commands | cub channel create --space previews preview-bot --commands -

  # Clone a channel to the staging and prod spaces
  cub channel create --space dev --channel argobot --dest-space staging,prod

  # Clone a channel once per region, labeled with it
  cub channel create --space dev --channel argobot --variant-labels region=us|eu \
    --name-pattern 'template:{{.SourceEntitySlug}}-{{.Labels.region}}'
`+"```"+`
`, ""),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        channelCreateCmdRun,
}

func init() {
	addStandardCreateFlags(channelCreateCmd)
	addFieldEditFlags(channelCreateCmd, "Channel")
	enableWhereFlag(channelCreateCmd)
	enableFilterFlag(channelCreateCmd)
	channelCreateCmd.Flags().StringSliceVar(&channelCreateArgs.permissions, "permission", []string{}, permissionCreateHelp)
	channelCreateCmd.Flags().StringVar(&channelCreateArgs.commands, "commands", "", "the commands the channel handles, as a JSON object by name, in a file or - for stdin")
	channelCreateCmd.Flags().StringVar(&channelCreateArgs.subscriptions, "subscriptions", "", "the events the channel watches, as a JSON file or - for stdin")
	channelCreateCmd.Flags().StringSliceVar(&channelCreateArgs.channelSlugs, "channel", []string{}, "channels to clone, by slug or UUID (can be repeated or comma-separated)")
	channelCreateCmd.Flags().StringSliceVar(&channelCreateArgs.destSpaces, "dest-space", []string{}, "spaces to clone into (can be repeated or comma-separated)")
	channelCreateCmd.Flags().StringVar(&channelCreateArgs.whereSpace, "where-space", "", "where expression selecting the spaces to clone into")
	channelCreateCmd.Flags().StringVar(&channelCreateArgs.filterSpace, "filter-space", "", "filter, by slug or UUID, selecting the spaces to clone into")
	channelCreateCmd.Flags().StringSliceVar(&channelCreateArgs.namePrefixes, "name-prefix", []string{}, "prefixes naming the clones (can be repeated or comma-separated)")
	channelCreateCmd.Flags().StringSliceVar(&channelCreateArgs.variantLabels, "variant-labels", []string{}, "labels for the clones, one clone per combination, in the format of key1=value1|value2,key2=value1|value2|value3")
	channelCreateCmd.Flags().StringVar(&channelCreateArgs.namePattern, "name-pattern", "", "a pattern naming the clones, with --variant-labels; prefix 'template:' to use a Go template with .SourceEntitySlug for the source's name and .Labels for the variant labels, as in 'template:{{.SourceEntitySlug}}-{{.Labels.env}}'")
	addBackingUnitFlags(channelCreateCmd, "Channel", false, true)
	addFromBackingUnitsFlags(channelCreateCmd, "Channel", true)
	channelCmd.AddCommand(channelCreateCmd)
}

func channelCreateCmdRun(_ *cobra.Command, args []string) error {
	cloning := len(channelCreateArgs.channelSlugs) > 0 || len(channelCreateArgs.destSpaces) > 0 ||
		channelCreateArgs.whereSpace != "" || channelCreateArgs.filterSpace != "" || len(channelCreateArgs.namePrefixes) > 0 ||
		len(channelCreateArgs.variantLabels) > 0 || channelCreateArgs.namePattern != "" || where != "" || filter != ""
	if len(args) == 0 {
		return runBulkChannelCreate()
	}
	if cloning {
		return errors.New("--channel, --where, --filter, --dest-space, --where-space, --filter-space, --name-prefix, --variant-labels and --name-pattern clone channels, and take no name")
	}
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

	if err := applyFieldEdits("Channel", newBody); err != nil {
		return err
	}
	params := &goclientnew.CreateChannelParams{}
	params.WithBackingUnits = withBackingUnitsParam()
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

func runBulkChannelCreate() error {
	if err := validateSpaceFlag(true); err != nil {
		return err
	}
	if len(channelCreateArgs.channelSlugs) > 0 && where != "" {
		return errors.New("--channel and --where are mutually exclusive")
	}
	if len(channelCreateArgs.destSpaces) > 0 && channelCreateArgs.whereSpace != "" {
		return errors.New("--dest-space and --where-space are mutually exclusive")
	}
	if !backingUnitArgs.fromBackingUnits && len(channelCreateArgs.destSpaces) == 0 && channelCreateArgs.whereSpace == "" &&
		channelCreateArgs.filterSpace == "" && len(channelCreateArgs.namePrefixes) == 0 && len(channelCreateArgs.variantLabels) == 0 {
		return errors.New("to clone channels, give at least one of --dest-space, --where-space, --filter-space, --name-prefix or --variant-labels; to create one, give its name")
	}
	if len(channelCreateArgs.namePrefixes) > 0 && len(channelCreateArgs.variantLabels) > 0 {
		return errors.New("--name-prefix and --variant-labels cannot be used together")
	}
	if channelCreateArgs.namePattern != "" && len(channelCreateArgs.variantLabels) == 0 {
		return errors.New("--name-pattern requires --variant-labels")
	}
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	effectiveWhere := where
	if len(channelCreateArgs.channelSlugs) > 0 {
		if effectiveWhere, err = buildWhereClauseFromChannels(channelCreateArgs.channelSlugs); err != nil {
			return err
		}
	}
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	patchJSON, err := BuildPatchDataWithPermissions(nil, channelCreateArgs.permissions)
	if err != nil {
		return err
	}
	include := "SpaceID"
	params := &goclientnew.BulkCreateChannelsParams{Where: &effectiveWhere, Include: &include}
	if filterID != "" {
		params.Filter = &filterID
	}
	if len(channelCreateArgs.variantLabels) > 0 {
		variantLabels := strings.Join(channelCreateArgs.variantLabels, ",")
		params.VariantLabels = &variantLabels
	}
	if channelCreateArgs.namePattern != "" {
		params.NamePattern = &channelCreateArgs.namePattern
	}
	if channelCreateArgs.filterSpace != "" {
		filterSpaceID, err := parseFilterFlag(channelCreateArgs.filterSpace)
		if err != nil {
			return errors.Wrap(err, "error parsing filter-space")
		}
		params.FilterSpace = &filterSpaceID
	}
	params.IncludeHidden = includeHiddenParam()
	params.WithBackingUnits = withBackingUnitsParam()
	if params.WhereUnit, params.FilterUnit, err = fromBackingUnitsCreateParams(selectedSpaceID); err != nil {
		return err
	}
	params.PatchExisting = patchExistingParam()
	if params.FromBackingUnits = fromBackingUnitsParam(); params.FromBackingUnits != nil {
		params.Where = nil
	}
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}
	if len(channelCreateArgs.namePrefixes) > 0 {
		namePrefixes := strings.Join(channelCreateArgs.namePrefixes, ",")
		params.NamePrefixes = &namePrefixes
	}
	whereSpace := channelCreateArgs.whereSpace
	if len(channelCreateArgs.destSpaces) > 0 {
		if whereSpace, err = buildWhereClauseForSpaces(channelCreateArgs.destSpaces); err != nil {
			return errors.Wrap(err, "error converting destination spaces to a where expression")
		}
	}
	if whereSpace != "" {
		params.WhereSpace = &whereSpace
	}
	params.DryRun = dryRunParam()

	bulkRes, err := cubClientNew.BulkCreateChannelsWithBodyWithResponse(ctx, params,
		"application/merge-patch+json", bytes.NewReader(patchJSON))
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}
	return displayBulkGenericCreateOrUpdateResults(
		bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "channel", "create", effectiveWhere,
		func(r *goclientnew.ChannelCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
		func(r *goclientnew.ChannelCreateOrUpdateResponse) string {
			if r.Channel != nil {
				return fmt.Sprintf("%s (ID: %s)", r.Channel.Slug, r.Channel.ChannelID)
			}
			return ""
		},
	)
}
