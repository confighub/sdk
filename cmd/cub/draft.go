// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

// draftCmd works across two spaces, the original and its draft, so like variantCmd it does not
// settle a single space before running.
var draftCmd = &cobra.Command{
	Use:               "draft",
	Short:             "Draft a change to a space in a space of its own, then land it",
	Long:              getCommandHelp(draftCommandGroupHelp, draftAgentContext),
	PersistentPreRunE: globalPreRun,
}

const draftCommandGroupHelp = `The draft subcommands make a change to a space without touching the space until the
change is ready, as a branch and a pull request do.

"cub draft create" clones some or all of a space's units into a draft space, with links back to
the units they were cloned from. Change the clones there as in any space: functions, Triggers,
validation and link resolution all run in the draft. The original space keeps taking edits,
upgrades and releases while the draft is open, without being locked, and a space can have several
drafts at once, which may change the same units.

"cub draft propose" creates the change set the draft will land in, in the original space, with a
description and review requests, and optionally a ChangeWorkflow that gates its close. Nothing in
the original space is locked yet.

"cub draft land" brings the draft's changes home: it opens that change set on the original units
the draft changed, merges each draft revision into its original unit as a revision of its own
("Replayed revision N of <draft-space>/<unit>"), and closes the change set. The draft space is
then archived, as the record of the change. A change set whose gates refuse the close stays open
on the original units with the change in it; land again once they hold.

"cub draft abandon" archives the draft space without landing it.

A draft is named <space>/<name>, where <space> is the original space's slug or ID or its
<component>/<variant>, or by the draft space's own slug or ID. The draft space is named
<space>-draft-<name>. It is in the original space's Component, with the label Stage=Draft, and
the label Draft=<name>. It has no release target unless --release-target gives it one, for a
preview deployment.`

const draftAgentContext = `A draft is "cub variant create --syncback" with porcelain: the draft space's units have
UpgradeUnit links from the original units, and the original units have MergeUnits links,
named syncback-<draft-space>-<unit>, to the clones. Landing resolves those MergeUnits links in a
change set of the original space; abandoning archives the draft space, which archives the
syncback links with it.`

func init() {
	rootCmd.AddCommand(draftCmd)
}

// resolveDraft finds the draft a reference names: <space>/<name>, where <space> may itself be a
// <component>/<variant>, or the draft space's own slug or ID.
func resolveDraft(ref string) (*goclientnew.Space, error) {
	if i := strings.LastIndex(ref, "/"); i > 0 {
		originalRef, name := ref[:i], ref[i+1:]
		// When the part before the last "/" is not a space, the whole reference may still be the
		// <component>/<variant> of a draft space, which is tried below.
		original, err := resolveVariantOrSpace(originalRef, "SpaceID,Slug")
		if err == nil {
			draft, err := cubapi.FindDraft(ctx, cubClient, original.Space.SpaceID, name)
			if err != nil {
				return nil, err
			}
			if draft == nil {
				return nil, errors.Errorf("space %s has no open draft named %s; \"cub draft list %s\" lists its drafts", original.Space.Slug, name, original.Space.Slug)
			}
			return draft, nil
		}
	}
	space, err := resolveVariantOrSpace(ref, "*")
	if err != nil {
		return nil, errors.Wrapf(err, "no draft %s: name it as <space>/<name> or by its draft space", ref)
	}
	if _, err := cubapi.DraftOriginal(space.Space); err != nil {
		return nil, err
	}
	return space.Space, nil
}

// draftOriginalSlug reads the slug of the space draft is a draft of.
func draftOriginalSlug(draft *goclientnew.Space) (string, error) {
	originalID, err := cubapi.DraftOriginal(draft)
	if err != nil {
		return "", err
	}
	original, err := resolveSpace(originalID.String(), "SpaceID,Slug")
	if err != nil {
		return "", err
	}
	return original.Space.Slug, nil
}

// --- create ---

var draftCreateArgs struct {
	releaseTarget string
	changeDesc    string
}

