// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

// Blame answers "who set this field, when, and why" for every field of a Unit,
// which the pieces on their own do not.
//
// MutationSources says which Mutation last wrote each path; a Mutation says what
// operation it was; a Revision says who ran it, when, and what they called it. The
// join across the three is what a reader wants and what none of them is.
//
// Two things the join alone still gets wrong, both handled here:
//
//   - MutationSources only holds the paths something has written *since the
//     resource arrived*. Everything the resource arrived with -- most of a chart's
//     output -- is covered by one resource-level entry and has no path of its own.
//     So the field list comes from the data, flattened by running compute-mutations
//     against an empty document, and a path with no entry of its own falls back to
//     the resource-level entry.
//
//   - A value that arrived by clone or merge is credited to the merge, which names
//     the upstream Unit and not what set the value there. Following MergeSourceID
//     into that Unit at the Revision the merge took, and repeating, is what turns
//     "merge from apptique-base" into "the chart", "the base", or "this variant".
//     A promotion copies the upstream Revision's description down, so the walk often
//     terminates immediately; a clone does not, so it usually does not.
//
// Nothing here is Kubernetes-specific. A Unit is Kubernetes/YAML, AppConfig/JSON,
// TOML, INI, Properties, Env, or ConfigHub/YAML, and its data is in that format --
// so the toolchain decides both which executor reads the data and which resource
// provider names an array's merge keys. Only the *values* MutationSources records
// are uniformly YAML, whatever the Unit's format, which is what lets one expansion
// serve every toolchain.

var unitBlameArgs struct {
	path         string
	resource     string
	noUpstream   bool
	upstreamMax  int
	showInternal bool
}

// blameMaxUpstreamDepth bounds the walk. Variant chains are shallow -- base, prod
// base, deployment -- so a walk longer than this means a cycle or a surprise, and
// stopping is better than reading forever.
const blameMaxUpstreamDepth = 8

var unitBlameCmd = &cobra.Command{
	Use:   "blame <unit>",
	Short: "Show what set each field of a unit, when, and why",
	Long: getCommandHelp(`Show, for every field of a unit's configuration, what set the value that is
there: the operation, the revision, who ran it, and the change description they gave it.

Where 'cub unit get -o mutations' shows the mutations grouped by resource and path, this
shows the fields, one per line, with the history joined onto each. It is the answer to
"who set this, and why" for a value you are looking at.

A value that arrived from upstream is followed to where it was actually set. A field a
variant took from its base reads as the base's change, and a field the base took from a
rendered chart reads as that chart -- so "did the chart set this, or did we?" is one
command rather than three. Pass --no-upstream to report only this unit's own record,
which names the upstream unit without reading it.

Columns:
  PATH      the field, in the same path syntax functions and links use
  VALUE     what is there now
  SET BY    the function, external source, link, or trigger that wrote it
  WHERE     the space whose change it was, after following upstream
  REV       that space's revision number
  WHEN      how long ago

A field marked "*" is a protected local override: this unit claims it, and a merge from
upstream leaves it alone.

Every toolchain is read: a Kubernetes manifest, and equally an AppConfig JSON, YAML,
TOML, INI, Properties, or Env unit, or a ConfigHub one. The unit's data is read in its
own format, and array elements are addressed the way that format addresses them -- by
merge key where it has them ("?name=server" for a Kubernetes container), by position
where it does not.

Use --path to ask about one field, --resource to restrict to one resource, and --verbose
to see each change description and the full upstream chain rather than its end. -o json
emits one object per field.

Examples:
`+"```"+`
  # Every field of a unit, and what set it.
  cub unit blame --space apptique-dev deployment-frontend

  # One field: did the chart set this, our base, or this variant?
  cub unit blame --space apptique-dev deployment-frontend --path spec.replicas

  # Every container image in the unit, with the change descriptions.
  cub unit blame --space apptique-dev deployment-frontend --path image --verbose

  # This unit's own record, without reading upstream.
  cub unit blame --space apptique-dev deployment-frontend --no-upstream
`+"```"+`
`, ""),
	Args: cobra.MaximumNArgs(1),
	RunE: runUnitBlame,
}

