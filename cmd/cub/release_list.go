// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var releaseListCmd = &cobra.Command{
	Use:   "list",
	Short: "List releases",
	Long: getCommandHelp(`List the releases of a space, newest first, or the newest release of each space when
--space is "*".

Each release is shown by its number, the tag on the revisions it bundled, whether it is
published, the first 12 hex digits of its OCI manifest digest, the live status the tool
deploying it reported, and when it was published. -o wide adds the release id and the whole
manifest digest.

Examples:
`+"```"+`
  # List all releases in a space
  cub release list --space my-space

  # Include the release ids and whole manifest digests
  cub release list --space my-space -o wide

  # The five newest releases
  cub release list --space my-space --limit 5

  # List releases in JSON format
  cub release list --space my-space -o json

  # The newest release of each space (organization-wide search)
  cub release list --space '*'

  # Find the release with a manifest digest in any space
  cub release list --space '*' --where "ManifestDigest = 'sha256:...'"
`+"```"+`
`, ""),
	Args:        cobra.NoArgs,
	RunE:        releaseListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// defaultReleaseColumns is what the table reads, both layouts included, so that the select list
// names every field either one shows.
var defaultReleaseColumns = []string{"Release.ReleaseNum", "Release.TagID", "Release.Published", "Release.ManifestDigest", "Release.LiveStatus", "Release.CreatedAt", "Release.ReleaseID", "Release.SpaceSlug"}

var releaseAliases = map[string]string{
	"ID":  "ReleaseID",
	"Num": "ReleaseNum",
}

var releaseCustomColumnDependencies = map[string][]string{}

// releaseListInclude expands the Tag, so the table shows its name rather than its id.
const releaseListInclude = "TagID"

func init() {
	addStandardListFlags(releaseListCmd)
	enableListPagingFlags(releaseListCmd)
	releaseCmd.AddCommand(releaseListCmd)
}

func releaseListCmdRun(cmd *cobra.Command, args []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Cross-space search when --space is "*", otherwise list within the space.
	if selectedSpaceID == "*" {
		releases, err := apiSearchListReleases(where, selectFields, filterID)
		if err != nil {
			return err
		}
		displayListResults(releases, getReleaseSlug, displayReleaseList)
		return nil
	}

	releases, err := apiListReleases(selectedSpaceID, where, selectFields, filterID)
	if err != nil {
		return err
	}
	displayListResults(releases, getReleaseSlug, displayReleaseList)
	return nil
}

// getReleaseSlug names a Release the way -o name prints it. A Release has no slug: it is numbered
// within its Space, and that number is what cub release get takes.
func getReleaseSlug(release *goclientnew.ExtendedRelease) string {
	if release.Release != nil {
		return strconv.FormatInt(release.Release.ReleaseNum, 10)
	}
	return ""
}

// shortDigestLength is how many hex digits of a digest the default layout shows: as many as
// container tooling shows of an image ID, enough to tell a Space's Releases apart at a glance.
const shortDigestLength = 12

// shortDigest is the first shortDigestLength hex digits of an OCI digest, without the algorithm.
func shortDigest(digest string) string {
	hex := digest
	if i := strings.IndexByte(digest, ':'); i >= 0 {
		hex = digest[i+1:]
	}
	if len(hex) > shortDigestLength {
		hex = hex[:shortDigestLength]
	}
	return hex
}

// releaseTag is the Tag on the Revisions a Release bundled, by slug when the response expanded it.
// Publishing names the Tag it creates release-<num>, which repeats the number, but a Release
// published from an existing Tag carries that Tag's name, and that is worth seeing.
func releaseTag(er *goclientnew.ExtendedRelease) string {
	if er.Tag != nil {
		return qualifySlug(er.Tag.Slug, er.Tag.SpaceSlug, er.Release.SpaceSlug)
	}
	if er.Release.TagID != nil && *er.Release.TagID != uuid.Nil {
		return er.Release.TagID.String()
	}
	return ""
}

