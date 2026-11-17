// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"slices"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/spf13/cobra"
)

var linkCmd = &cobra.Command{
	Use:   "link",
	Short: "Link commands",
	Long: getCommandHelp(`The link subcommands are used to manage links.

Links are explained at https://docs.confighub.com/background/entities/link/.
A guide for how to use links is at https://docs.confighub.com/guide/dependencies/.`, ""),
	PersistentPreRunE: spacePreRunE,
}

var (
	linkUpdateType                   string
	linkAutoUpdate                   bool
	linkNoAutoUpdate                 bool
	linkWhereMutation                string
	linkWhereResource                string
	linkMergeEnableSubtraction       bool
	linkNoMergeEnableSubtraction     bool
	linkProtect                      bool
	linkNoProtect                    bool
	linkClearance                    []string
	linkGuardSpecs                   []string
	linkSquash                       bool
	linkNoSquash                     bool
	linkMakeCurrent                  bool
	linkUpstreamLastMergedRevision   int64
	linkDownstreamLastMergedRevision int64
	linkTransformInvocation          string
)

// addLinkUpdateTypeFlag registers --update-type, which only a create takes: a Link's UpdateType is
// immutable.
func addLinkUpdateTypeFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&linkUpdateType, "update-type", "", "link update type (NeedsProvides, MergeUnits, UpgradeUnit, None, Insert, Upsert, or TransformPaths); the default is a NeedsProvides link that updates itself")
}

func addLinkFieldFlags(cmd *cobra.Command) {
	addLinkElementFlags(cmd)
	cmd.Flags().BoolVar(&linkAutoUpdate, "auto-update", false, "enable automatic downstream unit updates when upstream changes; a link created with no --update-type gets this without asking")
	cmd.Flags().BoolVar(&linkNoAutoUpdate, "no-auto-update", false, "disable automatic downstream unit updates; a link that does not update itself instead reports Stale when its upstream moves past it")
	cmd.Flags().StringVar(&linkWhereMutation, "where-mutation", "", "where expression selecting mutations of the downstream unit whose paths this link's merges must not overwrite; on top of the unit's guards, so it holds more and never re-opens a guarded path")
	cmd.Flags().StringVar(&linkWhereResource, "where-resource", "", "where expression to select upstream resources for propagation")
	cmd.Flags().BoolVar(&linkMergeEnableSubtraction, "merge-enable-subtraction", false, "also subtract the downstream unit's local differences from the patch when resolving this link, on top of the Override guards that hold its local overrides")
	cmd.Flags().BoolVar(&linkNoMergeEnableSubtraction, "no-merge-enable-subtraction", false, "return this link to the default: no subtraction step")
	cmd.Flags().BoolVar(&linkProtect, "protect", false, "record the guard Link=<update-type> on the paths this link's resolve writes, with the clearance for it; refused on UpgradeUnit and MergeUnits links")
	_ = cmd.Flags().MarkDeprecated("protect", "use --guard Link=<update-type> with --clearance Link=<update-type>; --protect records that guard and clearance on the link")
	cmd.Flags().BoolVar(&linkNoProtect, "no-protect", false, "deprecated: remove the guard Link=<update-type> that --protect records, and the clearance for it")
	_ = cmd.Flags().MarkDeprecated("no-protect", "Protect is recorded as the link's guard Link=<update-type> and the clearance for it, not stored; update --guard and --clearance to change them")
	cmd.Flags().StringArrayVar(&linkClearance, "clearance", nil,
		"class of guarded reason this link's resolve is cleared for, as KEY, KEY=VALUE[;VALUE...], KEY!=VALUE[;VALUE...], or !KEY to refuse any path carrying KEY (repeatable). Pass an empty value to clear")
	cmd.Flags().StringArrayVar(&linkGuardSpecs, "guard", nil,
		"reason to record on the paths this link's resolve writes, as KEY=VALUE (repeatable). A later operation must be cleared for it before overwriting those paths, the link's next resolve included, so --clearance has to cover it; refused on UpgradeUnit and MergeUnits links, whose inherited guards propagate from upstream. Pass an empty value to clear")
	cmd.Flags().BoolVar(&linkSquash, "squash", false, "merge this link's range as one rebased diff in one revision instead of walking it: by default a resolve re-runs the upstream's recorded function invocations against the downstream unit where it can, and records one revision per upstream revision that has an effect; only meaningful for UpgradeUnit and MergeUnits links")
	cmd.Flags().BoolVar(&linkNoSquash, "no-squash", false, "return this link to the default: its resolve walks the range")
	cmd.Flags().BoolVar(&linkMakeCurrent, "make-current", false, "set link revision numbers to current unit revisions; on create this skips the initial merge, on update it re-points the link at what the units now hold")
	cmd.Flags().Int64Var(&linkUpstreamLastMergedRevision, "upstream-last-merged-revision", 0, "set UpstreamLastMergedRevisionNum explicitly: the upstream revision the link is treated as merged through")
	cmd.Flags().Int64Var(&linkDownstreamLastMergedRevision, "downstream-last-merged-revision", 0, "set DownstreamLastMergedRevisionNum explicitly: the downstream revision the last merge produced")
	cmd.Flags().StringVar(&linkTransformInvocation, "transform-invocation", "", "Invocation slug (or space/slug, or UUID) whose function transforms upstream data before it is inserted or upserted; only valid with --update-type Insert or Upsert")
}

