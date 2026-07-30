// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import "testing"

func TestGetComponentURL(t *testing.T) {
	const spaceID = "5f1c807a-eb24-46b4-9208-10924d6c3e1d"

	tests := []struct {
		name      string
		serverURL string
		component string
		spaceID   string
		want      string
	}{
		{
			name:      "component only",
			serverURL: "https://hub.confighub.com",
			component: "eshop",
			want:      "https://hub.confighub.com/components?app=eshop",
		},
		{
			name:      "trailing slash is trimmed",
			serverURL: "https://hub.confighub.com/",
			component: "eshop",
			want:      "https://hub.confighub.com/components?app=eshop",
		},
		{
			// The UI keys its deployment nodes by SpaceID, not slug.
			name:      "space preselects a deployment node",
			serverURL: "https://hub.confighub.com",
			component: "eshop",
			spaceID:   spaceID,
			want:      "https://hub.confighub.com/components?app=eshop&space=" + spaceID,
		},
		{
			// Component label values are user-supplied and are not restricted to
			// slug characters, so they have to survive the round trip.
			name:      "component name is escaped",
			serverURL: "http://localhost:9090",
			component: "my app&co",
			want:      "http://localhost:9090/components?app=my+app%26co",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := GetComponentURL(test.serverURL, test.component, test.spaceID)
			if got != test.want {
				t.Errorf("GetComponentURL() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestWithOrganization(t *testing.T) {
	const orgID = "a0815dc4-83b7-4caf-84bc-26bee4447749"

	tests := []struct {
		name   string
		webURL string
		orgID  string
		want   string
	}{
		{
			name:   "adds the only query param",
			webURL: "https://hub.confighub.com/spaces",
			orgID:  orgID,
			want:   "https://hub.confighub.com/spaces?org=" + orgID,
		},
		{
			name:   "joins an existing query",
			webURL: "https://hub.confighub.com/components?app=eshop",
			orgID:  orgID,
			want:   "https://hub.confighub.com/components?app=eshop&org=" + orgID,
		},
		{
			// GetUnitEditURL and friends encode the tab as a query param, so the
			// org has to survive alongside it.
			name:   "preserves a tab param",
			webURL: "https://hub.confighub.com/units/space-id/unit-id?tab=2",
			orgID:  orgID,
			want:   "https://hub.confighub.com/units/space-id/unit-id?org=" + orgID + "&tab=2",
		},
		{
			// A context predating organization tracking has no org to stamp.
			name:   "empty org is a no-op",
			webURL: "https://hub.confighub.com/spaces",
			orgID:  "",
			want:   "https://hub.confighub.com/spaces",
		},
		{
			name:   "an org already named wins",
			webURL: "https://hub.confighub.com/spaces?org=other-org",
			orgID:  orgID,
			want:   "https://hub.confighub.com/spaces?org=other-org",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := WithOrganization(test.webURL, test.orgID)
			if got != test.want {
				t.Errorf("WithOrganization() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestGetComponentListURL(t *testing.T) {
	got := GetComponentListURL("https://hub.confighub.com/")
	want := "https://hub.confighub.com/components"
	if got != want {
		t.Errorf("GetComponentListURL() = %q, want %q", got, want)
	}
}
