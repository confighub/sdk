// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type variantUploadOptions struct {
	component       string
	variant         string
	stage           string
	environment     string
	region          string
	layer           string
	owner           string
	spacePattern    string
	space           string
	sourceName      string
	namespace       string
	createNamespace bool
	target          string
	labels          []string
	annotations     []string
	spaceLabels     []string
	changeDesc      string
	dryRun          bool
	yes             bool

	// The old names of --unit-label and --unit-annotation, kept as deprecated aliases.
	deprecatedLabels      []string
	deprecatedAnnotations []string
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
and Flux produce, or individual file layers as "oras push" produces). The pull is
anonymous: local registry credentials are not used, so the bundle has to be public.

The server does the work: it splits the bundle into resources and makes the Space's
Units, Links, and Invocations match it. Every resource becomes its own Unit, named
after the resource rather than the file it came from — a workload keeps its bare name
("backend"), and everything else takes its kind as a suffix ("backend-service").

Uploading is create-or-update, always. The first upload and every later one go through
the same path, so there is no separate "re-upload" mode and nothing to remember between
runs:

  create      the resource is new, so a Unit is created for it.
  update      the source changed since it last wrote that Unit, so the new content is
              3-way merged into it. Changes made in ConfigHub since — a set- function,
              a hand edit, a needs/provides binding — survive, and anything the merge
              had to withhold is reported as a conflict.
  unchanged   the source has not changed, so nothing is written.
  empty       the resource is gone from the bundle, so the Unit's data is emptied.
  revive      an emptied resource is back, so the whole resource is applied to the
              same Unit.
  adopt       a Unit that already held exactly this resource is taken over, keeping
              its UnitID, its slug, and its history.

Nothing is ever deleted. A resource the bundle no longer contains empties its Unit
instead, so the Unit keeps its identity, its links, and its history, and the next
Release withdraws the object from the cluster. Emptying is what "--yes" confirms:
without it, an upload whose plan empties Units asks first.

Ownership is a label. Every Unit, Link, and Invocation an upload writes is labeled
with its source name (--source-name, defaulting to --component), and a Unit belonging
to another source, or to no source, is never written or emptied — so one Space can
hold several sources and hand-written Units side by side.

Rendered Secrets are never uploaded — apply them out-of-band. AppConfig ConfigMaps
(carrying installer.confighub.com annotations) are expanded into an AppConfig data
Unit, a render-configmap Invocation, a placeholder Unit, and an Upsert link.

Links between Units are inferred from references, label selectors, and custom-resource
→ CRD relationships. Because ConfigHub does not break dependency cycles, any cycle in
the inferred links is broken — the weakest edge is dropped (a selector before a
reference; a cross-scope reference before a same-namespace one) — and reported.

The Space is created if missing and stamped with the well-known labels from --component,
--variant, --stage, --environment, --region, --layer, and --owner, and any other labels
given with --space-label. --component and --variant are required. The Space slug comes
from --space-pattern (a Go template over .Labels), or from --space to set it explicitly.
--unit-label and --unit-annotation set labels and annotations on every written Unit.

--namespace is the release namespace, as in "helm template -n": where namespaced
resources that name no namespace, and cluster-scoped resources, belong. It has no
default. Charts write the namespace into places set-namespace cannot reach — ConfigMap
data, flags, annotations, webhook references — so render with the real namespace rather
than substituting one afterwards. --create-namespace synthesizes the Namespace resource
when the bundle lacks it; it is off by default, because a bare Namespace has none of the
pod-security labels, NetworkPolicy, ResourceQuota, or LimitRange that make a namespace
usable, and the platform normally provisions those together.

The writes are recorded in a ChangeSet, so an entire upload can be rolled back with the
"cub unit update --restore Before:ChangeSet:<slug>" command printed at the end.

Use --dry-run to see what would be created, updated, emptied, revived, or adopted
without changing anything.

