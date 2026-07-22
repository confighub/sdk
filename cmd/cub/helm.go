// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/confighub/sdk/bridge-impl/helmutils"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// AnnotationGeneratesSpaceID is the Space annotation stamped on a helm source
// space, recording the UUID of the base variant space its HelmSource units
// generate. It stands in for a future generator link type.
const AnnotationGeneratesSpaceID = "GeneratesSpaceID"

const (
	helmSourceSpaceSuffix = "-helm"
	baseSpaceSuffix       = "-base"
	variantLabelBase      = "base"
	variantLabelHelm      = "helm-source"

	toolchainKubernetesYAML = "Kubernetes/YAML"
	toolchainConfigHubYAML  = "ConfigHub/YAML"
)

// helmCmd is the top-level command group for Helm-related operations. Helm
// commands operate on a component's source and base spaces derived from
// --component, not on the default space context, so like variantCmd they use
// only globalPreRun.
var helmCmd = &cobra.Command{
	Use:   "helm",
	Short: "Helm commands",
	Long: getCommandHelp(`Install Helm charts as ConfigHub components.

A chart is rendered entirely client-side and its output becomes units in the
component's base variant space (<component>-base). The chart reference, values,
and options are recorded as a HelmSource unit in the component's helm source
space (<component>-helm), which is the source of truth for upgrades.

Rendering never contacts a cluster: hooks are dropped unless --include-hooks is
set, the lookup template function returns nothing, and capabilities are Helm's
defaults. Charts that depend on those are out of scope; charts that render
cleanly client-side work fully.

Deployments are created from the base with 'cub variant create' and updated
with 'cub variant promote'; helm commands never touch them.`, ""),
	PersistentPreRunE: globalPreRun,
}

func init() {
	rootCmd.AddCommand(helmCmd)
}

// helmComponentSpaces holds the two spaces of a helm-installed component.
type helmComponentSpaces struct {
	source *goclientnew.Space
	base   *goclientnew.Space
}

// getSpaceBySlug returns the space with the given slug, or nil if it does not exist.
func getSpaceBySlug(slug string) (*goclientnew.Space, error) {
	spaces, err := apiListSpaces("Slug = '"+slug+"'", "*")
	if err != nil {
		return nil, err
	}
	for _, s := range spaces {
		if s.Slug == slug {
			return s, nil
		}
	}
	return nil, nil
}

// createHelmSpace creates a space with the given metadata.
func createHelmSpace(slug string, labels, annotations map[string]string) (*goclientnew.Space, error) {
	space := goclientnew.Space{
		Slug:        slug,
		Labels:      labels,
		Annotations: annotations,
	}
	res, err := cubClientNew.CreateSpaceWithResponse(ctx, &goclientnew.CreateSpaceParams{}, space)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	created := res.JSON200
	if created == nil {
		return nil, fmt.Errorf("failed to create space %q: %s", slug, res.Status())
	}
	tprint("Created space %s", slug)
	return created, nil
}

// ensureComponentSpaces gets or creates the component's base variant space and
// helm source space, stamping the component labels and the generator annotation.
func ensureComponentSpaces(component string) (*helmComponentSpaces, error) {
	baseSlug := component + baseSpaceSuffix
	sourceSlug := component + helmSourceSpaceSuffix

	base, err := getSpaceBySlug(baseSlug)
	if err != nil {
		return nil, err
	}
	if base == nil {
		base, err = createHelmSpace(baseSlug, map[string]string{
			"Component": component,
			"Variant":   variantLabelBase,
		}, nil)
		if err != nil {
			return nil, err
		}
	}

	source, err := getSpaceBySlug(sourceSlug)
	if err != nil {
		return nil, err
	}
	if source == nil {
		source, err = createHelmSpace(sourceSlug, map[string]string{
			"Component": component,
			"Variant":   variantLabelHelm,
		}, map[string]string{
			AnnotationGeneratesSpaceID: base.SpaceID.String(),
		})
		if err != nil {
			return nil, err
		}
	} else if source.Annotations[AnnotationGeneratesSpaceID] != base.SpaceID.String() {
		if err := patchHelmSpaceGeneratesAnnotation(source.SpaceID, base.SpaceID); err != nil {
			return nil, err
		}
	}

	return &helmComponentSpaces{source: source, base: base}, nil
}

// getComponentSpaces returns the component's spaces without creating anything,
// for commands that require a prior install.
func getComponentSpaces(component string) (*helmComponentSpaces, error) {
	base, err := getSpaceBySlug(component + baseSpaceSuffix)
	if err != nil {
		return nil, err
	}
	source, err := getSpaceBySlug(component + helmSourceSpaceSuffix)
	if err != nil {
		return nil, err
	}
	if source == nil || base == nil {
		return nil, fmt.Errorf("component %q has no helm source space; run 'cub helm install' first (or pass --component)", component)
	}
	return &helmComponentSpaces{source: source, base: base}, nil
}

