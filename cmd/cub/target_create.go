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

var targetCreateCmd = &cobra.Command{
	Use:   "create [<slug>]",
	Short: "Create a new target or bulk create targets",
	Long: getCommandHelp(`Create a new target with the specified slug, or bulk create targets by cloning existing ones.

A Target is where a Space's Releases are destined: a Space releases to the Target named by
its ReleaseTargetID, and a GitOps tool such as Argo CD or Flux pulls those Releases from
ConfigHub's OCI registry. The identity that pulls, such as a worker's bot user, needs View
and ViewChildren on the Target, granted with --permission or "cub target update --permission".

Without a slug, the command clones the targets --where, --filter or --target select, into the
spaces --dest-space, --where-space or --filter-space select and/or under the names --name-prefix,
--variant-labels and --name-pattern give them. The other flags, and --from-stdin, change the
clones. Each clone lists the Triggers its WhereTrigger and TriggerFilterID select.`, `
  # Create a Target
  cub target create --space infra prod

  # Create a Target a worker's bot user can pull from
  cub target create --space infra prod \
    --permission View:<bot-user-id> --permission ViewChildren:<bot-user-id>

  # Clone a Space's Targets into other Spaces
  cub target create --space infra --dest-space infra-east,infra-west

  # Clone Targets under prefixed names, with a label of their own
  cub target create --where "Labels.tier = 'prod'" --name-prefix dr- --label tier=dr`),
	Args:        cobra.MaximumNArgs(1),
	RunE:        targetCreateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var targetCreateArgs struct {
	whereTrigger  string
	triggerFilter string
	permissions   []string
	targetSlugs   []string
	destSpaces    []string
	whereSpace    string
	filterSpace   string
	namePrefixes  []string
	variantLabels []string
	namePattern   string
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
	enableWhereFlag(targetCreateCmd)
	enableFilterFlag(targetCreateCmd)
	targetCreateCmd.Flags().StringSliceVar(&targetCreateArgs.targetSlugs, "target", []string{}, "target specific targets by slug or UUID for bulk create (can be repeated or comma-separated)")
	targetCreateCmd.Flags().StringSliceVar(&targetCreateArgs.destSpaces, "dest-space", []string{}, "destination spaces for bulk create (can be repeated or comma-separated)")
	targetCreateCmd.Flags().StringVar(&targetCreateArgs.whereSpace, "where-space", "", "where expression to select destination spaces for bulk create")
	targetCreateCmd.Flags().StringVar(&targetCreateArgs.filterSpace, "filter-space", "", "filter entity containing WHERE expression to select destination spaces for bulk create (slug or UUID)")
	targetCreateCmd.Flags().StringSliceVar(&targetCreateArgs.namePrefixes, "name-prefix", []string{}, "name prefixes for bulk create (can be repeated or comma-separated)")
	targetCreateCmd.Flags().StringSliceVar(&targetCreateArgs.variantLabels, "variant-labels", []string{}, "labels for bulk create in the format of key1=value1|value2,key2=value1|value2|value3")
	targetCreateCmd.Flags().StringVar(&targetCreateArgs.namePattern, "name-pattern", "", "a pattern string for name generation of clones, prefix 'template:' to use a Go template with .SourceEntitySlug to access the original Target and .Labels to access variant labels, example: 'template:{{.SourceEntitySlug}}-{{.Labels.env}}'")
	addBackingUnitFlags(targetCreateCmd, "Target", false, true)
	addFromBackingUnitsFlags(targetCreateCmd, "Target", true)
	addFieldEditFlags(targetCreateCmd, "Target")
	targetCmd.AddCommand(targetCreateCmd)
}

func checkTargetCreateConflictingArgs(args []string) (bool, error) {
	isBulkCreateMode := len(args) == 0

	if isBulkCreateMode {
		if len(targetCreateArgs.targetSlugs) > 0 && where != "" {
			return false, errors.New("--target and --where flags are mutually exclusive")
		}
		if len(targetCreateArgs.destSpaces) > 0 && targetCreateArgs.whereSpace != "" {
			return false, errors.New("--dest-space and --where-space flags are mutually exclusive")
		}
		if !backingUnitArgs.fromBackingUnits && len(targetCreateArgs.destSpaces) == 0 && targetCreateArgs.whereSpace == "" && targetCreateArgs.filterSpace == "" &&
			len(targetCreateArgs.namePrefixes) == 0 && len(targetCreateArgs.variantLabels) == 0 {
			return false, errors.New("bulk create mode requires at least one of --dest-space, --where-space, --filter-space, --name-prefix, or --variant-labels")
		}
		if len(targetCreateArgs.namePrefixes) > 0 && len(targetCreateArgs.variantLabels) > 0 {
			return false, errors.New("--name-prefix and --variant-labels cannot be used together")
		}
		if targetCreateArgs.namePattern != "" && len(targetCreateArgs.namePrefixes) > 0 {
			return false, errors.New("--name-pattern and --name-prefix cannot be used together")
		}
		if targetCreateArgs.namePattern != "" && len(targetCreateArgs.variantLabels) == 0 {
			return false, errors.New("--name-pattern requires --variant-labels to be set")
		}
		if fromTarget != "" || fromTargetSpace != "" {
			return false, errors.New("--from-target and --from-target-space create a single target: pass its slug")
		}
	} else if filter != "" || where != "" ||
		targetCreateArgs.namePattern != "" || len(targetCreateArgs.targetSlugs) > 0 ||
		len(targetCreateArgs.destSpaces) > 0 || targetCreateArgs.whereSpace != "" || targetCreateArgs.filterSpace != "" ||
		len(targetCreateArgs.namePrefixes) > 0 || len(targetCreateArgs.variantLabels) > 0 {
		return false, errors.New(
			"bulk create flags (--filter, --where, --target, --dest-space, --where-space, --filter-space, --name-prefix, --variant-labels, --name-pattern) can only be used without positional arguments",
		)
	}

	if err := validateSpaceFlag(isBulkCreateMode); err != nil {
		return isBulkCreateMode, err
	}
	if err := validateStdinFlags(); err != nil {
		return isBulkCreateMode, err
	}
	// Validate no label removal
	if err := ValidateLabelRemoval(label, false); err != nil {
		return isBulkCreateMode, err
	}
	// Validate no delete gate removal
	if err := ValidateDeleteGateRemoval(deleteGate, false); err != nil {
		return isBulkCreateMode, err
	}
	return isBulkCreateMode, nil
}

func targetCreateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkCreateMode, err := checkTargetCreateConflictingArgs(args)
	if err != nil {
		return err
	}
	if isBulkCreateMode {
		return runBulkTargetCreate()
	}
	return runSingleTargetCreate(args)
}

