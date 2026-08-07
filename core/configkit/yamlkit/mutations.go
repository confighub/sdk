// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// ComputeMutations and ComputeMutationsForDocs Overview
//
// ComputeMutations performs a structured diff between two YAML configurations (represented
// as gaby.Container, which is a list of parsed YAML documents). It determines what changed
// and records the result as an api.ResourceMutationList: one entry per resource, each
// carrying a resource-level MutationType, a PathMutationMap of leaf-level changes, and
// alias information so renamed resources can still be matched.
//
// The output is the data form of a "patch" that can be:
//
//  1. Replayed onto a different (or earlier) configuration via PatchMutations as part of a
//     three-way merge (e.g., upgrading a downstream Unit from upstream).
//  2. Accumulated across sequential edits via api.OffsetMutations + AddMutations to record
//     a compiled history of who changed what (used as predicates for selective patching).
//  3. Diffed against another mutation set via SubtractMutations / PatchMutations'
//     mutationsToSubtract argument so target-side changes survive the patch.
//
// Inputs:
//   - previousParsedData / modifiedParsedData: the "before" / "after" parsed YAML docs.
//   - functionIndex: a sequence number identifying which operation produced this diff.
//   - resourceProvider: toolchain-specific interface for extracting resource metadata
//     and (importantly) for declaring which array paths are merge-keyed associative arrays.
//
// Algorithm at a glance:
//
//  1. Resource matching (this function): for each modified doc, find the corresponding
//     previous doc by ResourceType+Name, then by fuzzy similarity
//     score (path-mutation count / total lines, with a maxMatchScore threshold). Unmatched
//     modified docs are Adds; unmatched previous docs are Deletes. Both old and new names
//     are recorded in Aliases / AliasesWithoutScopes so subsequent operations can match
//     across renames.
//
//  2. Path-level diff (ComputeMutationsForDocs): for matched resources, do a stack-based
//     deep comparison. Maps are compared by key. Arrays whose path the resource provider
//     declares a merge key for match elements by merge-key value, and their paths use the
//     ?key=value;@index syntax so the per-element mutation can be applied at the target
//     element regardless of its current index. The rest are positional: elements match by
//     index, except that an element removed from or inserted into the array is recognized
//     as such and recorded as a Delete or Add of that one element (alignArrayElements),
//     rather than as an Update of every element after it.
//
//  3. Resource-level MutationType is then Add (modified-only), Delete (previous-only),
//     Update (matched and path map non-empty), or None (matched, no path changes).
//
// Example:
//
//	previous:                                    modified:
//	  apiVersion: apps/v1                          apiVersion: apps/v1
//	  kind: Deployment                             kind: Deployment
//	  metadata:                                    metadata:
//	    name: myapp                                  name: myapp
//	  spec:                                        spec:
//	    replicas: 1                                  replicas: 3
//	    template:                                    template:
//	      spec:                                        spec:
//	        containers:                                  containers:
//	        - name: app                                  - name: app
//	          image: nginx:1.19                            image: nginx:1.20
//
// With the K8s resource provider (merge key "name" on containers) the result is:
//
//	ResourceMutationList{
//	  {
//	    Resource: {ResourceType: "apps/v1/Deployment", ResourceName: "default/myapp", ...},
//	    ResourceMutationInfo: {MutationType: Update, Index: 1},
//	    PathMutationMap: {
//	      "spec.replicas": {MutationType: Update, Value: "3"},
//	      "spec.template.spec.containers.?name=app;@0.image": {MutationType: Update, Value: "nginx:1.20"},
//	    },
//	    Aliases: {"default/myapp": {}},
//	  },
//	}

// MergeKeyLookup is a function that returns the merge key field names for a given
// array path, if any exist. It is used by ComputeMutationsForDocs to match array
// elements by merge key value instead of positional index.
type MergeKeyLookup func(path string) ([]string, bool)

// anchorKeyPrefix marks a pair in an associative path segment whose key is not a field of
// the element but something computed from it. Field names never start with it, so a
// segment carrying such a pair is recognizably an anchor rather than a merge key.
const anchorKeyPrefix = "~"

// anchorDigestKey is the anchor pair whose value is a digest of the element's content.
const anchorDigestKey = "~h"

// anchorIdentityKey is the anchor pair whose value is a projection of the element that
// identifies it more stably than its content does — the part of it that says *which*
// element this is, as opposed to what it currently says. A digest stops matching the
// moment the target edits the element; an identity survives that, which is the difference
// between finding the element the patch means and writing over whatever now sits at its
// old index.
const anchorIdentityKey = "~i"

// anchorPreviousKey and anchorNextKey are the anchor pairs carrying digests of the
// elements on either side of this one — the structured analogue of a text patch's context
// lines. They are what lets the resolver refuse a positional fallback: an index alone says
// where the element sat, and the neighbors say whether the element sitting there now is
// plausibly the same one. Without them a patch that cannot find its element writes over
// whatever occupies its old index, which is how replaying a positional removal onto its own
// result used to delete the following element.
const anchorPreviousKey = "~p"
const anchorNextKey = "~n"

// anchorLengthKey carries the length of the array the path was computed against. It is the
// cheapest thing the anchor knows and the one that survives a target that customized every
// element: neighbor digests say nothing when every neighbor has been edited, but an array
// that still has the same number of elements has not had anything inserted or removed under
// the path, so its indices still mean what the patch meant by them.
const anchorLengthKey = "~l"

// arrayEdgeDigest is the neighbor value recorded when there is no neighbor, so that "first
// element of the array" is a fact the resolver can check rather than a gap it has to guess
// about.
const arrayEdgeDigest = "^"

// neighborDigestLength is how much of a neighbor's digest a path segment carries. Neighbors
// only have to be told apart from the other elements of one array, and they are recorded on
// every anchored segment, so they are kept shorter than the element's own digest: a
// collision between two neighbors costs an accepted positional fallback that would otherwise
// have been refused, not a wrong element.
const neighborDigestLength = 4

// neighborDigest returns the digest recorded for an element's neighbor, or arrayEdgeDigest
// when the element is at the edge of the array.
func neighborDigest(elements []*gaby.YamlDoc, index int) string {
	if index < 0 || index >= len(elements) {
		return arrayEdgeDigest
	}
	digest := ElementDigest(elements[index])
	if digest == "" {
		return arrayEdgeDigest
	}
	if len(digest) > neighborDigestLength {
		digest = digest[:neighborDigestLength]
	}
	return digest
}

// commandLineFlagRegexp matches a command-line flag with an inline value: one or two
// leading dashes, a name, then '='. It deliberately does not match a bare flag (there the
// whole value is already the identity) or a value that merely contains '=' without the
// leading dash.
var commandLineFlagRegexp = regexp.MustCompile(`^--?[A-Za-z0-9][^=\s]*=`)

// identityDirective is the structured comment that lets a person say which element of an
// unkeyed array this is. Two forms:
//
//	routes:
//	# confighub:id=api
//	- match: Host(`a.example.com`)
//	- match: Host(`b.example.com`)   # confighub:id=web
//	- name: admin                    # confighub:id
//	  match: Host(`c.example.com`)
//
// `confighub:id=<value>` states the identity outright. A bare `confighub:id` on a field
// says that field's value is the identity, which is how a person nominates a field the
// provider has no merge key for.
const identityDirective = "confighub:id"

// ElementIdentity returns a projection of an array element that identifies it independently
// of its current value, or "" when the element offers none.
//
// Two sources, the person's first:
//
//   - A structured comment, which is the only mechanism here that lets someone *correct* the
//     engine rather than work around it. Where a person has said which element this is, that
//     is what it is. See identityDirective.
//   - A scalar holding a command-line flag with an inline value, whose identity is the flag
//     name. An args list is the most common unkeyed array in a Kubernetes workload, and
//     `--log.level=INFO` and `--log.level=DEBUG` are the same flag.
//
// Either way the point is the same: an element the target has *edited* can still be found,
// which no digest can do, however either side has since reordered the list.
//
// Both sides of a merge have to carry the markup for it to do anything — the source records
// it in the path, and the target is matched against it — so it belongs upstream, where a
// clone inherits it, rather than only on the variant that needed it.
func ElementIdentity(element *gaby.YamlDoc) string {
	if element == nil {
		return ""
	}
	if identity := markedIdentity(element); identity != "" {
		return identity
	}
	value, ok := element.Data().(string)
	if !ok {
		return ""
	}
	if loc := commandLineFlagRegexp.FindStringIndex(value); loc != nil {
		// The name, without the '='.
		return value[:loc[1]-1]
	}
	return ""
}

// markedIdentity reads the identity a person wrote on an element, or "".
//
// The comment can sit on the element itself — a line above it, or an inline comment when the
// element is a scalar — or on any of its fields, which is where an inline comment on the
// first line of a mapping element actually lands. Fields are read in document order, so two
// markings resolve the same way every time.
func markedIdentity(element *gaby.YamlDoc) string {
	if identity, marked, found := identityFromComments(element.GetComments()); found && !marked {
		return identity
	}
	node := element.YNode()
	if node == nil || node.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		for _, comments := range []string{nodeComments(key), nodeComments(value)} {
			identity, marked, found := identityFromComments(comments)
			if !found {
				continue
			}
			if marked {
				// The field itself is the identity.
				if value.Kind == yaml.ScalarNode && identityIsUsable(value.Value) {
					return value.Value
				}
				continue
			}
			return identity
		}
	}
	return ""
}

// nodeComments joins whatever comments are attached to a YAML node.
func nodeComments(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	var parts []string
	for _, comment := range []string{node.HeadComment, node.LineComment, node.FootComment} {
		if comment != "" {
			parts = append(parts, comment)
		}
	}
	return strings.Join(parts, "\n")
}

// identityFromComments looks for the identity directive in a comment. marked reports the bare
// form, which names the field it sits on rather than carrying a value of its own.
func identityFromComments(comments string) (identity string, marked, found bool) {
	if comments == "" {
		return "", false, false
	}
	for _, line := range strings.Split(comments, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
		if line == identityDirective {
			return "", true, true
		}
		value, isDirective := strings.CutPrefix(line, identityDirective+"=")
		if !isDirective {
			continue
		}
		if value = strings.TrimSpace(value); identityIsUsable(value) {
			return value, false, true
		}
	}
	return "", false, false
}

// identityIsUsable rejects a value that would not survive being written into a path segment.
// Dots are escaped on the way in, but the characters the segment syntax itself uses have no
// encoding, so an identity containing one is ignored rather than corrupting the path.
func identityIsUsable(value string) bool {
	return value != "" && !strings.ContainsAny(value, ",=;")
}

// anchorDigestLength is how much of the digest the path segment carries. A path is read by
// people as well as by the resolver, and 8 hex characters is enough to tell the elements of
// one array apart while keeping the path legible.
const anchorDigestLength = 8

// ElementDigest returns a short digest of an array element's content, over the parsed
// subtree in canonical form rather than its text: comments, key order, indentation, and
// quoting style do not change it, so an element that was only reformatted still matches.
func ElementDigest(element *gaby.YamlDoc) string {
	if element == nil {
		return ""
	}
	canonical, err := json.Marshal(element.Data())
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])[:anchorDigestLength]
}

// AnchoredPathSegment builds the path segment naming an element of an array that has no
// declared merge key: the positional index, as before, plus a digest of the element's
// content.
//
// The index alone says where the element sat in the configuration the patch was computed
// against, which is only where it sits in the target if the target has not drifted. The
// digest says which element it was, so a target that inserted, removed, or reordered
// elements around it can still be patched in the right place. Where the target has edited
// the element itself the digest no longer matches and resolution falls back to the index,
// which is where it was before anchors — the anchor can only improve the outcome.
//
// The digest is taken from the element as it stood on the side the path names it from: the
// previous revision for a matched pair or a removal, the new content for an insertion.
// Both sides of a three-way merge diff against the same base, so their anchors for the same
// element agree and subtraction still recognizes them as the same path.
//
// withContext adds the shape of the array around the element: digests of the elements on
// either side and the array's length. All of it describes the array the patch was computed
// against, so it is recorded for a path that names an element of that array — a matched
// element or a removal — and not for an insertion, whose index names a position in the
// array that only exists once the patch has been applied.
func AnchoredPathSegment(elements []*gaby.YamlDoc, index int, withContext bool) string {
	var element *gaby.YamlDoc
	if index >= 0 && index < len(elements) {
		element = elements[index]
	}
	digest := ElementDigest(element)
	if digest == "" {
		return strconv.Itoa(index)
	}
	keys, values := []string{anchorDigestKey}, []string{digest}
	if identity := ElementIdentity(element); identity != "" {
		keys = append(keys, anchorIdentityKey)
		values = append(values, identity)
	}
	if withContext {
		keys = append(keys, anchorPreviousKey, anchorNextKey, anchorLengthKey)
		values = append(values, neighborDigest(elements, index-1), neighborDigest(elements, index+1),
			strconv.Itoa(len(elements)))
	}
	return AssociativePathSegment(keys, values, index)
}

// arrayContext is what an anchored segment records about the array around its element, as
// opposed to about the element itself.
type arrayContext struct {
	previous, next string
	length         int
	present        bool
}

// splitAnchorContext separates an anchored segment's array-context pairs from the pairs that
// describe the element. The element-matching stages only ever see the latter: a neighbor
// digest is a fact about the array, and an element cannot be asked whether it matches one.
func splitAnchorContext(keys, values []string) (contentKeys, contentValues []string, context arrayContext) {
	context.length = -1
	for i, key := range keys {
		switch key {
		case anchorPreviousKey:
			context.previous, context.present = values[i], true
		case anchorNextKey:
			context.next, context.present = values[i], true
		case anchorLengthKey:
			if length, err := strconv.Atoi(values[i]); err == nil {
				context.length, context.present = length, true
			}
		default:
			contentKeys = append(contentKeys, key)
			contentValues = append(contentValues, values[i])
		}
	}
	return contentKeys, contentValues, context
}

// indexIsTrustworthy reports whether an element's recorded index still means what the patch
// meant by it.
//
// Matching neighbors settle it: the element sits between the elements it sat between, so
// whatever else the target has done to the array, this position is the one the patch meant.
//
// Failing that, the array's length has to be the length the patch was computed against —
// inserting or removing an element is the only thing that renumbers the ones after it — and
// that is usually enough on its own, because a target that customized every element of an
// array (the case the whole positional-array mechanism exists for) has no neighbor left to
// match. But length alone can be a coincidence: an array that gained one element and lost
// another is the length it started at while its indices have all moved. The tell is that the
// element now at the index is one of the ones recorded beside it, which is what the array
// looks like after it has shifted under the path — as it has when a removal is replayed onto
// its own result.
//
// A target that has changed the array's length and edited the neighbors has told the
// resolver nothing it can rely on, and the caller reports an unresolved path rather than
// writing over whatever has moved into place.
func (c arrayContext) indexIsTrustworthy(elements []*gaby.YamlDoc, index int) bool {
	if !c.present {
		return true
	}
	if neighborsMatch(elements, index, c.previous, c.next) {
		return true
	}
	return c.length == len(elements) && !c.elementIsANeighbor(elements, index)
}

// elementIsANeighbor reports whether the element at index is one of the two the anchor
// recorded on either side of the element it names — the sign that the array has shifted
// under the path rather than that the element there was edited.
func (c arrayContext) elementIsANeighbor(elements []*gaby.YamlDoc, index int) bool {
	digest := neighborDigest(elements, index)
	if digest == arrayEdgeDigest {
		return false
	}
	return digest == c.previous || digest == c.next
}

// neighborsMatch reports whether the elements on either side of index are the ones the
// anchor recorded.
func neighborsMatch(elements []*gaby.YamlDoc, index int, previous, next string) bool {
	return neighborDigest(elements, index-1) == previous && neighborDigest(elements, index+1) == next
}

// segmentIsAnchor reports whether an associative path segment names its element by content
// rather than by merge key, which is what distinguishes an element of a positional array
// from an element that has an identity of its own.
func segmentIsAnchor(segment string) bool {
	pairs, isAssociative := strings.CutPrefix(segment, "?")
	if !isAssociative {
		return false
	}
	if rest, _, found := strings.Cut(pairs, ";@"); found {
		pairs = rest
	}
	keys, _, ok := associativeSegmentPairs(pairs)
	if !ok {
		return false
	}
	for _, key := range keys {
		if strings.HasPrefix(key, anchorKeyPrefix) {
			return true
		}
	}
	return false
}

// mergeKeySeparator separates the key=value pairs of an associative path segment whose
// array is identified by more than one field. A value containing it (or an '=') has no
// unambiguous encoding, so such an element is matched positionally instead — see
// MergeKeyValues.
const mergeKeySeparator = ","

// MergeKeyValues returns the merge-key values of an array element, in the order of keys.
// It reports false when the element is missing any of them, or when a value contains a
// character the path segment uses as punctuation: either way the element has no usable
// identity and the caller falls back to matching it by position.
func MergeKeyValues(element *gaby.YamlDoc, mergeKeys []string) ([]string, bool) {
	if element == nil || len(mergeKeys) == 0 {
		return nil, false
	}
	values := make([]string, 0, len(mergeKeys))
	for _, key := range mergeKeys {
		node := element.S(key)
		if node == nil {
			return nil, false
		}
		value := fmt.Sprintf("%v", node.Data())
		if len(mergeKeys) > 1 && (strings.Contains(value, mergeKeySeparator) || strings.Contains(value, "=")) {
			return nil, false
		}
		values = append(values, value)
	}
	return values, true
}

// mergeKeyValuesFromNode is MergeKeyValues for a raw mapping node, used by the patch-side
// passes that manipulate the YAML tree directly.
func mergeKeyValuesFromNode(elem *yaml.Node, mergeKeys []string) ([]string, bool) {
	if elem == nil || elem.Kind != yaml.MappingNode || len(mergeKeys) == 0 {
		return nil, false
	}
	values := make([]string, len(mergeKeys))
	found := make([]bool, len(mergeKeys))
	for j := 0; j+1 < len(elem.Content); j += 2 {
		k, v := elem.Content[j], elem.Content[j+1]
		if k.Kind != yaml.ScalarNode || v.Kind != yaml.ScalarNode {
			continue
		}
		for i, key := range mergeKeys {
			if k.Value == key {
				values[i] = v.Value
				found[i] = true
			}
		}
	}
	for _, ok := range found {
		if !ok {
			return nil, false
		}
	}
	return values, true
}

// MergeKeyIdentity joins merge-key values into the single string used to match an element
// between two revisions and to record array ordering.
func MergeKeyIdentity(values []string) string {
	return strings.Join(values, mergeKeySeparator)
}

// splitMergeKeyIdentity is the inverse of MergeKeyIdentity.
func splitMergeKeyIdentity(identity string, mergeKeys []string) []string {
	if len(mergeKeys) <= 1 {
		return []string{identity}
	}
	return strings.SplitN(identity, mergeKeySeparator, len(mergeKeys))
}

// AssociativePathSegment builds a path segment encoding the merge key values and the
// positional index, using the syntax ?key=value;@index — or, for an array whose elements
// are identified by more than one field, ?key1=value1,key2=value2;@index. Keys and values
// are escaped to handle dots.
func AssociativePathSegment(mergeKeys []string, mergeKeyValues []string, index int) string {
	var b strings.Builder
	b.WriteString("?")
	for i, key := range mergeKeys {
		if i > 0 {
			b.WriteString(mergeKeySeparator)
		}
		b.WriteString(EscapeDotsInPathSegment(key))
		b.WriteString("=")
		if i < len(mergeKeyValues) {
			b.WriteString(EscapeDotsInPathSegment(mergeKeyValues[i]))
		}
	}
	b.WriteString(";@")
	b.WriteString(strconv.Itoa(index))
	return b.String()
}

// associativeSegmentPairs parses the key=value pairs of an associative path segment (the
// text after '?' and before ';@'). Returns false if any pair is malformed.
func associativeSegmentPairs(segment string) (keys, values []string, ok bool) {
	for _, pair := range strings.Split(segment, mergeKeySeparator) {
		key, value, found := strings.Cut(pair, "=")
		if !found {
			return nil, nil, false
		}
		keys = append(keys, key)
		values = append(values, value)
	}
	return keys, values, true
}

// matchArrayElement returns the index of the element matching every key=value pair, or -1.
//
// The recorded index is tried first, when the element there also matches. An array may hold
// several elements that match — two identical arguments, two elements sharing a merge key,
// a flag repeated with different values — and the one the path was computed against is the
// one at the index it recorded. Scanning from the front would send a change meant for the
// second occurrence to the first.
func matchArrayElement(elements []*gaby.YamlDoc, keys, values []string, fallbackIndex string) int {
	if index, err := strconv.Atoi(fallbackIndex); err == nil && index >= 0 && index < len(elements) &&
		elementMatchesPairs(elements[index], keys, values) {
		return index
	}
	for index, child := range elements {
		if elementMatchesPairs(child, keys, values) {
			return index
		}
	}
	return -1
}

