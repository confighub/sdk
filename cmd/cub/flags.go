// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/skratchdot/open-golang/open"
	"github.com/spf13/cobra"
)

var flagPopulateModelFromStdin = false
var flagReplace = false
var flagFilename = ""
var where = ""
var filter = ""
var contains = ""
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
var webFlag = false
var wait = true
var actionWait = false
var getWait = false
var timeout string

const DefaultTimeoutDuration = 10 * time.Minute
const DefaultCreationTimeoutDuration = 30 * time.Second

var annotation []string
var label []string
var deleteGate []string
var option []string
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

func enableOptionFlag(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&option, "option", []string{}, "bridge options in key=value format; use semicolons to separate multiple options within one flag value (e.g., --option 'key1=val1;key2=val2'); each --option flag instance corresponds to a ConfigType by position")
}

// setOptions parses a single --option value (semicolon-separated key=value pairs) into a map.
func setOptions(optionMap *map[string]string) error {
	kvs := splitOptionsBySemicolon(option)
	err := setKeyValues(kvs, optionMap)
	if err != nil {
		return fmt.Errorf("invalid option: %w", err)
	}
	return nil
}

// splitOptionsBySemicolon flattens all option flag values by splitting on semicolons.
func splitOptionsBySemicolon(options []string) []string {
	var result []string
	for _, o := range options {
		parts := strings.Split(o, ";")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				result = append(result, p)
			}
		}
	}
	return result
}

// parseOptionSets parses multiple --option flag values into separate option maps,
// one per ConfigType (by position). Each --option value contains semicolon-separated key=value pairs.
func parseOptionSets(options []string) ([]map[string]string, error) {
	var sets []map[string]string
	for _, o := range options {
		optMap := map[string]string{}
		parts := strings.Split(o, ";")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			keyValue := strings.SplitN(p, "=", 2)
			if len(keyValue) != 2 {
				return nil, fmt.Errorf("expected key=value: %s", p)
			}
			optMap[keyValue[0]] = keyValue[1]
		}
		sets = append(sets, optMap)
	}
	return sets, nil
}

func enableFromStdinFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&flagPopulateModelFromStdin, "from-stdin", false, "Read the ConfigHub entity JSON (e.g., retrieved with cub <entity> get --quiet --json) from stdin; merged with command arguments on create, and merged with command arguments and existing entity on update")
}

func enableReplaceFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&flagReplace, "replace", false, "Replace entity instead of merging when using --from-stdin or --filename")
}

func enableFilenameFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&flagFilename, "filename", "", "Read the ConfigHub entity JSON from file, URL (https://), or stdin (-); mutually exclusive with --from-stdin")
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

func enableWebFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&webFlag, "web", false, "Open in web UI instead of executing")
}

func openWebUI(url string) error {
	if err := open.Start(url); err != nil {
		return fmt.Errorf("failed to open browser: %w", err)
	}
	tprint("Opened in web UI: %s", url)
	return nil
}

func enableWhereFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&where, "where", "", "Filter expression using SQL-inspired syntax. Supports conjunctions with AND. String operators: =, !=, <, >, <=, >=, LIKE, NOT LIKE, ILIKE, ~~, !~~, ~, ~*, !~, !~*. Pattern matching with LIKE/ILIKE uses % and _ wildcards. Regex operators (~, ~*, !~, !~*) support POSIX regular expressions. Examples: \"Slug LIKE 'app-%'\", \"DisplayName ILIKE '%backend%'\", \"Slug ~ '^[a-z]+-[0-9]+$'\"")
}

func enableFilterFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&filter, "filter", "", "Filter entity to apply to the list. Specify as 'space/filter' for cross-space filters or just 'filter' for current space. Supports both slugs and UUIDs. The filter will be combined with any --where clause using AND logic. Examples: \"production-filters/security-check\", \"my-filter-uuid\", \"validation-rules\"")
}

func enableContainsFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&contains, "contains", "", "Free text search for entities containing the specified text. Searches across string fields (like Slug, DisplayName) and map fields (like Labels, Annotations). Case-insensitive matching. Can be combined with --where using AND logic. Example: \"backend\" to find entities with backend in any searchable field")
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

// enableActionWaitFlag is for worker action commands (default=false)
// Uses separate actionWait variable to avoid conflicts with trigger commands
func enableActionWaitFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&actionWait, "wait", false, "wait for completion")
	cmd.Flags().StringVar(&timeout, "timeout", DefaultTimeoutDuration.String(), "completion timeout as a duration with units, such as 10s or 2m")
}

// enableGetWaitFlag is for get commands that may need to wait for resource creation
func enableGetWaitFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&getWait, "wait", false, "wait for resource to be created")
	cmd.Flags().StringVar(&timeout, "timeout", DefaultCreationTimeoutDuration.String(), "creation timeout as a duration with units, such as 10s or 2m")
}

func validateSpaceFlag(bulk bool) error {
	if !bulk && selectedSpaceID == "*" {
		return errors.New("--space must not be '*' when not performing bulk operations")
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

func addStandardListFlags(cmd *cobra.Command) {
	enableWhereFlag(cmd)
	enableFilterFlag(cmd)
	enableContainsFlag(cmd)
	enableSelectFlag(cmd)
	addStandardListDisplayFlags(cmd)
}

func addStandardCreateFlags(cmd *cobra.Command) {
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
	enableAnnotationFlag(cmd)
	enableLabelFlag(cmd)
	enableDeleteGateFlag(cmd)
	enableFromStdinFlag(cmd)
	enableReplaceFlag(cmd)
	enableFilenameFlag(cmd)
	addStandardDisplayFlags(cmd)
}

func addStandardDeleteFlags(cmd *cobra.Command) {
	addStandardDisplayFlags(cmd)
}
