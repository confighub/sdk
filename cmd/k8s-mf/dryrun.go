// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func newDryRunApplyCommand() *cobra.Command {
	var (
		file              string
		manager           string
		force             bool
		commit            bool
		showDiff          bool
		showManagedFields bool
		output            string
	)
	cmd := &cobra.Command{
		Use:   "dry-run-apply -f <manifest> --manager <name>",
		Short: "Server-side dry-run an apply as a given field manager",
		Long: `Server-side apply a manifest as a given field manager and show the resulting
merged resource — without persisting it (dry run by default). This answers
"what will happen, and what will I fight over, if I apply this as <manager>?".

When the apply would conflict with fields owned by another manager (and --force
is not set), the conflicting fields and their owners are listed — exactly the
information needed to understand an apply surprise before it happens.

This reaches the cluster (server dry-run mutates nothing). Pass --commit to
actually apply.

Examples:
  k8s-mf dry-run-apply -f deploy.yaml --manager kustomize-controller -n prod
  k8s-mf dry-run-apply -f deploy.yaml --manager argocd-controller --show-diff
  k8s-mf dry-run-apply -f deploy.yaml --manager kustomize-controller --force --commit`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" {
				return fmt.Errorf("-f/--file (the manifest to apply) is required")
			}
			if manager == "" {
				return fmt.Errorf("--manager (the field manager to apply as) is required")
			}
			ctx := cmd.Context()
			obj, err := loadObjectFromFile(file)
			if err != nil {
				return err
			}
			name := obj.GetName()
			if name == "" {
				return fmt.Errorf("manifest has no metadata.name")
			}
			c, err := newCluster()
			if err != nil {
				return err
			}
			mapping, err := c.mappingForObject(obj)
			if err != nil {
				return err
			}
			ri := c.resourceInterface(mapping, obj.GetNamespace())

			var current *unstructured.Unstructured
			if showDiff {
				// Best effort; the object may not exist yet.
				current, _ = ri.Get(ctx, name, metav1.GetOptions{})
			}

			data, err := json.Marshal(obj.Object)
			if err != nil {
				return fmt.Errorf("marshal manifest: %w", err)
			}
			opts := metav1.PatchOptions{FieldManager: manager, Force: &force}
			if !commit {
				opts.DryRun = []string{metav1.DryRunAll}
			}
			result, err := ri.Patch(ctx, name, types.ApplyPatchType, data, opts)
			if err != nil {
				if apierrors.IsConflict(err) {
					printConflicts(err, manager)
					return fmt.Errorf("apply as %q would conflict; re-run with --force to take ownership", manager)
				}
				return fmt.Errorf("apply: %w", err)
			}

			if commit {
				fmt.Printf("# Applied as %q.\n", manager)
			} else {
				fmt.Printf("# Server dry-run as %q (nothing persisted).\n", manager)
			}
			if showDiff {
				renderDiffHeader(current)
			}
			if !showManagedFields {
				result.SetManagedFields(nil)
			}
			return emitObject(result, output)
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "Manifest to apply (YAML/JSON, \"-\" for stdin) (required)")
	cmd.Flags().StringVar(&manager, "manager", "", "Field manager to apply as (required)")
	cmd.Flags().BoolVar(&force, "force", false, "Force conflicts (take ownership of conflicting fields)")
	cmd.Flags().BoolVar(&commit, "commit", false, "Actually apply instead of server dry-run")
	cmd.Flags().BoolVar(&showDiff, "show-diff", false, "Show the current object before the merged result")
	cmd.Flags().BoolVar(&showManagedFields, "show-managed-fields", false, "Include metadata.managedFields in the result")
	cmd.Flags().StringVarP(&output, "output", "o", outputYAML, "Output format: yaml|json")
	return cmd
}

// printConflicts lists the fields and owning managers an apply would contend
// with, parsed from the server's 409 Conflict status (shared with the conflicts
// command).
func printConflicts(err error, manager string) {
	conflicts := parseConflicts(err)
	if len(conflicts) == 0 {
		// Fall back to the raw message if structured causes are absent.
		fmt.Printf("Applying as %q conflicts with another manager:\n  %s\n", manager, err.Error())
		return
	}
	fmt.Printf("Applying as %q would conflict with %d field(s):\n", manager, len(conflicts))
	for _, c := range conflicts {
		owner := c.Manager
		if owner == "" {
			owner = "unknown"
		}
		fmt.Printf("  %s  —  owned by %q (%s)\n", c.Field, owner, c.Category)
	}
}

// renderDiffHeader prints the current object, then a header for the merged
// result (which the caller emits next).
func renderDiffHeader(current *unstructured.Unstructured) {
	fmt.Println("# --- current (before) ---")
	if current == nil {
		fmt.Println("# (does not exist yet)")
	} else {
		c := current.DeepCopy()
		c.SetManagedFields(nil)
		_ = emitYAML(c.Object)
	}
	fmt.Println("# --- result (after) ---")
}