// identifyingPairs returns the pairs of an anchored segment that identify the element
// independently of its current content — everything but the digest. Returns nothing for a
// segment that has only a digest, or for a merge-key segment, whose pairs are all identity
// already and have had their chance.
func identifyingPairs(keys, values []string) ([]string, []string) {
	var identityKeys, identityValues []string
	anchored := false
	for i, key := range keys {
		if key == anchorDigestKey {
			anchored = true
			continue
		}
		if strings.HasPrefix(key, anchorKeyPrefix) {
			anchored = true
		}
		identityKeys = append(identityKeys, key)
		identityValues = append(identityValues, values[i])
	}
	if !anchored {
		return nil, nil
	}
	return identityKeys, identityValues
}

// elementHasAnyKey reports whether an array element carries any of the merge-key fields.
// An element with none of them predates the merge key (or belongs to a toolchain that
// never had one) and is matched by position.
func elementHasAnyKey(element *gaby.YamlDoc, keys []string) bool {
	for _, key := range keys {
		// Anchor pairs do not count. A declared merge key is strong identity: an
		// element carrying a different value for it is a different element, and the
		// caller must not patch it. An anchor is weak identity — a digest that no
		// longer matches usually means the target edited the element, not that this is
		// the wrong element — so a segment that names its element only by content
		// always falls back to the index, which is where it was before anchors.
		if strings.HasPrefix(key, anchorKeyPrefix) {
			continue
		}
		if element.S(key) != nil {
			return true
		}
	}
	return false
}

// elementMatchesPairs reports whether an array element has all of the given key=value
// pairs. A key starting with anchorKeyPrefix names something computed from the element
// rather than one of its fields.
func elementMatchesPairs(element *gaby.YamlDoc, keys, values []string) bool {
	for i, key := range keys {
		if strings.HasPrefix(key, anchorKeyPrefix) {
			switch key {
			case anchorDigestKey:
				if ElementDigest(element) != values[i] {
					return false
				}
			case anchorIdentityKey:
				if ElementIdentity(element) != values[i] {
					return false
				}
			default:
				// An anchor kind this build does not know about. Refuse to guess.
				return false
			}
			continue
		}
		node := element.S(key)
		if node == nil || fmt.Sprintf("%v", node.Data()) != values[i] {
			return false
		}
	}
	return true
}

// ResolveAssociativeSegments resolves ?key=value;@index segments in a path to numeric
// indices by looking up elements in the document. An array whose elements are identified
// by more than one field carries one pair per key — ?key1=value1,key2=value2;@index — and
// an element matches only when it has all of them. If no element matches, it considers the
// positional index:
//
//   - Out of bounds: the index is used as-is. This preserves Add-as-append semantics
//     (e.g., a new element being appended to an array) and is harmless for Delete since
//     the caller checks existence before deleting.
//   - In bounds, element has no merge-key field: legacy data — fall back positionally.
//   - In bounds, element has a different merge-key value: a different element. The
//     segment is left unresolved so the caller can skip the operation.
//
// Returns the resolved path and a bool that is true only when every associative segment
// was resolved (by merge-key match, by out-of-bounds index, or by legacy fallback).
func ResolveAssociativeSegments(doc *gaby.YamlDoc, path string) (string, bool) {
	if !strings.Contains(path, "?") {
		return path, true
	}
	segments := gaby.DotPathToSlice(path)
	var resolvedSegments []string
	currentNode := doc
	allResolved := true
	for _, segment := range segments {
		kv, isAssociative := strings.CutPrefix(segment, "?")
		if !isAssociative {
			resolvedSegments = append(resolvedSegments, EscapeDotsInPathSegment(segment))
			if currentNode != nil {
				currentNode = currentNode.S(segment)
			}
			continue
		}
		// Parse ?key=value[,key=value...] with an optional ;@index suffix.
		pairs := kv
		fallbackIndex := ""
		if rest, idx, found := strings.Cut(kv, ";@"); found {
			pairs = rest
			fallbackIndex = idx
		}
		keys, values, ok := associativeSegmentPairs(pairs)
		if !ok {
			// Invalid, keep as-is
			resolvedSegments = append(resolvedSegments, EscapeDotsInPathSegment(segment))
			currentNode = nil
			allResolved = false
			continue
		}

		// Neighbor context describes the array rather than the element, so it is held
		// aside: the element-matching stages below see only the pairs that describe the
		// element itself.
		keys, values, context := splitAnchorContext(keys, values)

		resolved := false
		if currentNode != nil {
			elements := currentNode.Children()
			// Match in stages, weakest claim last. First on every pair; then, for an
			// anchored segment, on the identifying pairs alone, which is what finds an
			// element the target has edited — the digest stops matching the moment it
			// does, and the identity is the part that says which element it is.
			//
			// Array context deliberately does not get a stage of its own. Searching for
			// the element whose neighbors are the recorded ones finds a bystander as
			// readily as the element that moved: in an array whose first element was
			// removed, the element that took its place sits between the same neighbors
			// the removed one did. Context is evidence about a position, so it is used to
			// judge the recorded position below and not to nominate a different one.
			matchIndex := matchArrayElement(elements, keys, values, fallbackIndex)
			if matchIndex < 0 {
				if identityKeys, identityValues := identifyingPairs(keys, values); len(identityKeys) > 0 {
					matchIndex = matchArrayElement(elements, identityKeys, identityValues, fallbackIndex)
				}
			}
			if matchIndex >= 0 {
				resolvedSegments = append(resolvedSegments, strconv.Itoa(matchIndex))
				currentNode = elements[matchIndex]
				resolved = true
			}
			// Fall back to the positional index when there is no key match. If the index
			// is out of bounds (appending), use it as-is. If the element at the index
			// has none of the merge keys, treat it as legacy data and fall back
			// positionally. Otherwise the in-bounds element has different merge-key
			// values — it's a different element, so leave the segment unresolved.
			//
			// An anchored segment carrying neighbor context has one more condition: the
			// element at the index is only accepted when it sits between the elements the
			// anchor recorded. That is what stops a patch whose element is gone from
			// writing over whatever moved into its place.
			if !resolved && fallbackIndex != "" {
				idx, err := strconv.Atoi(fallbackIndex)
				if err == nil && idx >= 0 {
					if idx >= len(elements) {
						resolvedSegments = append(resolvedSegments, fallbackIndex)
						currentNode = nil
						resolved = true
					} else if !elementHasAnyKey(elements[idx], keys) && context.indexIsTrustworthy(elements, idx) {
						resolvedSegments = append(resolvedSegments, fallbackIndex)
						currentNode = elements[idx]
						resolved = true
					}
				}
			}
		}
		if !resolved {
			// Couldn't resolve. Keep the segment as-is so callers can detect the
			// unresolved state and skip the operation.
			resolvedSegments = append(resolvedSegments, EscapeDotsInPathSegment(segment))
			currentNode = nil
			allResolved = false
		}
	}
	return strings.Join(resolvedSegments, "."), allResolved
}

// mergeArrayOrderMaps merges source-side and target-side ArrayOrderMap entries,
// for each path that source has, by weaving the two desired sequences together.
// Paths only in target are dropped (target's reorder for an array source didn't
// touch is already in place; no patch reorder is needed). See mergeArrayOrders
// for the per-array merge semantics.
//
// sourceArrayElementAliases (a sourceParentPath -> {oldKey -> newKey} map)
// translates target-side keys to source-side keys before merging when source
// renamed an element: target's diff (computed against the merge base) uses
// the previous merge-key value for that element, while source's ArrayOrders
// entry uses the new key. Translating target's order brings them into the
// same key namespace so common elements line up correctly during the merge.
func mergeArrayOrderMaps(sourceArrayOrders, targetArrayOrders api.ArrayOrderMap, sourceArrayElementAliases api.ArrayElementAliasMap) api.ArrayOrderMap {
	if len(sourceArrayOrders) == 0 {
		return nil
	}
	if len(targetArrayOrders) == 0 && len(sourceArrayElementAliases) == 0 {
		return sourceArrayOrders
	}
	result := make(api.ArrayOrderMap, len(sourceArrayOrders))
	for path, sourceOrder := range sourceArrayOrders {
		targetOrder := targetArrayOrders[path]
		if pathAliases, ok := sourceArrayElementAliases[path]; ok && len(pathAliases) > 0 && len(targetOrder) > 0 {
			translated := make([]string, len(targetOrder))
			for i, k := range targetOrder {
				if newKey, ok := pathAliases[k]; ok {
					translated[i] = newKey
				} else {
					translated[i] = k
				}
			}
			targetOrder = translated
		}
		if len(targetOrder) == 0 {
			result[path] = sourceOrder
			continue
		}
		result[path] = mergeArrayOrders(sourceOrder, targetOrder)
	}
	return result
}

// mergeArrayOrders combines source's and target's desired sequences for a single
// merge-keyed array path. The result threads the two so:
//
//   - Common elements (present in both sequences by merge-key value) form a
//     spine in source's order. If source and target disagree about the relative
//     order of common elements, source wins (it's the patch's intent).
//   - Source-only elements (added by source's diff, not in target) keep their
//     position relative to source's spine: each is emitted right after its
//     preceding common element from source's view.
//   - Target-only elements (added by target's diff, not in source) keep their
//     position relative to source's spine using their preceding common element
//     from target's view: each is emitted right after that common.
//   - At each common anchor, source-only elements emit before target-only
//     elements (source has explicit intent to add at that position; target's
//     element predates the patch).
//   - Front-of-array (no preceding common): source-only first, then target-only.
//
// This is the LCS-style merge described in the positional-arrays plan: the
// common subsequence is the LCS picked in source's order; insertions on either
// side are attached to their preceding LCS anchor.
func mergeArrayOrders(sourceOrder, targetOrder []string) []string {
	if len(sourceOrder) == 0 {
		return targetOrder
	}
	if len(targetOrder) == 0 {
		return sourceOrder
	}
	sourceSet := make(map[string]bool, len(sourceOrder))
	for _, k := range sourceOrder {
		sourceSet[k] = true
	}
	targetSet := make(map[string]bool, len(targetOrder))
	for _, k := range targetOrder {
		targetSet[k] = true
	}

	// Bucket source-only keys by their preceding common in source's order.
	// Empty key = "before the first common".
	sourceOnlyAfter := make(map[string][]string)
	var lastCommon string
	for _, s := range sourceOrder {
		if sourceSet[s] && targetSet[s] {
			lastCommon = s
		} else {
			sourceOnlyAfter[lastCommon] = append(sourceOnlyAfter[lastCommon], s)
		}
	}
	// Bucket target-only keys by their preceding common in target's order.
	targetOnlyAfter := make(map[string][]string)
	lastCommon = ""
	for _, t := range targetOrder {
		if sourceSet[t] && targetSet[t] {
			lastCommon = t
		} else if !sourceSet[t] {
			targetOnlyAfter[lastCommon] = append(targetOnlyAfter[lastCommon], t)
		}
	}

	// Emit. Front first, then walk source over commons.
	result := make([]string, 0, len(sourceOrder)+len(targetOrder))
	result = append(result, sourceOnlyAfter[""]...)
	result = append(result, targetOnlyAfter[""]...)
	for _, s := range sourceOrder {
		if !(sourceSet[s] && targetSet[s]) {
			continue // source-only, already emitted via the bucket
		}
		result = append(result, s)
		result = append(result, sourceOnlyAfter[s]...)
		result = append(result, targetOnlyAfter[s]...)
	}
	return result
}

// reorderArrayByMergeKey rearranges the elements of a SequenceNode at the given
// path inside doc so they match desiredOrder by merge-key value. Elements whose
// merge-key value is in desiredOrder are emitted in that order, followed by
// elements not in desiredOrder (in their original relative order). Elements
// without a merge-key value are also kept in their original relative order
// after the keyed ones.
//
// path may contain associative segments — they're resolved against doc first.
// If the path doesn't resolve to a SequenceNode, this is a no-op.
func reorderArrayByMergeKey(doc *gaby.YamlDoc, path string, mergeKeys []string, desiredOrder []string) {
	if doc == nil || len(mergeKeys) == 0 || len(desiredOrder) == 0 {
		return
	}
	resolvedPath, ok := ResolveAssociativeSegments(doc, path)
	if !ok {
		return
	}
	arrayDoc := doc.Path(resolvedPath)
	if arrayDoc == nil {
		return
	}
	node := arrayDoc.YNode()
	if node == nil || node.Kind != yaml.SequenceNode {
		return
	}
	content := node.Content
	if len(content) <= 1 {
		return
	}

	// Build merge-key value -> index for current elements. Elements without a
	// merge-key field map to the empty string, but we don't include those in
	// the lookup map (they're always treated as not-in-desired).
	keyToIndex := make(map[string]int, len(content))
	for i, elem := range content {
		if values, ok := mergeKeyValuesFromNode(elem, mergeKeys); ok {
			keyToIndex[MergeKeyIdentity(values)] = i
		}
	}

	used := make([]bool, len(content))
	reordered := make([]*yaml.Node, 0, len(content))
	for _, key := range desiredOrder {
		if idx, found := keyToIndex[key]; found && !used[idx] {
			reordered = append(reordered, content[idx])
			used[idx] = true
		}
	}
	for i, elem := range content {
		if !used[i] {
			reordered = append(reordered, elem)
		}
	}
	node.Content = reordered
}

// mutationCost returns the cost (in leaf-value units) of a single mutation.
// A path mutation with no Value or with a scalar Value contributes 1; one
// whose Value is a YAML mapping or sequence contributes the number of leaf
// scalars inside the subtree, since deleting/adding a whole container or
// a whole resources block represents many field-level changes even though
// it shows up as a single entry in PathMutationMap.
func mutationCost(info api.MutationInfo) int {
	if info.Value == "" {
		return 1
	}
	parsed, err := gaby.ParseYAML([]byte(info.Value))
	if err != nil || parsed == nil {
		return 1
	}
	n := countLeafNodes(parsed.YNode())
	if n < 1 {
		return 1
	}
	return n
}

// mutationMapCost sums mutationCost over every entry in the map.
func mutationMapCost(m api.MutationMap) int {
	total := 0
	for _, info := range m {
		total += mutationCost(info)
	}
	return total
}