var draftCreateCmd = &cobra.Command{
	Use:   "create <space> <name>",
	Short: "Clone units of a space into a draft space",
	Args:  cobra.ExactArgs(2),
	Long: getCommandHelp(`Create a draft of a space: clone its units, or those --where selects among them, into a new
space named <space>-draft-<name>, with a syncback link from each original unit to its clone, so
the changes made in the draft can be landed in the original units.

The draft space is a clone of the original: it is in the same Component and has the same
labels, except that its Stage label is Draft, its Draft label is <name>, and its Variant label is
<variant>-draft-<name>. It selects the Triggers the original selects, so validation and
mutation run in the draft as they do in the original. It has no release target, so releasing it
cannot publish to where the original's releases go, unless --release-target gives it one for a
preview deployment.

Links between the cloned units are copied and retargeted at the clones. A cloned unit's links to
units that were not cloned are copied too, still pointing at those units, so a clone reads from
the same producers its original does. The original units' own upgrade links, and the syncback
links of other drafts, are not copied.

A draft that has landed keeps its change set, draft-<name>, in the original space, as the record
of the change, so its name cannot be used for another draft of that space.

Examples:
`+"```"+`
  # Draft a change to the deployment of acme-base.
  cub draft create acme-base raise-web-memory --where "Slug = 'deployment'"
  cub function set --space acme-base-draft-raise-web-memory --unit deployment \
      set-container-resources web all "" 1Gi 1

  # Draft a change to every unit of a variant, and deploy the draft as a preview.
  cub draft create acme/dev new-ingress --release-target preview/cluster
`+"```"+`
`, ""),
	RunE: draftCreateCmdRun,
}

func init() {
	addStandardDisplayFlags(draftCreateCmd)
	enableWhereFlag(draftCreateCmd)
	draftCreateCmd.Flags().StringVar(&draftCreateArgs.releaseTarget, "release-target", "", "Target to give the draft space as its release Target, for a preview deployment, addressed as <target-space>/<target-slug> or a Target ID")
	draftCreateCmd.Flags().StringVar(&draftCreateArgs.changeDesc, "change-desc", "", "change description recorded on each clone's first revision")
	enableWaitFlag(draftCreateCmd)
	draftCmd.AddCommand(draftCreateCmd)
}

func draftCreateCmdRun(cmd *cobra.Command, args []string) error {
	original, err := resolveVariantOrSpace(args[0], "SpaceID,Slug")
	if err != nil {
		return err
	}
	name := args[1]
	if err := cubapi.ValidateDraftName(name); err != nil {
		return err
	}
	var releaseTarget *goclientnew.UUID
	if draftCreateArgs.releaseTarget != "" {
		id, err := resolveTargetID(draftCreateArgs.releaseTarget)
		if err != nil {
			return err
		}
		releaseTarget = &id
	}
	result, err := cubapi.CreateDraft(ctx, cubClient, cubapi.DraftSpec{
		Original:          original.Space.SpaceID,
		Name:              name,
		UnitWhere:         where,
		ReleaseTarget:     releaseTarget,
		ChangeDescription: draftCreateArgs.changeDesc,
		Diff:              shouldDisplayMutations(),
	})
	if result != nil && result.Space != nil && !jsonOutput {
		tprint("Created draft space %s (ID: %s) of %s", result.Space.Slug, result.Space.SpaceID, original.Space.Slug)
	}
	if err != nil {
		return err
	}
	if len(result.Units) == 0 && !jsonOutput {
		tprint("No units of %s were selected; the draft is empty", original.Space.Slug)
	}
	bulkErr := handleBulkCreateOrUpdateResponse(&result.Units, result.UnitsStatus, "create", "")
	if shouldDisplayMutations() {
		displayDiffsForUnitWrites(&result.Units, "clone")
	}
	return bulkErr
}

// --- list ---

var draftListCmd = &cobra.Command{
	Use:   "list [<space>]",
	Short: "List open drafts",
	Args:  cobra.MaximumNArgs(1),
	Long: getCommandHelp(`List the open drafts of a space, or of every space. A draft that has landed or been abandoned
is archived, and is listed with --include-hidden=Archived.

Examples:
`+"```"+`
  cub draft list
  cub draft list acme-base
  cub draft list acme-base --include-hidden=Archived
`+"```"+`
`, ""),
	RunE: draftListCmdRun,
}

