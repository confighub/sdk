// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/cubapi"
	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var k8sAppConfigCmd = &cobra.Command{
	Use:   "appconfig",
	Short: "Commands for configuration files held in Kubernetes ConfigMaps",
	Long: getCommandHelp(`The appconfig subcommands work with application configuration files -- JSON, YAML,
TOML, INI, properties, .env and plain text -- that Kubernetes resources carry in ConfigMaps.

"extract" moves a file out of a ConfigMap into an AppConfig unit of its own, where it is
edited, validated and diffed as the format it is, and a link writes it back into the
ConfigMap.`, ""),
}

var k8sAppConfigExtractArgs struct {
	configMap         string
	keys              []string
	toolchain         string
	slug              string
	changeSet         string
	changeDescription string
	clearance         []string
}

var k8sAppConfigExtractCmd = &cobra.Command{
	Use:   "extract <unit>",
	Short: "Move files held in a ConfigMap into AppConfig units that are written back into it",
	Long: getCommandHelp(`Move each file a ConfigMap holds in its data into an AppConfig unit of its own, and link
the ConfigMap's unit to it so that the file is written back into the ConfigMap whenever the
AppConfig unit changes.

For each data key, extract:

  1. Creates an AppConfig unit in the ConfigMap unit's space holding the file exactly as the
     ConfigMap holds it. Its toolchain is the one the key's extension names (.json is
     AppConfig/JSON, .yaml and .yml AppConfig/YAML, .toml, .ini, .properties, .env), and
     AppConfig/Text for any other key; --toolchain picks one instead.
  2. Guards the data key in the ConfigMap's unit with Link=Insert, which says a link maintains
     it. Another change to that key has to be cleared for Link=Insert: an upgrade from the
     unit's upstream, for one, no longer overwrites it unless its link carries that clearance.
  3. Creates an Insert link from the ConfigMap's unit to the AppConfig unit, which writes the
     AppConfig unit's content to that data key, stating the same guard on what it writes and
     cleared for it. Nothing else in the ConfigMap is touched: its name, namespace, labels
     and annotations stay what they are, so whatever finds the ConfigMap by them -- a Grafana
     dashboard sidecar looking for grafana_dashboard: "1", a pod mounting it by name -- keeps
     finding it.

The ConfigMap's data is not changed: the AppConfig unit starts with the content the ConfigMap
already has, and the guard is the unit's only new revision. A data key that already carries
guards, its own or the ConfigMap's, is extracted only with a --clearance covering them, which
the guard edit and the link are both given. Edit the file from then on in the
AppConfig unit; the link writes the change into the ConfigMap's unit, and the next release of
that unit carries it to the cluster.

Every data key is extracted unless --key names some. --slug names the AppConfig unit when one
key is extracted; otherwise each is named after its key without the extension (dashboard.json
becomes dashboard).

A ConfigMap annotated confighub.com/ConfigMapFormat: KeyValue holds variables rather than files,
and is refused.

When the ConfigMap's unit is a variant of an upstream unit, its upgrades carry the upstream
ConfigMap's changes to the extracted keys, which the guard now withholds. Extract the upstream
ConfigMap too, then replace the unit's UpgradeUnit link with one from each AppConfig unit to the
one extracted upstream, so the file is upgraded as a file. Extract prints a warning saying so.

Examples:
`+"```"+`
  # Move a Grafana dashboard's JSON out of its ConfigMap
  cub k8s appconfig extract --space observability dashboard-confighub-app

  # The same, naming the AppConfig unit
  cub k8s appconfig extract observability/dashboard-confighub-app --slug confighub-app-dashboard

  # One key of a ConfigMap holding several, from a unit holding several ConfigMaps
  cub k8s appconfig extract --space app app-config --configmap app/settings --key application.properties
`+"```"+`
`, `Use this to turn configuration embedded in ConfigMaps into AppConfig units, so it can be
validated and edited as its own format. The ConfigMap's unit stays the unit that is released;
the AppConfig unit is never released itself. To undo, delete the link and the AppConfig unit
and remove the guard with "cub unit set-guard --remove-guard".`),
	Args:    cobra.ExactArgs(1),
	PreRunE: spacePreRunE,
	RunE:    k8sAppConfigExtractCmdRun,
}

