// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	installerapi "github.com/confighub/installer/pkg/api"
	"github.com/confighub/installer/pkg/installer"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var packageCmd = &cobra.Command{
	Use:   "package",
	Short: "Install packages into ConfigHub, and show what is installed",
	Long: `Install packages into ConfigHub, and show what is installed.

A package is an installer package: a base and optional components of
Kubernetes configuration, with inputs that are answered at install time. cub
links ConfigHub's installer, so nothing else needs installing. Each install
keeps an installer record in its Space, which install reads back to upgrade it,
and show and list read to say what is installed. Authoring and publishing
packages is the installer's job (cub installer, or the installer binary).`,
	PersistentPreRunE: globalPreRun,
}

type packageInstallFlags struct {
	space          string
	workDir        string
	base           string
	namespace      string
	selects        []string
	components     string
	inputs         []string
	setImages      []string
	nonInteractive bool
	reuse          bool
	variant        string
	target         string
	spacePattern   string
	yes            bool
}

var packageInstallArgs packageInstallFlags

var packageInstallCmd = &cobra.Command{
	Use:   "install <package> --space <space>",
	Short: "Install a package into a Space: pull it, answer its inputs, render it and upload it",
	Long: `Install a package into a Space: pull it, run its wizard, render it, and
upload the result, one Unit per resource, with an installer record that a
later install reads back.

<package> is one of:
  <name>[:<version>]   a package ConfigHub publishes, from
                       ` + installerapi.PackageRepository + `/<name>; without a
                       version, its newest release
  oci://<ref>          a package in any OCI registry
  <dir> or <file.tgz>  a package on this machine, written as a path: with a
                       / (./my-package), or ending in .tgz

--space is required. A Space that does not exist yet is created. When it
already holds an install of the package, its installer record is re-entered:
the answers it recorded still apply, so installing again, or installing a
newer version, upgrades it. A package with dependencies uploads each of them
into a Space of its own, named by --space-pattern.

The wizard asks for what the flags do not answer when stdin is a terminal;
--non-interactive never asks. An upload that would empty Units whose
resources are no longer rendered asks first, unless --yes is given.

Install does not release anything: the Space's Units are released to its
Target as any others are.

Rendered Secrets are never uploaded. Without --work-dir, the install works
in a temporary directory, which is removed unless the package rendered
Secrets; then the directory is kept and named.`,
	Example: `  # Install k8sbot's newest package into a new Space.
  cub package install k8sbot --space k8sbot-base --select argocd --namespace confighubbot

  # Upgrade it to a given version, keeping its answers.
  cub package install k8sbot:v0.2.13 --space k8sbot-base

  # Install a package from another registry, answering an input.
  cub package install oci://ghcr.io/example/packages/app:v1.2.0 --space app \
    --namespace app --input replicas=3`,
	Args: cobra.ExactArgs(1),
	RunE: packageInstallCmdRun,
}

func init() {
	f := packageInstallCmd.Flags()
	f.StringVar(&packageInstallArgs.space, "space", "", "Space to install into, by slug; created if it does not exist (required)")
	f.StringVar(&packageInstallArgs.workDir, "work-dir", "", "directory to keep the install's state in (default: a temporary directory)")
	f.StringVar(&packageInstallArgs.base, "base", "", "the package's base to install (default: its default base, or the earlier install's)")
	f.StringVar(&packageInstallArgs.namespace, "namespace", "", "Kubernetes namespace to install into; a first install that does not prompt needs it")
	f.StringSliceVar(&packageInstallArgs.selects, "select", nil, "component to install (repeatable); the components it requires come with it")
	f.StringVar(&packageInstallArgs.components, "components", "", "component preset instead of --select: minimal, default, all, or selected")
	f.StringArrayVar(&packageInstallArgs.inputs, "input", nil, "answer to one of the package's inputs, as name=value (repeatable)")
	f.StringArrayVar(&packageInstallArgs.setImages, "set-image", nil, "image override as name=ref (repeatable): name is the image without its tag or digest")
	f.BoolVar(&packageInstallArgs.nonInteractive, "non-interactive", false, "never prompt; what the flags and the earlier install do not answer is an error")
	f.BoolVar(&packageInstallArgs.reuse, "reuse", false, "take the earlier install's choices as they are, without prompting")
	f.StringVar(&packageInstallArgs.variant, "variant", "", "value for the well-known \"Variant\" Space label (default: the Space's own, or base)")
	f.StringVar(&packageInstallArgs.target, "target", "", "the Space's release Target, in <target-slug> or <space-slug>/<target-slug> form")
	f.StringVar(&packageInstallArgs.spacePattern, "space-pattern", "", "Go template for the Spaces of the package's dependencies, over .PackageName, .PackageVersion and .Variant (default {{.PackageName}})")
	f.BoolVar(&packageInstallArgs.yes, "yes", false, "do not ask for confirmation when the upload would empty Units")
	_ = packageInstallCmd.MarkFlagRequired("space")
	packageInstallCmd.MarkFlagsMutuallyExclusive("select", "components")
	packageCmd.AddCommand(packageInstallCmd)
	rootCmd.AddCommand(packageCmd)
}