func init() {
	enableNoheaderFlag(draftListCmd)
	enableIncludeHiddenFlag(draftListCmd)
	addStandardDisplayFlags(draftListCmd)
	draftCmd.AddCommand(draftListCmd)
}

func draftListCmdRun(cmd *cobra.Command, args []string) error {
	var originalID *goclientnew.UUID
	if len(args) == 1 {
		original, err := resolveVariantOrSpace(args[0], "SpaceID")
		if err != nil {
			return err
		}
		originalID = &original.Space.SpaceID
	}
	drafts, err := cubapi.ListDrafts(ctx, cubClient, originalID, includeHidden)
	if err != nil {
		return err
	}
	if renderPayload(drafts) {
		return nil
	}
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Draft", "Of", "Space", "Change-Set", "State"})
	}
	for _, d := range drafts {
		if d.Space == nil {
			continue
		}
		of := ""
		if d.UpstreamSpace != nil {
			of = d.UpstreamSpace.Slug
		}
		changeSet := ""
		if d.Space.Annotations[cubapi.AnnotationDraftChangeSetID] != "" {
			changeSet = cubapi.DraftChangeSetSlug(d.Space.Labels[cubapi.LabelDraft])
		}
		state := "Open"
		if d.Space.HiddenReason != "" {
			state = d.Space.HiddenReason
		}
		// An archived draft space's slug is id<uuid>; the slug it had is kept in a label.
		slug := d.Space.Slug
		if archived := d.Space.Labels[archivedSlugLabel]; archived != "" {
			slug = archived
		}
		table.Append([]string{d.Space.Labels[cubapi.LabelDraft], of, slug, changeSet, state})
	}
	table.Render()
	return nil
}

// --- get ---

