// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package flux

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/bridge-impl/common"
	"github.com/confighub/sdk/bridge-impl/helmutils"
	"github.com/confighub/sdk/bridge-impl/kubernetes"
	"github.com/confighub/sdk/bridge-impl/ociutils"
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	funcApi "github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/worker/api"
	"github.com/confighub/sdk/core/worker/lib"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/fluxcd/cli-utils/pkg/object"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

// FluxOCIWorker transforms Kubernetes manifests into Flux OCIRepository + Kustomization CRs
// using OCI registry as the source, then applies them using the parent's Kubernetes applier.
type FluxOCIWorker struct {
	kubernetes.KubernetesBridgeWorker
	workerID     string
	workerSecret string
}

// NewFluxOCIWorker creates a new FluxOCIWorker with properly initialized
// embedded kubernetes.KubernetesBridgeWorker. workerID and workerSecret are used to auto-generate
// OCI credential Secrets for Flux.
func NewFluxOCIWorker(workerID, workerSecret string) *FluxOCIWorker {
	return &FluxOCIWorker{
		KubernetesBridgeWorker: *kubernetes.NewKubernetesBridgeWorker(),
		workerID:               workerID,
		workerSecret:           workerSecret,
	}
}

var _ api.BridgeWorker = (*FluxOCIWorker)(nil)
var _ api.WatchableWorker = (*FluxOCIWorker)(nil)

func (w *FluxOCIWorker) ID() api.BridgeWorkerID {
	return api.BridgeWorkerID{
		ProviderType:   api.ProviderFluxOCI,
		ToolchainTypes: []workerapi.ToolchainType{workerapi.ToolchainKubernetesYAML},
	}
}

// FluxOCIBridgeOptions contains the configuration parameters for the Flux OCI bridge worker.
type FluxOCIBridgeOptions struct {
	KubeContext      string // Kubernetes context name to use (defaults to current context)
	WaitTimeout      string
	FluxNamespace    string // Namespace where Flux CRs will be created (default: "flux-system")
	TargetNamespace  string // Target namespace for deployed resources (default: "default")
	Interval         string // Flux reconcile interval (default: "10m")
	OCIRepoURL       string // OCI registry URL - if empty, auto-constructed from OCIHost and unit info
	OCIHost          string // OCI registry host - optional, inferred from server URL if not set
	OCIPath          string // Path within OCI artifact (default: ".")
	TargetRevision   string // OCI tag (default: "latest")
	Prune            bool   // Enable pruning of orphaned resources (default: true)
	DisableRepoCreds bool   // Skip auto-generation of OCI credentials Secret (default: false)
}

// Default values for Flux OCI worker parameters
const (
	defaultFluxNamespace      = "flux-system"
	defaultFluxInterval       = "10m"
	defaultFluxOCIPath        = "."
	defaultFluxTargetRevision = "latest"
	ociURLScheme                 = "oci://"
	defaultPollInterval          = 5 * time.Second
	annotationExternalLink       = "link.argocd.argoproj.io/external-link"
	k8sKindSecret                = "Secret"
	defaultDestinationNamespace  = "default"
	configHubUnitURLFormat       = "%s/units/%s/%s"
	defaultConfigHubURL          = "https://hub.confighub.com"
)

// Flux Kubernetes resource identifiers
const (
	fluxOCIRepoAPIVersion       = "source.toolkit.fluxcd.io/v1"
	fluxKustomizationAPIVersion = "kustomize.toolkit.fluxcd.io/v1"
	fluxKindOCIRepository       = "OCIRepository"
	fluxKindKustomization       = "Kustomization"
)

// Flux condition constants
const (
	fluxConditionReady       = "Ready"
	fluxConditionStatusTrue  = "True"
	fluxConditionStatusFalse = "False"
)

// OCI creds label constants for Flux
const (
	labelFluxManagedByValue = "flux-oci-bridge"
)

// OCI creds secret constants for Flux
const (
	fluxOCICredsSecretPrefix = "confighub-oci-creds-"
)

type fluxOCIArgs struct {
	Name            string
	FluxNamespace   string
	UnitSlug        string
	UnitID          string
	SpaceID         string
	RevisionNum     string
	OCIRepoURL      string
	OCIPath         string
	TargetRevision  string
	TargetNamespace string
	Interval        string
	Prune           bool
	Insecure        bool   // When true, sets spec.insecure on OCIRepository for HTTP registries
	SecretName      string // OCI credentials secret name (empty if disabled)
	ConfigHubURL    string
	// Helm-specific fields (set when unit is a Helm chart)
	IsHelm           bool
	HelmReleaseName  string
	HelmChartName    string
	HelmChartVersion string
}

func generateFluxOCIRepository(args *fluxOCIArgs) ([]byte, error) {
	spec := map[string]interface{}{
		"interval": args.Interval,
		"url":      args.OCIRepoURL,
		"ref": map[string]interface{}{
			"tag": args.TargetRevision,
		},
	}
	if args.Insecure {
		spec["insecure"] = true
	}
	if args.SecretName != "" {
		spec["secretRef"] = map[string]interface{}{
			"name": args.SecretName,
		}
	}

	repo := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": fluxOCIRepoAPIVersion,
			"kind":       fluxKindOCIRepository,
			"metadata": map[string]interface{}{
				"name":      args.Name,
				"namespace": args.FluxNamespace,
				"annotations": map[string]interface{}{
					k8skit.UnitSlugAnnotation:    args.UnitSlug,
					k8skit.SpaceIDAnnotation:     args.SpaceID,
					k8skit.RevisionNumAnnotation: args.RevisionNum,
					annotationExternalLink:       fmt.Sprintf(configHubUnitURLFormat, args.ConfigHubURL, args.SpaceID, args.UnitID),
				},
				"labels": map[string]interface{}{
					k8skit.LabelManagedBy: labelFluxManagedByValue,
				},
			},
			"spec": spec,
		},
	}

	out, err := yaml.Marshal(repo.Object)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Flux OCIRepository to YAML: %w", err)
	}
	return out, nil
}

