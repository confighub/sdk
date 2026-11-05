// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// A conflict dismissal records that the merge conflicts of some reason at a path are understood
// and expected: a variant that overrides a value its base keeps changing, a guard that is meant
// to withhold what the base sends. A merge that reports such a conflict again leaves it dismissed
// rather than outstanding, so the outstanding set -- and a Trigger gating on it -- shows only
// what someone still has to decide.
//
// It is an annotation of kind AnnotationKindConflictDismissal, on the path the conflicts are at
// or on the resource as a whole for conflicts at any of its paths. A path may name a list element
// or a map key with "*" for any one. Each key names what is dismissed there:
//
//   - "<Reason>", such as "Subtracted": every conflict of that reason.
//   - "<Reason>-<guard key>", for a reason a guard causes (Guarded, GuardWithheld): a conflict
//     withheld by that guard. The value is the guard's value, or empty for any value. A
//     conflict several guards withheld is dismissed only when each of them is, since each is a
//     reason the write did not land.
//
// The value of a reason key is unused and empty.

// conflictDismissalGuardSeparator joins a reason and a guard key. Unambiguous because no reason
// contains one: the key is split at the first.
const conflictDismissalGuardSeparator = "-"

// knownConflictReasons is every reason a merge reports.
var knownConflictReasons = []ConflictReason{
	ConflictReasonSubtracted,
	ConflictReasonDeleteShadowed,
	ConflictReasonProtectedPath,
	ConflictReasonUnresolvedPath,
	ConflictReasonExclusiveWithheld,
	ConflictReasonExclusiveCleared,
	ConflictReasonGuarded,
	ConflictReasonGuardWithheld,
	ConflictReasonReplayFailed,
}

// KnownConflictReasons returns every reason a merge reports.
func KnownConflictReasons() []ConflictReason {
	return append([]ConflictReason(nil), knownConflictReasons...)
}

// ConflictReasonIsKnown reports whether a reason is one a merge reports.
func ConflictReasonIsKnown(reason ConflictReason) bool {
	return slices.Contains(knownConflictReasons, reason)
}

// ConflictReasonHasGuards reports whether conflicts of a reason name the guards that caused them.
func ConflictReasonHasGuards(reason ConflictReason) bool {
	return reason == ConflictReasonGuarded || reason == ConflictReasonGuardWithheld
}

// ConflictDismissalKey is the annotation key dismissing conflicts of a reason, or, with a guard
// key, those of the reason a guard of that key withheld.
func ConflictDismissalKey(reason ConflictReason, guardKey string) string {
	if guardKey == "" {
		return string(reason)
	}
	return string(reason) + conflictDismissalGuardSeparator + guardKey
}

// ParseConflictDismissalKey splits a dismissal key into its reason and guard key, which is empty
// for a key dismissing the whole reason.
func ParseConflictDismissalKey(key string) (ConflictReason, string) {
	reason, guardKey, _ := strings.Cut(key, conflictDismissalGuardSeparator)
	return ConflictReason(reason), guardKey
}

// ConflictGuards returns the guards that withheld a conflict's write: Guards, or for a conflict
// recorded before Guards was, the one Guard it names.
func ConflictGuards(conflict *MutationConflict) []WithheldGuard {
	if len(conflict.Guards) > 0 {
		return conflict.Guards
	}
	if conflict.Guard != nil {
		return []WithheldGuard{*conflict.Guard}
	}
	return nil
}

// GuardSelector names a guard: a key, with any value or with one.
type GuardSelector struct {
	Key   string
	Value string
	// AnyValue is set for a selector written as a key alone.
	AnyValue bool
}

// ParseGuardSelector reads a guard named as "key" or "key=value".
func ParseGuardSelector(text string) (GuardSelector, error) {
	key, value, hasValue := strings.Cut(text, "=")
	key = strings.TrimSpace(key)
	if err := ValidateAnnotationKey(key); err != nil {
		return GuardSelector{}, fmt.Errorf("guard %q: %w", text, err)
	}
	if !hasValue {
		return GuardSelector{Key: key, AnyValue: true}, nil
	}
	if err := ValidateAnnotationValue(key, value); err != nil {
		return GuardSelector{}, fmt.Errorf("guard %q: %w", text, err)
	}
	return GuardSelector{Key: key, Value: value}, nil
}

// ParseGuardSelectors reads a list of guards named as "key" or "key=value".
func ParseGuardSelectors(texts []string) ([]GuardSelector, error) {
	selectors := make([]GuardSelector, 0, len(texts))
	for _, text := range texts {
		selector, err := ParseGuardSelector(text)
		if err != nil {
			return nil, err
		}
		selectors = append(selectors, selector)
	}
	return selectors, nil
}

// Matches reports whether a selector names a guard.
func (g GuardSelector) Matches(guard WithheldGuard) bool {
	return g.Key == guard.Key && (g.AnyValue || g.Value == guard.Value)
}

// GuardsSelected reports whether the selectors name every guard that withheld a conflict. A
// conflict no guard withheld is not selected by naming guards.
func GuardsSelected(selectors []GuardSelector, guards []WithheldGuard) bool {
	if len(guards) == 0 {
		return false
	}
	for _, guard := range guards {
		named := false
		for _, selector := range selectors {
			if selector.Matches(guard) {
				named = true
				break
			}
		}
		if !named {
			return false
		}
	}
	return true
}

