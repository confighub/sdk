// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// referenceOperandCommands name their operand by reference and resolve it, so
// they can run with no space: a UUID identifies the entity outright, a
// "space/slug" carries its own space, and a bare slug is searched for across
// the organization. Every API call they make afterwards addresses the space of
// the entity they resolved, never the selected one, which is what lets a
// qualified reference reach another space than --space or the default.
//
// The revision, mutation, unit-action, unit-event, release and attestation commands
// have no resolver of their own: they resolve the unit (or, for a release or an
// attestation, locate it by UUID organization-wide) and read through its space.
var referenceOperandCommands = []string{
	"attribute get", "changeset get", "changeorder get", "changeworkflow get", "filter get",
	"invocation get", "link get", "tag get", "target get", "trigger get",
	"unit get", "view get", "worker get",
	"unit blame", "unit conflicts", "unit data", "unit diff", "unit edit", "unit mutation-sources",
	"unit set-guard", "unit set-protection",
	"revision get", "revision data", "mutation get", "mutation list", "release get",
	"attestation get", "attestation revoke",
	"unit-action get", "unit-action data", "unit-event get",
	"target access", "k8s collect",
	"worker get-envs", "worker get-secret", "worker list-function", "worker list-status",
	"worker logs", "worker status", "worker stop",
	"worker key add", "worker key list", "worker key delete",
	"worker get-image", "worker run", "worker install", "worker upgrade",
	"user key add", "user key list", "user key delete",
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

// TestReferenceOperandCommandsAllowOmittedSpace pins the annotation to the
// commands that can honor it. A command that resolves by reference but demands
// a space up front rejects references that need none -- "cub filter get <uuid>"
// and "cub filter get other-space/thing" both name a filter unambiguously.
func TestReferenceOperandCommandsAllowOmittedSpace(t *testing.T) {
	for _, path := range referenceOperandCommands {
		cmd := findCommand(t, path)
		if !allowOmittedSpace(cmd) {
			t.Errorf("%q resolves its operand by reference but does not allow an omitted space; "+
				"call enableOptionalSpace in its init", path)
		}
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
		if _, ok := cmd.Annotations["OrgLevel"]; !ok {
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