func generateFluxKustomization(args *fluxOCIArgs) ([]byte, error) {
	spec := map[string]interface{}{
		"interval": args.Interval,
		"sourceRef": map[string]interface{}{
			"kind": fluxKindOCIRepository,
			"name": args.Name,
		},
		"path":            args.OCIPath,
		"prune":           args.Prune,
		"targetNamespace": args.TargetNamespace,
		"wait":            true,
	}

	ks := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": fluxKustomizationAPIVersion,
			"kind":       fluxKindKustomization,
			"metadata": map[string]interface{}{
				"name":      args.Name,
				"namespace": args.FluxNamespace,
				"annotations": map[string]interface{}{
					k8skit.UnitSlugAnnotation:    args.UnitSlug,
					k8skit.SpaceIDAnnotation:     args.SpaceID,
					k8skit.RevisionNumAnnotation: args.RevisionNum,
					annotationExternalLink:       fmt.Sprintf(configHubUnitURLFormat, args.ConfigHubURL, args.SpaceID, args.UnitID),
				},
				"labels": map[string]interface{}{
					k8skit.LabelManagedBy: labelFluxManagedByValue,
				},
			},
			"spec": spec,
		},
	}

	out, err := yaml.Marshal(ks.Object)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Flux Kustomization to YAML: %w", err)
	}
	return out, nil
}

// dockerConfigAuth represents a single registry authentication entry in a Docker config.
type dockerConfigAuth struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Auth     string `json:"auth"`
}

// dockerConfig represents the Docker config.json structure used for registry authentication.
type dockerConfig struct {
	Auths map[string]dockerConfigAuth `json:"auths"`
}

// generateFluxOCICreds generates a Docker registry Secret for Flux OCI authentication.
func generateFluxOCICreds(host, namespace, workerID, workerSecret string) ([]byte, error) {
	normalizedHost := k8skit.K8sNormalizeName(host)
	secretName := fluxOCICredsSecretPrefix + normalizedHost

	auth := base64.StdEncoding.EncodeToString([]byte(workerID + ":" + workerSecret))
	cfg := dockerConfig{
		Auths: map[string]dockerConfigAuth{
			host: {
				Username: workerID,
				Password: workerSecret,
				Auth:     auth,
			},
		},
	}
	dockerConfigJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Docker config JSON: %w", err)
	}

	secret := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       k8sKindSecret,
			"metadata": map[string]interface{}{
				"name":      secretName,
				"namespace": namespace,
				"labels": map[string]interface{}{
					k8skit.LabelManagedBy: labelFluxManagedByValue,
				},
			},
			"type": "kubernetes.io/dockerconfigjson",
			"stringData": map[string]interface{}{
				".dockerconfigjson": string(dockerConfigJSON),
			},
		},
	}

	out, err := yaml.Marshal(secret.Object)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal OCI creds Secret to YAML: %w", err)
	}
	return out, nil
}

func parseFluxOCIOptions(payload api.BridgeWorkerPayload) (FluxOCIBridgeOptions, error) {
	var options FluxOCIBridgeOptions
	// Set Prune=true as default; TargetOptions may override it.
	options.Prune = true

	if v, ok := payload.TargetOptions["FluxNamespace"]; ok {
		options.FluxNamespace = v
	}
	if v, ok := payload.TargetOptions["TargetNamespace"]; ok {
		options.TargetNamespace = v
	}
	if v, ok := payload.TargetOptions["Interval"]; ok {
		options.Interval = v
	}
	if v, ok := payload.TargetOptions["OCIRepoURL"]; ok {
		options.OCIRepoURL = v
	}
	if v, ok := payload.TargetOptions["OCIHost"]; ok {
		options.OCIHost = v
	}
	if v, ok := payload.TargetOptions["OCIPath"]; ok {
		options.OCIPath = v
	}
	if v, ok := payload.TargetOptions["TargetRevision"]; ok {
		options.TargetRevision = v
	}
	// TargetOptions values are always strings; bool fields need explicit conversion.
	if v, ok := payload.TargetOptions["Prune"]; ok {
		options.Prune = v != "false"
	}
	if v, ok := payload.TargetOptions["DisableRepoCreds"]; ok {
		options.DisableRepoCreds = v == "true"
	}
	// FIXME: KubeContext should be derived from BridgeHandle, not passed as an option.
	// BridgeHandle identifies the credentials/coordinates of the bridge to use;
	// each target has one BridgeHandle but can have multiple option tuples.
	if v, ok := payload.TargetOptions["KubeContext"]; ok {
		options.KubeContext = v
	}
	if v, ok := payload.TargetOptions["WaitTimeout"]; ok {
		options.WaitTimeout = v
	}

	// Apply defaults
	if options.FluxNamespace == "" {
		options.FluxNamespace = defaultFluxNamespace
	}
	if options.TargetNamespace == "" {
		options.TargetNamespace = defaultDestinationNamespace
	}
	if options.Interval == "" {
		options.Interval = defaultFluxInterval
	}
	if options.OCIPath == "" {
		options.OCIPath = defaultFluxOCIPath
	}
	if options.TargetRevision == "" {
		options.TargetRevision = defaultFluxTargetRevision
	}
	if options.WaitTimeout == "" {
		options.WaitTimeout = kubernetes.LargeWaitTimeout.String()
	}

	return options, nil
}

