// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var releaseUpdateArgs struct {
	patch bool
}

var releaseUpdateCmd = &cobra.Command{
	Use:         "update <release-id>",
	Short:       "Update a release",
	Args:        cobra.ExactArgs(1),
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Update a release, identified by its globally-unique release ID.

Only a release's labels, annotations, and delete gates can be updated. The bundled
configuration a release was published with is fixed, and so is the name it is stored
under -- the release's OCI manifest records that -- and whether it is published: use
`+"`cub release withdraw`"+` to take a release out of service, and publish a new
release to change what is bundled or what the bundle is called.

The release is located by its ID regardless of which Space it lives in, so --space
is not required (a Release lives in its bundled Units' Space, which need not be the
caller's default).

By default the release is read and sent back with the requested changes applied.
Pass --patch to send just the changes instead, which is also what removes a label,
annotation, or delete gate: --label key=- --annotation key=- --delete-gate name=-.

Examples:
`+"```"+`
  # Label a release
  cub release update 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c --label environment=production

  # Gate a release against deletion
  cub release update 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c --delete-gate retention/30-days

  # Clear that gate again
  cub release update --patch 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c --delete-gate retention/30-days=-

  # Update from a file
  cub release update 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c --filename release.yaml
`+"```"+`
`, ""),
	RunE: releaseUpdateCmdRun,
}

func init() {
	addStandardUpdateFlags(releaseUpdateCmd)
	releaseUpdateCmd.Flags().BoolVar(&releaseUpdateArgs.patch, "patch", false,
		"use the patch API, sending only the changes; required to remove a label, annotation, or delete gate")
	releaseCmd.AddCommand(releaseUpdateCmd)
}

func releaseUpdateCmdRun(cmd *cobra.Command, args []string) error {
	if err := validateStdinFlags(); err != nil {
		return err
	}
	if releaseUpdateArgs.patch && flagReplace {
		return fmt.Errorf("only one of --patch and --replace should be specified")
	}
	// The "-" removal forms can only be expressed as a merge patch.
	if err := ValidateLabelRemoval(label, releaseUpdateArgs.patch); err != nil {
		return err
	}
	if err := ValidateDeleteGateRemoval(deleteGate, releaseUpdateArgs.patch); err != nil {
		return err
	}

	releaseID, err := uuid.Parse(args[0])
	if err != nil {
		return fmt.Errorf("invalid release id %q: %w", args[0], err)
	}

	// As for withdraw: a Release's ID is org-unique but the write endpoints are
	// space-scoped, and a Release lives in its bundled Units' Space rather than the
	// caller's --space.
	if releaseUpdateArgs.patch {
		return runReleasePatch(releaseID, args[0])
	}

	// The org-wide search that resolves the Space doubles as the read of the
	// read-modify-write.
	currentRelease, err := apiGetReleaseForUpdate(releaseID)
	if err != nil {
		return err
	}
	spaceID := currentRelease.SpaceID

	// Handle --from-stdin or --filename with optional --replace
	if flagPopulateModelFromStdin || flagFilename != "" {
		existingRelease := currentRelease
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			currentRelease = new(goclientnew.Release)
			currentRelease.Version = existingRelease.Version
		}

		if err := populateModelFromFlags(currentRelease); err != nil {
			return err
		}

		// Ensure essential fields can't be clobbered
		currentRelease.OrganizationID = existingRelease.OrganizationID
		currentRelease.SpaceID = existingRelease.SpaceID
		currentRelease.ReleaseID = existingRelease.ReleaseID
	}

	if err = setLabels(&currentRelease.Labels); err != nil {
		return err
	}
	if err = setAnnotations(&currentRelease.Annotations); err != nil {
		return err
	}
	if err = setDeleteGates(&currentRelease.DeleteGates); err != nil {
		return err
	}

	relRes, err := cubClientNew.UpdateReleaseWithResponse(ctx, spaceID, releaseID, *currentRelease)
	if cubapi.IsAPIError(err, relRes) {
		return cubapi.InterpretErrorGeneric(err, relRes)
	}

	release := relRes.JSON200
	displayUpdateResults(release, "release", args[0], release.ReleaseID.String(), displayReleaseDetails)
	return nil
}

// runReleasePatch sends only the requested changes as a JSON merge patch. Unlike
// the whole-entity update it needs no read first -- the server applies the patch to
// the stored Release -- so it resolves the Space alone, and it can say null, which
// is what removes a label, annotation, or delete gate.
func runReleasePatch(releaseID uuid.UUID, identifier string) error {
	spaceID, err := apiGetReleaseSpaceID(releaseID)
	if err != nil {
		return err
	}

	// No enhancer: a Release has no patchable field of its own beyond the label,
	// annotation and delete gate maps BuildPatchData already assembles.
	patchData, err := BuildPatchData(nil)
	if err != nil {
		return fmt.Errorf("failed to build patch data: %w", err)
	}

	relRes, err := cubClientNew.PatchReleaseWithBodyWithResponse(
		ctx,
		spaceID,
		releaseID,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if cubapi.IsAPIError(err, relRes) {
		return cubapi.InterpretErrorGeneric(err, relRes)
	}

	release := relRes.JSON200
	displayUpdateResults(release, "release", identifier, release.ReleaseID.String(), displayReleaseDetails)
	return nil
}

// apiGetReleaseForUpdate reads the Release to be modified by its org-unique ID,
// with all fields, so the update sends back what it did not change. The server
// restores everything it owns -- the bundle, its digests, the published flag --
// from the stored Release, so what matters here is the Version the update is made
// against and the metadata maps the flags merge into.
func apiGetReleaseForUpdate(releaseID uuid.UUID) (*goclientnew.Release, error) {
	releases, err := apiSearchListReleases(fmt.Sprintf("ReleaseID = '%s'", releaseID), "*", "")
	if err != nil {
		return nil, err
	}
	for _, er := range releases {
		if er.Release != nil && er.Release.ReleaseID == releaseID {
			return er.Release, nil
		}
	}
	return nil, fmt.Errorf("release %s not found", releaseID)
}
