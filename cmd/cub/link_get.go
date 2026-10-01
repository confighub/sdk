// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strconv"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var linkGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a link",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a link in a space including its ID, slug, display name, and the connected units.

What the link reads and writes is shown a row apiece: its stated and resolved bindings, and a
TransformPaths link's upstream paths and getters, downstream paths, and downstream setters, each
setter as its function call. An entry with a Key is labeled by it. The merge pointers read "none" for a link that has not merged. Use -o json for the raw form.

Examples:
`+"```"+`
  # Get details about a deployment-to-namespace link
  cub link get --space my-space deployment-to-namespace
`+"```"+`
`, ""),
	RunE: linkGetCmdRun,
}

func init() {
	addStandardGetFlags(linkGetCmd)
	enableOptionalSpace(linkGetCmd)
	linkCmd.AddCommand(linkGetCmd)
}

func linkGetCmdRun(cmd *cobra.Command, args []string) error {
	linkDetails, err := resolveLink(args[0], selectedSpaceID, selectFields) // use select flag
	if err != nil {
		return err
	}

	displayGetResults(linkDetails, displayExtendedLinkDetails)
	return nil
}

func displayLinkDetails(linkDetails *goclientnew.Link) {
	// Create an ExtendedLink wrapper with just the Link set
	extendedLink := &goclientnew.ExtendedLink{
		Link: linkDetails,
		// All other fields (Space, ToSpace, FromUnit, ToUnit, etc.) will be nil, causing Extended display to show IDs
	}
	displayExtendedLinkDetails(extendedLink)
}

func displayExtendedLinkDetails(extendedLink *goclientnew.ExtendedLink) {
	linkDetails := extendedLink.Link
	view := tableView()
	view.Append([]string{"ID", linkDetails.LinkID.String()})
	view.Append([]string{"Name", linkDetails.Slug})
	if linkDetails.DisplayName != "" {
		view.Append([]string{"Display Name", linkDetails.DisplayName})
	}

	// Show Space slug instead of Space ID when available
	if extendedLink.Space != nil {
		view.Append([]string{"Space", extendedLink.Space.Slug})
	} else {
		view.Append([]string{"Space ID", linkDetails.SpaceID.String()})
	}

	view.Append([]string{"Created At", linkDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", linkDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(linkDetails.Labels)})
	appendBackingUnitRow(view, linkDetails.BackingUnitID)
	view.Append([]string{"Delete Gates", deleteGatesToString(linkDetails.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(linkDetails.Annotations)})
	view.Append([]string{"Organization ID", linkDetails.OrganizationID.String()})

	// Show related units by slug when available
	if extendedLink.FromUnit != nil {
		view.Append([]string{"From Unit", extendedLink.FromUnit.Slug})
	} else {
		view.Append([]string{"From Unit ID", linkDetails.FromUnitID.String()})
	}

	if extendedLink.ToUnit != nil {
		view.Append([]string{"To Unit", extendedLink.ToUnit.Slug})
	} else {
		view.Append([]string{"To Unit ID", linkDetails.ToUnitID.String()})
	}

	// Show To Space slug when available
	if extendedLink.ToSpace != nil {
		view.Append([]string{"To Space", extendedLink.ToSpace.Slug})
	} else {
		view.Append([]string{"To Space ID", linkDetails.ToSpaceID.String()})
	}

	if linkDetails.UpdateType != "" {
		view.Append([]string{"Update Type", linkDetails.UpdateType})
	}
	// AutoUpdate, Stale and Protect are shown for both values. A false is an answer here --
	// whether the Link updates itself, whether it is behind, and whether what it writes is
	// claimed as a local override -- and an omitted row is indistinguishable from one the
	// display does not carry.
	view.Append([]string{"Auto Update", fmt.Sprintf("%t", linkDetails.AutoUpdate)})
	view.Append([]string{"Stale", fmt.Sprintf("%t", linkDetails.Stale)})
	view.Append([]string{"Protect", fmt.Sprintf("%t", linkDetails.Protect)})
	if linkDetails.Squash {
		view.Append([]string{"Squash", "true"})
	}
	if linkDetails.MergeEnableSubtraction {
		view.Append([]string{"Merge Enable Subtraction", "true"})
	}
	if clearance := formatClearance(linkDetails.Clearance); clearance != "" {
		view.Append([]string{"Clearance", clearance})
	}
	if guards := formatGuardStamp(linkDetails.Guards); guards != "" {
		view.Append([]string{"Guards", guards})
	}
	if linkDetails.WhereMutation != "" {
		view.Append([]string{"Where Mutation", linkDetails.WhereMutation})
	}
	if linkDetails.WhereResource != "" {
		view.Append([]string{"Where Resource", linkDetails.WhereResource})
	}
	// The merge pointers are shown when zero too: a Link that has never resolved says so, which
	// is what a reader looking at a Stale Link needs to know.
	view.Append([]string{"Upstream Last Merged Rev", formatMergedRevision(linkDetails.UpstreamLastMergedRevisionNum)})
	view.Append([]string{"Downstream Last Merged Rev", formatMergedRevision(linkDetails.DownstreamLastMergedRevisionNum)})
	if linkDetails.UpstreamSpaceID != nil {
		view.Append([]string{"Upstream Space ID", linkDetails.UpstreamSpaceID.String()})
	}
	if linkDetails.UpstreamLinkID != nil {
		view.Append([]string{"Upstream Link ID", linkDetails.UpstreamLinkID.String()})
	}
	if linkDetails.TransformInvocationID != nil {
		if extendedLink.TransformInvocation != nil {
			view.Append([]string{"Transform Invocation", extendedLink.TransformInvocation.Slug})
		} else {
			view.Append([]string{"Transform Invocation ID", linkDetails.TransformInvocationID.String()})
		}
	}

	appendLinkDefinitionRows(view, linkDetails)

	view.Render()
}

// appendLinkDefinitionRows shows what the Link reads and writes, a row apiece: the bindings of an
// Insert or NeedsProvides Link -- those stated, then those resolution found -- and the paths,
// getters and setters of a TransformPaths Link. An entry with a Key is labeled by it; the others
// are numbered only when there is more than one, so a Link with one of each reads plainly.
// They are the Link's definition, so they are shown by default rather than behind --verbose, and
// -o json gives the raw form.
func appendLinkDefinitionRows(view *tablewriter.Table, link *goclientnew.Link) {
	if link.ManualBindings != nil {
		bindings := *link.ManualBindings
		for i := range bindings {
			view.Append([]string{keyedLabel("Manual Binding", bindings[i].Key, i, len(bindings)), formatBinding(&bindings[i])})
		}
	}
	if link.Bindings != nil {
		bindings := *link.Bindings
		for i := range bindings {
			view.Append([]string{numberedLabel("Binding", i, len(bindings)), formatBinding(&bindings[i])})
		}
	}
	for i := range link.UpstreamPaths {
		up := &link.UpstreamPaths[i]
		view.Append([]string{"Upstream Path " + up.Name, formatPathIn(up.Path, up.Resource)})
	}
	for i := range link.UpstreamGetters {
		getter := &link.UpstreamGetters[i]
		view.Append([]string{"Upstream Getter " + getter.Name, formatFunctionInvocationLine(getter.FunctionInvocation)})
	}
	for i := range link.DownstreamPaths {
		down := &link.DownstreamPaths[i]
		view.Append([]string{keyedLabel("Downstream Path", down.Key, i, len(link.DownstreamPaths)), formatPathExpression(down)})
	}
	for i := range link.DownstreamSetters {
		setter := &link.DownstreamSetters[i]
		line := formatFunctionInvocationLine(setter.FunctionInvocation)
		if len(setter.Parameters) > 0 {
			line += fmt.Sprintf(" [uses %s]", strings.Join(setter.Parameters, ", "))
		}
		view.Append([]string{keyedLabel("Downstream Setter", setter.Key, i, len(link.DownstreamSetters)), line})
	}
}

// keyedLabel is label and the entry's Key, for an entry that has one, and otherwise its
// numberedLabel.
func keyedLabel(label, key string, index, count int) string {
	if key != "" {
		return label + " " + key
	}
	return numberedLabel(label, index, count)
}

// numberedLabel is label alone for the only item of its kind, and label and a 1-based number
// otherwise.
func numberedLabel(label string, index, count int) string {
	if count == 1 {
		return label
	}
	return fmt.Sprintf("%s %d", label, index+1)
}

// formatMergedRevision is a merge pointer, or "none" for a Link that has not merged.
func formatMergedRevision(revisionNum int64) string {
	if revisionNum == 0 {
		return "none"
	}
	return strconv.FormatInt(revisionNum, 10)
}

// formatResourceInfo names a resource as its type and name.
func formatResourceInfo(resource *goclientnew.ResourceInfo) string {
	if resource == nil {
		return ""
	}
	if resource.ResourceName == "" {
		return resource.ResourceType
	}
	return resource.ResourceType + " " + resource.ResourceName
}

// formatPathIn is a path, and the resource it is in when there is one.
func formatPathIn(path string, resource *goclientnew.ResourceInfo) string {
	if in := formatResourceInfo(resource); in != "" {
		return path + " in " + in
	}
	return path
}

// formatPathExpression is where a TransformPaths Link writes and what: the path, then the
// expression with its evaluator and the type it is converted to.
func formatPathExpression(down *goclientnew.PathExpression) string {
	line := fmt.Sprintf("%s = %q", formatPathIn(down.Path, down.Resource), down.Expression)
	var qualifiers []string
	if down.Evaluator != "" {
		qualifiers = append(qualifiers, down.Evaluator)
	}
	if down.DataType != "" {
		qualifiers = append(qualifiers, down.DataType)
	}
	if len(qualifiers) > 0 {
		line += " (" + strings.Join(qualifiers, ", ") + ")"
	}
	if len(down.Parameters) > 0 {
		line += fmt.Sprintf(" [uses %s]", strings.Join(down.Parameters, ", "))
	}
	return line
}

// formatBinding is what a Binding connects: the path it writes in the downstream resource, and,
// for a NeedsProvides Link, the path it reads in the upstream one. An Insert Link's Binding has
// no provided side: it inserts the upstream Unit's data whole.
func formatBinding(binding *goclientnew.Binding) string {
	line := formatPathIn(binding.NeededPath, binding.NeededResource)
	if binding.ProvidedPath != "" || binding.ProvidedResource != nil {
		line += " <- " + formatPathIn(binding.ProvidedPath, binding.ProvidedResource)
	}
	var qualifiers []string
	if binding.AttributeName != "" {
		qualifiers = append(qualifiers, binding.AttributeName)
	}
	if binding.DataType != "" {
		qualifiers = append(qualifiers, binding.DataType)
	}
	if len(qualifiers) > 0 {
		line += " (" + strings.Join(qualifiers, ", ") + ")"
	}
	return line
}

// formatFunctionInvocationLine renders a function call on one line, as the function name and its
// arguments: name=value for a named argument, the value alone for a positional one, and the
// evaluator after an argument that is a template or expression rather than a literal.
func formatFunctionInvocationLine(invocation *goclientnew.FunctionInvocation) string {
	if invocation == nil {
		return ""
	}
	parts := []string{invocation.FunctionName}
	for _, argument := range invocation.Arguments {
		value := formatFunctionArgumentValue(argument.Value)
		if argument.ParameterName != nil && *argument.ParameterName != "" {
			value = *argument.ParameterName + "=" + value
		}
		if argument.Evaluator != nil && *argument.Evaluator != "" {
			value += " (" + *argument.Evaluator + ")"
		}
		parts = append(parts, value)
	}
	line := strings.Join(parts, " ")
	if invocation.WhereResource != "" {
		line += fmt.Sprintf(" where %q", invocation.WhereResource)
	}
	return line
}
