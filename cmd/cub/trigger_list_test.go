// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// The default table shows the controls a validating trigger covers, a run of them shortened, and
// --columns Controls shows the same.
func TestTriggerListShowsControls(t *testing.T) {
	trigger := &goclientnew.ExtendedTrigger{
		Trigger: &goclientnew.Trigger{
			Slug:        "correctness",
			Validating:  true,
			Annotations: map[string]string{"Controls": "CH-0201,CH-0202,CH-0203,CH-0207"},
		},
		Space: &goclientnew.Space{Slug: "policy"},
	}
	out := captureStdout(t, func() { displayTriggerList([]*goclientnew.ExtendedTrigger{trigger}) })
	if !strings.Contains(out, "CONTROLS") || !strings.Contains(out, "CH-0201–CH-0203,CH-0207") {
		t.Errorf("the table does not show the controls:\n%s", out)
	}
	if got := triggerCustomColumns["Controls"](trigger); got != "CH-0201–CH-0203,CH-0207" {
		t.Errorf("Controls column = %q", got)
	}
}