// isMappingDoc reports whether the document holds a mapping (a YAML object), unwrapping a
// document node. A nil doc is not a mapping.
func isMappingDoc(doc *gaby.YamlDoc) bool {
	if doc == nil {
		return false
	}
	node := doc.YNode()
	if node == nil {
		return false
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	return node.Kind == yaml.MappingNode
}

// countLeafNodes returns the number of scalar leaves reachable from node by
// recursively descending into mappings and sequences. A scalar contributes 1.
func countLeafNodes(node *yaml.Node) int {
	if node == nil {
		return 0
	}
	switch node.Kind {
	case yaml.DocumentNode:
		total := 0
		for _, c := range node.Content {
			total += countLeafNodes(c)
		}
		return total
	case yaml.MappingNode:
		total := 0
		// Mapping content alternates key, value; count value subtrees only.
		for i := 1; i < len(node.Content); i += 2 {
			total += countLeafNodes(node.Content[i])
		}
		return total
	case yaml.SequenceNode:
		total := 0
		for _, c := range node.Content {
			total += countLeafNodes(c)
		}
		return total
	case yaml.ScalarNode, yaml.AliasNode:
		return 1
	}
	return 0
}

// arrayAlignment records how the elements of a non-associative (positional) array in the
// previous configuration correspond to those in the modified configuration.
type arrayAlignment struct {
	// pairs holds (previousIndex, modifiedIndex) for elements that correspond to each
	// other, in ascending order of both indices.
	pairs [][2]int
	// deleted holds the previous indices with no counterpart, ascending.
	deleted []int
	// added holds the modified indices with no counterpart, ascending.
	added []int
}

// maxAlignmentPairs bounds the cost-minimizing alignment. Beyond it the arrays are aligned
// purely positionally (index i to index i, with a tail of deletions or additions), which is
// what every non-associative array got before alignment was introduced. The bound keeps the
// quadratic sub-diff cost in check for pathologically large arrays; real configuration
// arrays are far smaller.
const maxAlignmentPairs = 2500

// alignArrayElements pairs up the elements of a non-associative array across a diff so that
// an element removed from — or inserted into — the middle of the array is recorded as a
// single Delete or Add of that element, rather than as a cascade of Updates over every
// element that follows it. The cascade is what a positional diff produces, and it destroys
// every downstream customization after the point of change when the diff is replayed as a
// merge patch.
//
// The alignment is the minimum-cost order-preserving matching, computed with the usual
// edit-distance recurrence over the two element sequences:
//
//   - pairing element i with element j costs the leaf-value count of the sub-diff between
//     them (zero when they are identical), so near-identical elements pair up;
//   - dropping a previous element or introducing a modified one costs that element's own
//     leaf-value count.
//
// Costs are doubled and a pairing of two elements that differ carries an extra unit, which
// decides the case where editing one element into another costs exactly what dropping it
// and introducing the other does. That happens when the two share nothing, and the elements
// are then paired only because they sit at the same index — reading it as a removal and an
// insertion instead leaves the surrounding elements alone. Editing still wins whenever it is
// genuinely cheaper, so an array whose elements were edited in place aligns index to index
// exactly as the old positional diff did.
// Elements a person has named are matched by that name first, before any of this. An
// identity says which element this is regardless of where it sits, which the alignment
// cannot work out for itself: the recurrence is order-preserving, so two elements that swap
// places can never both be paired, and the one that loses is recorded as a removal and an
// insertion — which makes the whole element the target's, and the next merge's change to a
// field of it is filtered out as an override. Matching by name first is what stops that, and
// it is the same treatment a declared merge key gets.
func alignArrayElements(previous, modified []*gaby.YamlDoc, path string, mergeKeyLookup MergeKeyLookup) arrayAlignment {
	identityPairs, previousRest, modifiedRest := matchByIdentity(previous, modified)
	if len(identityPairs) > 0 {
		// Align what is left over among themselves, then translate its indices back.
		rest := alignRemainingElements(previous, modified, previousRest, modifiedRest, path, mergeKeyLookup)
		return mergeAlignments(identityPairs, rest, previousRest, modifiedRest)
	}
	return alignByCost(previous, modified, path, mergeKeyLookup)
}

// matchByIdentity pairs elements that carry the same identity, and returns the indices of
// those left over on each side.
//
// An identity is only usable here when it names exactly one element on each side: two
// elements sharing one identity say nothing about which is which, so both are left to the
// cost-based alignment rather than paired arbitrarily.
func matchByIdentity(previous, modified []*gaby.YamlDoc) (pairs [][2]int, previousRest, modifiedRest []int) {
	previousByIdentity := uniqueIdentities(previous)
	modifiedByIdentity := uniqueIdentities(modified)
	pairedPrevious := make([]bool, len(previous))
	pairedModified := make([]bool, len(modified))
	for identity, i := range previousByIdentity {
		j, ok := modifiedByIdentity[identity]
		if !ok {
			continue
		}
		pairs = append(pairs, [2]int{i, j})
		pairedPrevious[i], pairedModified[j] = true, true
	}
	slices.SortFunc(pairs, func(a, b [2]int) int { return a[0] - b[0] })
	for i, paired := range pairedPrevious {
		if !paired {
			previousRest = append(previousRest, i)
		}
	}
	for j, paired := range pairedModified {
		if !paired {
			modifiedRest = append(modifiedRest, j)
		}
	}
	return pairs, previousRest, modifiedRest
}

// uniqueIdentities maps each identity that names exactly one element to that element's index.
func uniqueIdentities(elements []*gaby.YamlDoc) map[string]int {
	seen := map[string]int{}
	duplicated := map[string]struct{}{}
	for i, element := range elements {
		identity := ElementIdentity(element)
		if identity == "" {
			continue
		}
		if _, exists := seen[identity]; exists {
			duplicated[identity] = struct{}{}
			continue
		}
		seen[identity] = i
	}
	for identity := range duplicated {
		delete(seen, identity)
	}
	return seen
}

// alignRemainingElements runs the cost-based alignment over the elements identity did not
// claim, in their original order.
func alignRemainingElements(previous, modified []*gaby.YamlDoc, previousRest, modifiedRest []int,
	path string, mergeKeyLookup MergeKeyLookup) arrayAlignment {
	if len(previousRest) == 0 && len(modifiedRest) == 0 {
		return arrayAlignment{}
	}
	previousSubset := make([]*gaby.YamlDoc, len(previousRest))
	for k, i := range previousRest {
		previousSubset[k] = previous[i]
	}
	modifiedSubset := make([]*gaby.YamlDoc, len(modifiedRest))
	for k, j := range modifiedRest {
		modifiedSubset[k] = modified[j]
	}
	return alignByCost(previousSubset, modifiedSubset, path, mergeKeyLookup)
}

// mergeAlignments translates a leftover alignment's indices back into the original arrays and
// combines it with the identity-matched pairs.
func mergeAlignments(identityPairs [][2]int, rest arrayAlignment, previousRest, modifiedRest []int) arrayAlignment {
	alignment := arrayAlignment{pairs: identityPairs}
	for _, pair := range rest.pairs {
		alignment.pairs = append(alignment.pairs, [2]int{previousRest[pair[0]], modifiedRest[pair[1]]})
	}
	for _, i := range rest.deleted {
		alignment.deleted = append(alignment.deleted, previousRest[i])
	}
	for _, j := range rest.added {
		alignment.added = append(alignment.added, modifiedRest[j])
	}
	slices.SortFunc(alignment.pairs, func(a, b [2]int) int { return a[0] - b[0] })
	slices.Sort(alignment.deleted)
	slices.Sort(alignment.added)
	return alignment
}

func alignByCost(previous, modified []*gaby.YamlDoc, path string, mergeKeyLookup MergeKeyLookup) arrayAlignment {
	if len(previous)*len(modified) > maxAlignmentPairs {
		return positionalAlignment(len(previous), len(modified))
	}

	elementCost := func(doc *gaby.YamlDoc) int {
		return 2 * max(countLeafNodes(doc.YNode()), 1)
	}
	previousCost := make([]int, len(previous))
	for i, doc := range previous {
		previousCost[i] = elementCost(doc)
	}
	modifiedCost := make([]int, len(modified))
	for j, doc := range modified {
		modifiedCost[j] = elementCost(doc)
	}
	previousText := make([]string, len(previous))
	for i, doc := range previous {
		previousText[i] = doc.String()
	}
	modifiedText := make([]string, len(modified))
	for j, doc := range modified {
		modifiedText[j] = doc.String()
	}

	// pairCost is the cost of turning previous[i] into modified[j]. Identical elements
	// short-circuit to zero without computing a sub-diff.
	pairCost := func(i, j int) int {
		if previousText[i] == modifiedText[j] {
			return 0
		}
		// Two elements a person named differently are different elements, whatever they
		// have in common — the same rule a declared merge key follows.
		previousIdentity, modifiedIdentity := ElementIdentity(previous[i]), ElementIdentity(modified[j])
		if previousIdentity != "" && modifiedIdentity != "" && previousIdentity != modifiedIdentity {
			return math.MaxInt32
		}
		subPathMap := api.MutationMap{}
		ComputeMutationsForDocs(path+"."+strconv.Itoa(i), previous[i], modified[j], 0,
			subPathMap, mergeKeyLookup, nil, nil)
		return 2*mutationMapCost(subPathMap) + 1
	}

	// cost[i][j] is the cheapest alignment of previous[:i] with modified[:j]. move records
	// the choice that produced it, for the backtrack below.
	const (
		movePair = iota
		moveDelete
		moveAdd
	)
	cost := make([][]int, len(previous)+1)
	move := make([][]int, len(previous)+1)
	for i := range cost {
		cost[i] = make([]int, len(modified)+1)
		move[i] = make([]int, len(modified)+1)
	}
	for i := 1; i <= len(previous); i++ {
		cost[i][0] = cost[i-1][0] + previousCost[i-1]
		move[i][0] = moveDelete
	}
	for j := 1; j <= len(modified); j++ {
		cost[0][j] = cost[0][j-1] + modifiedCost[j-1]
		move[0][j] = moveAdd
	}
	for i := 1; i <= len(previous); i++ {
		for j := 1; j <= len(modified); j++ {
			best := cost[i-1][j-1] + pairCost(i-1, j-1)
			bestMove := movePair
			if c := cost[i-1][j] + previousCost[i-1]; c < best {
				best, bestMove = c, moveDelete
			}
			if c := cost[i][j-1] + modifiedCost[j-1]; c < best {
				best, bestMove = c, moveAdd
			}
			cost[i][j] = best
			move[i][j] = bestMove
		}
	}

	var alignment arrayAlignment
	for i, j := len(previous), len(modified); i > 0 || j > 0; {
		switch {
		case i > 0 && j > 0 && move[i][j] == movePair:
			alignment.pairs = append(alignment.pairs, [2]int{i - 1, j - 1})
			i, j = i-1, j-1
		case i > 0 && (j == 0 || move[i][j] == moveDelete):
			alignment.deleted = append(alignment.deleted, i-1)
			i--
		default:
			alignment.added = append(alignment.added, j-1)
			j--
		}
	}
	slices.Reverse(alignment.pairs)
	slices.Reverse(alignment.deleted)
	slices.Reverse(alignment.added)
	return alignment
}

// positionalAlignment pairs index i with index i and treats the tail of the longer sequence
// as deletions or additions — the alignment a purely positional diff produces.
func positionalAlignment(previousLen, modifiedLen int) arrayAlignment {
	alignment := arrayAlignment{}
	for i := 0; i < min(previousLen, modifiedLen); i++ {
		alignment.pairs = append(alignment.pairs, [2]int{i, i})
	}
	for i := modifiedLen; i < previousLen; i++ {
		alignment.deleted = append(alignment.deleted, i)
	}
	for j := previousLen; j < modifiedLen; j++ {
		alignment.added = append(alignment.added, j)
	}
	return alignment
}

// appendPathForAdd returns a numeric path that appends to the parent array of an
// associative path whose trailing segment couldn't be matched by merge-key value.
// Used by applyPathMutations when an Add's merge-key path doesn't match any
// existing element and the element at the fallback index has a different
// merge-key value: rather than overwriting that unrelated element, the new
// element is appended to the array (index = len(elements)).
//
// Returns the rewritten path and true on success. Returns false if the path's
// last segment is not associative or the parent path can't be resolved to an
// array in the current doc.
func appendPathForAdd(doc *gaby.YamlDoc, path string) (string, bool) {
	segments := gaby.DotPathToSlice(path)
	if len(segments) == 0 {
		return "", false
	}
	last := segments[len(segments)-1]
	if !strings.HasPrefix(last, "?") {
		return "", false
	}
	parentSegments := segments[:len(segments)-1]
	parentPath := JoinPathSegments(parentSegments)
	parentNode := doc.Path(parentPath)
	if parentNode == nil {
		return "", false
	}
	children := parentNode.Children()
	if children == nil {
		return "", false
	}
	parentSegments = append(parentSegments, strconv.Itoa(len(children)))
	return JoinPathSegments(parentSegments), true
}

// CanonicalMutationPath drops the positional fallback from every associative segment of a
// path, turning ?key=value;@index into ?key=value.
//
// The index in an associative segment is a fallback for resolution, not part of the
// element's identity: it records where the element sat in the configuration the path was
// computed against. Two paths that name the same element by merge key are the same path
// even when the element sits at different positions on either side — which it will
// whenever one side has added or removed an earlier element. Comparing paths between two
// independently computed diffs has to be done on this form; applying a path does not,
// since the fallback is useful there.
//
// Splitting on "." is safe: dots within a segment are escaped as ~1.
func CanonicalMutationPath(path api.ResolvedPath) api.ResolvedPath {
	if !strings.Contains(string(path), ";@") {
		return path
	}
	segments := strings.Split(string(path), ".")
	for i, segment := range segments {
		if !strings.HasPrefix(segment, "?") {
			continue
		}
		if index := strings.Index(segment, ";@"); index >= 0 {
			segments[i] = segment[:index]
		}
	}
	return api.ResolvedPath(strings.Join(segments, "."))
}

// canonicalMutationMap re-keys a MutationMap by CanonicalMutationPath, and returns a map
// from each canonical path back to the path it came from.
func canonicalMutationMap(m api.MutationMap) (api.MutationMap, map[api.ResolvedPath]api.ResolvedPath) {
	canonical := make(api.MutationMap, len(m))
	originals := make(map[api.ResolvedPath]api.ResolvedPath, len(m))
	for path, info := range m {
		canonicalPath := CanonicalMutationPath(path)
		canonical[canonicalPath] = info
		originals[canonicalPath] = path
	}
	return canonical, originals
}

// arrayIndexBeyondEnd reports whether a resolved path addresses an element more than one
// position past the end of its parent array, and if so returns the path of the first free
// slot — the append position.
//
// ResolveAssociativeSegments deliberately keeps an out-of-bounds fallback index so that an
// Add of a merge-keyed element lands as an append. That is right when the index is exactly
// the array's length. When the target array is shorter still — the patch adds the third
// element of an array the target has trimmed to one — the index asks for a gap, which the
// setter cannot express: it replaces the value at the path's root with a null instead of
// growing the array, silently emptying the document. Appending is what the patch meant.
// It returns the append path only when the offending index is the path's last segment,
// which is the only case that can be repaired by appending: an index in the middle of a
// path addresses an element that has to already exist for the rest of the path to mean
// anything.
func arrayIndexBeyondEnd(doc *gaby.YamlDoc, resolvedPath string, allowAppend bool) (appendPath string, atEnd, beyondEnd bool) {
	segments := gaby.DotPathToSlice(resolvedPath)
	for i, segment := range segments {
		index, err := strconv.Atoi(segment)
		if err != nil {
			continue
		}
		// The append position — index == length — is a real position only for the last
		// segment of an Add, which is asking for a new element at the end. Anywhere
		// else the path is addressing an element that has to already be there.
		canAppendHere := allowAppend && i == len(segments)-1
		parentSegments := slices.Clone(segments[:i])
		var parentNode *gaby.YamlDoc
		if len(parentSegments) == 0 {
			parentNode = doc
		} else {
			parentNode = doc.Path(JoinPathSegments(parentSegments))
		}
		if parentNode == nil {
			return "", false, false
		}
		node := parentNode.YNode()
		if node == nil || node.Kind != yaml.SequenceNode {
			continue
		}
		if index < len(node.Content) || (canAppendHere && index == len(node.Content)) {
			continue
		}
		if !canAppendHere {
			return "", false, true
		}
		return JoinPathSegments(append(slices.Clone(segments[:i]), strconv.Itoa(len(node.Content)))), true, true
	}
	return "", false, false
}

// StripAssociativeSegments converts ?key=value;@index segments to just the numeric index.
// For ?key=@index (direct index), extracts just the index.
// Non-associative segments are passed through as-is.
func StripAssociativeSegments(path string) string {
	if !strings.Contains(path, "?") {
		return path
	}
	segments := gaby.DotPathToSlice(path)
	for i, segment := range segments {
		kv, isAssociative := strings.CutPrefix(segment, "?")
		if !isAssociative {
			continue
		}
		_, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if idx, isDirect := strings.CutPrefix(value, "@"); isDirect {
			// ?key=@index -> index
			segments[i] = idx
		} else if _, idx, ok := strings.Cut(value, ";@"); ok {
			// ?key=value;@index -> index
			segments[i] = idx
		}
	}
	return JoinPathSegments(segments)
}

// MergeKeyEntry represents a merge key/value pair extracted from an associative path segment.
type MergeKeyEntry struct {
	Key   string // merge key field name (e.g., "name")
	Value string // merge key value (e.g., "config")
}

// ExtractMergeKeysFromPath extracts merge key/value pairs from associative path segments.
// Path segments of the form ?key=value;@index yield {Key: key, Value: value}.
func ExtractMergeKeysFromPath(path string) []MergeKeyEntry {
	if !strings.Contains(path, "?") {
		return nil
	}
	segments := gaby.DotPathToSlice(path)
	var entries []MergeKeyEntry
	for _, segment := range segments {
		kv, isAssociative := strings.CutPrefix(segment, "?")
		if !isAssociative {
			continue
		}
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(value, "@") {
			// ?key=@index — direct index, no merge key value
			continue
		}
		if v, _, ok := strings.Cut(value, ";@"); ok {
			value = v
		}
		entries = append(entries, MergeKeyEntry{
			Key:   strings.ReplaceAll(key, "~1", "."),
			Value: strings.ReplaceAll(value, "~1", "."),
		})
	}
	return entries
}

// ComputeMutationsForDocs determines the edits that have been performed to transform the
// previousDoc into modifiedDoc and records them in pathMutationMap (modified in place),
// associated with the provided functionIndex.
//
// mergeKeyLookup, if non-nil, is called with array paths to determine whether the array is
// merge-keyed associative. If so, elements are matched by merge-key value (not positional
// index) and paths use the ?key=value;@index syntax. The positional index is retained as a
// fallback hint, but PatchMutations only uses it when the element at that index has no
// merge-key field (legacy data) or the index is out of bounds (append).
//
// Design notes:
//
//   - Elements of an array with no merge key are matched by position, but an element
//     removed from or inserted into the middle of one is recorded as a Delete or Add of
//     that element (see alignArrayElements) rather than as a positional shift of every
//     element after it. PatchMutations applies those last, so the indices the rest of the
//     patch carries stay valid while it is being applied.
//   - Removed paths are not tombstoned. If an element in the downstream is then modified
//     by upstream, the corresponding path will be present in mutationsPatch and absent
//     from the target's data; the upstream path's child will not be re-added because
//     PatchMutations honors the target's removal via mutationsToSubtract.
//   - The reciprocal case — a field modified downstream while the surrounding block is
//     removed upstream — is reconciled in PatchMutations / SubtractMutations.
//
// TODO: Decide what to do about embedded accessors
//
// arrayOrders, if non-nil, is populated with the desired merge-key sequence for
// every merge-keyed array we descend into whose modified-side order or element
// set differs from the previous side. PatchMutations consumes these to reorder
// the target array after path mutations are applied, so positional associative
// arrays preserve source-side ordering.
//
// recordAddedSubtree records what a newly present subtree adds, one entry per path the
// addition actually introduces.
//
// A map's keys are its identity, so a map that did not exist before is an addition of each
// of its keys rather than of one opaque value. Recording the whole map made it a single
// owned path, and ownership of a path covers everything under it: a downstream that added
// its own annotation to a resource that had none owned `metadata.annotations` entire, and
// the next upgrade's unrelated annotation was filtered out against it — two sides adding
// different keys to the same map is the case a data merge should handle best, and it was
// handling it worst.
//
// Recursion stops at anything that is not a non-empty map. A scalar has nothing below it. An
// array's elements are identified positionally or by merge key, which is a different
// mechanism, and an array that is wholly new has no element the target could own. An empty
// map is recorded as itself, because there is no key to record it under and something has to
// create it.
func recordAddedSubtree(path string, doc *gaby.YamlDoc, functionIndex int64, pathMutationMap api.MutationMap) {
	children := doc.ChildrenMap()
	if len(children) == 0 || !isMappingDoc(doc) {
		pathMutationMap[api.ResolvedPath(path)] = api.MutationInfo{
			MutationType: api.MutationTypeAdd,
			Index:        functionIndex,
			Predicate:    true,
			Value:        doc.String(), // new data
		}
		return
	}
	for key, child := range children {
		recordAddedSubtree(path+"."+EscapeDotsInPathSegment(key), child, functionIndex, pathMutationMap)
	}
}

// arrayElementAliases, if non-nil, is populated with element-level renames
// detected inside merge-keyed arrays. When an unmatched modified element and
// an unmatched previous element are similar enough, the pair is treated as a
// rename: child paths are emitted under the previous merge-key value (so they
// align with target-side paths in SubtractMutations) and the alias is
// recorded so PatchMutations rewrites the merge-key field at apply time.
func ComputeMutationsForDocs(rootPath string, previousDoc *gaby.YamlDoc, modifiedDoc *gaby.YamlDoc, functionIndex int64, pathMutationMap api.MutationMap, mergeKeyLookup MergeKeyLookup, arrayOrders api.ArrayOrderMap, arrayElementAliases api.ArrayElementAliasMap) {
	// Define a traversal item for our stack
	type traversalItem struct {
		path        string
		previousDoc *gaby.YamlDoc
		modifiedDoc *gaby.YamlDoc
	}

	// Initialize the stack with the root traversal item
	stack := []traversalItem{{
		path:        rootPath,
		previousDoc: previousDoc,
		modifiedDoc: modifiedDoc,
	}}

	// Process items until the stack is empty
	for len(stack) > 0 {
		// Pop the top item from the stack
		last := len(stack) - 1
		item := stack[last]
		stack = stack[:last]

		path := item.path
		previousDoc := item.previousDoc
		modifiedDoc := item.modifiedDoc

		// Now process this item (similar logic to the recursive function)
		modifiedChildren := modifiedDoc.ChildrenMap()
		previousChildren := previousDoc.ChildrenMap()

		if len(modifiedChildren) > 0 {
			if len(previousChildren) == 0 {
				// modifiedDoc is a map, but previousDoc is not a map, though it exists.
				// The path's contents have completely changed in this case.
				pathMutationMap[api.ResolvedPath(path)] = api.MutationInfo{
					MutationType: api.MutationTypeUpdate,
					Index:        functionIndex,
					Predicate:    true,
					Value:        modifiedDoc.String(), // new data
				}
				continue // process next stack element
			}

			// Process all modified children
			for key, modifiedChild := range modifiedChildren {
				var currentPath string
				if path != "" {
					currentPath = path + "." + EscapeDotsInPathSegment(key)
				} else {
					currentPath = EscapeDotsInPathSegment(key)
				}

				previousChild, present := previousChildren[key]
				if !present {
					recordAddedSubtree(currentPath, modifiedChild, functionIndex, pathMutationMap)
					continue // process next stack element
				}

				// Instead of recursion, push this item to the stack
				stack = append(stack, traversalItem{
					path:        currentPath,
					previousDoc: previousChild,
					modifiedDoc: modifiedChild,
				})

				delete(previousChildren, key)
			}

			// Remaining previousChildren must have been deleted
			for key, previousChild := range previousChildren {
				var currentPath string
				if path != "" {
					currentPath = path + "." + EscapeDotsInPathSegment(key)
				} else {
					currentPath = EscapeDotsInPathSegment(key)
				}
				pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
					MutationType: api.MutationTypeDelete,
					Index:        functionIndex,
					Predicate:    true,
					Value:        previousChild.String(), // deleted data
				}
			}
		} else if modifiedArrayChildren := modifiedDoc.Children(); modifiedArrayChildren != nil {
			// Compare arrays, treating differences in length as additions and deletions.
			// If a merge key is defined for this array path, match elements by merge key
			// value instead of positional index.
			// We'll also land here in the case of an empty map. Or empty arrays.
			previousArrayChildren := previousDoc.Children()
			if len(modifiedArrayChildren) == 0 && len(previousArrayChildren) == 0 {
				// Both are empty. No changes.
				continue // process next stack element
			}

			if !modifiedDoc.IsArray() {
				// modifiedDoc is an empty map.
				if len(previousChildren) != 0 {
					// The map children were deleted.
					for key, previousChild := range previousChildren {
						var currentPath string
						if path != "" {
							currentPath = path + "." + EscapeDotsInPathSegment(key)
						} else {
							currentPath = EscapeDotsInPathSegment(key)
						}
						pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
							MutationType: api.MutationTypeDelete,
							Index:        functionIndex,
							Predicate:    true,
							Value:        previousChild.String(), // deleted data
						}
					}
				} else {
					// The whole path was changed.
					pathMutationMap[api.ResolvedPath(path)] = api.MutationInfo{
						MutationType: api.MutationTypeUpdate,
						Index:        functionIndex,
						Predicate:    true,
						Value:        modifiedDoc.String(), // new data
					}
				}
				continue // process next stack element
			}

			if modifiedDoc.IsArray() && !previousDoc.IsArray() {
				// modifiedDoc is an array, but previousDoc is not an array, though it exists.
				// The path's contents have completely changed in this case.
				pathMutationMap[api.ResolvedPath(path)] = api.MutationInfo{
					MutationType: api.MutationTypeUpdate,
					Index:        functionIndex,
					Predicate:    true,
					Value:        modifiedDoc.String(), // new data
				}
				continue // process next stack element
			}

			// Check if this array has merge keys for associative matching.
			var mergeKeys []string
			if mergeKeyLookup != nil {
				mergeKeys, _ = mergeKeyLookup(path)
			}

			if len(mergeKeys) != 0 {
				// Associative array matching: match elements by merge key value.
				// Build a map from merge key value -> index for previous elements.
				type previousEntry struct {
					index int
					doc   *gaby.YamlDoc
				}
				type pendingAddEntry struct {
					modifiedIndex int
					modifiedChild *gaby.YamlDoc
					keyValue      string
				}
				var pendingAdds []pendingAddEntry
				previousByKey := make(map[string]previousEntry, len(previousArrayChildren))
				previousKeySeq := make([]string, 0, len(previousArrayChildren))
				for i, child := range previousArrayChildren {
					if values, ok := MergeKeyValues(child, mergeKeys); ok {
						keyValue := MergeKeyIdentity(values)
						previousByKey[keyValue] = previousEntry{index: i, doc: child}
						previousKeySeq = append(previousKeySeq, keyValue)
					}
				}
				// Build the modified-side merge-key sequence for arrayOrders.
				modifiedKeySeq := make([]string, 0, len(modifiedArrayChildren))
				for _, modifiedChild := range modifiedArrayChildren {
					if values, ok := MergeKeyValues(modifiedChild, mergeKeys); ok {
						modifiedKeySeq = append(modifiedKeySeq, MergeKeyIdentity(values))
					}
				}

				// Track which previous elements were matched.
				previousMatched := make([]bool, len(previousArrayChildren))

				for modifiedIndex, modifiedChild := range modifiedArrayChildren {
					modifiedKeyValues, hasKeyValues := MergeKeyValues(modifiedChild, mergeKeys)
					if !hasKeyValues {
						// No merge key value on this element; fall back to positional.
						currentPath := path + "." + strconv.Itoa(modifiedIndex)
						if modifiedIndex >= len(previousArrayChildren) {
							pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
								MutationType: api.MutationTypeAdd,
								Index:        functionIndex,
								Predicate:    true,
								Value:        modifiedChild.String(),
							}
						} else if !previousMatched[modifiedIndex] {
							previousMatched[modifiedIndex] = true
							stack = append(stack, traversalItem{
								path:        currentPath,
								previousDoc: previousArrayChildren[modifiedIndex],
								modifiedDoc: modifiedChild,
							})
						}
						continue
					}

					keyValue := MergeKeyIdentity(modifiedKeyValues)
					prev, found := previousByKey[keyValue]
					if found {
						// Matched by merge key. Use ?key=value;@index syntax with the
						// modified index for positional context.
						currentPath := path + "." + AssociativePathSegment(mergeKeys, modifiedKeyValues, modifiedIndex)
						previousMatched[prev.index] = true
						stack = append(stack, traversalItem{
							path:        currentPath,
							previousDoc: prev.doc,
							modifiedDoc: modifiedChild,
						})
					} else {
						// Defer: might be a rename. The rename-detection pass below
						// pairs each unmatched modified element against unmatched
						// previous elements via similarity; truly-unmatched ones
						// fall through to Add emission afterward.
						pendingAdds = append(pendingAdds, pendingAddEntry{
							modifiedIndex: modifiedIndex,
							modifiedChild: modifiedChild,
							keyValue:      keyValue,
						})
					}
				}

				// Rename-detection pass. For each pending Add, find the unmatched
				// previous element that yields the smallest sub-diff. If the
				// similarity score (sub-diff path count / modified element line
				// count) is below the threshold, treat the pair as a rename:
				//   - emit child path mutations under the PREVIOUS merge-key value
				//     (so SubtractMutations aligns source's paths with target's
				//     paths, which still use the previous key);
				//   - record the rename in arrayElementAliases so PatchMutations
				//     can rewrite the element's merge-key field at apply time;
				//   - mark the previous element as matched so it won't emit a
				//     Delete in the unmatched-previous loop below.
				for _, pa := range pendingAdds {
					bestPrevIdx := -1
					bestPrevDiff := math.MaxInt
					var bestPrevPathMap api.MutationMap
					var bestPrevArrayOrders api.ArrayOrderMap
					var bestPrevAliases api.ArrayElementAliasMap
					var bestPrevKeyValue string
					for prevIdx := range previousArrayChildren {
						if previousMatched[prevIdx] {
							continue
						}
						prevChild := previousArrayChildren[prevIdx]
						prevKeyValues, hasPrevKeyValues := MergeKeyValues(prevChild, mergeKeys)
						if !hasPrevKeyValues {
							continue
						}
						prevKeyValue := MergeKeyIdentity(prevKeyValues)
						tmpPathMap := api.MutationMap{}
						tmpArrayOrders := api.ArrayOrderMap{}
						tmpAliases := api.ArrayElementAliasMap{}
						subPath := path + "." + AssociativePathSegment(mergeKeys, prevKeyValues, pa.modifiedIndex)
						ComputeMutationsForDocs(subPath, prevChild, pa.modifiedChild, functionIndex, tmpPathMap, mergeKeyLookup, tmpArrayOrders, tmpAliases)
						// Cost is the leaf-value count of the sub-diff so a
						// mutation whose Value is a whole subtree (e.g., an
						// Add/Delete of a container or env-var block) is
						// counted at full weight rather than as one entry.
						cost := mutationMapCost(tmpPathMap)
						if cost < bestPrevDiff {
							bestPrevIdx = prevIdx
							bestPrevDiff = cost
							bestPrevPathMap = tmpPathMap
							bestPrevArrayOrders = tmpArrayOrders
							bestPrevAliases = tmpAliases
							bestPrevKeyValue = prevKeyValue
						}
					}

					accepted := false
					var mergeKeyFieldPath api.ResolvedPath
					if bestPrevIdx >= 0 {
						modLines := strings.Count(pa.modifiedChild.String(), "\n")
						score := float64(bestPrevDiff)
						if modLines > 0 {
							score = float64(bestPrevDiff) / float64(modLines)
						}
						// Tighter than the resource-level threshold (1.0): a
						// rename is "the merge key changed and most everything
						// else is the same". 0.3 lets a pure rename plus a
						// handful of correlated field changes (e.g., args on
						// an init container) pair, but rejects a similar-
						// shaped new element coincidentally landing alongside
						// an unrelated removal.
						const renameScoreThreshold = 0.3
						// Sanity check: the sub-diff must include the merge-
						// key field change itself. If it doesn't, this isn't
						// a rename at all (the merge keys differ but the diff
						// somehow elided the .name field) and we shouldn't
						// pair.
						// With more than one merge key, a rename is a change to any of
						// them, so look for the first key field the sub-diff touched.
						elementPath := path + "." + AssociativePathSegment(
							mergeKeys, splitMergeKeyIdentity(bestPrevKeyValue, mergeKeys), pa.modifiedIndex)
						hasMergeKeyChange := false
						for _, key := range mergeKeys {
							candidate := api.ResolvedPath(elementPath + "." + EscapeDotsInPathSegment(key))
							if _, found := bestPrevPathMap[candidate]; found {
								mergeKeyFieldPath = candidate
								hasMergeKeyChange = true
								break
							}
						}
						if hasMergeKeyChange && score < renameScoreThreshold {
							accepted = true
						}
					}

					if !accepted {
						currentPath := path + "." + AssociativePathSegment(
							mergeKeys, splitMergeKeyIdentity(pa.keyValue, mergeKeys), pa.modifiedIndex)
						pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
							MutationType: api.MutationTypeAdd,
							Index:        functionIndex,
							Predicate:    true,
							Value:        pa.modifiedChild.String(),
						}
						continue
					}

					previousMatched[bestPrevIdx] = true
					// Drop the merge-key field's path Update from the sub-diff:
					// the rename is applied via the ArrayElementAliases rename
					// pass at the end of applyPathMutations. Leaving an
					// explicit ?name=db-init;@N.name -> "db-init-v2" path
					// mutation in place would change the element's merge-key
					// value mid-loop and break subsequent child-path
					// resolution that still uses the previous key. (The path
					// was computed above as part of the rename guard.)
					delete(bestPrevPathMap, mergeKeyFieldPath)
					for p, m := range bestPrevPathMap {
						pathMutationMap[p] = m
					}
					if arrayOrders != nil {
						for p, o := range bestPrevArrayOrders {
							arrayOrders[p] = o
						}
					}
					if arrayElementAliases != nil {
						for p, a := range bestPrevAliases {
							if arrayElementAliases[p] == nil {
								arrayElementAliases[p] = make(map[string]string)
							}
							for k, v := range a {
								arrayElementAliases[p][k] = v
							}
						}
						ap := api.ResolvedPath(path)
						if arrayElementAliases[ap] == nil {
							arrayElementAliases[ap] = make(map[string]string)
						}
						arrayElementAliases[ap][bestPrevKeyValue] = pa.keyValue
					}
				}

				// Any unmatched previous elements were deleted.
				for i, child := range previousArrayChildren {
					if previousMatched[i] {
						continue
					}
					var currentPath string
					if keyValues, ok := MergeKeyValues(child, mergeKeys); ok {
						currentPath = path + "." + AssociativePathSegment(mergeKeys, keyValues, i)
					} else {
						currentPath = path + "." + strconv.Itoa(i)
					}
					pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
						MutationType: api.MutationTypeDelete,
						Index:        functionIndex,
						Predicate:    true,
						Value:        child.String(),
					}
				}

				// Record the modified-side merge-key sequence so PatchMutations
				// can reorder the target array after path mutations are applied.
				// Skip when the sequence matches previous: nothing to reorder
				// against the merge base.
				if arrayOrders != nil && len(modifiedKeySeq) > 0 && !slices.Equal(modifiedKeySeq, previousKeySeq) {
					arrayOrders[api.ResolvedPath(path)] = modifiedKeySeq
				}
			} else {
				// Non-associative array: match elements by position, but recognize
				// elements that were removed or inserted rather than diffing index
				// against index blindly. See alignArrayElements.
				alignment := alignArrayElements(previousArrayChildren, modifiedArrayChildren, path, mergeKeyLookup)

				// Matched elements are addressed by their PREVIOUS index. The patch is
				// replayed onto a target that shares the previous configuration's shape,
				// so previous indices are the ones that resolve there, and they line up
				// with the target's own diff against the same base.
				for _, pair := range alignment.pairs {
					stack = append(stack, traversalItem{
						path: path + "." + AnchoredPathSegment(
							previousArrayChildren, pair[0], true),
						previousDoc: previousArrayChildren[pair[0]],
						modifiedDoc: modifiedArrayChildren[pair[1]],
					})
				}

				// Removed elements are likewise addressed by their previous index.
				for _, index := range alignment.deleted {
					currentPath := path + "." + AnchoredPathSegment(previousArrayChildren, index, true)
					pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
						MutationType: api.MutationTypeDelete,
						Index:        functionIndex,
						Predicate:    true,
						Value:        previousArrayChildren[index].String(), // previous data
					}
				}

				// Inserted elements are addressed by their MODIFIED index: the position
				// the element occupies once the patch has been applied. PatchMutations
				// applies array insertions after the removals, in ascending index order,
				// so each lands where it belongs. An index past the end of the array is
				// an append, which is what a purely positional diff produced for every
				// addition.
				for _, index := range alignment.added {
					currentPath := path + "." + AnchoredPathSegment(modifiedArrayChildren, index, false)
					pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
						MutationType: api.MutationTypeAdd,
						Index:        functionIndex,
						Predicate:    true,
						Value:        modifiedArrayChildren[index].String(), // new data
					}
				}
			}
		} else {
			// modifiedDoc must be a value. Compare the contents.
			if modifiedDoc.String() != previousDoc.String() {
				mutation := api.MutationInfo{
					MutationType: api.MutationTypeUpdate,
					Index:        functionIndex,
					Predicate:    true,
					Value:        modifiedDoc.String(), // new data
				}
				// For string values that may contain structured data or multiple
				// lines, compute a patch so that PatchMutations can apply the
				// change to a modified target (three-way merge) rather than
				// wholesale replacement. Tries JSON and YAML structural diff
				// first (for embedded structured data), then falls back to
				// line-level text diff for multi-line strings.
				// Use Data() to get the actual string values (with real newlines),
				// not String() which returns the YAML serialization (escaped newlines).
				if prevStr, ok := previousDoc.Data().(string); ok {
					if modStr, ok := modifiedDoc.Data().(string); ok {
						if IsPatchableString(prevStr) || IsPatchableString(modStr) {
							mutation.Patch = ComputeScalarPatch(prevStr, modStr)
						}
					}
				}
				pathMutationMap[api.ResolvedPath(path)] = mutation
			}
		}
	}
}

