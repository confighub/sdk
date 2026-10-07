// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"github.com/confighub/sdk/core/cubapi"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
)

var flagPopulateModelFromStdin = false
var flagReplace = false
var flagFilename = ""
var where = ""
var filter = ""

// whereResource filters which resources within a Unit a function operates on.
// Shared by `run` and the function subcommands.
var whereResource string
var contains = ""
var includeHidden = ""
var verbose = false
var quiet = false
var jsonOutput = false
var yamlOutput = false
var jq = ""
var yq = ""
var names = false
var selectFields = ""
var debug = false
var noheader = false
var wait = true
var getWait = false
var timeout string

const DefaultTimeoutDuration = 10 * time.Minute
const DefaultCreationTimeoutDuration = 30 * time.Second

var annotation []string
var label []string
var deleteGate []string
var fact []string
var spaceIdentifiers []string
var allowExists bool

func enableAnnotationFlag(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&annotation, "annotation", []string{}, "annotations in key=value format; can separate by commas and/or use multiple instances of the flag")
}

func enableLabelFlag(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&label, "label", []string{}, "labels in key=value format; can separate by commas and/or use multiple instances of the flag")
}

func enableFactFlag(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&fact, "fact", []string{}, "facts in Key=Value format; use a separate --fact for each fact (values may contain commas, e.g. CRD lists); use Key=- with --patch to remove")
}

func enableAllowExistsFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&allowExists, "allow-exists", false, "Allow creation of resources that already exist")
}

// enableDryRunFlag adds --dry-run to a command that writes entities. The server makes the write,
// with every check it involves, and then rolls it back, so what the command reports is what the
// write would have done.
func enableDryRunFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what the command would write, and write nothing")
}

// dryRunParam is the dry_run parameter of an entity write: set only with --dry-run.
func dryRunParam() *bool {
	if !dryRun {
		return nil
	}
	dryRunValue := true
	return &dryRunValue
}

// permissionFlag is --permission for the commands that declare no flag of their own for it;
// space, component, target and worker do.
var permissionFlag []string
var displayNameFlag string
var hiddenReasonFlag string

// clearFlagValue is what a flag is given to clear the field it sets.
const clearFlagValue = "-"

func enableCreatePermissionFlag(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&permissionFlag, "permission", []string{}, "permission in format Action:UserIDOrUsername (e.g., Manage:user@example.com, can be repeated)")
}

func enableUpdatePermissionFlag(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&permissionFlag, "permission", []string{}, "permission in format Action:UserIDOrUsername to add, or -Action:UserIDOrUsername to remove (e.g., Manage:user@example.com, -View:user@example.com, can be repeated)")
}

func enableDisplayNameFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&displayNameFlag, "display-name", "", "friendly name for the entity, which need not be unique or URL-safe; an entity without one is shown by its slug")
}

func enableHiddenReasonFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&hiddenReasonFlag, "hidden-reason", "", "hide the entity, giving the reason as a name such as Archived: lists and bulk operations leave it out unless --include-hidden names the reason (use '-' to clear, which shows the entity again)")
}

// setDisplayNameAndHiddenReason applies --display-name and --hidden-reason to an entity that is
// written whole. A patch gets them from BuildPatchDataWithPermissions instead.
func setDisplayNameAndHiddenReason(displayName, hiddenReason *string) {
	if displayNameFlag != "" {
		*displayName = displayNameFlag
	}
	setHiddenReason(hiddenReason)
}

func setHiddenReason(hiddenReason *string) {
	switch hiddenReasonFlag {
	case "":
	case clearFlagValue:
		*hiddenReason = ""
	default:
		*hiddenReason = hiddenReasonFlag
	}
}

// hasMetadataFlags reports whether --annotation, --display-name, --hidden-reason, --set, --unset
// or a flag that is shorthand for those was given, for a command that refuses a patch nothing
// would be in.
func hasMetadataFlags() bool {
	return len(annotation) > 0 || displayNameFlag != "" || hiddenReasonFlag != "" ||
		len(setFlags) > 0 || len(unsetFlags) > 0 || len(pendingFieldEdits) > 0
}

// withDisplayNameAndHiddenReason adds --display-name and --hidden-reason to what enhancer puts in
// a patch. It returns enhancer itself, which may be nil, when neither flag was given, so that a
// patch nothing else adds to is left as it was read.
func withDisplayNameAndHiddenReason(enhancer PatchEnhancer) PatchEnhancer {
	if displayNameFlag == "" && hiddenReasonFlag == "" {
		return enhancer
	}
	return func(patchMap map[string]interface{}) {
		if enhancer != nil {
			enhancer(patchMap)
		}
		if displayNameFlag != "" {
			patchMap["DisplayName"] = displayNameFlag
		}
		switch hiddenReasonFlag {
		case "":
		case clearFlagValue:
			patchMap["HiddenReason"] = ""
		default:
			patchMap["HiddenReason"] = hiddenReasonFlag
		}
	}
}

