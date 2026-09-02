// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// Path annotations are metadata about a location in the configuration, stored beside the
// configuration rather than inside it (docs/design/path-annotations-and-guards.md).
//
// The level has had no home of its own: MutationSources carries path metadata incidentally,
// as a field of the diff record that every write rewrites. That is why the one existing piece
// of it -- MutationInfo.Protected -- needs a preserve pass on every operation, and why the
// question "does writing this path clear what was recorded about it?" is hard to even express.
// A separate table answers it: a write does not touch the table, so nothing is preserved and
// nothing is cleared.
//
// Nothing enforces anything yet. This is the structure and its validation.

// AnnotationKind names a family of path annotations. Each kind owns its own key namespace
// within a path's annotations, so two kinds may use the same key without colliding.
type AnnotationKind string

const (
	// AnnotationKindGuard: the reasons a path's value is what it is. An operation must be
	// cleared for every one of them before it may write the path. Enforcement is a later
	// stage; the kind is defined here because the table is keyed by kind from the start.
	AnnotationKindGuard AnnotationKind = "Guard"
)

// registeredAnnotationKinds is the set a stored kind is validated against. An unknown kind is
// rejected rather than stored, so the table cannot accumulate junk that no reader understands
// -- which is the failure mode of a schemaless side table, and the reason this is a closed set
// rather than a convention.
var registeredAnnotationKinds = map[AnnotationKind]struct{}{
	AnnotationKindGuard: {},
}

// AnnotationKindIsRegistered reports whether a kind is one this build understands.
func AnnotationKindIsRegistered(kind AnnotationKind) bool {
	_, registered := registeredAnnotationKinds[kind]
	return registered
}

// PathAnnotations is one path's annotations, by kind. Within a kind, keys and values are
// opaque strings.
//
// A map of maps rather than a list of structs, for four reasons that all come back to this
// table being re-examined on every write: setting the same annotation twice is a no-op with a
// map, where a list would need a dedup rule, which needs an element identity, which is the
// unkeyed-array problem in the structure whose whole job is to be stable; merging two sets is
// per-kind, per-key last-writer-wins, the shape AddMutations already uses; reading or writing
// one annotation is a lookup rather than a scan; and it unmarshals with no discriminated union
// and no custom UnmarshalJSON.
//
// The shape extends in the "add a key" direction and not the "add structure" direction. A kind
// that genuinely needs typed content gets a typed sibling field on ResourcePathAnnotations,
// not JSON encoded inside a string value -- that is how a legible table becomes an unparseable
// one.
type PathAnnotations map[AnnotationKind]map[string]string

// ResourcePathAnnotations is one resource's path annotations.
//
// It deliberately mirrors ResourceMutation. Resource matching across a rename is not a small
// amount of code -- ResourceMutationIndex, the alias maps, the scopeless fallbacks -- and a
// second implementation of it would be a second thing to keep in step. Sharing the shape means
// sharing that code instead.
type ResourcePathAnnotations struct {
	Resource ResourceInfo `description:"Identifiers of the resource whose paths are annotated"`
	// ResourceAnnotations annotate the resource as a whole, as ResourceMutationInfo records a
	// resource-level mutation. A path with no entry of its own inherits from its closest
	// annotated ancestor and then from here.
	ResourceAnnotations  PathAnnotations                  `json:",omitempty" description:"Annotations on the resource as a whole, inherited by paths with no more specific entry"`
	PathAnnotationMap    map[ResolvedPath]PathAnnotations `json:",omitempty" description:"Annotations by path. Paths are canonical: an associative segment names its element by merge key, with no positional fallback"`
	Aliases              map[ResourceName]struct{}        `json:",omitempty" description:"Names (with scopes, if any) used in current and prior revisions of this resource"`
	AliasesWithoutScopes map[ResourceName]struct{}        `json:",omitempty" description:"Names without scopes used in current and prior revisions of this resource"`
}

// PathAnnotationList is a Unit's or Revision's whole annotation table.
type PathAnnotationList []ResourcePathAnnotations