func (w *FluxOCIWorker) transformToFluxOCI(wctx api.BridgeWorkerContext, payload *api.BridgeWorkerPayload, skipRepoCreds bool) (FluxOCIBridgeOptions, error) {
	options, err := parseFluxOCIOptions(*payload)
	if err != nil {
		return options, err
	}

	// Determine OCI URL and target revision
	ociRepoURL := options.OCIRepoURL
	targetRevision := options.TargetRevision

	// Track the OCI host for creds generation
	var ociHost string

	if ociRepoURL == "" {
		// Auto-construct OCI URL from unit information
		if options.OCIHost != "" {
			ociHost = options.OCIHost
		} else {
			// Infer OCI host from server URL (e.g., "https://hub.confighub.com" → "oci.hub.confighub.com")
			serverURL := wctx.GetServerURL()
			if serverURL == "" {
				return options, errors.New("cannot infer OCI host: server URL is empty and neither OCIRepoURL nor OCIHost is configured")
			}
			builder := ociutils.NewOCIURLBuilderFromAPIHost(serverURL)
			ociHost = builder.Host
		}

		builder := ociutils.NewOCIURLBuilder(ociHost)
		info := ociutils.UnitOCIInfo{
			SpaceSlug: payload.SpaceSlug,
			UnitSlug:  payload.UnitSlug,
		}
		rawURL := builder.UnitURLFromInfo(info)

		// Split URL and reference: oci://host/unit/space/unit:ref -> repoURL without tag, targetRevision=ref
		parsed, parseErr := ociutils.ParseOCIURL(rawURL)
		if parseErr != nil {
			return options, fmt.Errorf("failed to parse auto-constructed OCI URL: %w", parseErr)
		}
		// Reconstruct URL without the reference for Flux OCIRepository
		ociRepoURL = fmt.Sprintf("%s%s/%s/%s/%s", ociURLScheme, parsed.Host, parsed.ResourceType, parsed.SpaceSlug, parsed.ResourceSlug)
		targetRevision = parsed.Reference
	} else {
		// Extract host from the provided OCIRepoURL
		rawURL := strings.TrimPrefix(ociRepoURL, ociURLScheme)
		if parsed, parseErr := url.Parse(ociURLScheme + rawURL); parseErr == nil {
			ociHost = parsed.Host
		}
	}

	// Generate a stable name based on SpaceSlug and UnitSlug
	appName := k8skit.K8sNormalizeName(fmt.Sprintf("%s-%s", payload.SpaceSlug, payload.UnitSlug))

	// Probe OCI protocol to detect insecure (HTTP-only) registries
	var isHTTP bool
	if ociHost != "" {
		isHTTP = probeOCIProtocol(ociHost)
	}

	// Detect Helm units and extract Helm metadata
	isHelm := helmutils.IsHelmChart(payload.UnitLabels)
	var helmReleaseName, helmChartName, helmChartVersion string
	if isHelm {
		metadata := helmutils.ExtractHelmMetadata(payload.UnitLabels, payload.UnitSlug)
		targetRevision = metadata.ChartVersion
		helmReleaseName = metadata.ReleaseName
		helmChartName = metadata.ChartName
		helmChartVersion = metadata.ChartVersion
	}

	args := &fluxOCIArgs{
		Name:             appName,
		FluxNamespace:    options.FluxNamespace,
		UnitSlug:         payload.UnitSlug,
		UnitID:           payload.UnitID.String(),
		SpaceID:          payload.SpaceID.String(),
		RevisionNum:      fmt.Sprintf("%d", payload.RevisionNum),
		OCIRepoURL:       ociRepoURL,
		OCIPath:          options.OCIPath,
		TargetRevision:   targetRevision,
		TargetNamespace:  options.TargetNamespace,
		Interval:         options.Interval,
		Prune:            options.Prune,
		Insecure:         isHTTP,
		ConfigHubURL:     configHubURLWithDefault(wctx.GetServerURL()),
		IsHelm:           isHelm,
		HelmReleaseName:  helmReleaseName,
		HelmChartName:    helmChartName,
		HelmChartVersion: helmChartVersion,
	}

	// Generate the primary CR pair: Helm path uses HelmRepository+HelmRelease,
	// non-Helm path uses OCIRepository+Kustomization.
	var primaryRepoYAML, secondaryCRYAML []byte
	var primaryRepoGeneratorWithSecret func(*fluxOCIArgs) ([]byte, error)

	if isHelm {
		primaryRepoYAML, err = generateFluxHelmRepository(args)
		if err != nil {
			return options, fmt.Errorf("failed to generate Flux HelmRepository: %w", err)
		}
		secondaryCRYAML, err = generateFluxHelmRelease(args)
		if err != nil {
			return options, fmt.Errorf("failed to generate Flux HelmRelease: %w", err)
		}
		primaryRepoGeneratorWithSecret = generateFluxHelmRepository
		log.Log.Info("Generated Flux Helm CRs", "name", appName, "namespace", options.FluxNamespace, "ociRepoURL", ociRepoURL, "chartVersion", helmChartVersion)
	} else {
		primaryRepoYAML, err = generateFluxOCIRepository(args)
		if err != nil {
			return options, fmt.Errorf("failed to generate Flux OCIRepository: %w", err)
		}
		secondaryCRYAML, err = generateFluxKustomization(args)
		if err != nil {
			return options, fmt.Errorf("failed to generate Flux Kustomization: %w", err)
		}
		primaryRepoGeneratorWithSecret = generateFluxOCIRepository
		log.Log.Info("Generated Flux CRs", "name", appName, "namespace", options.FluxNamespace, "ociRepoURL", ociRepoURL, "targetRevision", targetRevision)
	}

	// Generate OCI creds Secret if credentials are available and not disabled
	if !skipRepoCreds && !options.DisableRepoCreds && w.workerID != "" && w.workerSecret != "" && ociHost != "" {
		credsYAML, credsErr := generateFluxOCICreds(ociHost, options.FluxNamespace, w.workerID, w.workerSecret)
		if credsErr != nil {
			log.Log.Error(credsErr, "Failed to generate OCI creds Secret, proceeding without it")
		} else {
			// Set SecretName on args and re-generate the primary repo CR with the secretRef included
			normalizedHost := k8skit.K8sNormalizeName(ociHost)
			args.SecretName = fluxOCICredsSecretPrefix + normalizedHost
			primaryRepoYAML, err = primaryRepoGeneratorWithSecret(args)
			if err != nil {
				return options, fmt.Errorf("failed to generate Flux primary repo CR with secretRef: %w", err)
			}
			log.Log.Info("Generated Flux OCI creds Secret", "host", ociHost)
			// Combine as multi-doc YAML: Secret first, then primary repo CR, then secondary CR
			payload.Data = append(credsYAML, []byte("---\n")...)
			payload.Data = append(payload.Data, primaryRepoYAML...)
			payload.Data = append(payload.Data, []byte("---\n")...)
			payload.Data = append(payload.Data, secondaryCRYAML...)
			return options, nil
		}
	}

	// No creds: just primary repo CR + secondary CR
	payload.Data = primaryRepoYAML
	payload.Data = append(payload.Data, []byte("---\n")...)
	payload.Data = append(payload.Data, secondaryCRYAML...)
	return options, nil
}

