// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/changeworkflow"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// The Unit holding the ChangeWorkflow definition that governs how a ChangeOrder
// is promoted, recorded on the ChangeOrder as the Unit's ID and the Revision of
// it in force when the ChangeOrder was created.
//
// The Revision is pinned rather than read at head so that editing the workflow
// part way through a rollout cannot change the rules a change already started
// under: every promotion of one ChangeOrder is judged against one definition.
const (
	changeWorkflowUnitIDAnnotation   = "confighub.com/change-workflow-unit-id"
	changeWorkflowRevisionAnnotation = "confighub.com/change-workflow-revision-num"
)

var changeorderCreateCmd = &cobra.Command{
	Use:         "create [<slug> [--description <description>]]",
	Short:       "Create a new changeorder or bulk create changeorders",
	Long:        getChangeOrderCreateHelp(),
	Args:        cobra.MinimumNArgs(0), // Allow 0 args for bulk mode
	RunE:        changeorderCreateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func getChangeOrderCreateHelp() string {
	baseHelp := `Create a new changeorder or bulk create multiple changeorders by cloning existing ones.

SINGLE CHANGEORDER CREATION:

Create a new changeorder to record an entity changeorder specification.

Examples:
` + "```" + `
  # Create a change order
  cub changeorder create --space my-space bump-base-image --description "Bump the base image to 1.42"

  # Create one that says where it is headed: the spaces it propagates into
  cub changeorder create --space my-space bump-base-image --description "Bump the base image" \
    --in-scope-space staging,prod-use2,prod-usw2

  # The list is a list, so work it out however you like and pass the answer
  SPACES=$(cub space list --quiet --no-headers -o name --where "Labels.Component = 'my-app'" | paste -sd, -)
  cub changeorder create --space my-space bump-base-image --in-scope-space "$SPACES"

  # Or say it by component, which selects the same spaces without listing them
  cub changeorder create --space my-space bump-base-image --component my-app

  # Create one governed by a change workflow. The workflow says what each stage selects and
  # in what order; where the change is headed is the component's spaces, defaulting to the
  # component of the space it is created in.
  cub changeorder create --space my-space bump-base-image \
    --change-workflow workflows/my-app-main-line

  # End it at an existing boundary rather than at each unit's head. The tag is read to find
  # each unit's end revision; the change order marks with tags of its own.
  cub changeorder create --space my-space bump-base-image --end-tag my-space/release-42-end

  # Create a changeorder from JSON
  cub changeorder create --space my-space -o json my-changeorder --from-stdin < changeorder.json
` + "```" + `

BULK CHANGEORDER CREATION:

When no positional arguments are provided, bulk create mode is activated. This mode clones existing
changeorders based on filters and creates multiple new changeorders with optional modifications.

Bulk Create Examples:
` + "```" + `
  # Clone all changeorders matching a pattern with name prefixes
  cub changeorder create --where "Description LIKE '%release%'" --name-prefix archive- --dest-space archive-space

  # Clone specific changeorders to multiple spaces
  cub changeorder create --changeorder my-changeorder --dest-space dev-space,staging-space

  # Clone changeorders using a where expression for destination spaces
  cub changeorder create --where "Description LIKE 'Release%'" --where-space "Labels.Environment IN ('dev', 'staging')"

  # Clone changeorders with modifications via JSON patch
  echo '{"Description": "Archived changeorder"}' | cub changeorder create --where "CreatedAt < '2024-01-01'" --name-prefix old- --from-stdin
` + "```" + `
`

	return getCommandHelp(baseHelp, "")
}

var changeorderCreateArgs struct {
	destSpaces       []string
	whereSpace       string
	namePrefixes     []string
	changeorderSlugs []string
	description      string
	inScopeSpaces    []string
	endTag           string
	updateType       string
	filterSpace      string
	variantLabels    []string
	namePattern      string
	changeWorkflow   string
	component        string
}

func init() {
	addStandardCreateFlags(changeorderCreateCmd)
	enableWhereFlag(changeorderCreateCmd)
	enableFilterFlag(changeorderCreateCmd)

	// Single create specific flags
	changeorderCreateCmd.Flags().StringVar(&changeorderCreateArgs.changeWorkflow, "change-workflow", "", "identifier (slug, space/slug, or UUID) to identify the Unit carrying a ChangeWorkflow definition to use when promoting the ChangeOrder")
	changeorderCreateCmd.Flags().StringVar(&changeorderCreateArgs.description, "description", "", "human-readable description of the change")
	changeorderCreateCmd.Flags().StringSliceVar(&changeorderCreateArgs.inScopeSpaces, "in-scope-space", []string{}, "spaces (slug or UUID) this change order propagates into, stored on it as InScopeSpaceIDs (can be repeated or comma-separated); without any, wherever its links reach is where it is headed")
	changeorderCreateCmd.Flags().StringVar(&changeorderCreateArgs.updateType, "update-type", "", "link update type to follow when propagating: UpgradeUnit (the clone lineage, the default) or MergeUnits")
	changeorderCreateCmd.Flags().StringVar(&changeorderCreateArgs.endTag, "end-tag", "", "tag (slug, space/slug, or UUID) marking the last revision of each unit to promote; without one, each unit's head revision is the end. The change order always creates its own start and end tags -- this one is read to find the boundary, recorded as AdoptedEndTagID, and never written to, since the change order also marks the units it carries no changes for")
	changeorderCreateCmd.Flags().StringVar(&changeorderCreateArgs.component, "component", "", "filter for Component of the Variants to be promoted, defaults to containing Space's Component.")

	// Bulk create specific flags
	changeorderCreateCmd.Flags().StringSliceVar(&changeorderCreateArgs.destSpaces, "dest-space", []string{}, "destination spaces for bulk create (can be repeated or comma-separated)")
	changeorderCreateCmd.Flags().StringVar(&changeorderCreateArgs.whereSpace, "where-space", "", "where expression to select destination spaces for bulk create")
	changeorderCreateCmd.Flags().StringSliceVar(&changeorderCreateArgs.namePrefixes, "name-prefix", []string{}, "name prefixes for bulk create (can be repeated or comma-separated)")
	changeorderCreateCmd.Flags().StringSliceVar(&changeorderCreateArgs.variantLabels, "variant-labels", []string{}, "labels for bulk create in the format of key1=value1|value2,key2=value1|value2|value3")
	changeorderCreateCmd.Flags().StringVar(&changeorderCreateArgs.namePattern, "name-pattern", "", "a pattern string for name generation of clones, prefix 'template:' to use a Go template with .SourceEntitySlug to access the original ChangeOrder and .Labels to access variant labels, example: 'template:{{.SourceEntitySlug}}-{{.Labels.env}}'")
	changeorderCreateCmd.Flags().StringSliceVar(&changeorderCreateArgs.changeorderSlugs, "changeorder", []string{}, "target specific changeorders by slug or UUID for bulk create (can be repeated or comma-separated)")
	changeorderCreateCmd.Flags().StringVar(&changeorderCreateArgs.filterSpace, "filter-space", "", "filter entity containing WHERE expression to select destination spaces for bulk create (slug or UUID)")

	changeorderCmd.AddCommand(changeorderCreateCmd)
}

func checkChangeOrderCreateConflictingArgs(args []string) (bool, error) {
	// Determine if bulk create mode: no positional args
	isBulkCreateMode := len(args) == 0

	if isBulkCreateMode {
		// Validate bulk create requirements
		if len(changeorderCreateArgs.changeorderSlugs) > 0 && where != "" {
			return false, errors.New("--changeorder and --where flags are mutually exclusive")
		}

		if len(changeorderCreateArgs.destSpaces) > 0 && changeorderCreateArgs.whereSpace != "" {
			return false, errors.New("--dest-space and --where-space flags are mutually exclusive")
		}

		if len(changeorderCreateArgs.destSpaces) == 0 && changeorderCreateArgs.whereSpace == "" && len(changeorderCreateArgs.namePrefixes) == 0 && len(changeorderCreateArgs.variantLabels) == 0 {
			return false, errors.New("bulk create mode requires at least one of --dest-space, --where-space, --name-prefix, or --variant-labels")
		}

		if len(changeorderCreateArgs.namePrefixes) > 0 && len(changeorderCreateArgs.variantLabels) > 0 {
			return false, errors.New("--name-prefix and --variant-labels cannot be used together")
		}

		if changeorderCreateArgs.namePattern != "" && len(changeorderCreateArgs.namePrefixes) > 0 {
			return false, errors.New("--name-pattern and --name-prefix cannot be used together")
		}

		if changeorderCreateArgs.namePattern != "" && len(changeorderCreateArgs.variantLabels) == 0 {
			return false, errors.New("--name-pattern requires --variant-labels to be set")
		}

		if changeorderCreateArgs.changeWorkflow != "" {
			return false, errors.New("--change-workflow can only be used with single changeorder creation")
		}

		if changeorderCreateArgs.component != "" {
			return false, errors.New("--component can only be used with single changeorder creation")
		}
	} else {
		// Single create mode validation
		if len(args) != 1 {
			return false, errors.New("single changeorder creation requires: <slug>")
		}

		if filter != "" || where != "" || changeorderCreateArgs.namePattern != "" ||
			len(changeorderCreateArgs.changeorderSlugs) > 0 || len(changeorderCreateArgs.destSpaces) > 0 ||
			changeorderCreateArgs.whereSpace != "" || len(changeorderCreateArgs.namePrefixes) > 0 ||
			len(changeorderCreateArgs.variantLabels) > 0 {
			return false, errors.New(
				"bulk create flags (--filter, --where, --changeorder, --dest-space, --where-space, --name-prefix, --variant-labels, --name-pattern) can only be used without positional arguments",
			)
		}

		// Both answer the same question -- where the change is headed -- one as a
		// literal list and one as the filter that selects it.
		if changeorderCreateArgs.component != "" && len(changeorderCreateArgs.inScopeSpaces) > 0 {
			return false, errors.New("--component and --in-scope-space flags are mutually exclusive")
		}
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

// resolveChangeWorkflowUnit resolves --change-workflow to the Unit carrying the
// ChangeWorkflow definition. The definition does not say where the change
// starts: the base is the Space the ChangeOrder is created in.
func resolveChangeWorkflowUnit(identifier string) (*goclientnew.Unit, *changeworkflow.ChangeWorkflow, error) {
	unit, err := parseEntityIdentifierSingleAsEntity(identifier, EntityTypeUnit, "UnitID,Slug,HeadRevisionNum",
		apiGetUnitFromSlugInSpace,
		func(u *goclientnew.Unit) string { return u.UnitID.String() },
	)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to parse change-workflow")
	}

	// Read the Revision that will be pinned, not the Unit's data, so what is read
	// here is exactly what every later promotion will be judged against.
	changeWorkflow, err := getChangeWorkflowFromUnit(unit.UnitID.String(),
		strconv.FormatInt(unit.HeadRevisionNum, 10))
	if err != nil {
		return nil, nil, err
	}

	return unit, changeWorkflow, nil
}

// changeOrderCreateComponent is the component the change belongs to: the one
// --component names, and otherwise the Component label of the Space being
// created in.
//
// The ChangeOrder does not exist yet to be asked, which is why this reads the
// Space rather than going through changeOrderComponent as a promotion does. The
// two agree: without the flag both read the same label off the same Space.
func changeOrderCreateComponent(changeOrderSpaceID uuid.UUID) (string, error) {
	if changeorderCreateArgs.component != "" {
		return changeorderCreateArgs.component, nil
	}
	return spaceComponent(changeOrderSpaceID)
}

// componentSpaceIDs is where a change to one component is headed: every Space of
// that component, which is the Variants it is promoted through. The filter is the
// Component label -- a component being the set of Spaces sharing that label value
// rather than an entity of its own -- and it is the same term every Stage's
// selector is conjoined with (stageWhereSpace), so a Stage's membership is always a
// subset of this.
//
// This is the filter a creation can use. InScopeSpaceIDs is not determined until
// after the ChangeOrder exists, so it cannot select the Spaces of the ChangeOrder
// being created; the component can, and it says the same thing a Stage's clause
// says about which component's Spaces are meant.
func componentSpaceIDs(component string) ([]uuid.UUID, error) {
	where := fmt.Sprintf("Labels.%s = '%s'", labelComponent, component)
	spaces, err := apiListSpaces(where, "SpaceID")
	if err != nil {
		return nil, errors.Wrapf(err, "failed to resolve the Spaces of component '%s'", component)
	}
	spaceIDs := make([]uuid.UUID, 0, len(spaces))
	for _, space := range spaces {
		if space != nil {
			spaceIDs = append(spaceIDs, space.SpaceID)
		}
	}
	// The Space the ChangeOrder is created in carries the label, so the component
	// always has at least that one Space. None means the component named does not
	// exist, which would otherwise leave the change headed nowhere in particular.
	if len(spaceIDs) == 0 {
		return nil, errors.Newf("component '%s' has no Spaces, so there is nowhere for the change order to go", component)
	}
	return spaceIDs, nil
}

// validateChangeWorkflowStages refuses a definition whose Stages cannot be
// rendered for the change being created, before a ChangeOrder is pinned to one:
// no Stage may name the component itself, since the component is the
// ChangeOrder's own and is appended to every Stage's clause (stageWhereSpace).
//
// Only the clause is checked, not what it selects. Where the change is headed is
// the component's Spaces, or the list the client names -- never the union of the
// Stages -- so there is nothing here to resolve Spaces for.
func validateChangeWorkflowStages(changeWorkflow *changeworkflow.ChangeWorkflow, component string) error {
	for i := range changeWorkflow.Spec.Stages {
		if _, err := stageWhereSpace(&changeWorkflow.Spec.Stages[i], component); err != nil {
			return err
		}
	}
	return nil
}

func changeorderCreateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkCreateMode, err := checkChangeOrderCreateConflictingArgs(args)
	if err != nil {
		return err
	}

	if isBulkCreateMode {
		return runBulkChangeOrderCreate()
	}

	return runSingleChangeOrderCreate(args)
}

func runSingleChangeOrderCreate(args []string) error {
	spaceID := uuid.MustParse(selectedSpaceID)
	newBody := goclientnew.ChangeOrder{}
	if flagPopulateModelFromStdin || flagFilename != "" {
		if err := populateModelFromFlags(&newBody); err != nil {
			return err
		}
	}
	err := setAnnotations(&newBody.Annotations)
	if err != nil {
		return err
	}
	err = setLabels(&newBody.Labels)
	if err != nil {
		return err
	}
	err = setDeleteGates(&newBody.DeleteGates)
	if err != nil {
		return err
	}
	newBody.SpaceID = spaceID
	newBody.Slug = makeSlug(args[0])
	if newBody.DisplayName == "" {
		newBody.DisplayName = args[0]
	}

	// Set description if provided
	if changeorderCreateArgs.description != "" {
		newBody.Description = changeorderCreateArgs.description
	}
	if changeorderCreateArgs.updateType != "" {
		newBody.UpdateType = changeorderCreateArgs.updateType
	}
	if changeorderCreateArgs.endTag != "" {
		endTagID, err := parseTagSlug(changeorderCreateArgs.endTag)
		if err != nil {
			return errors.Wrap(err, "failed to parse end-tag")
		}
		newBody.EndTagID = endTagID
	}
	// Where the change is headed, settled here rather than derived from the
	// ChangeWorkflow: the Spaces named literally, or the ones the component filter
	// selects. A ChangeOrder with neither says nothing about where it is going, and
	// wherever its Links reach is what it covers.
	if len(changeorderCreateArgs.inScopeSpaces) > 0 {
		inScopeSpaceIDs, err := resolveChangeOrderInScopeSpaces(changeorderCreateArgs.inScopeSpaces)
		if err != nil {
			return err
		}
		newBody.InScopeSpaceIDs = inScopeSpaceIDs
	}
	if changeorderCreateArgs.component != "" || changeorderCreateArgs.changeWorkflow != "" {
		// The base is the Space the ChangeOrder is being created in, so the component
		// needs no lookup of its own and nothing in the definition to agree with.
		component, err := changeOrderCreateComponent(spaceID)
		if err != nil {
			return err
		}
		if len(newBody.InScopeSpaceIDs) == 0 {
			inScopeSpaceIDs, err := componentSpaceIDs(component)
			if err != nil {
				return err
			}
			newBody.InScopeSpaceIDs = inScopeSpaceIDs
		}
		if changeorderCreateArgs.changeWorkflow != "" {
			unit, changeWorkflow, err := resolveChangeWorkflowUnit(changeorderCreateArgs.changeWorkflow)
			if err != nil {
				return err
			}
			if err := validateChangeWorkflowStages(changeWorkflow, component); err != nil {
				return err
			}

			if newBody.Annotations == nil {
				newBody.Annotations = map[string]string{}
			}
			newBody.Annotations[changeWorkflowUnitIDAnnotation] = unit.UnitID.String()
			newBody.Annotations[changeWorkflowRevisionAnnotation] = strconv.FormatInt(unit.HeadRevisionNum, 10)
		}
	}

	// Create params with AllowExists if needed
	params := &goclientnew.CreateChangeOrderParams{}
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}

	changeorderRes, err := cubClientNew.CreateChangeOrderWithResponse(ctx, spaceID, params, newBody)
	if cubapi.IsAPIError(err, changeorderRes) {
		return cubapi.InterpretErrorGeneric(err, changeorderRes)
	}

	changeorderDetails := changeorderRes.JSON200
	displayCreateResults(changeorderDetails, "changeorder", args[0], changeorderDetails.ChangeOrderID.String(), displayChangeOrderDetails)
	return nil
}