var draftGetCmd = &cobra.Command{
	Use:   "get <draft>",
	Short: "Show a draft: its spaces, its change set, and which units it changes",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Show a draft: the space it drafts, its change set once proposed, and each of its units with
whether it has changes that have not landed. "cub draft diff" shows what they are.

Examples:
`+"```"+`
  cub draft get acme-base/raise-web-memory
`+"```"+`
`, ""),
	RunE: draftGetCmdRun,
}

func init() {
	addStandardGetFlags(draftGetCmd)
	draftCmd.AddCommand(draftGetCmd)
}

// draftDetails is what "cub draft get" reports, and its data form.
type draftDetails struct {
	Draft     string
	Space     *goclientnew.Space
	Original  string
	ChangeSet *goclientnew.ChangeSet `json:",omitempty"`
	Units     []draftUnitDetails
}

type draftUnitDetails struct {
	Unit              string
	DraftRevision     int64
	LandedThrough     int64 `json:",omitempty"`
	Pending           bool
	OriginalUnitID    *goclientnew.UUID `json:",omitempty"`
	CreatedInTheDraft bool              `json:",omitempty"`
	SyncBackLinkSlug  string            `json:",omitempty"`
}

func draftGetCmdRun(cmd *cobra.Command, args []string) error {
	draft, err := resolveDraft(args[0])
	if err != nil {
		return err
	}
	originalSlug, err := draftOriginalSlug(draft)
	if err != nil {
		return err
	}
	changeSet, err := cubapi.DraftChangeSet(ctx, cubClient, draft)
	if err != nil {
		return err
	}
	units, err := cubapi.DraftUnits(ctx, cubClient, draft)
	if err != nil {
		return err
	}
	details := draftDetails{Draft: draft.Labels[cubapi.LabelDraft], Space: draft, Original: originalSlug, ChangeSet: changeSet}
	for _, u := range units {
		d := draftUnitDetails{Unit: u.Draft.Slug, DraftRevision: u.Draft.HeadRevisionNum, Pending: u.Pending}
		if u.Link == nil {
			d.CreatedInTheDraft = true
		} else {
			d.LandedThrough = u.Link.UpstreamLastMergedRevisionNum
			d.OriginalUnitID = &u.Link.FromUnitID
			d.SyncBackLinkSlug = u.Link.Slug
		}
		details.Units = append(details.Units, d)
	}
	if renderPayload(details) {
		return nil
	}
	view := detailView()
	view.Append([]string{"Draft", details.Draft})
	view.Append([]string{"Of", originalSlug})
	view.Append([]string{"Draft Space", draft.Slug})
	view.Append([]string{"Draft Space ID", draft.SpaceID.String()})
	if changeSet != nil {
		view.Append([]string{"Change Set", originalSlug + "/" + changeSet.Slug})
		view.Append([]string{"Change Set State", changeSet.State})
		if len(changeSet.ReviewRequests) > 0 {
			view.Append([]string{"Review Requests", changeSetReviewRequestsSummary(changeSet.ReviewRequests)})
		}
	} else {
		view.Append([]string{"Change Set", "(not proposed)"})
	}
	if draft.HiddenReason != "" {
		view.Append([]string{"Hidden Reason", draft.HiddenReason})
	}
	view.Render()
	if len(details.Units) == 0 {
		return nil
	}
	tprint("")
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Unit", "Revision", "Landed-Through", "Changes"})
	}
	for _, u := range details.Units {
		changes := "none"
		switch {
		case u.CreatedInTheDraft:
			changes = "created in the draft; not landed"
		case u.Pending:
			changes = "to land"
		}
		landed := ""
		if !u.CreatedInTheDraft {
			landed = strconv.FormatInt(u.LandedThrough, 10)
		}
		table.Append([]string{u.Unit, strconv.FormatInt(u.DraftRevision, 10), landed, changes})
	}
	table.Render()
	return nil
}

// --- diff ---

var draftDiffArgs struct {
	context bool
}

var draftDiffCmd = &cobra.Command{
	Use:   "diff <draft>",
	Short: "Show what a draft changes that has not landed",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Show, for each unit of the draft, what has changed in it since the revision its syncback link
last merged: when the draft has not landed, everything the draft has done to the unit since it
was cloned. That is what "cub draft land" brings home. Each unit's change is shown path by path
with the values on both sides.

Examples:
`+"```"+`
  cub draft diff acme-base/raise-web-memory
  cub draft diff acme-base/raise-web-memory -o json
`+"```"+`
`, ""),
	RunE: draftDiffCmdRun,
}

func init() {
	enableOutputFlag(draftDiffCmd)
	enableJqFlag(draftDiffCmd)
	enableJsonFlag(draftDiffCmd)
	enableYamlFlag(draftDiffCmd)
	draftDiffCmd.Flags().BoolVar(&draftDiffArgs.context, "diff-context", false, "include the unchanged fields around each change")
	draftCmd.AddCommand(draftDiffCmd)
}

func draftDiffCmdRun(cmd *cobra.Command, args []string) error {
	draft, err := resolveDraft(args[0])
	if err != nil {
		return err
	}
	units, err := cubapi.DraftUnits(ctx, cubClient, draft)
	if err != nil {
		return err
	}
	var diffs []goclientnew.UnitDiff
	for _, u := range units {
		if !u.Pending {
			continue
		}
		from := strconv.FormatInt(u.Link.UpstreamLastMergedRevisionNum, 10)
		to := strconv.FormatInt(u.Draft.HeadRevisionNum, 10)
		diff, err := cubapi.GetUnitDiff(ctx, cubClient, draft.SpaceID, u.Draft.UnitID, from, to, draftDiffArgs.context)
		if err != nil {
			return errors.Wrapf(err, "diff of %s", u.Draft.Slug)
		}
		diffs = append(diffs, *diff)
	}
	if renderPayload(diffs) {
		return nil
	}
	if len(diffs) == 0 {
		tprint("Draft %s has nothing to land", draft.Slug)
		return nil
	}
	for i := range diffs {
		d := &diffs[i]
		label := fmt.Sprintf("%s/%s", draft.Slug, unitSlugOfDiff(units, d))
		displayUnitChanges(label, fmt.Sprintf("revisions %d to %d", d.FromRevisionNum, d.ToRevisionNum), d.Diff)
	}
	return nil
}

