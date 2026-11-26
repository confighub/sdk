// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/confighub/installer/pkg/api"
	"github.com/confighub/installer/pkg/installer"
	"github.com/spf13/cobra"
)

const packageTestRecord = `configHub:
  configSchema: InstallerRecord
  configName: installer
package:
  name: k8sbot
  installerMetadata:
    version: 0.2.12
  spec:
    bases:
    - name: default
      path: bases/default
source:
  ref: oci://ghcr.io/confighub/packages/k8sbot:0.2.12
inputs:
  namespace: confighubbot
`

// A record without source or inputs, as a package built locally and set up
// without a pull may leave.
const packageTestBareRecord = `configHub:
  configSchema: InstallerRecord
  configName: installer
package:
  name: hello-app
  installerMetadata:
    version: 0.1.0
  spec:
    bases:
    - name: default
      path: bases/default
`

func packageTestInstalled(t *testing.T, space, data string) *installer.InstalledRecord {
	t.Helper()
	rec, err := api.ParseRecord([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return &installer.InstalledRecord{Space: space, Data: []byte(data), Record: rec}
}

func TestPackageInstalls(t *testing.T) {
	installs, err := packageInstalls([]*installer.InstalledRecord{
		packageTestInstalled(t, "k8sbot-base", packageTestRecord),
		packageTestInstalled(t, "hello", packageTestBareRecord),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(installs) != 2 {
		t.Fatalf("got %d installs, want 2", len(installs))
	}
	got := installs[0]
	if got.Space != "k8sbot-base" || got.Package != "k8sbot" || got.Version != "0.2.12" ||
		got.Namespace != "confighubbot" || got.Source != "oci://ghcr.io/confighub/packages/k8sbot:0.2.12" {
		t.Errorf("install = %+v", got)
	}
	// -o prints the record as stored, so its keys are the stored ones.
	doc, ok := got.Record.(map[string]any)
	if !ok || doc["configHub"] == nil || doc["source"] == nil {
		t.Errorf("Record = %#v, want the stored document", got.Record)
	}
	bare := installs[1]
	if bare.Package != "hello-app" || bare.Namespace != "" || bare.Source != "" {
		t.Errorf("install without source or inputs = %+v", bare)
	}

	if _, err := packageInstalls([]*installer.InstalledRecord{{Space: "broken", Data: []byte("a: [")}}); err == nil || !strings.Contains(err.Error(), "Space broken") {
		t.Errorf("unparseable record: err = %v, want one naming its Space", err)
	}
}

func TestPackageRecordError(t *testing.T) {
	other := errors.New("boom")
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("Space shared holds installer records of 2 packages: %w", installer.ErrSeveralPackages), "cub package show <package> --space shared"},
		{fmt.Errorf("k8sbot is installed in 2 Spaces: %w", installer.ErrSeveralInstalls), "name the Space with --space"},
		{fmt.Errorf("token expired: %w", installer.ErrNotAuthenticated), "cub auth login"},
		{other, "boom"},
	} {
		got := packageRecordError(tc.err, "shared")
		if !strings.Contains(got.Error(), tc.want) || !errors.Is(got, tc.err) {
			t.Errorf("packageRecordError(%v) = %v, want it to wrap the error and say %q", tc.err, got, tc.want)
		}
	}
}

func TestPackageSpaceFlag(t *testing.T) {
	for in, want := range map[string]string{"*": "", "": "", "k8sbot-base": "k8sbot-base"} {
		if got := packageSpaceFlag(in); got != want {
			t.Errorf("packageSpaceFlag(%q) = %q, want %q", in, got, want)
		}
	}
}

// Formats with nothing to print for an install are refused, not answered
// with empty output.
func TestCheckPackageOutputFormat(t *testing.T) {
	orig := outputFormat
	t.Cleanup(func() { outputFormat = orig })
	cmd := &cobra.Command{Use: "list"}
	for format, refused := range map[string]bool{
		"":                     false,
		"json":                 false,
		"yaml":                 false,
		"jq=.[].Space":         false,
		"wide":                 false,
		"name":                 true,
		"mutations":            true,
		"custom-columns=Space": true,
	} {
		outputFormat = format
		if err := checkPackageOutputFormat(cmd); (err != nil) != refused {
			t.Errorf("-o %q: err = %v, want refused %v", format, err, refused)
		}
	}
}
