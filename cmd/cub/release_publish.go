// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var releasePublishCmd = &cobra.Command{
	Use:         "publish <variant>",
	Short:       "Publish a release",
	Args:        cobra.ExactArgs(1),
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Publish a Release for the Space's release Target, bundling the Units in <variant> that are assigned to that Target.

<variant> is the Space whose Units are bundled (and becomes the Release's
Space), named as <component>/<variant>, or by the Space's slug or ID. The
consuming Target is the Space's ReleaseTargetID, set with --release-target on `+"`cub space create`"+` or `+"`cub space update`"+`.

By default each bundled Unit is captured at its head Revision. Pass --revision
to instead pin each Unit to the highest-numbered Revision carrying a Tag. A Unit
the Tag marks no Revision of is left out of the Release, and listed as skipped,
when it has never been released; once a Unit has been released, the Tag has to
mark one of its Revisions, since leaving it out would delete it from the Target.

Each Unit's Revision is selected by Tag, so what --revision names has to come
down to one Tag. A Tag does; so do the boundaries of a ChangeSet and a
ChangeOrder, which is how a whole change is released rather than a Revision
number that means something different in every Unit:

  --revision <slug> | Tag:<slug>          the Revision that Tag marks
  --revision ChangeSet:<slug>             where a Closed ChangeSet ended
  --revision Before:ChangeSet:<slug>      the state it started from
  --revision ChangeOrder:<slug>           the change order's release tag
  --revision Before:ChangeOrder:<slug>    the state before it

A ChangeOrder's release tag marks every releasable Unit of each Space the change
is in, at the state the change left it in, and follows what resolving Links
within the Space writes after the promotion. Publishing by ChangeOrder publishes
for that ChangeOrder: its release prerequisites are checked against what is
bundled, and its stage is advanced. Move the release tag to adopt a later
Revision of a Unit before releasing with "cub unit tag --move".

A Release of a ChangeOrder can carry revisions from before the change: a unit's
revisions after its last released one, up to where the change starts, which an
earlier change promoted on from this Space without releasing it here.
--prior-revisions Error refuses such a Release, naming them and the change
orders they belong to; Include, the default, releases them.

--dry-run publishes in a transaction that is rolled back, reporting the Release
that would be published or why it would be refused.

