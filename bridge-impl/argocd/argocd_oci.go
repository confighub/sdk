// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package argocd

import (
	"context"
	"crypto/tls"
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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

// ArgoCDOCIWorker transforms Kubernetes manifests into ArgoCD Application CRDs
// using OCI registry as the source, then applies them using the parent's cliutils applier.
type ArgoCDOCIWorker struct {
	kubernetes.KubernetesBridgeWorker
	workerID     string
	workerSecret string
}

// NewArgoCDOCIWorker creates a new ArgoCDOCIWorker with properly initialized
// embedded KubernetesBridgeWorker (including applierType).
// workerID and workerSecret are used to auto-generate ArgoCD repo-creds Secrets.
func NewArgoCDOCIWorker(workerID, workerSecret string) *ArgoCDOCIWorker {
	return &ArgoCDOCIWorker{
		KubernetesBridgeWorker: *kubernetes.NewKubernetesBridgeWorker(),
		workerID:               workerID,
		workerSecret:           workerSecret,
	}
}

var _ api.BridgeWorker = (*ArgoCDOCIWorker)(nil)
var _ api.WatchableWorker = (*ArgoCDOCIWorker)(nil)

// argoCDWaitTimeout is the timeout for waiting on ArgoCD Application sync.
// Defaults to LargeWaitTimeout; overridable in tests.
var argoCDWaitTimeout = kubernetes.LargeWaitTimeout

func (w *ArgoCDOCIWorker) ID() api.BridgeWorkerID {
	return api.BridgeWorkerID{
		ProviderType:   api.ProviderArgoCDOCI,
		ToolchainTypes: []workerapi.ToolchainType{workerapi.ToolchainKubernetesYAML},
	}
}

// ArgoCDOCIBridgeOptions contains the configuration parameters for the ArgoCD OCI bridge worker.
type ArgoCDOCIBridgeOptions struct {
	KubeContext          string `json:",omitempty"`
	ArgoCDNamespace      string `json:",omitempty"` // Namespace where ArgoCD Application will be created (default: "argocd")
	DestinationServer    string `json:",omitempty"` // Target cluster API server URL (default: "https://kubernetes.default.svc")
	DestinationNamespace string `json:",omitempty"` // Target namespace for deployed resources (default: "default")
	Project              string `json:",omitempty"` // ArgoCD project name (default: "default")
	SyncPolicy           string `json:",omitempty"` // "automated" or "manual" (default: "manual")
	PruneEnabled         bool   `json:",omitempty"` // Enable pruning of orphaned resources
	SelfHealEnabled      bool   `json:",omitempty"` // Enable self-healing (auto-sync on drift)
	OCIRepoURL           string `json:",omitempty"` // OCI registry URL - if empty, auto-constructed from OCIHost and unit info
	OCIHost              string `json:",omitempty"` // OCI registry host - optional, inferred from server URL if not set
	OCIPath              string `json:",omitempty"` // Path within OCI artifact (default: ".")
	TargetRevision       string `json:",omitempty"` // OCI tag or digest (default: "latest")
	DisableRepoCreds     bool   `json:",omitempty"` // Skip auto-generation of ArgoCD repo-creds Secret (default: false)
}

// Default values for ArgoCD OCI worker parameters
const (
	defaultArgoCDNamespace      = "argocd"
	defaultDestinationServer    = "https://kubernetes.default.svc"
	defaultDestinationNamespace = "default"
	defaultProject              = "default"
	defaultSyncPolicy           = "manual"
	defaultOCIPath              = "."
	defaultTargetRevision       = "latest"
)

// ArgoCD Application sync status values (from .status.sync.status)
const (
	argoCDSyncStatusSynced    = "Synced"
	argoCDSyncStatusOutOfSync = "OutOfSync"
	argoCDSyncStatusUnknown   = "Unknown"
)

// ArgoCD Application health status values (from .status.health.status)
const (
	argoCDHealthStatusHealthy     = "Healthy"
	argoCDHealthStatusProgressing = "Progressing"
	argoCDHealthStatusDegraded    = "Degraded"
	argoCDHealthStatusSuspended   = "Suspended"
	argoCDHealthStatusMissing     = "Missing"
	argoCDHealthStatusUnknown     = "Unknown"
)

// ArgoCD operation phase values (from .status.operationState.phase)
const (
	argoCDOperationPhaseSucceeded = "Succeeded"
	argoCDOperationPhaseFailed    = "Failed"
	argoCDOperationPhaseError     = "Error"
	argoCDOperationPhaseRunning   = "Running"
)

// ArgoCD Kubernetes resource identifiers
const (
	argoCDAPIVersion          = "argoproj.io/v1alpha1"
	argoCDKindApplication     = "Application"
	argoCDFinalizer           = "resources-finalizer.argocd.argoproj.io"
	argoCDSyncPolicyAutomated = "automated"
	argoCDSyncOptionCreateNS  = "CreateNamespace=true"
	argoCDSyncOptionSSA       = "ServerSideApply=true"
	k8sKindSecret             = "Secret"
)

// Annotation and label keys
const (
	annotationExternalLink     = "link.argocd.argoproj.io/external-link"
	labelArgoCDSecretType      = "argocd.argoproj.io/secret-type"
	labelArgoCDSecretTypeValue = "repo-creds"
	labelManagedByValue        = "argocd-oci-bridge"
)

// Shared bridge constants
const (
	defaultPollInterval = 5 * time.Second
)

// OCI registry constants
const (
	ociURLScheme           = "oci://"
	ociCredsSecretPrefix   = "confighub-oci-creds-"
	ociSecretType          = "oci"
	ociFieldInsecureHTTP   = "insecureOCIForceHttp"
	ociFieldForceBasicAuth = "forceHttpBasicAuth"
)

// ConfigHub URL path format for external-link annotation
const configHubUnitURLFormat = "%s/units/%s/%s"

// buildResourceKey constructs a resource key in the format "group/version/kind#namespace/name"
// used by funcApi.ResourceTypeAndName.
func buildResourceKey(group, version, kind, namespace, name string) funcApi.ResourceTypeAndName {
	var resourceType string
	if group != "" {
		resourceType = fmt.Sprintf("%s/%s/%s", group, version, kind)
	} else {
		resourceType = fmt.Sprintf("%s/%s", version, kind)
	}
	return funcApi.ResourceTypeAndName(fmt.Sprintf("%s#%s/%s", resourceType, namespace, name))
}

type argoCDApplicationArgs struct {
	Name                 string
	ArgoCDNamespace      string
	UnitSlug             string
	UnitID               string
	SpaceID              string
	RevisionNum          string
	Project              string
	OCIRepoURL           string
	OCIPath              string
	TargetRevision       string
	DestinationServer    string
	DestinationNamespace string
	SyncPolicy           string
	PruneEnabled         bool
	SelfHealEnabled      bool
	ConfigHubURL         string
	// Helm-specific fields
	IsHelm          bool   // When true, generate Helm-style source (no path, chart version as targetRevision)
	HelmReleaseName string // Helm release name (from HelmRelease label)
	HelmChartName   string // Helm chart name (from HelmChart label)
}

const defaultConfigHubURL = "https://hub.confighub.com"

// configHubURLWithDefault returns the given URL if non-empty, otherwise defaultConfigHubURL.
func configHubURLWithDefault(u string) string {
	if u == "" {
		return defaultConfigHubURL
	}
	return u
}

func generateArgoCDApplication(args *argoCDApplicationArgs) ([]byte, error) {
	// Build source configuration: Helm charts use repoURL+targetRevision+helm.releaseName (no path),
	// while plain manifests use repoURL+path+targetRevision.
	source := map[string]interface{}{
		"repoURL":        args.OCIRepoURL,
		"targetRevision": args.TargetRevision,
	}
	if args.IsHelm {
		// Helm OCI source: set chart name, no path, add helm section with releaseName
		source["chart"] = args.HelmChartName
		helmSection := map[string]interface{}{}
		if args.HelmReleaseName != "" {
			helmSection["releaseName"] = args.HelmReleaseName
		}
		if len(helmSection) > 0 {
			source["helm"] = helmSection
		}
	} else {
		// OCIPath is a user-configurable parameter (default ".") from ArgoCDOCIBridgeOptions,
		// specifying the path within the OCI artifact where manifests reside.
		// Not applicable for Helm charts since ArgoCD consumes the entire chart tar.gz.
		source["path"] = args.OCIPath
	}

	spec := map[string]interface{}{
		"project": args.Project,
		"source":  source,
		"destination": map[string]interface{}{
			"server":    args.DestinationServer,
			"namespace": args.DestinationNamespace,
		},
	}

	spec["syncPolicy"] = map[string]interface{}{
		"syncOptions": []interface{}{argoCDSyncOptionCreateNS, argoCDSyncOptionSSA},
	}
	if args.SyncPolicy == argoCDSyncPolicyAutomated {
		spec["syncPolicy"].(map[string]interface{})["automated"] = map[string]interface{}{
			"prune":    args.PruneEnabled,
			"selfHeal": args.SelfHealEnabled,
		}
	}

	configHubURL := args.ConfigHubURL
	if configHubURL == "" {
		configHubURL = defaultConfigHubURL
	}

	app := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": argoCDAPIVersion,
			"kind":       argoCDKindApplication,
			"metadata": map[string]interface{}{
				"name":      args.Name,
				"namespace": args.ArgoCDNamespace,
				"finalizers": []interface{}{
					argoCDFinalizer,
				},
				"annotations": map[string]interface{}{
					k8skit.UnitSlugAnnotation:    args.UnitSlug,
					k8skit.SpaceIDAnnotation:     args.SpaceID,
					k8skit.RevisionNumAnnotation: args.RevisionNum,
					annotationExternalLink:       fmt.Sprintf(configHubUnitURLFormat, configHubURL, args.SpaceID, args.UnitID),
				},
			},
			"spec": spec,
			"operation": map[string]interface{}{
				"sync": map[string]interface{}{
					"syncOptions": []interface{}{argoCDSyncOptionCreateNS, argoCDSyncOptionSSA},
				},
			},
		},
	}

	out, err := yaml.Marshal(app.Object)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ArgoCD Application to YAML: %w", err)
	}
	return out, nil
}

