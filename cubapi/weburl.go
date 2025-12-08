// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"fmt"
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

// cleanServerURL removes trailing slashes from the server URL
func cleanServerURL(serverURL string) string {
	return strings.TrimRight(serverURL, "/")
}
