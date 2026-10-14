// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package blame answers "who set this field, when, and why" for every field of a Unit,
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
//
// A Resolver reads what it needs through the API, and caches it: a unit has far fewer
// Revisions than fields, and a variant's units all merge from the same base. Unit and
// Variant build the field list; FindField and Path describe one field in full.
package blame

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/core/workerapi"
)

// MaxUpstreamDepth bounds the walk. Variant chains are shallow -- base, prod
// base, deployment -- so a walk longer than this means a cycle or a surprise, and
// stopping is better than reading forever.
const MaxUpstreamDepth = 8

// Origin is one hop of a field's history: a change, in the unit and space it
// was made in.
type Origin struct {
	SpaceSlug   string `json:",omitempty"`
	UnitSlug    string `json:",omitempty"`
	RevisionNum int64  `json:",omitempty"`
	MutationNum int64  `json:",omitempty"`
	// SetBy is the one-word answer; the fields after it are what it was chosen from, so a
	// script can ask for the Link or the function without parsing it back out.
	SetBy      string `json:",omitempty"`
	Function   string `json:",omitempty"`
	Link       string `json:",omitempty"`
	Trigger    string `json:",omitempty"`
	Invocation string `json:",omitempty"`
	// Source is the operation that made the Revision, such as UpdateUnit or TransformPaths.
	Source      string    `json:",omitempty"`
	Description string    `json:",omitempty"`
	User        string    `json:",omitempty"`
	When        time.Time `json:",omitempty"`
	// What the Revision was part of: the ChangeOrders that made it, the Releases that
	// bundled it, and the Tags that mark it. Releases have no slug and are named by number.
	ChangeOrders []string `json:",omitempty"`
	Releases     []string `json:",omitempty"`
	Tags         []string `json:",omitempty"`
	// linkUpdateType is the Link's UpdateType, which tells an upgrade from other Links.
	linkUpdateType string
	// The records the hop was read from, for 'cub unit path', which prints all of them.
	mutation *goclientnew.ExtendedMutation
	revision *goclientnew.ExtendedRevision
	// upstreamUnit is set when this hop took the value from another unit, by a merge or by
	// a Link's resolve: the unit the walk continues into, at upstreamRevisionNum. Nil at the
	// hop that actually set the value, and at one whose upstream is no longer readable.
	upstreamUnit        *goclientnew.Unit
	upstreamRevisionNum int64
	// LinkUpstream and LinkUpstreamRevisionNum are the unit and Revision a Link's resolve read,
	// for 'cub unit path', whether or not the walk can follow it there. The Revision is the
	// one the Mutation records, or for one written before resolves recorded it, the one
	// legacyResolveRevision finds.
	LinkUpstream            string `json:",omitempty"`
	LinkUpstreamRevisionNum int64  `json:",omitempty"`
	// upstreamTarget is the field to look up there, when it is not this one: a Link's
	// resolve reads its own resource and path upstream, where a merge reads the same field.
	upstreamTarget *lookupTarget
}

// lookupTarget is a field as the walk looks it up in a unit's record.
type lookupTarget struct {
	resourceType     string
	resourceName     string
	resourceNameCore string
	path             string
}

// Field is one field and everything known about how it got its value. Chain
// runs from this unit outwards; Origin is its last hop, which is the answer.
type Field struct {
	SpaceSlug string `json:",omitempty"`
	UnitSlug  string `json:",omitempty"`
	// Ref is the field's short reference, which 'cub unit path' takes in place of the
	// resource and path.
	Ref          string
	ResourceType string
	ResourceName string
	// Path is the authored form -- what someone would pass to a function or a link.
	Path  string
	Value string
	// Flags is the FLAGS column: one letter per fact, in FlagLegend order.
	Flags string `json:",omitempty"`
	// storedPath is the same field as MutationSources keys it, anchored segments and
	// all. Only the lookups use it: an anchor is positional bookkeeping, and printing
	// one would hand the reader a path that is not the one they should type.
	storedPath string
	// resourceNameCore is the resource's name without its scope, which is how the
	// upstream walk matches a resource a variant has renamed.
	resourceNameCore string
	Protected        bool
	// Guards are the reasons recorded for this field: its own, and every annotated ancestor's,
	// which is what an operation writing here has to be cleared for.
	Guards map[string]string `json:",omitempty"`
	// Conflicts are the upstream changes to this field that a merge or resolve dropped, and
	// Findings what a Link's resolve could not do here. Both are the Unit's current record.
	Conflicts []goclientnew.MutationConflict `json:",omitempty"`
	Findings  []goclientnew.LinkFinding      `json:",omitempty"`
	// Validation are the unit's failed validating checks that name this field.
	Validation []Validation `json:",omitempty"`
	// Needed and Provided are the unit's NeededPaths and ProvidedPaths that are this field:
	// a value it takes from an upstream unit through a NeedsProvides Link, and one it offers
	// to downstream units.
	Needed   []NeededPath                `json:",omitempty"`
	Provided []goclientnew.AttributeInfo `json:",omitempty"`
	Chain    []*Origin                   `json:",omitempty"`
}