func runSingleTargetCreate(args []string) error {
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

	setDisplayNameAndHiddenReason(&newTarget.DisplayName, &newTarget.HiddenReason)
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
	params.WithBackingUnits = withBackingUnitsParam()
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}

	params.DryRun = dryRunParam()
	if err := applyFieldEdits("Target", &newTarget); err != nil {
		return err
	}
	targetRes, err := cubClientNew.CreateTargetWithResponse(ctx, spaceID, params, newTarget)
	if cubapi.IsAPIError(err, targetRes) {
		return cubapi.InterpretErrorGeneric(err, targetRes)
	}

	targetDetails := targetRes.JSON200
	extendedDetails := &goclientnew.ExtendedTarget{Target: targetDetails}
	displayCreateResults(extendedDetails, "target", args[0], targetDetails.TargetID.String(), displayTargetDetails)
	return nil
}

func runBulkTargetCreate() error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	effectiveWhere := where
	if len(targetCreateArgs.targetSlugs) > 0 {
		effectiveWhere, err = buildWhereClauseFromTargets(targetCreateArgs.targetSlugs)
		if err != nil {
			return err
		}
	}
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	targetEnhancer, err := targetPatchEnhancer(targetCreateArgs.whereTrigger, targetCreateArgs.triggerFilter)
	if err != nil {
		return err
	}
	patchJSON, err := BuildPatchDataWithPermissions(targetEnhancer, targetCreateArgs.permissions)
	if err != nil {
		return err
	}

	include := "SpaceID"
	params := &goclientnew.BulkCreateTargetsParams{
		Where:   &effectiveWhere,
		Include: &include,
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
	if filterID != "" {
		params.Filter = &filterID
	}
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}
	if len(targetCreateArgs.namePrefixes) > 0 {
		namePrefixesStr := strings.Join(targetCreateArgs.namePrefixes, ",")
		params.NamePrefixes = &namePrefixesStr
	}
	if len(targetCreateArgs.variantLabels) > 0 {
		variantLabelsStr := strings.Join(targetCreateArgs.variantLabels, ",")
		params.VariantLabels = &variantLabelsStr
	}
	if targetCreateArgs.namePattern != "" {
		params.NamePattern = &targetCreateArgs.namePattern
	}

	whereSpaceExpr := targetCreateArgs.whereSpace
	if len(targetCreateArgs.destSpaces) > 0 {
		whereSpaceExpr, err = buildWhereClauseForSpaces(targetCreateArgs.destSpaces)
		if err != nil {
			return errors.Wrapf(err, "error converting destination spaces to where expression")
		}
	}
	if whereSpaceExpr != "" {
		params.WhereSpace = &whereSpaceExpr
	}
	if targetCreateArgs.filterSpace != "" {
		filterSpaceID, err := parseFilterFlag(targetCreateArgs.filterSpace)
		if err != nil {
			return errors.Wrapf(err, "error parsing filter-space")
		}
		params.FilterSpace = &filterSpaceID
	}

	params.DryRun = dryRunParam()
	bulkRes, err := cubClientNew.BulkCreateTargetsWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}

	return displayBulkGenericCreateOrUpdateResults(
		bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "target", "create", effectiveWhere,
		func(r *goclientnew.TargetCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
		func(r *goclientnew.TargetCreateOrUpdateResponse) string {
			if r.Target != nil {
				return fmt.Sprintf("%s (ID: %s)", r.Target.Slug, r.Target.TargetID)
			}
			return ""
		},
	)
}
