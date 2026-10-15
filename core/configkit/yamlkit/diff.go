// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	dmp "github.com/sergi/go-diff/diffmatchpatch"
	"sigs.k8s.io/kustomize/kyaml/yaml"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// A ConfigDiff is produced by the same walk that computes mutations, not by a second diff
// algorithm, so it always agrees with what a merge treats as changed. The walk records,
// through a diffRecorder, both nodes it compared at each site where it records a mutation,
// together with where the path sits in each document. Recovering the old values afterwards
// by resolving the mutation paths against the previous document does not work in general:
// an unkeyed array element's anchor is a digest of one side's content, so it resolves on
// that side only.

// diffOrder places a path segment in document order. pos is the segment's position on the
// To side; a segment present only on the From side sits just after the nearest preceding
// segment that survived, with tie separating several such segments by their From position.
type diffOrder struct {
	pos float64
	tie int
}

func compareDiffOrder(a, b diffOrder) int {
	if a.pos != b.pos {
		if a.pos < b.pos {
			return -1
		}
		return 1
	}
	return a.tie - b.tie
}

// diffLocation is where a path sits: its segments for display, and its order key.
type diffLocation struct {
	segments []api.PathSegment
	order    []diffOrder
}

func (location diffLocation) child(segment api.PathSegment, order diffOrder) diffLocation {
	return diffLocation{
		segments: append(slices.Clone(location.segments), segment),
		order:    append(slices.Clone(location.order), order),
	}
}

func (location diffLocation) field(key string, order diffOrder) diffLocation {
	return location.child(api.FieldSegment(key), order)
}

// element is the location of an array element. fromIndex or toIndex is negative when the
// element is absent from that side.
func (location diffLocation) element(mergeKeys, mergeKeyValues []string, fromIndex, toIndex int, order diffOrder) diffLocation {
	segment := api.PathSegment{FromIndex: fromIndex, ToIndex: toIndex}
	for i, key := range mergeKeys {
		value := ""
		if i < len(mergeKeyValues) {
			value = mergeKeyValues[i]
		}
		segment.MergeKeys = append(segment.MergeKeys, api.MergeKeyValue{Key: key, Value: value})
	}
	return location.child(segment, order)
}

// recordedChange is one change the walk recorded for a diff.
type recordedChange struct {
	changeType api.DiffChangeType
	location   diffLocation
	from, to   *gaby.YamlDoc
	// fromText and toText stand in for from and to when the change is not a value, as for
	// a Reorder or a Rename, and textDataType says how to read them.
	fromText, toText string
	textDataType     api.DataType
	patch            string
	// container is the path the walk was comparing when it recorded the change: the
	// change's own path, or the map or array it was found in.
	container string
}

// visitedPath is a path present on both sides that the walk compared, kept for context.
type visitedPath struct {
	// parent is the path of the map or array the walk found it in.
	parent   string
	location diffLocation
	to       *gaby.YamlDoc
}

// diffRecorder collects the changes a diff reports, keyed by the path the walk recorded
// the corresponding mutation at. It is nil unless a diff was asked for.
type diffRecorder struct {
	changes map[api.ResolvedPath]*recordedChange
	// context asks for the unchanged paths around the changes. The fields below are kept
	// only then.
	context bool
	// current is the path the walk is comparing.
	current string
	visited map[string]*visitedPath
}

func newDiffRecorder(context bool) *diffRecorder {
	recorder := &diffRecorder{changes: map[api.ResolvedPath]*recordedChange{}, context: context}
	if context {
		recorder.visited = map[string]*visitedPath{}
	}
	return recorder
}

// sub returns a recorder for a sub-walk whose result may be merged into this one.
func (recorder *diffRecorder) sub() *diffRecorder {
	if recorder == nil {
		return nil
	}
	return newDiffRecorder(recorder.context)
}

// enter notes that the walk is now comparing the two nodes at path.
func (recorder *diffRecorder) enter(path string) {
	if recorder == nil || !recorder.context {
		return
	}
	recorder.current = path
}

