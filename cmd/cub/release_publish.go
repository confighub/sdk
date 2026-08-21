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
	Use:         "publish <space-slug>",
	Short:       "Publish a release",
	Args:        cobra.ExactArgs(1),
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Publish a Release for the Space's release Target, bundling the Units in <space-slug> that are assigned to that Target.

<space-slug> is the Space whose Units are bundled (and becomes the Release's
Space); a Space ID is also accepted. The consuming Target is the Space's
ReleaseTargetID, set via `+"`cub space create/update --release-target`"+`.

By default each bundled Unit is captured at its head Revision. Pass --revision
to instead pin each Unit to the highest-numbered Revision carrying a Tag; a Unit
with no matching tagged Revision falls back to its head Revision.

Each Unit's Revision is selected by Tag, so what --revision names has to come
down to one Tag. A Tag does; so do the boundaries of a ChangeSet and a
ChangeOrder, which is how a whole change is released rather than a Revision
number that means something different in every Unit:

  --revision <slug> | Tag:<slug>          the Revision that Tag marks
  --revision ChangeSet:<slug>             where a Closed ChangeSet ended
  --revision Before:ChangeSet:<slug>      the state it started from
  --revision ChangeOrder:<slug>           where the change arrived
  --revision Before:ChangeOrder:<slug>    the state before it

A revision number, a Revision ID and the named revisions each pick out a
Revision of one Unit, so they are not accepted. Slugs resolve like any other
entity slug: in --space, or in the context's default Space, unless qualified as
space/slug.

--label, --annotation and --delete-gate set the Release's metadata at publish
time; `+"`cub release update`"+` can change it afterwards. The bundled content
itself is fixed once published, and so is --bundle-base-name: the release's OCI
manifest records the name, so it can only be set here.

Examples:
`+"```"+`
  cub release publish my-space

  # Bundle each Unit at the Revision tagged "v1.2.0"
  cub release publish --revision v1.2.0 my-space

  # Bundle each Unit at the Revision a closed ChangeSet ended on
  cub release publish --revision ChangeSet:rollout-42 my-space

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
)

func init() {
	addStandardDisplayFlags(releasePublishCmd)
	enableLabelFlag(releasePublishCmd)
	enableAnnotationFlag(releasePublishCmd)
	enableDeleteGateFlag(releasePublishCmd)
	releasePublishCmd.Flags().StringVar(&releasePublishRevision, "revision", "", "Which tagged Revision to bundle for each Unit, as a Tag (slug, Tag:slug, space/slug or Tag ID) or as a ChangeSet:slug or ChangeOrder:slug boundary, optionally prefixed with Before:. Units without a matching tagged Revision fall back to their head Revision.")
	releasePublishCmd.Flags().StringVar(&releasePublishBundleBaseName, "bundle-base-name", "", "base filename for the release's stored bundle, without the .tar.gz suffix; defaults to the bundled Unit's slug for a single-Unit release and to the Space's slug otherwise")
	releaseCmd.AddCommand(releasePublishCmd)
}

func releasePublishCmdRun(cmd *cobra.Command, args []string) error {
	// args[0] is a Space slug (a Space ID is also accepted); resolve it to the
	// Space whose Units are bundled. The consuming Target is the Space's
	// ReleaseTargetID, resolved server-side.
	space, err := apiGetSpaceFromSlug(args[0], "SpaceID")
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

	// A Release records a TagID, so the CLI resolves --revision the whole way to a Tag rather
	// than handing the server a specification to resolve per Unit as the other revision flags
	// do. Revision numbers and Revision IDs have no Tag to become, and are refused.
	if releasePublishRevision != "" {
		formatted, _, err := parseSelectedRevisionParameter(releasePublishRevision, resolveToTag, 0)
		if err != nil {
			return fmt.Errorf("invalid --revision value: %w", err)
		}
		tagID, err := uuid.Parse(strings.TrimPrefix(formatted, "Tag:"))
		if err != nil {
			return err
		}
		body.TagID = &tagID
	}

	res, err := cubClientNew.PublishReleaseWithResponse(ctx, space.SpaceID, body)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}

	release := res.JSON200
	displayCreateResults(release, "release", release.ReleaseID.String(), release.ReleaseID.String(), displayReleaseDetails)
	return nil
}
