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
var spaceIdentifiers []string
var allowExists bool

func enableAnnotationFlag(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&annotation, "annotation", []string{}, "annotations in key=value format; can separate by commas and/or use multiple instances of the flag")
}

func enableLabelFlag(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&label, "label", []string{}, "labels in key=value format; can separate by commas and/or use multiple instances of the flag")
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

func setDeleteGates(deleteGateMap *map[string]bool) error {
	if deleteGate != nil && len(deleteGate) != 0 {
		if *deleteGateMap == nil {
			*deleteGateMap = map[string]bool{}
		}
		for _, deleteGateString := range deleteGate {
			keyValue := strings.Split(deleteGateString, "=")
			switch len(keyValue) {
			case 1:
				(*deleteGateMap)[keyValue[0]] = true
			case 2:
				// Note: For patch operations, value "-" indicates removal and is handled
				// by BuildPatchData and EnhancePatchData functions. This function only
				// handles non-patch (Put) operations where removal is not supported.
				if keyValue[1] != "true" {
					return fmt.Errorf("invalid delete-gate value; only 'true' is allowed: %s", deleteGateString)
				}
				(*deleteGateMap)[keyValue[0]] = true
			default:
				return fmt.Errorf("invalid delete-gate; expected key or key=true: %s", deleteGateString)
			}
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
	cmd.Flags().BoolVar(&noheader, "no-header", false, "No header for lists")
}

func enableJsonFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "JSON output, suppressing default output")
}

func enableYamlFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&yamlOutput, "yaml", false, "YAML output, suppressing default output")
}

func enableNamesFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&names, "names", false, "Only output names, suppressing default output")
}

func enableJqFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&jq, "jq", "", "jq expression, suppressing default output")
}

func enableYqFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&yq, "yq", "", "yq expression, suppressing default output")
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
	enableJsonFlag(cmd)
	enableJqFlag(cmd)
	enableYamlFlag(cmd)
	enableYqFlag(cmd)
}

func addStandardListFlags(cmd *cobra.Command) {
	enableWhereFlag(cmd)
	enableFilterFlag(cmd)
	enableContainsFlag(cmd)
	enableSelectFlag(cmd)
	enableNamesFlag(cmd)
	enableNoheaderFlag(cmd)
	addStandardDisplayFlags(cmd)
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
