// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// changeWorkflowStageLabel is the Space label a stage authored with --stage selects on. It is the
// label "cub variant create --stage" sets, so a workflow whose stage names are the variants'
// stages needs no selectors written out at all.
const changeWorkflowStageLabel = "Stage"

var changeworkflowCreateCmd = &cobra.Command{
	Use:         "create [<slug>]",
	Short:       "Create a new change workflow or bulk create change workflows",
	Long:        getChangeWorkflowCreateHelp(),
	Args:        cobra.MaximumNArgs(1), // Allow 0 args for bulk mode
	RunE:        changeworkflowCreateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func getChangeWorkflowCreateHelp() string {
	baseHelp := `Create a new change workflow, or bulk create change workflows by cloning existing ones.

SINGLE CHANGEWORKFLOW CREATION:

A ChangeWorkflow says how a change is promoted: the ordered stages it moves through, which Spaces
each stage selects, and the gates that have to pass before it enters one. Give the stages with
--stage, or write the whole workflow in a file and pass it with --filename.

A stage selects its Spaces with a "WhereSpace" expression over Space labels. It must not name
Labels.Component: the component is the change order's own and is appended to every stage's
selector, which is what lets one workflow be cloned to give another component the same shape of
rollout. A stage named with --stage selects "Labels.` + changeWorkflowStageLabel + ` = '<name>'", which is what
"cub variant create --stage" labels a variant's Space with. A stage that selects its Spaces some
other way is written in a file.

The gates a stage can declare are:
  Released   every Space of the stage ahead has published a Release carrying the change
  Healthy    every Space of the stage ahead reports it Synced, Succeeded and Healthy

--prerequisites is one set of gates, given to every stage and to the final stage alike. A stage's
gates are its entry gates: they are checked over the stage before it, so the first stage's are
never evaluated. The final stage's are what the last stage must satisfy for the rollout to read as
completed, which no promotion can gate because no hop is left. A rollout whose stages gate
differently from one another is written in a file, as is one declaring custom prerequisites.

Single Examples:
` + "```" + `
  # From flags. Each --stage names one stage, given in the order a change is promoted through
  # them, and --prerequisites gates every one of them alike.
  cub changeworkflow create --space workflows myapp-main-line \
    --stage dev --stage staging --stage prod \
    --prerequisites Released,Healthy

  # From a file, or from stdin with --from-stdin
  cub changeworkflow create --space workflows myapp-main-line --filename workflow.yaml

  # Then create a change order governed by it
  cub changeorder create --space myapp-base bump-base-image \
    --change-workflow workflows/myapp-main-line
` + "```" + `

A workflow file holds the entity, so its fields are the ones "cub changeworkflow get --json"
prints:
` + "```" + `
  Stages:
    - Name: dev
      WhereSpace: "Labels.Stage = 'dev'"
    - Name: prod
      WhereSpace: "Labels.Stage = 'prod'"
      Prerequisites: [Released, Healthy, code-freeze]
  Final:
    Prerequisites: [Released, Healthy]
  CustomPrerequisites:
    - Name: code-freeze
      Expression: "cel:Space.Annotations['code-freeze'] != 'true'"
` + "```" + `

BULK CHANGEWORKFLOW CREATION:

When no positional arguments are provided, bulk create mode is activated. This mode clones
existing change workflows and creates multiple new ones with optional modifications.

Cloning is how a workflow shape is shared across components: a workflow names no component, so a
clone governs whatever component's change orders name it.

Bulk Create Examples:
` + "```" + `
  # Give every team's workflow space a copy of the standard rollout
  cub changeworkflow create --changeworkflow myapp-main-line \
    --dest-space payments-workflows,search-workflows

  # Clone the workflows of one space into every space a where expression selects
  cub changeworkflow create --space workflows --where "Slug LIKE '%-main-line'" \
    --where-space "Labels.Kind = 'workflows'"

  # Clone with a name prefix, and label the clones
  cub changeworkflow create --changeworkflow myapp-main-line \
    --dest-space workflows --name-prefix canary- --label "Rollout=canary"
` + "```" + `
`

	return getCommandHelp(baseHelp, "")
}

var changeworkflowCreateArgs struct {
	// Single create specific flags
	stages        []string
	prerequisites []string
	// Bulk create specific flags
	changeworkflowSlugs []string
	destSpaces          []string
	whereSpace          string
	namePrefixes        []string
	variantLabels       []string
	namePattern         string
	filterSpace         string
}

func init() {
	addStandardCreateFlags(changeworkflowCreateCmd)
	enableWhereFlag(changeworkflowCreateCmd)
	enableFilterFlag(changeworkflowCreateCmd)

	// Single create specific flags
	changeworkflowCreateCmd.Flags().StringSliceVar(&changeworkflowCreateArgs.stages, "stage", nil, "name of one stage of the workflow (can be repeated or comma-separated), given in the order a change is promoted through them. A stage selects the Spaces labeled \"Labels."+changeWorkflowStageLabel+" = '<name>'\", which is what \"cub variant create --stage\" sets; a stage selecting its Spaces some other way is written in a file")
	changeworkflowCreateCmd.Flags().StringSliceVar(&changeworkflowCreateArgs.prerequisites, "prerequisites", nil, "gates given to every stage and to the final stage (can be repeated or comma-separated): "+strings.Join(knownPrerequisites, ", ")+". A stage's gates are checked over every Space of the stage ahead of it, so the first stage's are never evaluated. A custom prerequisite is declared under CustomPrerequisites and gated on by name, which only a file can carry")

	// Bulk create specific flags
	changeworkflowCreateCmd.Flags().StringSliceVar(&changeworkflowCreateArgs.changeworkflowSlugs, "changeworkflow", []string{}, "target specific change workflows by slug or UUID for bulk create (can be repeated or comma-separated)")
	changeworkflowCreateCmd.Flags().StringSliceVar(&changeworkflowCreateArgs.destSpaces, "dest-space", []string{}, "destination spaces for bulk create (can be repeated or comma-separated)")
	changeworkflowCreateCmd.Flags().StringVar(&changeworkflowCreateArgs.whereSpace, "where-space", "", "where expression to select destination spaces for bulk create")
	changeworkflowCreateCmd.Flags().StringSliceVar(&changeworkflowCreateArgs.namePrefixes, "name-prefix", []string{}, "name prefixes for bulk create (can be repeated or comma-separated)")
	changeworkflowCreateCmd.Flags().StringSliceVar(&changeworkflowCreateArgs.variantLabels, "variant-labels", []string{}, "labels for bulk create in the format of key1=value1|value2,key2=value1|value2|value3")
	changeworkflowCreateCmd.Flags().StringVar(&changeworkflowCreateArgs.namePattern, "name-pattern", "", "a pattern string for name generation of clones, prefix 'template:' to use a Go template with .SourceEntitySlug to access the original ChangeWorkflow and .Labels to access variant labels, example: 'template:{{.SourceEntitySlug}}-{{.Labels.env}}'")
	changeworkflowCreateCmd.Flags().StringVar(&changeworkflowCreateArgs.filterSpace, "filter-space", "", "filter entity containing WHERE expression to select destination spaces for bulk create (slug or UUID)")

	changeworkflowCmd.AddCommand(changeworkflowCreateCmd)
}

func checkChangeWorkflowCreateConflictingArgs(args []string) (bool, error) {
	// Determine if bulk create mode: no positional args
	isBulkCreateMode := len(args) == 0

	if isBulkCreateMode {
		// Validate bulk create requirements
		if len(changeworkflowCreateArgs.changeworkflowSlugs) > 0 && where != "" {
			return false, errors.New("--changeworkflow and --where flags are mutually exclusive")
		}

		if len(changeworkflowCreateArgs.destSpaces) > 0 && changeworkflowCreateArgs.whereSpace != "" {
			return false, errors.New("--dest-space and --where-space flags are mutually exclusive")
		}

		if len(changeworkflowCreateArgs.destSpaces) == 0 && changeworkflowCreateArgs.whereSpace == "" && len(changeworkflowCreateArgs.namePrefixes) == 0 && len(changeworkflowCreateArgs.variantLabels) == 0 {
			return false, errors.New("bulk create mode requires at least one of --dest-space, --where-space, --name-prefix, or --variant-labels")
		}

		if len(changeworkflowCreateArgs.namePrefixes) > 0 && len(changeworkflowCreateArgs.variantLabels) > 0 {
			return false, errors.New("--name-prefix and --variant-labels cannot be used together")
		}

		if changeworkflowCreateArgs.namePattern != "" && len(changeworkflowCreateArgs.namePrefixes) > 0 {
			return false, errors.New("--name-pattern and --name-prefix cannot be used together")
		}

		if changeworkflowCreateArgs.namePattern != "" && len(changeworkflowCreateArgs.variantLabels) == 0 {
			return false, errors.New("--name-pattern requires --variant-labels to be set")
		}

		// A bulk create clones workflows that already exist, so there is nothing here for a
		// workflow of its own to be part of.
		if len(changeworkflowCreateArgs.stages) > 0 || len(changeworkflowCreateArgs.prerequisites) > 0 {
			return false, errors.New("--stage and --prerequisites can only be used with single ChangeWorkflow creation")
		}
	} else {
		// Single create mode validation
		if filter != "" || where != "" || changeworkflowCreateArgs.namePattern != "" ||
			len(changeworkflowCreateArgs.changeworkflowSlugs) > 0 || len(changeworkflowCreateArgs.destSpaces) > 0 ||
			changeworkflowCreateArgs.whereSpace != "" || len(changeworkflowCreateArgs.namePrefixes) > 0 ||
			len(changeworkflowCreateArgs.variantLabels) > 0 {
			return false, errors.New(
				"bulk create flags (--filter, --where, --changeworkflow, --dest-space, --where-space, --name-prefix, --variant-labels, --name-pattern) can only be used without positional arguments",
			)
		}

		// The two ways to say what the workflow is. They answer the same question, so giving both
		// leaves no way to tell which was meant, and giving neither creates a workflow with no
		// stages, which no change order could be promoted under.
		fromFile := flagPopulateModelFromStdin || flagFilename != ""
		fromFlags := len(changeworkflowCreateArgs.stages) > 0 || len(changeworkflowCreateArgs.prerequisites) > 0

		if fromFile && fromFlags {
			return false, errors.New("--filename/--from-stdin and --stage/--prerequisites are mutually exclusive: the file is the workflow")
		}
		if !fromFile && !fromFlags {
			return false, errors.New("a ChangeWorkflow needs stages: name them with --stage, or give the whole workflow with --filename (or --from-stdin)")
		}
		// The gates are carried by the stages, so there have to be some.
		if len(changeworkflowCreateArgs.prerequisites) > 0 && len(changeworkflowCreateArgs.stages) == 0 {
			return false, errors.New("--prerequisites needs at least one --stage")
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

func changeworkflowCreateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkCreateMode, err := checkChangeWorkflowCreateConflictingArgs(args)
	if err != nil {
		return err
	}

	if isBulkCreateMode {
		return runBulkChangeWorkflowCreate()
	}

	return runSingleChangeWorkflowCreate(args)
}

// changeWorkflowStagesFromFlags builds the stages --stage and --prerequisites describe, along with
// the final stage's gates, which are the same set. Create and update both name their stages this
// way, so what a stage name means is decided here rather than by each command.
func changeWorkflowStagesFromFlags(stageNames, prerequisites []string) ([]goclientnew.ChangeWorkflowStage, *goclientnew.ChangeWorkflowFinalStage, error) {
	stages := make([]goclientnew.ChangeWorkflowStage, 0, len(stageNames))
	for _, name := range stageNames {
		stage, err := changeWorkflowStage(name)
		if err != nil {
			return nil, nil, err
		}
		// Cloned so the stages do not go on sharing one slice with each other and with the final
		// stage, which a later edit to any of them would write through.
		stage.Prerequisites = slices.Clone(prerequisites)
		stages = append(stages, *stage)
	}
	final := &goclientnew.ChangeWorkflowFinalStage{
		Prerequisites: slices.Clone(prerequisites),
	}
	return stages, final, nil
}

// changeWorkflowStage builds the stage --stage names, which selects the Spaces labeled with that
// name. A stage selecting its Spaces any other way is written in a file.
func changeWorkflowStage(name string) (*goclientnew.ChangeWorkflowStage, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("invalid --stage: a stage name is required")
	}
	// The name goes into an expression, so one carrying a quote would produce a selector that does
	// not parse. Such a stage has to name its Spaces itself, in a file.
	if strings.Contains(name, "'") {
		return nil, errors.Newf("stage '%s' has a quote in its name, so it needs a WhereSpace of its own, which only a file can give it", name)
	}
	return &goclientnew.ChangeWorkflowStage{
		Name:       name,
		WhereSpace: fmt.Sprintf("Labels.%s = '%s'", changeWorkflowStageLabel, name),
	}, nil
}

func runSingleChangeWorkflowCreate(args []string) error {
	spaceID := uuid.MustParse(selectedSpaceID)

	newBody := &goclientnew.ChangeWorkflow{}
	if flagPopulateModelFromStdin || flagFilename != "" {
		if err := populateModelFromFlags(newBody); err != nil {
			return err
		}
	}
	if err := setAnnotations(&newBody.Annotations); err != nil {
		return err
	}
	if err := setLabels(&newBody.Labels); err != nil {
		return err
	}
	if err := setDeleteGates(&newBody.DeleteGates); err != nil {
		return err
	}

	if len(changeworkflowCreateArgs.stages) > 0 {
		stages, final, err := changeWorkflowStagesFromFlags(
			changeworkflowCreateArgs.stages, changeworkflowCreateArgs.prerequisites)
		if err != nil {
			return err
		}
		newBody.Stages = stages
		newBody.Final = final
	}

	// Set after the file so that what the command was told wins over what it was handed.
	newBody.SpaceID = spaceID
	newBody.Slug = makeSlug(args[0])
	if newBody.DisplayName == "" {
		newBody.DisplayName = args[0]
	}

	params := &goclientnew.CreateChangeWorkflowParams{}
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}

	changeWorkflowRes, err := cubClientNew.CreateChangeWorkflowWithResponse(ctx, spaceID, params, *newBody)
	if cubapi.IsAPIError(err, changeWorkflowRes) {
		return cubapi.InterpretErrorGeneric(err, changeWorkflowRes)
	}

	changeWorkflowDetails := changeWorkflowRes.JSON200
	displayCreateResults(changeWorkflowDetails, "changeworkflow", args[0],
		changeWorkflowDetails.ChangeWorkflowID.String(), displayChangeWorkflowDetails)
	return nil
}

func runBulkChangeWorkflowCreate() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from changeworkflow identifiers or use provided where clause
	var effectiveWhere string
	if len(changeworkflowCreateArgs.changeworkflowSlugs) > 0 {
		whereClause, err := buildWhereClauseFromChangeWorkflows(changeworkflowCreateArgs.changeworkflowSlugs)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Build patch data using consolidated function
	patchJSON, err := BuildPatchData(nil)
	if err != nil {
		return err
	}

	// Build bulk create parameters
	include := "SpaceID"
	params := &goclientnew.BulkCreateChangeWorkflowsParams{
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
	if len(changeworkflowCreateArgs.namePrefixes) > 0 {
		namePrefixesStr := strings.Join(changeworkflowCreateArgs.namePrefixes, ",")
		params.NamePrefixes = &namePrefixesStr
	}

	// Add variant labels if specified
	if len(changeworkflowCreateArgs.variantLabels) > 0 {
		variantLabelsStr := strings.Join(changeworkflowCreateArgs.variantLabels, ",")
		params.VariantLabels = &variantLabelsStr
	}

	// Add name pattern if specified
	if changeworkflowCreateArgs.namePattern != "" {
		params.NamePattern = &changeworkflowCreateArgs.namePattern
	}

	// Set where_space parameter - either from direct where-space flag or converted from dest-space
	var whereSpaceExpr string
	if changeworkflowCreateArgs.whereSpace != "" {
		whereSpaceExpr = changeworkflowCreateArgs.whereSpace
	} else if len(changeworkflowCreateArgs.destSpaces) > 0 {
		// Convert dest-space identifiers to a where expression
		whereSpaceExpr, err = buildWhereClauseForSpaces(changeworkflowCreateArgs.destSpaces)
		if err != nil {
			return errors.Wrapf(err, "error converting destination spaces to where expression")
		}
	}

	if whereSpaceExpr != "" {
		params.WhereSpace = &whereSpaceExpr
	}

	// Parse and set filter_space parameter if specified
	if changeworkflowCreateArgs.filterSpace != "" {
		filterSpaceID, err := parseFilterFlag(changeworkflowCreateArgs.filterSpace)
		if err != nil {
			return errors.Wrapf(err, "error parsing filter-space")
		}
		params.FilterSpace = &filterSpaceID
	}

	// Call the bulk create API
	bulkRes, err := cubClientNew.BulkCreateChangeWorkflowsWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	return handleBulkChangeWorkflowCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "create", effectiveWhere)
}
