// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
	prune        bool
	dryRun       bool
	// sourceDesc is not a flag: it is the inputs as given on the command line,
	// stamped on each created Unit as its external source.
	sourceDesc string
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
credentials are reused from your Docker config.

Every input is recorded on the Space as a "confighub.com/external-source" annotation
(a JSON array), together with the --granularity and --namespace the upload ran with,
so the plan can be reproduced from the Space alone. For an oci:// input the resolved
digest is recorded too, making the exact bundle installed auditable.

An upload into a Space recorded with a different --granularity or --namespace is
refused. Granularity decides the Unit slugs, so changing it would replace that
Space's Units rather than update them; namespace decides the synthesized Namespace
resource and what AppConfig placeholders carry, so changing it would rewrite those
resources. Both have to be repeated on a re-upload — --granularity has a default,
so omitting it is itself a change. Re-run with the recorded values, or use --space
to upload elsewhere.

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

Each created Unit records the input it came from — the oci:// ref, file, or directory
as named on the command line — as its external source, so its change description reads
"from oci://ghcr.io/org/bundle". Use --change-desc to prefix your own description.

Re-uploading:
Running this command again against a Space it already populated is a re-upload, not a
second create. Each Unit is merged rather than replaced: the new content is 3-way merged
against the last upload, so changes made in ConfigHub after the first upload — a set-
function, a hand edit, a needs/provides binding — survive, while everything the source
actually changed lands. Resources new to the input become new Units. A re-upload whose
input has not changed does nothing.

The updates are recorded in a ChangeSet, so an entire re-upload can be rolled back with
the "cub unit update --restore Before:ChangeSet:<slug>" command printed at the end.

Units in the Space that the input no longer produces are left alone and reported. Pass
--prune to empty them instead: that merges empty content, withdrawing only the resources
this source contributed and leaving post-upload additions in place, so their deployed
resources are removed by the next apply. Units guarded by a DestroyGate refuse. Nothing is
ever deleted — the Unit record, its target binding, and its metadata survive.

