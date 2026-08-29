// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/k8sutil/cleanup"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func newCleanupCommand() *cobra.Command {
	var (
		output string
		file   string
	)
	cmd := &cobra.Command{
		Use:   "cleanup [TYPE NAME]",
		Short: "Show the result of the refresh/import cleanup (ExtraCleanupObjects)",
		Long: `Run ExtraCleanupObjects on a resource and print the result — exactly the
transformation "cub k8s refresh" (and import) apply before comparing live
cluster state against a Unit's configuration data.

Unlike "values" (which projects via managed-field set algebra), this calls the
real cleanup: RemoveUnmanagedFields, status/managedFields removal, internal
annotation/label stripping, and resource-quantity normalization. Use it to debug
cases where unexpected fields are carried into config data — e.g. a system-added
spec.finalizers surviving because an empty "f:spec: {}" ownership is treated as
atomic, which keeps the whole subtree.

The object is read from the live cluster (TYPE NAME) or from a file (-f); a file
must include metadata.managedFields.

Examples:
  k8s-mf cleanup namespace test-ns
  kubectl get deploy my-app -o yaml --show-managed-fields | k8s-mf cleanup -f -`,
		Args: cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			obj, _, err := inputObject(cmd.Context(), file, args)
			if err != nil {
				return err
			}
			cleaned := cleanup.ExtraCleanupObjects([]*unstructured.Unstructured{obj})
			if len(cleaned) == 0 {
				return emitObject(&unstructured.Unstructured{Object: map[string]interface{}{}}, output)
			}
			return emitObject(cleaned[0], output)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", outputYAML, "Output format: yaml|json")
	cmd.Flags().StringVarP(&file, "file", "f", "", "Read the object from a YAML/JSON file (\"-\" for stdin) instead of the cluster")
	return cmd
}