func validateLinkFieldFlags(cmd *cobra.Command) error {
	if linkAutoUpdate && linkNoAutoUpdate {
		return fmt.Errorf("--auto-update and --no-auto-update are mutually exclusive")
	}
	if linkMergeEnableSubtraction && linkNoMergeEnableSubtraction {
		return fmt.Errorf("--merge-enable-subtraction and --no-merge-enable-subtraction are mutually exclusive")
	}
	if linkProtect && linkNoProtect {
		return fmt.Errorf("--protect and --no-protect are mutually exclusive")
	}
	if linkSquash && linkNoSquash {
		return fmt.Errorf("--squash and --no-squash are mutually exclusive")
	}
	// --make-current writes the same two fields, computed from the Units. Letting both
	// through would make the winner depend on ordering rather than on what was asked for.
	if linkMakeCurrent && (cmd.Flags().Changed("upstream-last-merged-revision") || cmd.Flags().Changed("downstream-last-merged-revision")) {
		return fmt.Errorf("--make-current and --upstream-last-merged-revision/--downstream-last-merged-revision are mutually exclusive")
	}
	if linkUpstreamLastMergedRevision < 0 {
		return fmt.Errorf("--upstream-last-merged-revision must be >= 0, got %d", linkUpstreamLastMergedRevision)
	}
	if linkDownstreamLastMergedRevision < 0 {
		return fmt.Errorf("--downstream-last-merged-revision must be >= 0, got %d", linkDownstreamLastMergedRevision)
	}
	if linkUpdateType != "" && linkUpdateType != "NeedsProvides" && linkUpdateType != "MergeUnits" && linkUpdateType != "UpgradeUnit" && linkUpdateType != "None" && linkUpdateType != "Insert" && linkUpdateType != "Upsert" && linkUpdateType != "TransformPaths" {
		return fmt.Errorf("--update-type must be NeedsProvides, MergeUnits, UpgradeUnit, None, Insert, Upsert, or TransformPaths, got %q", linkUpdateType)
	}
	return queueLinkElementEdits()
}

// makeCurrentPointers returns the merged-revision pointers that mark a link as
// caught up with the two Units it connects: the downstream (From) Unit's head
// revision and the upstream (To) Unit's head revision.
//
// On a new link this skips the initial merge. On an existing link it re-points
// the link at what the Units actually hold, which repairs a link whose pointers
// name a revision that does not exist.
func makeCurrentPointers(fromUnit, toUnit *goclientnew.Unit) (upstream, downstream int64) {
	return toUnit.HeadRevisionNum, fromUnit.HeadRevisionNum
}

// resolveMakeCurrentPointers fetches an existing link's two Units and returns the
// pointers that would make it current.
func resolveMakeCurrentPointers(link *goclientnew.Link) (upstream, downstream int64, err error) {
	fromUnit, err := resolveUnit(link.FromUnitID.String(), link.SpaceID.String(), "*")
	if err != nil {
		return 0, 0, err
	}
	toUnit, err := resolveUnit(link.ToUnitID.String(), link.ToSpaceID.String(), "*")
	if err != nil {
		return 0, 0, err
	}
	upstream, downstream = makeCurrentPointers(fromUnit.Unit, toUnit.Unit)
	return upstream, downstream, nil
}