func init() {
	addSpaceFlags(k8sAppConfigExtractCmd)
	enableOptionalSpace(k8sAppConfigExtractCmd)
	flags := k8sAppConfigExtractCmd.Flags()
	flags.StringVar(&k8sAppConfigExtractArgs.configMap, "configmap", "", "the ConfigMap to extract from, by name or namespace/name; needed when the unit holds more than one")
	flags.StringSliceVar(&k8sAppConfigExtractArgs.keys, "key", nil, "data key to extract (repeatable); every key when omitted")
	flags.StringVar(&k8sAppConfigExtractArgs.toolchain, "toolchain", "", "AppConfig toolchain of the extracted files, such as AppConfig/JSON; by default the one each key's extension names")
	flags.StringVar(&k8sAppConfigExtractArgs.slug, "slug", "", "slug of the AppConfig unit, when one key is extracted; by default the key without its extension")
	flags.StringVar(&k8sAppConfigExtractArgs.changeSet, "changeset", "", "changeset open on the unit, as <slug> or <space>/<slug>, which the AppConfig units are created in and the guard is recorded in; open it on the unit first with cub unit update --patch --changeset")
	flags.StringVar(&k8sAppConfigExtractArgs.changeDescription, "change-desc", "", "change description")
	flags.StringArrayVar(&k8sAppConfigExtractArgs.clearance, "clearance", nil,
		"class of guarded reason the guard edit and the link are cleared for, as KEY, KEY=VALUE[;VALUE...], KEY!=VALUE[;VALUE...], or !KEY (repeatable); needed when a data key already carries guards. The link is cleared for Link=Insert as well")
	k8sAppConfigCmd.AddCommand(k8sAppConfigExtractCmd)
	k8sCmd.AddCommand(k8sAppConfigCmd)
}

// configMapResourceType is the ResourceType a ConfigMap has in a Kubernetes/YAML unit.
const configMapResourceType = "v1/ConfigMap"

// extractedFile is one data key of a ConfigMap and the AppConfig unit it goes into.
type extractedFile struct {
	key       string
	content   string
	toolchain workerapi.ToolchainType
	slug      string
}

