// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/confighub/sdk/k8sutil/mfclass"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"
)

func newValuesCommand() *cobra.Command {
	var (
		output          string
		file            string
		manager         string
		category        string
		includeDefaults bool
	)
	cmd := &cobra.Command{
		Use:   "values [TYPE NAME]",
		Short: "Show the values of fields owned by appliers",
		Long: `Project a resource down to just the values owned by appliers (or a chosen
manager or category), rendering a reduced object. This is what an applier such
as ArgoCD or Flux effectively "sees" as its config, which makes tool
transitions easier to reason about.

By default it shows fields owned by every applier. Narrow with --manager or
--category, or add --include-defaults to also include API-server default fields.

The object is read from the live cluster (TYPE NAME) or from a file (-f).

Examples:
  k8s-mf values deployment my-app -n prod
  k8s-mf values deployment my-app --manager argocd-controller
  k8s-mf values deployment my-app --category Applier --include-defaults`,
		Args: cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if manager == "" && !validCategory(category) {
				return fmt.Errorf("invalid --category %q; valid values: All, %s", category, strings.Join(categoryNames(), ", "))
			}
			obj, proj, err := inputObject(cmd.Context(), file, args)
			if err != nil {
				return err
			}
			a, err := analyze(obj, proj)
			if err != nil {
				return err
			}
			out, err := buildValues(a, manager, category, includeDefaults)
			if err != nil {
				return err
			}
			return emitObject(out, output)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", outputYAML, "Output format: yaml|json")
	cmd.Flags().StringVarP(&file, "file", "f", "", "Read the object from a YAML/JSON file (\"-\" for stdin) instead of the cluster")
	cmd.Flags().StringVar(&manager, "manager", "", "Show only fields owned by this exact field manager")
	cmd.Flags().StringVar(&category, "category", string(mfclass.CategoryApplier), "Category to show: All|"+strings.Join(categoryNames(), "|"))
	cmd.Flags().BoolVar(&includeDefaults, "include-defaults", false, "Also include API-server default (unmanaged) fields")
	return cmd
}

// categoryNames returns the valid category strings for --category.
func categoryNames() []string {
	cats := mfclass.Categories()
	names := make([]string, len(cats))
	for i, c := range cats {
		names[i] = string(c)
	}
	return names
}

// validCategory reports whether s is "all" or a known category.
func validCategory(s string) bool {
	if s == "All" || s == "" {
		return true
	}
	for _, c := range mfclass.Categories() {
		if string(c) == s {
			return true
		}
	}
	return false
}

// buildValues projects the selected field sets and deep-merges them into one
// object: object identity, then each selected manager's owned fields, then
// (optionally) the default fields. Each set is projected individually and
// merged rather than unioned-then-projected, because structured-merge-diff can
// drop fields when projecting a union of separately-parsed manager sets.
func buildValues(a *analysis, manager, category string, includeDefaults bool) (*unstructured.Unstructured, error) {
	result := map[string]interface{}{}
	merge := func(set *fieldpath.Set) error {
		p, err := a.proj.Project(a.obj, set)
		if err != nil {
			return err
		}
		deepMerge(result, p.Object)
		return nil
	}
	if err := merge(mfclass.IdentityFieldSet()); err != nil {
		return nil, err
	}
	for _, o := range a.owners {
		if selectOwner(o, manager, category) {
			if err := merge(o.set); err != nil {
				return nil, err
			}
		}
	}
	if includeDefaults {
		if err := merge(a.defaultFieldSet()); err != nil {
			return nil, err
		}
	}
	return &unstructured.Unstructured{Object: result}, nil
}

// deepMerge recursively merges src into dst. Nested maps are merged; scalars
// and lists from src overwrite dst.
func deepMerge(dst, src map[string]interface{}) {
	for k, v := range src {
		if sv, ok := v.(map[string]interface{}); ok {
			if dv, ok := dst[k].(map[string]interface{}); ok {
				deepMerge(dv, sv)
				continue
			}
		}
		dst[k] = v
	}
}

// selectOwner decides whether an owner's fields are included. --manager (exact
// name) takes precedence; otherwise the owner's category must match (or
// category == "all").
func selectOwner(o managerOwnership, manager, category string) bool {
	if manager != "" {
		return o.entry.Manager == manager
	}
	if category == "All" || category == "" {
		return true
	}
	return string(o.class.Category) == category
}