const (
	// maxRenameMatchScore is the renameMatchScore at or above which two resources with
	// different names are no longer considered the same resource. renameMatchScore is the
	// fraction of the maximum possible difference between the two, so 0.5 means a rename
	// candidate has to have more in common with its counterpart than not. Above that,
	// recording the change as a Delete plus an Add says the same thing more honestly.
	maxRenameMatchScore = 0.5

	// maxRenameCandidatePairs bounds the rename search in ComputeMutations. Scoring a
	// candidate pairing costs a full path-level diff, so the search is quadratic in the
	// number of resources left unmatched by name. Past this many pairs we stop looking
	// for renames rather than let the search dominate.
	maxRenameCandidatePairs = 2500
)

// resourcePairDiff is the path-level diff of one pairing of a previous resource with a
// modified resource.
type resourcePairDiff struct {
	pathMutationMap     api.MutationMap
	arrayOrders         api.ArrayOrderMap
	arrayElementAliases api.ArrayElementAliasMap
}

// renameCandidate is a scored pairing of an unmatched modified resource with an unmatched
// previous resource, considered as a rename of the same resource.
type renameCandidate struct {
	modifiedDocIndex int
	previousDocIndex int
	score            float64
}

// diffResourcePair computes the path-level diff between a previous and a modified document
// of the same resource, using the merge keys declared for the modified resource's type.
func diffResourcePair(previousDoc, modifiedDoc *gaby.YamlDoc, modifiedResourceType api.ResourceType,
	functionIndex int64, resourceProvider ResourceProvider) resourcePairDiff {
	diff := resourcePairDiff{
		pathMutationMap:     api.MutationMap{},
		arrayOrders:         api.ArrayOrderMap{},
		arrayElementAliases: api.ArrayElementAliasMap{},
	}
	mergeKeyLookup := MergeKeyLookup(func(path string) ([]string, bool) {
		return resourceProvider.MergeKeysForPath(modifiedResourceType, path)
	})
	ComputeMutationsForDocs("", previousDoc, modifiedDoc, functionIndex,
		diff.pathMutationMap, mergeKeyLookup, diff.arrayOrders, diff.arrayElementAliases)
	return diff
}

// renameMatchScore normalizes the cost of pairing two resources by the largest cost that
// pairing could have had — every leaf value on the previous side removed and every leaf
// value on the modified side added. Both the numerator (mutationMapCost) and the
// denominator count leaf values, so the result is the fraction of the two resources that
// differs: 0 for identical content, and 1 for two resources with nothing whatsoever in
// common. Normalizing per pair rather than per unit is what keeps the threshold meaningful
// as a unit grows; the cost of a pairing says nothing on its own, since a large resource
// with a small edit and a small resource replaced outright can cost the same.
func renameMatchScore(cost int, previousDoc, modifiedDoc *gaby.YamlDoc) float64 {
	maxCost := countLeafNodes(previousDoc.YNode()) + countLeafNodes(modifiedDoc.YNode())
	if maxCost <= 0 {
		maxCost = 1
	}
	return float64(cost) / float64(maxCost)
}

// ComputeMutations performs a kind of diff between two configuration Units where it determines what
// modifications were made at the resource/element level and at the path level. They are recorded in a
// way that can be accumulated and updated over subsequent edits and transformations.
func ComputeMutations(previousParsedData, modifiedParsedData gaby.Container, functionIndex int64, resourceProvider ResourceProvider) (api.ResourceMutationList, error) {
	// There are limits in how accurately we can determine the correspondence between resources/elements
	// across revisions. Once resources/elements change too significantly, they will be determined to be
	// distinct. Some properties, such as the ResourceCategory, ResourceType, and ResourceName, carry more
	// significance than other attributes. Also, presence of paths (keys) should carry more weight than values.
	// Line diffs use surrounding lines for context to identify matches, which sometimes works well,
	// but also can be fragile, such as in the case of insertions of partially similar blocks, or minor
	// changes in syntax, such as presence or absence of trailing commas.
	// Since we don't expect a vast number of resources/elements per unit, an algorithm that is quadratic in
	// numbers of resources/elements, such as using Jaccard Similarity or Levenshtein Distance, is acceptable.
	// As opposed to some kind of higher-dimensional vector distance using embeddings.
	// https://www.geeksforgeeks.org/jaccard-similarity/ -- intersection size divided by union size
	// https://www.geeksforgeeks.org/introduction-to-levenshtein-distance/ -- number of edits
	// We use ComputeMutationsForDocs to measure the distance between a candidate pair, normalized by the
	// size of the larger of the two resources (renameMatchScore).
	// Of course, we should optimize for the common case that resources are modified in their same positions
	// and are not renamed nor have types changed.
	// I decided not to impose a canonical order based on resource name because it would cause resources to
	// move when they are renamed, such as during cloning.
	//
	// Matching runs in two passes over the whole resource lists, rather than picking a match for each
	// modified resource in isolation:
	//
	//  1. Exact matches, by full name or by name without scope. Doing all of these first means a fuzzy
	//     match can never claim a resource that some other modified resource matches by name.
	//  2. Rename matching over whatever is left, best pair first: every remaining candidate pairing is
	//     scored, the scores are sorted, and pairs are accepted in that order, each resource being
	//     claimed at most once. This is the same shape as git's rename detection, and it is why the
	//     search is not a first-fit scan: with first-fit, the first modified resource to be considered
	//     takes the best previous resource it can find even when a later one is a far better match for
	//     it.
	//
	// A resource that ends up unmatched on the modified side is an Add; on the previous side, a Delete.

	previousInfos := make([]*api.ResourceInfo, len(previousParsedData))
	for previousDocIndex := range previousParsedData {
		info, err := GetResourceInfo(previousParsedData[previousDocIndex], resourceProvider)
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("error in previous resource/element %d", previousDocIndex))
		}
		previousInfos[previousDocIndex] = info
	}
	modifiedInfos := make([]*api.ResourceInfo, len(modifiedParsedData))
	for modifiedDocIndex := range modifiedParsedData {
		info, err := GetResourceInfo(modifiedParsedData[modifiedDocIndex], resourceProvider)
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("error in modified resource/element %d", modifiedDocIndex))
		}
		modifiedInfos[modifiedDocIndex] = info
	}

	// matchedPrevious[modifiedDocIndex] is the previous doc it was paired with, or -1.
	matchedPrevious := make([]int, len(modifiedParsedData))
	for modifiedDocIndex := range matchedPrevious {
		matchedPrevious[modifiedDocIndex] = -1
	}
	previousMatched := make([]bool, len(previousParsedData))

	// Category must be identical and type must be similar for any pairing.
	pairIsCompatible := func(previousDocIndex, modifiedDocIndex int) bool {
		if previousInfos[previousDocIndex].ResourceCategory != modifiedInfos[modifiedDocIndex].ResourceCategory {
			return false
		}
		return resourceProvider.ResourceTypesAreSimilar(
			previousInfos[previousDocIndex].ResourceType, modifiedInfos[modifiedDocIndex].ResourceType)
	}

	// Pass 1: exact name matches.
	for modifiedDocIndex := range modifiedParsedData {
		modifiedInfo := modifiedInfos[modifiedDocIndex]
		for previousDocIndex := range previousParsedData {
			if previousMatched[previousDocIndex] || !pairIsCompatible(previousDocIndex, modifiedDocIndex) {
				continue
			}
			previousInfo := previousInfos[previousDocIndex]
			nameMatches := previousInfo.ResourceName == modifiedInfo.ResourceName
			// Only compare unscoped names when both are populated: an empty unscoped name
			// on both sides says nothing about whether they are the same resource.
			if !nameMatches && previousInfo.ResourceNameWithoutScope != "" {
				nameMatches = previousInfo.ResourceNameWithoutScope == modifiedInfo.ResourceNameWithoutScope
			}
			if !nameMatches {
				continue
			}
			matchedPrevious[modifiedDocIndex] = previousDocIndex
			previousMatched[previousDocIndex] = true
			break
		}
	}

	// Pass 2: rename matching over the leftovers, best pair first.
	var unmatchedModified, unmatchedPrevious []int
	for modifiedDocIndex := range modifiedParsedData {
		if matchedPrevious[modifiedDocIndex] < 0 {
			unmatchedModified = append(unmatchedModified, modifiedDocIndex)
		}
	}
	for previousDocIndex := range previousParsedData {
		if !previousMatched[previousDocIndex] {
			unmatchedPrevious = append(unmatchedPrevious, previousDocIndex)
		}
	}
	if len(unmatchedModified) > 0 && len(unmatchedPrevious) > 0 {
		if len(unmatchedModified)*len(unmatchedPrevious) > maxRenameCandidatePairs {
			// Pathological input. Diffing every pairing would dominate the cost of the whole
			// operation, so treat the leftovers as Adds and Deletes rather than renames.
			slog.Debug("skipping resource rename matching: too many candidate pairs",
				"modified", len(unmatchedModified), "previous", len(unmatchedPrevious))
		} else {
			var candidates []renameCandidate
			for _, modifiedDocIndex := range unmatchedModified {
				for _, previousDocIndex := range unmatchedPrevious {
					if !pairIsCompatible(previousDocIndex, modifiedDocIndex) {
						continue
					}
					diff := diffResourcePair(previousParsedData[previousDocIndex], modifiedParsedData[modifiedDocIndex],
						modifiedInfos[modifiedDocIndex].ResourceType, functionIndex, resourceProvider)
					score := renameMatchScore(mutationMapCost(diff.pathMutationMap),
						previousParsedData[previousDocIndex], modifiedParsedData[modifiedDocIndex])
					if score >= maxRenameMatchScore {
						continue
					}
					candidates = append(candidates, renameCandidate{
						modifiedDocIndex: modifiedDocIndex,
						previousDocIndex: previousDocIndex,
						score:            score,
					})
				}
			}
			slices.SortFunc(candidates, func(a, b renameCandidate) int {
				if a.score != b.score {
					if a.score < b.score {
						return -1
					}
					return 1
				}
				if a.modifiedDocIndex != b.modifiedDocIndex {
					return a.modifiedDocIndex - b.modifiedDocIndex
				}
				return a.previousDocIndex - b.previousDocIndex
			})
			for _, candidate := range candidates {
				if matchedPrevious[candidate.modifiedDocIndex] >= 0 || previousMatched[candidate.previousDocIndex] {
					continue
				}
				matchedPrevious[candidate.modifiedDocIndex] = candidate.previousDocIndex
				previousMatched[candidate.previousDocIndex] = true
			}
		}
	}

	// Emit one mutation per modified resource, in modified order.
	mutations := api.ResourceMutationList{}
	for modifiedDocIndex := range modifiedParsedData {
		modifiedInfo := modifiedInfos[modifiedDocIndex]
		previousDocIndex := matchedPrevious[modifiedDocIndex]

		// Unmatched resources are Adds. During Create, including cloning, the previous
		// data should be empty.
		if previousDocIndex < 0 {
			mutations = append(mutations, api.ResourceMutation{
				Resource: api.ResourceInfo{
					ResourceType:             modifiedInfo.ResourceType,
					ResourceName:             modifiedInfo.ResourceName,
					ResourceNameWithoutScope: modifiedInfo.ResourceNameWithoutScope,
					ResourceCategory:         modifiedInfo.ResourceCategory,
				},
				ResourceMutationInfo: api.MutationInfo{
					MutationType: api.MutationTypeAdd,
					Index:        functionIndex,
					Predicate:    true,
					Value:        modifiedParsedData[modifiedDocIndex].String(), // new data
				},
				PathMutationMap: make(api.MutationMap),
				Aliases: map[api.ResourceName]struct{}{
					modifiedInfo.ResourceName: {},
				},
				AliasesWithoutScopes: map[api.ResourceName]struct{}{
					modifiedInfo.ResourceNameWithoutScope: {},
				},
			})
			continue
		}

		// Matched resource - record Update or None mutation. The pair's diff is recomputed
		// here rather than carried out of the matching passes, so that only the accepted
		// pairings are held rather than every candidate pairing.
		previousInfo := previousInfos[previousDocIndex]
		diff := diffResourcePair(previousParsedData[previousDocIndex], modifiedParsedData[modifiedDocIndex],
			modifiedInfo.ResourceType, functionIndex, resourceProvider)

		// Alias Tracking - record both old and new names
		mutation := api.ResourceMutation{
			Resource: api.ResourceInfo{
				ResourceType:             modifiedInfo.ResourceType,
				ResourceName:             modifiedInfo.ResourceName,
				ResourceNameWithoutScope: modifiedInfo.ResourceNameWithoutScope,
				ResourceCategory:         modifiedInfo.ResourceCategory,
			},
			ResourceMutationInfo: api.MutationInfo{
				MutationType: api.MutationTypeUpdate, // assume changed
				Index:        functionIndex,
				Predicate:    true,
				// no Value at this level
			},
			PathMutationMap:     diff.pathMutationMap,
			ArrayOrders:         diff.arrayOrders,
			ArrayElementAliases: diff.arrayElementAliases,
			Aliases: map[api.ResourceName]struct{}{
				previousInfo.ResourceName: {},
				modifiedInfo.ResourceName: {},
			},
			AliasesWithoutScopes: map[api.ResourceName]struct{}{
				previousInfo.ResourceNameWithoutScope: {},
				modifiedInfo.ResourceNameWithoutScope: {},
			},
		}
		if len(diff.arrayOrders) == 0 {
			mutation.ArrayOrders = nil
		}
		if len(diff.arrayElementAliases) == 0 {
			mutation.ArrayElementAliases = nil
		}
		if len(diff.pathMutationMap) == 0 && len(diff.arrayOrders) == 0 && len(diff.arrayElementAliases) == 0 {
			mutation.ResourceMutationInfo.MutationType = api.MutationTypeNone
		}
		mutations = append(mutations, mutation)
	}

	// Unmatched previous resources are Deletes.
	for previousDocIndex := range previousParsedData {
		if previousMatched[previousDocIndex] {
			continue
		}
		previousInfo := previousInfos[previousDocIndex]
		mutations = append(mutations, api.ResourceMutation{
			Resource: api.ResourceInfo{
				ResourceType:             previousInfo.ResourceType,
				ResourceName:             previousInfo.ResourceName,
				ResourceNameWithoutScope: previousInfo.ResourceNameWithoutScope,
				ResourceCategory:         previousInfo.ResourceCategory,
			},
			ResourceMutationInfo: api.MutationInfo{
				MutationType: api.MutationTypeDelete,
				Index:        functionIndex,
				Predicate:    true,
				Value:        previousParsedData[previousDocIndex].String(), // previous data
			},
			PathMutationMap: make(api.MutationMap),
			Aliases: map[api.ResourceName]struct{}{
				previousInfo.ResourceName: {},
			},
			AliasesWithoutScopes: map[api.ResourceName]struct{}{
				previousInfo.ResourceNameWithoutScope: {},
			},
		})
	}

	return mutations, nil
}