// Annotation keys take the same character class as AttributeName, and values take that class
// plus '.'. The restriction on values is not cosmetic: a value appears inside In (a, b) lists
// in the CLI and in --where-style expressions, so it must not contain the separators those
// forms parse on. A '.' is allowed because the motivating values are dotted (a policy
// exception naming a field, a version), and no expression form splits on it.
const (
	// MaxAnnotationKeyLength and MaxAnnotationValueLength bound one entry.
	MaxAnnotationKeyLength   = 128
	MaxAnnotationValueLength = 128

	// MaxAnnotationsPerPath and MaxAnnotatedPathsPerResource bound the column, so it cannot
	// grow without limit. These live here, next to the validation that enforces them, because
	// the existing quota system (models.EntityQuota) caps how many entities of a type an
	// organization may have and has no notion of a cap on a field within one. If per-field
	// caps ever get a home of their own, these are the first two tenants.
	MaxAnnotationsPerPath        = 32
	MaxAnnotatedPathsPerResource = 1000
)

var (
	annotationKeyRegexp   = regexp.MustCompile(`^[A-Za-z0-9][-_A-Za-z0-9]*$`)
	annotationValueRegexp = regexp.MustCompile(`^[-_.A-Za-z0-9]*$`)
)

// ReservedAnnotationKeyPrefix marks a key ConfigHub defines rather than a user. Reserving the
// namespace is what lets a well-known key be added later without colliding with one someone is
// already using -- the vocabulary itself is deliberately not chosen here, but the room for it
// has to be taken before anyone authors keys in it.
//
// A prefix rather than a '/'-separated namespace, as Kubernetes label keys have: the separator
// is a plausible later addition and is left out until there is a vocabulary that needs it.
const ReservedAnnotationKeyPrefix = "confighub-"

// AnnotationKeyIsReserved reports whether a key belongs to ConfigHub's namespace. Matching is
// case-insensitive, so a user cannot take a reserved key by changing its case.
func AnnotationKeyIsReserved(key string) bool {
	return strings.HasPrefix(strings.ToLower(key), ReservedAnnotationKeyPrefix)
}

// ValidateAnnotationKey checks one key's character class and length. It does not consider the
// reserved namespace: a key ConfigHub writes itself is legal, and only a user-supplied one is
// refused -- see ValidateUserPathAnnotations.
func ValidateAnnotationKey(key string) error {
	if key == "" {
		return fmt.Errorf("annotation key must not be empty")
	}
	if len(key) > MaxAnnotationKeyLength {
		return fmt.Errorf("annotation key %q exceeds maximum length of %d", key, MaxAnnotationKeyLength)
	}
	if !annotationKeyRegexp.MatchString(key) {
		return fmt.Errorf("annotation key %q must match %s", key, annotationKeyRegexp.String())
	}
	return nil
}

// ValidateAnnotationValue checks one value's character class and length. An empty value is
// legal: a key whose presence is the whole statement need not invent a value to carry.
func ValidateAnnotationValue(key, value string) error {
	if len(value) > MaxAnnotationValueLength {
		return fmt.Errorf("annotation value for key %q exceeds maximum length of %d",
			key, MaxAnnotationValueLength)
	}
	if !annotationValueRegexp.MatchString(value) {
		return fmt.Errorf("annotation value %q for key %q must match %s",
			value, key, annotationValueRegexp.String())
	}
	return nil
}

// ValidatePathAnnotations checks one path's annotations: every kind registered, every key and
// value legal, and the per-path count within bounds. The count is over all kinds together,
// since what it bounds is the size of the stored entry.
func ValidatePathAnnotations(annotations PathAnnotations) error {
	total := 0
	for kind, entries := range annotations {
		if !AnnotationKindIsRegistered(kind) {
			return fmt.Errorf("unknown annotation kind %q", kind)
		}
		for key, value := range entries {
			if err := ValidateAnnotationKey(key); err != nil {
				return err
			}
			if err := ValidateAnnotationValue(key, value); err != nil {
				return err
			}
		}
		total += len(entries)
	}
	if total > MaxAnnotationsPerPath {
		return fmt.Errorf("%d annotations on one path exceeds the maximum of %d",
			total, MaxAnnotationsPerPath)
	}
	return nil
}