// resolveChangeOrderInScopeSpaces turns the spaces named on the command line into the ids stored
// on the change order. Each is a slug or a UUID, the way every other space flag takes one.
func resolveChangeOrderInScopeSpaces(identifiers []string) ([]uuid.UUID, error) {
	spaceIDs := make([]uuid.UUID, 0, len(identifiers))
	for _, identifier := range identifiers {
		space, err := apiGetSpaceFromSlug(identifier, "SpaceID,Slug")
		if err != nil {
			return nil, errors.Wrapf(err, "failed to resolve in-scope space %s", identifier)
		}
		spaceIDs = append(spaceIDs, space.SpaceID)
	}
	return spaceIDs, nil
}

func runBulkChangeOrderCreate() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from changeorder identifiers or use provided where clause
	var effectiveWhere string
	if len(changeorderCreateArgs.changeorderSlugs) > 0 {
		whereClause, err := buildWhereClauseFromChangeOrders(changeorderCreateArgs.changeorderSlugs)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Resolved before the enhancer runs, since it cannot report an error of its own.
	var inScopeSpaceIDStrings []string
	if len(changeorderCreateArgs.inScopeSpaces) > 0 {
		inScopeSpaceIDs, err := resolveChangeOrderInScopeSpaces(changeorderCreateArgs.inScopeSpaces)
		if err != nil {
			return err
		}
		for _, spaceID := range inScopeSpaceIDs {
			inScopeSpaceIDStrings = append(inScopeSpaceIDStrings, spaceID.String())
		}
	}

	// Create enhancer function for changeorder-specific fields
	enhancer := func(patchMap map[string]interface{}) {
		// Add changeorder-specific fields
		if changeorderCreateArgs.description != "" {
			patchMap["Description"] = changeorderCreateArgs.description
		}
		if len(changeorderCreateArgs.inScopeSpaces) > 0 {
			patchMap["InScopeSpaceIDs"] = inScopeSpaceIDStrings
		}
	}

	// Build patch data using consolidated function
	patchJSON, err := BuildPatchData(enhancer)
	if err != nil {
		return err
	}

	// Build bulk create parameters
	include := "SpaceID"
	params := &goclientnew.BulkCreateChangeOrdersParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// Set allow_exists parameter if flag is set
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}

	// Add name prefixes if specified
	if len(changeorderCreateArgs.namePrefixes) > 0 {
		namePrefixesStr := strings.Join(changeorderCreateArgs.namePrefixes, ",")
		params.NamePrefixes = &namePrefixesStr
	}

	// Add variant labels if specified
	if len(changeorderCreateArgs.variantLabels) > 0 {
		variantLabelsStr := strings.Join(changeorderCreateArgs.variantLabels, ",")
		params.VariantLabels = &variantLabelsStr
	}

	// Add name pattern if specified
	if changeorderCreateArgs.namePattern != "" {
		params.NamePattern = &changeorderCreateArgs.namePattern
	}

	// Set where_space parameter - either from direct where-space flag or converted from dest-space
	var whereSpaceExpr string
	if changeorderCreateArgs.whereSpace != "" {
		whereSpaceExpr = changeorderCreateArgs.whereSpace
	} else if len(changeorderCreateArgs.destSpaces) > 0 {
		// Convert dest-space identifiers to a where expression
		whereSpaceExpr, err = buildWhereClauseForSpaces(changeorderCreateArgs.destSpaces)
		if err != nil {
			return errors.Wrapf(err, "error converting destination spaces to where expression")
		}
	}

	if whereSpaceExpr != "" {
		params.WhereSpace = &whereSpaceExpr
	}

	// Parse and set filter_space parameter if specified
	if changeorderCreateArgs.filterSpace != "" {
		filterSpaceID, err := parseFilterFlag(changeorderCreateArgs.filterSpace)
		if err != nil {
			return errors.Wrapf(err, "error parsing filter-space")
		}
		params.FilterSpace = &filterSpaceID
	}

	// Call the bulk create API
	bulkRes, err := cubClientNew.BulkCreateChangeOrdersWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	// Handle the response
	return handleBulkChangeOrderCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "create", effectiveWhere)
}