func unitSlugOfDiff(units []cubapi.DraftUnit, d *goclientnew.UnitDiff) string {
	for _, u := range units {
		if u.Draft.UnitID == d.UnitID {
			return u.Draft.Slug
		}
	}
	return d.UnitID.String()
}

// --- propose ---

var draftProposeArgs struct {
	description    string
	changeWorkflow string
}

var draftProposeCmd = &cobra.Command{
	Use:   "propose <draft>",
	Short: "Propose a draft for review",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Propose a draft for review: create the change set it will land in, in the original space, and
ask reviewers to look at it. The change set is named draft-<name>. It is opened on no unit, so
the original space is not locked: "cub draft land" opens it on the original units the draft
changed when it lands.

--change-workflow governs the change set with a ChangeWorkflow, whose Final prerequisites gate
closing it, which is the last step of landing. When the original space is a variant of a
Component that requires one, the change set needs one the Component allows.

Proposing again changes the review requests: --request-review asks another reviewer, or
re-requests a review already asked for, and --withdraw-review-request withdraws one. The
description and the ChangeWorkflow are set when the change set is created.

Examples:
`+"```"+`
  cub draft propose acme-base/raise-web-memory --request-review alice \
      --description "Raise the web container's memory to 1Gi"

  # Gate landing on a ChangeWorkflow's approvals.
  cub draft propose acme-base/raise-web-memory --change-workflow platform/changeset-review \
      --request-review alice,bob
`+"```"+`
`, ""),
	RunE: draftProposeCmdRun,
}

func init() {
	draftProposeCmd.Flags().StringVar(&draftProposeArgs.description, "description", "", "description of the change, for review, recorded on the change set when it is created")
	draftProposeCmd.Flags().StringVar(&draftProposeArgs.changeWorkflow, "change-workflow", "", "ChangeWorkflow to govern the change set, as <slug> (in the original space), <space>/<slug> or ID; set when it is created")
	enableChangeSetRequestReviewFlag(draftProposeCmd)
	enableChangeSetWithdrawReviewRequestFlag(draftProposeCmd)
	draftCmd.AddCommand(draftProposeCmd)
}

// draftChangeSetSpec reads the description and ChangeWorkflow flags, resolving a bare
// ChangeWorkflow slug in the original space.
func draftChangeSetSpec(draft *goclientnew.Space, description, changeWorkflow string) (cubapi.DraftChangeSetSpec, error) {
	spec := cubapi.DraftChangeSetSpec{Description: description}
	if changeWorkflow == "" {
		return spec, nil
	}
	originalID, err := cubapi.DraftOriginal(draft)
	if err != nil {
		return spec, err
	}
	workflow, err := resolveChangeWorkflow(changeWorkflow, originalID.String(), "ChangeWorkflowID")
	if err != nil {
		return spec, err
	}
	spec.ChangeWorkflowID = &workflow.ChangeWorkflow.ChangeWorkflowID
	return spec, nil
}

func draftProposeCmdRun(cmd *cobra.Command, args []string) error {
	draft, err := resolveDraft(args[0])
	if err != nil {
		return err
	}
	existing, err := cubapi.DraftChangeSet(ctx, cubClient, draft)
	if err != nil {
		return err
	}
	if existing != nil && (draftProposeArgs.description != "" || draftProposeArgs.changeWorkflow != "") {
		return errors.Errorf("draft %s was already proposed in change set %s; --description and --change-workflow are set when it is created, so change them with \"cub changeset update\"", draft.Slug, existing.Slug)
	}
	if existing != nil && !reviewRequestFlagsGiven() {
		return errors.Errorf("draft %s was already proposed in change set %s; give --request-review or --withdraw-review-request to change who reviews it", draft.Slug, existing.Slug)
	}
	spec, err := draftChangeSetSpec(draft, draftProposeArgs.description, draftProposeArgs.changeWorkflow)
	if err != nil {
		return err
	}
	changeSet, created, err := cubapi.EnsureDraftChangeSet(ctx, cubClient, draft, spec)
	if err != nil {
		return err
	}
	originalSlug, err := draftOriginalSlug(draft)
	if err != nil {
		return err
	}
	if created {
		tprint("Proposed draft %s in change set %s/%s", draft.Slug, originalSlug, changeSet.Slug)
	}
	if !reviewRequestFlagsGiven() {
		return nil
	}
	requests, err := applyChangeSetReviewRequestFlags(changeSet.ReviewRequests)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"ReviewRequests": requests})
	if err != nil {
		return err
	}
	res, err := cubClientNew.PatchChangeSetWithBodyWithResponse(ctx, changeSet.SpaceID, changeSet.ChangeSetID,
		&goclientnew.PatchChangeSetParams{}, "application/merge-patch+json", bytes.NewReader(body))
	if cubapi.IsAPIError(err, res) {
		return errors.Wrapf(cubapi.InterpretErrorGeneric(err, res), "updating the review requests of change set %s", changeSet.Slug)
	}
	tprint("Review requested of %d reviewer(s) on change set %s/%s", len(requests), originalSlug, changeSet.Slug)
	return nil
}

// --- land ---

var draftLandArgs struct {
	description    string
	changeWorkflow string
	forceReason    string
}

var draftLandCmd = &cobra.Command{
	Use:   "land <draft>",
	Short: "Land a draft's changes in the space it drafts",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Land a draft: merge the changes made in it into the original units, in the draft's change set,
and close the change set.

Landing opens the draft's change set (created now, as draft-<name>, if the draft was not
proposed) on the original units the draft changed, and resolves their syncback links in it.
Each draft revision becomes a revision of its original unit, described as "Replayed revision N
of <draft-space>/<unit>", so the original's history says where the change came from. Units the
draft did not change are left alone and not locked. Units created in the draft have no original
to land in, and are reported. A draft that changed nothing is refused, and its change set is not
created: abandon it instead.

Landing is a merge: when the original changed the same paths since the draft was created, the
draft's change to them can be withheld, and is recorded as a conflict on the original unit,
where "cub unit conflicts" shows it.

The change set is then closed, which ends the change and makes it ready to release or promote.
A change set its ChangeWorkflow governs closes only when the workflow's Final prerequisites hold,
and the gates are printed either way. If they do not hold, the change stays in the open change
set on the original units, the draft is kept, and the command fails; land again once they hold,
which also brings over anything changed in the draft since. --force-reason closes it although
they do not hold, as "cub variant changeset close --force-reason" does.

Once the change set has closed, the draft space is archived with everything in it, its syncback
links included, as the record of the change. List archived drafts with
"cub draft list --include-hidden=Archived".

Examples:
`+"```"+`
  cub draft land acme-base/raise-web-memory --dry-run
  cub draft land acme-base/raise-web-memory
`+"```"+`
`, ""),
	RunE: draftLandCmdRun,
}