// ValidateUserPathAnnotations is ValidatePathAnnotations plus the reserved-namespace check. It
// is what an API request goes through; ConfigHub's own writes use ValidatePathAnnotations, so
// that a key it defines can be written by the code that owns it and by nobody else.
func ValidateUserPathAnnotations(annotations PathAnnotations) error {
	for _, entries := range annotations {
		for key := range entries {
			if AnnotationKeyIsReserved(key) {
				return fmt.Errorf("annotation key %q is reserved: keys beginning with %q are ConfigHub's",
					key, ReservedAnnotationKeyPrefix)
			}
		}
	}
	return ValidatePathAnnotations(annotations)
}

// ValidatePathAnnotationList checks a whole table, including the per-resource path count.
// userSupplied applies the reserved-namespace check.
func ValidatePathAnnotationList(list PathAnnotationList, userSupplied bool) error {
	validate := ValidatePathAnnotations
	if userSupplied {
		validate = ValidateUserPathAnnotations
	}
	for i := range list {
		resource := &list[i]
		if err := validate(resource.ResourceAnnotations); err != nil {
			return fmt.Errorf("resource %s: %w", resource.Resource.ResourceName, err)
		}
		if len(resource.PathAnnotationMap) > MaxAnnotatedPathsPerResource {
			return fmt.Errorf("resource %s: %d annotated paths exceeds the maximum of %d",
				resource.Resource.ResourceName, len(resource.PathAnnotationMap),
				MaxAnnotatedPathsPerResource)
		}
		for path, annotations := range resource.PathAnnotationMap {
			if err := validate(annotations); err != nil {
				return fmt.Errorf("resource %s path %s: %w", resource.Resource.ResourceName, path, err)
			}
		}
	}
	return nil
}

// HasPathAnnotations reports whether a table holds anything at all. Worth a helper because the
// overwhelming majority of Units have none, and that is the case every consumer wants to
// shortcut on: a Unit that has never been annotated should cost nothing.
func HasPathAnnotations(list PathAnnotationList) bool {
	for i := range list {
		if len(list[i].ResourceAnnotations) > 0 || len(list[i].PathAnnotationMap) > 0 {
			return true
		}
	}
	return false
}

// ClonePathAnnotations deep-copies one path's annotations, so an edit to the copy cannot reach
// the original through the shared inner maps.
func ClonePathAnnotations(annotations PathAnnotations) PathAnnotations {
	if annotations == nil {
		return nil
	}
	cloned := make(PathAnnotations, len(annotations))
	for kind, entries := range annotations {
		cloned[kind] = maps.Clone(entries)
	}
	return cloned
}

// ClonePathAnnotationList deep-copies a whole table. Clone, restore, and the transient copies
// an operation works on all need one that shares nothing with the stored value: the maps go
// three deep, so a shallow copy aliases the annotations themselves.
func ClonePathAnnotationList(list PathAnnotationList) PathAnnotationList {
	if list == nil {
		return nil
	}
	cloned := make(PathAnnotationList, 0, len(list))
	for i := range list {
		resource := ResourcePathAnnotations{
			Resource:            list[i].Resource,
			ResourceAnnotations: ClonePathAnnotations(list[i].ResourceAnnotations),
		}
		if list[i].PathAnnotationMap != nil {
			resource.PathAnnotationMap = make(map[ResolvedPath]PathAnnotations, len(list[i].PathAnnotationMap))
			for path, annotations := range list[i].PathAnnotationMap {
				resource.PathAnnotationMap[path] = ClonePathAnnotations(annotations)
			}
		}
		resource.Aliases = cloneResourceNameSet(list[i].Aliases)
		resource.AliasesWithoutScopes = cloneResourceNameSet(list[i].AliasesWithoutScopes)
		cloned = append(cloned, resource)
	}
	return cloned
}

func cloneResourceNameSet(names map[ResourceName]struct{}) map[ResourceName]struct{} {
	if names == nil {
		return nil
	}
	cloned := make(map[ResourceName]struct{}, len(names))
	for name := range names {
		cloned[name] = struct{}{}
	}
	return cloned
}

// ResourceGuards specifies guard edits to apply to one resource's path annotations. It is the
// guard analogue of ResourceProtection, and the set-guard API's unit of work.
//
// Set and Remove are separate rather than a single map with a delete sentinel, because a guard
// value may legitimately be empty -- a key whose presence is the whole statement -- so an empty
// value cannot also mean "remove this".
//
// The empty ResolvedPath addresses the resource as a whole, as it does nowhere else; a guard
// there covers every path in the resource that has no more specific entry.
type ResourceGuards struct {
	Resource ResourceInfo                       `description:"Identifies the resource within the Unit whose guards are being edited"`
	Set      map[ResolvedPath]map[string]string `json:",omitempty" description:"Guard key/value pairs to add or overwrite, by path. The empty path addresses the resource as a whole"`
	Remove   map[ResolvedPath][]string          `json:",omitempty" description:"Guard keys to remove, by path. Removing a key that is not there is not an error"`
}