func (w *FluxOCIWorker) Info(options api.InfoOptions) api.BridgeWorkerInfo {
	info := w.KubernetesBridgeWorker.InfoForToolchainAndProvider(options, workerapi.ToolchainKubernetesYAML, api.ProviderFluxOCI)
	for i := range info.SupportedConfigTypes {
		info.SupportedConfigTypes[i].Options = append(info.SupportedConfigTypes[i].Options,
			api.BridgeOption{
				Name:        "FluxNamespace",
				Description: "Namespace where Flux CRs (OCIRepository, Kustomization, HelmRepository, HelmRelease) will be created. Defaults to \"flux-system\".",
				Required:    false,
				DataType:    funcApi.DataTypeString,
				Example:     "flux-system",
			},
			api.BridgeOption{
				Name:        "TargetNamespace",
				Description: "Target namespace for resources deployed by Flux. Defaults to \"default\".",
				Required:    false,
				DataType:    funcApi.DataTypeString,
				Example:     "default",
			},
			api.BridgeOption{
				Name:        "Interval",
				Description: "Flux reconcile interval (e.g. \"5m\", \"10m\"). Defaults to \"10m\".",
				Required:    false,
				DataType:    funcApi.DataTypeString,
				Example:     "10m",
			},
			api.BridgeOption{
				Name:        "OCIRepoURL",
				Description: "Full OCI registry URL (e.g. \"oci://ghcr.io/my-org/my-repo\"). If empty, auto-constructed from the unit's OCI host and space/unit slugs.",
				Required:    false,
				DataType:    funcApi.DataTypeString,
				Example:     "oci://ghcr.io/my-org/my-repo",
			},
			api.BridgeOption{
				Name:        "OCIHost",
				Description: "OCI registry host (e.g. \"ghcr.io\"). Optional; inferred from server URL when neither OCIRepoURL nor OCIHost is set.",
				Required:    false,
				DataType:    funcApi.DataTypeString,
				Example:     "ghcr.io",
			},
			api.BridgeOption{
				Name:        "OCIPath",
				Description: "Path within the OCI artifact where manifests reside. Only used for non-Helm (Kustomization) units. Defaults to \".\".",
				Required:    false,
				DataType:    funcApi.DataTypeString,
				Example:     ".",
			},
			api.BridgeOption{
				Name:        "TargetRevision",
				Description: "OCI tag or digest to deploy. Defaults to \"latest\".",
				Required:    false,
				DataType:    funcApi.DataTypeString,
				Example:     "latest",
			},
			api.BridgeOption{
				Name:        "Prune",
				Description: "Enable pruning of orphaned Kubernetes resources managed by Flux. Defaults to true.",
				Required:    false,
				DataType:    funcApi.DataTypeBool,
				Example:     "true",
			},
			api.BridgeOption{
				Name:        "DisableRepoCreds",
				Description: "When true, skip auto-generation of the OCI credentials Secret. Defaults to false.",
				Required:    false,
				DataType:    funcApi.DataTypeBool,
				Example:     "false",
			},
			api.BridgeOption{
				Name:        "KubeContext",
				Description: "Kubernetes context name to use. Defaults to the current context.",
				Required:    false,
				DataType:    funcApi.DataTypeString,
				Example:     "my-cluster",
			},
			api.BridgeOption{
				Name:        "WaitTimeout",
				Description: "Maximum duration to wait for Flux to reconcile (e.g. \"10m\", \"30m\"). Defaults to the worker large wait timeout.",
				Required:    false,
				DataType:    funcApi.DataTypeString,
				Example:     "10m",
			},
		)
	}
	return info
}

// findFluxKustomizationObject returns the first Kustomization object from a list of parsed objects.
func findFluxKustomizationObject(objects []*unstructured.Unstructured) *unstructured.Unstructured {
	for _, obj := range objects {
		if obj.GetKind() == fluxKindKustomization {
			return obj
		}
	}
	return nil
}

