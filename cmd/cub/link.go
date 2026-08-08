// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
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
	linkMakeCurrent                  bool
	linkUpstreamLastMergedRevision   int64
	linkDownstreamLastMergedRevision int64
	linkTransformInvocation          string
)

func addLinkFieldFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&linkUpdateType, "update-type", "", "link update type (NeedsProvides, MergeUnits, UpgradeUnit, None, Insert, Upsert, or TransformPaths)")
	cmd.Flags().BoolVar(&linkAutoUpdate, "auto-update", false, "enable automatic downstream unit updates when upstream changes")
	cmd.Flags().BoolVar(&linkNoAutoUpdate, "no-auto-update", false, "disable automatic downstream unit updates")
	cmd.Flags().StringVar(&linkWhereMutation, "where-mutation", "", "where expression to filter mutations during merge")
	cmd.Flags().StringVar(&linkWhereResource, "where-resource", "", "where expression to select upstream resources for propagation")
	cmd.Flags().BoolVar(&linkMergeEnableSubtraction, "merge-enable-subtraction", false, "also subtract the downstream unit's local differences from the patch when resolving this link, on top of the stored path protection that preserves overrides by default")
	cmd.Flags().BoolVar(&linkNoMergeEnableSubtraction, "no-merge-enable-subtraction", false, "return this link to the default: no subtraction step")
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
	return nil
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
	fromUnit, err := apiGetUnitInSpace(link.FromUnitID.String(), link.SpaceID.String(), "*")
	if err != nil {
		return 0, 0, err
	}
	toUnit, err := apiGetUnitInSpace(link.ToUnitID.String(), link.ToSpaceID.String(), "*")
	if err != nil {
		return 0, 0, err
	}
	upstream, downstream = makeCurrentPointers(fromUnit, toUnit)
	return upstream, downstream, nil
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
	link.UpdateType = linkUpdateType
	if linkAutoUpdate {
		link.AutoUpdate = true
	}
	link.WhereMutation = linkWhereMutation
	link.WhereResource = linkWhereResource
	if linkMergeEnableSubtraction {
		link.MergeEnableSubtraction = true
	}
	if cmd.Flags().Changed("upstream-last-merged-revision") {
		link.UpstreamLastMergedRevisionNum = linkUpstreamLastMergedRevision
	}
	if cmd.Flags().Changed("downstream-last-merged-revision") {
		link.DownstreamLastMergedRevisionNum = linkDownstreamLastMergedRevision
	}
	if linkTransformInvocation != "" {
		id, err := parseInvocationSlug(linkTransformInvocation)
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
	if cmd.Flags().Changed("update-type") {
		link.UpdateType = linkUpdateType
	}
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
			id, err := parseInvocationSlug(linkTransformInvocation)
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
		if cmd.Flags().Changed("upstream-last-merged-revision") {
			patchMap["UpstreamLastMergedRevisionNum"] = linkUpstreamLastMergedRevision
		}
		if cmd.Flags().Changed("downstream-last-merged-revision") {
			patchMap["DownstreamLastMergedRevisionNum"] = linkDownstreamLastMergedRevision
		}
		if cmd.Flags().Changed("transform-invocation") {
			if linkTransformInvocation == "" {
				patchMap["TransformInvocationID"] = nil
			} else if id, err := parseInvocationSlug(linkTransformInvocation); err == nil {
				patchMap["TransformInvocationID"] = id.String()
			}
		}
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
