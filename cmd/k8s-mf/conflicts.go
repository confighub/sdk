// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/confighub/sdk/bridge-impl/kubernetes/mfclass"
	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func newConflictsCommand() *cobra.Command {
	var (
		file    string
		manager string
		output  string
	)
	cmd := &cobra.Command{
		Use:   "conflicts -f <manifest> --manager <name>",
		Short: "Predict which fields an apply would conflict over, without changing anything",
		Long: `Server-side dry-run an apply as a given field manager with conflict-forcing
disabled, and report which fields are owned by other managers and would block the
apply — the same 409 server-side apply would raise, but surfaced as data rather
than an error. Nothing is ever written; this is a prediction.

Use it before switching a resource to a new tool, or before a "break glass"
kubectl apply, to see exactly what you would fight over and who owns it.

Examples:
  k8s-mf conflicts -f deploy.yaml --manager confighub-bridge-worker -n prod
  k8s-mf conflicts -f deploy.yaml --manager argocd-controller -o json`,
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
			data, err := json.Marshal(obj.Object)
			if err != nil {
				return fmt.Errorf("marshal manifest: %w", err)
			}
			force := false
			_, err = ri.Patch(ctx, name, types.ApplyPatchType, data, metav1.PatchOptions{
				FieldManager: manager,
				Force:        &force,
				DryRun:       []string{metav1.DryRunAll},
			})
			if err != nil && !apierrors.IsConflict(err) {
				return fmt.Errorf("dry-run apply: %w", err)
			}
			res := conflictsResult{
				Resource:  resourceRef(obj),
				Manager:   manager,
				Conflicts: parseConflicts(err),
			}
			if output == outputJSON {
				return emitJSON(res)
			}
			renderConflicts(res)
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "Manifest to apply (YAML/JSON, \"-\" for stdin) (required)")
	cmd.Flags().StringVar(&manager, "manager", "", "Field manager to apply as (required)")
	cmd.Flags().StringVarP(&output, "output", "o", outputText, "Output format: text|json")
	return cmd
}

// conflictItem is one field an apply would contend over, plus the manager that
// currently owns it and that manager's category.
type conflictItem struct {
	Field    string `json:"field"`
	Manager  string `json:"manager"`
	Category string `json:"category"`
}

type conflictsResult struct {
	Resource  string         `json:"resource"`
	Manager   string         `json:"manager"`
	Conflicts []conflictItem `json:"conflicts"`
}

// parseConflicts extracts the per-field conflicts from a server-side-apply 409
// StatusError. It returns nil for a nil/non-conflict error. The owning manager
// is read from each cause's message and classified via mfclass.
func parseConflicts(err error) []conflictItem {
	var statusErr *apierrors.StatusError
	if !errors.As(err, &statusErr) || statusErr.Status().Details == nil {
		return nil
	}
	var items []conflictItem
	for _, cause := range statusErr.Status().Details.Causes {
		owner := firstQuoted(cause.Message)
		cat, _ := mfclass.ClassifyManager(owner)
		items = append(items, conflictItem{
			Field:    string(cause.Field),
			Manager:  owner,
			Category: string(cat),
		})
	}
	return items
}

// firstQuoted returns the text inside the first pair of double quotes, or "".
// SSA conflict messages name the owning manager as `conflict with "name" ...`.
func firstQuoted(s string) string {
	_, after, found := strings.Cut(s, `"`)
	if !found {
		return ""
	}
	inner, _, found := strings.Cut(after, `"`)
	if !found {
		return ""
	}
	return inner
}

func renderConflicts(res conflictsResult) {
	if len(res.Conflicts) == 0 {
		fmt.Printf("No conflicts: applying %s as %q would not contend with any other field manager.\n", res.Resource, res.Manager)
		return
	}
	fmt.Printf("Applying %s as %q would conflict with %d field(s):\n", res.Resource, res.Manager, len(res.Conflicts))
	for _, c := range res.Conflicts {
		owner := c.Manager
		if owner == "" {
			owner = "unknown"
		}
		fmt.Printf("  %s  —  owned by %q (%s)\n", c.Field, owner, c.Category)
	}
	fmt.Printf("\nRe-apply with --force (dry-run-apply --force) to take ownership of these fields.\n")
}