// findFluxOCIRepositoryObject returns the first OCIRepository object from a list of parsed objects.
func findFluxOCIRepositoryObject(objects []*unstructured.Unstructured) *unstructured.Unstructured {
	for _, obj := range objects {
		if obj.GetKind() == fluxKindOCIRepository {
			return obj
		}
	}
	return nil
}

func (w *FluxOCIWorker) Apply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	if payload.DryRun {
		return fmt.Errorf("dry run is not supported for Flux apply")
	}
	if _, err := w.transformToFluxOCI(wctx, &payload, false); err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			err.Error(),
		), err)
	}
	return w.KubernetesBridgeWorker.Apply(wctx, payload)
}

func (w *FluxOCIWorker) WatchForApply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	// Save original data before transform overwrites payload.Data
	originalData := payload.Data

	options, err := w.transformToFluxOCI(wctx, &payload, false)
	if err != nil {
		return backoff.Permanent(lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			err.Error(),
		), err))
	}

	// Parse the generated CRs from the transformed payload
	objects, err := kubernetes.ParseObjects(payload.Data)
	if err != nil {
		return backoff.Permanent(lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			err.Error(),
		), err))
	}

	// Branch on Helm vs Kustomization path
	if hrObj := findFluxHelmReleaseObject(objects); hrObj != nil {
		helmRepoObj := findFluxHelmRepositoryObject(objects)
		return w.watchFluxHelmRelease(wctx, payload, options, hrObj, helmRepoObj, originalData)
	}

	ksObj := findFluxKustomizationObject(objects)
	if ksObj == nil {
		return backoff.Permanent(lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			"no Kustomization CR found in transformed payload",
		), errors.New("no Kustomization CR found in transformed payload")))
	}
	ksName := ksObj.GetName()
	ksNamespace := ksObj.GetNamespace()
	if ksNamespace == "" {
		ksNamespace = options.FluxNamespace
	}

	// Create a Kubernetes client to poll the Kustomization CR
	k8sClient, _, err := kubernetes.KubernetesClientFactory(options.KubeContext)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			fmt.Sprintf("failed to create Kubernetes client: %v", err),
		), err)
	}

	if err := wctx.SendStatus(common.NewActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Waiting for Flux Kustomization to reconcile...",
	)); err != nil {
		return err
	}

	// Parse timeout
	var timeout time.Duration
	if options.WaitTimeout != "" {
		if t, parseErr := time.ParseDuration(options.WaitTimeout); parseErr == nil {
			timeout = t
		}
	}
	if timeout == 0 {
		timeout = kubernetes.LargeWaitTimeout
	}

	ctx := wctx.Context()
	pollInterval := defaultPollInterval
	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			log.Log.Info("Flux WatchForApply cancelled")
			return nil
		default:
		}

		if time.Since(startTime) > timeout {
			return lib.SafeSendStatus(wctx, common.NewActionResult(
				api.ActionStatusFailed,
				api.ActionResultApplyWaitFailed,
				fmt.Sprintf("timed out waiting for Flux Kustomization %s/%s to reconcile", ksNamespace, ksName),
			), context.DeadlineExceeded)
		}

		// Fetch the live Kustomization CR from the cluster
		liveKs := &unstructured.Unstructured{}
		liveKs.SetGroupVersionKind(ksObj.GroupVersionKind())
		if err := k8sClient.Get(ctx, client.ObjectKey{
			Namespace: ksNamespace,
			Name:      ksName,
		}, liveKs); err != nil {
			log.Log.Error(err, "Failed to get Flux Kustomization, will retry", "name", ksName, "namespace", ksNamespace)
			time.Sleep(pollInterval)
			continue
		}

		isReady, isFailed, condMsg := getFluxCondition(liveKs)
		resourceStatuses := buildFluxResourceStatusMap(liveKs)

		log.Log.Info("Flux Kustomization status",
			"name", ksName,
			"isReady", isReady,
			"isFailed", isFailed,
			"message", condMsg,
		)

		progressStatus := common.NewActionResult(
			api.ActionStatusProgressing,
			api.ActionResultNone,
			fmt.Sprintf("Flux Kustomization %s: ready=%v, message=%s", ksName, isReady, condMsg),
		)
		progressStatus.ResourceStatuses = resourceStatuses
		if err := wctx.SendStatus(progressStatus); err != nil {
			return err
		}

		if isFailed {
			return lib.SafeSendStatus(wctx, common.NewActionResult(
				api.ActionStatusFailed,
				api.ActionResultApplyWaitFailed,
				fmt.Sprintf("Flux Kustomization reconciliation failed for %s/%s: %s", ksNamespace, ksName, condMsg),
			), fmt.Errorf("Flux Kustomization reconciliation failed: %s", condMsg))
		}

		if isReady {
			liveStateYAML, liveDataYAML, liveErr := computeManagedResourceState(ctx, k8sClient, liveKs, originalData)
			if liveErr != nil {
				log.Log.Error(liveErr, "Failed to fetch managed resources")
			}

			// Check if operation was cancelled/overridden while waiting
			select {
			case <-wctx.Context().Done():
				log.Log.Info("Flux apply operation was cancelled/overridden, skipping completion status")
				return nil
			default:
			}

			status := common.NewActionResult(
				api.ActionStatusCompleted,
				api.ActionResultApplyCompleted,
				fmt.Sprintf("Flux Kustomization %s reconciled successfully at %s", ksName, time.Now().Format(time.RFC3339)),
			)
			status.ResourceStatuses = resourceStatuses
			status.LiveState = []byte(liveStateYAML)
			status.LiveData = []byte(liveDataYAML)

			// BridgeState = inventory ConfigMap tracking the Flux Kustomization and OCIRepository CRs
			appliedObjects, _ := kubernetes.ParseObjects(payload.Data)
			status.BridgeState = w.BuildBridgeState(payload, appliedObjects)

			_ = wctx.SendStatus(status)
			return nil
		}

		time.Sleep(pollInterval)
	}
}

