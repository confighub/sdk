// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/confighub/sdk/cmd/cub/upload"
	"github.com/confighub/sdk/core/worker/api"
	"github.com/spf13/cobra"
)

type variantUploadOptions struct {
	component    string
	variant      string
	environment  string
	region       string
	layer        string
	owner        string
	spacePattern string
	space        string
	granularity  string
	namespace    string
	target       string
	labels       []string
	annotations  []string
	changeDesc   string
	allowExists  bool
}

var variantUploadArgs variantUploadOptions

var variantUploadCmd = &cobra.Command{
	Use:   "upload [flags] <file|dir|oci://ref|-> [<file|dir|oci://ref> ...]",
	Short: "Upload rendered Kubernetes resources into a Space as Units",
	Long: getCommandHelp(`Upload already-rendered Kubernetes manifests into a ConfigHub Space.

The input is a stream of rendered resources — from the installer, "kustomize build",
or "helm template" — supplied as files, directories (walked for .yaml/.yml), "-" for
stdin, or an "oci://" reference to a manifest bundle. This command does not render
anything; it ingests what you give it.

An oci:// input is pulled and its YAML extracted before ingestion. The bundle is a
standard OCI image artifact (a tar or tar+gzip layer of YAML, as "cub release publish"
and Flux produce, or individual file layers as "oras push" produces); registry
credentials are reused from your Docker config. The source reference is recorded on the
Space as an "ExternalSource" annotation, and the resolved digest as "ExternalSourceDigest".

Resources become Units in one of three granularities (--granularity):
  minimal       one Unit for everything, with CRDs split into their own Unit and each
                AppConfig file split into its own Unit set (the default).
  per-resource  one Unit per resource.
  per-file      one Unit per source file, named from the file's stem — so the input's
                file layout defines the Unit set (matches how "cub helm" groups a
                chart's template files). Useful with an oci:// bundle of named files.

In minimal mode the resources in the combined Unit are ordered by install priority
(Namespaces, RBAC, config, then workloads) and by their references to one another. A
Namespace resource is synthesized if --namespace is given and none is present. AppConfig
ConfigMaps (carrying installer.confighub.com annotations) are expanded into an AppConfig
data Unit, a render-configmap Invocation, a placeholder Unit, and an Upsert link. Rendered
Secrets are never uploaded — apply them out-of-band.

Links between Units are inferred from references, label selectors, and custom-resource →
CRD relationships. Because ConfigHub does not break dependency cycles, any cycle found in
the ordering or the links is broken here — the weakest edge is dropped (a selector before a
reference; a cross-scope reference before a same-namespace one) and the broken edge is
reported.

The Space is created if missing and stamped with the well-known labels from --component,
--variant, --environment, --region, --layer, and --owner. --component and --variant are
required. The Space slug comes from --space-pattern (a Go template over .Labels), or from
--space to set it explicitly.

Examples:
`+"```"+`
  # Minimal upload of a kustomize build into a derived Space slug "web-base".
  kustomize build overlays/base | cub variant upload --component web --variant base -

  # One Unit per resource, into an explicit Space, bound to a target.
  cub variant upload --component web --variant prod --space web-prod \
    --granularity per-resource --target web-prod/cluster ./rendered/

  # Helm output, ensuring a namespace and a regional label.
  helm template myapp ./chart | cub variant upload --component myapp --variant prod \
    --environment Prod --region us-east1 --namespace myapp -

  # Seed a base from a published OCI manifest bundle, one Unit per bundled file.
  cub variant upload --component cubbychat --variant base --granularity per-file \
    oci://ghcr.io/confighub/configs/cubbychat
`+"```"+`
`, ""),
	Args: cobra.MinimumNArgs(1),
	RunE: variantUploadCmdRun,
}

func init() {
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.component, "component", "", "value for the well-known \"Component\" Space label (required)")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.variant, "variant", "", "value for the well-known \"Variant\" Space label (required)")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.environment, "environment", "", "value for the well-known \"Environment\" Space label (e.g. Prod)")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.region, "region", "", "value for the well-known \"Region\" Space label (e.g. us-east1)")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.layer, "layer", "", "value for the well-known \"Layer\" Space label (e.g. App)")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.owner, "owner", "", "value for the well-known \"Owner\" Space label (e.g. Engineering)")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.spacePattern, "space-pattern", "template:{{.Labels.Component}}-{{.Labels.Variant}}", "Go template (prefix 'template:') for the Space slug, evaluated over .Labels")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.space, "space", "", "explicit Space slug; overrides --space-pattern")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.granularity, "granularity", string(upload.Minimal), "how resources map to Units: minimal, per-resource, or per-file")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.namespace, "namespace", "", "ensure a Namespace resource with this name exists (unless \"default\")")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.target, "target", "", "target for the created Units, in <target-slug> or <space-slug>/<target-slug> form")
	variantUploadCmd.Flags().StringSliceVar(&variantUploadArgs.labels, "label", nil, "label key=value to set on every created Unit (repeatable)")
	variantUploadCmd.Flags().StringSliceVar(&variantUploadArgs.annotations, "annotation", nil, "annotation key=value to set on every created Unit (repeatable)")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.changeDesc, "change-desc", "", "change description recorded on each created Unit")
	variantUploadCmd.Flags().BoolVar(&variantUploadArgs.allowExists, "allow-exists", false, "tolerate Spaces, Units, Invocations, and Links that already exist (retry a partial upload)")
	variantCmd.AddCommand(variantUploadCmd)
}

