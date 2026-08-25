// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strconv"
	"strings"
)

// ConfigHub is pre-1.0, and its three-number version reserves the second number
// for API changes that are not backward compatible: v0.2.x and v0.3.x do not
// speak the same API. So a client and server whose first two numbers match are
// compatible, and any other pair is not, in whichever direction.
//
// Skew is only decidable between two numeric versions. A build from a working tree
// gets one -- the Makefiles stamp the API version the tree speaks, suffixed "-dev" --
// but a build from a fork, or one whose version was never stamped at all, may report
// anything. A version that does not parse says nothing about the API, so those pairs
// are left alone rather than guessed at.

// versionSkew is the result of comparing a client version against a server one.
type versionSkew int

const (
	// versionUnknown means at least one side's version does not parse, so
	// compatibility cannot be determined.
	versionUnknown versionSkew = iota
	// versionCompatible means the first two numbers match.
	versionCompatible
	// versionClientBehind means the client's API version predates the server's.
	versionClientBehind
	// versionClientAhead means the client's API version postdates the server's.
	versionClientAhead
)

// parseMajorMinor extracts the first two numbers of a version such as "v0.2.34"
// or "0.2.34". ok is false for anything that is not two numbers followed by
// either the end of the string or a separator -- "dev", "" and a git describe
// string all land there. A pre-release or build suffix ("v0.3.0-rc1", "v0.2-dev")
// parses, since it names the same API as its release.
func parseMajorMinor(version string) (major, minor int, ok bool) {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if v == "" {
		return 0, 0, false
	}
	// Cut off any pre-release or build metadata before splitting on dots, so
	// that "0.3.0-rc1" and "0.3.0+abc" parse the same as "0.3.0".
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil || major < 0 {
		return 0, 0, false
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil || minor < 0 {
		return 0, 0, false
	}
	return major, minor, true
}

// compareVersions reports how the client's API version relates to the server's.
func compareVersions(clientVersion, serverVersion string) versionSkew {
	clientMajor, clientMinor, clientOK := parseMajorMinor(clientVersion)
	serverMajor, serverMinor, serverOK := parseMajorMinor(serverVersion)
	if !clientOK || !serverOK {
		return versionUnknown
	}
	switch {
	case clientMajor == serverMajor && clientMinor == serverMinor:
		return versionCompatible
	case clientMajor < serverMajor || (clientMajor == serverMajor && clientMinor < serverMinor):
		return versionClientBehind
	default:
		return versionClientAhead
	}
}

// devVersionSuffix marks a version the build stamped from a working tree rather than
// from a release tag. The build declares which API that tree speaks, so the version
// still compares, but the remedy for a stale one is a rebuild rather than a download.
const devVersionSuffix = "-dev"

// upgradeInstruction names the fix for a cub that is behind. 'cub upgrade' downloads a
// release, which would throw away the binary a developer just built, so a build from a
// working tree is told to move the tree instead.
func upgradeInstruction(clientVersion string) string {
	if strings.HasSuffix(strings.TrimSpace(clientVersion), devVersionSuffix) {
		return "This cub was built from a working tree. Check out a tree at the server's version and rebuild it"
	}
	return "Run 'cub upgrade' to update cub"
}

// checkVersionSkew compares this cub against the server it is talking to. A client
// behind the server is a hard error naming the fix, because the commands that would
// follow are built against an API the server no longer serves. A client ahead of the
// server -- a released cub against a server that has not been updated yet -- only
// warns: the user typically cannot upgrade the server themselves, and much of what
// they run will still work.
//
// serverVersion is what the server reported, from the ConfigHub-Version response
// header or from /api/info. It returns nil when either version is not numeric.
func checkVersionSkew(clientVersion, serverVersion string) error {
	switch compareVersions(clientVersion, serverVersion) {
	case versionClientBehind:
		return fmt.Errorf("cub %s is too old for server %s: pre-1.0, a change in the second version number is not backward compatible. %s",
			clientVersion, serverVersion, upgradeInstruction(clientVersion))
	case versionClientAhead:
		tprintErr("Warning: cub %s is newer than server %s: pre-1.0, a change in the second version number is not backward compatible, so some commands may fail. Ask the server's operator to upgrade ConfigHub.",
			clientVersion, serverVersion)
	}
	return nil
}