// visit notes a path, inside the one being compared, that is present on both sides and
// that the walk will compare. Whatever of it turns out unchanged is context.
func (recorder *diffRecorder) visit(path string, location diffLocation, to *gaby.YamlDoc) {
	if recorder == nil || !recorder.context {
		return
	}
	recorder.visited[path] = &visitedPath{parent: recorder.current, location: location, to: to}
}

// record notes a change between two nodes, either of which may be nil.
func (recorder *diffRecorder) record(path string, location diffLocation, changeType api.DiffChangeType, from, to *gaby.YamlDoc, patch string) {
	if recorder == nil {
		return
	}
	if changeType == api.DiffChangeTypeUpdate && from != nil && to != nil &&
		from.YNode().Kind != to.YNode().Kind {
		changeType = api.DiffChangeTypeReplace
	}
	if changeType == api.DiffChangeTypeUpdate && patch != "" {
		// The mutation's patch may be structural; a reader gets a line diff.
		patch = DisplayLinePatch(diffValueText(from), diffValueText(to))
	}
	recorder.changes[api.ResolvedPath(path)] = &recordedChange{
		changeType: changeType,
		location:   location,
		from:       from,
		to:         to,
		patch:      patch,
		container:  recorder.current,
	}
}

// recordText notes a change that is not between two nodes.
func (recorder *diffRecorder) recordText(path string, location diffLocation, changeType api.DiffChangeType, fromText, toText string, dataType api.DataType) {
	if recorder == nil {
		return
	}
	recorder.changes[api.ResolvedPath(path)] = &recordedChange{
		changeType:   changeType,
		location:     location,
		fromText:     fromText,
		toText:       toText,
		textDataType: dataType,
		container:    recorder.current,
	}
}

// merge adds the changes of a sub-walk whose result was accepted.
func (recorder *diffRecorder) merge(other *diffRecorder) {
	if recorder == nil || other == nil {
		return
	}
	maps.Copy(recorder.changes, other.changes)
	maps.Copy(recorder.visited, other.visited)
}

// contextChanges returns, for each map or array the changes are in, and each one those are
// in up to the resource, its children that are unchanged, as ChangeType None. An unchanged
// child that is itself a map or array is one entry holding its block.
func (recorder *diffRecorder) contextChanges() map[api.ResolvedPath]*recordedChange {
	// A path is marked when a change is at it or beneath it.
	marked := map[string]bool{}
	for _, change := range recorder.changes {
		for path := change.container; !marked[path]; {
			marked[path] = true
			visited, found := recorder.visited[path]
			if !found {
				break
			}
			path = visited.parent
		}
	}
	context := map[api.ResolvedPath]*recordedChange{}
	for path, visited := range recorder.visited {
		if !marked[visited.parent] || marked[path] || recorder.changes[api.ResolvedPath(path)] != nil {
			continue
		}
		context[api.ResolvedPath(path)] = &recordedChange{
			changeType: api.DiffChangeTypeNone,
			location:   visited.location,
			to:         visited.to,
		}
	}
	return context
}

// pathChanges returns the recorded changes in document order, with the unchanged paths
// around them when context was asked for.
func (recorder *diffRecorder) pathChanges() []api.PathChange {
	if recorder == nil || len(recorder.changes) == 0 {
		return nil
	}
	recorded := recorder.changes
	if recorder.context {
		recorded = recorder.contextChanges()
		maps.Copy(recorded, recorder.changes)
	}
	paths := make([]api.ResolvedPath, 0, len(recorded))
	for path := range recorded {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		a, b := recorded[paths[i]].location.order, recorded[paths[j]].location.order
		if c := slices.CompareFunc(a, b, compareDiffOrder); c != 0 {
			return c < 0
		}
		return paths[i] < paths[j]
	})
	changes := make([]api.PathChange, 0, len(paths))
	for _, path := range paths {
		change := recorded[path]
		segments, comment := commentTarget(change.location.segments)
		pathChange := api.PathChange{
			Path:        path,
			DisplayPath: api.DisplayPathFromSegments(segments),
			Segments:    segments,
			ChangeType:  change.changeType,
			Comment:     comment,
			FromValue:   change.fromText,
			ToValue:     change.toText,
			Patch:       change.patch,
		}
		if change.fromText != "" {
			pathChange.FromDataType = change.textDataType
		}
		if change.toText != "" {
			pathChange.ToDataType = change.textDataType
		}
		if change.from != nil {
			pathChange.FromValue = diffValueText(change.from)
			pathChange.FromDataType = diffValueDataType(change.from)
		}
		if change.to != nil {
			pathChange.ToValue = diffValueText(change.to)
			pathChange.ToDataType = diffValueDataType(change.to)
		}
		changes = append(changes, pathChange)
	}
	return changes
}

