// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8sdescribe

import "strings"

// viewFunc renders the kind-specific sections of a resource.
type viewFunc func(doc record) []*Section

// registry maps "group/Kind" (bare "Kind" for the core group) to a view.
// The API version is deliberately ignored so that, e.g., autoscaling/v1 and
// autoscaling/v2 HorizontalPodAutoscalers, or either Traefik group, resolve to
// the same view. Keep in sync with the UI registry
// (ui/src/pages/x/resource-explorer/components/ResourceDrawer/friendly/registry.tsx).
var registry = map[string]viewFunc{
	// Core (empty group).
	"Namespace":             namespaceView,
	"Service":               serviceView,
	"ConfigMap":             configMapView,
	"Secret":                secretView,
	"ServiceAccount":        serviceAccountView,
	"PersistentVolumeClaim": persistentVolumeClaimView,

	// Workloads.
	"apps/Deployment":  deploymentView,
	"apps/StatefulSet": statefulSetView,
	"apps/DaemonSet":   daemonSetView,
	"batch/Job":        jobView,
	"batch/CronJob":    cronJobView,

	// Autoscaling.
	"autoscaling/HorizontalPodAutoscaler": horizontalPodAutoscalerView,

	// RBAC.
	"rbac.authorization.k8s.io/Role":               roleView,
	"rbac.authorization.k8s.io/ClusterRole":        roleView,
	"rbac.authorization.k8s.io/RoleBinding":        roleBindingView,
	"rbac.authorization.k8s.io/ClusterRoleBinding": roleBindingView,

	// Networking.
	"networking.k8s.io/Ingress":       ingressView,
	"networking.k8s.io/NetworkPolicy": networkPolicyView,

	// ArgoCD.
	"argoproj.io/Application": argoApplicationView,

	// Flux.
	"helm.toolkit.fluxcd.io/HelmRelease":        helmReleaseView,
	"kustomize.toolkit.fluxcd.io/Kustomization": fluxKustomizationView,
	"source.toolkit.fluxcd.io/GitRepository":    fluxSourceView,
	"source.toolkit.fluxcd.io/HelmRepository":   fluxSourceView,
	"source.toolkit.fluxcd.io/HelmChart":        fluxSourceView,
	"source.toolkit.fluxcd.io/OCIRepository":    fluxSourceView,
	"source.toolkit.fluxcd.io/Bucket":           fluxSourceView,
	"notification.toolkit.fluxcd.io/Alert":      fluxAlertView,

	// cert-manager.
	"cert-manager.io/Certificate":   certificateView,
	"cert-manager.io/Issuer":        issuerView,
	"cert-manager.io/ClusterIssuer": issuerView,

	// External Secrets Operator.
	"external-secrets.io/ExternalSecret":        externalSecretView,
	"external-secrets.io/ClusterExternalSecret": externalSecretView,
	"external-secrets.io/SecretStore":           secretStoreView,
	"external-secrets.io/ClusterSecretStore":    secretStoreView,

	// Traefik — current group and the legacy containo.us group.
	"traefik.io/IngressRoute":             ingressRouteView,
	"traefik.io/IngressRouteTCP":          ingressRouteTCPView,
	"traefik.io/IngressRouteUDP":          ingressRouteTCPView,
	"traefik.io/Middleware":               middlewareView,
	"traefik.io/MiddlewareTCP":            middlewareView,
	"traefik.io/TraefikService":           traefikServiceView,
	"traefik.containo.us/IngressRoute":    ingressRouteView,
	"traefik.containo.us/IngressRouteTCP": ingressRouteTCPView,
	"traefik.containo.us/IngressRouteUDP": ingressRouteTCPView,
	"traefik.containo.us/Middleware":      middlewareView,
	"traefik.containo.us/MiddlewareTCP":   middlewareView,
	"traefik.containo.us/TraefikService":  traefikServiceView,
}

// registryKey reduces an apiVersion ("group/version" or "version") and kind to
// the registry key.
func registryKey(apiVersion, kind string) string {
	group, _, hasGroup := strings.Cut(apiVersion, "/")
	if !hasGroup {
		return kind
	}
	return group + "/" + kind
}

// HasView reports whether a dedicated (non-generic) view exists for the
// apiVersion/kind pair.
func HasView(apiVersion, kind string) bool {
	return registry[registryKey(apiVersion, kind)] != nil
}

// Describe renders a parsed resource document as metadata sections followed by
// kind-specific sections. Resources without a dedicated view fall back to a
// generic rendering of their top-level fields.
func Describe(doc map[string]any) []*Section {
	if doc == nil {
		return nil
	}
	sections := metadataSections(doc)
	view := registry[registryKey(asString(doc["apiVersion"]), asString(doc["kind"]))]
	if view == nil {
		view = genericView
	}
	for _, s := range view(doc) {
		sections = add(sections, s)
	}
	return sections
}

// metadataSections render name/namespace/kind, labels, and annotations —
// the header every resource has.
func metadataSections(doc record) []*Section {
	var sections []*Section

	meta := newSection("Metadata")
	meta.field("Name", str(doc, "metadata", "name"))
	meta.field("Namespace", str(doc, "metadata", "namespace"))
	meta.field("API Version", asScalar(doc["apiVersion"]))
	meta.field("Kind", asScalar(doc["kind"]))
	sections = add(sections, meta)

	labels := asStringMap(get(doc, "metadata", "labels"))
	if len(labels) > 0 {
		s := newSection("Labels")
		for _, k := range sortedKeys(labels) {
			s.field(k, labels[k])
		}
		sections = add(sections, s)
	}

	// ConfigHub's own bookkeeping annotations are noise here; the Unit section
	// printed by the caller already shows the ConfigHub provenance.
	annotations := asStringMap(get(doc, "metadata", "annotations"))
	if len(annotations) > 0 {
		s := newSection("Annotations")
		for _, k := range sortedKeys(annotations) {
			if strings.HasPrefix(k, "confighub.com/") {
				continue
			}
			if strings.Contains(annotations[k], "\n") {
				s.block(k, annotations[k])
			} else {
				s.field(k, annotations[k])
			}
		}
		sections = add(sections, s)
	}

	return sections
}

// genericView is the fallback for kinds without a dedicated view: scalar
// top-level fields become rows, nested structures become YAML blocks.
func genericView(doc record) []*Section {
	skip := map[string]bool{"apiVersion": true, "kind": true, "metadata": true, "status": true}
	s := newSection("Fields")
	scalarsAndNested(s, doc, skip)
	if s.Empty() {
		s.note("This resource has no fields beyond its metadata.")
	}
	return []*Section{s}
}