func (w *FluxOCIWorker) Refresh(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	// Save original data before transform overwrites payload.Data
	originalData := payload.Data

	// Parse RefreshParams for BaseRevisionData
	var refreshParams *api.RefreshParams
	if len(payload.ExtraParams) > 0 {
		refreshParams = new(api.RefreshParams)
		if err := json.Unmarshal(payload.ExtraParams, refreshParams); err != nil {
			refreshParams = nil
		}
	}

	// Transform payload to Flux CRs (same as Apply)
	options, err := w.transformToFluxOCI(wctx, &payload, false)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			err.Error(),
		), err)
	}

	// Parse the generated CRs
	objects, err := kubernetes.ParseObjects(payload.Data)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("failed to parse Flux CRs: %v", err),
		), err)
	}

	// Branch on Helm vs Kustomization path
	if expectedHR := findFluxHelmReleaseObject(objects); expectedHR != nil {
		expectedHelmRepo := findFluxHelmRepositoryObject(objects)
		return w.refreshFluxHelmRelease(wctx, payload, options, expectedHR, expectedHelmRepo, originalData, refreshParams)
	}

	expectedKs := findFluxKustomizationObject(objects)
	if expectedKs == nil {
		noObjErr := errors.New("no Kustomization CR found in transformed payload")
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			noObjErr.Error(),
		), noObjErr)
	}
	// Create Kubernetes client
	k8sClient, _, err := kubernetes.KubernetesClientFactory(options.KubeContext)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("failed to create Kubernetes client: %v", err),
		), err)
	}

	ksNamespace := expectedKs.GetNamespace()
	if ksNamespace == "" {
		ksNamespace = options.FluxNamespace
	}
	ksName := expectedKs.GetName()

	if err := wctx.SendStatus(common.NewActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Retrieving Flux Kustomization state...",
	)); err != nil {
		return err
	}

	// Fetch the live Kustomization CR from the cluster
	liveKs := &unstructured.Unstructured{}
	liveKs.SetGroupVersionKind(expectedKs.GroupVersionKind())
	if err := k8sClient.Get(wctx.Context(), client.ObjectKey{
		Namespace: ksNamespace,
		Name:      ksName,
	}, liveKs); err != nil {
		if apierrors.IsNotFound(err) {
			return wctx.SendStatus(common.NewActionResult(
				api.ActionStatusCompleted,
				api.ActionResultRefreshAndDrifted,
				fmt.Sprintf("Flux Kustomization %s/%s not found - drift detected", ksNamespace, ksName),
			))
		}
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("failed to get Kustomization CR: %v", err),
		), err)
	}

	// Extract Flux Kustomization status
	isReady, _, condMsg := getFluxCondition(liveKs)
	resourceStatuses := buildFluxResourceStatusMap(liveKs)

	log.Log.Info("Flux Kustomization refresh",
		"name", ksName,
		"isReady", isReady,
		"message", condMsg,
	)

	// --- Managed resource discovery from Flux Kustomization .status.inventory ---
	syncDrifted := !isReady
	contentDrifted := false
	var patchedData []byte

	liveStateYAML, liveDataYAML, liveErr := computeManagedResourceState(wctx.Context(), k8sClient, liveKs, originalData)
	if liveErr != nil {
		log.Log.Error(liveErr, "Failed to fetch managed resources")
	}

	if liveDataYAML != "" {
		// Content drift detection via diff-patch.
		//
		// "Base data" is the last-known-good revision — the YAML that was last
		// successfully applied to the cluster. We compare it against LiveData
		// (what's actually running now) to detect whether someone changed the
		// cluster state outside of ConfigHub. If BaseRevisionData is not
		// available (e.g. first refresh after import), we fall back to the
		// current revision's rendered data (originalData).
		var baseData []byte
		if refreshParams != nil && len(refreshParams.BaseRevisionData) > 0 {
			baseData = refreshParams.BaseRevisionData
		} else {
			baseData = originalData
		}

		cleanedBaseData, cleanErr := kubernetes.CleanBaseDataForDrift(baseData)
		if cleanErr != nil {
			log.Log.Error(cleanErr, "Failed to clean base data for drift comparison")
			cleanedBaseData = baseData
		}

		patched, drifted, diffErr := yamlkit.DiffPatchWithOptions(cleanedBaseData, []byte(liveDataYAML), originalData, w.GetResourceProvider(), false, nil)
		if diffErr != nil {
			log.Log.Error(diffErr, "Failed to diff-patch managed resources")
		} else {
			contentDrifted = drifted
			if drifted {
				patchedData = patched
			}
		}
	}

	isDrifted := syncDrifted || contentDrifted

	var resultType api.ActionResultType
	var message string
	if isDrifted {
		resultType = api.ActionResultRefreshAndDrifted
		message = fmt.Sprintf("Flux Kustomization %s: ready=%v - drift detected", ksName, isReady)
	} else {
		resultType = api.ActionResultRefreshAndNoDrift
		message = fmt.Sprintf("Flux Kustomization %s: ready=%v - no drift", ksName, isReady)
	}

	result := common.NewActionResult(api.ActionStatusCompleted, resultType, message)
	result.LiveData = []byte(liveDataYAML)
	result.LiveState = []byte(liveStateYAML)
	result.ResourceStatuses = resourceStatuses
	if contentDrifted && len(patchedData) > 0 {
		result.Data = patchedData
	}

	// BridgeState = inventory ConfigMap tracking the Flux Kustomization and OCIRepository CRs
	appliedObjects, _ := kubernetes.ParseObjects(payload.Data)
	result.BridgeState = w.BuildBridgeState(payload, appliedObjects)

	return wctx.SendStatus(result)
}