func k8sAppConfigExtractCmdRun(_ *cobra.Command, args []string) error {
	opts := k8sAppConfigExtractArgs
	if opts.toolchain != "" && !workerapi.IsAppConfigToolchain(workerapi.ToolchainType(opts.toolchain)) {
		return fmt.Errorf("--toolchain %s is not an AppConfig toolchain", opts.toolchain)
	}

	configMapUnit, err := resolveUnit(args[0], selectedSpaceID, "*")
	if err != nil {
		return err
	}
	unit := configMapUnit.Unit
	if unit.ToolchainType != string(workerapi.ToolchainKubernetesYAML) {
		return fmt.Errorf("unit %s is %s; extract reads ConfigMaps from %s units",
			unit.Slug, unit.ToolchainType, workerapi.ToolchainKubernetesYAML)
	}
	data, err := fetchUnitData(unit.SpaceID, unit.UnitID)
	if err != nil {
		return err
	}
	configMap, configMapData, format, err := findConfigMap(data, opts.configMap)
	if err != nil {
		return fmt.Errorf("unit %s: %w", unit.Slug, err)
	}
	if format == constants.ConfigMapFormatKeyValue {
		return fmt.Errorf("ConfigMap %s is annotated %s: %s, so its data keys are variables rather than files, and extract moves files",
			configMap.ResourceName, configMapFormatAnnotation, constants.ConfigMapFormatKeyValue)
	}
	clearance, err := parseClearanceSpecs(opts.clearance)
	if err != nil {
		return err
	}
	files, err := filesToExtract(configMap, configMapData, opts.keys, opts.toolchain, opts.slug)
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := checkConfigData(string(f.toolchain), f.content); err != nil {
			return fmt.Errorf("data key %s of ConfigMap %s is not valid %s, so nothing was extracted; pass --toolchain AppConfig/Text to take it as text: %w",
				f.key, configMap.ResourceName, f.toolchain, err)
		}
	}
	// Checked before anything is written, so that a key the clearance does not cover leaves no
	// AppConfig unit behind.
	if err := checkExtractCleared(unit, configMap, files, clearance); err != nil {
		return err
	}
	var changeSetID *uuid.UUID
	if changeSetID, err = resolveOptionalChangeSetID(opts.changeSet, unit.SpaceID.String()); err != nil {
		return err
	}
	// The AppConfig units are created in the ChangeSet, and the guard's revision belongs to it only
	// if the ChangeSet is open on the ConfigMap's unit. Refused before anything is written rather
	// than leaving the guard outside the ChangeSet the AppConfig units are in.
	if changeSetID != nil && (unit.ChangeSetID == nil || *unit.ChangeSetID != *changeSetID) {
		space := unit.SpaceID.String()
		if configMapUnit.Space != nil {
			space = configMapUnit.Space.Slug
		}
		return fmt.Errorf("changeset %s is not open on unit %s, so nothing was extracted; open it with: cub unit update --patch --space %s %s --changeset %s",
			opts.changeSet, unit.Slug, space, unit.Slug, opts.changeSet)
	}
	if err := checkSlugsFree(unit.SpaceID, files); err != nil {
		return err
	}

	extracted := make([]string, 0, len(files))
	for _, f := range files {
		appConfigUnit, err := createExtractedUnit(unit, configMap, f, changeSetID, opts.changeDescription)
		if err != nil {
			return err
		}
		// Guarded before the link exists, so the link's first resolve does not race the guard's
		// write to the same unit.
		if err := guardExtractedKey(unit, configMap, f, clearance, changeSetID, opts.changeDescription); err != nil {
			return fmt.Errorf("AppConfig unit %s was created, but data key %s was not guarded and no link was created: %w",
				appConfigUnit.Slug, f.key, err)
		}
		link, err := createExtractLink(unit, appConfigUnit, configMap, f, clearance)
		if err != nil {
			return fmt.Errorf("AppConfig unit %s was created and data key %s guarded, but the link to it from unit %s was not created: %w",
				appConfigUnit.Slug, f.key, unit.Slug, err)
		}
		extracted = append(extracted, appConfigUnit.Slug)
		if !quiet {
			fmt.Printf("Extracted data key %s of ConfigMap %s into %s unit %s, written back by link %s\n",
				f.key, configMap.ResourceName, f.toolchain, appConfigUnit.Slug, link.Slug)
		}
	}
	warnExtractedFromVariant(configMapUnit, extracted)
	return nil
}

// configMapFormatAnnotation is the annotation that says whether a ConfigMap holds files or
// variables.
const configMapFormatAnnotation = "confighub.com/" + constants.ConfigMapFormatKeySuffix

// warnExtractedFromVariant warns that a variant's upgrades still carry the upstream ConfigMap's
// changes to the keys extracted, which the guards on them now withhold, and says how to upgrade
// the files instead.
func warnExtractedFromVariant(configMapUnit *goclientnew.ExtendedUnit, extracted []string) {
	unit := configMapUnit.Unit
	if unit.UpstreamUnitID == nil || len(extracted) == 0 {
		return
	}
	upstream := unit.UpstreamUnitID.String()
	if configMapUnit.UpstreamUnit != nil {
		upstream = configMapUnit.UpstreamUnit.Slug
		if configMapUnit.UpstreamSpace != nil {
			upstream = configMapUnit.UpstreamSpace.Slug + "/" + upstream
		}
	}
	fmt.Fprintf(os.Stderr, "Warning: unit %s is a variant of unit %s. Its upgrades still carry changes to the extracted "+
		"data, and the guard withholds them as conflicts. To upgrade the files instead, extract the same keys from %s "+
		"(cub k8s appconfig extract %s), delete the UpgradeUnit link from %s, and create an UpgradeUnit link from each "+
		"of %s to the AppConfig unit extracted upstream (cub link create --update-type UpgradeUnit).\n",
		unit.Slug, upstream, upstream, upstream, unit.Slug, strings.Join(extracted, ", "))
}

