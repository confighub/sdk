// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package diffview

import (
	"bytes"
	"os"
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func changeType(t goclientnew.MutationType) *goclientnew.MutationType { return &t }

func sampleDiff() *goclientnew.ConfigDiff {
	return &goclientnew.ConfigDiff{Resources: []goclientnew.ResourceDiff{
		{
			Resource:   &goclientnew.ResourceInfo{ResourceType: "apps/v1/Deployment", ResourceName: "web/web"},
			ChangeType: changeType(goclientnew.Update),
			Changes: []goclientnew.PathChange{
				{DisplayPath: "spec.replicas", ChangeType: "Update", FromValue: "1\n", ToValue: "3\n"},
				{DisplayPath: "spec.template.spec.securityContext", ChangeType: "Add", ToValue: "runAsNonRoot: true\nrunAsUser: 1000\n"},
				{DisplayPath: "metadata.labels.tier", ChangeType: "Delete", FromValue: "web"},
				{DisplayPath: "metadata.name", ChangeType: "None", ToValue: "web"},
				{DisplayPath: "data.config", ChangeType: "Update", Patch: "--- a\n+++ b\n line\n-old\n+new\n"},
				{DisplayPath: "spec.template", ChangeType: "Update", FromValue: "a: 1\n", ToValue: "a: 2\nb: 3\n"},
				{DisplayPath: "spec.paused", ChangeType: "Update", Comment: "Line", FromValue: "false", ToValue: "false",
					Attribution: &goclientnew.MutationInfo{Index: 4}},
			},
		},
		{
			Resource:   &goclientnew.ResourceInfo{ResourceType: "v1/ConfigMap", ResourceName: "web/settings"},
			ChangeType: changeType(goclientnew.Add),
			ToValue:    "apiVersion: v1\nkind: ConfigMap\n",
		},
		{
			Resource:         &goclientnew.ResourceInfo{ResourceType: "v1/Service", ResourceName: "web/front"},
			PreviousResource: &goclientnew.ResourceInfo{ResourceType: "v1/Service", ResourceName: "web/web"},
			ChangeType:       changeType(goclientnew.Delete),
			FromValue:        "kind: Service\n",
		},
	}}
}

func TestWriteUnitChangesPlain(t *testing.T) {
	var buf bytes.Buffer
	WriteUnitChanges(&buf, "web", "harden", sampleDiff(), Style{})
	want := `Changes to unit web from harden:
Resource: apps/v1/Deployment web/web
  ~ [Update] spec.replicas
      1 → 3
  + [Add] spec.template.spec.securityContext
      runAsNonRoot: true
      runAsUser: 1000
  - [Delete] metadata.labels.tier
      web
    metadata.name: web
  ~ [Update] data.config
       line
      -old
      +new
  ~ [Update] spec.template
      a: 1
      →
      a: 2
      b: 3
  ~ [Update] spec.paused (line comment)  (#4)
      false → false

Resource: v1/ConfigMap web/settings  (added)
    apiVersion: v1
    kind: ConfigMap

Resource: v1/Service web/front  (deleted)  (was v1/Service web/web)
    kind: Service
`
	if got := buf.String(); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteConfigDiffNoChanges(t *testing.T) {
	for _, diff := range []*goclientnew.ConfigDiff{nil, {}} {
		var buf bytes.Buffer
		WriteConfigDiff(&buf, diff, Style{Color: true})
		if buf.String() != "No changes\n" {
			t.Errorf("WriteConfigDiff(%v) = %q, want No changes", diff, buf.String())
		}
	}
	var buf bytes.Buffer
	WriteUnitChanges(&buf, "web", "", nil, Style{})
	if buf.String() != "Changes to unit web:\nThe server returned no diff\n" {
		t.Errorf("WriteUnitChanges with no diff = %q", buf.String())
	}
}

// Color wraps the same text in ANSI escapes; stripping them gives the plain rendering back.
func TestWriteConfigDiffColor(t *testing.T) {
	var plain, color bytes.Buffer
	WriteConfigDiff(&plain, sampleDiff(), Style{})
	WriteConfigDiff(&color, sampleDiff(), Style{Color: true})
	out := color.String()
	for _, want := range []string{
		ansiLightBlue + "Resource: apps/v1/Deployment web/web" + ansiReset,
		"      " + ansiRed + "1" + ansiReset + " → " + ansiGreen + "3" + ansiReset,
		ansiDim + "    metadata.name: web" + ansiReset,
		"      " + ansiGreen + "+new" + ansiReset,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("colored output lacks %q:\n%s", want, out)
		}
	}
	stripped := out
	for _, code := range []string{ansiReset, ansiRed, ansiGreen, ansiLightBlue, ansiDim} {
		stripped = strings.ReplaceAll(stripped, code, "")
	}
	if stripped != plain.String() {
		t.Errorf("colored output without escapes differs from plain:\n%s\nvs\n%s", stripped, plain.String())
	}
}

func TestColorFor(t *testing.T) {
	if ColorFor(&bytes.Buffer{}) {
		t.Error("a buffer is not a terminal")
	}
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if ColorFor(f) {
		t.Error("a regular file is not a terminal")
	}
	t.Setenv("NO_COLOR", "1")
	if ColorFor(os.Stdout) {
		t.Error("NO_COLOR turns color off")
	}
}
