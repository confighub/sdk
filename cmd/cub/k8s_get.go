// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/confighub/sdk/cmd/cub/k8sdescribe"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// k8sGetShow selects how much of each resource to render: the one-line-per-
// resource table, a described summary, or the raw YAML.
var k8sGetShow string

const (
	k8sShowList   = "list"
	k8sShowDetail = "detail"
	k8sShowData   = "data"
)

var k8sGetCmd = &cobra.Command{
	Use:         "get <type>[,<type>...] [<name> ...]",
	Short:       "List and describe Kubernetes resources stored in ConfigHub",
	Long:        getK8sGetHelp(),
	Args:        cobra.MinimumNArgs(1),
	Annotations: map[string]string{"OrgLevel": ""},
	PreRunE:     k8sPreRunE,
	RunE:        k8sGetCmdRun,
}

func getK8sGetHelp() string {
	baseHelp := `List Kubernetes resources held in ConfigHub Units, describe them, or print their YAML.

Resources are named the way kubectl names them: a plural ("deployments"), a singular
("deployment"), a short name ("deploy"), a Kind ("Deployment"), a qualified name
("deployments.apps"), or a full ConfigHub resource type ("apps/v1/Deployment"). Types not
built in still resolve by Kind across any API group, so custom resources work too. Several
types may be given at once, comma-separated.

The pseudo-type "all" means every resource type except CustomResourceDefinition, whose
schemas would swamp everything else. Ask for "crd" explicitly to see those.

This shows configuration, not live cluster state: everything comes from the configuration
data in ConfigHub, along with the Space, Unit, and Target it belongs to.

Three views, selected with --show:

  list    (default) one row per resource
  detail  a described summary of each resource, like "kubectl describe"
  data    the resource's YAML as stored

Examples:
` + "```" + `
  # Deployments in a space
  cub k8s get deploy --space my-space

  # NetworkPolicies across everything applied to a target
  cub k8s get netpol --target prod-use2/prod-use2-oci

  # Two types at once, across all spaces
  cub k8s get deploy,sts --space "*"

  # Describe one resource by name
  cub k8s get deploy my-app --space my-space --show detail

  # Print the YAML of every ConfigMap in a namespace
  cub k8s get cm -n kube-system --space "*" --show data

  # Everything except CRDs in one unit
  cub k8s get all --space my-space --where "Slug = 'my-unit'"

  # Deployments with more than one replica
  cub k8s get deploy --space "*" --where-resource "spec.replicas > 1"

  # Custom resources by Kind
  cub k8s get externalsecrets --space "*"

  # Machine-readable output
  cub k8s get svc --space "*" -o json
  cub k8s get svc --space "*" -o name
` + "```" + `

Filtering combines four independent scopes, all ANDed:

  --space / --target / --where   which Units to read
  <type> / <name> / --namespace  which resources within those Units
  --where-resource               any additional resource-level condition

Units that contain no matching resource produce no output.`

	agentContext := `Reads Kubernetes configuration across the fleet without cloning repos or listing units first.

Cost model: the resource type is applied inside a single function pass over the selected
Units, so narrowing with --space, --target, or --where is what makes a query fast; narrowing
the type alone does not reduce the number of Units read. "--space '*'" over a large
organization takes tens of seconds.

Use --show data when you need to feed the YAML to another tool, --show detail to read
configuration quickly, and -o json when post-processing with jq. -o name prints
"<space>/<unit>/<resource-type>/<namespace>/<name>" (no namespace segment for cluster-scoped
resources), which identifies a resource uniquely.

To change a resource, hand off to "cub function set" or "cub unit update" — this command
is read-only.`

	return getCommandHelp(baseHelp, agentContext)
}

func init() {
	addK8sQueryFlags(k8sGetCmd)
	k8sGetCmd.Flags().StringVar(&k8sGetShow, "show", k8sShowList,
		"how much of each resource to show. One of: list, detail, data")
	k8sCmd.AddCommand(k8sGetCmd)
}

func k8sGetCmdRun(_ *cobra.Command, args []string) error {
	switch k8sGetShow {
	case k8sShowList, k8sShowDetail, k8sShowData:
	default:
		return fmt.Errorf("unknown --show value %q; valid: list, detail, data", k8sGetShow)
	}
	if err := checkK8sOutputFormat(); err != nil {
		return err
	}

	types, err := parseResourceTypes(args[0])
	if err != nil {
		return err
	}
	names := args[1:]

	// The body is needed for anything but the plain table, and for the
	// serialized formats, which include the resource itself.
	withBody := k8sGetShow != k8sShowList || isSerializedOutput()
	resources, err := listK8sResources(types, names, withBody)
	if err != nil {
		return err
	}

	// The Target and the other Unit fields aren't part of a function response,
	// so fetch them only for the views that show them.
	spec := effectiveOutput()
	needUnits := k8sGetShow == k8sShowDetail || spec.Kind == OutputWide || isSerializedOutput()
	var units map[uuid.UUID]*k8sUnit
	if needUnits && len(resources) > 0 {
		units, err = loadK8sUnits()
		if err != nil {
			return err
		}
		for _, resource := range resources {
			if unit := units[resource.UnitID]; unit != nil {
				resource.Target = unit.target
			}
		}
	}

	if renderPayload(resources) {
		return nil
	}
	if spec.Kind == OutputName {
		for _, resource := range resources {
			tprintRaw(k8sResourceName(resource))
		}
		return nil
	}
	// --show data selects the configuration itself as the payload, so --quiet
	// only drops the provenance comments; for the other views it suppresses
	// the output, as it does on the list commands.
	if k8sGetShow == k8sShowData {
		displayK8sResourceData(resources)
		return nil
	}
	if quiet && !verbose {
		return nil
	}
	if k8sGetShow == k8sShowDetail {
		displayK8sResourceDetails(resources, units)
		return nil
	}
	displayK8sResourceList(resources, spec.Kind == OutputWide)
	return nil
}