// PatchMutations applies a set of mutations to configuration data, effectively "replaying"
// recorded changes onto a YAML document. It's the inverse of ComputeMutations: whereas
// ComputeMutations determines what changed, PatchMutations applies the recorded changes.
//
// In typical usage mutationsPatch is the diff produced by ComputeMutations against a
// different (or earlier) version of the same configuration — e.g., the diff between an
// upstream Unit's old and new revisions, applied to a downstream Unit. Because of that,
// mutationsPatch may reference resource names, alias names, or paths that don't match
// parsedData verbatim; PatchMutations does its own resource lookup (with alias fallback)
// and path resolution.
//
// Three-way merge: pass mutationsToSubtract to subtract another mutation set (typically
// the diff between the merge base and the current target) from mutationsPatch first.
// This is how target-side changes are preserved against the upstream patch (see
// SubtractMutations). Pass nil (or an empty list) to skip subtraction.
//
// Predicates: mutationsPredicates is the accumulated MutationSources of the data being
// patched (see AddMutations). When a Predicate is false at the resource or any ancestor
// path, that part of mutationsPatch is filtered out. Default Predicate=true means all
// changes are eligible. mutationsPredicates may be nil.
//
// Algorithm:
//
//  1. Resource matching (per document in parsedData): look up the corresponding patch
//     entry by ResourceTypeAndName, then by predicate aliases (so
//     a renamed resource is still matched to its upstream patch entry).
//
//  2. Resource-level mutation:
//
//     | MutationType     | Action                                           |
//     |------------------|--------------------------------------------------|
//     | Add / Replace    | Replace entire document with the mutation's Value|
//     | Delete           | Set document to nil (filtered on serialization)  |
//     | None             | Skip (no changes)                                |
//     | Update           | Process path-level mutations                     |
//
//  3. Path-level mutation (for Update): sorted by api.SortedMutationMapEntries (numeric
//     segments compared as integers, parents before children), then partitioned so all
//     Deletes run before all non-Deletes. Deletes-first prevents a Delete with positional
//     fallback from clobbering an Add at the same array parent.
//
//     Each path is resolved through ResolveAssociativeSegments, which honors merge keys
//     and only falls back to a positional index when the element at that index has no
//     merge-key field (legacy data) or the index is out of bounds. If the path can't be
//     fully resolved, the operation is skipped — except for Add/Replace, which appends
//     to the parent array (so a new merge-keyed element can be introduced even when its
//     desired index is occupied by a different element). The append-on-clash rule
//     trades position fidelity for data preservation; positional associative arrays
//     such as initContainers will require an additional reorder pass to fully restore
//     source-side ordering.
//
//     Predicate filtering: a path or any ancestor with Predicate=false in
//     mutationsPredicates causes the path to be skipped.
//
//     Apply by type:
//
//     | MutationType     | Action                                                |
//     |------------------|-------------------------------------------------------|
//     | Add / Replace    | Set value at the resolved path (overwrites)           |
//     | Update (scalar)  | If MutationInfo.Patch is set, three-way text merge.   |
//     |                  | Otherwise replace (preserving YAML comments).         |
//     | Update (complex) | Recursive merge with the existing value (preserves    |
//     |                  | nested comments and unset fields).                    |
//     | Delete           | Remove the path from document (no-op if missing)      |
//
//  4. After visiting all existing documents, any unmatched Add/Replace patch entries are
//     parsed and appended as new documents to parsedData. Their per-path mutations are
//     applied to the new document (no subtraction, no predicate filtering).
//
// Errors are accumulated and joined; PatchMutations does its best to apply every patch
// it can rather than aborting on the first problem.
//
// PatchMutations also returns a MutationConflictList recording every part of the patch
// that was not applied: SubtractMutations conflicts (forwarded from the subtract step),
// PredicateFiltered (resource-level and path-level), and UnresolvedPath (when an
// associative segment couldn't be matched against the target). The conflicts are
// advisory — the returned data already reflects the drops.
func PatchMutations(parsedData gaby.Container, mutationsPredicates, mutationsPatch, mutationsToSubtract api.ResourceMutationList, resourceProvider ResourceProvider, options *api.FunctionOptions) (gaby.Container, api.MutationConflictList, error) {
	var conflicts api.MutationConflictList
	if len(mutationsToSubtract) > 0 {
		var subtractConflicts api.MutationConflictList
		mutationsPatch, subtractConflicts = SubtractMutations(mutationsPatch, mutationsToSubtract)
		conflicts = append(conflicts, subtractConflicts...)
	}
	// Build predicate index with prefer-predicate dedup: when multiple mutation sources
	// exist for the same resource (e.g., one from clone and one from triggers), prefer
	// the one with Predicate=true so the resource is not incorrectly filtered out.
	predicateIdx := api.NewResourceMutationIndex(mutationsPredicates)
	for i := range mutationsPredicates {
		resourceInfo := mutationsPredicates[i].Resource
		if resourceInfo.ResourceNameWithoutScope == "" {
			resourceInfo.ResourceNameWithoutScope = resourceProvider.RemoveScopeFromResourceName(resourceInfo.ResourceName)
		}
		key := api.ResourceTypeAndNameFromResourceInfo(resourceInfo)
		if existingIdx, exists := predicateIdx.NameMap[key]; exists {
			if mutationsPredicates[existingIdx].ResourceMutationInfo.Predicate &&
				!mutationsPredicates[i].ResourceMutationInfo.Predicate {
				continue
			}
		}
		predicateIdx.NameMap[key] = i
	}

	patchIdx := api.NewResourceMutationIndex(mutationsPatch)
	// The subtrahend is the target's own diff. Subtraction has already removed the patch
	// entries it overlaps; what is still wanted from it is the record of which paths the
	// target claimed, which is how a merge with subtraction on expresses the ownership a
	// merge without it expresses as a stored Predicate.
	subtractIdx := api.NewResourceMutationIndex(mutationsToSubtract)

	// Track which patch mutations were matched to existing documents.
	// Unmatched Add/Replace mutations need to be appended as new documents.
	matchedPatchIndices := make(map[int]bool)

	var errs []error

	visitor := func(doc *gaby.YamlDoc, _ any, docIndex int, docResourceInfo *api.ResourceInfo) (any, []error) {
		var visitorErrs []error

		// Find predicate for this document
		mutationPredicateIndex, hasPredicate := predicateIdx.Find(*docResourceInfo, nil)

		// Find patch for this document, using predicate aliases as additional aliases
		var predicateAliases map[api.ResourceName]struct{}
		if hasPredicate {
			predicateAliases = mutationsPredicates[mutationPredicateIndex].AliasesWithoutScopes
		}
		mutationPatchIndex, ok := patchIdx.Find(*docResourceInfo, predicateAliases)
		if !ok {
			return nil, nil
		}

		// Filter the patch at the resource level.
		if hasPredicate && !mutationsPredicates[mutationPredicateIndex].ResourceMutationInfo.Predicate {
			slog.Info("patch filtered", "resource", api.ResourceTypeAndNameFromResourceInfo(*docResourceInfo))
			predicateMutInfo := mutationsPredicates[mutationPredicateIndex].ResourceMutationInfo
			conflicts = append(conflicts, api.MutationConflict{
				Reason:   api.ConflictReasonPredicateFiltered,
				Resource: mutationsPatch[mutationPatchIndex].Resource,
				Source:   mutationsPatch[mutationPatchIndex].ResourceMutationInfo,
				Target:   &predicateMutInfo,
			})
			matchedPatchIndices[mutationPatchIndex] = true
			return nil, nil
		}

		matchedPatchIndices[mutationPatchIndex] = true
		resourcePatchMutation := &mutationsPatch[mutationPatchIndex].ResourceMutationInfo
		switch resourcePatchMutation.MutationType {
		case api.MutationTypeAdd, api.MutationTypeReplace:
			// Replace at the resource level means there was a delete then an add, so
			// treat it like add.
			valueString := resourcePatchMutation.Value
			valueDoc, err := gaby.ParseYAML([]byte(valueString))
			if err != nil {
				visitorErrs = append(visitorErrs, fmt.Errorf("error parsing value for resource %s: %w",
					api.ResourceTypeAndNameFromResourceInfo(*docResourceInfo), err))
			}
			parsedData[docIndex] = valueDoc
			// Some paths also could have been modified
		case api.MutationTypeDelete:
			// Mark the document as deleted by setting it to nil
			// The document will be filtered out when serializing the result
			parsedData[docIndex] = nil
			// Shouldn't be any modified paths
			return nil, visitorErrs
		case api.MutationTypeNone:
			// None at the resource level means the resource wasn't modified.
			return nil, nil
		case api.MutationTypeUpdate:
			// Update at the resource level means some paths were modified.
		}

		var pathConflicts api.MutationConflictList
		mergeKeyLookup := MergeKeyLookup(func(path string) ([]string, bool) {
			return resourceProvider.MergeKeysForPath(docResourceInfo.ResourceType, path)
		})
		exclusiveLookup := ExclusiveFieldsLookup(func(path string) (ExclusiveFields, bool) {
			return resourceProvider.ExclusiveFieldsForPath(docResourceInfo.ResourceType, path)
		})
		var subtractedPaths api.MutationMap
		if subtractIndex, ok := subtractIdx.Find(*docResourceInfo, predicateAliases); ok {
			subtractedPaths = mutationsToSubtract[subtractIndex].PathMutationMap
		}
		visitorErrs, pathConflicts = applyPathMutations(doc, mutationsPatch[mutationPatchIndex].PathMutationMap,
			hasPredicate, mutationsPredicates, mutationPredicateIndex, mutationsPatch[mutationPatchIndex].Resource,
			mutationsPatch[mutationPatchIndex].ArrayOrders, mutationsPatch[mutationPatchIndex].ArrayElementAliases,
			mergeKeyLookup, exclusiveLookup, subtractedPaths,
			visitorErrs)
		conflicts = append(conflicts, pathConflicts...)
		return nil, visitorErrs
	}

	_, visitErr := VisitResourcesFiltered(parsedData, nil, resourceProvider, options, visitor)
	if visitErr != nil {
		errs = append(errs, visitErr)
	}

	// Resources of the patch that matched no document in the target. An Add or a Replace
	// introduces one; anything else has nowhere to land.
	for i := range mutationsPatch {
		if matchedPatchIndices[i] {
			continue
		}
		resourcePatchMutation := &mutationsPatch[i].ResourceMutationInfo

		// A resource the target deleted is a local override like any other: the deletion
		// is recorded in the accumulated mutations with a Predicate, and Predicate=false
		// means the target removed it on purpose. Consult that here as the matched branch
		// does, or an upgrade re-adds what the target deliberately deleted.
		predicateReason := api.ConflictReasonUnresolvedPath
		var predicateMutInfo *api.MutationInfo
		if len(mutationsPredicates) > 0 {
			if predicateIndex, found := predicateIdx.Find(mutationsPatch[i].Resource,
				mutationsPatch[i].AliasesWithoutScopes); found &&
				!mutationsPredicates[predicateIndex].ResourceMutationInfo.Predicate {
				info := mutationsPredicates[predicateIndex].ResourceMutationInfo
				predicateMutInfo = &info
				predicateReason = api.ConflictReasonPredicateFiltered
			}
		}

		switch resourcePatchMutation.MutationType {
		case api.MutationTypeNone:
			continue
		case api.MutationTypeDelete:
			// The target does not have the resource the patch removes. Nothing to do,
			// and nothing withheld: both sides agree it is gone.
			continue
		case api.MutationTypeUpdate:
			// The patch changes a resource the target does not have — deleted
			// downstream, or never cloned. The changes have nowhere to land, and
			// dropping them silently loses a whole resource's worth of upstream work.
			// Report the resource and each path it carried.
			conflicts = append(conflicts, api.MutationConflict{
				Reason:   predicateReason,
				Resource: mutationsPatch[i].Resource,
				Source:   *resourcePatchMutation,
				Target:   predicateMutInfo,
			})
			for _, entry := range api.SortedMutationMapEntries(mutationsPatch[i].PathMutationMap) {
				conflicts = append(conflicts, api.MutationConflict{
					Reason:   predicateReason,
					Resource: mutationsPatch[i].Resource,
					Path:     entry.Path,
					Source:   *entry.MutationInfo,
					Target:   predicateMutInfo,
				})
			}
			continue
		case api.MutationTypeAdd, api.MutationTypeReplace:
			if predicateMutInfo != nil {
				// The target deleted this resource and marked the deletion protected.
				slog.Info("patch filtered", "resource",
					api.ResourceTypeAndNameFromResourceInfo(mutationsPatch[i].Resource))
				conflicts = append(conflicts, api.MutationConflict{
					Reason:   api.ConflictReasonPredicateFiltered,
					Resource: mutationsPatch[i].Resource,
					Source:   *resourcePatchMutation,
					Target:   predicateMutInfo,
				})
				continue
			}
			valueString := resourcePatchMutation.Value
			valueDoc, err := gaby.ParseYAML([]byte(valueString))
			if err != nil {
				errs = append(errs, fmt.Errorf("error parsing value for unmatched resource %s: %w",
					api.ResourceTypeAndNameFromResourceInfo(mutationsPatch[i].Resource), err))
				continue
			}
			var pathConflicts api.MutationConflictList
			mergeKeyLookup := MergeKeyLookup(func(path string) ([]string, bool) {
				return resourceProvider.MergeKeysForPath(mutationsPatch[i].Resource.ResourceType, path)
			})
			exclusiveLookup := ExclusiveFieldsLookup(func(path string) (ExclusiveFields, bool) {
				return resourceProvider.ExclusiveFieldsForPath(mutationsPatch[i].Resource.ResourceType, path)
			})
			errs, pathConflicts = applyPathMutations(valueDoc, mutationsPatch[i].PathMutationMap,
				false, nil, 0, mutationsPatch[i].Resource,
				mutationsPatch[i].ArrayOrders, mutationsPatch[i].ArrayElementAliases,
				mergeKeyLookup, exclusiveLookup, nil,
				errs)
			conflicts = append(conflicts, pathConflicts...)
			parsedData = append(parsedData, valueDoc)
		}
	}

	// Drop documents deleted above. A resource-level Delete marks the slot
	// nil (see the visitor) rather than removing it, relying on
	// Container.String() to omit nils on serialization. But PatchMutations
	// also hands this container to callers that iterate it directly without
	// re-serializing — notably the compute-mutations pass the function
	// handler runs after every mutating function. ComputeMutations calls
	// GetResourceInfo on each element, which fails on a nil doc
	// ("apiVersion not found"), so a patch that deletes a resource (e.g.
	// emptying a single-resource Unit via merge) would otherwise blow up the
	// whole update. Compact the nils away so the returned container matches
	// what String() would produce.
	compacted := parsedData[:0]
	for _, doc := range parsedData {
		if doc == nil {
			continue
		}
		compacted = append(compacted, doc)
	}
	parsedData = compacted

	return parsedData, conflicts, errors.Join(errs...)
}

// arrayElementOp is a removal or insertion of a whole element of a positional array,
// deferred by applyPathMutations until the rest of the patch has been applied. remove
// distinguishes the two; value carries the element to insert.
type arrayElementOp struct {
	arrayPath string // resolved path of the array itself
	index     int    // element index within that array
	remove    bool
	value     *gaby.YamlDoc    // insertions only
	path      api.ResolvedPath // insertions only: the original element path, for the fallback
}