var diffComments = map[CommentType]api.DiffComment{
	CommentHead: api.DiffCommentHead,
	CommentLine: api.DiffCommentLine,
	CommentFoot: api.DiffCommentFoot,
}

// commentTarget reports a change to a comment, which the walk sees as a change to the
// comment's key beside the node it annotates (see CommentKey), at that node's path. A
// comment with no target key annotates the map that holds it.
func commentTarget(segments []api.PathSegment) ([]api.PathSegment, api.DiffComment) {
	if len(segments) == 0 {
		return segments, ""
	}
	last := segments[len(segments)-1]
	if last.IsElement() {
		return segments, ""
	}
	commentType, target, isComment := ParseCommentKey(last.Field)
	if !isComment {
		return segments, ""
	}
	targetSegments := slices.Clone(segments[:len(segments)-1])
	if target != "" {
		targetSegments = append(targetSegments, api.FieldSegment(target))
	}
	return targetSegments, diffComments[commentType]
}

// diffValueText renders a value for display: a scalar as its text, without the quoting or
// escaping YAML would give it, and anything else as a YAML block.
func diffValueText(doc *gaby.YamlDoc) string {
	if node := doc.YNode(); node != nil && node.Kind == yaml.ScalarNode {
		return node.Value
	}
	return strings.TrimRight(diffDocumentText(doc), "\n")
}

// diffDocumentText renders a map or array as YAML, with the comments the parse lifted into
// comment keys (see CommentKey) written back as comments.
func diffDocumentText(doc *gaby.YamlDoc) string {
	text := doc.String()
	if !strings.Contains(text, CommentKeyPrefix) {
		return text
	}
	copied, err := gaby.ParseYAML([]byte(text))
	if err != nil {
		return text
	}
	docs := []*gaby.YamlDoc{copied}
	if copied.IsArray() {
		docs = copied.Children()
	}
	for _, d := range docs {
		if err := d.InjectCommentsFromKeys(); err != nil {
			return text
		}
	}
	// A document's own head comment is kept as a key, holding the comment lines that were
	// above its separator.
	header := ""
	if node := copied.YNode(); node.Kind == yaml.MappingNode && len(node.Content) >= 2 &&
		node.Content[0].Value == CommentKey(CommentHead, "") {
		if node.Content[1].Value != "" {
			header = strings.TrimRight(node.Content[1].Value, "\n") + "\n"
		}
		node.Content = node.Content[2:]
	}
	return header + copied.String()
}

// diffValueDataType says how to read a value's text, as functions type values: a scalar is
// a string, int, or bool, or has no DataType, and anything else is YAML.
func diffValueDataType(doc *gaby.YamlDoc) api.DataType {
	node := doc.YNode()
	if node == nil {
		return api.DataTypeNone
	}
	if node.Kind != yaml.ScalarNode {
		return api.DataTypeYAML
	}
	return api.DataTypeOfValue(doc.Data())
}

// orderedMapKeys returns a map's scalar keys in document order.
func orderedMapKeys(doc *gaby.YamlDoc) []string {
	if doc == nil {
		return nil
	}
	node := doc.YNode()
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	keys := make([]string, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Kind == yaml.ScalarNode {
			keys = append(keys, node.Content[i].Value)
		}
	}
	return keys
}

// mapKeyOrders places every key of either map in document order.
func mapKeyOrders(previous, modified *gaby.YamlDoc) map[string]diffOrder {
	previousKeys := orderedMapKeys(previous)
	modifiedKeys := orderedMapKeys(modified)
	orders := make(map[string]diffOrder, len(modifiedKeys)+len(previousKeys))
	modifiedPosition := make(map[string]int, len(modifiedKeys))
	for i, key := range modifiedKeys {
		modifiedPosition[key] = i
		orders[key] = diffOrder{pos: float64(i)}
	}
	last := -1.0
	for j, key := range previousKeys {
		if position, survived := modifiedPosition[key]; survived {
			last = float64(position)
			continue
		}
		orders[key] = diffOrder{pos: last + 0.5, tie: j}
	}
	return orders
}