// ValidateResourceGuards checks the edits a request is asking for: every key and value legal,
// and no key in ConfigHub's reserved namespace. It does not check that the paths exist -- a
// guard is forward-looking policy, and has to be able to cover a path that does not exist yet.
//
// The per-path and per-resource count bounds are not checked here, because what they bound is
// the stored result rather than the edit; the caller checks them on the table it produces.
func ValidateResourceGuards(guards []ResourceGuards) error {
	for i := range guards {
		for path, entries := range guards[i].Set {
			if err := ValidateUserPathAnnotations(PathAnnotations{AnnotationKindGuard: entries}); err != nil {
				return fmt.Errorf("resource %s path %s: %w", guards[i].Resource.ResourceName, path, err)
			}
		}
		for path, keys := range guards[i].Remove {
			for _, key := range keys {
				if err := ValidateAnnotationKey(key); err != nil {
					return fmt.Errorf("resource %s path %s: %w", guards[i].Resource.ResourceName, path, err)
				}
				if AnnotationKeyIsReserved(key) {
					return fmt.Errorf("resource %s path %s: annotation key %q is reserved: keys beginning with %q are ConfigHub's",
						guards[i].Resource.ResourceName, path, key, ReservedAnnotationKeyPrefix)
				}
			}
		}
	}
	return nil
}

// GuardStamp is the set of guards a change states about what it writes, as a key/value map of
// the same shape a path's Guard annotations already have. It is the guard analogue of the
// Protect bit: Protect claims what an operation wrote, a stamp says why it wrote it, and both
// land on the paths the operation produced without either having to name them.
//
// Add and overwrite only. A stamp has no way to say "remove this key", because a removal is a
// decision about the Unit's policy rather than a by-product of writing a value -- and an
// operation that runs repeatedly, a Trigger above all, would otherwise retire policy every time
// it ran. Removal stays with set-guard.
type GuardStamp map[string]string

// ValidateGuardStamp checks the guards a change proposes to stamp: legal keys and values, and
// nothing in ConfigHub's reserved namespace. The per-path and per-Unit count bounds are not
// checked here for the reason ValidateResourceGuards does not check them either -- what they
// bound is the stored table, which the caller validates after stamping.
func ValidateGuardStamp(stamp GuardStamp) error {
	if len(stamp) == 0 {
		return nil
	}
	return ValidateUserPathAnnotations(PathAnnotations{AnnotationKindGuard: map[string]string(stamp)})
}

// MergeGuardStamps combines two stamps, later winning per key. Nil-safe and allocation-free
// when either side is empty, which is the ordinary case: most operations carry one stamp or
// none.
//
// Last writer wins rather than union-with-conflict because a stamp is a map, which is what §3
// chose it to be: a Trigger that says owner=trigger and an Invocation that says owner=library
// are two statements about one class of reason, and the nearer one is the one that ran.
func MergeGuardStamps(earlier, later GuardStamp) GuardStamp {
	if len(later) == 0 {
		return earlier
	}
	if len(earlier) == 0 {
		return later
	}
	merged := make(GuardStamp, len(earlier)+len(later))
	for key, value := range earlier {
		merged[key] = value
	}
	for key, value := range later {
		merged[key] = value
	}
	return merged
}