// arrayElementTarget reports whether resolvedPath addresses a whole element of a positional
// array that already exists in doc — a trailing numeric segment whose parent resolves to a
// sequence. It returns the array's path and the element index. Paths that create a new
// array, or that address a map key, are not array element targets: they are applied in
// place.
//
// unresolvedPath is the path as the patch recorded it. A trailing merge-key segment is not
// an element of a positional array however it resolves: those elements are addressed by
// identity, so their indices don't have to be kept straight, and deferring them would break
// the append fallback in applyPathMutations, which counts on preceding deletes having
// already been applied. A trailing anchor segment is a positional element — the anchor
// names which element it is, but the array still renumbers around it.
func arrayElementTarget(doc *gaby.YamlDoc, resolvedPath string, unresolvedPath api.ResolvedPath) (string, int, bool) {
	unresolvedSegments := gaby.DotPathToSlice(string(unresolvedPath))
	if len(unresolvedSegments) == 0 {
		return "", 0, false
	}
	if last := unresolvedSegments[len(unresolvedSegments)-1]; strings.HasPrefix(last, "?") && !segmentIsAnchor(last) {
		return "", 0, false
	}
	segments := gaby.DotPathToSlice(resolvedPath)
	if len(segments) < 2 {
		return "", 0, false
	}
	index, err := strconv.Atoi(segments[len(segments)-1])
	if err != nil || index < 0 {
		return "", 0, false
	}
	arrayPath := JoinPathSegments(segments[:len(segments)-1])
	arrayDoc := doc.Path(arrayPath)
	if arrayDoc == nil || !arrayDoc.IsArray() {
		return "", 0, false
	}
	return arrayPath, index, true
}

// applyArrayElementOps applies the deferred removals and insertions of whole array
// elements, in the one order in which every recorded index is still valid when it is used:
//
//   - Deeper arrays first. Reshaping an outer array renumbers the elements that contain the
//     inner ones, so the inner arrays have to be done while the outer indices still hold.
//   - Within one array, removals before insertions, removals in descending index order and
//     insertions in ascending index order. Removals carry indices into the array as the
//     patch found it, and removing from the back leaves the lower indices undisturbed;
//     insertions carry indices into the array as it will end up, and by the time each one
//     runs every element before it is already in place.
//
// This is the same order a textual patch applies its hunks in, and it is what lets an
// element removed or inserted upstream land in a downstream copy without disturbing the
// customizations in the elements around it.
func applyArrayElementOps(doc *gaby.YamlDoc, ops []arrayElementOp, errs []error) []error {
	if len(ops) == 0 {
		return errs
	}
	depth := func(op arrayElementOp) int {
		return len(gaby.DotPathToSlice(op.arrayPath))
	}
	slices.SortStableFunc(ops, func(a, b arrayElementOp) int {
		if d := depth(b) - depth(a); d != 0 {
			return d
		}
		if a.arrayPath != b.arrayPath {
			return strings.Compare(a.arrayPath, b.arrayPath)
		}
		if a.remove != b.remove {
			if a.remove {
				return -1
			}
			return 1
		}
		if a.remove {
			return b.index - a.index
		}
		return a.index - b.index
	})
	for _, op := range ops {
		elementPath := op.arrayPath + "." + strconv.Itoa(op.index)
		if op.remove {
			if !doc.ExistsP(elementPath) {
				continue
			}
			if err := doc.DeleteP(elementPath); err != nil {
				errs = append(errs, fmt.Errorf("error deleting path %s: %w", elementPath, err))
			}
			continue
		}
		// Skip an insertion whose element is already sitting at the target index. An
		// insertion carries no identity beyond its position, so replaying a patch that
		// has already been applied — a re-run of the same merge, a retried resolve, an
		// upgrade whose cursor didn't advance — would otherwise add the element a
		// second time. Matching on content is the only identity available here: if the
		// element is already there, the insertion has nothing left to do.
		if existing := doc.Path(elementPath); existing != nil && existing.String() == op.value.String() {
			continue
		}
		if err := doc.ArrayInsertP(op.value.YNode(), op.index, op.arrayPath); err != nil {
			// The index is past the end of the array — the target is shorter than the
			// configuration the patch was computed against. Append instead. Appending
			// rather than writing at the recorded index keeps several insertions in
			// their source-side order, since they are applied front to back, and avoids
			// asking the setter for a gap in the array, which it cannot express.
			appended := false
			if arrayNode := doc.Path(op.arrayPath); arrayNode != nil {
				appended = doc.ArrayInsertP(op.value.YNode(), len(arrayNode.Children()), op.arrayPath) == nil
			}
			if !appended {
				// No array to append to: set the element at the recorded path, which
				// creates the array around it.
				if _, setErr := doc.SetDocExpandP(op.value, string(op.path)); setErr != nil {
					errs = append(errs, fmt.Errorf("error inserting value at path %s: %w", op.path, setErr))
				}
			}
		}
	}
	return errs
}

// exclusiveTouch records that a patch set something inside a union: the path of the object
// holding it, and which member (or the discriminator) it wrote.
//
// The path is kept in both forms. groupPath is resolved against this document, which is what
// reads and edits it; patchGroupPath is the form the patch named it in, which is what the
// target's own mutation records are keyed by and what a conflict has to quote back — the same
// two forms the predicate lookup needs, for the same reason.
type exclusiveTouch struct {
	groupPath      string
	patchGroupPath string
	// member is the union member the patch wrote, or "" when it wrote the discriminator.
	member string
	// patchPath and source are the mutation that wrote it, for the conflict that reports
	// the write as withheld.
	patchPath api.ResolvedPath
	source    api.MutationInfo
}

// findExclusiveTouch reports whether a path writes into a union, and which part of it. Every
// prefix of the path is a candidate for the object holding the union, so a write anywhere
// under a member — `volumes.0.configMap.name`, not just `volumes.0.configMap` — counts as
// setting that member.
//
// patchPath is the path as the patch named it and resolvedPath is the same path resolved
// against this document; resolution maps segment to segment, so a prefix of one is the same
// prefix of the other.
func findExclusiveTouch(patchPath api.ResolvedPath, resolvedPath string, lookup ExclusiveFieldsLookup) (exclusiveTouch, bool) {
	if lookup == nil {
		return exclusiveTouch{}, false
	}
	segments := gaby.DotPathToSlice(resolvedPath)
	patchSegments := gaby.DotPathToSlice(string(patchPath))
	if len(patchSegments) != len(segments) {
		// Resolution changed the shape of the path, so the two forms cannot be lined up.
		// Fall back to the resolved form for both; the ownership lookup below has a
		// resolved-form fallback of its own.
		patchSegments = segments
	}
	// The last segment cannot be the object holding the union: something has to be written
	// inside it for there to be a member.
	for i := 0; i < len(segments)-1; i++ {
		groupPath := strings.Join(segments[:i+1], ".")
		fields, ok := lookup(groupPath)
		if !ok {
			continue
		}
		touch := exclusiveTouch{
			groupPath:      groupPath,
			patchGroupPath: strings.Join(patchSegments[:i+1], "."),
		}
		next := segments[i+1]
		if fields.IsMember(next) {
			touch.member = next
			return touch, true
		}
		if fields.Discriminator != "" && next == fields.Discriminator {
			return touch, true
		}
	}
	return exclusiveTouch{}, false
}

// pathOwnership answers whether the target has claimed a path as its own — a stored
// Predicate=false on it or an ancestor, or, when subtraction is in play, a mutation of its
// own at it or an ancestor. Both forms of the path are consulted for the same reason the
// predicate filter consults both: the target's records name a path the way the diff produced
// it, while the path in hand has been resolved against this document.
type pathOwnership func(canonicalPath, resolvedPath string) bool

// clearExclusiveSiblings settles a union the patch wrote into, so that at most one member is
// left standing.
//
// Setting one member of a union has to clear the others; Kubernetes spells this
// patchStrategy:"retainKeys". A field-level merge does not do it on its own — the addition of
// the new member applies, the removal of the old one is withheld against the target's
// ownership of it or subtracted as one of the target's own differences, and the result is a
// volume with two sources or a Recreate strategy that still carries rollingUpdate. Neither
// will apply.
//
// Which member survives follows the ownership rules the rest of the engine runs on rather
// than overriding them:
//
//   - If the target owns another member — it chose that volume source, and the choice is
//     recorded at the member itself rather than somewhere beneath it — the patch's member is
//     withheld and reported as ExclusiveWithheld. That is an ordinary conflict: the source
//     wanted a change and did not get it, and applying it later performs the switch, because
//     replayed on its own there is no ownership left to withhold it.
//   - Otherwise the patch's member stands and the others are cleared, reported as
//     ExclusiveCleared with the removed value.
//
// Where the union has a discriminator the *document* decides: whatever `type` reads after the
// merge, only the member it permits may remain. Ownership does not enter into it, because
// there is no arrangement that keeps both — and ownership of the discriminator itself has
// already had its say through the ordinary predicate filter, which is what decides whether
// the patch's `type` change applied at all.
func clearExclusiveSiblings(doc *gaby.YamlDoc, touches []exclusiveTouch, resource api.ResourceInfo,
	lookup ExclusiveFieldsLookup, targetOwns pathOwnership) api.MutationConflictList {
	if len(touches) == 0 || lookup == nil {
		return nil
	}
	var conflicts api.MutationConflictList
	// One decision per union, however many paths the patch wrote inside it.
	seen := map[string]struct{}{}
	for _, touch := range touches {
		if _, done := seen[touch.groupPath]; done {
			continue
		}
		seen[touch.groupPath] = struct{}{}

		fields, ok := lookup(touch.groupPath)
		if !ok {
			continue
		}
		group := doc.Path(touch.groupPath)
		if group == nil {
			continue
		}

		// Which member the document is left saying it is.
		keep := touch.member
		withheld := ""
		if fields.Discriminator != "" {
			discriminator := group.S(fields.Discriminator)
			if discriminator == nil {
				// Nothing says which member is valid, so nothing can be ruled out.
				continue
			}
			keep = fields.AllowedMember[fmt.Sprintf("%v", discriminator.Data())]
		} else if owned := ownedMember(group, fields, touch, targetOwns); owned != "" && owned != touch.member {
			// The target chose a different member. Its choice stands, and what the
			// patch wrote is what goes.
			keep, withheld = owned, touch.member
		}

		for _, member := range fields.Members {
			if member == keep {
				continue
			}
			present := group.S(member)
			if present == nil {
				continue
			}
			removed := api.MutationInfo{
				MutationType: api.MutationTypeDelete,
				Index:        touch.source.Index,
				Predicate:    true,
				Value:        present.String(),
			}
			if err := group.Delete(member); err != nil {
				slog.Info("error clearing exclusive field", "path", touch.groupPath,
					"member", member, "error", err)
				continue
			}
			if member == withheld {
				// The patch's own writes were undone, so each of them is reported as
				// the change it is: one the source wanted and did not get. Applying
				// them later performs the switch.
				conflicts = append(conflicts, withheldWriteConflicts(touches, touch.groupPath, member, resource)...)
				continue
			}
			conflicts = append(conflicts, api.MutationConflict{
				Reason:   api.ConflictReasonExclusiveCleared,
				Resource: resource,
				Path:     api.ResolvedPath(touch.patchGroupPath + "." + member),
				Source:   touch.source,
				Target:   &removed,
			})
		}
	}
	return conflicts
}

// withheldWriteConflicts reports every path the patch wrote into a member that was then
// withheld, one conflict per write, so the report says which changes did not land rather
// than only which member did not.
func withheldWriteConflicts(touches []exclusiveTouch, groupPath, member string,
	resource api.ResourceInfo) api.MutationConflictList {
	var conflicts api.MutationConflictList
	for _, touch := range touches {
		if touch.groupPath != groupPath || touch.member != member {
			continue
		}
		conflicts = append(conflicts, api.MutationConflict{
			Reason:   api.ConflictReasonExclusiveWithheld,
			Resource: resource,
			Path:     touch.patchPath,
			Source:   touch.source,
		})
	}
	return conflicts
}

// ownedMember returns the member of a union that the target claimed as its own, or "".
//
// The claim has to be on the member itself, not on something under it. A target that switched
// its volume from a configMap to an emptyDir has a record at the emptyDir; a target that only
// tuned a field inside the member it inherited has one further down, and tuning a knob is not
// choosing the source.
func ownedMember(group *gaby.YamlDoc, fields ExclusiveFields, touch exclusiveTouch,
	targetOwns pathOwnership) string {
	if targetOwns == nil {
		return ""
	}
	for _, member := range fields.Members {
		if member == touch.member || group.S(member) == nil {
			continue
		}
		memberPath := touch.patchGroupPath + "." + member
		if targetOwns(string(CanonicalMutationPath(api.ResolvedPath(memberPath))),
			touch.groupPath+"."+member) {
			return member
		}
	}
	return ""
}

// expandCoarsePatchEntries breaks a patch entry that covers a whole subtree into the leaves
// it actually changes, when the target protects something inside that subtree.
//
// A Predicate is found by walking up from the patch's path to the closest ancestor that has
// one, so a coarse entry is matched by a coarse predicate and the finer ones underneath never
// get a say: protecting `spec.tls.secretName` does nothing when the source recorded a single
// Update of `spec.tls`, because the filter stops at `spec.tls` and writes the whole block.
// Splitting the entry first is what gives each leaf its own decision.
//
// The split is a sub-diff of the entry's value against what the target has at that path, not
// a walk of the value alone, because a coarse Update *replaces* a subtree while a set of leaf
// Updates merges into it. Diffing recovers the removals the coarse form would have performed,
// so splitting changes which paths are filtered and nothing else.
//
// It only runs where it can matter: the target has a protected path strictly below the entry,
// both sides hold a mapping there, and the entry is not itself a Delete -- deleting a subtree
// is not a set of leaf deletions, and what it displaces is already reported as DeleteShadowed.
func expandCoarsePatchEntries(doc *gaby.YamlDoc, entries []api.MutationMapEntry,
	canonicalPredicates, storedPredicates api.MutationMap,
	mergeKeyLookup MergeKeyLookup) []api.MutationMapEntry {
	expanded := make([]api.MutationMapEntry, 0, len(entries))
	for _, entry := range entries {
		leaves := coarseEntryLeaves(doc, entry, canonicalPredicates, storedPredicates, mergeKeyLookup)
		if leaves == nil {
			expanded = append(expanded, entry)
			continue
		}
		expanded = append(expanded, leaves...)
	}
	return expanded
}

// coarseEntryLeaves returns the per-leaf entries an entry breaks into, or nil to leave it
// whole.
func coarseEntryLeaves(doc *gaby.YamlDoc, entry api.MutationMapEntry,
	canonicalPredicates, storedPredicates api.MutationMap,
	mergeKeyLookup MergeKeyLookup) []api.MutationMapEntry {
	if entry.MutationInfo.MutationType == api.MutationTypeDelete {
		return nil
	}
	resolvedPath, resolved := ResolveAssociativeSegments(doc, string(entry.Path))
	if !resolved {
		return nil
	}
	if !protectsBelow(canonicalPredicates, CanonicalMutationPath(entry.Path)) &&
		!protectsBelow(storedPredicates, api.ResolvedPath(resolvedPath)) {
		return nil
	}
	target := doc.Path(resolvedPath)
	if !isMappingDoc(target) {
		return nil
	}
	value, err := gaby.ParseYAML([]byte(entry.MutationInfo.Value))
	if err != nil || !isMappingDoc(value) {
		return nil
	}

	subMap := api.MutationMap{}
	ComputeMutationsForDocs(string(entry.Path), target, value, entry.MutationInfo.Index,
		subMap, mergeKeyLookup, nil, nil)
	if len(subMap) == 0 {
		return nil
	}
	leaves := make([]api.MutationMapEntry, 0, len(subMap))
	for _, leaf := range api.SortedMutationMapEntries(subMap) {
		// The sub-diff describes what changes; the provenance stays the original entry's,
		// so a leaf is attributed to the mutation that produced the subtree.
		leaf.MutationInfo.Index = entry.MutationInfo.Index
		leaf.MutationInfo.Predicate = entry.MutationInfo.Predicate
		leaves = append(leaves, leaf)
	}
	return leaves
}

// protectsBelow reports whether the target protects a path strictly below the given one. An
// overwritable descendant changes nothing, so only Predicate=false counts.
func protectsBelow(predicates api.MutationMap, path api.ResolvedPath) bool {
	if len(predicates) == 0 {
		return false
	}
	prefix := string(path) + "."
	for candidate, info := range predicates {
		if !info.Predicate && strings.HasPrefix(string(candidate), prefix) {
			return true
		}
	}
	return false
}