// Origin is the hop that actually set the value: the end of the chain.
func (f *Field) Origin() *Origin {
	if len(f.Chain) == 0 {
		return nil
	}
	return f.Chain[len(f.Chain)-1]
}

// Unit builds the field list for one unit, reading its record and its data. The unit
// has to carry what a full read returns: its toolchain, PathAnnotations, Conflicts,
// LinkFindings, ValidationResults, NeededPaths, ProvidedPaths, HeadRevisionNum and
// LastReleasedRevisionNum.
func (r *Resolver) Unit(ctx context.Context, unit *goclientnew.Unit) ([]*Field, error) {
	mutationSources, err := r.unitMutationSources(ctx, unit)
	if err != nil {
		return nil, err
	}
	if len(mutationSources) == 0 {
		return nil, nil
	}
	data, err := cubapi.UnitData(ctx, r.client, unit.SpaceID, unit.UnitID)
	if err != nil {
		return nil, err
	}
	return r.UnitData(ctx, unit, mutationSources, data)
}

// UnitData builds the field list for one unit from its record and data: flatten the
// data, credit each field to the mutation that wrote it, then follow each merge to where the
// value was set. The resolver's caches are shared by every unit it is handed, which is what
// lets a variant's units, all merging from the same base, read that base once.
func (r *Resolver) UnitData(ctx context.Context, unit *goclientnew.Unit, mutationSources goclientnew.ResourceMutationList,
	data string) ([]*Field, error) {
	if len(mutationSources) == 0 {
		return nil, nil
	}
	flattened, err := flattenUnitFields(data, unit.ToolchainType)
	if err != nil {
		return nil, err
	}
	space := r.space(ctx, unit.SpaceID)
	spaceSlug, releasable := space.slug, space.releasable

	// Resources are matched in their records across the API versions of their types. Without a
	// provider for the toolchain they are matched by their exact types.
	identifier, _ := blameResourceProvider(unit.ToolchainType)
	// Before any filter, so a field's REF is the same whatever else was asked for.
	assignBlameRefs(identifier, flattened)
	fields := make([]*Field, 0, len(flattened))
	// The annotation table travels on the Unit, so no second fetch. A Unit that has never been
	// guarded has none, which is the common case and costs a nil check per field.
	var annotations goclientnew.PathAnnotationList
	if unit.PathAnnotations != nil {
		annotations = *unit.PathAnnotations
	}

	for _, f := range flattened {
		f.SpaceSlug = spaceSlug
		f.UnitSlug = unit.Slug
		info, protected := creditPath(identifier, mutationSources, f.ResourceType, f.ResourceName, f.resourceNameCore, f.storedPath)
		f.Protected = protected
		// Keyed by the authored path, which is the form the annotation table stores: its
		// paths are canonical, so they carry no anchor to strip.
		f.Guards = blameGuardsForPath(identifier, annotations, f.ResourceType, f.ResourceName, f.resourceNameCore, f.Path)
		f.Conflicts = blameConflictsForPath(identifier, unit.Conflicts, f.ResourceType, f.ResourceName, f.resourceNameCore, f.Path)
		f.Findings = blameFindingsForPath(identifier, unit.LinkFindings, f.ResourceType, f.ResourceName, f.resourceNameCore, f.Path)
		f.Validation = blameValidationsForPath(identifier, unit, f.ResourceType, f.ResourceName, f.resourceNameCore, f.Path, f.storedPath)
		f.Needed, f.Provided = blameAttributesForPath(identifier, unit, f.ResourceType, f.ResourceName, f.resourceNameCore, f.Path)
		if info != nil {
			chain, err := r.walk(ctx, unit, *info, f.ResourceType, f.ResourceName, f.resourceNameCore, f.storedPath,
				unit.HeadRevisionNum, 0)
			if err != nil {
				return nil, err
			}
			f.Chain = chain
		}
		f.Flags = blameFlags(f, unit.LastReleasedRevisionNum, releasable)
		fields = append(fields, f)
	}
	r.nameBoundLinks(ctx, fields)
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
func flattenUnitFields(data, toolchainType string) ([]*Field, error) {
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
	resp, err := localExecutor().Invoke(context.Background(), &api.FunctionInvocationRequest{
		FunctionContext: api.FunctionContext{ToolchainType: workerapi.ToolchainType(toolchainType)},
		ConfigData:      data,
		FunctionInvocations: []api.FunctionInvocation{{
			FunctionName: "compute-mutations",
			Arguments:    []api.FunctionArgument{{Value: ""}, {Value: 0}},
		}},
	})
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

	fields := make([]*Field, 0)
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
			fields = append(fields, &Field{
				ResourceType:     resourceType,
				ResourceName:     string(rm.Resource.ResourceName),
				resourceNameCore: string(rm.Resource.ResourceNameWithoutScope),
				Path:             AuthoredPath(leaf.Path),
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
		replaced[AuthoredPath(leaf.Path)] = struct{}{}
	}
	merged := make([]blameLeaf, 0, len(base)+len(newer))
	for _, leaf := range base {
		if _, ok := replaced[AuthoredPath(leaf.Path)]; !ok {
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

// creditPath finds the MutationSources entry that wrote a path: its own, the closest
// ancestor's, or the resource-level entry covering everything the resource arrived with.
//
// The ancestor is what credits a value that arrived inside a larger one. A change that
// adds an array element or a map records one entry for the whole of it, not one per leaf,
// so a leaf of an env var added later has no entry of its own; without the ancestor it
// falls through to the resource and reads as having arrived when the Unit was created.
func creditPath(identifier api.ResourceIdentifier, mutationSources goclientnew.ResourceMutationList, resourceType, resourceName, resourceNameCore, path string) (*goclientnew.MutationInfo, bool) {
	rm := matchBlameResource(identifier, mutationSources, resourceType, resourceName, resourceNameCore)
	if rm != nil {
		if info := lookupPathMutation(rm.PathMutationMap, path); info != nil {
			return info, info.Protected
		}
		if info := lookupAncestorMutation(rm.PathMutationMap, path); info != nil {
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
	wanted := AuthoredPath(path)
	for key, info := range *pathMutationMap {
		if info.MutationType == nil || *info.MutationType == goclientnew.None {
			continue
		}
		if AuthoredPath(key) == wanted {
			return &info
		}
	}
	return nil
}

// lookupAncestorMutation finds the entry of the closest ancestor of a path that wrote a
// value there. A Delete is passed over: a value present now was not written by removing
// what held it.
func lookupAncestorMutation(pathMutationMap *goclientnew.MutationMap, path string) *goclientnew.MutationInfo {
	segments := strings.Split(path, ".")
	for n := len(segments) - 1; n > 0; n-- {
		info := lookupPathMutation(pathMutationMap, strings.Join(segments[:n], "."))
		if info != nil && *info.MutationType != goclientnew.Delete {
			return info
		}
	}
	return nil
}

// matchBlameResource finds the entry for a resource, by name and then by name
// without its scope, when that names one resource -- the rule every lookup of a
// stored record follows (api.ResourceIndex).
//
// The second try is what makes the upstream walk work at all: a variant fills in the
// namespace its base left as a placeholder, so the same resource is
// "apptique/frontend" here and "confighubplaceholder/frontend" upstream. Matching on
// the scoped name alone would stop every walk at the clone.
func matchBlameResource(identifier api.ResourceIdentifier, mutationSources goclientnew.ResourceMutationList, resourceType, resourceName, resourceNameCore string) *goclientnew.ResourceMutation {
	resources := make([]*goclientnew.ResourceInfo, len(mutationSources))
	for i := range mutationSources {
		resources[i] = mutationSources[i].Resource
	}
	if i, found := matchBlameResourceInfo(identifier, resources, resourceType, resourceName, resourceNameCore); found {
		return &mutationSources[i]
	}
	return nil
}

// matchBlameResourceInfo finds a resource among recorded ones as api.ResourceIndex does.
// A nil entry is no resource.
func matchBlameResourceInfo(identifier api.ResourceIdentifier, recorded []*goclientnew.ResourceInfo,
	resourceType, resourceName, resourceNameCore string) (int, bool) {
	var resources []api.ResourceInfo
	var positions []int
	for i, resource := range recorded {
		if resource == nil {
			continue
		}
		resources = append(resources, api.ResourceInfo{
			ResourceType:             api.ResourceType(resource.ResourceType),
			ResourceName:             api.ResourceName(resource.ResourceName),
			ResourceNameWithoutScope: api.ResourceName(resource.ResourceNameWithoutScope),
		})
		positions = append(positions, i)
	}
	position, found := api.NewResourceIndex(identifier, resources).Find(api.ResourceInfo{
		ResourceType:             api.ResourceType(resourceType),
		ResourceName:             api.ResourceName(resourceName),
		ResourceNameWithoutScope: api.ResourceName(resourceNameCore),
	})
	if !found {
		return 0, false
	}
	return positions[position], true
}

// AuthoredPath renders a stored path the way someone would type it: an associative
// segment keeps its selector and drops the ";@N" anchor, which is positional
// bookkeeping the merge engine keeps and no command accepts.
func AuthoredPath(path string) string {
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

// Filter applies --path, --resource, and the comment-field rule.
func Filter(fields []*Field, pathFilter, resourceFilter string, showInternal bool) []*Field {
	kept := make([]*Field, 0, len(fields))
	for _, f := range fields {
		// The "$comment$..." pseudo-fields carry YAML comments rather than
		// configuration, at whatever depth the comment is. They are real mutations, but
		// nobody reading a blame table is asking who wrote a comment.
		if !showInternal && IsCommentPath(f.Path) {
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

// IsCommentPath reports whether a path is a comment-carrying pseudo-field: one whose
// last segment holds a YAML comment rather than configuration.
func IsCommentPath(path string) bool {
	segments := strings.Split(path, ".")
	return strings.HasPrefix(segments[len(segments)-1], "$comment$")
}
