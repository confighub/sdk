// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var releaseListCmd = &cobra.Command{
	Use:   "list",
	Short: "List releases",
	Long: getCommandHelp(`List releases in a space, or across all spaces when --space is "*".

Examples:
`+"```"+`
  # List all releases in a space
  cub release list --space my-space

  # List releases in JSON format
  cub release list --space my-space -o json

  # List releases across all spaces (organization-wide search)
  cub release list --space '*' --where "Digest = 'sha256:...'"
`+"```"+`
`, ""),
	Args:        cobra.NoArgs,
	RunE:        releaseListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var defaultReleaseColumns = []string{"Release.ReleaseID", "Release.ManifestDigest", "Release.CreatedAt"}

var releaseAliases = map[string]string{
	"ID": "ReleaseID",
}

var releaseCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(releaseListCmd)
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

func getReleaseSlug(release *goclientnew.ExtendedRelease) string {
	if release.Release != nil {
		return release.Release.ReleaseID.String()
	}
	return ""
}

func displayReleaseList(releases []*goclientnew.ExtendedRelease) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Release-ID", "Manifest-Digest", "Created"})
	}
	for _, er := range releases {
		rel := er.Release
		manifestDigest := ""
		created := ""
		releaseID := ""
		if rel != nil {
			manifestDigest = rel.ManifestDigest
			created = rel.CreatedAt.String()
			releaseID = rel.ReleaseID.String()
		}
		table.Append([]string{releaseID, manifestDigest, created})
	}
	table.Render()
}

func releaseSelectValue(selectParam, include string) string {
	return handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"ReleaseID", "SpaceID", "OrganizationID"}
		return buildSelectList("Release", nil, include, defaultReleaseColumns, releaseAliases, releaseCustomColumnDependencies, baseFields)
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
	if selectValue := releaseSelectValue(selectParam, ""); selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	relRes, err := cubClientNew.ListExtendedReleasesWithResponse(ctx,
		uuid.MustParse(spaceID),
		newParams,
	)
	if cubapi.IsAPIError(err, relRes) {
		return nil, cubapi.InterpretErrorGeneric(err, relRes)
	}
	return extendedReleasePtrs(relRes.JSON200), nil
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
	if selectValue := releaseSelectValue(selectParam, ""); selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	relRes, err := cubClientNew.ListAllReleasesWithResponse(ctx, newParams)
	if cubapi.IsAPIError(err, relRes) {
		return nil, cubapi.InterpretErrorGeneric(err, relRes)
	}
	return extendedReleasePtrs(relRes.JSON200), nil
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