func init() {
	addStandardDisplayFlags(draftLandCmd)
	enableDryRunFlag(draftLandCmd)
	draftLandCmd.Flags().StringVar(&draftLandArgs.description, "description", "", "description of the change set, when the draft was not proposed and landing creates it")
	draftLandCmd.Flags().StringVar(&draftLandArgs.changeWorkflow, "change-workflow", "", "ChangeWorkflow to govern the change set, when the draft was not proposed and landing creates it")
	draftLandCmd.Flags().StringVar(&draftLandArgs.forceReason, "force-reason", "", "close the change set although its ChangeWorkflow's gates do not hold, recording this reason on it")
	draftCmd.AddCommand(draftLandCmd)
}

func draftLandCmdRun(cmd *cobra.Command, args []string) error {
	draft, err := resolveDraft(args[0])
	if err != nil {
		return err
	}
	originalSlug, err := draftOriginalSlug(draft)
	if err != nil {
		return err
	}
	spec, err := draftChangeSetSpec(draft, draftLandArgs.description, draftLandArgs.changeWorkflow)
	if err != nil {
		return err
	}
	if cs, err := cubapi.DraftChangeSet(ctx, cubClient, draft); err != nil {
		return err
	} else if cs != nil && (draftLandArgs.description != "" || draftLandArgs.changeWorkflow != "") {
		return errors.Errorf("draft %s was proposed in change set %s, which has its description and ChangeWorkflow; change them with \"cub changeset update\"", draft.Slug, cs.Slug)
	}

	result, err := cubapi.LandDraft(ctx, cubClient, draft, cubapi.DraftLandSpec{
		ChangeSet:   spec,
		ForceReason: draftLandArgs.forceReason,
		DryRun:      dryRun,
	})
	if result != nil {
		reportDraftLanding(draft, originalSlug, result)
	}
	if err != nil {
		return err
	}
	if !dryRun && result.ChangeSet != nil && !result.Closed {
		return errors.Errorf("draft %s landed in change set %s/%s, which did not close: %s\nLand again once its gates hold, or give --force-reason",
			draft.Slug, originalSlug, result.ChangeSet.Slug, result.CloseError)
	}
	return nil
}