func parseArgoCDOCIOptions(payload api.BridgeWorkerPayload) (ArgoCDOCIBridgeOptions, error) {
	var options ArgoCDOCIBridgeOptions

	// Prefer BridgeHandle over deprecated TargetOptions["KubeContext"].
	options.KubeContext = payload.BridgeHandle
	if options.KubeContext == "" {
		if v, ok := payload.TargetOptions["KubeContext"]; ok {
			options.KubeContext = v
		}
	}
	// "cluster" is the BridgeHandle for in-cluster targets, but
	// KubernetesConfigFactory expects "" to trigger in-cluster config detection.
	if options.KubeContext == "cluster" {
		options.KubeContext = ""
	}
	if v, ok := payload.TargetOptions["ArgoCDNamespace"]; ok {
		options.ArgoCDNamespace = v
	}
	if v, ok := payload.TargetOptions["DestinationServer"]; ok {
		options.DestinationServer = v
	}
	if v, ok := payload.TargetOptions["DestinationNamespace"]; ok {
		options.DestinationNamespace = v
	}
	if v, ok := payload.TargetOptions["Project"]; ok {
		options.Project = v
	}
	if v, ok := payload.TargetOptions["SyncPolicy"]; ok {
		options.SyncPolicy = v
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
	if v, ok := payload.TargetOptions["PruneEnabled"]; ok {
		options.PruneEnabled = v == "true"
	}
	if v, ok := payload.TargetOptions["SelfHealEnabled"]; ok {
		options.SelfHealEnabled = v == "true"
	}
	if v, ok := payload.TargetOptions["DisableRepoCreds"]; ok {
		options.DisableRepoCreds = v == "true"
	}

	// Apply defaults
	if options.ArgoCDNamespace == "" {
		options.ArgoCDNamespace = defaultArgoCDNamespace
	}
	if options.DestinationServer == "" {
		options.DestinationServer = defaultDestinationServer
	}
	if options.DestinationNamespace == "" {
		options.DestinationNamespace = defaultDestinationNamespace
	}
	if options.Project == "" {
		options.Project = defaultProject
	}
	if options.SyncPolicy == "" {
		options.SyncPolicy = defaultSyncPolicy
	}
	if options.OCIPath == "" {
		options.OCIPath = defaultOCIPath
	}
	if options.TargetRevision == "" {
		options.TargetRevision = defaultTargetRevision
	}

	return options, nil
}

// probeOCIProtocol checks whether the OCI host supports HTTPS.
// Returns true if the host only supports HTTP (i.e., HTTPS probe fails).
func probeOCIProtocol(host string) bool {
	httpClient := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := httpClient.Head(fmt.Sprintf("https://%s/v2/", host))
	if err != nil {
		// Connection failed (refused, TLS error, timeout, etc.) → HTTP
		return true
	}
	resp.Body.Close()
	// Any HTTP response means HTTPS works
	return false
}

// generateArgoCDRepoCreds generates a repo-creds Secret YAML for ArgoCD OCI authentication.
func generateArgoCDRepoCreds(host, namespace, workerID, workerSecret string, isHTTP bool) ([]byte, error) {
	normalizedHost := k8skit.K8sNormalizeName(host)
	secretName := ociCredsSecretPrefix + normalizedHost

	stringData := map[string]interface{}{
		"type":     ociSecretType,
		"url":      ociURLScheme + host,
		"username": workerID,
		"password": workerSecret,
	}
	if isHTTP {
		stringData[ociFieldInsecureHTTP] = "true"
		stringData[ociFieldForceBasicAuth] = "true"
	}

	secret := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       k8sKindSecret,
			"metadata": map[string]interface{}{
				"name":      secretName,
				"namespace": namespace,
				"labels": map[string]interface{}{
					labelArgoCDSecretType: labelArgoCDSecretTypeValue,
					k8skit.LabelManagedBy: labelManagedByValue,
				},
			},
			"type":       "Opaque",
			"stringData": stringData,
		},
	}

	out, err := yaml.Marshal(secret.Object)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal repo-creds Secret to YAML: %w", err)
	}
	return out, nil
}

