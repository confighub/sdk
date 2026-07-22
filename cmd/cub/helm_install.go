// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"

	"github.com/confighub/sdk/bridge-impl/helmutils"
)

// helmInstallCmd installs a Helm chart as (part of) a ConfigHub component.
var helmInstallCmd = &cobra.Command{
	Use:   "install <release-name> <chart-ref>",
	Short: "Install a Helm chart as a ConfigHub component",
	Long: getCommandHelp(`Render a Helm chart client-side and install it as a ConfigHub component.

The chart reference may be an oci:// reference, a local chart directory, or a
chart name resolved against --repo.

Install creates two spaces (if missing): the base variant space
<component>-base holding the rendered units, and the helm source space
<component>-helm holding one HelmSource unit per release with the chart
reference, values, and options. The component defaults to the release name.

Rendered output becomes one unit per chart template file, named from the
chart's file layout: templates/backend.yaml becomes unit "backend",
templates/rbac/role.yaml becomes "rbac-role", crds/foo.yaml becomes
"crds-foo", and subchart files are prefixed with the subchart name. When a
component contains multiple releases, each release's units are namespaced
with --prefix (defaulted to the release name for the second and later
releases).

The base is untargeted. If --namespace is not given, the release renders with
the confighubplaceholder namespace, which each deployment fills:

  cub variant create <variant> <component>-base --target <space>/<target> --namespace <ns>

Hook manifests are dropped by default because Helm's hook lifecycle cannot run
without Helm; --include-hooks keeps them as plain resources. The lookup
template function returns nothing and capabilities are Helm's defaults —
charts that depend on cluster access are out of scope.

Examples:
`+"```"+`
  # Install a chart as component "cubbychat" (spaces cubbychat-helm and cubbychat-base)
  cub helm install cubbychat oci://ghcr.io/confighub/charts/cubbychat

  # Add a second chart to the same component; its units are prefixed "pg-"
  cub helm install --component cubbychat --prefix pg pg oci://registry-1.docker.io/bitnamicharts/postgresql

  # Explicit namespace and a synthesized Namespace unit
  cub helm install --namespace cert-manager --create-namespace cert-manager jetstack/cert-manager --version v1.17.1
`+"```"+`
`, ""),
	Args:          cobra.ExactArgs(2),
	RunE:          helmInstallCmdRun,
	SilenceUsage:  true,
	SilenceErrors: true,
}

var helmInstallArgs struct {
	component       string
	prefix          string
	namespace       string
	createNamespace bool
	valuesFiles     []string
	set             []string
	version         string
	repo            string
	includeHooks    bool
	skipCRDs        bool
}

func init() {
	helmInstallCmd.Flags().StringVar(&helmInstallArgs.component, "component", "", "component to install into (defaults to the release name); spaces <component>-helm and <component>-base are created if missing")
	helmInstallCmd.Flags().StringVar(&helmInstallArgs.prefix, "prefix", "", "prefix for generated unit slugs; required to be unique per release within a component, and empty for at most one release")
	helmInstallCmd.Flags().StringVar(&helmInstallArgs.namespace, "namespace", "", "release namespace, recorded and rendered literally; when omitted the placeholder namespace is used and deployments set it via 'cub variant create --namespace'")
	helmInstallCmd.Flags().BoolVar(&helmInstallArgs.createNamespace, "create-namespace", false, "synthesize a Namespace unit for the release namespace (skipped when the chart renders one itself)")
	helmInstallCmd.Flags().StringArrayVarP(&helmInstallArgs.valuesFiles, "values", "f", []string{}, "specify values in a YAML file (can specify multiple)")
	helmInstallCmd.Flags().StringArrayVar(&helmInstallArgs.set, "set", []string{}, "set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	helmInstallCmd.Flags().StringVar(&helmInstallArgs.version, "version", "", "chart version constraint: a specific version (e.g. 1.1.1) or a range (e.g. ^2.0.0)")
	helmInstallCmd.Flags().StringVar(&helmInstallArgs.repo, "repo", "", "chart repository URL to resolve a bare chart name against")
	helmInstallCmd.Flags().BoolVar(&helmInstallArgs.includeHooks, "include-hooks", false, "keep helm.sh/hook manifests as plain resources instead of dropping them")
	helmInstallCmd.Flags().BoolVar(&helmInstallArgs.skipCRDs, "skip-crds", false, "do not generate units from the chart's crds/ directories (mirrors 'helm install --skip-crds')")

	enableWaitFlag(helmInstallCmd)
	enableQuietFlag(helmInstallCmd)

	helmCmd.AddCommand(helmInstallCmd)
}

func helmInstallCmdRun(cmd *cobra.Command, args []string) error {
	releaseName := args[0]
	chartRef := args[1]

	component := helmInstallArgs.component
	if component == "" {
		component = makeSlug(releaseName)
	} else {
		component = makeSlug(component)
	}

	values, err := helmutils.MergeValues(helmInstallArgs.valuesFiles, helmInstallArgs.set)
	if err != nil {
		return err
	}

	spaces, err := ensureComponentSpaces(component)
	if err != nil {
		return err
	}

	others, err := listHelmSources(spaces.source.SpaceID)
	if err != nil {
		return err
	}

	// The prefix defaults to empty for the component's first release and to
	// the release name for subsequent releases.
	prefix := helmInstallArgs.prefix
	if !cmd.Flags().Changed("prefix") && countOtherReleases(others, releaseName) > 0 {
		prefix = makeSlug(releaseName)
	}
	if err := checkPrefixConflict(others, releaseName, prefix); err != nil {
		return err
	}

	src := &helmutils.HelmSource{
		APIVersion: helmutils.HelmSourceAPIVersion,
		Kind:       helmutils.HelmSourceKind,
		Metadata:   helmutils.HelmSourceMetadata{Name: releaseName},
		Spec: helmutils.HelmSourceSpec{
			Chart: helmutils.HelmSourceChart{
				Ref:     chartRef,
				Repo:    helmInstallArgs.repo,
				Version: helmInstallArgs.version,
			},
			Release: helmutils.HelmSourceRelease{
				Name:      releaseName,
				Namespace: helmInstallArgs.namespace,
			},
			CreateNamespace: helmInstallArgs.createNamespace,
			UnitPrefix:      prefix,
			IncludeHooks:    helmInstallArgs.includeHooks,
			SkipCRDs:        helmInstallArgs.skipCRDs,
			Values:          values,
		},
	}

	return applyHelmSource(src, component, spaces)
}

// countOtherReleases counts HelmSources other than the given release.
func countOtherReleases(sources []helmSourceUnit, releaseName string) int {
	count := 0
	for _, s := range sources {
		if s.unit.Slug != makeSlug(releaseName) {
			count++
		}
	}
	return count
}