// ConflictDismissed reports whether the dismissal entries of a path cover a conflict there.
func ConflictDismissed(entries map[string]string, conflict *MutationConflict) bool {
	if len(entries) == 0 {
		return false
	}
	if _, dismissed := entries[ConflictDismissalKey(conflict.Reason, "")]; dismissed {
		return true
	}
	guards := ConflictGuards(conflict)
	if len(guards) == 0 {
		return false
	}
	for _, guard := range guards {
		value, dismissed := entries[ConflictDismissalKey(conflict.Reason, guard.Key)]
		if !dismissed || (value != "" && value != guard.Value) {
			return false
		}
	}
	return true
}

// HasConflictDismissals reports whether a table records any conflict dismissal, which is what a
// merge checks before doing any work to apply them.
func HasConflictDismissals(list PathAnnotationList) bool {
	for i := range list {
		if len(list[i].ResourceAnnotations[AnnotationKindConflictDismissal]) > 0 {
			return true
		}
		for _, annotations := range list[i].PathAnnotationMap {
			if len(annotations[AnnotationKindConflictDismissal]) > 0 {
				return true
			}
		}
	}
	return false
}

// ConflictDismissalEntries returns the dismissal keys a request records: the reason alone, or,
// with guards, one key per guard.
func ConflictDismissalEntries(reason ConflictReason, guards []GuardSelector) (map[string]string, error) {
	if !ConflictReasonIsKnown(reason) {
		return nil, fmt.Errorf("a dismissal names the reason of the conflicts it dismisses, one of %s; got %q",
			conflictReasonList(), reason)
	}
	if len(guards) == 0 {
		return map[string]string{ConflictDismissalKey(reason, ""): ""}, nil
	}
	if !ConflictReasonHasGuards(reason) {
		return nil, fmt.Errorf("guards name the cause of %s and %s conflicts, not of %s",
			ConflictReasonGuarded, ConflictReasonGuardWithheld, reason)
	}
	entries := map[string]string{}
	for _, guard := range guards {
		key := ConflictDismissalKey(reason, guard.Key)
		if err := ValidateAnnotationKey(key); err != nil {
			return nil, fmt.Errorf("guard %s: %w", guard.Key, err)
		}
		value := guard.Value
		if guard.AnyValue {
			value = ""
		}
		entries[key] = value
	}
	return entries, nil
}

func conflictReasonList() string {
	names := make([]string, len(knownConflictReasons))
	for i, reason := range knownConflictReasons {
		names[i] = string(reason)
	}
	return strings.Join(names, ", ")
}

// SetConflictDismissal records dismissal entries for a resource's path, or for the resource as a
// whole when the path is empty, and returns the table. The table is modified in place where it
// has an entry for the resource and extended where it does not. The path must already be in the
// form the table is keyed in.
func SetConflictDismissal(identifier ResourceIdentifier, table PathAnnotationList, resource ResourceInfo,
	path ResolvedPath, entries map[string]string) PathAnnotationList {
	position, found := findResourceAnnotations(identifier, table, resource)
	if !found {
		table = append(table, ResourcePathAnnotations{Resource: resource})
		position = len(table) - 1
	}
	target := annotationsForPath(&table[position], path)
	dismissals := target[AnnotationKindConflictDismissal]
	if dismissals == nil {
		dismissals = map[string]string{}
		target[AnnotationKindConflictDismissal] = dismissals
	}
	maps.Copy(dismissals, entries)
	return table
}

// RemoveConflictDismissals removes the dismissal entries of one resource that a request names,
// at the paths pathSelected accepts (the empty path being the resource as a whole), and returns
// how many it removed. An empty reason names every reason; guards, when given, name only the
// entries for those guards: a guard named by key alone, its entry whatever value it dismisses, and
// one named with a value, the entry dismissing that value. Empty maps are left for the caller to
// prune.
func RemoveConflictDismissals(identifier ResourceIdentifier, table PathAnnotationList, resource ResourceInfo,
	pathSelected func(ResolvedPath) bool, reason ConflictReason, guards []GuardSelector) int {
	position, found := findResourceAnnotations(identifier, table, resource)
	if !found {
		return 0
	}
	entry := &table[position]
	removed := 0
	remove := func(dismissals map[string]string) {
		for key := range dismissals {
			keyReason, guardKey := ParseConflictDismissalKey(key)
			if reason != "" && keyReason != reason {
				continue
			}
			if len(guards) > 0 {
				named := false
				for _, guard := range guards {
					if guard.Key == guardKey && (guard.AnyValue || guard.Value == dismissals[key]) {
						named = true
						break
					}
				}
				if !named {
					continue
				}
			}
			delete(dismissals, key)
			removed++
		}
	}
	if pathSelected("") {
		remove(entry.ResourceAnnotations[AnnotationKindConflictDismissal])
	}
	for path, annotations := range entry.PathAnnotationMap {
		if pathSelected(path) {
			remove(annotations[AnnotationKindConflictDismissal])
		}
	}
	return removed
}