func (w *ArgoCDOCIWorker) transformToArgoCDOCIApplication(wctx api.BridgeWorkerContext, payload *api.BridgeWorkerPayload, skipRepoCreds bool) (ArgoCDOCIBridgeOptions, error) {
	options, err := parseArgoCDOCIOptions(*payload)
	if err != nil {
		return options, err
	}

	// Determine OCI URL and target revision
	ociRepoURL := options.OCIRepoURL
	targetRevision := options.TargetRevision

	// Track the OCI host for repo-creds generation
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
		ociRepoURL = builder.UnitURLFromInfo(info)

		// When auto-constructing, the reference is embedded in the URL
		// ArgoCD expects repoURL without the tag for OCI sources
		// So we need to split: oci://host/unit/space/unit:ref -> repoURL=oci://host/unit/space/unit, targetRevision=ref
		parsed, parseErr := ociutils.ParseOCIURL(ociRepoURL)
		if parseErr != nil {
			return options, fmt.Errorf("failed to parse auto-constructed OCI URL: %w", parseErr)
		}
		// Reconstruct URL without the reference for ArgoCD
		ociRepoURL = fmt.Sprintf("%s%s/%s/%s/%s", ociURLScheme, parsed.Host, parsed.ResourceType, parsed.SpaceSlug, parsed.ResourceSlug)
		targetRevision = parsed.Reference
	} else {
		// Extract host from the provided OCIRepoURL
		// Strip "oci://" prefix for URL parsing
		rawURL := strings.TrimPrefix(ociRepoURL, ociURLScheme)
		if parsed, parseErr := url.Parse(ociURLScheme + rawURL); parseErr == nil {
			ociHost = parsed.Host
		}
	}

	// Generate a stable name based on SpaceSlug and UnitSlug for in-place updates
	appName := k8skit.K8sNormalizeName(fmt.Sprintf("%s-%s", payload.SpaceSlug, payload.UnitSlug))

	// Detect Helm units by checking for all required Helm labels
	// (HelmRelease, HelmChart, HelmChartVersion, HelmChartAPIVersion).
	// When detected, override targetRevision with the chart version.
	isHelm := helmutils.IsHelmChart(payload.UnitLabels)
	var helmReleaseName string
	var helmChartName string
	if isHelm {
		metadata := helmutils.ExtractHelmMetadata(payload.UnitLabels, payload.UnitSlug)
		targetRevision = metadata.ChartVersion
		helmReleaseName = metadata.ReleaseName
		helmChartName = metadata.ChartName
	}

	args := &argoCDApplicationArgs{
		Name:                 appName,
		ArgoCDNamespace:      options.ArgoCDNamespace,
		UnitSlug:             payload.UnitSlug,
		UnitID:               payload.UnitID.String(),
		SpaceID:              payload.SpaceID.String(),
		RevisionNum:          fmt.Sprintf("%d", payload.RevisionNum),
		Project:              options.Project,
		OCIRepoURL:           ociRepoURL,
		OCIPath:              options.OCIPath,
		TargetRevision:       targetRevision,
		DestinationServer:    options.DestinationServer,
		DestinationNamespace: options.DestinationNamespace,
		SyncPolicy:           options.SyncPolicy,
		PruneEnabled:         options.PruneEnabled,
		SelfHealEnabled:      options.SelfHealEnabled,
		ConfigHubURL:         wctx.GetServerURL(),
		IsHelm:               isHelm,
		HelmReleaseName:      helmReleaseName,
		HelmChartName:        helmChartName,
	}

	applicationYAML, err := generateArgoCDApplication(args)
	if err != nil {
		return options, fmt.Errorf("failed to generate ArgoCD Application: %w", err)
	}

	log.Log.Info("Generated ArgoCD Application", "name", appName, "namespace", options.ArgoCDNamespace, "ociRepoURL", ociRepoURL, "targetRevision", targetRevision, "isHelm", isHelm)

	// Generate repo-creds Secret if credentials are available and not disabled
	if !skipRepoCreds && !options.DisableRepoCreds && w.workerID != "" && w.workerSecret != "" && ociHost != "" {
		isHTTP := probeOCIProtocol(ociHost)
		repoCredsYAML, repoErr := generateArgoCDRepoCreds(ociHost, options.ArgoCDNamespace, w.workerID, w.workerSecret, isHTTP)
		if repoErr != nil {
			log.Log.Error(repoErr, "Failed to generate repo-creds Secret, proceeding without it")
		} else {
			log.Log.Info("Generated ArgoCD repo-creds Secret", "host", ociHost, "isHTTP", isHTTP)
			// Combine as multi-doc YAML: Secret first, then Application
			payload.Data = append(repoCredsYAML, []byte("---\n")...)
			payload.Data = append(payload.Data, applicationYAML...)
			return options, nil
		}
	}

	payload.Data = applicationYAML
	return options, nil
}