// SetGuards applies guard edits to a table and returns the result. The table is modified in
// place where it already has entries and extended where it does not; the caller supplies a copy
// if it needs the original.
//
// canonicalizePath brings an incoming path to the form the table is keyed in. It is passed in
// rather than called directly because canonicalization lives in yamlkit, which imports this
// package.
//
// A resource the Unit does not have is not refused, for the same reason a path that does not
// exist is not: policy about a configuration can be written before the configuration has the
// thing it is about, and a guard on something absent is inert rather than wrong.
func SetGuards(table PathAnnotationList, guards []ResourceGuards,
	canonicalizePath func(ResolvedPath) ResolvedPath) PathAnnotationList {
	for i := range guards {
		edit := &guards[i]
		position, found := findResourceAnnotations(table, edit.Resource)
		if !found {
			table = append(table, ResourcePathAnnotations{Resource: edit.Resource})
			position = len(table) - 1
		}
		entry := &table[position]

		for path, entries := range edit.Set {
			target := annotationsForPath(entry, canonicalizePath(path))
			guardEntries, ok := target[AnnotationKindGuard]
			if !ok {
				guardEntries = map[string]string{}
				target[AnnotationKindGuard] = guardEntries
			}
			for key, value := range entries {
				guardEntries[key] = value
			}
		}

		for path, keys := range edit.Remove {
			canonical := canonicalizePath(path)
			var target PathAnnotations
			if canonical == "" {
				target = entry.ResourceAnnotations
			} else {
				target = entry.PathAnnotationMap[canonical]
			}
			guardEntries, ok := target[AnnotationKindGuard]
			if !ok {
				continue
			}
			for _, key := range keys {
				delete(guardEntries, key)
			}
		}
	}
	return table
}

// annotationsForPath returns the PathAnnotations to write into for a path, creating the maps as
// needed. The empty path addresses the resource as a whole.
func annotationsForPath(entry *ResourcePathAnnotations, path ResolvedPath) PathAnnotations {
	if path == "" {
		if entry.ResourceAnnotations == nil {
			entry.ResourceAnnotations = PathAnnotations{}
		}
		return entry.ResourceAnnotations
	}
	if entry.PathAnnotationMap == nil {
		entry.PathAnnotationMap = map[ResolvedPath]PathAnnotations{}
	}
	if entry.PathAnnotationMap[path] == nil {
		entry.PathAnnotationMap[path] = PathAnnotations{}
	}
	return entry.PathAnnotationMap[path]
}

// findResourceAnnotations locates a resource in a table by name, then by alias -- the same order
// every other resource lookup uses.
func findResourceAnnotations(table PathAnnotationList, resource ResourceInfo) (int, bool) {
	for i := range table {
		if table[i].Resource.ResourceName == resource.ResourceName {
			return i, true
		}
	}
	for i := range table {
		if _, aliased := table[i].Aliases[resource.ResourceName]; aliased {
			return i, true
		}
		if resource.ResourceNameWithoutScope != "" {
			if _, aliased := table[i].AliasesWithoutScopes[resource.ResourceNameWithoutScope]; aliased {
				return i, true
			}
		}
	}
	return 0, false
}

// A clearance is what an operation carries to say which classes of reason it knows about. A
// guard names a reason; a clearance names the reasons an operation is cleared for. The pairing
// reads correctly in an error message, which is most of why the words were chosen: the write was
// withheld by the guard policy-exception=host-network, which the operation's clearance did not
// cover.
//
// The operators are Kubernetes' set-based label selection, minus the parts that do not apply.
// '=' and '!=' are In and NotIn with a single value. This is the expressivity asked for and no
// more; a general expression language could subsume it later without changing the storage.

type ClearanceOperator string

const (
	// ClearanceOperatorExists clears a guard key whatever its value.
	ClearanceOperatorExists ClearanceOperator = "Exists"
	// ClearanceOperatorIn clears a guard key whose value is one of Values.
	ClearanceOperatorIn ClearanceOperator = "In"
	// ClearanceOperatorNotIn clears a guard key whose value is not one of Values.
	ClearanceOperatorNotIn ClearanceOperator = "NotIn"
	// ClearanceOperatorDoesNotExist is a precondition rather than a clearing rule: a path
	// carrying the key at all is not to be written, whatever else the clearance says. It
	// expresses "I am cleared for owner guards, but never touch anything carrying a
	// policy-exception".
	ClearanceOperatorDoesNotExist ClearanceOperator = "DoesNotExist"
)

type ClearanceRequirement struct {
	Key      string            `description:"The guard key this requirement is about"`
	Operator ClearanceOperator `description:"Exists, In, NotIn, or DoesNotExist"`
	Values   []string          `json:",omitempty" description:"The values In and NotIn compare against; unused by Exists and DoesNotExist"`
}

