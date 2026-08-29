// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/confighub/sdk/k8sutil/mfclass"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func newTakeoverCommand() *cobra.Command {
	var (
		keeper         string
		removeManagers []string
		removeCategory string
		dryRun         bool
		assumeYes      bool
	)
	cmd := &cobra.Command{
		Use:   "takeover TYPE NAME --manager <keeper>",
		Short: "Remove other appliers' field managers so one applier owns the resource",
		Long: `Remove the managedFields entries of competing appliers so that a single
applier (the keeper) can own the resource on its next server-side apply. This
is the fix for "I switched from kubectl/ArgoCD/Flux to X but X can't
change/delete a field someone else still owns".

By default it removes every applier manager except the keeper (kubectl, ArgoCD,
Flux, Helm, Sveltos, Tanka, the legacy ConfigHub managers, …) and preserves
controller-owned fields (HPA, VPA, …). Scope it with --remove-manager or
--remove-category.

Note: removing an entry does not transfer its values — it makes those fields
unowned. The keeper claims (and may prune) them on its next apply.

This mutates the cluster. It prints the JSON patch and asks for confirmation
unless --yes is given; --dry-run prints the patch without applying it.

Examples:
  k8s-mf takeover deployment my-app --manager flux -n prod --dry-run
  k8s-mf takeover deployment my-app --manager argocd-controller --yes`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if keeper == "" {
				return fmt.Errorf("--manager (the keeper) is required")
			}
			ctx := cmd.Context()
			c, err := newCluster()
			if err != nil {
				return err
			}
			mapping, err := c.mappingFor(args[0])
			if err != nil {
				return err
			}
			ri := c.resourceInterface(mapping, flagNamespace)
			obj, err := ri.Get(ctx, args[1], metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("get %s/%s: %w", args[0], args[1], err)
			}

			var keep []metav1.ManagedFieldsEntry
			var removed []string
			for _, mf := range obj.GetManagedFields() {
				if removeManager(mf, keeper, removeManagers, removeCategory) {
					removed = append(removed, fmt.Sprintf("%s (op=%s)", mf.Manager, mf.Operation))
				} else {
					keep = append(keep, mf)
				}
			}

			fmt.Printf("Resource: %s\n", resourceRef(obj))
			fmt.Printf("Keeper:   %s\n", keeper)
			if len(removed) == 0 {
				fmt.Println("\nNo matching applier managers to remove — nothing to do.")
				return nil
			}
			fmt.Printf("\nWill remove %d manager(s):\n", len(removed))
			for _, r := range removed {
				fmt.Printf("  - %s\n", r)
			}

			patch, err := managedFieldsPatch(keep)
			if err != nil {
				return err
			}
			fmt.Printf("\nJSON patch:\n%s\n", string(patch))

			if dryRun {
				fmt.Println("\n--dry-run: not applied.")
				return nil
			}
			if !assumeYes && !confirm("\nApply this patch to the cluster?") {
				return fmt.Errorf("aborted")
			}
			if _, err := ri.Patch(ctx, obj.GetName(), types.JSONPatchType, patch, metav1.PatchOptions{}); err != nil {
				return fmt.Errorf("patch managedFields: %w", err)
			}
			fmt.Printf("\nDone. %d manager(s) removed; %q can now take ownership on its next apply.\n", len(removed), keeper)
			return nil
		},
	}
	cmd.Flags().StringVar(&keeper, "manager", "", "The field manager to keep (required)")
	cmd.Flags().StringSliceVar(&removeManagers, "remove-manager", nil, "Remove only these exact managers (repeatable); overrides the default applier set")
	cmd.Flags().StringVar(&removeCategory, "remove-category", "", "Remove all managers in this category instead of the default applier set")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the patch without applying it")
	cmd.Flags().BoolVar(&assumeYes, "yes", false, "Skip the confirmation prompt")
	return cmd
}

// removeManager decides whether a managedFields entry should be dropped. The
// keeper is never removed. --remove-manager (explicit list) wins, then
// --remove-category, then the default policy (every other applier, mirroring
// the bridge's mfclass.ShouldTakeOver).
func removeManager(mf metav1.ManagedFieldsEntry, keeper string, removeManagers []string, removeCategory string) bool {
	if mf.Manager == keeper {
		return false
	}
	if len(removeManagers) > 0 {
		for _, m := range removeManagers {
			if mf.Manager == m {
				return true
			}
		}
		return false
	}
	if removeCategory != "" {
		cat, _ := mfclass.ClassifyManager(mf.Manager)
		return string(cat) == removeCategory
	}
	return mfclass.ShouldTakeOver(mf.Manager, keeper)
}

// managedFieldsPatch builds a JSON patch that replaces metadata.managedFields
// with keep (or removes it entirely when keep is empty), matching the approach
// the bridge uses so only managedFields is touched.
func managedFieldsPatch(keep []metav1.ManagedFieldsEntry) ([]byte, error) {
	if len(keep) == 0 {
		return []byte(`[{"op":"remove","path":"/metadata/managedFields"}]`), nil
	}
	b, err := json.Marshal(keep)
	if err != nil {
		return nil, fmt.Errorf("marshal kept managedFields: %w", err)
	}
	return []byte(fmt.Sprintf(`[{"op":"replace","path":"/metadata/managedFields","value":%s}]`, b)), nil
}

func confirm(prompt string) bool {
	fmt.Printf("%s [y/N]: ", prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes"
}