Examples:
`+"```"+`
  # Upload a kustomize build into a derived Space slug "web-base".
  kustomize build overlays/base | cub variant upload --component web --variant base -

  # Into an explicit Space, bound to a target.
  cub variant upload --component web --variant prod --space web-prod \
    --target web-prod/cluster ./rendered/

  # Helm output, rendered with the real namespace and uploaded into it.
  helm template myapp ./chart -n myapp | cub variant upload \
    --component myapp --variant prod --environment Prod --namespace myapp -

  # Seed a base from a published OCI manifest bundle.
  cub variant upload --component cubbychat --variant base \
    oci://ghcr.io/confighub/configs/cubbychat

  # Upload a newer bundle. Changed Units are merged, preserving edits made in
  # ConfigHub since; resources the bundle dropped have their Units emptied.
  cub variant upload --component cubbychat --variant base --yes \
    oci://ghcr.io/confighub/configs/cubbychat:v2

  # Preview that upload first.
  cub variant upload --dry-run --component cubbychat --variant base \
    oci://ghcr.io/confighub/configs/cubbychat:v2
`+"```"+`
`, ""),
	Args: cobra.MinimumNArgs(1),
	RunE: variantUploadCmdRun,
}

func init() {
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.component, "component", "", "value for the well-known \"Component\" Space label (required)")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.variant, "variant", "", "value for the well-known \"Variant\" Space label (required)")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.stage, "stage", "", "value for the well-known \"Stage\" Space label (e.g. Canary)")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.environment, "environment", "", "value for the well-known \"Environment\" Space label (e.g. Prod)")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.region, "region", "", "value for the well-known \"Region\" Space label (e.g. us-east1)")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.layer, "layer", "", "value for the well-known \"Layer\" Space label (e.g. App)")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.owner, "owner", "", "value for the well-known \"Owner\" Space label (e.g. Engineering)")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.spacePattern, "space-pattern", "template:{{.Labels.Component}}-{{.Labels.Variant}}", "Go template (prefix 'template:') for the Space slug, evaluated over .Labels")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.space, "space", "", "explicit Space slug; overrides --space-pattern")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.sourceName, "source-name", "", "ownership name for the Units, Links, and Invocations this upload writes; defaults to --component")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.namespace, "namespace", "", "the release namespace: where namespaced resources that name no namespace, and cluster-scoped resources, belong")
	variantUploadCmd.Flags().BoolVar(&variantUploadArgs.createNamespace, "create-namespace", false, "synthesize the release Namespace resource if the bundle does not contain it")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.target, "target", "", "target for the created Units, in <target-slug> or <space-slug>/<target-slug> form")
	variantUploadCmd.Flags().StringSliceVar(&variantUploadArgs.spaceLabels, "space-label", nil, "label key=value to set on the Space (repeatable); it may not name a label another flag sets, such as Component")
	variantUploadCmd.Flags().StringSliceVar(&variantUploadArgs.labels, "unit-label", nil, "label key=value to set on every written Unit (repeatable)")
	variantUploadCmd.Flags().StringSliceVar(&variantUploadArgs.annotations, "unit-annotation", nil, "annotation key=value to set on every written Unit (repeatable)")
	variantUploadCmd.Flags().StringSliceVar(&variantUploadArgs.deprecatedLabels, "label", nil, "label key=value to set on every written Unit (repeatable)")
	variantUploadCmd.Flags().StringSliceVar(&variantUploadArgs.deprecatedAnnotations, "annotation", nil, "annotation key=value to set on every written Unit (repeatable)")
	_ = variantUploadCmd.Flags().MarkDeprecated("label", "use --unit-label")
	_ = variantUploadCmd.Flags().MarkDeprecated("annotation", "use --unit-annotation")
	variantUploadCmd.Flags().StringVar(&variantUploadArgs.changeDesc, "change-desc", "", "change description recorded on each written Unit")
	variantUploadCmd.Flags().BoolVar(&variantUploadArgs.dryRun, "dry-run", false, "report what the upload would create, update, empty, revive, or adopt, and exit without changing anything")
	variantUploadCmd.Flags().BoolVar(&variantUploadArgs.yes, "yes", false, "do not ask for confirmation when the upload would empty Units")
	variantCmd.AddCommand(variantUploadCmd)
}

// variantUploadReservedLabels are the Space labels upload sets through a flag of its own, each
// mapped to that flag, which --space-label may not name.
var variantUploadReservedLabels = map[string]string{
	"Component":   "--component",
	"Variant":     "--variant",
	"Stage":       "--stage",
	"Environment": "--environment",
	"Region":      "--region",
	"Layer":       "--layer",
	"Owner":       "--owner",
}

func variantUploadCmdRun(cmd *cobra.Command, args []string) error {
	a := &variantUploadArgs
	if a.component == "" {
		return fmt.Errorf("--component is required")
	}
	if a.variant == "" {
		return fmt.Errorf("--variant is required")
	}

	a.labels = append(a.labels, a.deprecatedLabels...)
	a.annotations = append(a.annotations, a.deprecatedAnnotations...)
	if err := checkSpaceLabelFlags(a.spaceLabels, variantUploadReservedLabels); err != nil {
		return err
	}

	labels := map[string]string{
		"Component": a.component,
		"Variant":   a.variant,
	}
	for _, kv := range a.spaceLabels {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			return fmt.Errorf("--space-label must be key=value: %s", kv)
		}
		labels[key] = value
	}
	for flag, value := range map[string]string{
		"Stage": a.stage, "Environment": a.environment, "Region": a.region,
		"Layer": a.layer, "Owner": a.owner,
	} {
		if value != "" {
			labels[flag] = value
		}
	}

	component := goclientnew.UploadComponentRequest{
		Name:            a.component,
		SourceName:      a.sourceName,
		Namespace:       a.namespace,
		CreateNamespace: a.createNamespace,
		Space:           a.space,
	}
	var err error
	if component.UnitLabels, err = keyValueMap(a.labels, "--unit-label"); err != nil {
		return err
	}
	if component.UnitAnnotations, err = keyValueMap(a.annotations, "--unit-annotation"); err != nil {
		return err
	}

	// The target is resolved here because a bare slug is scoped to the Space this
	// upload writes to, which the client is the one that knows how to name.
	if a.target != "" {
		spaceSlug := a.space
		if spaceSlug == "" {
			if spaceSlug, err = renderSpacePattern(a.spacePattern, labels); err != nil {
				return err
			}
			spaceSlug = makeSlug(spaceSlug)
		}
		id, _, _, resolveErr := resolveUploadTarget(spaceSlug, a.target)
		if resolveErr != nil {
			return resolveErr
		}
		targetID, parseErr := uuid.Parse(id)
		if parseErr != nil {
			return fmt.Errorf("target %q resolved to an unparseable ID %q: %w", a.target, id, parseErr)
		}
		component.TargetID = &targetID
	}

	files, digest, usedStdin, err := collectUploadFiles(args)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no configuration files found in the input")
	}

	req := goclientnew.UploadRequest{
		Files:             files,
		Components:        []goclientnew.UploadComponentRequest{component},
		SpaceLabels:       labels,
		SpacePattern:      strings.TrimPrefix(a.spacePattern, "template:"),
		ChangeDescription: a.changeDesc,
		Source: &goclientnew.UploadSourceInfo{
			Ref:           uploadSourceDescription(args),
			Digest:        digest,
			Client:        "cub",
			ClientVersion: Version,
		},
	}

	// Emptying a Unit withdraws what it deployed, so an upload that would empty
	// anything is confirmed first. The plan comes from a dry run, which is the
	// same code path the apply takes, so what is confirmed is what happens.
	if !a.dryRun && !a.yes {
		preview, previewErr := cubapi.Upload(ctx, cubClient, req, true)
		if previewErr != nil {
			return previewErr
		}
		if err := confirmUploadEmpties(preview, usedStdin); err != nil {
			return err
		}
	}

	result, err := cubapi.Upload(ctx, cubClient, req, a.dryRun)
	if err != nil {
		return err
	}
	reportUploadResult(result)
	return nil
}

// maxUploadSourceDescription bounds the source string recorded for the upload so
// a long input list can't push the resulting change description past the
// server's LastChangeDescription limit.
const maxUploadSourceDescription = 512

// uploadSourceRef renders one input the way the user named it — an oci:// ref or
// a path rather than the temp directory it is extracted into, "stdin" for "-"
// (matching what "unit create" records for stdin input).
func uploadSourceRef(input string) string {
	if input == "-" {
		return "stdin"
	}
	return input
}

// uploadSourceDescription renders the upload's inputs the way the user named
// them, joined into the single string recorded as the upload's source.
func uploadSourceDescription(inputs []string) string {
	parts := make([]string, 0, len(inputs))
	for _, in := range inputs {
		parts = append(parts, uploadSourceRef(in))
	}
	return truncateWithEllipsis(strings.Join(parts, ", "), maxUploadSourceDescription)
}

// collectUploadFiles reads every input into memory as bundle files. The server
// splits them into resources, so the client only has to name each file the way
// the bundle's author did. It also reports the resolved digest of an oci:// input
// and whether stdin was consumed.
func collectUploadFiles(inputs []string) (files []goclientnew.UploadRequestFile, digest string, usedStdin bool, err error) {
	add := func(path, content string) {
		files = append(files, goclientnew.UploadRequestFile{Path: path, Content: content})
	}

	for _, in := range inputs {
		switch {
		case in == "-":
			data, readErr := io.ReadAll(os.Stdin)
			if readErr != nil {
				return nil, "", usedStdin, fmt.Errorf("read stdin: %w", readErr)
			}
			usedStdin = true
			add("stdin.yaml", string(data))

		case isOCIRef(in):
			dir, tmpErr := os.MkdirTemp("", "cub-oci-*")
			if tmpErr != nil {
				return nil, "", usedStdin, tmpErr
			}
			defer os.RemoveAll(dir)
			resolved, pullErr := pullOCIManifests(ctx, in, dir)
			if pullErr != nil {
				return nil, "", usedStdin, pullErr
			}
			tprint("Pulled %s (%s)", in, resolved)
			digest = resolved
			if walkErr := walkUploadDir(dir, add); walkErr != nil {
				return nil, "", usedStdin, walkErr
			}

		default:
			info, statErr := os.Stat(in)
			if statErr != nil {
				return nil, "", usedStdin, statErr
			}
			if info.IsDir() {
				if walkErr := walkUploadDir(in, add); walkErr != nil {
					return nil, "", usedStdin, walkErr
				}
				continue
			}
			data, readErr := os.ReadFile(in)
			if readErr != nil {
				return nil, "", usedStdin, readErr
			}
			// The bundle names its own files, so a path that escapes the bundle,
			// or names an absolute location, is not sent.
			add(filepath.Base(in), string(data))
		}
	}
	return files, digest, usedStdin, nil
}

// walkUploadDir adds every YAML file under dir, named relative to dir so the
// paths read as the bundle's own layout rather than as wherever it was unpacked.
func walkUploadDir(dir string, add func(path, content string)) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isUploadYAMLFile(path) {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			rel = filepath.Base(path)
		}
		add(filepath.ToSlash(rel), string(data))
		return nil
	})
}

func isUploadYAMLFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

// keyValueMap parses repeated key=value flags into a map.
func keyValueMap(values []string, flag string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(values))
	for _, kv := range values {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, fmt.Errorf("%s must be key=value: %s", flag, kv)
		}
		out[key] = value
	}
	return out, nil
}

// confirmUploadEmpties asks before an upload withdraws anything. The Units are
// emptied rather than deleted, but their resources leave the cluster on the next
// Release, which is the part worth confirming.
func confirmUploadEmpties(preview *goclientnew.UploadResult, usedStdin bool) error {
	var emptied []string
	for _, c := range preview.Components {
		for _, s := range c.Spaces {
			for _, u := range s.Units {
				if u.Action == "Empty" {
					emptied = append(emptied, u.Slug)
				}
			}
		}
	}
	if len(emptied) == 0 {
		return nil
	}

	tprint("This upload empties %d Unit(s) whose resources are no longer in the bundle:", len(emptied))
	for _, slug := range emptied {
		tprint("  - %s", slug)
	}
	tprint("Their resources are withdrawn from the cluster by the next Release. Nothing is deleted.")

	// The bundle arrived on stdin, so there is no console left to ask on.
	if usedStdin {
		return fmt.Errorf("refusing to empty %d Unit(s): the bundle was read from stdin, so there is no input left to confirm on; pass --yes", len(emptied))
	}
	tprint("")
	fmt.Print("Continue? [y/N]: ")
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return fmt.Errorf("read confirmation: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	default:
		return fmt.Errorf("cancelled")
	}
}

// reportUploadResult prints what the upload did, or would do.
func reportUploadResult(result *goclientnew.UploadResult) {
	if result.DryRun {
		tprint("Dry run: nothing was written.")
	}
	for _, c := range result.Components {
		for _, s := range c.Spaces {
			tprint("Space %s (%s)", s.SpaceSlug, s.Action)
			unchanged := 0
			for _, u := range s.Units {
				if u.Error != nil {
					tprint("  %-9s %s: %s", "FAILED", u.Slug, errString(u.Error))
					continue
				}
				if u.Action == "Unchanged" {
					unchanged++
					continue
				}
				tprint("  %-9s %s", u.Action, u.Slug)
			}
			if unchanged > 0 {
				tprint("  %-9s %d Unit(s)", "Unchanged", unchanged)
			}
			for _, l := range s.Links {
				if l.Error != nil {
					tprint("  link FAILED %s -> %s: %s", l.FromUnit, l.ToUnit, errString(l.Error))
					continue
				}
				if l.Action == "Create" {
					tprint("  linked    %s -> %s (%s)", l.FromUnit, l.ToUnit, l.Reason)
				}
			}
		}

		for _, b := range c.BrokenLinks {
			tprint("broke %s link %s -> %s to resolve cycle: %s",
				b.Kind, b.From, b.To, strings.Join(b.Cycle, " -> "))
		}
		if len(c.SkippedSecrets) > 0 {
			tprint("")
			tprint("Note: %d Secret(s) were NOT uploaded. Apply them out-of-band:", len(c.SkippedSecrets))
			for _, s := range c.SkippedSecrets {
				tprint("  - %s", s)
			}
		}
		if c.NamespaceCollision != nil {
			tprint("")
			tprint("Note: --create-namespace was given, but the bundle already carries Namespace %q,",
				c.NamespaceCollision.Namespace)
			tprint("so none was synthesized. The bundle's own Namespace is the one uploaded.")
		}
		if len(c.UnmatchedReferences) > 0 {
			tprint("")
			tprint("Note: the following references didn't resolve to any uploaded Unit (expected when the")
			tprint("target lives in the cluster, e.g. a Secret created out-of-band):")
			for _, u := range c.UnmatchedReferences {
				tprint("  - %s -> %s %q", u.FromUnit, u.TargetType, u.TargetName)
			}
		}
	}
}

// errString renders a per-item error from the API response.
func errString(e *goclientnew.ResponseError) string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return "unknown error"
}

// renderSpacePattern evaluates a --space-pattern (optionally prefixed "template:")
// over the well-known Space labels. The server renders the pattern itself; this
// is used only to scope a bare --target slug to the Space being written.
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

// resolveUploadTarget resolves a --target ref to the TargetID UUID, the target's
// ProviderType, and the fully qualified <space>/<slug> ref.
//
// A bare slug is scoped to the Space the upload writes to. A qualified ref or a
// UUID identifies the target on its own, so ParseRef handles both and the scope
// is left empty for them.
func resolveUploadTarget(unitSpace, targetRef string) (id, providerType, qualifiedRef string, err error) {
	spaceID := ""
	if !strings.Contains(targetRef, "/") {
		space, spaceErr := resolveSpace(unitSpace, "SpaceID,Slug")
		if spaceErr != nil {
			return "", "", "", spaceErr
		}
		spaceID = space.Space.SpaceID.String()
	}
	target, err := resolveTarget(targetRef, spaceID, "*")
	if err != nil {
		return "", "", "", err
	}
	if target.Target == nil {
		return "", "", "", fmt.Errorf("target %q not found", targetRef)
	}
	// The Space comes back on the expansion, which is what names a target that a
	// qualified ref or a UUID put in another Space.
	spaceSlug := unitSpace
	if target.Space != nil && target.Space.Slug != "" {
		spaceSlug = target.Space.Slug
	}
	return target.Target.TargetID.String(), target.Target.ProviderType,
		spaceSlug + "/" + target.Target.Slug, nil
}