func (w *ArgoCDOCIWorker) Info(options api.InfoOptions) api.BridgeWorkerInfo {
	info := w.KubernetesBridgeWorker.InfoForToolchainAndProvider(options, workerapi.ToolchainKubernetesYAML, api.ProviderArgoCDOCI)
	for i := range info.SupportedConfigTypes {
		info.SupportedConfigTypes[i].Options = append(info.SupportedConfigTypes[i].Options,
			api.BridgeOption{
				Name:        "ArgoCDNamespace",
				Description: "Namespace where ArgoCD Application CRs will be created. Defaults to \"argocd\".",
				Required:    false,
				DataType:    funcApi.DataTypeString,
				Example:     "argocd",
			},
			api.BridgeOption{
				Name:        "DestinationServer",
				Description: "Target cluster API server URL. Defaults to \"https://kubernetes.default.svc\".",
				Required:    false,
				DataType:    funcApi.DataTypeString,
				Example:     "https://kubernetes.default.svc",
			},
			api.BridgeOption{
				Name:        "DestinationNamespace",
				Description: "Target namespace for deployed resources. Defaults to \"default\".",
				Required:    false,
				DataType:    funcApi.DataTypeString,
				Example:     "default",
			},
			api.BridgeOption{
				Name:        "Project",
				Description: "ArgoCD project name. Defaults to \"default\".",
				Required:    false,
				DataType:    funcApi.DataTypeString,
				Example:     "default",
			},
			api.BridgeOption{
				Name:        "SyncPolicy",
				Description: "ArgoCD sync policy: \"automated\" or \"manual\". Defaults to \"manual\".",
				Required:    false,
				DataType:    funcApi.DataTypeString,
				Example:     "manual",
			},
			api.BridgeOption{
				Name:        "PruneEnabled",
				Description: "Enable pruning of orphaned resources during sync. Defaults to false.",
				Required:    false,
				DataType:    funcApi.DataTypeBool,
				Example:     "false",
			},
			api.BridgeOption{
				Name:        "SelfHealEnabled",
				Description: "Enable self-healing (auto-sync on drift). Defaults to false.",
				Required:    false,
				DataType:    funcApi.DataTypeBool,
				Example:     "false",
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
				Description: "Path within the OCI artifact where manifests reside. Defaults to \".\".",
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
				Name:        "DisableRepoCreds",
				Description: "When true, skip auto-generation of the ArgoCD repo-creds Secret. Defaults to false.",
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
		)
	}
	return info
}

// findApplicationObject returns the first Application object from a list of parsed objects.
func findApplicationObject(objects []*unstructured.Unstructured) *unstructured.Unstructured {
	for _, obj := range objects {
		if obj.GetKind() == argoCDKindApplication {
			return obj
		}
	}
	return nil
}

func (w *ArgoCDOCIWorker) Apply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	if payload.DryRun {
		return fmt.Errorf("dry run is not supported for ArgoCD apply")
	}
	if _, err := w.transformToArgoCDOCIApplication(wctx, &payload, false); err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			err.Error(),
		), err)
	}
	return w.KubernetesBridgeWorker.Apply(wctx, payload)
}

