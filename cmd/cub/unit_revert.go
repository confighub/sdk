// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var revertArgs struct {
	changeDescription string
	changeset         string
}

// revertRevisionsHelp describes the revisions argument both revert commands take.
const revertRevisionsHelp = `What to revert is one change, or a range of revisions:

  7                          revision 7
  ChangeSet:<changeset>      the revisions of a changeset
  Tag:<tag>                  the revision the tag marks
  <from>..<to>               the revisions after <from>, up to and including <to>, as with git:
                             5..7 is revisions 6 and 7, and Tag:pre..Tag:post is what was done
                             between two tags

Each end is a revision as --restore takes one: a number, HeadRevisionNum,
LastReleasedRevisionNum, Tag:<tag>, ChangeSet:<changeset>, Release:<number>, or Revision:<uuid>,
any of them prefixed with Before:. A bare slug is a tag.

A range whose ends resolve the wrong way round on a unit, such as two tags named in the wrong
order, is refused rather than re-applied.

A revert writes one new revision that takes back what the reverted revisions changed, and keeps
everything done since, as git revert does. It is a merge of the unit with its own history with
the range reversed ("cub unit update --merge-source Self --squash"), so a path changed again
since keeps the later value only where the two do not touch. To go back to an earlier revision
outright, dropping everything after it, restore it instead ("cub unit update --restore").

A revert is cleared for Override guards, since it takes back the unit's own change. A path held
by any other guard is left as it is and reported as a conflict, with the --clearance that would
take it.

A change order is undone with "cub variant demote", which restores each space it reached and
records that it did.`

var unitRevertCmd = &cobra.Command{
	Use:         "revert <unit> <revisions>",
	Short:       "Take back a change to a unit, keeping what came after it",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Take back a change to a unit with a new revision, keeping the changes made since.

`+revertRevisionsHelp+`

Without --change-desc, the new revision is described by what it reverted, as "Reverted revisions
6-7", or for a changeset by the changeset and its description.

Examples:
`+"```"+`
  # Revert the latest change
  cub unit revert --space web-dev backend HeadRevisionNum

  # Revert revision 7, showing what it would change first
  cub unit revert --space web-dev backend 7 --dry-run -o mutations

  # Revert revisions 6 and 7
  cub unit revert --space web-dev backend 5..7

  # Revert a changeset's changes to this unit
  cub unit revert --space web-dev backend ChangeSet:web-dev/tls-rollout
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(2),
	RunE: unitRevertCmdRun,
}

func init() {
	addRevertFlags(unitRevertCmd)
	enableWaitFlag(unitRevertCmd)
	addStandardDisplayFlags(unitRevertCmd)
	unitCmd.AddCommand(unitRevertCmd)
}

// addRevertFlags registers the flags both revert commands take.
func addRevertFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&revertArgs.changeDescription, "change-desc", "", "change description recorded on the new revisions; by default, what they reverted")
	cmd.Flags().StringVar(&revertArgs.changeset, "changeset", "", "open changeset to record the new revisions in")
	cmd.Flags().StringArrayVar(&changeClearance, "clearance", nil, clearanceFlagUsage+
		". A revert is cleared for Override guards without it, since it takes back the unit's own change")
	enableDryRunFlag(cmd)
}

// revertRange is what a revert takes back: the revisions after From, up to and including To. Each
// is a revision specification, as --restore takes one.
type revertRange struct {
	From, To string
	// Change is the specification naming the change, when one was named rather than a range.
	Change string
}

// parseRevertRange parses the revisions a revert command takes: one change, which is the range
// from the revision before it, or <from>..<to>.
func parseRevertRange(spec string) (revertRange, error) {
	from, to, isRange := strings.Cut(spec, "..")
	r := revertRange{From: from, To: to}
	if !isRange {
		if change, ok := strings.CutPrefix(spec, "Before:"); ok {
			return revertRange{}, fmt.Errorf("%s names the state before a change rather than a change: name the change, %s, or a range from it, as %s..HeadRevisionNum",
				spec, change, spec)
		}
		r = revertRange{From: "Before:" + spec, To: spec, Change: spec}
	} else if from == "" || to == "" {
		return revertRange{}, fmt.Errorf("a range is <from>..<to>, the revisions after <from> up to and including <to>; %q is missing one", spec)
	}
	for _, end := range []string{r.From, r.To} {
		if entityType, _ := revisionSpecEntity(end); entityType == "ChangeOrder" {
			return revertRange{}, errors.New(`a change order is undone with "cub variant demote", which restores every unit it marked, in each space it reached, and records that it did`)
		}
	}
	fromNum, fromErr := strconv.ParseInt(r.From, 10, 64)
	toNum, toErr := strconv.ParseInt(r.To, 10, 64)
	if fromErr == nil && toErr == nil && fromNum >= 0 && toNum >= 0 && fromNum >= toNum {
		return revertRange{}, fmt.Errorf("%s..%s is empty: a range is the revisions after the first up to and including the second, so the first is the earlier", r.From, r.To)
	}
	return r, nil
}