Use --dry-run to see what would be created, updated, or emptied, including the per-field
merge as the server would resolve it, without changing anything.

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

  # Re-upload: same command, newer bundle. Changed Units are merged, preserving
  # edits made in ConfigHub since the first upload.
  cub variant upload --component cubbychat --variant base --granularity per-file \
    oci://ghcr.io/confighub/configs/cubbychat:v2

  # Preview that re-upload first, including the per-field merge.
  cub variant upload --dry-run --component cubbychat --variant base \
    --granularity per-file oci://ghcr.io/confighub/configs/cubbychat:v2

  # Re-upload and withdraw the resources the new render no longer contains.
  cub variant upload --prune --component web --variant base ./rendered/
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
	variantUploadCmd.Flags().BoolVar(&variantUploadArgs.prune, "prune", false, "on a re-upload, empty Units in the Space that this input no longer produces")
	variantUploadCmd.Flags().BoolVar(&variantUploadArgs.dryRun, "dry-run", false, "report what the upload would create, update, or empty, and exit without changing anything")
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

	// Describe the inputs as the user named them, before oci:// refs are resolved
	// to temp directories and Unit bodies are staged in temp files. Each Unit is
	// created with this as its external source, so its change description reads
	// "from oci://ghcr.io/org/bundle" rather than the temp path cub handed itself.
	a.sourceDesc = uploadSourceDescription(args)

	// Resolve any oci:// inputs to local directories of extracted manifests, so
	// the rest of the pipeline treats them like any other input path, and record
	// every input — oci:// or not — for the Space's confighub.com/external-source
	// annotation written below.
	parseInputs := make([]string, len(args))
	sources := make([]externalSourceRecord, 0, len(args))
	for i, in := range args {
		record := externalSourceRecord{
			Ref:         uploadSourceRef(in),
			Granularity: string(gran),
			Namespace:   a.namespace,
		}
		if !isOCIRef(in) {
			parseInputs[i] = in
			sources = append(sources, record)
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
		record.Digest = digest
		sources = append(sources, record)
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

	// Refuse an option change before anything is created, including under
	// --dry-run, where the plan it would print is meaningless.
	if err := checkUploadOptions(spaceSlug, gran, a.namespace); err != nil {
		return err
	}

	if a.dryRun {
		return reportUploadDryRun(spaceSlug, plan, a)
	}

	// Whether this is a re-upload has to be settled before the Space is created
	// below, since that create is what would otherwise make an absent Space look
	// like an existing one.
	reUpload := uploadSpaceHasUnits(spaceSlug)

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
	spaceMeta = append(spaceMeta, spaceSlug)
	if err := runCub(spaceMeta...); err != nil {
		return err
	}

	// Record the source(s) on the Space so a later re-upload can reproduce the
	// plan. Component/variant/etc. are already Space labels and the target is the
	// Space's TargetID annotation, so the record carries only the source ref, the
	// resolved digest (oci:// only), and the options that govern how bytes map to
	// Units (--granularity, --namespace). Those two are why this is written for
	// every input and not just oci://: re-uploading a Space at a different
	// granularity produces an entirely different Unit set, so the value the first
	// upload used has to be recoverable from the Space itself.
	if err := recordExternalSource(spaceSlug, sources); err != nil {
		return err
	}

	// A Space that already holds Units is a re-upload: its Units may carry
	// post-upload edits, so they are merged rather than recreated.
	if reUpload {
		if err := variantUploadReconcile(spaceSlug, plan, a); err != nil {
			return err
		}
		reportUploadPlan(plan)
		return nil
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

	if err := createInferredLinks(spaceSlug, plan, a.allowExists, nil); err != nil {
		return err
	}

	reportUploadPlan(plan)
	return nil
}

// maxUploadSourceDescription bounds the source string stamped on every Unit so a
// long input list can't push the resulting change description past the server's
// LastChangeDescription limit.
const maxUploadSourceDescription = 512

// uploadSourceRef renders one input the way the user named it — an oci:// ref or
// a path rather than the temp directory it is extracted or staged into, "stdin"
// for "-" (matching what "unit create" records for stdin input).
func uploadSourceRef(input string) string {
	if input == "-" {
		return "stdin"
	}
	return input
}

// uploadSourceDescription renders the upload's inputs the way the user named them,
// joined into the single string stamped on each Unit as its external source.
func uploadSourceDescription(inputs []string) string {
	parts := make([]string, 0, len(inputs))
	for _, in := range inputs {
		parts = append(parts, uploadSourceRef(in))
	}
	desc := strings.Join(parts, ", ")
	if len(desc) > maxUploadSourceDescription {
		// ToValidUTF8 drops a rune the cut landed in the middle of.
		desc = strings.ToValidUTF8(desc[:maxUploadSourceDescription-3], "") + "..."
	}
	return desc
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
	// Otherwise "unit create" defaults the external source to the temp file below.
	if a.sourceDesc != "" {
		cubArgs = append(cubArgs, "--merge-external-source", a.sourceDesc)
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
//
// The steps are separate functions because a re-upload needs them individually:
// the reconcile engine owns the data Unit (step 2), so it can 3-way merge it,
// while the surrounding scaffolding is re-asserted with --allow-exists. See
// uploadScaffolding in variant_upload_reconcile.go.
func uploadAppConfigUnit(spaceSlug string, u upload.Unit, a *variantUploadOptions) error {
	ac := u.AppConfig
	if err := createAppConfigInvocation(spaceSlug, ac, a.allowExists); err != nil {
		return err
	}
	if err := createAppConfigDataUnit(spaceSlug, u, a); err != nil {
		return err
	}
	if err := createAppConfigPlaceholder(spaceSlug, ac, a, a.allowExists); err != nil {
		return err
	}
	if err := linkAppConfigPlaceholder(spaceSlug, ac, a.allowExists); err != nil {
		return err
	}
	return setPlaceholderNamespace(spaceSlug, ac, a)
}

// createAppConfigInvocation creates the render-configmap Invocation that turns
// the AppConfig data Unit into a rendered ConfigMap.
func createAppConfigInvocation(spaceSlug string, ac *upload.AppConfigManifest, allowExists bool) error {
	invArgs := []string{"invocation", "create"}
	if allowExists {
		invArgs = append(invArgs, "--allow-exists")
	}
	invArgs = append(invArgs, "--space", spaceSlug, ac.InvocationSlug(), ac.Toolchain, "--", "render-configmap")
	invArgs = append(invArgs, ac.RenderConfigMapArgs()...)
	return runCub(invArgs...)
}

// createAppConfigDataUnit creates the AppConfig data Unit (no target — it is a
// pure data source, rendered into the placeholder by the Upsert link).
func createAppConfigDataUnit(spaceSlug string, u upload.Unit, a *variantUploadOptions) error {
	ac := u.AppConfig
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
	if a.sourceDesc != "" {
		dataArgs = append(dataArgs, "--merge-external-source", a.sourceDesc)
	}
	for _, l := range a.labels {
		dataArgs = append(dataArgs, "--label", l)
	}
	for _, an := range a.annotations {
		dataArgs = append(dataArgs, "--annotation", an)
	}
	dataArgs = append(dataArgs, ac.UnitSlug(), tmp.Name())
	return runCub(dataArgs...)
}

// createAppConfigPlaceholder creates the empty Kubernetes/YAML Unit the Upsert
// link renders the ConfigMap into. It is the Unit that carries the target.
func createAppConfigPlaceholder(spaceSlug string, ac *upload.AppConfigManifest, a *variantUploadOptions, allowExists bool) error {
	phArgs := []string{"unit", "create"}
	if allowExists {
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
	return runCub(phArgs...)
}

// linkAppConfigPlaceholder creates the Upsert link placeholder -> AppConfig
// data Unit, transformed through the render-configmap Invocation.
func linkAppConfigPlaceholder(spaceSlug string, ac *upload.AppConfigManifest, allowExists bool) error {
	linkArgs := []string{"link", "create"}
	if allowExists {
		linkArgs = append(linkArgs, "--allow-exists")
	}
	linkArgs = append(linkArgs, "--wait", "--quiet", "--space", spaceSlug,
		"--update-type", "Upsert", "--auto-update",
		"--transform-invocation", spaceSlug+"/"+ac.InvocationSlug(),
		"-", ac.PlaceholderSlug(), ac.UnitSlug())
	return runCub(linkArgs...)
}

// setPlaceholderNamespace stamps the real namespace onto the placeholder so it
// applies correctly.
func setPlaceholderNamespace(spaceSlug string, ac *upload.AppConfigManifest, a *variantUploadOptions) error {
	if a.namespace == "" {
		return nil
	}
	return runCub("function", "do", "--quiet", "--space", spaceSlug,
		"--toolchain", "Kubernetes/YAML", "--unit", ac.PlaceholderSlug(),
		"set-namespace", a.namespace)
}

// createInferredLinks creates the Unit→Unit links inferred from the bundle's
// references, label selectors, and CRD relationships.
//
// skip holds link pairs (see linkPairKey) that already exist and must not be
// re-created. It is nil on a first upload, where nothing exists yet, and
// populated on a re-upload: the inferred links are created with an
// auto-generated slug, so --allow-exists — which only tolerates a slug
// collision — does not cover re-asserting a link whose from→to pair is already
// there, and the server rejects it as a duplicate value.
func createInferredLinks(spaceSlug string, plan *upload.Plan, allowExists bool, skip map[string]bool) error {
	for _, l := range plan.Links {
		if skip[linkPairKey(l.FromUnit, l.ToUnit)] {
			continue
		}
		linkArgs := []string{"link", "create"}
		if allowExists {
			linkArgs = append(linkArgs, "--allow-exists")
		}
		linkArgs = append(linkArgs, "--quiet", "--space", spaceSlug, "-", l.FromUnit, l.ToUnit)
		if err := runCub(linkArgs...); err != nil {
			return err
		}
		tprint("Linked %s -> %s (%s)", l.FromUnit, l.ToUnit, l.Reason)
	}
	return nil
}

// linkPairKey identifies a link by the Unit slugs it connects, which is the
// identity the server enforces as unique.
func linkPairKey(fromUnit, toUnit string) string {
	return fromUnit + " -> " + toUnit
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

// externalSourceAnnotation is the well-known Space annotation recording the
// source(s) the Space was uploaded from, as a JSON array of externalSourceRecord.
// It lets a later "variant upload" reproduce the plan from the Space alone.
const externalSourceAnnotation = "confighub.com/external-source"

// externalSourceRecord captures one input and the options that govern how its
// bytes map to Units. Component/variant/etc. live in the Space labels and the
// target in the Space's TargetID annotation, so they are not repeated here.
//
// Written for every input, not only oci:// ones. A digest exists only for an
// oci:// ref, but Granularity and Namespace decide the shape of the Unit set for
// any input at all — re-uploading at a different granularity yields an entirely
// different set of Units — so what the first upload used has to be recoverable
// whether it came from a registry, a directory, or stdin.
type externalSourceRecord struct {
	Ref         string `json:"ref"`
	Digest      string `json:"digest,omitempty"`
	Granularity string `json:"granularity"`
	Namespace   string `json:"namespace,omitempty"`
}

// checkUploadOptions refuses an upload whose --granularity or --namespace
// disagrees with what the Space was uploaded with. Both decide the shape of the
// result rather than merely how it is presented, and neither disagreement is
// something a re-upload can reconcile — it would rewrite the Space instead.
//
// A Space with no record — new, or seeded before the annotation was written for
// every input — is left alone rather than guessed at.
func checkUploadOptions(spaceSlug string, gran upload.Granularity, namespace string) error {
	recorded, found := uploadRecordedSource(spaceSlug)
	if !found {
		return nil
	}
	// Granularity decides the Unit slugs. minimal collapses a bundle into one Unit
	// named for the component, per-file names a Unit after each source file's stem,
	// per-resource one per resource. A re-upload matches existing Units by slug, so
	// arriving with a different granularity does not update the Space: every
	// recorded Unit reads as absent from the input and every new one as an
	// addition, replacing the Space's contents and offering to prune what was there.
	//
	// Guarded only when recorded, so a hand-written annotation that omits it still
	// uploads.
	if recorded.Granularity != "" && recorded.Granularity != string(gran) {
		return fmt.Errorf(
			"Space %q was uploaded with --granularity %s, but this upload specifies %s.\n"+
				"Granularity determines the Unit slugs, so re-uploading at a different one would\n"+
				"replace the Space's Units rather than update them.\n"+
				"Re-run with --granularity %s, or use --space to upload into a different Space.",
			spaceSlug, recorded.Granularity, gran, recorded.Granularity)
	}
	// Namespace decides whether a Namespace resource is synthesized and which
	// namespace the AppConfig placeholders are stamped with. Unlike granularity it
	// changes content rather than Unit identity, so a re-upload would appear to
	// succeed while rewriting or dropping those resources. An empty value is
	// meaningful here — it means the upload ran without --namespace — so this
	// compares directly rather than treating empty as "unrecorded".
	if recorded.Namespace != namespace {
		rerun := "without --namespace"
		if recorded.Namespace != "" {
			rerun = "with --namespace " + recorded.Namespace
		}
		return fmt.Errorf(
			"Space %q was uploaded with %s, but this upload specifies %s.\n"+
				"--namespace decides whether a Namespace resource is synthesized and which namespace\n"+
				"AppConfig placeholders carry, so changing it rewrites those resources.\n"+
				"Re-run %s, or use --space to upload into a different Space.",
			spaceSlug, uploadNamespaceDesc(recorded.Namespace), uploadNamespaceDesc(namespace), rerun)
	}
	return nil
}

// uploadNamespaceDesc renders a --namespace value for an error message, naming
// its absence rather than printing an empty string.
func uploadNamespaceDesc(namespace string) string {
	if namespace == "" {
		return "no --namespace"
	}
	return "--namespace " + namespace
}

// uploadRecordedSource returns the external-source record on the Space and
// whether one was found. Not found covers a Space that does not exist, carries
// no annotation, or whose annotation cannot be read — all cases where there is
// nothing to check against and the upload should simply proceed.
func uploadRecordedSource(spaceSlug string) (externalSourceRecord, bool) {
	space, err := apiGetSpaceFromSlug(spaceSlug, "SpaceID,Slug,Annotations")
	if err != nil || space == nil {
		return externalSourceRecord{}, false
	}
	return recordedSource(space.Annotations[externalSourceAnnotation])
}

// recordedSource parses an external-source annotation value. Every record from
// one upload carries the same granularity and namespace, so the first stands for
// the Space.
func recordedSource(annotation string) (externalSourceRecord, bool) {
	if annotation == "" {
		return externalSourceRecord{}, false
	}
	var records []externalSourceRecord
	if err := json.Unmarshal([]byte(annotation), &records); err != nil || len(records) == 0 {
		return externalSourceRecord{}, false
	}
	return records[0], true
}

// recordExternalSource stamps the external-source annotation on the Space via a
// direct merge-patch. The "space update --annotation" flag comma-splits its value
// (CSV parsing), which would corrupt the JSON, so this bypasses it. Merge-patch
// merges into the existing annotation map, preserving TargetID and any others.
func recordExternalSource(spaceSlug string, records []externalSourceRecord) error {
	encoded, err := json.Marshal(records)
	if err != nil {
		return err
	}
	space, err := apiGetSpaceFromSlug(spaceSlug, "SpaceID,Slug")
	if err != nil {
		return err
	}
	patchData, err := json.Marshal(map[string]map[string]string{
		"Annotations": {externalSourceAnnotation: string(encoded)},
	})
	if err != nil {
		return err
	}
	if _, err := patchSpace(space.SpaceID, patchData); err != nil {
		return err
	}
	return nil
}

// cubBinaryPath returns the binary that runCub should invoke: this same running
// executable, wherever it lives and whatever it is called.
//
// It deliberately never consults $PATH for a binary named "cub". Resolving by
// name means a locally built or renamed binary delegates half its work to some
// other cub that happens to be installed — a `bin/cub-dev` doing its own space
// creation but handing unit creation to last month's release. The resulting
// version skew is invisible at the call site and surfaces as behaviour that
// looks like the server's.
//
// The bare "cub" it returns when os.Executable fails is the sole exception, and
// the only case where no self-reference is available at all: that call fails
// only where the OS cannot report the running binary, and resolving through
// $PATH there beats refusing to run. This function always returns something
// runnable, so callers never have to decide what an unresolved binary means.
func cubBinaryPath() string {
	bin, err := os.Executable()
	if err != nil || bin == "" {
		return "cub"
	}
	return bin
}

// runCub executes the cub binary (this same CLI) as a subprocess, streaming its
// output. Side effects go through cub so this command reuses its create/link
// semantics without re-implementing them.
func runCub(args ...string) error {
	c := exec.Command(cubBinaryPath(), args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("cub %s: %w", strings.Join(args, " "), err)
	}
	return nil
}