// resolveLinkToUnit resolves the upstream (To) unit of a link from the positional arguments
// <to unit> [<to space>] in args. A bare slug is looked up in the to space when there is one,
// and in spaceID otherwise.
//
// The link's ToSpaceID is the SpaceID of the unit this returns, not the space the lookup
// started from: a unit named as <space>/<slug> or by UUID is found wherever it is. A to space
// that the unit turns out not to be in is refused rather than ignored.
func resolveLinkToUnit(args []string, spaceID string) (*goclientnew.ExtendedUnit, error) {
	if len(args) < 2 {
		return resolveUnit(args[0], spaceID, "*") // get all fields for now
	}
	toSpace, err := resolveSpace(args[1], "*") // get all fields for now
	if err != nil {
		return nil, err
	}
	toUnit, err := resolveUnit(args[0], toSpace.Space.SpaceID.String(), "*") // get all fields for now
	if err != nil {
		return nil, err
	}
	if toUnit.Unit.SpaceID != toSpace.Space.SpaceID {
		return nil, fmt.Errorf("unit %s is not in space %s", args[0], args[1])
	}
	return toUnit, nil
}

// withMakeCurrentPointers wraps base so the patch also carries the make-current
// pointers for one specific link. The values are per-link, so unlike the other
// link field flags they cannot be folded into linkFieldsEnhancer.
func withMakeCurrentPointers(base PatchEnhancer, upstream, downstream int64) PatchEnhancer {
	return func(patchMap map[string]interface{}) {
		if base != nil {
			base(patchMap)
		}
		patchMap["UpstreamLastMergedRevisionNum"] = upstream
		patchMap["DownstreamLastMergedRevisionNum"] = downstream
	}
}

// setLinkFieldsOnCreate sets link-specific fields on a new Link for create operations.
func setLinkFieldsOnCreate(link *goclientnew.Link, cmd *cobra.Command) error {
	// The flag's default is empty so that this can tell "no update type asked for" from a
	// NeedsProvides one asked for by name. The first is the legacy default link -- a
	// NeedsProvides link that updates itself -- which is now said rather than left blank for
	// the server to infer. The second is a link whose kind the caller chose, and it gets
	// AutoUpdate only if the caller asked for that too.
	if linkUpdateType == "" {
		link.UpdateType = "NeedsProvides"
		link.AutoUpdate = !linkNoAutoUpdate
	} else {
		link.UpdateType = linkUpdateType
		link.AutoUpdate = linkAutoUpdate
	}
	link.WhereMutation = linkWhereMutation
	link.WhereResource = linkWhereResource
	if linkMergeEnableSubtraction {
		link.MergeEnableSubtraction = true
	}
	if linkProtect {
		link.Protect = true
	}
	if cmd.Flags().Changed("clearance") {
		clearance, err := parseClearanceSpecs(linkClearance)
		if err != nil {
			return err
		}
		link.Clearance = &clearance
	}
	if cmd.Flags().Changed("guard") {
		guards, err := parseGuardStampSpecs(linkGuardSpecs)
		if err != nil {
			return err
		}
		if guards == nil {
			guards = goclientnew.GuardStamp{}
		}
		link.Guards = &guards
	}
	if linkSquash {
		link.Squash = true
	}
	if cmd.Flags().Changed("upstream-last-merged-revision") {
		link.UpstreamLastMergedRevisionNum = linkUpstreamLastMergedRevision
	}
	if cmd.Flags().Changed("downstream-last-merged-revision") {
		link.DownstreamLastMergedRevisionNum = linkDownstreamLastMergedRevision
	}
	if linkTransformInvocation != "" {
		id, err := resolveInvocationID(linkTransformInvocation)
		if err != nil {
			return fmt.Errorf("--transform-invocation: %w", err)
		}
		uid := openapi_types.UUID(id)
		link.TransformInvocationID = &uid
	}
	return nil
}

