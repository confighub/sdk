// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// releaseReferenceLatest is the only tag-style OCI reference supported: it
// selects the highest-numbered Release for a Target.
const releaseReferenceLatest = "latest"

// releaseGetInclude expands every entity a Release references, so the details name each one
// rather than giving its id.
const releaseGetInclude = "SpaceID,TagID,ChangeOrderID,TargetID,UserID"

var releaseGetCmd = &cobra.Command{
	Use:   "get [release-num | release-id]",
	Short: "Get details about a release",
	Args:  cobra.MaximumNArgs(1),
	Long: getCommandHelp(`Get detailed information about a specific release.

The release may be identified by its number within a specific --space, the number
cub release list shows, or by its id. Within a specific --space, it may instead be
identified by an OCI reference via one of the following flags:

  --oci-reference <ref>    An OCI image reference. A manifest digest (sha256:...)
                           selects the release with that manifest digest.
                           Otherwise <ref> is a tag; the only supported tag is
                           "latest", which selects the space's newest release.
  --bundle-digest <digest> The release's bundle content digest (sha256:...).

The OCI Repository row is the oci:// URL the release is pulled from, its space's. A GitOps
operator pinned to this release pulls that repository at the Manifest Digest: Argo CD's
targetRevision, or Flux's ref.digest.

Examples:
`+"```"+`
  # Get details about release 3 of a space
  cub release get --space my-space 3

  # Get details about a release by id
  cub release get --space my-space 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c

  # Get a release in JSON format
  cub release get --space my-space -o json 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c

  # Get the space's newest release (tag reference)
  cub release get --space my-space --oci-reference latest

  # Get a release by its OCI manifest digest
  cub release get --space my-space --oci-reference sha256:2222...

  # Get a release by its bundle content digest
  cub release get --space my-space --bundle-digest sha256:1111...

  # The OCI repository and manifest digest that pin a deployment to release 3
  cub release get --space my-space 3 -o jq='.Release.OCIRepositoryURL + "@" + .Release.ManifestDigest'
`+"```"+`
`, ""),
	RunE: releaseGetCmdRun,
}

var (
	releaseGetOCIReference string
	releaseGetBundleDigest string
)

func init() {
	addStandardGetFlags(releaseGetCmd)
	releaseGetCmd.Flags().StringVar(&releaseGetOCIReference, "oci-reference", "", "OCI reference of the release to get: the \"latest\" tag or a manifest digest (sha256:...)")
	releaseGetCmd.Flags().StringVar(&releaseGetBundleDigest, "bundle-digest", "", "Bundle content digest (sha256:...) of the release to get, instead of a release id")
	enableOptionalSpace(releaseGetCmd)
	releaseCmd.AddCommand(releaseGetCmd)
}

func releaseGetCmdRun(cmd *cobra.Command, args []string) error {
	if len(args) > 0 && (releaseGetOCIReference != "" || releaseGetBundleDigest != "") {
		return fmt.Errorf("--oci-reference, --bundle-digest and a release argument cannot be used together, use only one")
	}

	// The OCI-reference and bundle-digest lookups resolve via list within a
	// single Space, so they require --space to name one.
	if (releaseGetOCIReference != "" || releaseGetBundleDigest != "") &&
		(selectedSpaceID == "" || selectedSpaceID == "*") {
		return fmt.Errorf("--oci-reference and --bundle-digest require a specific --space")
	}

	// Identify the release explicitly by an OCI reference, a bundle digest, or an
	// id argument. The reference selectors resolve the release via a where-clause
	// (mirroring cub unit get, which resolves a UUID or a slug via list).
	var release *goclientnew.ExtendedRelease
	var err error
	switch {
	case releaseGetOCIReference != "":
		ref := releaseGetOCIReference
		if isOCIDigest(ref) {
			release, err = apiGetExtendedReleaseByWhere(selectedSpaceID,
				"ManifestDigest = '"+ref+"'", fmt.Sprintf("OCI reference %q", ref))
		} else {
			// A tag reference: only "latest" is supported, selecting the Space's
			// newest Release (a Space has a single release Target).
			if ref != releaseReferenceLatest {
				return fmt.Errorf("unsupported OCI reference %q (supported: a manifest digest or the %q tag)", ref, releaseReferenceLatest)
			}
			release, err = apiGetLatestRelease(selectedSpaceID)
		}
	case releaseGetBundleDigest != "":
		release, err = apiGetExtendedReleaseByWhere(selectedSpaceID,
			"Digest = '"+releaseGetBundleDigest+"'", fmt.Sprintf("bundle digest %q", releaseGetBundleDigest))
	case len(args) == 1:
		release, err = apiGetExtendedReleaseByArg(args[0])
	default:
		return fmt.Errorf("specify a release number or id argument, --oci-reference, or --bundle-digest")
	}
	if err != nil {
		return err
	}
	displayGetResults(release, displayExtendedReleaseDetails)
	return nil
}