// Clearance is the set of reasons an operation is cleared for. The zero value clears nothing,
// which is the point: protect by default, so an operation that says nothing about guards cannot
// write a guarded path.
type Clearance []ClearanceRequirement

// ValidateClearance checks that every requirement names a legal key, uses a known operator, and
// carries values exactly when its operator uses them.
func ValidateClearance(clearance Clearance) error {
	for i := range clearance {
		requirement := &clearance[i]
		if err := ValidateAnnotationKey(requirement.Key); err != nil {
			return err
		}
		switch requirement.Operator {
		case ClearanceOperatorIn, ClearanceOperatorNotIn:
			if len(requirement.Values) == 0 {
				return fmt.Errorf("clearance requirement %s %s needs at least one value",
					requirement.Key, requirement.Operator)
			}
			for _, value := range requirement.Values {
				if err := ValidateAnnotationValue(requirement.Key, value); err != nil {
					return err
				}
			}
		case ClearanceOperatorExists, ClearanceOperatorDoesNotExist:
			if len(requirement.Values) > 0 {
				return fmt.Errorf("clearance requirement %s %s takes no values",
					requirement.Key, requirement.Operator)
			}
		default:
			return fmt.Errorf("unknown clearance operator %q for key %q",
				requirement.Operator, requirement.Key)
		}
	}
	return nil
}

// WithheldGuard is the guard that stopped a write, for the conflict that reports it.
type WithheldGuard struct {
	Key   string `description:"The guard key the operation was not cleared for"`
	Value string `json:",omitempty" description:"The guard value"`
	// Precondition says the clearance named this key with DoesNotExist -- the operation
	// declared it would not touch a path carrying the key, rather than merely failing to
	// mention it. Worth distinguishing in the report: one is an omission and the other is a
	// decision, and the fix is different.
	Precondition bool `json:",omitempty" description:"True when the clearance forbade this key with DoesNotExist rather than simply not covering it"`
}

// Admits reports whether an operation carrying this clearance may write a path whose effective
// guards are the given key/value pairs, and if not, which guard stopped it.
//
// The rule, from the design:
//
//  1. Every guard key must be cleared. For each guard, the clearance must hold at least one
//     requirement with that key whose operator admits the value.
//  2. DoesNotExist is a precondition, not a clearing rule: a path carrying that key is not
//     written regardless of what else the clearance says.
//  3. An empty clearance clears nothing.
//
// A path with no guards is admitted by any clearance, including an empty one -- which is the
// overwhelming majority of paths, and the case that has to stay free.
func (c Clearance) Admits(guards map[string]string) (bool, WithheldGuard) {
	if len(guards) == 0 {
		return true, WithheldGuard{}
	}
	// Preconditions first, so that a clearance which both forbids a key and would otherwise
	// clear it reports the forbidding -- the more deliberate of the two statements.
	for i := range c {
		if c[i].Operator != ClearanceOperatorDoesNotExist {
			continue
		}
		if value, carried := guards[c[i].Key]; carried {
			return false, WithheldGuard{Key: c[i].Key, Value: value, Precondition: true}
		}
	}
	for key, value := range guards {
		if !c.admitsGuard(key, value) {
			return false, WithheldGuard{Key: key, Value: value}
		}
	}
	return true, WithheldGuard{}
}

// admitsGuard reports whether any requirement clears one guard.
func (c Clearance) admitsGuard(key, value string) bool {
	for i := range c {
		requirement := &c[i]
		if requirement.Key != key {
			continue
		}
		switch requirement.Operator {
		case ClearanceOperatorExists:
			return true
		case ClearanceOperatorIn:
			if slices.Contains(requirement.Values, value) {
				return true
			}
		case ClearanceOperatorNotIn:
			if !slices.Contains(requirement.Values, value) {
				return true
			}
		}
	}
	return false
}

