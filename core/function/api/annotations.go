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
	// cleared for every one of them before it may write the path.
	AnnotationKindGuard AnnotationKind = "Guard"
	// AnnotationKindGuardInherited: which of the guards at the same entry a clone and a merge
	// carry to downstream Units. Each key names a Guard key beside it, and the value is always
	// empty: the guard is the statement and this only says it travels.
	//
	// Most guards do not travel. One a Link or a Trigger stamps is about that Link or Trigger,
	// and one protecting a variant's own value from upgrades is about that variant; carried
	// downstream, each would be a reason nobody there has. What should travel is policy about
	// the configuration itself -- a value a mass change must not normalize away -- which every
	// variant of it has to honor.
	//
	// A marker rather than a second kind of guard, so that everything enforcing guards reads
	// the Guard kind alone and treats an inherited guard exactly as a local one. Only what copies
	// guards between Units reads this.
	AnnotationKindGuardInherited AnnotationKind = "GuardInherited"
	// AnnotationKindConflictDismissal: the merge conflicts at a path that are understood and
	// expected, so that a merge reporting one again does not leave it outstanding. Each key
	// names a conflict reason, alone or with a guard key (see ConflictDismissalKey). Recorded by
	// the conflicts API, never by a merge, and not carried to a clone: what a Unit has decided
	// about its own conflicts is not a decision about its clones'.
	AnnotationKindConflictDismissal AnnotationKind = "ConflictDismissal"
)

// registeredAnnotationKinds is the set a stored kind is validated against. An unknown kind is
// rejected rather than stored, so the table cannot accumulate junk that no reader understands
// -- which is the failure mode of a schemaless side table, and the reason this is a closed set
// rather than a convention.
var registeredAnnotationKinds = map[AnnotationKind]struct{}{
	AnnotationKindGuard:             {},
	AnnotationKindGuardInherited:    {},
	AnnotationKindConflictDismissal: {},
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
		if kind != AnnotationKindGuardInherited {
			total += len(entries)
		}
	}
	if total > MaxAnnotationsPerPath {
		return fmt.Errorf("%d annotations on one path exceeds the maximum of %d",
			total, MaxAnnotationsPerPath)
	}
	return validateGuardInheritedMarkers(annotations)
}