func (w *ArgoCDOCIWorker) WatchForApply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	// Save original data before transform overwrites payload.Data
	originalData := payload.Data

	options, err := w.transformToArgoCDOCIApplication(wctx, &payload, false)
	if err != nil {
		return backoff.Permanent(lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			err.Error(),
		), err))
	}

	// Parse the Application CR from the transformed payload
	objects, err := kubernetes.ParseObjects(payload.Data)
	if err != nil {
		return backoff.Permanent(lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			err.Error(),
		), err))
	}
	appObj := findApplicationObject(objects)
	if appObj == nil {
		return backoff.Permanent(lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			"no Application CR found in transformed payload",
		), errors.New("no Application CR found in transformed payload")))
	}

	appName := appObj.GetName()
	appNamespace := appObj.GetNamespace()
	if appNamespace == "" {
		appNamespace = options.ArgoCDNamespace
	}

	// Create a Kubernetes client to poll the Application CR
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
		"Waiting for ArgoCD Application to sync and become healthy...",
	)); err != nil {
		return err
	}

	timeout := argoCDWaitTimeout

	ctx := wctx.Context()
	pollInterval := defaultPollInterval
	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			log.Log.Info("ArgoCD WatchForApply cancelled")
			return nil
		default:
		}

		if time.Since(startTime) > timeout {
			return lib.SafeSendStatus(wctx, common.NewActionResult(
				api.ActionStatusFailed,
				api.ActionResultApplyWaitFailed,
				fmt.Sprintf("timed out waiting for ArgoCD Application %s/%s to become healthy", appNamespace, appName),
			), context.DeadlineExceeded)
		}

		// Fetch the live Application CR from the cluster
		liveApp := &unstructured.Unstructured{}
		liveApp.SetGroupVersionKind(appObj.GroupVersionKind())
		if err := k8sClient.Get(ctx, client.ObjectKey{
			Namespace: appNamespace,
			Name:      appName,
		}, liveApp); err != nil {
			log.Log.Error(err, "Failed to get ArgoCD Application, will retry", "name", appName, "namespace", appNamespace)
			time.Sleep(pollInterval)
			continue
		}

		// Extract ArgoCD Application status
		overallHealth := getArgoCDAppHealthStatus(liveApp)
		overallSync := getArgoCDAppSyncStatus(liveApp)
		operationPhase := getArgoCDOperationPhase(liveApp)
		resourceStatuses := buildArgoCDResourceStatusMap(liveApp)

		log.Log.Info("ArgoCD Application status",
			"name", appName,
			"health", overallHealth,
			"sync", overallSync,
			"operationPhase", operationPhase,
			"resourceCount", len(resourceStatuses),
		)

		// Send intermediate progress with resource statuses
		progressStatus := common.NewActionResult(
			api.ActionStatusProgressing,
			api.ActionResultNone,
			fmt.Sprintf("ArgoCD Application %s: sync=%s, health=%s, operation=%s", appName, overallSync, overallHealth, operationPhase),
		)
		progressStatus.ResourceStatuses = resourceStatuses
		if err := wctx.SendStatus(progressStatus); err != nil {
			return err
		}

		// Check if the operation has failed
		// Only report failure if sync status is NOT Unknown - when sync is Unknown, a new sync
		// operation is in progress and the operationState.phase may be stale from a previous sync.
		if (operationPhase == argoCDOperationPhaseFailed || operationPhase == argoCDOperationPhaseError) &&
			overallSync != argoCDSyncStatusUnknown {
			return lib.SafeSendStatus(wctx, common.NewActionResult(
				api.ActionStatusFailed,
				api.ActionResultApplyWaitFailed,
				fmt.Sprintf("ArgoCD sync operation failed for %s/%s (phase: %s)", appNamespace, appName, operationPhase),
			), fmt.Errorf("ArgoCD sync operation %s", operationPhase))
		}

		// Check for completion: synced + healthy + operation finished
		// operationPhase must be "Succeeded" or "" (cleared after reconcile) to avoid
		// declaring success on stale Synced+Healthy state from a previous revision.
		if overallSync == argoCDSyncStatusSynced && overallHealth == argoCDHealthStatusHealthy &&
			(operationPhase == argoCDOperationPhaseSucceeded || operationPhase == "") {

			liveStateYAML, liveDataYAML, liveErr := computeManagedResourceState(ctx, k8sClient, liveApp, originalData)
			if liveErr != nil {
				log.Log.Error(liveErr, "Failed to fetch managed resources")
			}

			// Check if operation was cancelled/overridden while waiting
			select {
			case <-wctx.Context().Done():
				log.Log.Info("ArgoCD apply operation was cancelled/overridden, skipping completion status")
				return nil
			default:
			}

			status := common.NewActionResult(
				api.ActionStatusCompleted,
				api.ActionResultApplyCompleted,
				fmt.Sprintf("ArgoCD Application %s synced and healthy at %s", appName, time.Now().Format(time.RFC3339)),
			)
			status.ResourceStatuses = resourceStatuses
			status.LiveState = []byte(liveStateYAML)
			status.LiveData = []byte(liveDataYAML)

			// BridgeState = inventory ConfigMap tracking the ArgoCD Application and repo-creds Secret
			appliedObjects, _ := kubernetes.ParseObjects(payload.Data)
			status.BridgeState = w.BuildBridgeState(payload, appliedObjects)

			// Intentionally ignore: the operation succeeded on the cluster;
			// if the status channel closed, the worker framework handles it.
			_ = wctx.SendStatus(status)
			return nil
		}

		time.Sleep(pollInterval)
	}
}

