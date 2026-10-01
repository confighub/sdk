// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var targetCreateCmd = &cobra.Command{
	Use:   "create <slug>",
	Short: "Create a new target",
	Long: getCommandHelp(`Create a new target with the specified slug.

A Target is where a Space's Releases are destined: a Space releases to the Target named by
its ReleaseTargetID, and a GitOps tool such as Argo CD or Flux pulls those Releases from
ConfigHub's OCI registry. The identity that pulls, such as a worker's bot user, needs View
and ViewChildren on the Target, granted with --permission or "cub target update --permission".`, `
  # Create a Target
  cub target create --space infra prod

  # Create a Target a worker's bot user can pull from
  cub target create --space infra prod \
    --permission View:<bot-user-id> --permission ViewChildren:<bot-user-id>`),
	Args: cobra.ExactArgs(1),
	RunE: targetCreateCmdRun,
}

var targetCreateArgs struct {
	whereTrigger  string
	triggerFilter string
	permissions   []string
}

var fromTarget string
var fromTargetSpace string

func init() {
	addStandardCreateFlags(targetCreateCmd)
	// TODO: Remove client-side copying now that server-side bulk create exists
	targetCreateCmd.Flags().StringVar(&fromTarget, "from-target", "", "target to copy from another space")
	targetCreateCmd.Flags().StringVar(&fromTargetSpace, "from-target-space", "", "space of target to copy")
	targetCreateCmd.Flags().StringSliceVar(&targetCreateArgs.permissions, "permission", []string{}, "permission in format Action:UserIDOrUsername (e.g., Manage:user@example.com, can be repeated)")
	enableFactFlag(targetCreateCmd)
	targetCreateCmd.Flags().StringVar(&targetCreateArgs.whereTrigger, "where-trigger", "", "filter expression to identify Triggers that should be invoked on Units associated with this Target (use '-' to clear)")
	targetCreateCmd.Flags().StringVar(&targetCreateArgs.triggerFilter, "trigger-filter", "", "Filter slug or UUID to identify Triggers that should be invoked on Units associated with this Target (use '-' to clear)")
	targetCmd.AddCommand(targetCreateCmd)
}

func targetCreateCmdRun(cmd *cobra.Command, args []string) error {
	if err := validateStdinFlags(); err != nil {
		return err
	}
	// Validate no label removal
	if err := ValidateLabelRemoval(label, false); err != nil {
		return err
	}
	// Validate no delete gate removal
	if err := ValidateDeleteGateRemoval(deleteGate, false); err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)
	newTarget := goclientnew.Target{}

	if fromTarget != "" || fromTargetSpace != "" {
		if fromTarget == "" || fromTargetSpace == "" {
			return errors.New("both of --from-target and --from-target-space must be specified")
		}
		if flagPopulateModelFromStdin || flagFilename != "" {
			return errors.New("only one of --from-target or --from-stdin/--filename may be specified")
		}
		ftSpace, err := resolveSpace(fromTargetSpace, "*") // get all fields for now
		if err != nil {
			return err
		}
		ftTarget, err := resolveTarget(fromTarget, ftSpace.Space.SpaceID.String(), "*") // get all fields for copy
		if err != nil {
			return err
		}
		newTarget = *ftTarget.Target
	}

	if flagPopulateModelFromStdin || flagFilename != "" {
		if err := populateModelFromFlags(&newTarget); err != nil {
			return err
		}
	}

	err := setAnnotations(&newTarget.Annotations)
	if err != nil {
		return err
	}
	err = setLabels(&newTarget.Labels)
	if err != nil {
		return err
	}
	err = setFacts(&newTarget.Facts)
	if err != nil {
		return err
	}
	err = setDeleteGates(&newTarget.DeleteGates)
	if err != nil {
		return err
	}

	// Parse and set permissions
	err = applyPermissions(targetCreateArgs.permissions, &newTarget.Permissions)
	if err != nil {
		return err
	}

	// Set WhereTrigger if provided
	if targetCreateArgs.whereTrigger == "-" {
		newTarget.WhereTrigger = ""
	} else if targetCreateArgs.whereTrigger != "" {
		newTarget.WhereTrigger = targetCreateArgs.whereTrigger
	}

	// Set TriggerFilterID if provided
	if targetCreateArgs.triggerFilter == "-" {
		newTarget.TriggerFilterID = nil
	} else if targetCreateArgs.triggerFilter != "" {
		triggerFilterID, err := parseFilterFlag(targetCreateArgs.triggerFilter)
		if err != nil {
			return err
		}
		triggerFilterUUID := uuid.MustParse(triggerFilterID)
		newTarget.TriggerFilterID = &triggerFilterUUID
	}

	newTarget.SpaceID = spaceID
	newTarget.Slug = makeSlug(args[0])
	if newTarget.DisplayName == "" {
		newTarget.DisplayName = args[0]
	}

	// Create params with AllowExists if needed
	params := &goclientnew.CreateTargetParams{}
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}

	params.DryRun = dryRunParam()
	targetRes, err := cubClientNew.CreateTargetWithResponse(ctx, spaceID, params, newTarget)
	if cubapi.IsAPIError(err, targetRes) {
		return cubapi.InterpretErrorGeneric(err, targetRes)
	}

	targetDetails := targetRes.JSON200
	extendedDetails := &goclientnew.ExtendedTarget{Target: targetDetails}
	displayCreateResults(extendedDetails, "target", args[0], targetDetails.TargetID.String(), displayTargetDetails)
	return nil
}