// GuardsForPath returns the guards in force at a path: its own, every annotated ancestor's, and
// the resource's. Nil when there are none, which is what a caller should check before doing any
// further work. The path must already be canonical, as the table's keys are.
//
// Two ways this differs from how Protected resolves, both deliberate.
//
// It unions rather than taking the closest entry. Protected is one bit, so a nearer entry can
// only agree or disagree with a farther one and the nearer should win. Guards are a set of
// reasons, and a path having a reason of its own does not make a subtree-wide policy stop
// applying to it.
//
// It walks the whole chain rather than stopping at the closest annotated ancestor, which is what
// the design says (§4). Stopping would mean that annotating a path for any reason cancels every
// guard above it -- guard spec.template.spec.containers, then annotate one container's image
// with something unrelated, and the containers-level policy quietly stops covering that image.
// Since the point of guarding a subtree is to cover what is added inside it later, an
// inheritance that a later annotation can switch off is not the inheritance that was wanted.
// Walking the chain costs the same order as stopping at the first match: both are map lookups
// per level of path depth.
func (r *ResourcePathAnnotations) GuardsForPath(path ResolvedPath) map[string]string {
	if r == nil {
		return nil
	}
	var effective map[string]string
	add := func(annotations PathAnnotations) {
		for key, value := range annotations[AnnotationKindGuard] {
			if effective == nil {
				effective = map[string]string{}
			}
			// A nearer entry wins on the value of a key an ancestor also names, since it is
			// the more specific statement about the same class of reason.
			if _, nearer := effective[key]; !nearer {
				effective[key] = value
			}
		}
	}
	// Nearest first, so the "nearer wins" rule above needs no ordering bookkeeping.
	// Splitting on "." is safe: dots within a segment are escaped as ~1.
	remaining := string(path)
	for {
		add(r.PathAnnotationMap[ResolvedPath(remaining)])
		lastDot := strings.LastIndex(remaining, ".")
		if lastDot < 0 {
			break
		}
		remaining = remaining[:lastDot]
	}
	add(r.ResourceAnnotations)
	return effective
}

// GuardDelta is one change to the guards at one path: keys to set, and keys to remove.
//
// The direction matters, which is why a removal is a list of keys rather than an absent entry.
// Guards propagate by diff rather than by union, and applying a diff is what lets a *removal*
// travel: when a workload no longer needs hostNetwork and the base drops the exception, the
// variants drop it too, instead of accumulating stale guards nobody can account for.
type GuardDelta struct {
	Path   ResolvedPath      `json:",omitempty" description:"The path whose guards changed; empty for the resource as a whole"`
	Set    map[string]string `json:",omitempty" description:"Guard keys added or changed, with their new values"`
	Remove []string          `json:",omitempty" description:"Guard keys removed"`
}

// IsEmpty reports whether a delta says nothing.
func (d *GuardDelta) IsEmpty() bool {
	return d == nil || (len(d.Set) == 0 && len(d.Remove) == 0)
}

// ResourceGuardDiff is one resource's guard changes over a range.
type ResourceGuardDiff struct {
	Resource ResourceInfo
	Deltas   []GuardDelta
}

// DiffGuards computes the guard changes between two annotation tables -- the source's, at the
// base and end of the range a merge is carrying.
//
// Only guards are diffed. Other annotation kinds do not propagate yet, and folding them in here
// silently would be a decision made by omission rather than on purpose.
//
// Resources are matched by name and alias, so a rename inside the range does not read as one
// resource losing all its guards and another gaining them.
func DiffGuards(base, end PathAnnotationList) []ResourceGuardDiff {
	var diffs []ResourceGuardDiff
	for i := range end {
		endEntry := &end[i]
		var baseEntry *ResourcePathAnnotations
		if position, found := findResourceAnnotations(base, endEntry.Resource); found {
			baseEntry = &base[position]
		}
		if deltas := diffResourceGuards(baseEntry, endEntry); len(deltas) > 0 {
			diffs = append(diffs, ResourceGuardDiff{Resource: endEntry.Resource, Deltas: deltas})
		}
	}
	// A resource the end no longer has: everything it carried was removed. Reported so the
	// removal propagates rather than being lost with the entry.
	for i := range base {
		baseEntry := &base[i]
		if _, found := findResourceAnnotations(end, baseEntry.Resource); found {
			continue
		}
		if deltas := diffResourceGuards(baseEntry, &ResourcePathAnnotations{Resource: baseEntry.Resource}); len(deltas) > 0 {
			diffs = append(diffs, ResourceGuardDiff{Resource: baseEntry.Resource, Deltas: deltas})
		}
	}
	return diffs
}