// removedElementOrder places an element present only on the From side just after the
// nearest preceding element that survived. previousToModified maps each From index to its
// To index, or -1.
func removedElementOrder(previousIndex int, previousToModified []int) diffOrder {
	for i := previousIndex - 1; i >= 0; i-- {
		if previousToModified[i] >= 0 {
			return diffOrder{pos: float64(previousToModified[i]) + 0.5, tie: previousIndex}
		}
	}
	return diffOrder{pos: -0.5, tie: previousIndex}
}

// reorderedCommonKeys reports whether the elements present on both sides of a merge-keyed
// array are in a different relative order, and returns each side's sequence of them.
// Adding or removing an element changes the key sequence without reordering anything.
func reorderedCommonKeys(previousKeys, modifiedKeys []string) (bool, []string, []string) {
	inPrevious := make(map[string]bool, len(previousKeys))
	for _, key := range previousKeys {
		inPrevious[key] = true
	}
	inModified := make(map[string]bool, len(modifiedKeys))
	for _, key := range modifiedKeys {
		inModified[key] = true
	}
	var previousCommon, modifiedCommon []string
	for _, key := range previousKeys {
		if inModified[key] {
			previousCommon = append(previousCommon, key)
		}
	}
	for _, key := range modifiedKeys {
		if inPrevious[key] {
			modifiedCommon = append(modifiedCommon, key)
		}
	}
	return !slices.Equal(previousCommon, modifiedCommon), previousCommon, modifiedCommon
}

// keySequenceText renders a sequence of merge-key values as a YAML block list.
func keySequenceText(keys []string) string {
	lines := make([]string, len(keys))
	for i, key := range keys {
		lines[i] = "- " + key
	}
	return strings.Join(lines, "\n")
}

// DiffOptions adjusts what ComputeDiff returns.
type DiffOptions struct {
	// IncludeUnchanged returns resources with no changes, as ChangeType None.
	IncludeUnchanged bool
	// IncludeContext returns, within each changed resource, the unchanged paths around the
	// changes, as ChangeType None: every unchanged child of each map or array that holds a
	// change, from the change up to the resource.
	IncludeContext bool
}

