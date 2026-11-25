// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// renderingFailedUpload is an upload result whose one Unit was written, but whose rendering could
// not be set up.
func renderingFailedUpload() *goclientnew.UploadResult {
	return &goclientnew.UploadResult{Components: []goclientnew.UploadComponentResult{{
		Spaces: []goclientnew.UploadSpaceResult{{
			SpaceSlug: "app",
			Action:    "Update",
			Units: []goclientnew.UploadUnitResult{{
				Slug:           "app-env",
				Action:         "Create",
				RenderingError: &goclientnew.ResponseError{Message: "the rendering of Unit app-env could not be set up: taken"},
			}},
		}},
	}}}
}

// assertRenderingReported checks that the report names the Unit, what was done to it and why its
// rendering is not set up, and does not call it failed, since it was written.
func assertRenderingReported(t *testing.T, out string) {
	t.Helper()
	for _, want := range []string{"Create", "app-env", "could not be set up: taken"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report has no %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "FAILED") {
		t.Errorf("the Unit was written, so it is not FAILED:\n%s", out)
	}
}

// A Unit whose rendering could not be set up is reported with what was done to it and why.
func TestUploadReportsARenderingError(t *testing.T) {
	assertRenderingReported(t, captureStdout(t, func() { reportUploadResult(renderingFailedUpload()) }))
}

// cub apply reports it the same way, and does not count it among the failures, which set the exit
// status.
func TestApplyReportsARenderingErrorWithoutFailing(t *testing.T) {
	var failed int
	assertRenderingReported(t, captureStdout(t, func() { failed = reportApplyResult(renderingFailedUpload(), false) }))
	if failed != 0 {
		t.Errorf("reportApplyResult counted %d failures; the Unit was written", failed)
	}
	if n := countApplyFailures(renderingFailedUpload()); n != 0 {
		t.Errorf("countApplyFailures counted %d failures; the Unit was written", n)
	}
}