// diffResourceGuards computes the per-path deltas between two entries for the same resource.
func diffResourceGuards(base, end *ResourcePathAnnotations) []GuardDelta {
	var deltas []GuardDelta
	appendDelta := func(path ResolvedPath, baseGuards, endGuards map[string]string) {
		delta := GuardDelta{Path: path}
		for key, value := range endGuards {
			if baseValue, present := baseGuards[key]; !present || baseValue != value {
				if delta.Set == nil {
					delta.Set = map[string]string{}
				}
				delta.Set[key] = value
			}
		}
		for key := range baseGuards {
			if _, present := endGuards[key]; !present {
				delta.Remove = append(delta.Remove, key)
			}
		}
		if !delta.IsEmpty() {
			slices.Sort(delta.Remove)
			deltas = append(deltas, delta)
		}
	}

	var baseResource, baseByPath = map[string]string(nil), map[ResolvedPath]PathAnnotations(nil)
	if base != nil {
		baseResource = base.ResourceAnnotations[AnnotationKindGuard]
		baseByPath = base.PathAnnotationMap
	}
	appendDelta("", baseResource, end.ResourceAnnotations[AnnotationKindGuard])

	for path, annotations := range end.PathAnnotationMap {
		appendDelta(path, baseByPath[path][AnnotationKindGuard], annotations[AnnotationKindGuard])
	}
	for path, annotations := range baseByPath {
		if _, stillThere := end.PathAnnotationMap[path]; stillThere {
			continue
		}
		appendDelta(path, annotations[AnnotationKindGuard], nil)
	}
	// Sorted so the order a merge reports and applies changes in does not depend on map
	// iteration, which is what makes a propagation reproducible.
	slices.SortFunc(deltas, func(a, b GuardDelta) int { return strings.Compare(string(a.Path), string(b.Path)) })
	return deltas
}

// ApplyGuardDelta applies one delta to a table entry, creating the entry if the resource has
// none. Returns whether anything changed.
func ApplyGuardDelta(table PathAnnotationList, resource ResourceInfo, delta *GuardDelta) (PathAnnotationList, bool) {
	if delta.IsEmpty() {
		return table, false
	}
	position, found := findResourceAnnotations(table, resource)
	if !found {
		table = append(table, ResourcePathAnnotations{Resource: resource})
		position = len(table) - 1
	}
	entry := &table[position]

	changed := false
	if len(delta.Set) > 0 {
		target := annotationsForPath(entry, delta.Path)
		guards, ok := target[AnnotationKindGuard]
		if !ok {
			guards = map[string]string{}
			target[AnnotationKindGuard] = guards
		}
		for key, value := range delta.Set {
			if existing, present := guards[key]; !present || existing != value {
				guards[key] = value
				changed = true
			}
		}
	}
	if len(delta.Remove) > 0 {
		var target PathAnnotations
		if delta.Path == "" {
			target = entry.ResourceAnnotations
		} else {
			target = entry.PathAnnotationMap[delta.Path]
		}
		if guards, ok := target[AnnotationKindGuard]; ok {
			for _, key := range delta.Remove {
				if _, present := guards[key]; present {
					delete(guards, key)
					changed = true
				}
			}
		}
	}
	return table, changed
}

// ClearanceSuggestion renders the narrowest clearance requirement that would admit a withheld
// guard, in the form the CLI accepts, so a report can say what would allow the change rather
// than only what stopped it. Empty for a precondition, where the answer is not to add a
// requirement but to remove one.
func ClearanceSuggestion(withheld WithheldGuard) string {
	if withheld.Precondition {
		return ""
	}
	if withheld.Value == "" {
		return withheld.Key
	}
	return withheld.Key + "=" + withheld.Value
}

// WithheldGuardDetails is the sentence a conflict carries about a guard that stopped something:
// which reason it was, and what would have covered it. The Reason alone says a guard stopped the
// write; this says which and what to do, which is the difference between a report someone can
// act on and one they have to investigate.
func WithheldGuardDetails(withheld WithheldGuard) string {
	guard := withheld.Key
	if withheld.Value != "" {
		guard += "=" + withheld.Value
	}
	if withheld.Precondition {
		return "withheld by the guard " + guard +
			", which this operation's clearance refuses outright with DoesNotExist"
	}
	return "withheld by the guard " + guard +
		", which this operation's clearance did not cover; a clearance of " +
		ClearanceSuggestion(withheld) + " would allow it"
}
