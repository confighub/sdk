// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/confighub/installer/pkg/installer"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

var packageShowArgs struct {
	space string
}

var packageShowCmd = &cobra.Command{
	Use:   "show [<package>] [--space <space>]",
	Short: "Show what was installed in a Space, from its installer record",
	Long: `Show what was installed in a Space, from the installer record an install keeps
there: the package and where it was pulled from, the base and the components
selected among those the package offers, each input's answer beside its
default, the facts collected, the dependencies locked, the cluster
requirements of what was selected, and the functions the render ran.

Name the Space with --space, and the package too when the Space holds more
than one package's install. Without --space, the package's only install in the
organization is shown.

-o json and -o yaml print the record as it is stored.`,
	Example: `  cub package show --space k8sbot-base
  cub package show k8sbot
  cub package show k8sbot --space k8sbot-base -o yaml`,
	Args: cobra.MaximumNArgs(1),
	RunE: packageShowCmdRun,
}

var packageListArgs struct {
	space string
}

var packageListCmd = &cobra.Command{
	Use:   "list [<package>] [--space <space>]",
	Short: "List the packages installed in the organization, from their installer records",
	Long: `List the packages installed in the organization, or in one Space with --space,
from the installer records installs keep: each install's Space, package,
version, namespace, and where the package was pulled from. Name a package to
list only its installs. A package with dependencies has an install of each
dependency in a Space of its own.

-o json and -o yaml print each install with its record as it is stored.`,
	Example: `  cub package list
  cub package list k8sbot
  cub package list --space k8sbot-base`,
	Args: cobra.MaximumNArgs(1),
	RunE: packageListCmdRun,
}

func init() {
	packageShowCmd.Flags().StringVar(&packageShowArgs.space, "space", "", "Space the package is installed in, by slug or UUID (default: the package's only install in the organization)")
	addStandardDisplayFlags(packageShowCmd)
	packageCmd.AddCommand(packageShowCmd)

	packageListCmd.Flags().StringVar(&packageListArgs.space, "space", "", "list only the installs in this Space, by slug or UUID")
	enableNoheaderFlag(packageListCmd)
	addStandardDisplayFlags(packageListCmd)
	packageCmd.AddCommand(packageListCmd)
}

func packageShowCmdRun(cmd *cobra.Command, args []string) error {
	if err := checkPackageOutputFormat(cmd); err != nil {
		return err
	}
	space, packageName := packageSpaceFlag(packageShowArgs.space), ""
	if len(args) == 1 {
		packageName = args[0]
	}
	if space == "" && packageName == "" {
		return fmt.Errorf("name the package, or the Space with --space")
	}
	inst, err := newCubInstaller(os.Stderr)
	if err != nil {
		return err
	}
	found, err := inst.FindRecord(ctx, space, packageName)
	if err != nil {
		return packageRecordError(err, space)
	}
	if !quiet && !isAlternativeOutput() {
		return installer.WriteRecord(os.Stdout, found)
	}
	doc, err := storedRecord(found)
	if err != nil {
		return err
	}
	renderPayload(doc)
	return nil
}

// packageInstall is an install as cub package list prints it with -o.
type packageInstall struct {
	Space     string
	Package   string
	Version   string
	Namespace string `json:",omitempty"`
	Source    string `json:",omitempty"`
	// Record is the installer record as it is stored.
	Record any
}

func packageListCmdRun(cmd *cobra.Command, args []string) error {
	if err := checkPackageOutputFormat(cmd); err != nil {
		return err
	}
	packageName := ""
	if len(args) == 1 {
		packageName = args[0]
	}
	// The installer reads a Space it can't find as one with nothing
	// installed, which would make a misspelled --space an empty list.
	space := packageSpaceFlag(packageListArgs.space)
	if space != "" {
		found, err := resolveSpace(space, "SpaceID,Slug")
		if err != nil {
			return err
		}
		space = found.Space.Slug
	}
	inst, err := newCubInstaller(os.Stderr)
	if err != nil {
		return err
	}
	records, err := inst.ListRecords(ctx, space, packageName)
	if err != nil {
		return packageInstallError(err)
	}
	installs, err := packageInstalls(records)
	if err != nil {
		return err
	}
	if !quiet && !isAlternativeOutput() {
		table := tableView()
		if !noheader {
			table.SetHeader([]string{"Space", "Package", "Version", "Namespace", "Source"})
		}
		for _, in := range installs {
			table.Append([]string{in.Space, in.Package, in.Version, in.Namespace, in.Source})
		}
		table.Render()
	}
	renderPayload(installs)
	return nil
}

// packageSpaceFlag reads a --space value, where "*" is the whole
// organization, as on every command.
func packageSpaceFlag(space string) string {
	if space == "*" {
		return ""
	}
	return space
}

// checkPackageOutputFormat refuses the output formats show and list have
// nothing to print for, rather than printing nothing.
func checkPackageOutputFormat(cmd *cobra.Command) error {
	if err := rejectRequestedColumns(cmd); err != nil {
		return err
	}
	switch effectiveOutput().Kind {
	case OutputName, OutputMutations:
		return fmt.Errorf("%s does not support --output=%s; use -o json with -o jq=<expr>", cmd.CommandPath(), outputFormat)
	default:
		return nil
	}
}

// packageRecordError adds the cub flag or argument that picks one install
// when FindRecord found several.
func packageRecordError(err error, space string) error {
	switch {
	case errors.Is(err, installer.ErrSeveralPackages):
		return fmt.Errorf("%w; name the package: cub package show <package> --space %s", err, space)
	case errors.Is(err, installer.ErrSeveralInstalls):
		return fmt.Errorf("%w; name the Space with --space", err)
	}
	return packageInstallError(err)
}

// packageInstalls is the rows cub package list prints, one per record.
func packageInstalls(records []*installer.InstalledRecord) ([]packageInstall, error) {
	installs := make([]packageInstall, 0, len(records))
	for _, r := range records {
		doc, err := storedRecord(r)
		if err != nil {
			return nil, err
		}
		in := packageInstall{Space: r.Space, Package: r.Package(), Version: r.Version(), Record: doc}
		if r.Record.Inputs != nil {
			in.Namespace = r.Record.Inputs.Namespace
		}
		if r.Record.Source != nil {
			in.Source = r.Record.Source.Ref
		}
		installs = append(installs, in)
	}
	return installs, nil
}

// storedRecord is an installer record as it is stored, for -o json and -o
// yaml, which would otherwise print the parsed record's empty fields.
func storedRecord(r *installer.InstalledRecord) (any, error) {
	var doc any
	if err := yaml.Unmarshal(r.Data, &doc); err != nil {
		return nil, fmt.Errorf("parse the installer record in Space %s: %w", r.Space, err)
	}
	return doc, nil
}