func enableDeleteGateFlag(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&deleteGate, "delete-gate", []string{}, "delete gates in key[=true] format; can separate by commas and/or use multiple instances of the flag")
}

func setKeyValues(kvStrings []string, kvMap *map[string]string) error {
	if kvStrings != nil && len(kvStrings) != 0 {
		if *kvMap == nil {
			*kvMap = map[string]string{}
		}
		for _, kvString := range kvStrings {
			keyValue := strings.Split(kvString, "=")
			switch len(keyValue) {
			case 1:
				(*kvMap)[keyValue[0]] = ""
			case 2:
				// Note: For patch operations, value "-" indicates removal and is handled
				// by BuildPatchData and EnhancePatchData functions. This function only
				// handles non-patch (Put) operations where removal is not supported.
				(*kvMap)[keyValue[0]] = keyValue[1]
			default:
				return fmt.Errorf("expected key=value: %s", kvString)
			}
		}
	}
	return nil
}

func setAnnotations(annotationMap *map[string]string) error {
	err := setKeyValues(annotation, annotationMap)
	if err != nil {
		return fmt.Errorf("invalid annotation: %w", err)
	}

	return nil
}

func setLabels(labelMap *map[string]string) error {
	err := setKeyValues(label, labelMap)
	if err != nil {
		return fmt.Errorf("invalid label; %w", err)
	}

	return nil
}

func setFacts(factMap *map[string]string) error {
	err := setKeyValues(fact, factMap)
	if err != nil {
		return fmt.Errorf("invalid fact; %w", err)
	}

	return nil
}

func setDeleteGates(deleteGateMap *map[string]bool) error {
	return setGatesFromSlice(deleteGate, deleteGateMap)
}

// setGatesFromSlice parses key[=true] gate strings into gateMap. Takes the
// slice as a parameter (like setKeyValues) so callers with their own backing
// vars — e.g. cub variant create's --unit-delete-gate / --unit-destroy-gate /
// --space-delete-gate — reuse it instead of duplicating the parser. Only
// "true" is a valid explicit value; the "-" removal form is handled for patch
// operations by BuildPatchData / EnhancePatchData, not here.
func setGatesFromSlice(gateStrings []string, gateMap *map[string]bool) error {
	if len(gateStrings) == 0 {
		return nil
	}
	if *gateMap == nil {
		*gateMap = map[string]bool{}
	}
	for _, gateString := range gateStrings {
		keyValue := strings.Split(gateString, "=")
		switch len(keyValue) {
		case 1:
			(*gateMap)[keyValue[0]] = true
		case 2:
			if keyValue[1] != "true" {
				return fmt.Errorf("invalid gate value; only 'true' is allowed: %s", gateString)
			}
			(*gateMap)[keyValue[0]] = true
		default:
			return fmt.Errorf("invalid gate; expected key or key=true: %s", gateString)
		}
	}
	return nil
}

func enableFromStdinFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&flagPopulateModelFromStdin, "from-stdin", false, "Read the ConfigHub entity's fields, as YAML or JSON, from stdin; merged with command arguments on create, and merged with command arguments and existing entity on update. A field the entity does not have, or one only the server sets, is refused; cub <entity> explain lists the fields")
}

func enableReplaceFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&flagReplace, "replace", false, "Replace entity instead of merging when using --from-stdin or --filename")
}

func enableFilenameFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&flagFilename, "filename", "", "Read the ConfigHub entity's fields, as YAML or JSON, from file, URL (https://), or stdin (-), as --from-stdin does; mutually exclusive with --from-stdin")
}

func enableVerboseFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Detailed output, additive with default output")
}

func enableQuietFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&quiet, "quiet", false, "No default output.")
}

func enableNoheaderFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&noheader, "no-headers", false, "Don't print headers for table output")
	// --no-header (singular) is the legacy name; keep as deprecated alias bound to the same var.
	cmd.Flags().BoolVar(&noheader, "no-header", false, "Deprecated: use --no-headers")
	_ = cmd.Flags().MarkDeprecated("no-header", "use --no-headers")
}

func enableJsonFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "JSON output, suppressing default output")
	_ = cmd.Flags().MarkDeprecated("json", "use -o json")
}

func enableYamlFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&yamlOutput, "yaml", false, "YAML output, suppressing default output")
	_ = cmd.Flags().MarkDeprecated("yaml", "use -o yaml")
}

func enableNamesFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&names, "names", false, "Only output names, suppressing default output")
	_ = cmd.Flags().MarkDeprecated("names", "use -o name")
}

func enableJqFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&jq, "jq", "", "jq expression, suppressing default output")
	_ = cmd.Flags().MarkDeprecated("jq", "use -o jq=<expr>")
}

func enableYqFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&yq, "yq", "", "yq expression, suppressing default output")
	_ = cmd.Flags().MarkDeprecated("yq", "use -o yq=<expr>")
}

func enableOutputFlag(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "",
		"Output format. One of: json, yaml, name, wide, mutations, jq=<expr>, yq=<expr>, custom-columns=<spec>")
}

func enableColumnsFlag(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&columns, "columns", nil,
		"columns to display; can be repeated or comma-separated (e.g., Slug,Labels.Environment)")
}

func enableSelectFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&selectFields, "select", "", "Comma-separated list of fields to retrieve and display. Entity IDs and Slug are always included. Example: \"DisplayName,CreatedAt,Labels\"")
}

func enableWhereFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&where, "where", "", "Filter expression using SQL-inspired syntax. Supports conjunctions with AND. String operators: =, !=, <, >, <=, >=, LIKE, NOT LIKE, ILIKE, ~~, !~~, ~, ~*, !~, !~*. Pattern matching with LIKE/ILIKE uses % and _ wildcards. Regex operators (~, ~*, !~, !~*) support POSIX regular expressions. A related entity is referenced by prefix, as in \"UpstreamUnit.Slug = 'base'\"; when the reference names a list, a * segment matches any element, as in \"FromLink.*.Slug = 'upgrade-app'\". Examples: \"Slug LIKE 'app-%'\", \"DisplayName ILIKE '%backend%'\", \"Slug ~ '^[a-z]+-[0-9]+$'\"")
	enableIncludeHiddenFlag(cmd)
}

func enableFilterFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&filter, "filter", "", "Filter entity to apply to the list. Specify as 'space/filter' for cross-space filters or just 'filter' for current space. Supports both slugs and UUIDs. The filter will be combined with any --where clause using AND logic. Examples: \"production-filters/security-check\", \"my-filter-uuid\", \"validation-rules\"")
}

func enableContainsFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&contains, "contains", "", "Free text search for entities containing the specified text. Searches across string fields (like Slug, DisplayName) and map fields (like Labels, Annotations). Case-insensitive matching. Can be combined with --where using AND logic. Example: \"backend\" to find entities with backend in any searchable field")
}

// enableIncludeHiddenFlag goes with --where: whatever selects entities, to list or to act on,
// leaves out hidden ones unless asked for them.
func enableIncludeHiddenFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&includeHidden, "include-hidden", "", "Also select hidden entities, to list or to act on: those hidden for the given HiddenReasons, comma-separated, as in --include-hidden=BackingUnit, or for any reason when given no value or \"*\". ConfigHub/YAML Units, which hold the configuration of other entities, are hidden with the reason BackingUnit. A --where naming entities by Slug or ID selects them whether hidden or not")
	cmd.Flags().Lookup("include-hidden").NoOptDefVal = "*"
}

// includeHiddenParam is the include_hidden parameter --include-hidden asks for, or nil.
func includeHiddenParam() *string {
	if includeHidden == "" {
		return nil
	}
	return &includeHidden
}

// enableWaitFlagWithDefault allows setting a custom default for the wait flag
func enableWaitFlagWithDefault(cmd *cobra.Command, defaultValue bool) {
	cmd.Flags().BoolVar(&wait, "wait", defaultValue, "wait for completion")
	cmd.Flags().StringVar(&timeout, "timeout", DefaultTimeoutDuration.String(), "completion timeout as a duration with units, such as 10s or 2m")
}

// enableWaitFlag is for trigger commands (default=true)
func enableWaitFlag(cmd *cobra.Command) {
	enableWaitFlagWithDefault(cmd, true)
}

// enableGetWaitFlag is for get commands that may need to wait for resource creation
func enableGetWaitFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&getWait, "wait", false, "wait for resource to be created")
	cmd.Flags().StringVar(&timeout, "timeout", DefaultCreationTimeoutDuration.String(), "creation timeout as a duration with units, such as 10s or 2m")
}

// validateSpaceFlag refuses the "*" space for an operation that has to name one
// concrete space: a single create, which has to put the new entity somewhere.
//
// A single update or delete does not call this. Those resolve their operand
// first and write through whatever space that entity turns out to be in, so a
// wildcard or absent space means "look for it anywhere" rather than an error --
// and a reference matching in more than one space is refused by the resolver,
// which can say how many matched.
func validateSpaceFlag(bulk bool) error {
	if !bulk && selectedSpaceID == "*" {
		return errors.New("creating a single entity needs a space: pass --space <space> (the context's default space is no longer used)")
	}
	return nil
}