// revisionSpecEntity is the kind of entity a revision specification names, with the identifier
// naming it: "" for a number or a named revision, and Tag for a bare slug.
func revisionSpecEntity(spec string) (entityType, identifier string) {
	spec = strings.TrimPrefix(spec, "Before:")
	if entityType, identifier, ok := strings.Cut(spec, ":"); ok {
		return entityType, identifier
	}
	if _, err := strconv.ParseInt(spec, 10, 64); err == nil || isNamedRevision(spec) {
		return "", spec
	}
	// A bare UUID is a Revision's, as parseSelectedRevisionParameter reads it.
	if _, err := uuid.Parse(spec); err == nil {
		return "Revision", spec
	}
	return "Tag", spec
}

// revertMergeRange is the merge base and end of the self merge that reverts r: the range reversed,
// in the form the API takes, with slugs resolved in spaceID and a delta counted back from
// headRevisionNum.
func revertMergeRange(r revertRange, spaceID string, headRevisionNum int64) (base, end string, err error) {
	format := func(spec string) (string, error) {
		return formatRangeEnd(spec, spaceID, headRevisionNum)
	}
	if base, err = format(r.To); err != nil {
		return "", "", err
	}
	if end, err = format(r.From); err != nil {
		return "", "", err
	}
	// Deltas from head are numbers by now, so a backwards range of them is caught here. The
	// server refuses a range that runs forwards however its ends are named, once it has resolved
	// them on each unit.
	baseNum, baseErr := strconv.ParseInt(base, 10, 64)
	endNum, endErr := strconv.ParseInt(end, 10, 64)
	if baseErr == nil && endErr == nil && endNum >= baseNum {
		return "", "", fmt.Errorf("%s..%s is empty: it is revisions %d..%d, and a range is the revisions after the first up to and including the second, so the first is the earlier",
			r.From, r.To, endNum, baseNum)
	}
	return base, end, nil
}

// revertChangeDescription is the change description a revert records: --change-desc, or for a
// changeset, the changeset and its description. Otherwise it is empty, and the server describes
// each revision by the revisions it reverted.
func revertChangeDescription(r revertRange, spaceID string) (string, error) {
	if revertArgs.changeDescription != "" {
		return revertArgs.changeDescription, nil
	}
	entityType, identifier := revisionSpecEntity(r.Change)
	if entityType != "ChangeSet" {
		return "", nil
	}
	changeSet, err := resolveChangeSet(identifier, spaceID, "Slug,SpaceID,Description")
	if err != nil {
		return "", fmt.Errorf("failed to get changeset: %w", err)
	}
	return changeSetRevertDescription(changeSet.ChangeSet.Slug, changeSet.ChangeSet.Description), nil
}

func changeSetRevertDescription(slug, description string) string {
	if description == "" {
		return "Revert changeset " + slug
	}
	return fmt.Sprintf("Revert changeset %s: %s", slug, description)
}

// revertPatchBody is the merge patch a revert sends with its merge: the change description and the
// changeset to record it in, when there are any.
func revertPatchBody(changeDescription string) ([]byte, *goclientnew.UUID, error) {
	patch := map[string]any{}
	if changeDescription != "" {
		patch["LastChangeDescription"] = changeDescription
	}
	var changeSetID *goclientnew.UUID
	if revertArgs.changeset != "" {
		id, err := resolveChangeSetID(revertArgs.changeset)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get changeset: %w", err)
		}
		patch["ChangeSetID"] = id
		changeSetID = &id
	}
	body, err := json.Marshal(patch)
	return body, changeSetID, err
}