func (w *ArgoCDOCIWorker) Refresh(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
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

	// Transform payload to Application CR (same as Apply)
	options, err := w.transformToArgoCDOCIApplication(wctx, &payload, false)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			err.Error(),
		), err)
	}

	// Parse the expected Application CR
	objects, err := kubernetes.ParseObjects(payload.Data)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("failed to parse Application CR: %v", err),
		), err)
	}
	expectedApp := findApplicationObject(objects)
	if expectedApp == nil {
		noObjErr := errors.New("no Application CR found in transformed payload")
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

	appNamespace := expectedApp.GetNamespace()
	if appNamespace == "" {
		appNamespace = options.ArgoCDNamespace
	}
	appName := expectedApp.GetName()

	if err := wctx.SendStatus(common.NewActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Retrieving ArgoCD Application state...",
	)); err != nil {
		return err
	}

	// Fetch the live Application CR from the cluster
	liveApp := &unstructured.Unstructured{}
	liveApp.SetGroupVersionKind(expectedApp.GroupVersionKind())
	if err := k8sClient.Get(wctx.Context(), client.ObjectKey{
		Namespace: appNamespace,
		Name:      appName,
	}, liveApp); err != nil {
		if apierrors.IsNotFound(err) {
			return wctx.SendStatus(common.NewActionResult(
				api.ActionStatusCompleted,
				api.ActionResultRefreshAndDrifted,
				fmt.Sprintf("ArgoCD Application %s/%s not found - drift detected", appNamespace, appName),
			))
		}
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("failed to get Application CR: %v", err),
		), err)
	}

	// Extract ArgoCD Application status
	overallSync := getArgoCDAppSyncStatus(liveApp)
	overallHealth := getArgoCDAppHealthStatus(liveApp)
	resourceStatuses := buildArgoCDResourceStatusMap(liveApp)

	log.Log.Info("ArgoCD Application refresh",
		"name", appName,
		"sync", overallSync,
		"health", overallHealth,
	)

	// --- Managed resource discovery from ArgoCD .status.resources[] ---
	syncDrifted := (overallSync != argoCDSyncStatusSynced)
	contentDrifted := false
	var patchedData []byte

	liveStateYAML, liveDataYAML, liveErr := computeManagedResourceState(wctx.Context(), k8sClient, liveApp, originalData)
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
		message = fmt.Sprintf("ArgoCD Application %s: sync=%s, health=%s - drift detected",
			appName, overallSync, overallHealth)
	} else {
		resultType = api.ActionResultRefreshAndNoDrift
		message = fmt.Sprintf("ArgoCD Application %s: sync=%s, health=%s - no drift",
			appName, overallSync, overallHealth)
	}

	result := common.NewActionResult(api.ActionStatusCompleted, resultType, message)
	result.LiveData = []byte(liveDataYAML)
	result.LiveState = []byte(liveStateYAML)
	result.ResourceStatuses = resourceStatuses
	if contentDrifted && len(patchedData) > 0 {
		result.Data = patchedData
	}

	// BridgeState = inventory ConfigMap tracking the ArgoCD Application and repo-creds Secret
	appliedObjects, _ := kubernetes.ParseObjects(payload.Data)
	result.BridgeState = w.BuildBridgeState(payload, appliedObjects)

	return wctx.SendStatus(result)
}