func validateStdinFlags() error {
	if flagPopulateModelFromStdin && flagFilename != "" {
		return errors.New("--from-stdin and --filename are mutually exclusive")
	}
	return nil
}

func addStandardDisplayFlags(cmd *cobra.Command) {
	enableQuietFlag(cmd)
	enableVerboseFlag(cmd)
	enableOutputFlag(cmd)
	// Back-compat: legacy boolean/string output flags. Marked deprecated by their enable*Flag funcs.
	enableJsonFlag(cmd)
	enableJqFlag(cmd)
	enableYamlFlag(cmd)
	enableYqFlag(cmd)
}

// addStandardListDisplayFlags registers the display-side flags common to any
// list-shaped command: --names, --no-headers, --columns, and the full set of
// alternative output flags (-o + deprecated aliases). Commands that list local
// entities (like contexts) use this without the server-side filter flags.
func addStandardListDisplayFlags(cmd *cobra.Command) {
	enableNamesFlag(cmd)
	enableNoheaderFlag(cmd)
	enableColumnsFlag(cmd)
	addStandardDisplayFlags(cmd)
}

var (
	listLimit   int
	listOrderBy string
)

// enableListPagingFlags adds --limit and --order-by to a list command. The list helpers read them,
// so a command that also lists entities to look something up has to pass its own options.
func enableListPagingFlags(cmd *cobra.Command) {
	cmd.Flags().IntVar(&listLimit, "limit", 0, "Return at most this many entities; 0 returns all of them. They are read from the server in pages of at most 1000")
	cmd.Flags().StringVar(&listOrderBy, "order-by", "", "Order the entities by these fields, as comma-separated 'ASC:Field', 'DESC:Field' or 'Field' terms, such as \"DESC:CreatedAt\". Entities with equal values are ordered by ID. With --limit, this decides which entities are returned")
}

// listPageOpts returns a list command's --limit and --order-by. newestFirst is the order to read in
// when there is a --limit and no --order-by, for a list displayed newest first, so that the limit
// keeps the newest entities.
func listPageOpts(newestFirst string) cubapi.ListOpts {
	orderBy := listOrderBy
	if orderBy == "" && listLimit > 0 {
		orderBy = newestFirst
	}
	return cubapi.ListOpts{Limit: listLimit, OrderBy: orderBy}
}

func addStandardListFlags(cmd *cobra.Command) {
	enableWhereFlag(cmd)
	enableFilterFlag(cmd)
	enableContainsFlag(cmd)
	enableSelectFlag(cmd)
	addStandardListDisplayFlags(cmd)
}

func addStandardCreateFlags(cmd *cobra.Command) {
	addCreateFlagsWithoutDryRun(cmd)
	enableDryRunFlag(cmd)
	enableDisplayNameFlag(cmd)
	enableHiddenReasonFlag(cmd)
}

// addCreateFlagsWithoutDryRun is addStandardCreateFlags for a create the identity provider holds,
// which the server does not dry-run because a rollback cannot reach it.
func addCreateFlagsWithoutDryRun(cmd *cobra.Command) {
	enableAnnotationFlag(cmd)
	enableLabelFlag(cmd)
	enableDeleteGateFlag(cmd)
	enableAllowExistsFlag(cmd)
	enableFromStdinFlag(cmd)
	enableFilenameFlag(cmd)
	addStandardDisplayFlags(cmd)
}

func addStandardGetFlags(cmd *cobra.Command) {
	enableSelectFlag(cmd)
	addStandardDisplayFlags(cmd)
}

func addStandardUpdateFlags(cmd *cobra.Command) {
	addUpdateFlagsWithoutDisplayName(cmd)
	enableDisplayNameFlag(cmd)
}

// addUpdateFlagsWithoutDisplayName is addStandardUpdateFlags for an entity with no DisplayName.
func addUpdateFlagsWithoutDisplayName(cmd *cobra.Command) {
	enableHiddenReasonFlag(cmd)
	enableAnnotationFlag(cmd)
	enableLabelFlag(cmd)
	enableDeleteGateFlag(cmd)
	enableDryRunFlag(cmd)
	enableFromStdinFlag(cmd)
	enableReplaceFlag(cmd)
	enableFilenameFlag(cmd)
	addStandardDisplayFlags(cmd)
}

func addStandardDeleteFlags(cmd *cobra.Command) {
	addStandardDisplayFlags(cmd)
}

var deleteDetach bool

// addDetachFlag adds --detach to a delete whose entities other entities may reference.
func addDetachFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&deleteDetach, "detach", false,
		"Remove the references other entities have to what is deleted, instead of refusing the delete while any remain")
}

// detachParam is the detach query parameter --detach sets.
func detachParam() *bool {
	if !deleteDetach {
		return nil
	}
	return &deleteDetach
}