func (w *FluxOCIWorker) Import(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	return lib.SafeSendStatus(wctx, common.NewActionResult(
		api.ActionStatusFailed,
		api.ActionResultImportFailed,
		"Import not supported for Flux OCI bridge",
	), errors.New("Import not supported for Flux OCI bridge"))
}

func (w *FluxOCIWorker) Destroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	if payload.DryRun {
		return fmt.Errorf("dry run is not supported for Flux destroy")
	}
	if _, err := w.transformToFluxOCI(wctx, &payload, true); err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			err.Error(),
		), err)
	}
	// Filter out Secret objects — OCI creds are shared infrastructure and should not be deleted per-unit
	payload.Data = filterOutSecrets(payload.Data)
	return w.KubernetesBridgeWorker.Destroy(wctx, payload)
}

func (w *FluxOCIWorker) WatchForDestroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	if _, err := w.transformToFluxOCI(wctx, &payload, true); err != nil {
		return backoff.Permanent(lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyWaitFailed,
			err.Error(),
		), err))
	}
	// Filter out Secret objects — OCI creds are shared infrastructure and should not be deleted per-unit
	payload.Data = filterOutSecrets(payload.Data)
	return w.KubernetesBridgeWorker.WatchForDestroy(wctx, payload)
}

// collectFluxLiveCRs fetches the live OCIRepository (if expected) and returns it together
// with the live Kustomization as a single slice for LiveState/LiveData assembly.
func collectFluxLiveCRs(ctx context.Context, k8sClient kubernetes.KubernetesClient, liveKs *unstructured.Unstructured, expectedOCIRepo *unstructured.Unstructured, fluxNamespace string) []*unstructured.Unstructured {
	allLiveCRs := []*unstructured.Unstructured{liveKs}
	if expectedOCIRepo != nil {
		liveOCIRepo := &unstructured.Unstructured{}
		liveOCIRepo.SetGroupVersionKind(expectedOCIRepo.GroupVersionKind())
		ociRepoNs := expectedOCIRepo.GetNamespace()
		if ociRepoNs == "" {
			ociRepoNs = fluxNamespace
		}
		if fetchErr := k8sClient.Get(ctx, client.ObjectKey{
			Namespace: ociRepoNs,
			Name:      expectedOCIRepo.GetName(),
		}, liveOCIRepo); fetchErr != nil {
			log.Log.Error(fetchErr, "Failed to fetch live OCIRepository, continuing with Kustomization only")
		} else {
			allLiveCRs = append(allLiveCRs, liveOCIRepo)
		}
	}
	return allLiveCRs
}

// Flux status helpers

// getFluxCondition extracts the Ready condition from a Flux object.
// Returns (isReady, isFailed, message).
func getFluxCondition(obj *unstructured.Unstructured) (bool, bool, string) {
	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found {
		return false, false, ""
	}

	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _ := cond["type"].(string)
		if condType != fluxConditionReady {
			continue
		}
		status, _ := cond["status"].(string)
		message, _ := cond["message"].(string)
		switch status {
		case fluxConditionStatusTrue:
			return true, false, message
		case fluxConditionStatusFalse:
			return false, true, message
		default:
			return false, false, message
		}
	}
	return false, false, ""
}

// extractFluxInventoryObjects extracts managed resource objects from Flux Kustomization .status.inventory.entries[].
// Each returned object has GVK, namespace, and name set — suitable for passing to GetLiveObjects.
// Uses fluxcd/cli-utils ParseObjMetadata to parse the inventory entry ID format.
func extractFluxInventoryObjects(ks *unstructured.Unstructured) []*unstructured.Unstructured {
	entries, found, err := unstructured.NestedSlice(ks.Object, "status", "inventory", "entries")
	if err != nil || !found || len(entries) == 0 {
		return nil
	}

	var result []*unstructured.Unstructured
	for _, e := range entries {
		entry, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := entry["id"].(string)
		if id == "" {
			continue
		}
		version, _ := entry["v"].(string)
		if version == "" {
			continue
		}

		objMeta, err := object.ParseObjMetadata(id)
		if err != nil {
			log.Log.Error(err, "Failed to parse Flux inventory entry", "id", id)
			continue
		}

		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   objMeta.GroupKind.Group,
			Version: version,
			Kind:    objMeta.GroupKind.Kind,
		})
		obj.SetName(objMeta.Name)
		obj.SetNamespace(objMeta.Namespace)
		result = append(result, obj)
	}
	return result
}