func init() {
	addStandardGetFlags(unitBlameCmd)
	unitBlameCmd.Flags().StringVar(&unitBlameArgs.path, "path", "",
		"only fields whose path contains this substring")
	unitBlameCmd.Flags().StringVar(&unitBlameArgs.resource, "resource", "",
		"only fields of this resource, as <type>/<name> or <name>")
	unitBlameCmd.Flags().BoolVar(&unitBlameArgs.noUpstream, "no-upstream", false,
		"report this unit's own record without following a merge to where the value was set")
	unitBlameCmd.Flags().IntVar(&unitBlameArgs.upstreamMax, "upstream-depth", blameMaxUpstreamDepth,
		"how many upstream units to follow before giving up")
	unitBlameCmd.Flags().BoolVar(&unitBlameArgs.showInternal, "show-comments", false,
		"include the comment-carrying pseudo-fields ($comment$...) that hold YAML comments")
	unitCmd.AddCommand(unitBlameCmd)
}

// blameOrigin is one hop of a field's history: a change, in the unit and space it
// was made in.
type blameOrigin struct {
	SpaceSlug   string    `json:",omitempty"`
	UnitSlug    string    `json:",omitempty"`
	RevisionNum int64     `json:",omitempty"`
	MutationNum int64     `json:",omitempty"`
	SetBy       string    `json:",omitempty"`
	Description string    `json:",omitempty"`
	User        string    `json:",omitempty"`
	When        time.Time `json:",omitempty"`
	// upstreamUnit is set when this hop credits a merge: the unit the walk continues
	// into, as the Mutation's expansion already resolved it. Nil at the hop that
	// actually set the value, and at one whose upstream is no longer readable.
	upstreamUnit        *goclientnew.Unit
	upstreamRevisionNum int64
}

// blameField is one field and everything known about how it got its value. Chain
// runs from this unit outwards; Origin is its last hop, which is the answer.
type blameField struct {
	ResourceType string
	ResourceName string
	// Path is the authored form -- what someone would pass to a function or a link.
	Path  string
	Value string
	// storedPath is the same field as MutationSources keys it, anchored segments and
	// all. Only the lookups use it: an anchor is positional bookkeeping, and printing
	// one would hand the reader a path that is not the one they should type.
	storedPath string
	// resourceNameCore is the resource's name without its scope, which is how the
	// upstream walk matches a resource a variant has renamed.
	resourceNameCore string
	Protected        bool
	Chain            []*blameOrigin `json:",omitempty"`
}

// Origin is the hop that actually set the value: the end of the chain.
func (f *blameField) Origin() *blameOrigin {
	if len(f.Chain) == 0 {
		return nil
	}
	return f.Chain[len(f.Chain)-1]
}

func runUnitBlame(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return errors.New("name a unit to blame")
	}
	unit, err := apiGetUnitFromSlugInSpace(args[0], selectedSpaceID, "*")
	if err != nil {
		return err
	}

	fields, err := blameUnit(unit, unitBlameArgs.upstreamMax)
	if err != nil {
		return err
	}
	fields = filterBlameFields(fields, unitBlameArgs.path, unitBlameArgs.resource, unitBlameArgs.showInternal)

	if renderPayload(fields) {
		return nil
	}
	displayBlame(fields)
	return nil
}

// blameUnit builds the field list for one unit: flatten the data, credit each field
// to the mutation that wrote it, then follow each merge to where the value was set.
func blameUnit(unit *goclientnew.Unit, upstreamMax int) ([]*blameField, error) {
	mutationSources, err := fetchUnitMutationSources(unit.SpaceID, unit.UnitID)
	if err != nil {
		return nil, err
	}
	if mutationSources == nil || len(*mutationSources) == 0 {
		return nil, nil
	}
	data, err := fetchUnitData(unit.SpaceID, unit.UnitID)
	if err != nil {
		return nil, err
	}

	flattened, err := flattenUnitFields(data, unit.ToolchainType)
	if err != nil {
		return nil, err
	}

	resolver := newBlameResolver(upstreamMax)
	fields := make([]*blameField, 0, len(flattened))
	for _, f := range flattened {
		info, protected := creditPath(*mutationSources, f.ResourceType, f.ResourceName, f.resourceNameCore, f.storedPath)
		f.Protected = protected
		if info != nil {
			chain, err := resolver.walk(unit, *info, f.ResourceType, f.ResourceName, f.resourceNameCore, f.storedPath, 0)
			if err != nil {
				return nil, err
			}
			f.Chain = chain
		}
		fields = append(fields, f)
	}
	return fields, nil
}