func unitRevertCmdRun(cmd *cobra.Command, args []string) error {
	r, err := parseRevertRange(args[1])
	if err != nil {
		return err
	}
	resolved, err := resolveUnit(args[0], selectedSpaceID, "*")
	if err != nil {
		return err
	}
	unit := resolved.Unit
	spaceID := unit.SpaceID.String()

	base, end, err := revertMergeRange(r, spaceID, unit.HeadRevisionNum)
	if err != nil {
		return err
	}
	changeDescription, err := revertChangeDescription(r, spaceID)
	if err != nil {
		return err
	}
	body, changeSetID, err := revertPatchBody(changeDescription)
	if err != nil {
		return err
	}
	// A self merge is cleared for Override guards by the server whatever it is sent with, so only
	// what --clearance adds goes with it.
	clearance, err := clearanceJSON(true)
	if err != nil {
		return err
	}

	// Squash is implied by revert, and sent as well for a server that predates revert.
	self, squash, revert := "Self", true, true
	params := &goclientnew.PatchUnitParams{
		MergeSource: &self,
		MergeBase:   &base,
		MergeEnd:    &end,
		Squash:      &squash,
		Revert:      &revert,
		DryRun:      dryRunParam(),
		ChangeSetId: changeSetID,
	}
	if clearance != "" {
		params.Clearance = &clearance
	}
	if shouldDisplayMutations() {
		params.Include = includeWriteDiff()
	}
	res, err := cubClientNew.PatchUnitWithBodyWithResponse(ctx, unit.SpaceID, unit.UnitID, params,
		"application/merge-patch+json", bytes.NewReader(body))
	if cubapi.IsAPIError(err, res) {
		return withClearanceFlags(cubapi.InterpretErrorGeneric(err, res))
	}
	written, err := unitFromWrite(res.JSON200)
	if err != nil {
		return err
	}
	if wait && !dryRun && written.HeadRevisionNum != unit.HeadRevisionNum {
		printAwaitingTriggers()
		if err := awaitTriggersRemoval(written); err != nil {
			return err
		}
	}

	if !renderPayload(res.JSON200) && showProgress() {
		displayRevertResult(unit.Slug, args[1], unit.HeadRevisionNum, written)
	}
	space := selectedSpaceSlug
	if space == "" || space == "*" {
		space = spaceID
	}
	displayWriteConflicts(space, unit.Slug, res.JSON200.Conflicts, dryRun)
	if shouldDisplayMutations() {
		tprintRaw("")
		displayUnitChanges(unit.Slug, "revert "+args[1], res.JSON200.Diff)
	}
	return nil
}

// displayRevertResult says what a revert of one unit did, or on a dry run would do.
func displayRevertResult(unitSlug, revisions string, priorHead int64, written *goclientnew.Unit) {
	if _, err := strconv.ParseInt(revisions, 10, 64); err == nil {
		revisions = "revision " + revisions
	}
	switch {
	case dryRun:
		tprint("Dry run: would revert %s on unit %s; nothing was written", revisions, unitSlug)
	case written.HeadRevisionNum == priorHead:
		// What it would have taken back is already gone, or is held by a guard, which the
		// conflicts printed next report.
		tprint("Reverting %s changed nothing on unit %s", revisions, unitSlug)
	default:
		tprint("Reverted %s on unit %s as revision %d", revisions, unitSlug, written.HeadRevisionNum)
	}
}

// formatRangeEnd formats one end of a revision range as the server resolves it on each unit, with
// slugs looked up in spaceID.
func formatRangeEnd(spec, spaceID string, headRevisionNum int64) (string, error) {
	// The revision before a Revision named by ID is the one end the --restore syntax has no
	// form for, since a restore takes the ID itself, but a merge range resolves it.
	if entityType, identifier := revisionSpecEntity(spec); entityType == "Revision" && strings.HasPrefix(spec, "Before:") {
		id, err := uuid.Parse(identifier)
		if err != nil {
			return "", fmt.Errorf("invalid revision identifier '%s': must be a UUID", identifier)
		}
		return "Before:Revision:" + id.String(), nil
	}
	formatted, isUUID, err := parseSelectedRevisionParameterIn(spaceID, spec, serverResolvedRevision, headRevisionNum)
	if err != nil {
		return "", err
	}
	if isUUID {
		formatted = "Revision:" + formatted
	}
	return formatted, nil
}