// ComputeDiff returns the difference between two configurations for display. Resources are
// matched as ComputeMutations matches them, including across renames, and each resource's
// changes are those the mutation walk found, with both sides' values.
func ComputeDiff(previousParsedData, modifiedParsedData gaby.Container, resourceProvider ResourceProvider, options DiffOptions) (api.ConfigDiff, error) {
	matching, err := matchResources(previousParsedData, modifiedParsedData, 0, resourceProvider)
	if err != nil {
		return api.ConfigDiff{}, err
	}

	type orderedResource struct {
		order diffOrder
		diff  api.ResourceDiff
	}
	var resources []orderedResource

	previousToModified := make([]int, len(previousParsedData))
	for i := range previousToModified {
		previousToModified[i] = -1
	}
	for modifiedDocIndex, previousDocIndex := range matching.matchedPrevious {
		if previousDocIndex >= 0 {
			previousToModified[previousDocIndex] = modifiedDocIndex
		}
	}

	for modifiedDocIndex, previousDocIndex := range matching.matchedPrevious {
		modifiedInfo := matching.modifiedInfos[modifiedDocIndex]
		order := diffOrder{pos: float64(modifiedDocIndex)}
		if previousDocIndex < 0 {
			resources = append(resources, orderedResource{order: order, diff: api.ResourceDiff{
				Resource:   *modifiedInfo,
				ChangeType: api.MutationTypeAdd,
				ToValue:    diffDocumentText(modifiedParsedData[modifiedDocIndex]),
			}})
			continue
		}
		previousInfo := matching.previousInfos[previousDocIndex]
		recorder := newDiffRecorder(options.IncludeContext)
		diffResourcePairRecorded(previousParsedData[previousDocIndex], modifiedParsedData[modifiedDocIndex],
			modifiedInfo.ResourceType, 0, resourceProvider, recorder)
		resourceDiff := api.ResourceDiff{
			Resource:   *modifiedInfo,
			ChangeType: api.MutationTypeUpdate,
			Changes:    recorder.pathChanges(),
		}
		if previousInfo.ResourceName != modifiedInfo.ResourceName || previousInfo.ResourceType != modifiedInfo.ResourceType {
			resourceDiff.PreviousResource = previousInfo
		}
		if len(resourceDiff.Changes) == 0 && resourceDiff.PreviousResource == nil {
			if !options.IncludeUnchanged {
				continue
			}
			resourceDiff.ChangeType = api.MutationTypeNone
		}
		resources = append(resources, orderedResource{order: order, diff: resourceDiff})
	}

	for previousDocIndex := range previousParsedData {
		if matching.previousMatched[previousDocIndex] {
			continue
		}
		resources = append(resources, orderedResource{
			order: removedElementOrder(previousDocIndex, previousToModified),
			diff: api.ResourceDiff{
				Resource:   *matching.previousInfos[previousDocIndex],
				ChangeType: api.MutationTypeDelete,
				FromValue:  diffDocumentText(previousParsedData[previousDocIndex]),
			},
		})
	}

	slices.SortStableFunc(resources, func(a, b orderedResource) int {
		return compareDiffOrder(a.order, b.order)
	})
	diff := api.ConfigDiff{Resources: make([]api.ResourceDiff, 0, len(resources))}
	for _, resource := range resources {
		diff.Resources = append(diff.Resources, resource.diff)
	}
	return diff, nil
}

// diffContextLines is how many unchanged lines a display patch keeps around each change.
const diffContextLines = 3

// DisplayLinePatch returns a unified diff of two multi-line strings for a reader: hunks with
// a few lines of context, each line prefixed with '+', '-' or ' '. It differs from
// ComputeLinePatch, whose text is escaped for ApplyLinePatch rather than for reading.
func DisplayLinePatch(previous, modified string) string {
	d := dmp.New()
	chars1, chars2, lineArray := d.DiffLinesToChars(previous, modified)
	diffs := d.DiffCharsToLines(d.DiffMain(chars1, chars2, false), lineArray)

	type patchLine struct {
		op   byte
		text string
	}
	var lines []patchLine
	for _, diff := range diffs {
		op := byte(' ')
		switch diff.Type {
		case dmp.DiffInsert:
			op = '+'
		case dmp.DiffDelete:
			op = '-'
		}
		for _, text := range strings.SplitAfter(diff.Text, "\n") {
			if text != "" {
				lines = append(lines, patchLine{op: op, text: strings.TrimSuffix(text, "\n")})
			}
		}
	}

	// Group changed lines, with their context, into hunks.
	var b strings.Builder
	previousLine, modifiedLine := 1, 1
	for start := 0; start < len(lines); {
		first := start
		for first < len(lines) && lines[first].op == ' ' {
			first++
		}
		if first == len(lines) {
			break
		}
		hunkStart := max(first-diffContextLines, start)
		end := first
		for end < len(lines) {
			if lines[end].op != ' ' {
				end++
				continue
			}
			run := end
			for run < len(lines) && lines[run].op == ' ' {
				run++
			}
			if run == len(lines) || run-end > 2*diffContextLines {
				end = min(end+diffContextLines, run)
				break
			}
			end = run
		}
		// Everything skipped before the hunk is context, on both sides.
		previousLine += hunkStart - start
		modifiedLine += hunkStart - start
		previousCount, modifiedCount := 0, 0
		for _, line := range lines[hunkStart:end] {
			if line.op != '+' {
				previousCount++
			}
			if line.op != '-' {
				modifiedCount++
			}
		}
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", previousLine, previousCount, modifiedLine, modifiedCount)
		for _, line := range lines[hunkStart:end] {
			b.WriteByte(line.op)
			b.WriteString(line.text)
			b.WriteByte('\n')
		}
		previousLine += previousCount
		modifiedLine += modifiedCount
		start = end
	}
	return b.String()
}