// apiGetExtendedReleaseByArg reads the Release an argument names: an id, or a number within the
// selected Space. Numbers are only unique within a Space, so one needs a specific --space.
func apiGetExtendedReleaseByArg(arg string) (*goclientnew.ExtendedRelease, error) {
	if releaseID, err := uuid.Parse(arg); err == nil {
		return apiGetExtendedReleaseByID(releaseID, selectFields)
	}
	num, err := strconv.ParseInt(arg, 10, 64)
	if err != nil || num < 1 {
		return nil, fmt.Errorf("invalid release %q: expected a release number or id", arg)
	}
	if selectedSpaceID == "" || selectedSpaceID == "*" {
		return nil, fmt.Errorf("a release number requires a specific --space")
	}
	return apiGetExtendedReleaseByWhere(selectedSpaceID, fmt.Sprintf("ReleaseNum = %d", num), fmt.Sprintf("number %d", num))
}

// resolveReleaseIDIn resolves a Release named by its id, or by its number within spaceID.
func resolveReleaseIDIn(spaceID string, ref string) (uuid.UUID, error) {
	if releaseID, err := uuid.Parse(ref); err == nil {
		return releaseID, nil
	}
	num, err := strconv.ParseInt(ref, 10, 64)
	if err != nil || num < 1 {
		return uuid.Nil, fmt.Errorf("expected a release number or id")
	}
	if spaceID == "" || spaceID == "*" {
		return uuid.Nil, fmt.Errorf("a release number requires a specific --space")
	}
	releases, err := apiListReleases(spaceID, fmt.Sprintf("ReleaseNum = %d", num), "ReleaseID,ReleaseNum", "", "")
	if err != nil {
		return uuid.Nil, err
	}
	for _, er := range releases {
		if er.Release != nil {
			return er.Release.ReleaseID, nil
		}
	}
	return uuid.Nil, fmt.Errorf("no release %d in space %s", num, releaseSpaceName(spaceID))
}

// releaseSpaceName names a Space in a message by the slug the user selected it with, when it is that one.
func releaseSpaceName(spaceID string) string {
	if spaceID == selectedSpaceID && selectedSpaceSlug != "" {
		return selectedSpaceSlug
	}
	return spaceID
}

// isOCIDigest reports whether ref is an OCI digest (sha256:...).
func isOCIDigest(ref string) bool {
	return strings.HasPrefix(strings.ToLower(ref), "sha256:")
}

