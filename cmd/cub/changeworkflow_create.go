// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/changeworkflow"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/core/worker/api"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

// A ChangeWorkflow definition is a document ConfigHub reads itself rather than delivers
// anywhere, and it is held as Kubernetes/YAML. The server leaves a Kubernetes Unit to the
// Provider defaulting every other Kubernetes Unit gets, so the create path below pins the
// definition to ProviderNone itself to keep it out of its Space's Releases.
const changeWorkflowToolchainType = string(workerapi.ToolchainKubernetesYAML)

// changeWorkflowStageLabel is the Space label a Stage authored with --stage selects on. It is
// the label "cub variant create --stage" sets, so a workflow whose stage names are the
// variants' stages needs no selectors written out at all.
const changeWorkflowStageLabel = "Stage"

var changeworkflowCreateCmd = &cobra.Command{
	Use:         "create [<slug> [<definition-file>]]",
	Short:       "Create a new ChangeWorkflow Unit or bulk create ChangeWorkflow Units",
	Long:        getChangeWorkflowCreateHelp(),
	Args:        cobra.MaximumNArgs(2), // Allow 0 args for bulk mode
	RunE:        changeworkflowCreateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func getChangeWorkflowCreateHelp() string {
	baseHelp := `Create a Unit holding a ChangeWorkflow definition, or bulk create ChangeWorkflow Units by
cloning existing ones.

SINGLE CHANGEWORKFLOW CREATION:

A ChangeWorkflow says how a change is promoted: the ordered stages it moves through, which
Spaces each stage selects, and the gates that have to pass before it enters one. The definition
is a YAML document, and this command creates the Kubernetes/YAML Unit that holds it, from a file
or from flags.

A stage selects its Spaces with a "whereSpace" expression over Space labels. It must not name
Labels.Component: the component is the change order's own and is appended to every stage's
selector, which is what lets one definition be cloned to give another component the same shape
of rollout. A stage named with --stage selects "Labels.` + changeWorkflowStageLabel + ` = '<name>'", which is what
"cub variant create --stage" labels a variant's Space with. A stage that selects its Spaces
some other way is written as a definition file.

The gates a stage can declare are:
  released   every Space of the stage ahead has published a Release carrying the change
  healthy    every Space of the stage ahead reports it Synced, Succeeded and Healthy

--prerequisites is one set of gates, given to every stage and to the final stage alike. A
stage's gates are its entry gates: they are checked over the stage before it, so the first
stage's are never evaluated. The final stage's are what the last stage must satisfy for the
rollout to read as completed, which no promotion can gate because no hop is left. A rollout
whose stages gate differently from one another is written as a definition file.

Single Examples:
` + "```" + `
  # From a file, or from stdin with "-"
  cub changeworkflow create --space workflows myapp-main-line workflow.yaml

  # From flags. Each --stage names one stage, given in the order a change is promoted through
  # them, and --prerequisites gates every one of them alike.
  cub changeworkflow create --space workflows myapp-main-line \
    --stage dev --stage staging --stage prod \
    --prerequisites released,healthy

  # Then create a change order governed by it
  cub changeorder create --space myapp-base bump-base-image \
    --change-workflow workflows/myapp-main-line
` + "```" + `

BULK CHANGEWORKFLOW CREATION:

When no positional arguments are provided, bulk create mode is activated. This mode clones
existing ChangeWorkflow Units and creates multiple new ones with optional modifications. Only
Units of the Kubernetes/YAML toolchain are selected, since that is what a definition is held as.

A clone carries the definition as it stands, metadata.name included, so a clone still names the
workflow it was cloned from. That name is what promotions and "cub changeorder get" report the
workflow as; change it with "cub unit update" where it matters.

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

  # Clone with modifications via JSON merge patch
  echo '{"DisplayName": "Standard rollout"}' | cub changeworkflow create \
    --changeworkflow myapp-main-line --dest-space workflows --name-prefix std- --from-stdin
` + "```" + `
`

	return getCommandHelp(baseHelp, "")
}

var changeworkflowCreateArgs struct {
	// Single create specific flags
	stages            []string
	prerequisites     []string
	changeDescription string
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
	enableWaitFlag(changeworkflowCreateCmd)
	enableWhereFlag(changeworkflowCreateCmd)
	enableFilterFlag(changeworkflowCreateCmd)

	// Single create specific flags
	changeworkflowCreateCmd.Flags().StringSliceVar(&changeworkflowCreateArgs.stages, "stage", nil, "name of one stage of the workflow (can be repeated or comma-separated), given in the order a change is promoted through them. A stage selects the Spaces labeled \"Labels."+changeWorkflowStageLabel+" = '<name>'\", which is what \"cub variant create --stage\" sets; a stage selecting its Spaces some other way is written as a definition file")
	changeworkflowCreateCmd.Flags().StringSliceVar(&changeworkflowCreateArgs.prerequisites, "prerequisites", nil, "gates given to every stage and to the final stage (can be repeated or comma-separated): "+strings.Join(knownPrerequisites, ", ")+". A stage's gates are checked over every Space of the stage ahead of it, so the first stage's are never evaluated")
	changeworkflowCreateCmd.Flags().StringVar(&changeworkflowCreateArgs.changeDescription, "change-desc", "", "change description recorded on the revision the definition is written as")

	// Bulk create specific flags
	changeworkflowCreateCmd.Flags().StringSliceVar(&changeworkflowCreateArgs.changeworkflowSlugs, "changeworkflow", []string{}, "target specific ChangeWorkflow Units by slug or UUID for bulk create (can be repeated or comma-separated)")
	changeworkflowCreateCmd.Flags().StringSliceVar(&changeworkflowCreateArgs.destSpaces, "dest-space", []string{}, "destination spaces for bulk create (can be repeated or comma-separated)")
	changeworkflowCreateCmd.Flags().StringVar(&changeworkflowCreateArgs.whereSpace, "where-space", "", "where expression to select destination spaces for bulk create")
	changeworkflowCreateCmd.Flags().StringSliceVar(&changeworkflowCreateArgs.namePrefixes, "name-prefix", []string{}, "name prefixes for bulk create (can be repeated or comma-separated)")
	changeworkflowCreateCmd.Flags().StringSliceVar(&changeworkflowCreateArgs.variantLabels, "variant-labels", []string{}, "labels for bulk create in the format of key1=value1|value2,key2=value1|value2|value3")
	changeworkflowCreateCmd.Flags().StringVar(&changeworkflowCreateArgs.namePattern, "name-pattern", "", "a pattern string for name generation of clones, prefix 'template:' to use a Go template with .SourceEntitySlug to access the original Unit and .Labels to access variant labels, example: 'template:{{.SourceEntitySlug}}-{{.Labels.env}}'")
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

		// A bulk create clones definitions that already exist, so there is nothing here for a
		// definition of its own to be part of.
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

		// The two ways to say what the definition is. They answer the same question, so
		// giving both leaves no way to tell which was meant, and giving neither creates a
		// Unit no change order could be governed by.
		fromFile := len(args) > 1
		fromFlags := len(changeworkflowCreateArgs.stages) > 0 || len(changeworkflowCreateArgs.prerequisites) > 0

		if fromFile && fromFlags {
			return false, errors.New("a definition file and --stage/--prerequisites are mutually exclusive: the file is the definition")
		}
		if !fromFile && !fromFlags {
			return false, errors.New("a ChangeWorkflow definition is required: give a file (or \"-\" for stdin), or name the stages with --stage")
		}
		// The gates are carried by the stages, so there have to be some.
		if len(changeworkflowCreateArgs.prerequisites) > 0 && len(changeworkflowCreateArgs.stages) == 0 {
			return false, errors.New("--prerequisites needs at least one --stage")
		}
		if fromFile && args[1] == "-" && flagPopulateModelFromStdin {
			return false, errors.New("can't read both entity attributes and the ChangeWorkflow definition from stdin")
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

// changeWorkflowDefinition is the document the Unit will hold, along with where it came from
// for the Unit's external source. Either the file the caller named or the one the flags
// describe, and either way it is checked here, so a definition a promotion could not run is
// refused at authoring time rather than at the first promotion that reads it.
func changeWorkflowDefinition(args []string, slug string) (string, string, error) {
	if len(args) > 1 {
		content, err := fetchContent(args[1])
		if err != nil {
			return "", "", errors.Wrap(err, "failed to read the ChangeWorkflow definition")
		}
		definition := &changeworkflow.ChangeWorkflow{}
		if err := yaml.Unmarshal(content, definition); err != nil {
			return "", "", errors.Wrapf(err, "%s does not hold a ChangeWorkflow definition", args[1])
		}
		if err := validateChangeWorkflow(definition); err != nil {
			return "", "", errors.Wrapf(err, "%s does not hold a usable ChangeWorkflow definition", args[1])
		}
		source := args[1]
		if source == "-" {
			source = "stdin"
		}
		// Stored as it was written. Round-tripping it through the struct would drop its
		// comments, and a definition is a document someone maintains.
		return string(content), source, nil
	}

	definition, err := changeWorkflowFromFlags(slug)
	if err != nil {
		return "", "", err
	}
	if err := validateChangeWorkflow(definition); err != nil {
		return "", "", err
	}
	rendered, err := yaml.Marshal(definition)
	if err != nil {
		return "", "", errors.Wrap(err, "failed to render the ChangeWorkflow definition")
	}
	return string(rendered), "", nil
}

// changeWorkflowFromFlags builds the definition --stage and --prerequisites describe.
// metadata.name is the Unit's slug: the document names itself, and that name is what a
// promotion reports the workflow as.
func changeWorkflowFromFlags(slug string) (*changeworkflow.ChangeWorkflow, error) {
	definition := &changeworkflow.ChangeWorkflow{}
	definition.APIVersion = changeworkflow.APIVersion
	definition.Kind = changeworkflow.Kind
	definition.Name = slug

	for _, name := range changeworkflowCreateArgs.stages {
		stage, err := changeWorkflowStage(name)
		if err != nil {
			return nil, err
		}
		// Cloned so the stages do not go on sharing one slice with each other and with the
		// final stage, which a later edit to any of them would write through.
		stage.Prerequisites = slices.Clone(changeworkflowCreateArgs.prerequisites)
		definition.Spec.Stages = append(definition.Spec.Stages, *stage)
	}
	definition.Spec.Final.Prerequisites = slices.Clone(changeworkflowCreateArgs.prerequisites)
	return definition, nil
}

// changeWorkflowStage builds the stage --stage names, which selects the Spaces labeled with
// that name. A stage selecting its Spaces any other way is written as a definition file.
func changeWorkflowStage(name string) (*changeworkflow.ChangeWorkflowStage, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("invalid --stage: a stage name is required")
	}
	// The name goes into an expression, so one carrying a quote would produce a selector that
	// does not parse. Such a stage has to name its Spaces itself, in a definition file.
	if strings.Contains(name, "'") {
		return nil, errors.Newf("stage '%s' has a quote in its name, so it needs a whereSpace of its own, which only a definition file can give it", name)
	}
	return &changeworkflow.ChangeWorkflowStage{
		Name:       name,
		WhereSpace: fmt.Sprintf("Labels.%s = '%s'", changeWorkflowStageLabel, name),
	}, nil
}

// validateChangeWorkflow refuses a definition a promotion could not run. What it checks is
// what promotion insists on: the stages it walks in order, the selectors it renders
// (stageWhereSpace) and the gates it knows how to evaluate (checkVariantPrerequisites).
func validateChangeWorkflow(definition *changeworkflow.ChangeWorkflow) error {
	if definition.APIVersion != changeworkflow.APIVersion {
		return errors.Newf("apiVersion is %q, expected %q", definition.APIVersion, changeworkflow.APIVersion)
	}
	if definition.Kind != changeworkflow.Kind {
		return errors.Newf("kind is %q, expected %q", definition.Kind, changeworkflow.Kind)
	}
	if definition.Name == "" {
		return errors.New("metadata.name is required: it is what a promotion reports the workflow as")
	}
	if len(definition.Spec.Stages) == 0 {
		return errors.New("spec.stages is empty: a workflow with no stages has nowhere to promote a change to")
	}

	named := map[string]bool{}
	for i := range definition.Spec.Stages {
		stage := &definition.Spec.Stages[i]
		if stage.Name == "" {
			return errors.Newf("spec.stages[%d] has no name", i)
		}
		// --target-stage names a stage and a promotion reports the one it entered, so two
		// stages answering to one name leave both ambiguous.
		if named[stage.Name] {
			return errors.Newf("spec.stages has more than one stage named '%s'", stage.Name)
		}
		named[stage.Name] = true
		// A definition names no component -- the change order supplies it -- so the clause
		// stageWhereSpace renders here is meaningless and discarded. It is asked anyway so
		// that what a stage may not say, and how that is worded, lives in one place.
		if _, err := stageWhereSpace(stage, ""); err != nil {
			return err
		}
		if err := validateChangeWorkflowPrerequisites(stage.Prerequisites, fmt.Sprintf("stage '%s'", stage.Name)); err != nil {
			return err
		}
	}
	return validateChangeWorkflowPrerequisites(definition.Spec.Final.Prerequisites, "spec.final")
}

// validateChangeWorkflowPrerequisites refuses a gate promotion has no check for. The names are
// the ones the promotion evaluates (checkVariantPrerequisites), so a definition accepted here
// is one it can run.
func validateChangeWorkflowPrerequisites(prerequisites []string, where string) error {
	for _, prerequisite := range prerequisites {
		if !slices.Contains(knownPrerequisites, prerequisite) {
			return errors.Newf("unrecognized prerequisite for %s: '%s'; the gates are: %s",
				where, prerequisite, strings.Join(knownPrerequisites, ", "))
		}
	}
	return nil
}

func runSingleChangeWorkflowCreate(args []string) error {
	spaceID := uuid.MustParse(selectedSpaceID)
	slug := makeSlug(args[0])

	// The definition is settled before the Unit is created, so one nothing could promote
	// under leaves no half-made Unit behind.
	definition, source, err := changeWorkflowDefinition(args, slug)
	if err != nil {
		return err
	}

	newUnit := &goclientnew.Unit{}
	if flagPopulateModelFromStdin || flagFilename != "" {
		if err := populateModelFromFlags(newUnit); err != nil {
			return err
		}
	}
	if err := setAnnotations(&newUnit.Annotations); err != nil {
		return err
	}
	if err := setLabels(&newUnit.Labels); err != nil {
		return err
	}
	if err := setDeleteGates(&newUnit.DeleteGates); err != nil {
		return err
	}
	if changeworkflowCreateArgs.changeDescription != "" {
		newUnit.LastChangeDescription = changeworkflowCreateArgs.changeDescription
	}

	// Set after stdin so that what the command was told wins over what it was handed.
	newUnit.SpaceID = spaceID
	newUnit.Slug = slug
	newUnit.ToolchainType = changeWorkflowToolchainType
	// A definition is read by ConfigHub itself and never delivered to a Provider, so it is
	// held to ProviderNone rather than left to the server's default for the toolchain, which
	// only applies to a Unit that arrives without one. ProviderNone is also what keeps a
	// definition out of its Space's Releases.
	newUnit.ProviderType = string(api.ProviderNone)
	if newUnit.DisplayName == "" {
		newUnit.DisplayName = args[0]
	}

	newParams := &goclientnew.CreateUnitParams{}
	if allowExists {
		allowExistsStr := "true"
		newParams.AllowExists = &allowExistsStr
	}
	if source != "" {
		newParams.MergeExternalSource = &source
	}

	unitRes, err := cubClientNew.CreateUnitWithResponse(ctx, spaceID, newParams, *newUnit)
	if cubapi.IsAPIError(err, unitRes) {
		return cubapi.InterpretErrorGeneric(err, unitRes)
	}
	unitDetails, err := unitFromWrite(unitRes.JSON200)
	if err != nil {
		return err
	}

	// Configuration is not part of a Unit, so the definition is a second call. The Unit is
	// left in place if that fails, as in "cub unit create": it exists, somebody may already
	// be looking at it, and deleting it would be a worse outcome than an empty Unit the
	// caller can write to again.
	dataParams, err := unitDataParamsFromCreate(newParams,
		newUnit.LastChangeDescription, changeSetIDForDataWrite(newUnit))
	if err != nil {
		return err
	}
	if _, err := putUnitData(spaceID, unitDetails.UnitID, definition, dataParams); err != nil {
		return errors.Wrapf(err, "unit %s was created, but the ChangeWorkflow definition could not be written",
			unitDetails.Slug)
	}
	// Re-read so what is displayed reflects the definition that was just written.
	if refreshed, refreshErr := apiGetUnitInSpace(unitDetails.UnitID.String(), spaceID.String(), "*"); refreshErr == nil {
		unitDetails = refreshed
	}

	if wait {
		if err := awaitTriggersRemoval(unitDetails); err != nil {
			return err
		}
	}
	displayCreateResults(unitDetails, "changeworkflow", args[0], unitDetails.UnitID.String(), displayUnitDetails)
	return nil
}

// addChangeWorkflowToolchainToWhereClause confines a bulk selection to Units of the toolchain a
// definition is held as. A clause selecting by slug or label would otherwise reach Units of any
// toolchain, and cloning one of those through this command would produce something no change
// order could ever be governed by.
func addChangeWorkflowToolchainToWhereClause(whereClause string) string {
	toolchainConstraint := fmt.Sprintf("ToolchainType = '%s'", changeWorkflowToolchainType)
	if whereClause != "" {
		return fmt.Sprintf("%s AND %s", whereClause, toolchainConstraint)
	}
	return toolchainConstraint
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
		whereClause, err := buildWhereClauseFromUnits(changeworkflowCreateArgs.changeworkflowSlugs)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)
	effectiveWhere = addChangeWorkflowToolchainToWhereClause(effectiveWhere)

	// Create enhancer function for changeworkflow-specific fields
	enhancer := func(patchMap map[string]interface{}) {
		// Held to ProviderNone whatever the Unit it was cloned from carried, for the same
		// reason a single create is.
		patchMap["ProviderType"] = string(api.ProviderNone)
		if changeworkflowCreateArgs.changeDescription != "" {
			patchMap["LastChangeDescription"] = changeworkflowCreateArgs.changeDescription
		}
	}

	// Build patch data using consolidated function
	patchJSON, err := BuildPatchData(enhancer)
	if err != nil {
		return err
	}

	// Build bulk create parameters
	include := "UnitEventID,SpaceID"
	params := &goclientnew.BulkCreateUnitsParams{
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
	responses, statusCode, err := bulkCreateUnits(params, patchJSON)
	if err != nil {
		return err
	}

	return handleBulkCreateOrUpdateResponse(responses, statusCode, "create", "")
}