// displayReleaseList renders the table. The default layout identifies a Release by its number and
// the first digits of its manifest digest, which is what Argo CD and the cluster show of it; -o
// wide adds the id and the whole digest, for passing to another command.
func displayReleaseList(releases []*goclientnew.ExtendedRelease) {
	if displayRequestedColumns(releases, releaseAliases, nil) {
		return
	}
	wide := effectiveOutput().Kind == OutputWide
	crossSpace := selectedSpaceID == "*"
	table := tableView()
	if !noheader {
		var header []string
		if crossSpace {
			header = append(header, "Space")
		}
		header = append(header, "Num", "Tag", "Published")
		if !wide {
			header = append(header, "Digest")
		}
		header = append(header, "Live", "Created")
		if wide {
			header = append(header, "ID", "Manifest-Digest")
		}
		table.SetHeader(header)
	}
	for _, er := range releases {
		rel := er.Release
		if rel == nil {
			continue
		}
		var row []string
		if crossSpace {
			row = append(row, rel.SpaceSlug)
		}
		// Withdrawn Releases are retained and still listed, so print the flag for both values
		// rather than leaving the false case blank.
		row = append(row, strconv.FormatInt(rel.ReleaseNum, 10), releaseTag(er), strconv.FormatBool(rel.Published))
		if !wide {
			row = append(row, shortDigest(rel.ManifestDigest))
		}
		row = append(row, liveStatusSummary(rel.LiveStatus), rel.CreatedAt.Format("2006-01-02 15:04:05"))
		if wide {
			row = append(row, rel.ReleaseID.String(), rel.ManifestDigest)
		}
		table.Append(row)
	}
	table.Render()
}

// sortReleasesNewestFirst orders Releases newest first, unless --order-by asked for another order.
// Within a Space that is descending ReleaseNum; across Spaces the numbers are not comparable, so it
// is by when each was published.
func sortReleasesNewestFirst(releases []*goclientnew.ExtendedRelease) {
	if listOrderBy != "" {
		return
	}
	sort.SliceStable(releases, func(i, j int) bool {
		a, b := releases[i].Release, releases[j].Release
		if a == nil || b == nil {
			return b == nil && a != nil
		}
		if a.SpaceID == b.SpaceID && a.ReleaseNum != b.ReleaseNum {
			return a.ReleaseNum > b.ReleaseNum
		}
		return a.CreatedAt.After(b.CreatedAt)
	})
}

func releaseSelectValue(selectParam, include string) string {
	return handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"ReleaseID", "ReleaseNum", "SpaceID", "SpaceSlug", "OrganizationID", "CreatedAt"}
		return buildSelectList("Release", listColumnsFor("cub release list"), include, defaultReleaseColumns, releaseAliases, releaseCustomColumnDependencies, baseFields)
	})
}

func apiListReleases(spaceID string, whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedRelease, error) {
	newParams := &goclientnew.ListExtendedReleasesParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if filterParam != "" {
		newParams.Filter = &filterParam
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	if includeHidden != "" {
		newParams.IncludeHidden = &includeHidden
	}
	include := releaseListInclude
	newParams.Include = &include
	if selectValue := releaseSelectValue(selectParam, include); selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	opts := listPageOpts("DESC:ReleaseNum")
	if opts.OrderBy != "" {
		newParams.OrderBy = &opts.OrderBy
	}
	releases, err := cubapi.ReadPages(opts, func(limit *int, token *string) (*http.Response, *[]goclientnew.ExtendedRelease, error) {
		newParams.Limit, newParams.Continue = limit, token
		res, err := cubClientNew.ListExtendedReleasesWithResponse(ctx, uuid.MustParse(spaceID), newParams)
		if cubapi.IsAPIError(err, res) {
			return nil, nil, cubapi.InterpretErrorGeneric(err, res)
		}
		return res.HTTPResponse, res.JSON200, nil
	})
	if err != nil {
		return nil, err
	}
	sortReleasesNewestFirst(releases)
	return releases, nil
}

func apiSearchListReleases(whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedRelease, error) {
	newParams := &goclientnew.ListAllReleasesParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if filterParam != "" {
		newParams.Filter = &filterParam
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	if includeHidden != "" {
		newParams.IncludeHidden = &includeHidden
	}
	include := releaseListInclude
	newParams.Include = &include
	if selectValue := releaseSelectValue(selectParam, include); selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	relRes, err := cubClientNew.ListAllReleasesWithResponse(ctx, newParams)
	if cubapi.IsAPIError(err, relRes) {
		return nil, cubapi.InterpretErrorGeneric(err, relRes)
	}
	releases := extendedReleasePtrs(relRes.JSON200)
	sortReleasesNewestFirst(releases)
	return releases, nil
}

func extendedReleasePtrs(releases *[]goclientnew.ExtendedRelease) []*goclientnew.ExtendedRelease {
	if releases == nil {
		return nil
	}
	result := make([]*goclientnew.ExtendedRelease, len(*releases))
	for i := range *releases {
		result[i] = &(*releases)[i]
	}
	return result
}