// k8sResourceName is the -o name form: where the resource lives in ConfigHub,
// then what identifies it within that Unit. The resource type is part of it
// because a Unit may hold several resources of different types under the same
// Kubernetes name.
func k8sResourceName(resource *k8sResource) string {
	return strings.Join([]string{
		resource.Space, resource.Unit, resource.ResourceType, qualifiedName(resource),
	}, "/")
}

// qualifiedName is the Kubernetes name as a person writes it: "namespace/name"
// for namespaced resources, plain "name" for cluster-scoped ones (whose
// ResourceName carries a leading slash).
func qualifiedName(resource *k8sResource) string {
	if resource.Namespace == "" {
		return resource.Name
	}
	return resource.Namespace + "/" + resource.Name
}

// sortedMapKeys renders the keys of a set-shaped map (ApplyGates and the like)
// in a stable order.
func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func displayK8sResourceList(resources []*k8sResource, wide bool) {
	table := tableView()
	headers := []string{"Namespace", "Name", "Kind", "Space", "Unit"}
	if wide {
		headers = append(headers, "API Version", "Target")
	}
	if !noheader {
		table.SetHeader(headers)
	}
	for _, resource := range resources {
		row := []string{resource.Namespace, resource.Name, resource.Kind, resource.Space, resource.Unit}
		if wide {
			row = append(row, resource.APIVersion, resource.Target)
		}
		table.Append(row)
	}
	table.Render()
}

// displayK8sResourceData prints each resource's YAML as a multi-document
// stream, headed by a comment naming where it came from so the output stays
// usable when it covers more than one Unit.
func displayK8sResourceData(resources []*k8sResource) {
	for i, resource := range resources {
		if i > 0 {
			tprintRaw("---")
		}
		if !quiet {
			tprintRaw(fmt.Sprintf("# %s: %s %s",
				prefixedSlug(resource.Space, resource.Unit), resource.ResourceType, qualifiedName(resource)))
		}
		tprintRaw(resource.body)
	}
}

func displayK8sResourceDetails(resources []*k8sResource, units map[uuid.UUID]*k8sUnit) {
	for i, resource := range resources {
		if i > 0 {
			tprintRaw("")
		}
		tprintRaw(fmt.Sprintf("%s %s", resource.ResourceType, qualifiedName(resource)))
		renderSection(unitSection(resource, units[resource.UnitID]))
		for _, section := range k8sdescribe.Describe(resource.Resource) {
			renderSection(section)
		}
	}
}

// unitSection describes where the resource lives in ConfigHub. It stands in for
// the runtime status a "kubectl describe" would show, which ConfigHub does not
// have.
func unitSection(resource *k8sResource, unit *k8sUnit) *k8sdescribe.Section {
	section := &k8sdescribe.Section{Title: "ConfigHub"}
	appendField := func(label, value string) {
		if value != "" {
			field := k8sdescribe.Field{Label: label, Value: value}
			section.Items = append(section.Items, k8sdescribe.Item{Field: &field})
		}
	}
	appendField("Space", resource.Space)
	appendField("Unit", resource.Unit)
	appendField("Target", resource.Target)
	if unit != nil && unit.unit != nil {
		appendField("Revision", fmt.Sprintf("%d", unit.unit.HeadRevisionNum))
		if unit.unit.LastAppliedRevisionNum > 0 {
			appendField("Last Released Revision", fmt.Sprintf("%d", unit.unit.LastAppliedRevisionNum))
		}
		appendField("Unit Labels", labelsToString(unit.unit.Labels))
		appendField("Apply Gates", strings.Join(sortedMapKeys(unit.unit.ApplyGates), ", "))
		appendField("Last Change", unit.unit.LastChangeDescription)
	}
	return section
}

// renderSection prints one described section: a title, then label/value rows,
// tables, and multi-line blocks, all indented under it.
func renderSection(section *k8sdescribe.Section) {
	if section == nil || section.Empty() {
		return
	}
	tprintRaw(strings.ToUpper(section.Title))

	width := 0
	for _, item := range section.Items {
		if item.Field != nil && len(item.Field.Label) > width {
			width = len(item.Field.Label)
		}
	}
	for _, item := range section.Items {
		switch {
		case item.Field != nil:
			tprintRaw(fmt.Sprintf("  %-*s  %s", width, item.Field.Label, item.Field.Value))
		case item.Table != nil:
			tprintRaw(indent(renderDetailTable(item.Table), "  "))
		case item.Block != nil:
			tprintRaw("  " + item.Block.Title + ":")
			tprintRaw(indent(item.Block.Text, "    "))
		case item.Note != "":
			tprintRaw("  " + item.Note)
		}
	}
}

// renderDetailTable renders a described table into a string so it can be
// indented under its section; tableView writes straight to stdout.
func renderDetailTable(t *k8sdescribe.Table) string {
	var buf bytes.Buffer
	table := tableViewTo(&buf)
	if !noheader {
		table.SetHeader(t.Columns)
	}
	for _, row := range t.Rows {
		table.Append(row)
	}
	table.Render()
	return strings.TrimRight(buf.String(), "\n")
}

func indent(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}
