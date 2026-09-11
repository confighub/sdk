// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// resolverBackedGets are the "get" commands that name their operand by
// reference and resolve it, so they can run with no space: a UUID identifies
// the entity outright, a "space/slug" carries its own space, and a bare slug is
// searched for across the organization.
var resolverBackedGets = []string{
	"attribute get", "changeset get", "changeorder get", "changeworkflow get", "filter get",
	"invocation get", "link get", "tag get", "target get", "trigger get",
	"unit get", "view get", "worker get",
}

// spaceScopedGets are the "get" commands that still address a per-space
// endpoint directly, so a space is still required. They lose the requirement
// when their entity gains a resolver.
var spaceScopedGets = []string{
	"revision get", "mutation get", "release get", "unit-action get", "unit-event get",
}

func findCommand(t *testing.T, path string) *cobra.Command {
	t.Helper()
	cmd, _, err := rootCmd.Find(strings.Split(path, " "))
	if err != nil || cmd == nil {
		t.Fatalf("command %q not found: %v", path, err)
	}
	if cmd.Name() != strings.Fields(path)[len(strings.Fields(path))-1] {
		t.Fatalf("looking for %q found %q; the command was renamed or removed", path, cmd.CommandPath())
	}
	return cmd
}

// TestResolverBackedGetsAllowOmittedSpace pins the annotation to the commands
// that can honor it. A get that resolves by reference but demands a space up
// front rejects references that need none -- "cub filter get <uuid>" and
// "cub filter get other-space/thing" both name a filter unambiguously.
func TestResolverBackedGetsAllowOmittedSpace(t *testing.T) {
	for _, path := range resolverBackedGets {
		cmd := findCommand(t, path)
		if !allowOmittedSpace(cmd) {
			t.Errorf("%q resolves its operand by reference but does not allow an omitted space; "+
				"call enableOptionalSpace in its init", path)
		}
	}
}

// TestSpaceScopedGetsStillRequireSpace is the other half: these commands read
// through a per-space endpoint and would parse an empty space as a UUID, so the
// annotation must not be applied to them before their entity has a resolver.
func TestSpaceScopedGetsStillRequireSpace(t *testing.T) {
	for _, path := range spaceScopedGets {
		cmd := findCommand(t, path)
		if allowOmittedSpace(cmd) {
			t.Errorf("%q still addresses a per-space endpoint, so it cannot run without a space; "+
				"give its entity a resolver before marking it", path)
		}
	}
}

// TestOptionalSpaceImpliesWildcardIsAccepted: a command that can run with no
// space at all must also accept the explicit spelling of the same thing, or
// omitting --space would work where --space '*' did not.
func TestOptionalSpaceImpliesWildcardIsAccepted(t *testing.T) {
	cmd := &cobra.Command{Use: "example"}
	if allowWildcardSpace(cmd) {
		t.Fatal("a bare command must not accept --space '*'")
	}
	enableOptionalSpace(cmd)
	if !allowWildcardSpace(cmd) {
		t.Error("a command that may omit its space must also accept --space '*'")
	}
}

// TestEnableOptionalSpaceKeepsExistingAnnotations guards the map initialization:
// the list commands already carry OrgLevel, and marking one must not drop it.
func TestEnableOptionalSpaceKeepsExistingAnnotations(t *testing.T) {
	cmd := &cobra.Command{Use: "example", Annotations: map[string]string{"OrgLevel": ""}}
	enableOptionalSpace(cmd)
	if _, ok := cmd.Annotations["OrgLevel"]; !ok {
		t.Error("enableOptionalSpace dropped an existing annotation")
	}
	if !allowOmittedSpace(cmd) {
		t.Error("enableOptionalSpace did not take effect")
	}
}

// resolverBackedWrites are the single update and delete commands whose entity
// has a resolver. They take their space from the entity they resolved rather
// than from the selected one, so they run with no space the same way the gets
// do -- but by a different route: OrgLevel already lets spacePreRunE settle on
// "*" for their bulk mode, and nothing downstream reads the selected space.
var resolverBackedWrites = []string{
	"attribute delete", "attribute update", "changeset delete", "changeset update",
	"changeorder delete", "changeorder update", "changeworkflow delete", "changeworkflow update",
	"filter delete", "filter update",
	"invocation delete", "invocation update", "link delete", "link update",
	"tag delete", "tag update", "target delete", "target update",
	"trigger delete", "trigger update", "unit delete", "unit update",
	"view delete", "view update", "worker delete", "worker update",
}

// TestResolverBackedWritesAcceptWildcardSpace pins the gate these depend on. A
// single update or delete resolves its operand and writes through that entity's
// space, so it needs spacePreRunE to let it start without one; OrgLevel is what
// does that. Drop the annotation and "cub filter delete <uuid>" goes back to
// refusing to run.
func TestResolverBackedWritesAcceptWildcardSpace(t *testing.T) {
	for _, path := range resolverBackedWrites {
		cmd := findCommand(t, path)
		if !allowWildcardSpace(cmd) {
			t.Errorf("%q resolves its operand and writes through that entity's space, "+
				"so it must be able to start with no space selected; give it the OrgLevel annotation", path)
		}
	}
}

// TestSingleCreateStillRequiresAConcreteSpace: a create has to put the new
// entity somewhere, so it is the one single operation for which "*" is still
// wrong. Bulk create is unaffected.
func TestSingleCreateStillRequiresAConcreteSpace(t *testing.T) {
	saved := selectedSpaceID
	t.Cleanup(func() { selectedSpaceID = saved })

	selectedSpaceID = "*"
	if err := validateSpaceFlag(false); err == nil {
		t.Error("a single create with --space '*' must be refused: it has no space to create in")
	}
	if err := validateSpaceFlag(true); err != nil {
		t.Errorf("a bulk operation with --space '*' must be allowed, got %v", err)
	}

	selectedSpaceID = "some-space-uuid"
	if err := validateSpaceFlag(false); err != nil {
		t.Errorf("a single create with a concrete space must be allowed, got %v", err)
	}
}