// patchHelmSpaceGeneratesAnnotation sets the GeneratesSpaceID annotation on the
// helm source space.
func patchHelmSpaceGeneratesAnnotation(sourceSpaceID, baseSpaceID uuid.UUID) error {
	patchMap := map[string]any{
		"Annotations": map[string]any{
			AnnotationGeneratesSpaceID: baseSpaceID.String(),
		},
	}
	patchData, err := json.Marshal(patchMap)
	if err != nil {
		return err
	}
	if _, err := patchSpace(sourceSpaceID, patchData); err != nil {
		return fmt.Errorf("failed to set %s annotation on helm source space: %w", AnnotationGeneratesSpaceID, err)
	}
	return nil
}

// helmSourceUnit pairs a source-space unit with its parsed HelmSource document.
type helmSourceUnit struct {
	unit   *goclientnew.Unit
	source *helmutils.HelmSource
}

// listHelmSources returns the parsed HelmSource units in the source space.
// Units that do not parse are skipped with a warning.
func listHelmSources(sourceSpaceID uuid.UUID) ([]helmSourceUnit, error) {
	units, err := apiListUnits(sourceSpaceID.String(), "", "*")
	if err != nil {
		return nil, err
	}
	sources := make([]helmSourceUnit, 0, len(units))
	for _, u := range units {
		data, err := base64.StdEncoding.DecodeString(u.Data)
		if err != nil {
			continue
		}
		src, err := helmutils.ParseHelmSource(data)
		if err != nil {
			tprint("Warning: unit %s in the helm source space is not a valid HelmSource: %v", u.Slug, err)
			continue
		}
		sources = append(sources, helmSourceUnit{unit: u, source: src})
	}
	return sources, nil
}

// checkPrefixConflict enforces that no two HelmSources in a component share a
// unit prefix. In particular at most one may have an empty prefix.
func checkPrefixConflict(others []helmSourceUnit, release, prefix string) error {
	for _, other := range others {
		if other.unit.Slug == makeSlug(release) {
			continue
		}
		if other.source.Spec.UnitPrefix == prefix {
			if prefix == "" {
				return fmt.Errorf("release %q already uses an empty unit prefix in this component; pass --prefix", other.source.Spec.Release.Name)
			}
			return fmt.Errorf("release %q already uses unit prefix %q in this component; pass a different --prefix", other.source.Spec.Release.Name, prefix)
		}
	}
	return nil
}

// applyHelmSource renders the HelmSource and reconciles both the source unit
// and the generated units in the base space. It is the shared core of install
// and upgrade.
func applyHelmSource(src *helmutils.HelmSource, component string, spaces *helmComponentSpaces) error {
	chrt, err := helmutils.LoadChart(src)
	if err != nil {
		return err
	}

	result, err := helmutils.Generate(chrt, src, component)
	if err != nil {
		return err
	}

	for _, dropped := range result.DroppedHooks {
		tprint("Dropped hook manifest: %s (use --include-hooks to keep hook manifests as plain resources)", dropped)
	}
	if len(result.SkippedCRDFiles) > 0 {
		tprint("Skipped %d CRD file(s) due to --skip-crds", len(result.SkippedCRDFiles))
	}
	if src.Spec.IncludeHooks && chartDeclaresHooks(result) {
		tprint("Note: hook manifests are included as plain resources; Helm hook lifecycle (weights, deletion policies) does not apply")
	}

	src.Status.ResolvedVersion = result.ResolvedVersion
	src.Status.AppVersion = result.AppVersion

	if err := upsertHelmSourceUnit(spaces.source.SpaceID, src, result.UnitLabels); err != nil {
		return err
	}

	return reconcileHelmUnits(spaces.base.SpaceID, src, result)
}

// chartDeclaresHooks reports whether the render produced any hook manifests.
// When hooks are included they are not in DroppedHooks, so detect them by
// scanning the generated content for the annotation.
func chartDeclaresHooks(result *helmutils.GenerateResult) bool {
	if len(result.DroppedHooks) > 0 {
		return true
	}
	for _, u := range result.Units {
		if containsHelmHookAnnotation(u.Content) {
			return true
		}
	}
	return false
}

func containsHelmHookAnnotation(content string) bool {
	return strings.Contains(content, "helm.sh/hook:") || strings.Contains(content, `"helm.sh/hook"`)
}

// upsertHelmSourceUnit creates or updates the HelmSource unit in the source space.
func upsertHelmSourceUnit(sourceSpaceID uuid.UUID, src *helmutils.HelmSource, labels map[string]string) error {
	data, err := src.Marshal()
	if err != nil {
		return err
	}
	slug := makeSlug(src.Spec.Release.Name)

	existing, err := getUnitBySlug(sourceSpaceID, slug)
	if err != nil {
		return err
	}
	if existing == nil {
		_, err := createUnitInSpace(sourceSpaceID, slug, toolchainConfigHubYAML, string(data), labels)
		if err != nil {
			return fmt.Errorf("failed to create HelmSource unit %q: %w", slug, err)
		}
		tprint("Created HelmSource unit %s", slug)
		return nil
	}

	existing.Data = base64.StdEncoding.EncodeToString(data)
	mergeLabels(existing, labels)
	updated, err := updateUnit(existing.SpaceID, existing, &goclientnew.UpdateUnitParams{})
	if err != nil {
		return fmt.Errorf("failed to update HelmSource unit %q: %w", slug, err)
	}
	tprint("Updated HelmSource unit %s", updated.Slug)
	return nil
}