// checkExtractCleared refuses keys that carry guards, the key's own or inherited from the data or
// the ConfigMap, that the clearance does not cover. Setting the guard edits a guarded path, and
// the link writes it, so both need the clearance.
func checkExtractCleared(unit *goclientnew.Unit, configMap *api.ResourceInfo, files []extractedFile, flagClearance goclientnew.Clearance) error {
	if unit.PathAnnotations == nil {
		return nil
	}
	var annotations api.PathAnnotationList
	var clearance api.Clearance
	if err := convertJSON(*unit.PathAnnotations, &annotations); err != nil {
		return err
	}
	if err := convertJSON(flagClearance, &clearance); err != nil {
		return err
	}
	for i := range annotations {
		resource := &annotations[i]
		if resource.Resource.ResourceType != configMap.ResourceType || resource.Resource.ResourceName != configMap.ResourceName {
			continue
		}
		var needed []string
		for _, f := range files {
			for _, withheld := range clearance.Withheld(resource.GuardsForPath(api.ResolvedPath(extractedKeyPath(f.key)))) {
				spec := withheld.Key + "=" + withheld.Value
				if withheld.Precondition {
					return fmt.Errorf("data key %s of ConfigMap %s carries the guard %s, which --clearance forbids with !%s",
						f.key, configMap.ResourceName, spec, withheld.Key)
				}
				if !slices.Contains(needed, spec) {
					needed = append(needed, spec)
				}
			}
		}
		if len(needed) > 0 {
			flags := make([]string, len(needed))
			for j, spec := range needed {
				flags[j] = fmt.Sprintf("--clearance %q", spec)
			}
			return fmt.Errorf("ConfigMap %s already guards the data being extracted, so nothing was extracted; to extract it anyway, pass %s",
				configMap.ResourceName, strings.Join(flags, " "))
		}
	}
	return nil
}

// convertJSON converts between a client type and the API type it is the wire form of.
func convertJSON(from, to any) error {
	encoded, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, to)
}

// findConfigMap returns the ConfigMap the unit's data holds, its data, and its
// confighub.com/ConfigMapFormat annotation: the one named, or the only one there is.
func findConfigMap(data, name string) (*api.ResourceInfo, map[string]string, string, error) {
	docs, err := gaby.ParseAll([]byte(data))
	if err != nil {
		return nil, nil, "", err
	}
	provider := k8skit.NewK8sResourceProvider()
	type found struct {
		info *api.ResourceInfo
		doc  *gaby.YamlDoc
	}
	var matches []found
	var all []string
	for _, doc := range docs {
		if doc == nil {
			continue
		}
		info, err := yamlkit.GetResourceInfo(doc, provider)
		if err != nil || info.ResourceType != configMapResourceType {
			continue
		}
		all = append(all, string(info.ResourceName))
		if name == "" || string(info.ResourceName) == name || string(info.ResourceNameWithoutScope) == name {
			matches = append(matches, found{info, doc})
		}
	}
	switch {
	case len(all) == 0:
		return nil, nil, "", fmt.Errorf("it holds no ConfigMap")
	case len(matches) == 0:
		return nil, nil, "", fmt.Errorf("it holds no ConfigMap %s; it holds %s", name, strings.Join(all, ", "))
	case len(matches) > 1 && name == "":
		return nil, nil, "", fmt.Errorf("it holds %d ConfigMaps (%s); name one with --configmap", len(all), strings.Join(all, ", "))
	case len(matches) > 1:
		return nil, nil, "", fmt.Errorf("--configmap %s names %d ConfigMaps; give it as namespace/name", name, len(matches))
	}
	configMapData := map[string]string{}
	if node := matches[0].doc.Path("data"); node != nil {
		values, ok := node.Data().(map[string]any)
		if !ok {
			return nil, nil, "", fmt.Errorf("ConfigMap %s: data is not a map", matches[0].info.ResourceName)
		}
		for key, value := range values {
			s, ok := value.(string)
			if !ok {
				return nil, nil, "", fmt.Errorf("ConfigMap %s: data key %s does not hold a string", matches[0].info.ResourceName, key)
			}
			configMapData[key] = s
		}
	}
	format, _, _ := yamlkit.YamlSafePathGetValue[string](matches[0].doc,
		api.ResolvedPath(k8skit.K8sContextPath(constants.ConfigMapFormatKeySuffix)), true)
	return matches[0].info, configMapData, format, nil
}