func variantUploadCmdRun(cmd *cobra.Command, args []string) error {
	a := &variantUploadArgs
	if a.component == "" {
		return fmt.Errorf("--component is required")
	}
	if a.variant == "" {
		return fmt.Errorf("--variant is required")
	}
	gran := upload.Granularity(a.granularity)
	if gran != upload.Minimal && gran != upload.PerResource && gran != upload.PerFile {
		return fmt.Errorf("--granularity must be %q, %q, or %q", upload.Minimal, upload.PerResource, upload.PerFile)
	}

	labels := map[string]string{
		"Component": a.component,
		"Variant":   a.variant,
	}
	if a.environment != "" {
		labels["Environment"] = a.environment
	}
	if a.region != "" {
		labels["Region"] = a.region
	}
	if a.layer != "" {
		labels["Layer"] = a.layer
	}
	if a.owner != "" {
		labels["Owner"] = a.owner
	}

	spaceSlug := a.space
	if spaceSlug == "" {
		s, err := renderSpacePattern(a.spacePattern, labels)
		if err != nil {
			return err
		}
		spaceSlug = s
	}
	spaceSlug = makeSlug(spaceSlug)
	if spaceSlug == "" {
		return fmt.Errorf("computed Space slug is empty; set --space")
	}

	// Resolve any oci:// inputs to local directories of extracted manifests, so
	// the rest of the pipeline treats them like any other input path. The source
	// references and resolved digests are recorded on the Space below as
	// ExternalSource and ExternalSourceDigest annotations.
	parseInputs := make([]string, len(args))
	var ociSources, ociDigests []string
	for i, in := range args {
		if !isOCIRef(in) {
			parseInputs[i] = in
			continue
		}
		dir, err := os.MkdirTemp("", "cub-oci-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		digest, err := pullOCIManifests(ctx, in, dir)
		if err != nil {
			return err
		}
		tprint("Pulled %s (%s)", in, digest)
		parseInputs[i] = dir
		ociSources = append(ociSources, in)
		ociDigests = append(ociDigests, digest)
	}

	resources, err := upload.Parse(parseInputs)
	if err != nil {
		return err
	}
	if len(resources) == 0 {
		return fmt.Errorf("no Kubernetes resources found in the input")
	}

	plan, err := upload.BuildPlan(resources, gran, a.component, a.namespace)
	if err != nil {
		return err
	}

	// Create and stamp the Space.
	if err := runCub("space", "create", "--allow-exists", "--quiet", spaceSlug); err != nil {
		return err
	}
	spaceMeta := []string{"space", "update", "--patch", "--quiet"}
	for k, v := range labels {
		spaceMeta = append(spaceMeta, "--label", k+"="+v)
	}
	if a.target != "" {
		if id, providerType, qualifiedRef, err := resolveUploadTarget(spaceSlug, a.target); err == nil && id != "" {
			spaceMeta = append(spaceMeta, "--annotation", "TargetID="+id)
			// For an OCI target, also set the Space's ReleaseTargetID: releases are
			// published per Space ("cub release publish <space>") and publish requires it.
			// Pass the qualified ref rather than the UUID: space update resolves a bare
			// Target ID against the selected space, which may not be set here.
			if providerType == string(api.ProviderOCI) {
				spaceMeta = append(spaceMeta, "--release-target", qualifiedRef)
			}
		}
	}
	if len(ociSources) > 0 {
		// ExternalSource carries the reference(s) as given (tag included);
		// ExternalSourceDigest carries the resolved immutable digest(s) in the
		// same order, so the exact bundle installed is auditable and a later
		// refresh can detect when a moving tag points somewhere new.
		spaceMeta = append(spaceMeta, "--annotation", "ExternalSource="+strings.Join(ociSources, ","))
		spaceMeta = append(spaceMeta, "--annotation", "ExternalSourceDigest="+strings.Join(ociDigests, ","))
	}
	spaceMeta = append(spaceMeta, spaceSlug)
	if err := runCub(spaceMeta...); err != nil {
		return err
	}

	// Create Units in plan order.
	for _, u := range plan.Units {
		switch u.Kind {
		case upload.UnitAppConfig:
			if err := uploadAppConfigUnit(spaceSlug, u, a); err != nil {
				return err
			}
		default:
			targeted := u.Kind == upload.UnitNormal || u.Kind == upload.UnitCRD
			if err := createPlainUnit(spaceSlug, u, a, targeted); err != nil {
				return err
			}
		}
	}

	// Create inferred links.
	for _, l := range plan.Links {
		linkArgs := []string{"link", "create"}
		if a.allowExists {
			linkArgs = append(linkArgs, "--allow-exists")
		}
		linkArgs = append(linkArgs, "--quiet", "--space", spaceSlug, "-", l.FromUnit, l.ToUnit)
		if err := runCub(linkArgs...); err != nil {
			return err
		}
		tprint("Linked %s -> %s (%s)", l.FromUnit, l.ToUnit, l.Reason)
	}

	reportUploadPlan(plan)
	return nil
}

// createPlainUnit writes the Unit body to a temp file and runs cub unit create.
func createPlainUnit(spaceSlug string, u upload.Unit, a *variantUploadOptions, targeted bool) error {
	tmp, err := os.CreateTemp("", "cub-upload-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(u.Content); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	cubArgs := []string{"unit", "create"}
	if a.allowExists {
		cubArgs = append(cubArgs, "--allow-exists")
	}
	cubArgs = append(cubArgs, "--space", spaceSlug, "--toolchain", u.Toolchain)
	if targeted && a.target != "" {
		cubArgs = append(cubArgs, "--target", a.target)
	}
	if a.changeDesc != "" {
		cubArgs = append(cubArgs, "--change-desc", a.changeDesc)
	}
	for _, l := range a.labels {
		cubArgs = append(cubArgs, "--label", l)
	}
	for _, an := range a.annotations {
		cubArgs = append(cubArgs, "--annotation", an)
	}
	cubArgs = append(cubArgs, u.Slug, tmp.Name())
	return runCub(cubArgs...)
}

// uploadAppConfigUnit materializes the AppConfig data Unit, the render-configmap
// Invocation, the placeholder Unit, and the Upsert link that renders the
// ConfigMap into the placeholder. Mirrors the installer's AppConfig expansion.
func uploadAppConfigUnit(spaceSlug string, u upload.Unit, a *variantUploadOptions) error {
	ac := u.AppConfig

	// 1. render-configmap Invocation.
	invArgs := []string{"invocation", "create"}
	if a.allowExists {
		invArgs = append(invArgs, "--allow-exists")
	}
	invArgs = append(invArgs, "--space", spaceSlug, ac.InvocationSlug(), ac.Toolchain, "--", "render-configmap")
	invArgs = append(invArgs, ac.RenderConfigMapArgs()...)
	if err := runCub(invArgs...); err != nil {
		return err
	}

	// 2. AppConfig data Unit (no target — pure data source).
	tmp, err := os.CreateTemp("", "cub-appconfig-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(u.Content); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	dataArgs := []string{"unit", "create"}
	if a.allowExists {
		dataArgs = append(dataArgs, "--allow-exists")
	}
	dataArgs = append(dataArgs, "--space", spaceSlug, "--toolchain", ac.Toolchain)
	for _, l := range a.labels {
		dataArgs = append(dataArgs, "--label", l)
	}
	for _, an := range a.annotations {
		dataArgs = append(dataArgs, "--annotation", an)
	}
	dataArgs = append(dataArgs, ac.UnitSlug(), tmp.Name())
	if err := runCub(dataArgs...); err != nil {
		return err
	}

	// 3. Placeholder Kubernetes/YAML Unit (empty; populated by the Upsert link).
	phArgs := []string{"unit", "create"}
	if a.allowExists {
		phArgs = append(phArgs, "--allow-exists")
	}
	phArgs = append(phArgs, "--space", spaceSlug, "--toolchain", "Kubernetes/YAML")
	if a.target != "" {
		phArgs = append(phArgs, "--target", a.target)
	}
	for _, l := range a.labels {
		phArgs = append(phArgs, "--label", l)
	}
	for _, an := range a.annotations {
		phArgs = append(phArgs, "--annotation", an)
	}
	phArgs = append(phArgs, ac.PlaceholderSlug())
	if err := runCub(phArgs...); err != nil {
		return err
	}

	// 4. Upsert link placeholder -> AppConfig data Unit, via the Invocation.
	linkArgs := []string{"link", "create"}
	if a.allowExists {
		linkArgs = append(linkArgs, "--allow-exists")
	}
	linkArgs = append(linkArgs, "--wait", "--quiet", "--space", spaceSlug,
		"--update-type", "Upsert", "--auto-update",
		"--transform-invocation", spaceSlug+"/"+ac.InvocationSlug(),
		"-", ac.PlaceholderSlug(), ac.UnitSlug())
	if err := runCub(linkArgs...); err != nil {
		return err
	}

	// 5. Stamp the real namespace onto the placeholder so it applies correctly.
	if a.namespace != "" {
		if err := runCub("function", "do", "--quiet", "--space", spaceSlug,
			"--toolchain", "Kubernetes/YAML", "--unit", ac.PlaceholderSlug(),
			"set-namespace", a.namespace); err != nil {
			return err
		}
	}
	return nil
}

// reportUploadPlan prints the broken-edge reports, skipped Secrets, and
// unmatched references after the upload completes.
func reportUploadPlan(plan *upload.Plan) {
	for _, b := range plan.BrokenOrdering {
		tprint("broke %s ordering edge %s -> %s to resolve cycle: %s",
			b.Kind, b.From, b.To, strings.Join(b.Cycle, " -> "))
	}
	for _, b := range plan.BrokenLinks {
		tprint("broke %s link %s -> %s to resolve cycle: %s",
			b.Kind, b.From, b.To, strings.Join(b.Cycle, " -> "))
	}
	if len(plan.SkippedSecrets) > 0 {
		tprint("")
		tprint("Note: %d Secret(s) were NOT uploaded. Apply them out-of-band:", len(plan.SkippedSecrets))
		for _, s := range plan.SkippedSecrets {
			tprint("  - %s %q", s.Type, s.ScopedName)
		}
	}
	if len(plan.Unmatched) > 0 {
		tprint("")
		tprint("Note: the following references didn't resolve to any uploaded Unit (expected when the")
		tprint("target lives in the cluster, e.g. a Secret created out-of-band):")
		for _, u := range plan.Unmatched {
			tprint("  - %s -> %s %q", u.FromUnit, u.TargetType, u.TargetName)
		}
	}
}

// renderSpacePattern evaluates a --space-pattern (optionally prefixed "template:")
// over the well-known Space labels.
func renderSpacePattern(pattern string, labels map[string]string) (string, error) {
	pattern = strings.TrimPrefix(pattern, "template:")
	t, err := template.New("space").Parse(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid --space-pattern: %w", err)
	}
	var b strings.Builder
	if err := t.Execute(&b, struct{ Labels map[string]string }{Labels: labels}); err != nil {
		return "", fmt.Errorf("evaluate --space-pattern: %w", err)
	}
	return strings.TrimSpace(b.String()), nil
}

// resolveUploadTarget resolves a --target ref by shelling out to cub, returning
// the TargetID UUID (recorded as the Space's TargetID annotation), the target's
// ProviderType (an OCI target is also set as the Space's release target), and
// the fully qualified <space>/<slug> ref for passing to other cub commands.
func resolveUploadTarget(unitSpace, targetRef string) (id, providerType, qualifiedRef string, err error) {
	lookupSpace, slug := unitSpace, targetRef
	if i := strings.IndexByte(targetRef, '/'); i >= 0 {
		lookupSpace, slug = targetRef[:i], targetRef[i+1:]
	}
	var stdout, stderr bytes.Buffer
	c := exec.Command("cub", "target", "get", "--space", lookupSpace, "-o", "json", slug)
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return "", "", "", err
	}
	var extended struct {
		Target struct {
			TargetID     string `json:"TargetID"`
			ProviderType string `json:"ProviderType"`
		} `json:"Target"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &extended); err != nil {
		return "", "", "", err
	}
	if extended.Target.TargetID == "" {
		return "", "", "", fmt.Errorf("target %q has no TargetID", slug)
	}
	return extended.Target.TargetID, extended.Target.ProviderType, lookupSpace + "/" + slug, nil
}

// runCub executes the cub binary (this same CLI) as a subprocess, streaming its
// output. Side effects go through cub so this command reuses its create/link
// semantics without re-implementing them.
func runCub(args ...string) error {
	bin, err := os.Executable()
	if err != nil || bin == "" {
		bin = "cub"
	}
	if p := filepath.Base(bin); p != "cub" {
		if _, lookErr := exec.LookPath("cub"); lookErr == nil {
			bin = "cub"
		}
	}
	c := exec.Command(bin, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("cub %s: %w", strings.Join(args, " "), err)
	}
	return nil
}