// reconcileHelmUnits makes the base space's units for this release match the
// generated set: changed units are updated, new ones created, and units whose
// source file disappeared are deleted.
func reconcileHelmUnits(baseSpaceID uuid.UUID, src *helmutils.HelmSource, result *helmutils.GenerateResult) error {
	release := src.Spec.Release.Name
	existing, err := apiListUnits(baseSpaceID.String(), fmt.Sprintf("Labels.%s = '%s'", helmutils.HelmReleaseLabel, release), "*")
	if err != nil {
		return err
	}
	existingBySlug := map[string]*goclientnew.Unit{}
	for _, u := range existing {
		existingBySlug[u.Slug] = u
	}

	desired := map[string]bool{}
	for _, gen := range result.Units {
		desired[gen.Slug] = true
		if ex, ok := existingBySlug[gen.Slug]; ok {
			current, decodeErr := base64.StdEncoding.DecodeString(ex.Data)
			if decodeErr == nil && string(current) == gen.Content && labelsMatch(ex.Labels, result.UnitLabels) {
				continue
			}
			ex.Data = base64.StdEncoding.EncodeToString([]byte(gen.Content))
			mergeLabels(ex, result.UnitLabels)
			updated, err := updateUnit(ex.SpaceID, ex, &goclientnew.UpdateUnitParams{})
			if err != nil {
				return fmt.Errorf("failed to update unit %q: %w", gen.Slug, err)
			}
			if wait {
				if err := awaitTriggersRemoval(updated); err != nil {
					return err
				}
			}
			displayUpdateResults(updated, "unit", updated.Slug, updated.UnitID.String(), displayUnitDetails)
			continue
		}

		// A synthesized Namespace unit (Source == "") may already exist,
		// created by another release sharing the namespace; leave it alone.
		if gen.Source == "" {
			other, err := getUnitBySlug(baseSpaceID, gen.Slug)
			if err != nil {
				return err
			}
			if other != nil {
				if !quiet {
					tprint("Namespace unit %s already exists (shared); leaving it unchanged", gen.Slug)
				}
				continue
			}
		}

		created, err := createUnitInSpace(baseSpaceID, gen.Slug, toolchainKubernetesYAML, gen.Content, result.UnitLabels)
		if err != nil {
			return fmt.Errorf("failed to create unit %q: %w", gen.Slug, err)
		}
		if wait {
			if err := awaitTriggersRemoval(created); err != nil {
				return err
			}
		}
		displayCreateResults(created, "unit", created.Slug, created.UnitID.String(), displayUnitDetails)
	}

	for slug, ex := range existingBySlug {
		if desired[slug] {
			continue
		}
		deleteRes, err := cubClientNew.DeleteUnitWithResponse(ctx, baseSpaceID, ex.UnitID)
		if cubapi.IsAPIError(err, deleteRes) {
			return fmt.Errorf("failed to delete unit %q (its source file was removed from the chart): %w",
				slug, cubapi.InterpretErrorGeneric(err, deleteRes))
		}
		tprint("Deleted unit %s (its source file was removed from the chart)", slug)
	}

	return nil
}

// getUnitBySlug returns the unit with the given slug in the space, or nil if
// it does not exist.
func getUnitBySlug(spaceID uuid.UUID, slug string) (*goclientnew.Unit, error) {
	units, err := apiListUnits(spaceID.String(), "Slug = '"+slug+"'", "*")
	if err != nil {
		return nil, err
	}
	for _, u := range units {
		if u.Slug == slug {
			return u, nil
		}
	}
	return nil, nil
}

// createUnitInSpace creates a unit with the given content and labels.
func createUnitInSpace(spaceID uuid.UUID, slug, toolchainType, content string, labels map[string]string) (*goclientnew.Unit, error) {
	apiUnit := goclientnew.Unit{
		SpaceID:       spaceID,
		Slug:          slug,
		ToolchainType: toolchainType,
		Data:          base64.StdEncoding.EncodeToString([]byte(content)),
		Labels:        labels,
	}
	unitRes, err := cubClientNew.CreateUnitWithResponse(ctx, spaceID, &goclientnew.CreateUnitParams{}, apiUnit)
	if cubapi.IsAPIError(err, unitRes) {
		return nil, cubapi.InterpretErrorGeneric(err, unitRes)
	}
	created := unitRes.JSON200
	if created == nil {
		return nil, fmt.Errorf("unexpected response status %s", unitRes.Status())
	}
	return created, nil
}

// mergeLabels sets the given labels on the unit, preserving unrelated ones.
func mergeLabels(unit *goclientnew.Unit, labels map[string]string) {
	if unit.Labels == nil {
		unit.Labels = map[string]string{}
	}
	maps.Copy(unit.Labels, labels)
}

// labelsMatch reports whether every wanted label is present with the same value.
func labelsMatch(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}