func (w *ArgoCDOCIWorker) Import(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	return lib.SafeSendStatus(wctx, common.NewActionResult(
		api.ActionStatusFailed,
		api.ActionResultImportFailed,
		"Import not supported for ArgoCD OCI bridge",
	), errors.New("Import not supported for ArgoCD OCI bridge"))
}

func (w *ArgoCDOCIWorker) Destroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	if payload.DryRun {
		return fmt.Errorf("dry run is not supported for ArgoCD destroy")
	}
	if _, err := w.transformToArgoCDOCIApplication(wctx, &payload, true); err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			err.Error(),
		), err)
	}
	// Filter out Secret objects — repo-creds are shared infrastructure and should not be deleted per-unit
	payload.Data = filterOutSecrets(payload.Data)
	return w.KubernetesBridgeWorker.Destroy(wctx, payload)
}

func (w *ArgoCDOCIWorker) WatchForDestroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	if _, err := w.transformToArgoCDOCIApplication(wctx, &payload, true); err != nil {
		return backoff.Permanent(lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyWaitFailed,
			err.Error(),
		), err))
	}
	// Filter out Secret objects — repo-creds are shared infrastructure and should not be deleted per-unit
	payload.Data = filterOutSecrets(payload.Data)
	return w.KubernetesBridgeWorker.WatchForDestroy(wctx, payload)
}