func packageInstallCmdRun(cmd *cobra.Command, args []string) error {
	a := packageInstallArgs
	ref := args[0]
	inputs, err := keyValueFlags("--input", a.inputs)
	if err != nil {
		return err
	}
	images, err := keyValueFlags("--set-image", a.setImages)
	if err != nil {
		return err
	}

	inst, err := newCubInstaller(os.Stdout)
	if err != nil {
		return err
	}
	workDir, temporary := a.workDir, a.workDir == ""
	if temporary {
		if workDir, err = os.MkdirTemp("", "cub-package-*"); err != nil {
			return err
		}
	}
	opts := installer.InstallOptions{
		Space: a.space,
		Setup: installer.SetupOptions{
			WorkDir:   workDir,
			Pull:      ref,
			Base:      a.base,
			Namespace: a.namespace,
			Select:    a.selects,
			Preset:    a.components,
			Inputs:    inputs,
			SetImage:  images,
			Reuse:     a.reuse,
		},
		Upload: installer.UploadOptions{
			SpacePattern: a.spacePattern,
			Variant:      a.variant,
			Target:       a.target,
		},
	}
	stdinIsTerminal := term.IsTerminal(int(os.Stdin.Fd()))
	if !a.nonInteractive && stdinIsTerminal {
		opts.Setup.Terminal = &installer.Terminal{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}
	}
	if !a.yes {
		noConsole := ""
		switch {
		case a.nonInteractive:
			noConsole = "--non-interactive never asks"
		case !stdinIsTerminal:
			noConsole = "there is no terminal to confirm on"
		}
		opts.ConfirmEmpty = func(units []string) error {
			return confirmEmpties(units, "the render", noConsole)
		}
	}

	fmt.Fprintf(os.Stderr, "Installing %s into Space %s...\n", ref, a.space)
	res, err := inst.Install(ctx, opts)
	if temporary {
		keepPackageWorkDir(workDir, err)
	}
	if err != nil {
		return packageInstallError(err)
	}
	if res.Upload.Failed() {
		return fmt.Errorf("some writes failed; the rest landed, and installing again retries the failures")
	}
	return nil
}

// newCubInstaller is the installer cub runs packages with.
func newCubInstaller(out io.Writer) (*installer.Installer, error) {
	opts, err := cubInstallerOptions(out)
	if err != nil {
		return nil, err
	}
	return installer.New(opts)
}

// cubInstallerOptions run the installer through cub's client, printing to
// out. Registries are read with the credentials Docker stores, except
// ConfigHub's own packages, which the installer reads anonymously. The
// installer's own settings still apply: the signing policy it enforces on
// what it pulls, and the cosign binary that checks it.
func cubInstallerOptions(out io.Writer) (installer.Options, error) {
	policy, err := installer.LoadSigningPolicy(os.Getenv("INSTALLER_SIGNING_POLICY"))
	if err != nil {
		return installer.Options{}, err
	}
	return installer.Options{
		ConfigHub:     cubClient,
		Out:           out,
		ErrOut:        os.Stderr,
		SigningPolicy: policy,
		CosignBinary:  os.Getenv("INSTALLER_COSIGN_BIN"),
		ClientVersion: Version,
	}, nil
}

// keyValueFlags reads name=value flag values, split at the first "=".
func keyValueFlags(flag string, values []string) (map[string]string, error) {
	m := map[string]string{}
	for _, v := range values {
		key, value, ok := strings.Cut(v, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("%s %q must be name=value", flag, v)
		}
		m[key] = value
	}
	return m, nil
}

// keepPackageWorkDir removes a temporary work-dir, unless the package rendered
// Secrets into it, which are never uploaded and would be lost; then it says
// where they are.
func keepPackageWorkDir(workDir string, installErr error) {
	if installErr == nil {
		if secrets, _ := filepath.Glob(filepath.Join(workDir, "out", "secrets", "*")); len(secrets) > 0 {
			fmt.Fprintf(os.Stderr, "The package rendered Secrets, which are not uploaded. They are in %s; apply them to the cluster yourself.\n", filepath.Join(workDir, "out", "secrets"))
			return
		}
	}
	os.RemoveAll(workDir)
}

// packageInstallError adds the cub flag that remedies an install failure, when
// there is one.
func packageInstallError(err error) error {
	var newRequired *installer.NewRequiredInputsError
	switch {
	case errors.Is(err, installer.ErrNotAuthenticated):
		return fmt.Errorf("%w; run cub auth login", err)
	case errors.Is(err, installer.ErrNamespaceRequired):
		return fmt.Errorf("%w: pass --namespace", err)
	case errors.Is(err, installer.ErrNoPriorInstall):
		return fmt.Errorf("--reuse needs an earlier install of the package in the Space: %w", err)
	case errors.Is(err, installer.ErrInputTypesChanged):
		return fmt.Errorf("%w: pass --input <name>=<value> for each, or install on a terminal to be asked", err)
	case errors.As(err, &newRequired):
		names := make([]string, 0, len(newRequired.Inputs))
		for _, in := range newRequired.Inputs {
			names = append(names, in.Name)
		}
		return fmt.Errorf("%w: pass --input <name>=<value> for %s, or install on a terminal to be asked", err, strings.Join(names, ", "))
	}
	return err
}
