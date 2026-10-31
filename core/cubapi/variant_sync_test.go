// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// The merge's own writes are what a sync protects: its paths, and not those an earlier
// Mutation wrote or a None entry. A resource it added whole has no path to protect, and is
// protected as a whole.
func TestSyncProtection(t *testing.T) {
	update, add, none := goclientnew.Update, goclientnew.Add, goclientnew.None
	paths := goclientnew.MutationMap{
		"data.HOST":     {Index: 7, MutationType: &update},
		"data.REGION":   {Index: 7, MutationType: &add},
		"data.OLD":      {Index: 3, MutationType: &update},
		"data.NOTHING":  {Index: 7, MutationType: &none},
		"metadata.name": {Index: 1, MutationType: &add},
	}
	mutationSources := goclientnew.ResourceMutationList{
		{Resource: &goclientnew.ResourceInfo{ResourceType: "v1/ConfigMap", ResourceName: "ns/env"}, PathMutationMap: &paths},
		{Resource: &goclientnew.ResourceInfo{ResourceType: "v1/ServiceAccount", ResourceName: "ns/restore"},
			ResourceMutationInfo: &goclientnew.MutationInfo{Index: 7, MutationType: &add}},
		{Resource: &goclientnew.ResourceInfo{ResourceType: "v1/Service", ResourceName: "ns/web"},
			ResourceMutationInfo: &goclientnew.MutationInfo{Index: 2, MutationType: &add}},
	}

	protection := syncProtection(mutationSources, 7)
	if len(protection) != 2 || protection[0].Resource.ResourceName != "ns/env" || protection[1].Resource.ResourceName != "ns/restore" {
		t.Fatalf("protection = %+v, want the ConfigMap and the ServiceAccount", protection)
	}
	got := protection[0].Protected
	if len(got) != 2 || !got["data.HOST"] || !got["data.REGION"] {
		t.Errorf("protected paths = %v, want data.HOST and data.REGION", got)
	}
	if whole := protection[1].Protected; len(whole) != 1 || !whole[""] {
		t.Errorf("the ServiceAccount the merge added should be protected as a whole, got %v", whole)
	}
}

// A copied link leaves from the target's unit of the same slug, and leads to the target's unit
// of the same slug when it is within the source and to the same unit when it leads out of it.
// It keeps what it resolves and starts with no history of its own. An UpgradeUnit link is not
// copied, nor one whose units the target does not have.
func TestSyncLinkCopy(t *testing.T) {
	source, target, other := uuid.New(), uuid.New(), uuid.New()
	targetUnits := map[string]*goclientnew.Unit{
		"deployment": {UnitID: uuid.New(), Slug: "deployment"},
		"configmap":  {UnitID: uuid.New(), Slug: "configmap"},
	}
	spec := VariantSyncSpec{Source: source, Target: target}
	upstreamLinkID := uuid.New()

	within := goclientnew.Link{
		LinkID: uuid.New(), Slug: "deployment-to-configmap", SpaceID: source, ToSpaceID: source, ToUnitID: uuid.New(),
		UpdateType: "NeedsProvides", AutoUpdate: true, UpstreamLinkID: &upstreamLinkID,
		Hash: "h", Stale: true, Version: 4, UpstreamLastMergedRevisionNum: 9, DownstreamLastMergedRevisionNum: 8,
		DeleteGates: map[string]bool{"keep": true},
	}
	copied, reason := syncLinkCopy(within, spec, "deployment", &goclientnew.Unit{Slug: "configmap"}, targetUnits)
	if copied == nil {
		t.Fatalf("within the source: not copied: %s", reason)
	}
	if copied.SpaceID != target || copied.FromUnitID != targetUnits["deployment"].UnitID ||
		copied.ToUnitID != targetUnits["configmap"].UnitID || copied.ToSpaceID != target {
		t.Errorf("within the source: copied to %+v, want the target's deployment and configmap", copied)
	}
	if copied.LinkID != (uuid.UUID{}) || copied.Hash != "" || copied.Stale || copied.Version != 0 ||
		copied.UpstreamLastMergedRevisionNum != 0 || copied.DownstreamLastMergedRevisionNum != 0 || copied.DeleteGates != nil {
		t.Errorf("within the source: the copy kept the source link's identity or history: %+v", copied)
	}
	if !copied.AutoUpdate || copied.UpdateType != "NeedsProvides" || copied.UpstreamLinkID == nil || *copied.UpstreamLinkID != upstreamLinkID {
		t.Errorf("within the source: the copy lost what it resolves: %+v", copied)
	}

	outgoingTo := uuid.New()
	outgoing := goclientnew.Link{Slug: "deployment-stable-image", ToSpaceID: other, ToUnitID: outgoingTo, UpdateType: "TransformPaths"}
	copied, reason = syncLinkCopy(outgoing, spec, "deployment", &goclientnew.Unit{Slug: "image"}, targetUnits)
	if copied == nil || copied.ToUnitID != outgoingTo || copied.ToSpaceID != other {
		t.Errorf("out of the source: got %+v (%s), want the same unit in the other space", copied, reason)
	}

	for name, tc := range map[string]struct {
		link     goclientnew.Link
		fromSlug string
		toSlug   string
	}{
		"UpgradeUnit":  {goclientnew.Link{UpdateType: "UpgradeUnit", ToSpaceID: other}, "deployment", "deployment"},
		"no from unit": {goclientnew.Link{UpdateType: "NeedsProvides", ToSpaceID: source}, "ui", "configmap"},
		"no to unit":   {goclientnew.Link{UpdateType: "NeedsProvides", ToSpaceID: source}, "deployment", "namespace"},
	} {
		if copied, _ := syncLinkCopy(tc.link, spec, tc.fromSlug, &goclientnew.Unit{Slug: tc.toSlug}, targetUnits); copied != nil {
			t.Errorf("%s: copied %+v, want it skipped", name, copied)
		}
	}
}

// A merge keeps the target's order of keys and style of lists, so the comparison with the
// source is of what the documents say, for YAML and JSON. Other formats are compared as text.
func TestSameConfiguration(t *testing.T) {
	a := "# a comment\nkind: ConfigMap\ndata:\n  b: \"2\"\n  a: \"1\"\nargs: [db, migrate]\n---\nkind: Service\n"
	b := "kind: Service\n---\nkind: ConfigMap\nargs:\n- db\n- migrate\ndata:\n  a: \"1\"\n  b: \"2\"\n"
	if !sameConfiguration(a, b, "Kubernetes/YAML") {
		t.Error("the same documents in another order and style should match")
	}
	if sameConfiguration(a, strings.Replace(b, `b: "2"`, `b: "3"`, 1), "Kubernetes/YAML") {
		t.Error("a different value should not match")
	}
	if sameConfiguration(a, b+"---\nkind: Secret\n", "Kubernetes/YAML") {
		t.Error("an extra document should not match")
	}
	if sameConfiguration("a = 1\nb = 2\n", "b = 2\na = 1\n", "AppConfig/Properties") {
		t.Error("a format other than YAML and JSON is compared as text")
	}
}