A publish is refused when a bundled Unit has outstanding ValidationErrors on the
Revision it would bundle, or when that Revision is part of a ChangeSet that has not
closed or is not where the ChangeSet ended. It is also refused when the Component
requires a ChangeWorkflow and the Release is not of a ChangeOrder that has one (for
a ChangeOrder, the Component of the ChangeOrder's Space). --dry-run reports why.

A revision number, a Revision ID and the named revisions each pick out a
Revision of one Unit, so they are not accepted. Slugs resolve like any other
entity slug: in --space, or across the organization when there is none, unless
qualified as space/slug.

--label, --annotation and --delete-gate set the Release's metadata at publish
time; `+"`cub release update`"+` can change it afterwards. The bundled content
itself is fixed once published, and so is --bundle-base-name: the release's OCI
manifest records the name, so it can only be set here.

Publishing a Space that is unchanged since its latest published Release succeeds
without creating a Release, and reports "No change".

A Release that would bundle no Units is refused when the Space's latest published
Release bundles some: a GitOps controller pulling the Space deletes everything the
previous Release deployed, and reports success.

Examples:
`+"```"+`
  cub release publish my-space

  # Bundle each Unit at the Revision tagged "v1.2.0"
  cub release publish --revision v1.2.0 my-space

  # Bundle each Unit at the Revision a closed ChangeSet ended on
  cub release publish --revision ChangeSet:rollout-42 my-space

  # Release a change order, after checking what would be released
  cub release publish --revision ChangeOrder:bump-base-image --dry-run my-space
  cub release publish --revision ChangeOrder:bump-base-image my-space

  # Release the state a promotion started from, to roll it back
  cub release publish --revision Before:ChangeOrder:bump-base-image my-space

  # Publish with metadata, and a gate that blocks deleting the Release
  cub release publish --label env=prod --annotation ticket=OPS-1234 my-space
  cub release publish --delete-gate retention/30-days my-space
`+"```"+`
`, ""),
	RunE: releasePublishCmdRun,
}

var (
	releasePublishRevision       string
	releasePublishBundleBaseName string
	releasePublishDryRun         bool
	releasePublishPrior          string
)

func init() {
	addStandardDisplayFlags(releasePublishCmd)
	enableLabelFlag(releasePublishCmd)
	enableAnnotationFlag(releasePublishCmd)
	enableDeleteGateFlag(releasePublishCmd)
	enableCreatePermissionFlag(releasePublishCmd)
	releasePublishCmd.Flags().StringVar(&releasePublishRevision, "revision", "", "Which tagged Revision to bundle for each Unit, as a Tag (slug, Tag:slug, space/slug or Tag ID) or as a ChangeSet:slug or ChangeOrder:slug boundary, optionally prefixed with Before:. A Unit without a matching tagged Revision is skipped if it has never been released, and refused otherwise.")
	releasePublishCmd.Flags().StringVar(&releasePublishPrior, "prior-revisions", "", "with --revision ChangeOrder:<slug>, what to do with a unit's revisions from before the change -- after its last released revision, up to where the change starts: Include releases them; Error refuses, naming them and the change orders they belong to. Defaults to the change order's ReleasePriorRevisions")
	releasePublishCmd.Flags().BoolVar(&releasePublishDryRun, "dry-run", false, "publish in a transaction that is rolled back: report the release that would be published, or why it would be refused, including a change order's release prerequisites, and publish nothing")
	releasePublishCmd.Flags().StringVar(&releasePublishBundleBaseName, "bundle-base-name", "", "base filename for the release's stored bundle, without the .tar.gz suffix; defaults to the bundled Unit's slug for a single-Unit release and to the Space's slug otherwise")
	releaseCmd.AddCommand(releasePublishCmd)
}

func releasePublishCmdRun(cmd *cobra.Command, args []string) error {
	// args[0] is a variant, as <component>/<variant> or by its space slug or ID; resolve it to the
	// Space whose Units are bundled. The consuming Target is the Space's
	// ReleaseTargetID, resolved server-side.
	space, err := resolveVariantOrSpace(args[0], "SpaceID")
	if err != nil {
		return err
	}

	body := goclientnew.PublishReleaseJSONRequestBody{
		// Publishing is the only point at which the bundle can be named: the name
		// goes into the release's OCI manifest and is read-only afterwards.
		BundleBaseName: releasePublishBundleBaseName,
	}

	if err := setLabels(&body.Labels); err != nil {
		return err
	}
	if err := setAnnotations(&body.Annotations); err != nil {
		return err
	}
	if err := setDeleteGates(&body.DeleteGates); err != nil {
		return err
	}
	if err := setPermissions(&body.Permissions); err != nil {
		return err
	}

	// A Release records a TagID, so the CLI resolves --revision the whole way to a Tag rather
	// than handing the server a specification to resolve per Unit as the other revision flags
	// do. Revision numbers and Revision IDs have no Tag to become, and are refused.
	if releasePublishRevision != "" {
		formatted, _, err := parseSelectedRevisionParameterIn(space.Space.SpaceID.String(), releasePublishRevision, resolveToTag, 0)
		if err != nil {
			return fmt.Errorf("invalid --revision value: %w", err)
		}
		tagID, err := uuid.Parse(strings.TrimPrefix(formatted, "Tag:"))
		if err != nil {
			return err
		}
		body.TagID = &tagID

		// A Release of a ChangeOrder is published for that ChangeOrder, which the server records
		// on the Release and advances in the same transaction, and at its release tag, which the
		// server supplies: only the ChangeOrder is sent. The state before it is not the change,
		// so Before:ChangeOrder: names none and is sent as the Tag it resolves to.
		if identifier, ok := strings.CutPrefix(releasePublishRevision, "ChangeOrder:"); ok {
			changeOrder, err := changeOrderByRef(identifier)
			if err != nil {
				return err
			}
			body.ChangeOrderID = &changeOrder.ChangeOrderID
			body.TagID = nil
		}
	}

	body.DryRun = releasePublishDryRun
	body.PriorRevisions = goclientnew.ReleasePublishRequestPriorRevisions(releasePublishPrior)

	res, err := cubClientNew.PublishReleaseWithResponse(ctx, space.Space.SpaceID, body)
	if cubapi.IsAPIError(err, res) {
		// A refusal carries the release gates evaluated; a structured output shows them.
		if err == nil && res.JSON422 != nil && isAlternativeOutput() {
			renderPayload(res.JSON422)
		}
		return cubapi.InterpretErrorGeneric(err, res)
	}
	if releasePublishDryRun && res.JSON200.Message == "" {
		if !quiet && !isAlternativeOutput() {
			tprint("Dry run: nothing was published. The release would be:")
			displayReleaseDetails(res.JSON200.Release)
			return nil
		}
		renderPayload(res.JSON200)
		return nil
	}

	// A Space unchanged since its latest Release publishes nothing: the response
	// carries that latest Release and a Message saying so.
	release := res.JSON200.Release
	if res.JSON200.Message != "" {
		if !quiet && !isAlternativeOutput() {
			tprint("No change: %s", res.JSON200.Message)
		}
		renderPayload(res.JSON200)
		return nil
	}
	displayCreateResults(release, "release", release.ReleaseID.String(), release.ReleaseID.String(), displayReleaseDetails)

	// A variant deployed to a `cub cluster up --controller sveltos` cluster has
	// its ClusterProfile released only now that its first Release exists, and one
	// deployed to a `--controller flux` cluster without argobot its Flux Unit.
	if err := publishVariantSveltosProfile(cmd.OutOrStdout(), space.Space.SpaceID); err != nil {
		return err
	}
	return publishVariantFluxApp(cmd.OutOrStdout(), space.Space.SpaceID)
}