// applyPathMutations applies path-level mutations from a PathMutationMap to a document.
// If hasPredicate is true, paths whose path or any ancestor has Predicate=false in the
// caller's predicate map are skipped.
//
// Path resolution uses ResolveAssociativeSegments. If a path can't be fully resolved
// (typically because a merge-keyed element no longer exists in the target with the
// recorded merge-key value), Update and Delete operations are skipped. Add/Replace
// instead falls back to appending to the parent array (see appendPathForAdd) so a new
// merge-keyed element from the patch can still be introduced even when its source-side
// index in the target is occupied by an unrelated element.
//
// Within a single PathMutationMap, Deletes run before Adds/Updates/Replaces. This
// matters for "rename" pairs (Delete old + Add new at the same array parent): with
// Deletes-first the old element is gone before the new element's path is resolved, so
// the Add doesn't have to fight the Delete for the same fallback index. ComputeMutations
// produces Deletes and Adds for disjoint elements, so reordering them within a resource
// is safe.
//
// Removals and insertions of whole elements of a positional (non-merge-keyed) array are
// the exception: they change the indices of every element after them, so applying one
// mid-pass would invalidate the paths of the mutations that follow. They are collected
// instead and applied last, by applyArrayElementOps.
//
// Returns the (possibly extended) errs slice and a list of MutationConflicts for any
// path mutations that were dropped (predicate-filtered or unresolved). Conflicts for
// paths skipped via the Add append-fallback are NOT recorded (the Add was applied,
// just at a different index).
//
// If arrayOrders is non-empty, after path mutations are applied each merge-keyed
// array is reordered to match the recorded source-side merge-key sequence. This
// is what gives positional associative arrays (initContainers, env, ports) their
// correct ordering when a rename, insertion, or reorder is part of the patch.
func applyPathMutations(doc *gaby.YamlDoc, pathMutationMap api.MutationMap,
	hasPredicate bool, mutationsPredicates api.ResourceMutationList, mutationPredicateIndex int,
	resource api.ResourceInfo, arrayOrders api.ArrayOrderMap,
	arrayElementAliases api.ArrayElementAliasMap,
	mergeKeyLookup MergeKeyLookup,
	exclusiveLookup ExclusiveFieldsLookup,
	mutationsSubtracted api.MutationMap,
	errs []error) ([]error, api.MutationConflictList) {

	var conflicts api.MutationConflictList
	var arrayElementOps []arrayElementOp
	var exclusiveTouches []exclusiveTouch

	// Predicates are looked up on canonical paths. The stored MutationSources record a
	// path the way ComputeMutations produced it — with associative segments naming the
	// element by merge key — while the path being applied has been resolved against this
	// document into numeric indices. Comparing the two forms directly never matches, so
	// a Predicate=false inside a merge-keyed array (a hand-edited container image, an
	// env value, a resource limit — the things variants customize most) protected
	// nothing. The resolved form is still consulted as a fallback, since predicates set
	// through the /predicates API and predicates from toolchains with no merge keys are
	// recorded numerically.
	var canonicalPredicates api.MutationMap
	if hasPredicate {
		canonicalPredicates, _ = canonicalMutationMap(mutationsPredicates[mutationPredicateIndex].PathMutationMap)
	}

	// What the target has claimed as its own, in whichever way this merge expresses it: a
	// stored Predicate=false, or — when subtraction is in play, where there are no
	// predicates at all — a mutation of the target's own at the path. Only the exclusive-
	// field rules consult it; every other filter is already expressed as a predicate.
	var canonicalSubtracted api.MutationMap
	if len(mutationsSubtracted) > 0 {
		canonicalSubtracted, _ = canonicalMutationMap(mutationsSubtracted)
	}
	targetOwns := pathOwnership(func(canonicalPath, resolvedPath string) bool {
		if hasPredicate {
			for _, candidate := range []api.MutationMap{canonicalPredicates,
				mutationsPredicates[mutationPredicateIndex].PathMutationMap} {
				if _, mutation, found := api.FindAncestorPath(candidate,
					api.ResolvedPath(canonicalPath)); found && !mutation.Predicate {
					return true
				}
				if _, mutation, found := api.FindAncestorPath(candidate,
					api.ResolvedPath(resolvedPath)); found && !mutation.Predicate {
					return true
				}
			}
		}
		for _, candidate := range []api.MutationMap{canonicalSubtracted, mutationsSubtracted} {
			if len(candidate) == 0 {
				continue
			}
			if _, _, found := api.FindAncestorPath(candidate, api.ResolvedPath(canonicalPath)); found {
				return true
			}
			if _, _, found := api.FindAncestorPath(candidate, api.ResolvedPath(resolvedPath)); found {
				return true
			}
		}
		return false
	})

	// Sort paths so parents are processed before children, then partition so all Deletes
	// run before all non-Deletes. Path order is preserved within each partition.
	sorted := api.SortedMutationMapEntries(pathMutationMap)
	if hasPredicate {
		sorted = expandCoarsePatchEntries(doc, sorted, canonicalPredicates,
			mutationsPredicates[mutationPredicateIndex].PathMutationMap, mergeKeyLookup)
	}
	patches := make([]api.MutationMapEntry, 0, len(sorted))
	for _, entry := range sorted {
		if entry.MutationInfo.MutationType == api.MutationTypeDelete {
			patches = append(patches, entry)
		}
	}
	for _, entry := range sorted {
		if entry.MutationInfo.MutationType != api.MutationTypeDelete {
			patches = append(patches, entry)
		}
	}

	for i := range patches {
		resolvedPath, resolved := ResolveAssociativeSegments(doc, string(patches[i].Path))
		patchPath := api.ResolvedPath(resolvedPath)
		patchMutation := patches[i].MutationInfo
		if !resolved {
			// The path contains an associative segment whose merge-key value didn't
			// match any element in the current doc, and the element at the fallback
			// index has a different merge-key value (i.e., is not the same element).
			// For Add/Replace, fall back to appending the new element to the parent
			// array — this preserves the upstream's intent to add a new element with
			// a unique merge-key value while avoiding clobbering the unrelated
			// element that happens to occupy the source-side index. For Update and
			// Delete, the element being addressed simply isn't there, so skip.
			if patchMutation.MutationType == api.MutationTypeAdd ||
				patchMutation.MutationType == api.MutationTypeReplace {
				if appendPath, ok := appendPathForAdd(doc, resolvedPath); ok {
					patchPath = api.ResolvedPath(appendPath)
				} else {
					slog.Debug("patch path unresolved (Add), skipping",
						"path", string(patches[i].Path), "resolved", resolvedPath)
					conflicts = append(conflicts, api.MutationConflict{
						Reason:   api.ConflictReasonUnresolvedPath,
						Resource: resource,
						Path:     patches[i].Path,
						Source:   *patchMutation,
					})
					continue
				}
			} else {
				slog.Debug("patch path unresolved, skipping",
					"path", string(patches[i].Path), "resolved", resolvedPath,
					"mutationType", string(patchMutation.MutationType))
				conflicts = append(conflicts, api.MutationConflict{
					Reason:   api.ConflictReasonUnresolvedPath,
					Resource: resource,
					Path:     patches[i].Path,
					Source:   *patchMutation,
				})
				continue
			}
		}
		// Check for patches that conflict with the predicate. A coarse entry covering a
		// path the target protects part of has already been broken into its leaves, so
		// this decides one leaf at a time -- see expandCoarsePatchEntries.
		if hasPredicate {
			// Walk up path ancestors to find if any predicate filters this path.
			_, predicateMutation, hasFilter := api.FindAncestorPath(
				canonicalPredicates, CanonicalMutationPath(patches[i].Path))
			if !hasFilter {
				_, predicateMutation, hasFilter = api.FindAncestorPath(
					mutationsPredicates[mutationPredicateIndex].PathMutationMap, patchPath)
			}
			if hasFilter && !predicateMutation.Predicate {
				slog.Debug("path filtered", "path", string(patchPath))
				predicateMutCopy := predicateMutation
				conflicts = append(conflicts, api.MutationConflict{
					Reason:   api.ConflictReasonPredicateFiltered,
					Resource: resource,
					Path:     patches[i].Path,
					Source:   *patchMutation,
					Target:   &predicateMutCopy,
				})
				continue
			}
		}
		// Note what the patch writes into a union, so its siblings can be cleared once
		// the whole patch has been applied. A Delete removes a member rather than
		// choosing one, so it is not a touch.
		if patchMutation.MutationType != api.MutationTypeDelete {
			if touch, ok := findExclusiveTouch(patches[i].Path, string(patchPath), exclusiveLookup); ok {
				touch.patchPath = patches[i].Path
				touch.source = *patchMutation
				exclusiveTouches = append(exclusiveTouches, touch)
			}
		}

		// Removing or inserting a whole element of a positional array renumbers every
		// element after it. Defer those so the rest of the patch, whose paths are all
		// indices into the array as it stands now, is applied against the shape it was
		// computed against. applyArrayElementOps runs them at the end.
		if arrayPath, index, isElement := arrayElementTarget(doc, string(patchPath), patches[i].Path); isElement {
			switch patchMutation.MutationType {
			case api.MutationTypeDelete:
				arrayElementOps = append(arrayElementOps, arrayElementOp{
					arrayPath: arrayPath,
					index:     index,
					remove:    true,
				})
				continue
			case api.MutationTypeAdd:
				valueDoc, err := gaby.ParseYAML([]byte(patchMutation.Value))
				if err != nil {
					errs = append(errs, fmt.Errorf("error parsing value at path %s: %w", patchPath, err))
					continue
				}
				arrayElementOps = append(arrayElementOps, arrayElementOp{
					arrayPath: arrayPath,
					index:     index,
					value:     valueDoc,
					path:      patchPath,
				})
				continue
			}
		}

		// A path can resolve and still address an element past the end of its parent
		// array in the target, because ResolveAssociativeSegments keeps an out-of-bounds
		// fallback index so that an Add of a merge-keyed element lands as an append.
		// Writing to such a path asks the setter for a gap in the array, which it cannot
		// express: it nulls out the value at the root of the path instead of growing the
		// array, silently emptying the document. Append for an Add whose own index is
		// the one past the end; for anything else the element the path addresses simply
		// is not there. Positional element ops are exempt: they were deferred above and
		// keep their recorded indices, which applyArrayElementOps needs in order to
		// apply several insertions in their source-side order.
		isAdd := patchMutation.MutationType == api.MutationTypeAdd ||
			patchMutation.MutationType == api.MutationTypeReplace
		if appendPath, atEnd, beyondEnd := arrayIndexBeyondEnd(doc, string(patchPath), isAdd); beyondEnd {
			if atEnd && isAdd {
				patchPath = api.ResolvedPath(appendPath)
			} else {
				slog.Debug("patch path is past the end of its array, skipping",
					"path", string(patches[i].Path), "resolved", string(patchPath),
					"mutationType", string(patchMutation.MutationType))
				conflicts = append(conflicts, api.MutationConflict{
					Reason:   api.ConflictReasonUnresolvedPath,
					Resource: resource,
					Path:     patches[i].Path,
					Source:   *patchMutation,
				})
				continue
			}
		}

		switch patchMutation.MutationType {
		case api.MutationTypeAdd, api.MutationTypeReplace:
			valueString := patchMutation.Value
			valueDoc, err := gaby.ParseYAML([]byte(valueString))
			if err != nil {
				errs = append(errs, fmt.Errorf("error parsing value at path %s: %w", patchPath, err))
			}
			// An Add says the path did not exist in the configuration the patch was
			// computed against. It says nothing about the target, which may have grown
			// a map of its own at the same path — both sides adding annotations, or
			// nodeSelector entries, or labels, is an ordinary thing for two variants to
			// do. Merge the two in that case, so each side's keys survive and the
			// patch's win where both set the same key; replacing would discard target
			// content the patch never meant to touch. Replace keeps its wholesale
			// meaning (it is a Delete followed by an Add), and a value that isn't a
			// mapping has nothing to merge into.
			if patchMutation.MutationType == api.MutationTypeAdd && isMappingDoc(valueDoc) &&
				isMappingDoc(doc.Path(string(patchPath))) {
				if mergeErr := doc.MergeDocP(valueDoc, string(patchPath)); mergeErr != nil {
					errs = append(errs, fmt.Errorf("error merging value at path %s: %w", patchPath, mergeErr))
				}
				continue
			}
			// Note: This doesn't preserve indentation nor field ordering.
			_, err = doc.SetDocExpandP(valueDoc, string(patchPath))
			if err != nil {
				errs = append(errs, fmt.Errorf("error setting value at path %s: %w", patchPath, err))
			}
		case api.MutationTypeUpdate:
			// For updates, try to preserve comments when possible
			valueString := patchMutation.Value
			valueDoc, err := gaby.ParseYAML([]byte(valueString))
			if err != nil {
				errs = append(errs, fmt.Errorf("error parsing value at path %s: %w", patchPath, err))
				continue
			}

			// Check if the value is a complex object (map or list) vs a scalar
			ynode := valueDoc.YNode()
			isScalarValue := ynode.Kind == yaml.ScalarNode

			if isScalarValue {
				// For multi-line string updates with a line-level patch, apply the
				// patch to the target's current value (three-way merge) rather than
				// replacing it wholesale. This correctly handles the case where the
				// target has been independently modified.
				if patchMutation.Patch != "" {
					currentField := doc.Path(string(patchPath))
					if currentField != nil {
						if currentStr, ok := currentField.Data().(string); ok {
							patched, ok := ApplyScalarPatch(currentStr, patchMutation.Patch)
							if ok {
								// Set the patched string directly as a scalar value
								// rather than parsing it as YAML, which would lose
								// multi-line string formatting.
								_, setErr := doc.SetExpandP(patched, string(patchPath))
								if setErr != nil {
									errs = append(errs, fmt.Errorf("error setting patched value at path %s: %w", patchPath, setErr))
								}
								continue
							}
							slog.Info("scalar patch failed, falling back to full value",
								"path", string(patchPath))
							// Fall through to use valueDoc (the full Value) as wholesale replacement.
						}
					}
				}

				// TODO: This may no longer make sense now that comments are represented as attributes.
				// For scalar values, we need to preserve the comment manually
				// Get the current field to check if it has a comment
				currentField := doc.Path(string(patchPath))
				var existingComment string
				if currentField != nil {
					existingComment = currentField.GetComments()
				}

				// Set the new value
				_, err = doc.SetDocExpandP(valueDoc, string(patchPath))
				if err != nil {
					errs = append(errs, fmt.Errorf("error setting value at path %s: %w", patchPath, err))
				} else if existingComment != "" {
					// Restore the comment after setting the value
					updatedField := doc.Path(string(patchPath))
					if updatedField != nil {
						updatedField.SetComment(existingComment)
					}
				}
			} else {
				// For complex objects (maps/lists), use merge to preserve nested comments
				err = doc.MergeDocP(valueDoc, string(patchPath))
				if err != nil {
					errs = append(errs, fmt.Errorf("error merging value at path %s: %w", patchPath, err))
				}
			}
		case api.MutationTypeDelete:
			if !doc.ExistsP(string(patchPath)) {
				continue
			}
			err := doc.DeleteP(string(patchPath))
			if err != nil {
				errs = append(errs, fmt.Errorf("error deleting path %s: %w", patchPath, err))
			}
		case api.MutationTypeNone:
			// Shouldn't happen for paths, but also shouldn't be anything to do
		}
	}

	errs = applyArrayElementOps(doc, arrayElementOps, errs)

	// Clear the siblings of any union member the patch set. This runs after the element
	// ops so the paths it works with are the ones the merged document actually has.
	conflicts = append(conflicts,
		clearExclusiveSiblings(doc, exclusiveTouches, resource, exclusiveLookup, targetOwns)...)

	// Rename pass: for each (arrayPath, oldKey -> newKey) in arrayElementAliases,
	// find the array element whose merge-key value is oldKey at arrayPath in the
	// current doc and rewrite its merge-key field to newKey. Runs after path
	// mutations (which used oldKey-encoded paths to align with target's diff)
	// and before the reorder pass (which uses newKey to look up elements via
	// ArrayOrders).
	if len(arrayElementAliases) > 0 && mergeKeyLookup != nil {
		for arrayPath, aliases := range arrayElementAliases {
			mergeKeys, _ := mergeKeyLookup(string(arrayPath))
			if len(mergeKeys) == 0 {
				continue
			}
			resolvedArrayPath, ok := ResolveAssociativeSegments(doc, string(arrayPath))
			if !ok {
				continue
			}
			arrayDoc := doc.Path(resolvedArrayPath)
			if arrayDoc == nil {
				continue
			}
			node := arrayDoc.YNode()
			if node == nil || node.Kind != yaml.SequenceNode {
				continue
			}
			for _, elem := range node.Content {
				if elem.Kind != yaml.MappingNode {
					continue
				}
				// Read the element's current identity, look it up among the renames,
				// and write back each key field that the new identity changes. With a
				// single merge key this is the one field it always was.
				currentValues, ok := mergeKeyValuesFromNode(elem, mergeKeys)
				if !ok {
					continue
				}
				newKey, renamed := aliases[MergeKeyIdentity(currentValues)]
				if !renamed {
					continue
				}
				newValues := splitMergeKeyIdentity(newKey, mergeKeys)
				for i, key := range mergeKeys {
					if i >= len(newValues) || newValues[i] == currentValues[i] {
						continue
					}
					for j := 0; j < len(elem.Content)-1; j += 2 {
						k, v := elem.Content[j], elem.Content[j+1]
						if k.Kind == yaml.ScalarNode && k.Value == key && v.Kind == yaml.ScalarNode {
							v.Value = newValues[i]
							break
						}
					}
				}
			}
		}
	}

	// Reorder pass: for each merge-keyed array path with a recorded desired
	// order, rearrange the target array's elements so they match. This runs
	// after path mutations (so newly-added or appended elements are present)
	// and uses the resource provider's merge-key lookup to identify each
	// element by its key field.
	if len(arrayOrders) > 0 && mergeKeyLookup != nil {
		// Process longer paths first so an inner array (e.g. an env array
		// nested in a container) is reordered before its outer container is
		// reordered around it.
		paths := make([]api.ResolvedPath, 0, len(arrayOrders))
		for p := range arrayOrders {
			paths = append(paths, p)
		}
		slices.SortFunc(paths, func(a, b api.ResolvedPath) int {
			return len(b) - len(a) // longer paths first
		})
		for _, path := range paths {
			mergeKeys, _ := mergeKeyLookup(string(path))
			if len(mergeKeys) == 0 {
				continue
			}
			reorderArrayByMergeKey(doc, string(path), mergeKeys, arrayOrders[path])
		}
	}

	return errs, conflicts
}

// Reset walks each path in mutationsPredicates and, where Predicate=true and the value
// at the corresponding location in parsedData is a string or int, sets the value back to
// the toolchain's placeholder marker (PlaceHolderBlockApplyString / PlaceHolderBlockApplyInt).
// Used by the "reset" function to revert the leaves last touched by a chosen subset of
// historical mutations to their unset state, leaving everything else alone.
func Reset(parsedData gaby.Container, mutationsPredicates api.ResourceMutationList, resourceProvider ResourceProvider, options *api.FunctionOptions) error {
	mutationPredicateMap := make(map[api.ResourceTypeAndName]int)
	for i := range mutationsPredicates {
		resourceInfo := mutationsPredicates[i].Resource
		if resourceInfo.ResourceNameWithoutScope == "" {
			resourceInfo.ResourceNameWithoutScope = resourceProvider.RemoveScopeFromResourceName(resourceInfo.ResourceName)
		}
		resourceInfoKey := api.ResourceTypeAndNameFromResourceInfo(resourceInfo)
		mutationPredicateMap[resourceInfoKey] = i
	}

	visitor := func(doc *gaby.YamlDoc, _ any, _ int, docResourceInfo *api.ResourceInfo) (any, []error) {
		resourceInfoKey := api.ResourceTypeAndNameFromResourceInfo(*docResourceInfo)

		mutationPredicateIndex, hasPredicate := mutationPredicateMap[resourceInfoKey]
		if !hasPredicate {
			// Nothing to reset
			return nil, nil
		}

		// TODO: The predicate for the resource could set the default, but would require traversing
		// all the paths, like FindYAMLPathsByValue.
		// shouldBeReset := hasPredicate && mutationsPredicates[mutationPredicateIndex].ResourceMutationInfo.Predicate
		// PathMutationMap is a map, which could be in arbitrary order.
		// We're only going to reset leaves, so that should be ok.

		var errs []error
		for path, mutation := range mutationsPredicates[mutationPredicateIndex].PathMutationMap {
			if !mutation.Predicate {
				// Shouldn't be reset
				continue
			}
			resolvedPathStr, pathResolved := ResolveAssociativeSegments(doc, string(path))
			if !pathResolved {
				continue
			}
			resolvedPath := api.ResolvedPath(resolvedPathStr)
			value, found, err := YamlSafePathGetValueAnyType(doc, resolvedPath, true)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if !found {
				continue
			}
			switch value.(type) {
			case string:
				_, err = doc.SetP(PlaceHolderBlockApplyString, string(resolvedPath))
				if err != nil {
					slog.Info("error setting string value at path", "path", string(resolvedPath), "error", err)
				}
			case int:
				_, err = doc.SetP(PlaceHolderBlockApplyInt, string(resolvedPath))
				if err != nil {
					slog.Info("error setting int value at path", "path", string(resolvedPath), "error", err)
				}
			default:
				// Not a leaf or no placeholder value. Skip.
			}
		}
		return nil, errs
	}

	_, err := VisitResourcesFiltered(parsedData, nil, resourceProvider, options, visitor)
	return err
}

// AddMutations merges newMutations into mutations and returns the result, accumulating
// changes over sequential edits to produce a compiled history of all modifications. The
// accumulated form is what's stored as a Unit's MutationSources and what feeds the
// Predicate map passed into PatchMutations.
//
// Algorithm:
//
//  1. Resource matching: by current ResourceTypeAndName, then by
//     AliasesWithoutScopes (handling renames). Unmatched new mutations are appended as
//     new resource entries.
//
//  2. Resource-level merge:
//
//     | Existing Type    | New Type              | Result              |
//     |------------------|-----------------------|---------------------|
//     | Any              | None                  | Keep existing       |
//     | Any              | Delete or Replace     | Replace with new    |
//     | None             | Any (non-None)        | Replace with new    |
//     | Delete           | Any (non-Delete)      | Change to Replace   |
//     | Add/Update       | Add/Update            | Merge path mutations|
//
//  3. Path-level merge: process newMutations' paths sorted least-specific to most-specific
//     so parent paths land before children. For each new path:
//
//     - Exact match in existing: replace the existing entry, taking the new MutationType
//       (so a later edit's intent — e.g., an Update on a previously-Added field — is
//       reflected). Exception: Delete → non-Delete becomes Replace, since the field was
//       previously erased and is now being re-set.
//     - Existing path is a child of the new path AND new path is Delete or Replace: drop
//       the now-superseded child paths.
//     - Otherwise: insert the new path verbatim, dropping any existing children it
//       supersedes.
//
//     Because the new MutationType replaces the existing one on exact match, when this
//     accumulated record is later used as a patch, PatchMutations sees the latest intent
//     (e.g., Update to merge with the target's value rather than wholesale Replace).
//
//  4. Alias tracking: union of both sides' Aliases / AliasesWithoutScopes so a resource
//     can still be matched after another rename.
//
// Key behaviors:
//   - Accumulative: designed to be called repeatedly as changes occur.
//   - Last-write-wins for values and types on exact-path matches.
//   - Alias awareness: handles resources renamed between mutation sets.
func AddMutations(mutations, newMutations api.ResourceMutationList) (api.ResourceMutationList, bool) {
	hasMutations := false
	idx := api.NewResourceMutationIndex(mutations)
	for i := range newMutations {
		if newMutations[i].ResourceMutationInfo.MutationType == api.MutationTypeNone {
			continue
		}
		hasMutations = true
		mi, present := idx.Find(newMutations[i].Resource, newMutations[i].AliasesWithoutScopes)
		if !present {
			mutations = append(mutations, newMutations[i])
			continue
		}
		if newMutations[i].ResourceMutationInfo.MutationType == api.MutationTypeDelete ||
			newMutations[i].ResourceMutationInfo.MutationType == api.MutationTypeReplace ||
			mutations[mi].ResourceMutationInfo.MutationType == api.MutationTypeNone {
			mutations[mi] = newMutations[i]
			continue
		}
		if mutations[mi].ResourceMutationInfo.MutationType == api.MutationTypeDelete {
			mutations[mi] = newMutations[i]
			mutations[mi].ResourceMutationInfo.MutationType = api.MutationTypeReplace
			continue
		}

		// Update the resource name, which may have changed.
		mutations[mi].Resource.ResourceName = newMutations[i].Resource.ResourceName
		mutations[mi].Resource.ResourceNameWithoutScope = newMutations[i].Resource.ResourceNameWithoutScope
		if mutations[mi].Aliases == nil {
			mutations[mi].Aliases = make(map[api.ResourceName]struct{})
		}
		for alias := range newMutations[i].Aliases {
			mutations[mi].Aliases[alias] = struct{}{}
		}
		if mutations[mi].AliasesWithoutScopes == nil {
			mutations[mi].AliasesWithoutScopes = make(map[api.ResourceName]struct{})
		}
		for alias := range newMutations[i].AliasesWithoutScopes {
			mutations[mi].AliasesWithoutScopes[alias] = struct{}{}
		}

		// Merge ArrayOrders: last-writer-wins per array path. The newer
		// mutation set's view of the desired array order supersedes the
		// older one's. (Both are computed against the same target — newer
		// reflects the latest source-side intent.)
		if len(newMutations[i].ArrayOrders) > 0 {
			if mutations[mi].ArrayOrders == nil {
				mutations[mi].ArrayOrders = make(api.ArrayOrderMap, len(newMutations[i].ArrayOrders))
			}
			for path, order := range newMutations[i].ArrayOrders {
				mutations[mi].ArrayOrders[path] = order
			}
		}

		// Merge ArrayElementAliases: last-writer-wins per (arrayPath, oldKey).
		if len(newMutations[i].ArrayElementAliases) > 0 {
			if mutations[mi].ArrayElementAliases == nil {
				mutations[mi].ArrayElementAliases = make(api.ArrayElementAliasMap, len(newMutations[i].ArrayElementAliases))
			}
			for path, aliases := range newMutations[i].ArrayElementAliases {
				if mutations[mi].ArrayElementAliases[path] == nil {
					mutations[mi].ArrayElementAliases[path] = make(map[string]string, len(aliases))
				}
				for oldKey, newKey := range aliases {
					mutations[mi].ArrayElementAliases[path][oldKey] = newKey
				}
			}
		}

		// Merge the path mutations. The overall MutationType, Add or Update or Replace, should remain the same.
		// If newMutations contains a path that's a prefix of paths in mutations, we need to remove them.
		// If the path matches, then we need to look at the existing MutationType.
		// Otherwise we add the path.
		// Process new paths sorted from least to most specific so that parent paths are
		// processed before children, ensuring child cleanup is correct.
		existingIdx := api.NewPathPrefixIndex(mutations[mi].PathMutationMap)
		for _, entry := range api.SortedMutationMapEntries(newMutations[i].PathMutationMap) {
			path := entry.Path
			mutation := *entry.MutationInfo
			// Exact match: update in place.
			// We originally preserved the original mutation type (either Add or Replace),
			// unless it's Delete. The idea was that the change is relative to less-specific
			// changes in the same set of mutations rather than relative to other configuration data.
			// However, it was unclear the type shouldn't be changed to Update, which would more
			// accurately represent the latest change, so now we update the mutation type, which
			// will cause PatchMutations to attempt to merge if used as a patch.
			if existing, ok := mutations[mi].PathMutationMap[path]; ok {
				mutationType := mutation.MutationType
				if existing.MutationType == api.MutationTypeDelete &&
					mutation.MutationType != api.MutationTypeDelete {
					mutationType = api.MutationTypeReplace
				}
				mutations[mi].PathMutationMap[path] = api.MutationInfo{
					MutationType: mutationType,
					Index:        mutation.Index,
					Predicate:    mutation.Predicate,
					Value:        mutation.Value,
				}
				if mutation.MutationType == api.MutationTypeDelete || mutation.MutationType == api.MutationTypeReplace {
					// Remove any existing child paths it supersedes.
					for _, childPath := range existingIdx.ChildPaths(path) {
						delete(mutations[mi].PathMutationMap, childPath)
					}
				}
			} else {
				// New path: add it and remove any existing child paths it supersedes.
				mutations[mi].PathMutationMap[path] = mutation
				for _, childPath := range existingIdx.ChildPaths(path) {
					delete(mutations[mi].PathMutationMap, childPath)
				}
			}
		}
	}
	return mutations, hasMutations
}

