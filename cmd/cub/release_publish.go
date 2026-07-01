// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var releasePublishCmd = &cobra.Command{
	Use:   "publish <space-slug> <target-space>/<target-slug>",
	Short: "Publish a release",
	Args:  cobra.ExactArgs(2),
	Long: getCommandHelp(`Publish a Release for a consuming Target, bundling the Units in <space-slug> that are assigned to that Target.

<space-slug> is the Space whose Units are bundled (and becomes the Release's
Space); a Space ID is also accepted. The consuming Target is addressed as
<target-space>/<target-slug> (a bare <target-slug> resolves in the selected or
default Space, and a Target ID is also accepted); it may live in a different
Space than the bundled Units.

By default each bundled Unit is captured at its head Revision. Pass --revision
to instead pin each Unit to the highest-numbered Revision carrying that Tag; a
Unit with no matching tagged Revision falls back to its head Revision. The
revision Tag slug is resolved within the bundled Units' Space (<space-slug>).

Examples:
`+"```"+`
  cub release publish my-space target-space/my-target

  # Bundle each Unit at the Revision tagged "v1.2.0"
  cub release publish --revision v1.2.0 my-space target-space/my-target
`+"```"+`
`, ""),
	RunE: releasePublishCmdRun,
}

var releasePublishRevision string

func init() {
	addStandardDisplayFlags(releasePublishCmd)
	releasePublishCmd.Flags().StringVar(&releasePublishRevision, "revision", "", "Tag slug identifying the tagged Revision to bundle for each Unit. Units without a matching tagged Revision fall back to their head Revision.")
	releaseCmd.AddCommand(releasePublishCmd)
}

func releasePublishCmdRun(cmd *cobra.Command, args []string) error {
	// args[0] is a Space slug (a Space ID is also accepted); resolve it to the
	// Space whose Units are bundled.
	space, err := apiGetSpaceFromSlug(args[0], "SpaceID")
	if err != nil {
		return err
	}
	// The consuming Target may live in any Space, so it is addressed as
	// <target-space>/<target-slug> (a bare <target-slug> resolves in the
	// selected/default Space, and a Target UUID is also accepted).
	targetID, err := parseEntityIdentifierSingle[goclientnew.Target](
		args[1],
		EntityTypeTarget,
		apiGetTargetFromSlugInSpaceCore,
		func(t *goclientnew.Target) string { return t.TargetID.String() },
	)
	if err != nil {
		return err
	}

	body := goclientnew.PublishReleaseJSONRequestBody{TargetID: targetID}

	// Resolve the revision Tag slug to its ID within the bundled Units' Space (args[0]).
	if releasePublishRevision != "" {
		tag, err := apiGetTagFromSlugInSpace(releasePublishRevision, space.SpaceID.String(), "*")
		if err != nil {
			return err
		}
		body.TagID = &tag.TagID
	}

	res, err := cubClientNew.PublishReleaseWithResponse(ctx, space.SpaceID, body)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}

	release := res.JSON200
	displayCreateResults(release, "release", release.ReleaseID.String(), release.ReleaseID.String(), displayReleaseDetails)
	return nil
}
