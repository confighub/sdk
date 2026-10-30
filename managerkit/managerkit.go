// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package managerkit holds what manager tools -- command-line tools that each
// manage one concern across a fleet, such as namespaces, scheduling or
// autoscaling -- agree on. Its subpackages are the shared parts of such a tool:
// guardrails installs a policy pack, profiles a library of reusable edits, and
// write reports what a mutating command did.
//
// Reading a fleet is github.com/confighub/sdk/core/cubapi (SnapshotLoader,
// MemoizedClient), and the generic command-line surface is
// github.com/confighub/sdk/cliutil (QueryFlags and its Space-label shorthands,
// the output and table helpers). What is here is a convention of manager tools
// rather than a ConfigHub primitive.
package managerkit

// CommonSpace is the Space manager tools put whatever an organization keeps in
// common: the guardrail Triggers, the profile library, the Attributes a
// validating function reads.
//
// One Space rather than one per tool. A Space selects guardrails with one
// WhereTrigger naming one policy Space and the groups it has opted into, so
// per-tool policy Spaces could not be combined in it. Profiles have no such
// constraint, but an operator should still have one place to look rather than
// one per tool they happen to have installed.
//
// Every command that writes here takes a flag to put it elsewhere.
const CommonSpace = "common"