// computeManagedResourceState fetches managed resources from a Flux CR's inventory
// and returns LiveState (full objects) and LiveData (cleaned, spec-only).
// LiveData is always derived from LiveState as its single source of truth.
// originalData is the unit's original data (before transform); it is used to determine
// which resources had an explicit namespace set — resources without a namespace in the
// original data have their namespace cleared in LiveData to avoid false drift.
func computeManagedResourceState(ctx context.Context, k8sClient kubernetes.KubernetesClient, fluxCR *unstructured.Unstructured, originalData []byte) (liveStateYAML, liveDataYAML string, err error) {
	managedObjs := extractFluxInventoryObjects(fluxCR)
	if len(managedObjs) == 0 {
		return "", "", nil
	}

	liveManagedResources, liveErr := kubernetes.GetLiveObjects(ctx, k8sClient, managedObjs, false, true)
	if liveErr != nil {
		return "", "", liveErr
	}
	if len(liveManagedResources) == 0 {
		return "", "", nil
	}

	// LiveState = full managed resources (with status, metadata, etc.)
	liveStateYAML, liveStateErr := kubernetes.ObjectsToYAML(liveManagedResources)
	if liveStateErr != nil {
		log.Log.Error(liveStateErr, "Failed to serialize LiveState objects to YAML")
	}

	// LiveData = cleaned version of the same objects (spec-only, no internal annotations)
	cleanedManagedResources := kubernetes.ExtraCleanupObjects(liveManagedResources)

	// Namespace removal rule for LiveData:
	//
	// Flux applies the targetNamespace from the Kustomization CR at reconcile time,
	// but the unit's original data (rendered manifests) may not include a namespace.
	// If we keep the namespace in LiveData, it would appear as a diff against the
	// original data, causing false drift on every Refresh.
	//
	// Rule: remove the namespace from a LiveData resource ONLY IF the original data
	// did not explicitly set a namespace on that resource. If the user explicitly set
	// a namespace in the unit data, we preserve it in LiveData so that any divergence
	// from the intended namespace IS detected as drift.
	originalNamespaces := kubernetes.BuildOriginalNamespaceMap(originalData)
	for _, obj := range cleanedManagedResources {
		if _, hasNS := originalNamespaces[kubernetes.OriginalNamespaceKey(obj)]; !hasNS {
			obj.SetNamespace("")
		}
	}

	liveDataYAML, liveDataErr := kubernetes.ObjectsToYAML(cleanedManagedResources)
	if liveDataErr != nil {
		log.Log.Error(liveDataErr, "Failed to serialize LiveData objects to YAML")
	}

	return liveStateYAML, liveDataYAML, nil
}

// buildFluxResourceStatusMap builds a ResourceStatusMap from Flux Kustomization status.
// It reports the Kustomization itself plus all inventory entries as managed resources.
// Since Flux doesn't expose per-resource health/sync, status is derived from the
// Kustomization's overall Ready condition.
func buildFluxResourceStatusMap(ks *unstructured.Unstructured) api.ResourceStatusMap {
	isReady, isFailed, _ := getFluxCondition(ks)

	statusMap := make(api.ResourceStatusMap)
	now := time.Now()

	var syncStatus api.ResourceSyncStatusType
	var readiness api.ResourceReadinessType
	if isReady {
		syncStatus = api.ResourceSyncStatusSynced
		readiness = api.ResourceReadinessReady
	} else if isFailed {
		syncStatus = api.ResourceSyncStatusPending
		readiness = api.ResourceReadinessFailed
	} else {
		syncStatus = api.ResourceSyncStatusPending
		readiness = api.ResourceReadinessInProgress
	}

	// Report the Kustomization CR itself
	ksKey := buildResourceKey("kustomize.toolkit.fluxcd.io", "v1", fluxKindKustomization, ks.GetNamespace(), ks.GetName())
	statusMap[ksKey] = api.ResourceStatus{
		SyncStatus: syncStatus,
		Readiness:  readiness,
		UpdatedAt:  now,
	}

	// Report each managed resource from inventory
	for _, obj := range extractFluxInventoryObjects(ks) {
		gvk := obj.GroupVersionKind()
		key := buildResourceKey(gvk.Group, gvk.Version, gvk.Kind, obj.GetNamespace(), obj.GetName())
		statusMap[key] = api.ResourceStatus{
			SyncStatus: syncStatus,
			Readiness:  readiness,
			UpdatedAt:  now,
		}
	}

	return statusMap
}

// filterOutSecrets removes Secret objects from multi-doc YAML data.
func filterOutSecrets(data []byte) []byte {
	objects, err := kubernetes.ParseObjects(data)
	if err != nil {
		return data // fallback to original data on parse error
	}
	var filtered []*unstructured.Unstructured
	for _, obj := range objects {
		if obj.GetKind() != "Secret" {
			filtered = append(filtered, obj)
		}
	}
	if len(filtered) == len(objects) {
		return data // no secrets found, return original
	}
	result, err := kubernetes.ObjectsToYAML(filtered)
	if err != nil {
		return data // fallback to original data on marshal error
	}
	return []byte(result)
}

// probeOCIProtocol probes whether the given host uses HTTP (true) or HTTPS (false) for OCI.
func probeOCIProtocol(host string) bool {
	httpClient := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := httpClient.Head(fmt.Sprintf("https://%s/v2/", host))
	if err != nil {
		return true // Connection failed → HTTP
	}
	resp.Body.Close()
	return false // HTTPS works
}

// configHubURLWithDefault returns the given URL if non-empty, otherwise defaultConfigHubURL.
func configHubURLWithDefault(u string) string {
	if u == "" {
		return defaultConfigHubURL
	}
	return u
}

// buildResourceKey constructs a ResourceTypeAndName key from GVK components.
func buildResourceKey(group, version, kind, namespace, name string) funcApi.ResourceTypeAndName {
	var resourceType string
	if group != "" {
		resourceType = fmt.Sprintf("%s/%s/%s", group, version, kind)
	} else {
		resourceType = fmt.Sprintf("%s/%s", version, kind)
	}
	return funcApi.ResourceTypeAndName(fmt.Sprintf("%s#%s/%s", resourceType, namespace, name))
}