// setLinkFieldsOnUpdate sets link-specific fields on an existing Link for update operations.
// Only sets fields that were explicitly changed via flags.
func setLinkFieldsOnUpdate(link *goclientnew.Link, cmd *cobra.Command) error {
	if linkAutoUpdate {
		link.AutoUpdate = true
	} else if linkNoAutoUpdate {
		link.AutoUpdate = false
	}
	if cmd.Flags().Changed("where-mutation") {
		link.WhereMutation = linkWhereMutation
	}
	if cmd.Flags().Changed("where-resource") {
		link.WhereResource = linkWhereResource
	}
	if linkMergeEnableSubtraction {
		link.MergeEnableSubtraction = true
	} else if linkNoMergeEnableSubtraction {
		link.MergeEnableSubtraction = false
	}
	if linkProtect {
		link.Protect = true
	}
	if cmd.Flags().Changed("clearance") {
		clearance, err := parseClearanceSpecs(linkClearance)
		if err != nil {
			return err
		}
		link.Clearance = &clearance
	}
	if cmd.Flags().Changed("guard") {
		guards, err := parseGuardStampSpecs(linkGuardSpecs)
		if err != nil {
			return err
		}
		if guards == nil {
			guards = goclientnew.GuardStamp{}
		}
		link.Guards = &guards
	}
	if linkNoProtect {
		unprotectLink(link, !cmd.Flags().Changed("guard"), !cmd.Flags().Changed("clearance"))
	}
	if linkSquash {
		link.Squash = true
	} else if linkNoSquash {
		link.Squash = false
	}
	if cmd.Flags().Changed("upstream-last-merged-revision") {
		link.UpstreamLastMergedRevisionNum = linkUpstreamLastMergedRevision
	}
	if cmd.Flags().Changed("downstream-last-merged-revision") {
		link.DownstreamLastMergedRevisionNum = linkDownstreamLastMergedRevision
	}
	if cmd.Flags().Changed("transform-invocation") {
		if linkTransformInvocation == "" {
			link.TransformInvocationID = nil
		} else {
			id, err := resolveInvocationID(linkTransformInvocation)
			if err != nil {
				return fmt.Errorf("--transform-invocation: %w", err)
			}
			uid := openapi_types.UUID(id)
			link.TransformInvocationID = &uid
		}
	}
	return nil
}

// linkFieldsEnhancer creates a PatchEnhancer for link-specific fields in patch/bulk operations.
func linkFieldsEnhancer(cmd *cobra.Command) PatchEnhancer {
	if !hasLinkFieldFlags(cmd) {
		return nil
	}
	return func(patchMap map[string]interface{}) {
		// Only a create has the flag, where it sets the type of the links a bulk create makes.
		if cmd.Flags().Changed("update-type") {
			patchMap["UpdateType"] = linkUpdateType
		}
		if linkAutoUpdate {
			patchMap["AutoUpdate"] = true
		} else if linkNoAutoUpdate {
			patchMap["AutoUpdate"] = false
		}
		if cmd.Flags().Changed("where-mutation") {
			patchMap["WhereMutation"] = linkWhereMutation
		}
		if cmd.Flags().Changed("where-resource") {
			patchMap["WhereResource"] = linkWhereResource
		}
		if linkMergeEnableSubtraction {
			patchMap["MergeEnableSubtraction"] = true
		} else if linkNoMergeEnableSubtraction {
			patchMap["MergeEnableSubtraction"] = false
		}
		// A malformed flag leaves the field out rather than failing, since a PatchEnhancer
		// has no way to report an error. Both are validated again on the way in, so a
		// malformed one is refused at the create and update paths that can say so.
		if cmd.Flags().Changed("clearance") {
			clearance, err := parseClearanceSpecs(linkClearance)
			if err == nil {
				patchMap["Clearance"] = clearance
			}
		}
		if cmd.Flags().Changed("guard") {
			guards, err := parseGuardStampSpecs(linkGuardSpecs)
			if err == nil {
				if guards == nil {
					guards = goclientnew.GuardStamp{}
				}
				patchMap["Guards"] = guards
			}
		}
		if linkProtect {
			patchMap["Protect"] = true
		} else if linkNoProtect && !cmd.Flags().Changed("guard") {
			// Protect is the guard Link=<update-type>, which a merge patch takes off by naming
			// the key with null. Each Link's own type is not known here, so its clearance for
			// the guard stays; it admits a guard the Link no longer sets.
			patchMap["Guards"] = map[string]any{"Link": nil}
		}
		if linkSquash {
			patchMap["Squash"] = true
		} else if linkNoSquash {
			patchMap["Squash"] = false
		}
		if cmd.Flags().Changed("upstream-last-merged-revision") {
			patchMap["UpstreamLastMergedRevisionNum"] = linkUpstreamLastMergedRevision
		}
		if cmd.Flags().Changed("downstream-last-merged-revision") {
			patchMap["DownstreamLastMergedRevisionNum"] = linkDownstreamLastMergedRevision
		}
		if cmd.Flags().Changed("transform-invocation") {
			if linkTransformInvocation == "" {
				patchMap["TransformInvocationID"] = nil
			} else if id, err := resolveInvocationID(linkTransformInvocation); err == nil {
				patchMap["TransformInvocationID"] = id.String()
			}
		}
	}
}