func reportDraftLanding(draft *goclientnew.Space, originalSlug string, result *cubapi.DraftLandResult) {
	if renderPayload(result) {
		return
	}
	csName := ""
	if result.ChangeSet != nil {
		csName = originalSlug + "/" + result.ChangeSet.Slug
	}
	if result.CreatedChangeSet {
		verb := "Created"
		if dryRun {
			verb = "Would create"
		}
		tprint("%s change set %s", verb, csName)
	}
	verb := "Landed"
	if dryRun {
		verb = "Would land"
	}
	conflicted := 0
	for _, r := range result.Landed {
		if r.Unit == nil {
			continue
		}
		note := ""
		if r.Conflicts != nil && len(*r.Conflicts) > 0 {
			conflicted++
			note = fmt.Sprintf(", with %d conflict(s)", len(*r.Conflicts))
		}
		tprint("%s %s/%s at revision %d%s", verb, originalSlug, r.Unit.Slug, r.Unit.HeadRevisionNum, note)
	}
	if len(result.Landed) == 0 {
		tprint("Draft %s had no new changes to land", draft.Slug)
	}
	if conflicted > 0 {
		tprint("%d unit(s) withheld part of the draft's change; see \"cub variant conflicts %s\"", conflicted, originalSlug)
	}
	if len(result.Unlinked) > 0 {
		tprint("Not landed, as they were created in the draft and have no original: %s", strings.Join(result.Unlinked, ", "))
	}
	if shouldDisplayMutations() {
		displayDiffsForUnitWrites(&result.Landed, "land")
	}
	if result.Close != nil {
		displayChangeSetCloseGates(result.Close)
	}
	for _, gate := range result.FailedGates {
		tprint("Gate does not hold: %s", gate)
	}
	if result.Closed {
		forced := ""
		if result.Close != nil && result.Close.Forced {
			forced = ", forced past its gates"
		}
		tprint("Closed change set %s%s", csName, forced)
	}
	if result.Archived {
		tprint("Archived draft space %s", draft.Slug)
	}
}

// --- abandon ---

var draftAbandonCmd = &cobra.Command{
	Use:   "abandon <draft>",
	Short: "Abandon a draft without landing it",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Abandon a draft: archive the draft space with everything in it, its syncback links included,
and the change set it would have landed in, if it was proposed. Nothing in the original space
changes. An abandoned draft is listed with "cub draft list --include-hidden=Archived", and can be
brought back with "cub space unarchive --recursive".

A draft that has landed in a change set still open on the original units cannot be abandoned:
the change is in the original space by then, so close the change set, or revert it with
"cub variant revert".

Examples:
`+"```"+`
  cub draft abandon acme-base/raise-web-memory
`+"```"+`
`, ""),
	RunE: draftAbandonCmdRun,
}

func init() {
	draftCmd.AddCommand(draftAbandonCmd)
}

func draftAbandonCmdRun(cmd *cobra.Command, args []string) error {
	draft, err := resolveDraft(args[0])
	if err != nil {
		return err
	}
	cs, err := cubapi.AbandonDraft(ctx, cubClient, draft)
	if err != nil {
		return err
	}
	tprint("Archived draft space %s", draft.Slug)
	if cs != nil {
		tprint("Archived change set %s", cs.Slug)
	}
	return nil
}