// filesToExtract picks the data keys to extract and says what each becomes.
func filesToExtract(configMap *api.ResourceInfo, data map[string]string, keys []string, toolchain, slug string) ([]extractedFile, error) {
	present := make([]string, 0, len(data))
	for key := range data {
		present = append(present, key)
	}
	sort.Strings(present)
	if len(keys) == 0 {
		keys = present
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("ConfigMap %s has no data to extract", configMap.ResourceName)
	}
	if slug != "" && len(keys) > 1 {
		return nil, fmt.Errorf("--slug names one AppConfig unit, and %d keys are being extracted (%s); name one with --key",
			len(keys), strings.Join(keys, ", "))
	}
	files := make([]extractedFile, 0, len(keys))
	slugs := map[string]string{}
	for _, key := range keys {
		content, ok := data[key]
		if !ok {
			return nil, fmt.Errorf("ConfigMap %s has no data key %s; it has %s", configMap.ResourceName, key, strings.Join(present, ", "))
		}
		f := extractedFile{key: key, content: content, toolchain: workerapi.ToolchainType(toolchain), slug: slug}
		if f.toolchain == "" {
			f.toolchain = appConfigToolchainForKey(key)
		}
		if f.slug == "" {
			stem := strings.TrimSuffix(key, filepath.Ext(key))
			if stem == "" {
				// A key that is only an extension, such as .env, is named for it.
				stem = strings.TrimPrefix(key, ".")
			}
			f.slug = makeSlug(stem)
		}
		if f.slug == "" {
			return nil, fmt.Errorf("data key %s gives no unit slug; name its AppConfig unit with --key %s --slug", key, key)
		}
		if other, taken := slugs[f.slug]; taken {
			return nil, fmt.Errorf("data keys %s and %s would both be extracted into unit %s; extract them one at a time with --key and --slug",
				other, key, f.slug)
		}
		slugs[f.slug] = key
		files = append(files, f)
	}
	return files, nil
}

// checkSlugsFree refuses slugs that units in the space already have, before anything is written,
// so that a key whose unit cannot be created does not leave the keys before it extracted.
func checkSlugsFree(spaceID uuid.UUID, files []extractedFile) error {
	slugs := make([]string, len(files))
	for i, f := range files {
		slugs[i] = f.slug
	}
	units, err := cubapi.ListUnits(ctx, cubClient, cubapi.Where{}.Eq("SpaceID", spaceID.String()).InStrings("Slug", slugs),
		cubapi.ListOpts{Select: "UnitID,Slug", IncludeHidden: "*"})
	if err != nil {
		return err
	}
	if len(units) == 0 {
		return nil
	}
	taken := make([]string, 0, len(units))
	for _, u := range units {
		taken = append(taken, u.Unit.Slug)
	}
	sort.Strings(taken)
	return fmt.Errorf("the space already has units %s, so nothing was extracted; name the AppConfig units with --key and --slug",
		strings.Join(taken, ", "))
}

// appConfigToolchainForKey is the AppConfig toolchain whose files carry the key's extension, and
// AppConfig/Text, which holds any file, for a key with none of theirs.
func appConfigToolchainForKey(key string) workerapi.ToolchainType {
	ext := strings.ToLower(filepath.Ext(key))
	if ext == ".yml" {
		return workerapi.ToolchainAppConfigYAML
	}
	for toolchain, toolchainExt := range workerapi.AppConfigFileExtensions {
		if ext == toolchainExt {
			return toolchain
		}
	}
	return workerapi.ToolchainAppConfigText
}

// extractedKeyPath is the path of a ConfigMap's data key. A dot in the key is escaped, as it is
// in every path ConfigHub stores, since the dot separates the segments.
func extractedKeyPath(key string) string {
	return "data." + yamlkit.EscapeDotsInPathSegment(key)
}