// apiGetExtendedReleaseByWhere resolves a Release in spaceID via list with the
// given where-clause, returning the match with the highest ReleaseNum. Used for
// the digest selectors (ManifestDigest, Digest), which each match a single
// Release.
func apiGetExtendedReleaseByWhere(spaceID, where, describe string) (*goclientnew.ExtendedRelease, error) {
	// The default for get is "*" (all fields) rather than auto-selected list columns.
	selectParam := selectFields
	if selectParam == "" {
		selectParam = "*"
	}
	releases, err := apiListReleases(spaceID, where, selectParam, "", releaseGetInclude)
	if err != nil {
		return nil, err
	}
	var latest *goclientnew.ExtendedRelease
	for _, er := range releases {
		if er.Release == nil {
			continue
		}
		if latest == nil || er.Release.ReleaseNum > latest.Release.ReleaseNum {
			latest = er
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("no release found for %s in space %s", describe, releaseSpaceName(spaceID))
	}
	return latest, nil
}

// apiGetLatestRelease returns the newest published Release (highest ReleaseNum) in
// spaceID: the Release served at the "latest" tag. A Space has a single release
// Target, so its newest Release is that Target's latest. Withdrawn Releases are
// retained but no longer served, so they are filtered out server-side — a
// client-side check would miss them whenever --select omits Published.
func apiGetLatestRelease(spaceID string) (*goclientnew.ExtendedRelease, error) {
	// The default for get is "*" (all fields) rather than auto-selected list columns.
	selectParam := selectFields
	if selectParam == "" {
		selectParam = "*"
	}
	releases, err := apiListReleases(spaceID, "Published = true", selectParam, "", releaseGetInclude)
	if err != nil {
		return nil, err
	}
	var latest *goclientnew.ExtendedRelease
	for _, er := range releases {
		if er.Release == nil {
			continue
		}
		if latest == nil || er.Release.ReleaseNum > latest.Release.ReleaseNum {
			latest = er
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("no release found in space %s", releaseSpaceName(spaceID))
	}
	return latest, nil
}

func displayReleaseDetails(releaseDetails *goclientnew.Release) {
	displayExtendedReleaseDetails(&goclientnew.ExtendedRelease{Release: releaseDetails})
}

// displayExtendedReleaseDetails shows every field of a Release, naming the entities it references
// by slug when the response expanded them and by id when it did not.
func displayExtendedReleaseDetails(er *goclientnew.ExtendedRelease) {
	view := tableView()
	rel := er.Release
	if rel == nil {
		view.Render()
		return
	}
	view.Append([]string{"ID", rel.ReleaseID.String()})
	view.Append([]string{"Release Num", strconv.FormatInt(rel.ReleaseNum, 10)})
	switch {
	case er.Space != nil:
		view.Append([]string{"Space", er.Space.Slug})
	case rel.SpaceSlug != "":
		view.Append([]string{"Space", rel.SpaceSlug})
	default:
		view.Append([]string{"Space ID", rel.SpaceID.String()})
	}
	// Published is omitempty in the generated client, so a withdrawn Release
	// decodes as false rather than being reported as absent; format it explicitly
	// so withdrawal is visible here instead of showing an empty cell.
	view.Append([]string{"Published", strconv.FormatBool(rel.Published)})
	if tag := releaseTag(er); tag != "" {
		view.Append([]string{"Tag", tag})
	}
	if changeOrder := releaseChangeOrder(er); changeOrder != "" {
		view.Append([]string{"ChangeOrder", changeOrder})
	}
	if er.Target != nil {
		view.Append([]string{"Target", qualifySlug(er.Target.Slug, er.Target.SpaceSlug, rel.SpaceSlug)})
	} else if rel.TargetID != nil && *rel.TargetID != uuid.Nil {
		view.Append([]string{"Target ID", rel.TargetID.String()})
	}
	if er.User != nil {
		view.Append([]string{"Published By", er.User.Username})
	} else if rel.UserID != nil && *rel.UserID != uuid.Nil {
		view.Append([]string{"Published By", rel.UserID.String()})
	}
	view.Append([]string{"Unit Count", strconv.FormatInt(rel.UnitCount, 10)})
	if len(rel.SkippedUnits) > 0 {
		view.Append([]string{"Skipped Units", skippedUnitsSummary(rel.SkippedUnits)})
	}
	view.Append([]string{"Bundle Base Name", rel.BundleBaseName})
	view.Append([]string{"Digest", rel.Digest})
	view.Append([]string{"Manifest Digest", rel.ManifestDigest})
	if rel.OCIRepositoryURL != "" {
		view.Append([]string{"OCI Repository", rel.OCIRepositoryURL})
	}
	if rel.DataSize > 0 {
		view.Append([]string{"Data Size", strconv.FormatInt(rel.DataSize, 10) + " bytes"})
	}
	if status := rel.LiveStatus; status != nil {
		view.Append([]string{"Sync", reportedValue(string(status.Sync), status.ReporterSync)})
		view.Append([]string{"Health", reportedValue(string(status.Health), status.ReporterHealth)})
		if status.Operation != "" || status.ReporterOperation != "" {
			view.Append([]string{"Operation", reportedValue(string(status.Operation), status.ReporterOperation)})
		}
		if status.Message != "" {
			view.Append([]string{"Live Message", status.Message})
		}
		view.Append([]string{"Reporter", status.Reporter})
		if status.DataSource != "" {
			view.Append([]string{"Data Source", status.DataSource})
		}
		view.Append([]string{"Observed At", status.ObservedAt.String()})
	}
	view.Append([]string{"Organization ID", rel.OrganizationID.String()})
	view.Append([]string{"Created At", rel.CreatedAt.String()})
	view.Append([]string{"Updated At", rel.UpdatedAt.String()})
	view.Append([]string{"Version", strconv.FormatInt(rel.Version, 10)})
	view.Append([]string{"Labels", labelsToString(rel.Labels)})
	view.Append([]string{"Delete Gates", deleteGatesToString(rel.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(rel.Annotations)})
	view.Append([]string{"Permissions", permissionsToString(rel.Permissions)})
	view.Render()
}

// reportedValue is a normalized live-status value, followed by the reporter's own word for it
// when that differs, as Argo CD's Error does from Failed: the reporter's word is the one to
// search its console and logs for.
func reportedValue(normalized, reported string) string {
	if reported == "" || reported == normalized {
		return normalized
	}
	if normalized == "" {
		return reported
	}
	return normalized + " (reported as " + reported + ")"
}

func apiGetExtendedRelease(spaceID string, releaseID string, selectParam string) (*goclientnew.ExtendedRelease, error) {
	newParams := &goclientnew.GetExtendedReleaseParams{}
	include := releaseGetInclude
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	relRes, err := cubClientNew.GetExtendedReleaseWithResponse(ctx,
		uuid.MustParse(spaceID),
		uuid.MustParse(releaseID),
		newParams,
	)
	if cubapi.IsAPIError(err, relRes) {
		return nil, cubapi.InterpretErrorGeneric(err, relRes)
	}
	return relRes.JSON200, nil
}

// apiGetExtendedReleaseByID reads one release by UUID. A UUID identifies the release
// in any space, so when no concrete space is selected the release is first located
// through the organization-wide list and then read from its own space.
func apiGetExtendedReleaseByID(releaseID uuid.UUID, selectParam string) (*goclientnew.ExtendedRelease, error) {
	spaceID := selectedSpaceID
	if spaceID == "" || spaceID == "*" {
		found, err := apiSearchListReleases("ReleaseID = '"+releaseID.String()+"'", "ReleaseID,SpaceID", "")
		if err != nil {
			return nil, err
		}
		if len(found) == 0 || found[0].Release == nil {
			return nil, fmt.Errorf("release %s not found in any space", releaseID)
		}
		spaceID = found[0].Release.SpaceID.String()
	}
	return apiGetExtendedRelease(spaceID, releaseID.String(), selectParam)
}