// unprotectLink undoes what Protect recorded on a Link: the guard Link=<update-type>, where it has
// that value, and the clearance for it. The server records Protect as that guard and clearance
// rather than storing it, so Protect false says nothing to it. guards and clearance say whether
// each is the command's to change, rather than set by --guard or --clearance.
func unprotectLink(link *goclientnew.Link, guards, clearance bool) {
	updateType := link.UpdateType
	if guards && link.Guards != nil && (*link.Guards)["Link"] == updateType {
		stamp := goclientnew.GuardStamp{}
		for key, value := range *link.Guards {
			if key != "Link" {
				stamp[key] = value
			}
		}
		link.Guards = &stamp
	}
	if clearance && link.Clearance != nil {
		kept := goclientnew.Clearance{}
		for _, requirement := range *link.Clearance {
			if requirement.Key == "Link" && requirement.Operator == "In" {
				requirement.Values = slices.DeleteFunc(slices.Clone(requirement.Values), func(v string) bool { return v == updateType })
				if len(requirement.Values) == 0 {
					continue
				}
			}
			kept = append(kept, requirement)
		}
		link.Clearance = &kept
	}
}

// hasLinkFieldFlags returns true if any link-specific field flags were explicitly set.
// linkMakeCurrent is included so that --make-current alone satisfies the "requires
// one of" guard on the patch paths, even though its per-link values are applied
// outside linkFieldsEnhancer.
func hasLinkFieldFlags(cmd *cobra.Command) bool {
	return cmd.Flags().Changed("update-type") || linkAutoUpdate || linkNoAutoUpdate ||
		cmd.Flags().Changed("where-mutation") || cmd.Flags().Changed("where-resource") ||
		linkMergeEnableSubtraction || linkNoMergeEnableSubtraction ||
		linkProtect || linkNoProtect ||
		cmd.Flags().Changed("clearance") ||
		cmd.Flags().Changed("guard") ||
		linkSquash || linkNoSquash ||
		linkMakeCurrent ||
		cmd.Flags().Changed("upstream-last-merged-revision") ||
		cmd.Flags().Changed("downstream-last-merged-revision") ||
		cmd.Flags().Changed("transform-invocation")
}

func buildWhereClauseFromLinks(linkIds []string) (string, error) {
	return buildWhereClauseFromIdentifiers(linkIds, "LinkID", "Slug")
}

// buildLinkBulkEffectiveWhere builds the effective where clause for bulk link
// operations. If linkIdents are supplied, they are converted to a LinkID/Slug
// IN clause; otherwise the user-supplied where clause is used. The space
// constraint is then appended unless the caller passed "*" for spaceID.
func buildLinkBulkEffectiveWhere(linkIdents []string, whereClause, spaceID string) (string, error) {
	var ew string
	if len(linkIdents) > 0 {
		wc, err := buildWhereClauseFromLinks(linkIdents)
		if err != nil {
			return "", err
		}
		ew = wc
	} else {
		ew = whereClause
	}
	return addSpaceIDToWhereClause(ew, spaceID), nil
}

// buildLinkIDsWhere returns "LinkID IN ('uuid1','uuid2',...)" for the given
// link IDs, or "LinkID = '<id>'" for a single ID. Returns "" for an empty list.
func buildLinkIDsWhere(linkIDs []uuid.UUID) string {
	if len(linkIDs) == 0 {
		return ""
	}
	if len(linkIDs) == 1 {
		return fmt.Sprintf("LinkID = '%s'", linkIDs[0])
	}
	quoted := make([]string, len(linkIDs))
	for i, id := range linkIDs {
		quoted[i] = "'" + id.String() + "'"
	}
	return "LinkID IN (" + strings.Join(quoted, ",") + ")"
}

func init() {
	addSpaceFlags(linkCmd)
	rootCmd.AddCommand(linkCmd)
	addExplainCmd(linkCmd, "Link")
}
