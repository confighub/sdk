// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
)

func TestParseMajorMinor(t *testing.T) {
	tests := []struct {
		version string
		major   int
		minor   int
		ok      bool
	}{
		{"v0.2.34", 0, 2, true},
		{"0.2.34", 0, 2, true},
		{"v1.0.0", 1, 0, true},
		{"v0.10.0", 0, 10, true},
		{" v0.2.34 ", 0, 2, true},
		{"v0.3.0-rc1", 0, 3, true},
		{"v0.3.0+9ff0792", 0, 3, true},
		// What the Makefiles stamp on a build from a working tree.
		{"v0.2-dev", 0, 2, true},
		{"dev", 0, 0, false},
		{"", 0, 0, false},
		{"v0", 0, 0, false},
		{"v0.x.1", 0, 0, false},
		{"vx.2.1", 0, 0, false},
		{"v-1.2.3", 0, 0, false},
	}
	for _, tc := range tests {
		major, minor, ok := parseMajorMinor(tc.version)
		if ok != tc.ok || major != tc.major || minor != tc.minor {
			t.Errorf("parseMajorMinor(%q) = %d, %d, %v; want %d, %d, %v",
				tc.version, major, minor, ok, tc.major, tc.minor, tc.ok)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		client string
		server string
		want   versionSkew
	}{
		// The third number moves for backward-compatible changes, so it is not
		// part of the comparison.
		{"v0.2.34", "v0.2.34", versionCompatible},
		{"v0.2.1", "v0.2.99", versionCompatible},
		{"v0.2.34", "v0.3.0", versionClientBehind},
		{"v0.3.0", "v0.2.34", versionClientAhead},
		{"v0.9.0", "v1.0.0", versionClientBehind},
		// A dev tree and a dev server built from it agree.
		{"v0.2-dev", "v0.2-dev", versionCompatible},
		{"v0.2-dev", "v0.2.34", versionCompatible},
		{"v0.2-dev", "v0.3.0", versionClientBehind},
		{"v1.0.0", "v0.9.0", versionClientAhead},
		// A version number that is not numeric says nothing about the API.
		{"dev", "v0.2.34", versionUnknown},
		{"v0.2.34", "dev", versionUnknown},
		{"dev", "dev", versionUnknown},
		// A server predating the version header reports nothing at all.
		{"v0.2.34", "", versionUnknown},
	}
	for _, tc := range tests {
		if got := compareVersions(tc.client, tc.server); got != tc.want {
			t.Errorf("compareVersions(%q, %q) = %v; want %v", tc.client, tc.server, got, tc.want)
		}
	}
}

// 'cub upgrade' downloads a release, which is the wrong advice for a binary the
// developer just built from source.
func TestUpgradeInstructionTellsADevBuildToRebuild(t *testing.T) {
	released := upgradeInstruction("v0.2.34")
	if !strings.Contains(released, "cub upgrade") {
		t.Errorf("upgradeInstruction for a release = %q, want it to name 'cub upgrade'", released)
	}
	dev := upgradeInstruction("v0.2-dev")
	if strings.Contains(dev, "cub upgrade") {
		t.Errorf("upgradeInstruction for a dev build = %q, want it not to name 'cub upgrade'", dev)
	}
}

func TestCheckVersionSkew(t *testing.T) {
	// Only a client behind the server is fatal; the other cases let the command
	// through, warning or not.
	if err := checkVersionSkew("v0.2.34", "v0.3.0"); err == nil {
		t.Error("checkVersionSkew with client behind = nil, want error")
	}
	for _, tc := range [][2]string{
		{"v0.2.34", "v0.2.34"},
		{"v0.3.0", "v0.2.34"},
		{"dev", "v0.2.34"},
		{"v0.2.34", ""},
	} {
		if err := checkVersionSkew(tc[0], tc[1]); err != nil {
			t.Errorf("checkVersionSkew(%q, %q) = %v; want nil", tc[0], tc[1], err)
		}
	}
}