// flattenUnitFields turns the unit's configuration into one entry per field, with
// its current value.
//
// The flattening is compute-mutations run locally against a skeleton of each
// resource's identity: identical identity means the resources match, so the diff is
// per-path rather than one whole-resource add, and every remaining field comes back
// as an Add. Doing it this way rather than walking the YAML here is what keeps the
// paths identical to the ones MutationSources, functions, and links use -- including
// the associative segments ("?name=server") that a naive walk would render as
// indices.
func flattenUnitFields(data, toolchainType string) ([]*blameField, error) {
	if toolchainType == "" {
		return nil, errors.New("unit has no toolchain type, so its data cannot be read")
	}
	provider, err := blameResourceProvider(toolchainType)
	if err != nil {
		return nil, err
	}

	// Empty "previous" data: nothing pairs with it, so every resource comes back as
	// one whole-resource Add whose value is the resource as YAML -- the same shape
	// for a Deployment and for a JSON settings file -- and is expanded to leaves
	// below. Two reasons it is empty rather than a skeleton of each resource's
	// identity, which would make compute-mutations report the leaves directly:
	//
	//   - a skeleton only exists where the resource has an identity to build one
	//     from, and an AppConfig resource is "NoSchema"/"NoName";
	//   - a skeleton has to be written in the Unit's own format, and "{}" is a
	//     document in YAML and JSON but a syntax error in TOML and Properties.
	//
	// The empty string is the one empty document every toolchain reads.
	//
	// The flattening is how blame reads the fields, not something the reader asked to
	// watch, and the shared function handler logs every invocation at Info. Quiet it
	// for the duration rather than making the library quieter for everyone.
	restore := quietSlog()
	resp, err := invokeLocalFunction(data, "compute-mutations", []string{"", "0"}, toolchainType)
	restore()
	if err != nil {
		return nil, errors.Wrap(err, "failed to flatten the unit's fields")
	}
	if resp == nil || !resp.Success {
		message := "compute-mutations reported no result"
		if resp != nil && len(resp.ErrorMessages) > 0 {
			message = strings.Join(resp.ErrorMessages, "; ")
		}
		return nil, errors.New(message)
	}

	raw, ok := resp.Outputs[api.OutputTypeResourceMutationList]
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	var mutations api.ResourceMutationList
	if err := json.Unmarshal(raw, &mutations); err != nil {
		return nil, errors.Wrap(err, "failed to read the flattened fields")
	}

	fields := make([]*blameField, 0)
	for i := range mutations {
		rm := &mutations[i]
		resourceType := string(rm.Resource.ResourceType)
		leaves := expandBlameValue(provider, resourceType, "", rm.ResourceMutationInfo.Value)

		// A path entry, where one exists, is the newer word on that field than the
		// whole-resource value: last writer wins, as elsewhere.
		for path, info := range rm.PathMutationMap {
			leaves = overlayBlameLeaves(leaves,
				expandBlameValue(provider, resourceType, string(path), info.Value))
		}

		sort.Slice(leaves, func(a, b int) bool { return leaves[a].Path < leaves[b].Path })
		for _, leaf := range leaves {
			fields = append(fields, &blameField{
				ResourceType:     resourceType,
				ResourceName:     string(rm.Resource.ResourceName),
				resourceNameCore: string(rm.Resource.ResourceNameWithoutScope),
				Path:             authoredPath(leaf.Path),
				storedPath:       leaf.Path,
				Value:            leaf.Value,
			})
		}
	}
	return fields, nil
}

// overlayBlameLeaves replaces the leaves a newer value covers, keyed by authored
// path so the two sides agree whether an associative segment carries its anchor.
func overlayBlameLeaves(base, newer []blameLeaf) []blameLeaf {
	if len(newer) == 0 {
		return base
	}
	replaced := make(map[string]struct{}, len(newer))
	for _, leaf := range newer {
		replaced[authoredPath(leaf.Path)] = struct{}{}
	}
	merged := make([]blameLeaf, 0, len(base)+len(newer))
	for _, leaf := range base {
		if _, ok := replaced[authoredPath(leaf.Path)]; !ok {
			merged = append(merged, leaf)
		}
	}
	return append(merged, newer...)
}