func createExtractedUnit(configMapUnit *goclientnew.Unit, configMap *api.ResourceInfo, f extractedFile,
	changeSetID *uuid.UUID, changeDescription string) (*goclientnew.Unit, error) {
	if changeDescription == "" {
		changeDescription = fmt.Sprintf("Extracted from data key %s of ConfigMap %s in unit %s",
			f.key, configMap.ResourceName, configMapUnit.Slug)
	}
	// The description goes on the revision that writes the content. Given to the create as well,
	// the content's revision would repeat it, which a write takes as giving none.
	newUnit := goclientnew.Unit{
		SpaceID:       configMapUnit.SpaceID,
		Slug:          f.slug,
		DisplayName:   f.slug,
		ToolchainType: string(f.toolchain),
		ChangeSetID:   changeSetID,
	}
	res, err := cubClientNew.CreateUnitWithResponse(ctx, configMapUnit.SpaceID, &goclientnew.CreateUnitParams{}, newUnit)
	if cubapi.IsAPIError(err, res) {
		return nil, fmt.Errorf("creating AppConfig unit %s: %w", f.slug, cubapi.InterpretErrorGeneric(err, res))
	}
	created, err := unitFromWrite(res.JSON200)
	if err != nil {
		return nil, err
	}
	params, err := unitDataParams(changeDescription, changeSetIDString(changeSetID))
	if err != nil {
		return nil, err
	}
	if _, err := putUnitData(created.SpaceID, created.UnitID, f.content, params); err != nil {
		return nil, fmt.Errorf("AppConfig unit %s was created, but its content could not be written: %w", created.Slug, err)
	}
	return created, nil
}

// extractUpdateType is the UpdateType of the link that writes an extracted file back, and the
// value of the Link guard it states.
const extractUpdateType = "Insert"

// createExtractLink creates the Insert link that writes the AppConfig unit's content to the data
// key, stating and cleared for the guard that says the link maintains it.
func createExtractLink(configMapUnit, appConfigUnit *goclientnew.Unit, configMap *api.ResourceInfo, f extractedFile,
	flagClearance goclientnew.Clearance) (*goclientnew.Link, error) {
	guards := goclientnew.GuardStamp{api.GuardKeyLink: extractUpdateType}
	var clearance api.Clearance
	if err := convertJSON(flagClearance, &clearance); err != nil {
		return nil, err
	}
	var linkClearance goclientnew.Clearance
	if err := convertJSON(api.ClearanceAdmitting(clearance, api.GuardKeyLink, extractUpdateType), &linkClearance); err != nil {
		return nil, err
	}
	bindings := goclientnew.BindingList{{
		NeededResource: &goclientnew.ResourceInfo{
			ResourceType: configMapResourceType,
			ResourceName: string(configMap.ResourceName),
		},
		NeededPath: extractedKeyPath(f.key),
		DataType:   string(api.DataTypeString),
	}}
	link := goclientnew.Link{
		SpaceID:        configMapUnit.SpaceID,
		FromUnitID:     configMapUnit.UnitID,
		ToSpaceID:      appConfigUnit.SpaceID,
		ToUnitID:       appConfigUnit.UnitID,
		UpdateType:     extractUpdateType,
		AutoUpdate:     true,
		ManualBindings: &bindings,
		Guards:         &guards,
		Clearance:      &linkClearance,
	}
	res, err := cubClientNew.CreateLinkWithResponse(ctx, configMapUnit.SpaceID, nil, link)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	return res.JSON200, nil
}

// guardExtractedKey guards the data key with the guard the link states. The link stamps it only
// on what its resolve writes, and its first resolve writes nothing, since the AppConfig unit
// starts with the content the key already has.
func guardExtractedKey(configMapUnit *goclientnew.Unit, configMap *api.ResourceInfo, f extractedFile,
	clearance goclientnew.Clearance, changeSetID *uuid.UUID, changeDescription string) error {
	if changeDescription == "" {
		changeDescription = fmt.Sprintf("Guard data key %s, written by a link from AppConfig unit %s", f.key, f.slug)
	}
	body := goclientnew.UnitGuardRequest{
		ChangeDescription: changeDescription,
		ChangeSetID:       changeSetID,
		ResourceGuards: []goclientnew.ResourceGuards{{
			Resource: &goclientnew.ResourceInfo{
				ResourceType: configMapResourceType,
				ResourceName: string(configMap.ResourceName),
			},
			Set: map[string]map[string]string{extractedKeyPath(f.key): {api.GuardKeyLink: extractUpdateType}},
		}},
	}
	if len(clearance) > 0 {
		body.Clearance = &clearance
	}
	res, err := cubClientNew.SetUnitGuardWithResponse(ctx, configMapUnit.SpaceID, configMapUnit.UnitID, body)
	if cubapi.IsAPIError(err, res) {
		return withClearanceFlags(cubapi.InterpretErrorGeneric(err, res))
	}
	return nil
}

func changeSetIDString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}
