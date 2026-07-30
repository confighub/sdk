// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"fmt"
	"net/url"
	"strings"
)

// GetSpaceListURL returns the web UI URL for the spaces list page
func GetSpaceListURL(serverURL string) string {
	return fmt.Sprintf("%s/spaces", cleanServerURL(serverURL))
}

// GetSpaceDetailURL returns the web UI URL for a specific space detail page
func GetSpaceDetailURL(serverURL, spaceID string) string {
	return fmt.Sprintf("%s/spaces/%s", cleanServerURL(serverURL), spaceID)
}

// GetUnitListURL returns the web UI URL for the units list page
// Note: Space context is handled by the UI session
func GetUnitListURL(serverURL string) string {
	return fmt.Sprintf("%s/units", cleanServerURL(serverURL))
}

// GetUnitDetailURL returns the web UI URL for a specific unit detail page
func GetUnitDetailURL(serverURL, spaceID, unitID string) string {
	return fmt.Sprintf("%s/units/%s/%s", cleanServerURL(serverURL), spaceID, unitID)
}

// GetUnitEditURL returns the web UI URL for unit edit view
// Tab 1 is the edit tab in the unit detail page
func GetUnitEditURL(serverURL, spaceID, unitID string) string {
	return fmt.Sprintf("%s/units/%s/%s?tab=1",
		cleanServerURL(serverURL), spaceID, unitID)
}

// GetUnitRevisionsURL returns the web UI URL for unit revisions view
// Tab 2 is the revisions tab in the unit detail page
func GetUnitRevisionsURL(serverURL, spaceID, unitID string) string {
	return fmt.Sprintf("%s/units/%s/%s?tab=2",
		cleanServerURL(serverURL), spaceID, unitID)
}

// GetTargetListURL returns the web UI URL for the targets list page
func GetTargetListURL(serverURL string) string {
	return fmt.Sprintf("%s/targets", cleanServerURL(serverURL))
}

// GetWorkerListURL returns the web UI URL for the bridge workers list page
func GetWorkerListURL(serverURL string) string {
	return fmt.Sprintf("%s/bridge-workers", cleanServerURL(serverURL))
}

// GetComponentListURL returns the web UI URL for the component view with no
// component selected, i.e. the component overview.
func GetComponentListURL(serverURL string) string {
	return fmt.Sprintf("%s/components", cleanServerURL(serverURL))
}

// GetComponentURL returns the web UI URL for a specific component. The component
// is identified by the value of the well-known "Component" Space label, not by an
// entity ID: a component is the set of spaces sharing that label.
//
// When spaceID is non-empty the UI additionally preselects that space's node in
// the component's deployment graph.
func GetComponentURL(serverURL, component, spaceID string) string {
	query := url.Values{"app": []string{component}}
	if spaceID != "" {
		query.Set("space", spaceID)
	}
	return fmt.Sprintf("%s/components?%s", cleanServerURL(serverURL), query.Encode())
}

// WithOrganization adds the "org" query parameter to a web UI URL, naming the
// organization the URL should be viewed in. Without it the UI renders whichever
// organization the browser session last selected, which for anyone in more than
// one org silently shows the wrong data — or nothing at all, since the spaces
// and units in the path belong to a different org.
//
// externalOrgID must be the organization's ExternalID (the identity provider's
// ID), not its ConfigHub OrganizationID: the UI compares it against the current
// session's external org ID and hands it to /auth/switch-organization, which
// verifies membership against the identity provider. An empty externalOrgID, or
// a URL that already names an org, is left alone.
func WithOrganization(webURL, externalOrgID string) string {
	if externalOrgID == "" {
		return webURL
	}
	parsed, err := url.Parse(webURL)
	if err != nil {
		return webURL
	}
	query := parsed.Query()
	if query.Has("org") {
		return webURL
	}
	query.Set("org", externalOrgID)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// cleanServerURL removes trailing slashes from the server URL
func cleanServerURL(serverURL string) string {
	return strings.TrimRight(serverURL, "/")
}