// filterOutSecrets removes Secret objects from multi-doc YAML data.
func filterOutSecrets(data []byte) []byte {
	objects, err := kubernetes.ParseObjects(data)
	if err != nil {
		return data // fallback to original data on parse error
	}
	var filtered []*unstructured.Unstructured
	for _, obj := range objects {
		if obj.GetKind() != k8sKindSecret {
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

// ArgoCD status helpers

// getArgoCDAppHealthStatus extracts .status.health.status from an ArgoCD Application CR.
func getArgoCDAppHealthStatus(app *unstructured.Unstructured) string {
	status, found, err := unstructured.NestedString(app.Object, "status", "health", "status")
	if err != nil || !found {
		return argoCDHealthStatusUnknown
	}
	return status
}

// getArgoCDAppSyncStatus extracts .status.sync.status from an ArgoCD Application CR.
func getArgoCDAppSyncStatus(app *unstructured.Unstructured) string {
	status, found, err := unstructured.NestedString(app.Object, "status", "sync", "status")
	if err != nil || !found {
		return argoCDSyncStatusUnknown
	}
	return status
}

// getArgoCDOperationPhase extracts .status.operationState.phase from an ArgoCD Application CR.
func getArgoCDOperationPhase(app *unstructured.Unstructured) string {
	phase, found, err := unstructured.NestedString(app.Object, "status", "operationState", "phase")
	if err != nil || !found {
		return ""
	}
	return phase
}

// mapArgoCDSyncStatus maps an ArgoCD resource sync status to ConfigHub's ResourceSyncStatusType.
func mapArgoCDSyncStatus(syncStatus string) api.ResourceSyncStatusType {
	switch syncStatus {
	case argoCDSyncStatusSynced:
		return api.ResourceSyncStatusSynced
	case argoCDSyncStatusOutOfSync, argoCDSyncStatusUnknown:
		return api.ResourceSyncStatusPending
	default:
		return api.ResourceSyncStatusPending
	}
}

// mapArgoCDHealthStatus maps an ArgoCD resource health status to ConfigHub's ResourceReadinessType.
func mapArgoCDHealthStatus(healthStatus string) api.ResourceReadinessType {
	switch healthStatus {
	case argoCDHealthStatusHealthy:
		return api.ResourceReadinessReady
	case argoCDHealthStatusProgressing:
		return api.ResourceReadinessInProgress
	case argoCDHealthStatusDegraded:
		return api.ResourceReadinessFailed
	case argoCDHealthStatusSuspended:
		return api.ResourceReadinessInProgress
	case argoCDHealthStatusMissing:
		return api.ResourceReadinessFailed
	default:
		return api.ResourceReadinessUnknown
	}
}

// extractResourceObjects extracts managed resource objects from ArgoCD Application .status.resources[].
// Each returned object has GVK, namespace, and name set — suitable for passing to getLiveObjects.
func extractResourceObjects(app *unstructured.Unstructured) []*unstructured.Unstructured {
	resources, found, err := unstructured.NestedSlice(app.Object, "status", "resources")
	if err != nil || !found || len(resources) == 0 {
		return nil
	}

	var result []*unstructured.Unstructured
	for _, r := range resources {
		res, ok := r.(map[string]interface{})
		if !ok {
			continue
		}

		group, _ := res["group"].(string)
		version, _ := res["version"].(string)
		kind, _ := res["kind"].(string)
		name, _ := res["name"].(string)
		namespace, _ := res["namespace"].(string)

		// Skip entries with missing required fields
		if kind == "" || name == "" || version == "" {
			continue
		}

		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   group,
			Version: version,
			Kind:    kind,
		})
		obj.SetName(name)
		obj.SetNamespace(namespace)
		result = append(result, obj)
	}
	return result
}

// buildArgoCDResourceStatusMap builds a ResourceStatusMap from ArgoCD Application .status.resources[].
func buildArgoCDResourceStatusMap(app *unstructured.Unstructured) api.ResourceStatusMap {
	resources, found, err := unstructured.NestedSlice(app.Object, "status", "resources")
	if err != nil || !found || len(resources) == 0 {
		return nil
	}

	statusMap := make(api.ResourceStatusMap)
	now := time.Now()

	for _, r := range resources {
		res, ok := r.(map[string]interface{})
		if !ok {
			continue
		}

		group, _ := res["group"].(string)
		version, _ := res["version"].(string)
		kind, _ := res["kind"].(string)
		name, _ := res["name"].(string)
		namespace, _ := res["namespace"].(string)
		syncStatus, _ := res["status"].(string)

		// Extract health status from nested health object
		var healthStatus string
		if health, ok := res["health"].(map[string]interface{}); ok {
			healthStatus, _ = health["status"].(string)
		}

		// Build the resource type key in the format "apiVersion/kind#namespace/name"
		key := buildResourceKey(group, version, kind, namespace, name)

		statusMap[key] = api.ResourceStatus{
			SyncStatus: mapArgoCDSyncStatus(syncStatus),
			Readiness:  mapArgoCDHealthStatus(healthStatus),
			UpdatedAt:  now,
		}
	}

	return statusMap
}

// computeManagedResourceState fetches managed resources from an ArgoCD Application's
// .status.resources[] and returns LiveState (full objects) and LiveData (cleaned, spec-only).
// LiveData is always derived from LiveState as its single source of truth.
// originalData is the unit's original data (before transform); it is used to determine
// which resources had an explicit namespace set — resources without a namespace in the
// original data have their namespace cleared in LiveData to avoid false drift.
func computeManagedResourceState(ctx context.Context, k8sClient kubernetes.KubernetesClient, app *unstructured.Unstructured, originalData []byte) (liveStateYAML, liveDataYAML string, err error) {
	managedObjs := extractResourceObjects(app)
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
	// ArgoCD's manifest rendering API returns resources WITHOUT a namespace — the
	// namespace is only applied at sync time from the Application's spec.destination.namespace.
	// This means the unit's original data (rendered manifests) typically has no namespace,
	// but the live cluster resources do. If we keep the namespace in LiveData, it would
	// appear as a diff against the original data, causing false drift on every Refresh.
	//
	// Rule: remove the namespace from a LiveData resource ONLY IF the original data
	// did not explicitly set a namespace on that resource. If the user (or a function
	// like set-namespace) explicitly set a namespace in the unit data, we preserve it
	// in LiveData so that any divergence from the intended namespace IS detected as drift.
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