// quietSlog raises the default log level to Error and returns a function putting it
// back.
func quietSlog() func() {
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	return func() { slog.SetDefault(previous) }
}

// creditPath finds the MutationSources entry that wrote a path: its own, or the
// resource-level entry covering everything the resource arrived with.
func creditPath(mutationSources goclientnew.ResourceMutationList, resourceType, resourceName, resourceNameCore, path string) (*goclientnew.MutationInfo, bool) {
	rm := matchBlameResource(mutationSources, resourceType, resourceName, resourceNameCore)
	if rm != nil {
		if info := lookupPathMutation(rm.PathMutationMap, path); info != nil {
			return info, info.Protected
		}
		if rm.ResourceMutationInfo != nil && rm.ResourceMutationInfo.MutationType != nil &&
			*rm.ResourceMutationInfo.MutationType != goclientnew.None {
			return rm.ResourceMutationInfo, rm.ResourceMutationInfo.Protected
		}
	}
	return nil, false
}

// lookupPathMutation finds a path's entry, by the key as given and then by authored
// form.
//
// Whether an associative segment carries its ";@N" anchor depends on what wrote the
// entry, and the two forms address the same field. Comparing the authored form of
// each is what lets a field flattened here find the mutation that set it -- without
// it every path falls through to its resource-level entry, and every value in a
// variant reads as "arrived with the clone".
func lookupPathMutation(pathMutationMap *goclientnew.MutationMap, path string) *goclientnew.MutationInfo {
	if pathMutationMap == nil {
		return nil
	}
	if info, ok := (*pathMutationMap)[path]; ok &&
		info.MutationType != nil && *info.MutationType != goclientnew.None {
		return &info
	}
	wanted := authoredPath(path)
	for key, info := range *pathMutationMap {
		if info.MutationType == nil || *info.MutationType == goclientnew.None {
			continue
		}
		if authoredPath(key) == wanted {
			return &info
		}
	}
	return nil
}

// matchBlameResource finds the entry for a resource, by name and then by name
// without its scope.
//
// The second try is what makes the upstream walk work at all: a variant fills in the
// namespace its base left as a placeholder, so the same resource is
// "apptique/frontend" here and "confighubplaceholder/frontend" upstream. Matching on
// the scoped name alone would stop every walk at the clone.
func matchBlameResource(mutationSources goclientnew.ResourceMutationList, resourceType, resourceName, resourceNameCore string) *goclientnew.ResourceMutation {
	var unscoped *goclientnew.ResourceMutation
	for i := range mutationSources {
		rm := &mutationSources[i]
		if rm.Resource == nil || rm.Resource.ResourceType != resourceType {
			continue
		}
		if rm.Resource.ResourceName == resourceName {
			return rm
		}
		if unscoped == nil && resourceNameCore != "" &&
			rm.Resource.ResourceNameWithoutScope == resourceNameCore {
			unscoped = rm
		}
	}
	return unscoped
}

// authoredPath renders a stored path the way someone would type it: an associative
// segment keeps its selector and drops the ";@N" anchor, which is positional
// bookkeeping the merge engine keeps and no command accepts.
func authoredPath(path string) string {
	if !strings.Contains(path, ";@") {
		return path
	}
	segments := strings.Split(path, ".")
	for i, segment := range segments {
		if !strings.HasPrefix(segment, "?") {
			continue
		}
		if selector, _, found := strings.Cut(segment, ";@"); found {
			segments[i] = selector
		}
	}
	return strings.Join(segments, ".")
}

// filterBlameFields applies --path, --resource, and the comment-field rule.
func filterBlameFields(fields []*blameField, pathFilter, resourceFilter string, showInternal bool) []*blameField {
	kept := make([]*blameField, 0, len(fields))
	for _, f := range fields {
		// The "$comment$..." pseudo-fields carry YAML comments rather than
		// configuration. They are real mutations, but nobody reading a blame table
		// is asking who wrote a comment.
		if !showInternal && strings.HasPrefix(f.Path, "$comment$") {
			continue
		}
		if pathFilter != "" && !strings.Contains(f.Path, pathFilter) {
			continue
		}
		if resourceFilter != "" &&
			!strings.Contains(f.ResourceType+"/"+f.ResourceName, resourceFilter) &&
			!strings.Contains(f.ResourceName, resourceFilter) {
			continue
		}
		kept = append(kept, f)
	}
	return kept
}