// validateGuardInheritedMarkers checks that every inheritance marker names a guard beside it and
// carries no value. A marker without its guard would be a statement about nothing, and a value on
// one would be a second copy of the guard's that could disagree with it. The markers are not
// counted against MaxAnnotationsPerPath: each is a property of a guard that already is.
func validateGuardInheritedMarkers(annotations PathAnnotations) error {
	guards := annotations[AnnotationKindGuard]
	for key, value := range annotations[AnnotationKindGuardInherited] {
		if _, guarded := guards[key]; !guarded {
			return fmt.Errorf("%s key %q names no guard at the same path",
				AnnotationKindGuardInherited, key)
		}
		if value != "" {
			return fmt.Errorf("%s key %q must have an empty value", AnnotationKindGuardInherited, key)
		}
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

// HasGuards reports whether a table holds any guard. It is what the guard filter shortcuts on,
// rather than HasPathAnnotations: a table holding only other kinds of annotation has nothing
// for an operation to be cleared for, and a Unit that has recorded a conflict dismissal must not
// start paying for a filter that can withhold nothing.
func HasGuards(list PathAnnotationList) bool {
	for i := range list {
		if len(list[i].ResourceAnnotations[AnnotationKindGuard]) > 0 {
			return true
		}
		for _, annotations := range list[i].PathAnnotationMap {
			if len(annotations[AnnotationKindGuard]) > 0 {
				return true
			}
		}
	}
	return false
}

// WithoutAnnotationKind returns a copy of a table with every annotation of one kind removed, and
// the entries left with nothing dropped.
func WithoutAnnotationKind(list PathAnnotationList, kind AnnotationKind) PathAnnotationList {
	if list == nil {
		return nil
	}
	cloned := ClonePathAnnotationList(list)
	kept := cloned[:0]
	for i := range cloned {
		entry := cloned[i]
		delete(entry.ResourceAnnotations, kind)
		if len(entry.ResourceAnnotations) == 0 {
			entry.ResourceAnnotations = nil
		}
		for path, annotations := range entry.PathAnnotationMap {
			delete(annotations, kind)
			if len(annotations) == 0 {
				delete(entry.PathAnnotationMap, path)
			}
		}
		if len(entry.PathAnnotationMap) == 0 {
			entry.PathAnnotationMap = nil
		}
		if entry.ResourceAnnotations == nil && entry.PathAnnotationMap == nil {
			continue
		}
		kept = append(kept, entry)
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// InheritedGuards returns a copy of a table holding only its inherited guards, each with its
// marker, and the entries left with nothing dropped. It is what a clone takes from the Unit it is
// cloned from, and what a merge diffs to find the guard changes to carry downstream: the guards
// that are policy about the configuration, and none of the ones about the source Unit itself --
// its local guards and its conflict dismissals.
func InheritedGuards(list PathAnnotationList) PathAnnotationList {
	if list == nil {
		return nil
	}
	cloned := ClonePathAnnotationList(list)
	kept := cloned[:0]
	for i := range cloned {
		entry := cloned[i]
		entry.ResourceAnnotations = inheritedGuardsOnly(entry.ResourceAnnotations)
		for path, annotations := range entry.PathAnnotationMap {
			if inherited := inheritedGuardsOnly(annotations); inherited != nil {
				entry.PathAnnotationMap[path] = inherited
			} else {
				delete(entry.PathAnnotationMap, path)
			}
		}
		if len(entry.PathAnnotationMap) == 0 {
			entry.PathAnnotationMap = nil
		}
		if entry.ResourceAnnotations == nil && entry.PathAnnotationMap == nil {
			continue
		}
		kept = append(kept, entry)
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// inheritedGuardsOnly reduces one path's annotations to its inherited guards and their markers,
// or nil when it has none.
func inheritedGuardsOnly(annotations PathAnnotations) PathAnnotations {
	markers := annotations[AnnotationKindGuardInherited]
	if len(markers) == 0 {
		return nil
	}
	guards := map[string]string{}
	kept := map[string]string{}
	for key := range markers {
		if value, guarded := annotations[AnnotationKindGuard][key]; guarded {
			guards[key] = value
			kept[key] = ""
		}
	}
	if len(guards) == 0 {
		return nil
	}
	return PathAnnotations{AnnotationKindGuard: guards, AnnotationKindGuardInherited: kept}
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
//
// Set and SetInherited write the same guards and differ in whether they travel: a guard set
// through Set stays with this Unit, and one set through SetInherited is carried to its clones and,
// by merges, to every downstream Unit. Each writes whether the guard is inherited as well as its
// value, so re-setting an inherited guard through Set makes it local again.
type ResourceGuards struct {
	Resource     ResourceInfo                       `description:"The resource whose guards are being edited: ResourceName, its whole name (namespace/name) or a name it had before a rename, or ResourceNameWithoutScope, a name without its namespace that has to match one resource; ResourceType, in any API version, is optional. A resource the unit does not have yet is named by ResourceType and ResourceName."`
	Set          map[ResolvedPath]map[string]string `json:",omitempty" description:"Guard key/value pairs to add or overwrite, by path, as guards of this unit alone: clones and downstream units do not receive them, and one that was inherited stops being. The empty path addresses the resource as a whole. A list element is named by merge key or by position, and * stands for every element or key there is; a path that does not exist yet is kept as written"`
	SetInherited map[ResolvedPath]map[string]string `json:",omitempty" description:"Guard key/value pairs to add or overwrite, by path, as inherited guards: a clone of the unit receives them, and a merge carries them, and any later change to them, to the units downstream of it. Named as in Set; a key may not be in both at the same path"`
	Remove       map[ResolvedPath][]string          `json:",omitempty" description:"Guard keys to remove, by path, named as in Set; * also matches the paths guarded that the data no longer has. Removing a key that is not there is not an error"`
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
		for path, entries := range guards[i].SetInherited {
			if err := ValidateUserPathAnnotations(PathAnnotations{AnnotationKindGuard: entries}); err != nil {
				return fmt.Errorf("resource %s path %s: %w", guards[i].Resource.ResourceName, path, err)
			}
			for key := range entries {
				if _, local := guards[i].Set[path][key]; local {
					return fmt.Errorf("resource %s path %s: guard %q is set both as inherited and not",
						guards[i].Resource.ResourceName, path, key)
				}
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
func SetGuards(identifier ResourceIdentifier, table PathAnnotationList, guards []ResourceGuards,
	canonicalizePath func(ResolvedPath) ResolvedPath) PathAnnotationList {
	return setGuards(identifier, table, guards, canonicalizePath, markLocal)
}

// StampGuards is SetGuards for the guards a change records on what it wrote: Set leaves whether
// each guard is inherited as it was, where SetGuards makes it local. A stamp only adds and
// overwrites, and making an inherited guard local would retract it from every Unit downstream --
// a decision about policy that belongs to set-guard, as removing a guard does.
func StampGuards(identifier ResourceIdentifier, table PathAnnotationList, guards []ResourceGuards,
	canonicalizePath func(ResolvedPath) ResolvedPath) PathAnnotationList {
	return setGuards(identifier, table, guards, canonicalizePath, keepInheritance)
}

// guardInheritance says what writing a guard does to whether it is inherited.
type guardInheritance int

const (
	keepInheritance guardInheritance = iota
	markLocal
	markInherited
)

func setGuards(identifier ResourceIdentifier, table PathAnnotationList, guards []ResourceGuards,
	canonicalizePath func(ResolvedPath) ResolvedPath, local guardInheritance) PathAnnotationList {
	for i := range guards {
		edit := &guards[i]
		position, found := findResourceAnnotations(identifier, table, edit.Resource)
		if !found {
			table = append(table, ResourcePathAnnotations{Resource: edit.Resource})
			position = len(table) - 1
		}
		entry := &table[position]

		for path, entries := range edit.Set {
			setGuardEntries(annotationsForPath(entry, canonicalizePath(path)), entries, local)
		}
		for path, entries := range edit.SetInherited {
			setGuardEntries(annotationsForPath(entry, canonicalizePath(path)), entries, markInherited)
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
				delete(target[AnnotationKindGuardInherited], key)
			}
		}
	}
	return table
}

// setGuardEntries writes guards into one path's annotations, marking each inherited, local, or
// leaving that as it was. Reports whether anything changed.
func setGuardEntries(target PathAnnotations, entries map[string]string, inheritance guardInheritance) bool {
	changed := false
	guards, ok := target[AnnotationKindGuard]
	if !ok {
		guards = map[string]string{}
		target[AnnotationKindGuard] = guards
	}
	for key, value := range entries {
		if existing, present := guards[key]; !present || existing != value {
			guards[key] = value
			changed = true
		}
		markers := target[AnnotationKindGuardInherited]
		_, marked := markers[key]
		switch {
		case inheritance == markInherited && !marked:
			if markers == nil {
				markers = map[string]string{}
				target[AnnotationKindGuardInherited] = markers
			}
			markers[key] = ""
			changed = true
		case inheritance == markLocal && marked:
			delete(markers, key)
			changed = true
		}
	}
	return changed
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

// findResourceAnnotations locates a resource in a table as MutationSources are searched: by type
// and name, with its scope or, when that names one resource, without it, then by alias.
func findResourceAnnotations(identifier ResourceIdentifier, table PathAnnotationList, resource ResourceInfo) (int, bool) {
	return annotationIndex(identifier, table).Find(resource, nil)
}

// FindResourceAnnotations is findResourceAnnotations for a reader outside this package: the entry
// of a table about a resource, matched as MutationSources entries are, including by the names
// the resource had before a rename.
func FindResourceAnnotations(identifier ResourceIdentifier, table PathAnnotationList, resource ResourceInfo) (int, bool) {
	return findResourceAnnotations(identifier, table, resource)
}

// annotationIndex indexes a table's resources as a ResourceMutationIndex, which is what
// ResourcePathAnnotations was shaped for: one implementation of resource matching and of
// renames.
func annotationIndex(identifier ResourceIdentifier, table PathAnnotationList) *ResourceMutationIndex {
	identities := make(ResourceMutationList, len(table))
	for i := range table {
		identities[i] = ResourceMutation{
			Resource:             table[i].Resource,
			Aliases:              table[i].Aliases,
			AliasesWithoutScopes: table[i].AliasesWithoutScopes,
		}
	}
	return NewResourceMutationIndex(identifier, identities)
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
	Values   []string          `json:",omitempty" listType:"set" description:"The values In and NotIn compare against; unused by Exists and DoesNotExist"`
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
// guards are the given key/value pairs, and if not, which guard stopped it: the first that
// Withheld lists.
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
	withheld := c.Withheld(guards)
	if len(withheld) == 0 {
		return true, WithheldGuard{}
	}
	return false, withheld[0]
}

// Withheld returns every guard of a path that the clearance does not cover: the ones it forbids
// with DoesNotExist first, the more deliberate of the two statements, and then the ones it does
// not mention or does not clear, each group in key order.
//
// All of them rather than the first, because each is a reason the write was withheld, and a
// person deciding the conflict is understood has to have been told every reason: a conflict that
// named one of two would be dismissed for that one, and the other would never be seen. And in key
// order rather than as the guards map iterates, so that the same write withheld twice reports
// the same guards in the same order -- which is what lets a recorded dismissal recognize it.
func (c Clearance) Withheld(guards map[string]string) []WithheldGuard {
	if len(guards) == 0 {
		return nil
	}
	var preconditions, uncleared []WithheldGuard
	forbidden := map[string]bool{}
	for i := range c {
		if c[i].Operator != ClearanceOperatorDoesNotExist || forbidden[c[i].Key] {
			continue
		}
		if value, carried := guards[c[i].Key]; carried {
			forbidden[c[i].Key] = true
			preconditions = append(preconditions, WithheldGuard{Key: c[i].Key, Value: value, Precondition: true})
		}
	}
	for key, value := range guards {
		if !forbidden[key] && !c.admitsGuard(key, value) {
			uncleared = append(uncleared, WithheldGuard{Key: key, Value: value})
		}
	}
	byKey := func(a, b WithheldGuard) int { return strings.Compare(a.Key, b.Key) }
	slices.SortFunc(preconditions, byKey)
	slices.SortFunc(uncleared, byKey)
	return append(preconditions, uncleared...)
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

// InheritedGuardKeysForPath returns, in order, the keys of GuardsForPath(path) that are
// inherited: those whose value comes from an entry that marks the key inherited. Nearer wins
// here as it does on the value, so a path that names a key of its own decides whether that key,
// at that path, is inherited.
func (r *ResourcePathAnnotations) InheritedGuardKeysForPath(path ResolvedPath) []string {
	if r == nil {
		return nil
	}
	decided := map[string]bool{}
	var inherited []string
	add := func(annotations PathAnnotations) {
		for key := range annotations[AnnotationKindGuard] {
			if decided[key] {
				continue
			}
			decided[key] = true
			if _, marked := annotations[AnnotationKindGuardInherited][key]; marked {
				inherited = append(inherited, key)
			}
		}
	}
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
	slices.Sort(inherited)
	return inherited
}

// GuardDelta is one change to the guards at one path: keys to set, and keys to remove.
//
// The direction matters, which is why a removal is a list of keys rather than an absent entry.
// Guards propagate by diff rather than by union, and applying a diff is what lets a *removal*
// travel: when a workload no longer needs hostNetwork and the base drops the exception, the
// variants drop it too, instead of accumulating stale guards nobody can account for.
//
// A merge's delta is about inherited guards, since those are the only ones that travel: what it
// sets arrives inherited, so it travels on from there, and what it removes is removed only where
// it had arrived as inherited -- a downstream's own guard with the same key is that Unit's
// decision, not the upstream's to retract.
//
// A restore puts back a Unit's own guards too, so its delta also says what it sets and removes
// as local guards (SetLocal, RemoveLocal).
type GuardDelta struct {
	Path        ResolvedPath      `json:",omitempty" description:"The path whose guards changed; empty for the resource as a whole"`
	Set         map[string]string `json:",omitempty" description:"Inherited guard keys added or changed, with their new values"`
	Remove      []string          `json:",omitempty" description:"Inherited guard keys removed"`
	SetLocal    map[string]string `json:",omitempty" description:"Guard keys added or changed as guards that stay with the Unit, with their new values; a restore putting back the Unit's own guards"`
	RemoveLocal []string          `json:",omitempty" description:"Guard keys removed that stay with the Unit, not inherited; a restore taking out guards the restored Revision did not have"`
}

// IsEmpty reports whether a delta says nothing.
func (d *GuardDelta) IsEmpty() bool {
	return d == nil || (len(d.Set) == 0 && len(d.Remove) == 0 && len(d.SetLocal) == 0 && len(d.RemoveLocal) == 0)
}

// ResourceGuardDiff is one resource's guard changes over a range.
type ResourceGuardDiff struct {
	Resource ResourceInfo
	Deltas   []GuardDelta
}

// DiffInheritedGuards computes the changes to inherited guards between two annotation tables --
// the source's, at the base and end of the range a merge is carrying.
//
// Only inherited guards are diffed: a local guard is about the source Unit and does not travel,
// and other annotation kinds do not propagate at all. A guard that stops being inherited inside
// the range reads as removed, which is what it is to the Units downstream: it has stopped being
// policy for them, though the source keeps it.
//
// Resources are matched by name and alias, so a rename inside the range does not read as one
// resource losing all its guards and another gaining them.
func DiffInheritedGuards(identifier ResourceIdentifier, base, end PathAnnotationList) []ResourceGuardDiff {
	base, end = InheritedGuards(base), InheritedGuards(end)
	var diffs []ResourceGuardDiff
	baseIndex := annotationIndex(identifier, base)
	endIndex := annotationIndex(identifier, end)
	for i := range end {
		endEntry := &end[i]
		var baseEntry *ResourcePathAnnotations
		if position, found := baseIndex.Find(endEntry.Resource, endEntry.AliasesWithoutScopes); found {
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
		if _, found := endIndex.Find(baseEntry.Resource, baseEntry.AliasesWithoutScopes); found {
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

// DiffGuards computes every guard change between two annotation tables, local and inherited: what
// a restore from end's Revision back over base would change. A key whose value or inheritance
// differs is set with end's; a key end does not have is removed, as inherited or local as base
// held it.
func DiffGuards(identifier ResourceIdentifier, base, end PathAnnotationList) []ResourceGuardDiff {
	var diffs []ResourceGuardDiff
	baseIndex := annotationIndex(identifier, base)
	endIndex := annotationIndex(identifier, end)
	for i := range end {
		endEntry := &end[i]
		var baseEntry *ResourcePathAnnotations
		if position, found := baseIndex.Find(endEntry.Resource, endEntry.AliasesWithoutScopes); found {
			baseEntry = &base[position]
		}
		if deltas := diffAllResourceGuards(baseEntry, endEntry); len(deltas) > 0 {
			diffs = append(diffs, ResourceGuardDiff{Resource: endEntry.Resource, Deltas: deltas})
		}
	}
	for i := range base {
		baseEntry := &base[i]
		if _, found := endIndex.Find(baseEntry.Resource, baseEntry.AliasesWithoutScopes); found {
			continue
		}
		if deltas := diffAllResourceGuards(baseEntry, &ResourcePathAnnotations{Resource: baseEntry.Resource}); len(deltas) > 0 {
			diffs = append(diffs, ResourceGuardDiff{Resource: baseEntry.Resource, Deltas: deltas})
		}
	}
	return diffs
}

// diffAllResourceGuards computes the per-path deltas, local and inherited, between two entries
// for the same resource.
func diffAllResourceGuards(base, end *ResourcePathAnnotations) []GuardDelta {
	var deltas []GuardDelta
	appendDelta := func(path ResolvedPath, baseAnnotations, endAnnotations PathAnnotations) {
		if delta := diffPathGuards(path, baseAnnotations, endAnnotations); !delta.IsEmpty() {
			deltas = append(deltas, delta)
		}
	}
	var baseResource PathAnnotations
	var baseByPath map[ResolvedPath]PathAnnotations
	if base != nil {
		baseResource, baseByPath = base.ResourceAnnotations, base.PathAnnotationMap
	}
	appendDelta("", baseResource, end.ResourceAnnotations)
	for path, annotations := range end.PathAnnotationMap {
		appendDelta(path, baseByPath[path], annotations)
	}
	for path, annotations := range baseByPath {
		if _, stillThere := end.PathAnnotationMap[path]; !stillThere {
			appendDelta(path, annotations, nil)
		}
	}
	slices.SortFunc(deltas, func(a, b GuardDelta) int { return strings.Compare(string(a.Path), string(b.Path)) })
	return deltas
}

// diffPathGuards is the delta that turns one path's guards, base, into end's, inheritance
// included.
func diffPathGuards(path ResolvedPath, base, end PathAnnotations) GuardDelta {
	delta := GuardDelta{Path: path}
	inherited := func(annotations PathAnnotations, key string) bool {
		_, marked := annotations[AnnotationKindGuardInherited][key]
		return marked
	}
	set := func(into *map[string]string, key, value string) {
		if *into == nil {
			*into = map[string]string{}
		}
		(*into)[key] = value
	}
	for key, value := range end[AnnotationKindGuard] {
		endInherited := inherited(end, key)
		if baseValue, present := base[AnnotationKindGuard][key]; present && baseValue == value &&
			inherited(base, key) == endInherited {
			continue
		}
		if endInherited {
			set(&delta.Set, key, value)
		} else {
			set(&delta.SetLocal, key, value)
		}
	}
	for key := range base[AnnotationKindGuard] {
		if _, present := end[AnnotationKindGuard][key]; present {
			continue
		}
		if inherited(base, key) {
			delta.Remove = append(delta.Remove, key)
		} else {
			delta.RemoveLocal = append(delta.RemoveLocal, key)
		}
	}
	slices.Sort(delta.Remove)
	slices.Sort(delta.RemoveLocal)
	return delta
}

// pathAnnotationsAt returns one path's annotations in an entry, or the resource's own for the
// empty path; nil when the entry has none there.
func pathAnnotationsAt(entry *ResourcePathAnnotations, path ResolvedPath) PathAnnotations {
	if entry == nil {
		return nil
	}
	if path == "" {
		return entry.ResourceAnnotations
	}
	return entry.PathAnnotationMap[path]
}

// RestoreGuards returns the annotation table a restore leaves, and the guard changes it withheld.
//
// The annotations go back with the data they annotate, so the table is restored's, except where
// that would change a path's guards without the restore being cleared for them. Changing the
// guards at a path is a guard edit, and an edit is checked against the guards the path has before
// it: a restore to a Revision from before a guard was set would otherwise remove the guard, and,
// with the data filtered by the guards it brings back, write the value under it in the same step.
// A path whose guards are withheld keeps the guards it has now, and the change it would have made
// is returned, for the caller to report as a conflict that can be applied later.
func RestoreGuards(identifier ResourceIdentifier, current, restored PathAnnotationList, clearance Clearance) (PathAnnotationList, []ResourceGuardDiff) {
	result := ClonePathAnnotationList(restored)
	var withheld []ResourceGuardDiff
	for _, diff := range DiffGuards(identifier, current, restored) {
		var currentEntry *ResourcePathAnnotations
		if position, found := findResourceAnnotations(identifier, current, diff.Resource); found {
			currentEntry = &current[position]
		}
		var kept []GuardDelta
		for _, delta := range diff.Deltas {
			now := pathAnnotationsAt(currentEntry, delta.Path)[AnnotationKindGuard]
			if admitted, _ := clearance.Admits(now); len(now) == 0 || admitted {
				continue
			}
			kept = append(kept, delta)
			var restoredAnnotations PathAnnotations
			if position, found := findResourceAnnotations(identifier, result, diff.Resource); found {
				restoredAnnotations = pathAnnotationsAt(&result[position], delta.Path)
			}
			back := diffPathGuards(delta.Path, restoredAnnotations, pathAnnotationsAt(currentEntry, delta.Path))
			result, _ = ApplyGuardDelta(identifier, result, diff.Resource, &back)
		}
		if len(kept) > 0 {
			withheld = append(withheld, ResourceGuardDiff{Resource: diff.Resource, Deltas: kept})
		}
	}
	return result, withheld
}

// ApplyGuardDelta applies one delta to a table entry, creating the entry if the resource has
// none. Returns whether anything changed.
func ApplyGuardDelta(identifier ResourceIdentifier, table PathAnnotationList, resource ResourceInfo, delta *GuardDelta) (PathAnnotationList, bool) {
	if delta.IsEmpty() {
		return table, false
	}
	position, found := findResourceAnnotations(identifier, table, resource)
	if !found {
		table = append(table, ResourcePathAnnotations{Resource: resource})
		position = len(table) - 1
	}
	entry := &table[position]

	changed := false
	if len(delta.Set) > 0 {
		changed = setGuardEntries(annotationsForPath(entry, delta.Path), delta.Set, markInherited)
	}
	if len(delta.SetLocal) > 0 {
		changed = setGuardEntries(annotationsForPath(entry, delta.Path), delta.SetLocal, markLocal) || changed
	}
	if len(delta.RemoveLocal) > 0 {
		var target PathAnnotations
		if delta.Path == "" {
			target = entry.ResourceAnnotations
		} else {
			target = entry.PathAnnotationMap[delta.Path]
		}
		for _, key := range delta.RemoveLocal {
			if _, present := target[AnnotationKindGuard][key]; !present {
				continue
			}
			if _, marked := target[AnnotationKindGuardInherited][key]; marked {
				continue
			}
			delete(target[AnnotationKindGuard], key)
			changed = true
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
			for _, key := range RemovedGuardKeys(target, delta) {
				delete(guards, key)
				delete(target[AnnotationKindGuardInherited], key)
				changed = true
			}
		}
	}
	return table, changed
}

// RemovedGuardKeys returns the keys a delta's Remove takes out of one path's annotations: those
// present there as inherited guards.
func RemovedGuardKeys(target PathAnnotations, delta *GuardDelta) []string {
	var removed []string
	for _, key := range delta.Remove {
		if _, present := target[AnnotationKindGuard][key]; !present {
			continue
		}
		if _, marked := target[AnnotationKindGuardInherited][key]; !marked {
			continue
		}
		removed = append(removed, key)
	}
	return removed
}

// GuardKeyLink is the guard key that says a Link maintains a path. Its value is the Link's
// UpdateType (Link=Insert, Link=Upsert, Link=TransformPaths), so a clearance can name the kinds of
// Link whose writes it understands.
const GuardKeyLink = "Link"

// GuardKeyTrigger is the guard key that says a Trigger maintains a path. Its value names what the
// Trigger does, such as the function it runs (Trigger=set-hash).
const GuardKeyTrigger = "Trigger"

// ClearanceAdmitting returns the clearance, extended if it has to be to admit the guard
// key=value: by adding value to its In requirement on key, or else by adding one. The clearance
// passed in is not modified.
func ClearanceAdmitting(clearance Clearance, key, value string) Clearance {
	if admitted, _ := clearance.Admits(map[string]string{key: value}); admitted {
		return clearance
	}
	extended := make(Clearance, len(clearance), len(clearance)+1)
	for i, requirement := range clearance {
		requirement.Values = slices.Clone(requirement.Values)
		extended[i] = requirement
	}
	for i := range extended {
		if extended[i].Key == key && extended[i].Operator == ClearanceOperatorIn {
			extended[i].Values = append(extended[i].Values, value)
			return extended
		}
	}
	return append(extended, ClearanceRequirement{Key: key, Operator: ClearanceOperatorIn, Values: []string{value}})
}

// CheckGuardStampCleared refuses guards a change would record that its own clearance does not
// cover. A change is withheld by every guard it is not cleared for, its own included, so a stamp
// the change is not cleared for stops it the next time it writes the same path: the next
// Revision of a replayed merge, the next run of a Trigger, the next resolve of a Link, the same
// edit made again. The guards and the clearance that covers them are stated together.
func CheckGuardStampCleared(stamp GuardStamp, clearance Clearance) error {
	// One key at a time, in order, so that the guard an error names does not depend on map
	// iteration.
	for _, key := range slices.Sorted(maps.Keys(stamp)) {
		admitted, withheld := clearance.Admits(map[string]string{key: stamp[key]})
		if !admitted {
			return guardStampNotClearedError(withheld)
		}
	}
	return nil
}

func guardStampNotClearedError(withheld WithheldGuard) error {
	if withheld.Precondition {
		return fmt.Errorf("the guards include %s=%s, which the clearance forbids with !%s; a change is withheld by guards it is not cleared for, including its own",
			withheld.Key, withheld.Value, withheld.Key)
	}
	return fmt.Errorf("the guards include %s=%s, which the clearance does not cover; a change is withheld by guards it is not cleared for, including its own, so add the clearance %s",
		withheld.Key, withheld.Value, ClearanceSuggestion(withheld))
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