// SubtractMutations removes from mutations any changes that overlap with subtractMutations,
// implementing the "preserve target-side changes" half of three-way merging. Typically
// invoked from PatchMutations via its mutationsToSubtract argument.
//
// Use case:
//
//	source : ComputeMutations(base, sourceEnd)  // upstream changes
//	target : ComputeMutations(base, target)     // local customizations
//	patch  : SubtractMutations(source, target)  // source changes that don't conflict
//
// When PatchMutations applies patch to target, target-side customizations remain because
// the source paths that would have overwritten them have been removed.
//
// Both operands are expected to be diffs produced by ComputeMutations: Add, Delete,
// Update, or None at the resource level. (Replace, which AddMutations may produce when
// accumulating, is handled here defensively but not expected.) Update at the resource
// level has an empty Value — all changes live in PathMutationMap.
//
// Algorithm:
//
//  1. Resource matching: by ResourceTypeAndName, then AliasesWithoutScopes
//     from either side (so renamed resources subtract correctly).
//
//  2. Resource-level subtraction:
//
//     | Subtract Type | Mutation Type | Result                                          |
//     |---------------|---------------|-------------------------------------------------|
//     | Delete        | Any           | Drop (target removed the resource)              |
//     | Replace       | Any           | Drop (target redefined the resource)            |
//     | None          | Any           | Keep (target didn't change it)                  |
//     | Any           | None          | Keep (source didn't change it)                  |
//     | Update/Add    | Delete        | Keep source Delete; emit DeleteShadowed for     |
//     |               |               | each target mutation under it (the target's     |
//     |               |               | edits have nowhere to live once the resource    |
//     |               |               | is gone)                                        |
//     | Update/Add    | Update/Add    | Path-level subtraction                          |
//
//  3. Path-level subtraction: paths are walked using a NewPathPrefixIndex (binary search
//     over a sorted path list) so prefix relationships are O(log n + k):
//
//     - Case 1 (exact match): subtract has the same path → drop the source path.
//     - Case 2 (subtract is ancestor): subtract has spec.containers.0 and source has
//       spec.containers.0.image → drop the source path (parent was changed in target).
//     - Case 3 (subtract is descendant): subtract has spec.containers.0.image and source
//       has spec.containers.0 (whole block). If the source path is a Delete, keep it
//       and emit a DeleteShadowed conflict for each target child path that's being
//       erased — once the parent is gone the child changes can't apply. Otherwise
//       keep the source path and splice in subtract's more-specific paths so
//       PatchMutations' parent-before-child processing lets target's change win.
//
//  4. If subtraction empties an Update's PathMutationMap, the resource-level type
//     downgrades to None.
//
// Returns the patch with subtractions applied, plus a MutationConflictList
// recording every drop (resource-level and path-level) so callers can surface
// them as merge conflicts. The conflicts are advisory — the returned
// ResourceMutationList already reflects the drops.
//
// Key behaviors:
//   - Target precedence: subtractMutations always wins where it overlaps.
//   - Alias awareness: matches resources across renames.
//   - Partial expansion: only splits a parent path when subtract has finer-grained
//     conflicts under it; unaffected branches stay whole.
func SubtractMutations(mutations, subtractMutations api.ResourceMutationList) (api.ResourceMutationList, api.MutationConflictList) {
	subtractIdx := api.NewResourceMutationIndex(subtractMutations)

	result := make(api.ResourceMutationList, 0, len(mutations))
	var conflicts api.MutationConflictList

	for i := range mutations {
		mutation := mutations[i]

		si, found := subtractIdx.Find(mutation.Resource, mutation.AliasesWithoutScopes)
		if !found {
			// No matching subtraction, keep the mutation as is
			result = append(result, mutation)
			continue
		}

		subtractMutation := subtractMutations[si]
		targetResMutInfo := subtractMutation.ResourceMutationInfo

		// Handle resource-level subtraction

		// We expect both operands to be mutations diffs produced by ComputeMutations.
		// ComputeMutations just generates Add, Delete, Update, and None mutations.
		// None means that the resource was present, but not changed.
		// It's AddMutations that converts Delete followed by Add to Replace and
		// Add followed by Update to just Add. We handle Replace here just in case, but
		// don't really expect to see it.
		// Update at the resource level will have an empty Value -- all of the values will be
		// associated with paths.

		// If the resource was deleted in subtractMutations, don't include any changes to it
		if subtractMutation.ResourceMutationInfo.MutationType == api.MutationTypeDelete {
			conflicts = append(conflicts, api.MutationConflict{
				Reason:   api.ConflictReasonSubtracted,
				Resource: mutation.Resource,
				Source:   mutation.ResourceMutationInfo,
				Target:   &targetResMutInfo,
			})
			continue
		}

		// If the resource was replaced in subtractMutations, remove it entirely
		// (the target has completely redefined this resource)
		if subtractMutation.ResourceMutationInfo.MutationType == api.MutationTypeReplace {
			conflicts = append(conflicts, api.MutationConflict{
				Reason:   api.ConflictReasonSubtracted,
				Resource: mutation.Resource,
				Source:   mutation.ResourceMutationInfo,
				Target:   &targetResMutInfo,
			})
			continue
		}

		// Otherwise the resource was not deleted in the target

		// Handle MutationType None
		// in mutation - changes nothing, so it's safe to keep
		// in subtractMutation - the target didn't override anything, so keep it
		if mutation.ResourceMutationInfo.MutationType == api.MutationTypeNone ||
			subtractMutation.ResourceMutationInfo.MutationType == api.MutationTypeNone {
			result = append(result, mutation)
			continue
		}

		// If the source is a Delete at resource level and the target made changes,
		// the source's intent to remove the resource still wins — the target's
		// customizations have nowhere to live once the resource is gone. Let
		// the Delete through and emit DeleteShadowed for the target's
		// resource-level mutation and each path mutation it had, so the caller
		// can surface the lost work.
		if mutation.ResourceMutationInfo.MutationType == api.MutationTypeDelete {
			conflicts = append(conflicts, api.MutationConflict{
				Reason:   api.ConflictReasonDeleteShadowed,
				Resource: mutation.Resource,
				Source:   mutation.ResourceMutationInfo,
				Target:   &targetResMutInfo,
			})
			for _, entry := range api.SortedMutationMapEntries(subtractMutation.PathMutationMap) {
				targetMutCopy := *entry.MutationInfo
				conflicts = append(conflicts, api.MutationConflict{
					Reason:   api.ConflictReasonDeleteShadowed,
					Resource: mutation.Resource,
					Path:     entry.Path,
					Source:   mutation.ResourceMutationInfo,
					Target:   &targetMutCopy,
				})
			}
			result = append(result, mutation)
			continue
		}

		// If the resource was added or updated in subtractMutations and independently added,
		// replaced, or updated in mutations, then merge the two.

		// For Update at resource level, we need to filter out path mutations
		// that were changed in the target. ArrayOrders is merged: each
		// merge-keyed array's source-side desired order is woven with the
		// target-side desired order so target-side inserts keep their
		// relative position in the final merged sequence.
		newMutation := api.ResourceMutation{
			Resource:             mutation.Resource,
			ResourceMutationInfo: mutation.ResourceMutationInfo,
			PathMutationMap:      make(api.MutationMap),
			ArrayOrders:          mergeArrayOrderMaps(mutation.ArrayOrders, subtractMutation.ArrayOrders, mutation.ArrayElementAliases),
			ArrayElementAliases:  mutation.ArrayElementAliases,
			Aliases:              mutation.Aliases,
			AliasesWithoutScopes: mutation.AliasesWithoutScopes,
		}

		// Process each path mutation using sorted iteration and efficient prefix lookups.
		// The comparison is done on canonical paths, so an element that both sides
		// changed is recognized as the same element even when it sits at a different
		// index on either side — which it does as soon as one side adds or removes an
		// earlier element of the same array. subtractOriginalPaths recovers the target's
		// own path when one of its mutations is carried into the result, so what gets
		// applied is still the path the target's diff recorded.
		subtractCanonicalMap, subtractOriginalPaths := canonicalMutationMap(subtractMutation.PathMutationMap)
		subtractPrefixIdx := api.NewPathPrefixIndex(subtractCanonicalMap)
		for _, entry := range api.SortedMutationMapEntries(mutation.PathMutationMap) {
			path := entry.Path
			canonicalPath := CanonicalMutationPath(path)
			pathMutation := *entry.MutationInfo

			// Case 1: Exact match - subtract this path
			if targetMut, found := subtractCanonicalMap[canonicalPath]; found {
				targetMutCopy := targetMut
				conflicts = append(conflicts, api.MutationConflict{
					Reason:   api.ConflictReasonSubtracted,
					Resource: mutation.Resource,
					Path:     path,
					Source:   pathMutation,
					Target:   &targetMutCopy,
				})
				continue
			}

			// Case 2: Check if a subtract path is an ancestor of the mutation path.
			// e.g., subtract has spec.containers.0 and we have spec.containers.0.image
			// In this case, the target changed a parent, so don't apply child changes.
			// Walk up path segments doing map lookups: O(depth) instead of O(n).
			if ancestorPath, ancestorMut, hasAncestor := api.FindAncestorPath(subtractCanonicalMap, canonicalPath); hasAncestor {
				_ = ancestorPath
				ancestorMutCopy := ancestorMut
				conflicts = append(conflicts, api.MutationConflict{
					Reason:   api.ConflictReasonSubtracted,
					Resource: mutation.Resource,
					Path:     path,
					Source:   pathMutation,
					Target:   &ancestorMutCopy,
				})
				continue
			}

			// Case 3: Check if the mutation path is an ancestor of any subtract path.
			// e.g., we have spec.containers.0 (a large block) and subtract has spec.containers.0.image
			// Use the prefix index for O(log n) lookup.
			childPaths := subtractPrefixIdx.ChildPaths(canonicalPath)
			if len(childPaths) > 0 {
				// If the source mutation is a Delete, the target's child changes
				// have nowhere to live once the parent is removed. Let the Delete
				// through and emit DeleteShadowed for each lost child so the
				// caller can surface the dropped target work.
				if pathMutation.MutationType == api.MutationTypeDelete {
					for _, childPath := range childPaths {
						childMutCopy := subtractCanonicalMap[childPath]
						conflicts = append(conflicts, api.MutationConflict{
							Reason:   api.ConflictReasonDeleteShadowed,
							Resource: mutation.Resource,
							Path:     subtractOriginalPaths[childPath],
							Source:   pathMutation,
							Target:   &childMutCopy,
						})
					}
					newMutation.PathMutationMap[path] = pathMutation
					continue
				}
				// Keep the mutation path and add the subtractMutation paths that override it.
				// Since PatchMutations processes paths from least specific to most specific,
				// the subtractMutation's more specific paths will override the mutation's value.
				newMutation.PathMutationMap[path] = pathMutation
				for _, childPath := range childPaths {
					newMutation.PathMutationMap[subtractOriginalPaths[childPath]] = subtractCanonicalMap[childPath]
				}
			} else {
				// No child subtract paths, keep the mutation as-is
				newMutation.PathMutationMap[path] = pathMutation
			}
		}

		// If we removed all path mutations and there are no array reorders or
		// element-rename rewrites to apply either, downgrade an Update to None.
		if len(newMutation.PathMutationMap) == 0 && len(newMutation.ArrayOrders) == 0 &&
			len(newMutation.ArrayElementAliases) == 0 &&
			newMutation.ResourceMutationInfo.MutationType == api.MutationTypeUpdate {
			newMutation.ResourceMutationInfo.MutationType = api.MutationTypeNone
		}

		// Only add if there's something left
		if newMutation.ResourceMutationInfo.MutationType != api.MutationTypeNone || len(result) > 0 {
			result = append(result, newMutation)
		}
	}

	return result, conflicts
}

// FindMutationIndex looks up the mutation index for a specific resource and path
// in a ResourceMutationList. It matches the resource by ResourceTypeAndName,
// handling aliases and scope changes (same pattern as AddMutations).
// For the path, it walks up parent paths to find the most specific mutation index,
// falling back to the resource-level index if no path-level match is found.
// Returns the mutation index and true if found.
func FindMutationIndex(mutationSources api.ResourceMutationList, resource api.ResourceInfo, path api.ResolvedPath) (int64, bool) {
	idx := api.NewResourceMutationIndex(mutationSources)
	mi, found := idx.Find(resource, nil)
	if !found {
		return 0, false
	}

	rm := mutationSources[mi]

	// Build a reverse lookup from stripped (numeric-only) paths to mutation info,
	// so that incoming resolved paths with numeric indices can match mutation entries
	// that use ?key=value;@index format.
	strippedPathMap := make(map[api.ResolvedPath]*api.MutationInfo, len(rm.PathMutationMap))
	for p, info := range rm.PathMutationMap {
		stripped := api.ResolvedPath(StripAssociativeSegments(string(p)))
		if stripped != p {
			infoCopy := info
			strippedPathMap[stripped] = &infoCopy
		}
	}

	// Walk up path segments from most specific to least specific,
	// same pattern as the predicate check at line ~793 in this file.
	pathSegments := gaby.DotPathToSlice(string(path))
	for len(pathSegments) > 0 {
		candidatePath := api.ResolvedPath(JoinPathSegments(pathSegments))
		if mutInfo, ok := rm.PathMutationMap[candidatePath]; ok {
			return mutInfo.Index, true
		}
		// Also try matching against stripped (numeric-only) paths
		if mutInfo, ok := strippedPathMap[candidatePath]; ok {
			return mutInfo.Index, true
		}
		pathSegments = pathSegments[:len(pathSegments)-1]
	}

	// No path-level match; fall back to the resource-level index.
	return rm.ResourceMutationInfo.Index, true
}

// SetPredicates sets the Predicate flag on path-level mutations of a single resource in
// mutations (typically a Unit's accumulated MutationSources), returning the updated list
// and the paths that could not be resolved.
//
// The Predicate flag records whether the path is eligible to be overwritten by a merge:
// true means a merge may patch it, false marks it a protected local override.
// SetMutationPredicates consumes these stored values when no WhereMutation filter is
// supplied, so editing them changes what a subsequent upgrade/merge will overwrite.
//
// For each (path, value) in predicates:
//
//   - Exact match: the entry at path has its Predicate set to value; its other fields
//     (including Value) are left intact.
//   - No exact match: the closest ancestor present in the resource's PathMutationMap (or,
//     failing that, the resource-level mutation) supplies the MutationType and Index, so the
//     new, more-specific entry keeps the same provenance. The entry's Value is taken from the
//     data at path (via YamlSafePathGetDoc) — NOT copied from the ancestor, whose Value is a
//     broader block — and Patch is left empty (it is a line-diff that does not apply to a
//     freshly-set value). Because predicate lookup during PatchMutations walks to the most
//     specific ancestor, this scopes the predicate to path without disturbing the ancestor.
//     This mirrors the parent-splitting in SubtractMutations' Case 3.
//
// A path is returned in unresolved (and left unchanged) when its resource is absent, when it
// has neither an ancestor path nor a resource-level mutation to inherit from, or when it does
// not exist in the resource's data (so no Value can be extracted). parsedData and
// resourceProvider are used to locate the resource's document and read the value at each
// path. mutations is modified in place and also returned for convenience.
func SetPredicates(parsedData gaby.Container, mutations api.ResourceMutationList, resource api.ResourceInfo, predicates map[api.ResolvedPath]bool, resourceProvider ResourceProvider) (api.ResourceMutationList, []api.ResolvedPath) {
	var unresolved []api.ResolvedPath
	idx := api.NewResourceMutationIndex(mutations)
	mi, found := idx.Find(resource, nil)
	if !found {
		for path := range predicates {
			unresolved = append(unresolved, path)
		}
		return mutations, unresolved
	}
	rm := &mutations[mi]
	if rm.PathMutationMap == nil {
		rm.PathMutationMap = make(api.MutationMap)
	}
	doc, _ := FindResourceDoc(parsedData, resourceProvider, &resource)
	for path, predicate := range predicates {
		// Exact match: the entry's Value is already correct; just flip the flag.
		if info, ok := rm.PathMutationMap[path]; ok {
			info.Predicate = predicate
			rm.PathMutationMap[path] = info
			continue
		}
		// Split: inherit MutationType+Index from the closest ancestor (or the
		// resource-level mutation), take Value from the data at path, leave Patch empty.
		_, ancInfo, hasAncestor := api.FindAncestorPath(rm.PathMutationMap, path)
		if !hasAncestor {
			if rm.ResourceMutationInfo.MutationType == api.MutationTypeNone {
				unresolved = append(unresolved, path)
				continue
			}
			ancInfo = rm.ResourceMutationInfo
		}
		if doc == nil {
			unresolved = append(unresolved, path)
			continue
		}
		resolvedPath, ok := ResolveAssociativeSegments(doc, string(path))
		if !ok {
			unresolved = append(unresolved, path)
			continue
		}
		valueDoc, valueFound, err := YamlSafePathGetDoc(doc, api.ResolvedPath(resolvedPath), true)
		if err != nil || !valueFound || valueDoc == nil {
			unresolved = append(unresolved, path)
			continue
		}
		rm.PathMutationMap[path] = api.MutationInfo{
			MutationType: ancInfo.MutationType,
			Index:        ancInfo.Index,
			Predicate:    predicate,
			Value:        valueDoc.String(),
			// Patch intentionally left empty for a freshly-extracted value.
		}
	}
	return mutations, unresolved
}
