// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package mfclass classifies Kubernetes server-side-apply field managers into
// meaningful categories — appliers (kubectl, ConfigHub, ArgoCD, Flux, Sveltos,
// Helm, …), asynchronous controllers, admission controllers, and default
// fields — and provides field-set utilities built on structured-merge-diff for
// reasoning about which fields each manager owns.
//
// It is the single source of truth for manager categorization shared by the
// ConfigHub Kubernetes bridge (which uses it to decide which managers to take
// over and which controller-set defaults to strip) and the k8s-mf diagnostic
// tool. Keeping the knowledge here means the tool faithfully mirrors the
// bridge's real apply behavior.
//
// This package must not import the parent kubernetes bridge package — the
// dependency runs the other way.
package mfclass

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Category is the kind of actor a field manager represents.
type Category string

const (
	// CategoryApplier is a whole-resource owner that a user manages config
	// with: kubectl, the ConfigHub bridge, ArgoCD, Flux, Sveltos, Helm, etc.
	// These are the managers a "take over" operation contends with.
	CategoryApplier Category = "Applier"

	// CategoryAsyncController is a reconcile-loop controller that writes fields
	// out of band after creation (HPA/VPA, the built-in workload controllers,
	// cert-manager, ingress controllers, …). Its fields are normally stripped
	// when importing config because they are cluster-owned, not user intent.
	CategoryAsyncController Category = "AsyncController"

	// CategoryAdmissionController is a mutating webhook / injector that rewrites
	// the object at write time (Istio/Linkerd sidecar injection, the VPA
	// admission controller, …).
	CategoryAdmissionController Category = "AdmissionController"

	// CategoryDefaultFields is not a real manager. It is the synthetic category
	// for fields present on the live object but owned by no manager — values the
	// API server defaulted. It is computed (object fields minus all managed
	// fields), never returned by ClassifyManager.
	CategoryDefaultFields Category = "DefaultFields"

	// CategoryUnknown is an unrecognized manager that no heuristic could place.
	CategoryUnknown Category = "Unknown"
)

// Categories returns the display-ordered list of real (non-synthetic)
// categories, appliers first.
func Categories() []Category {
	return []Category{
		CategoryApplier,
		CategoryAdmissionController,
		CategoryAsyncController,
		CategoryUnknown,
	}
}

// ConfigHubFieldManager is the field manager name used by the ConfigHub
// Kubernetes bridge worker for server-side apply. Mirrors the FieldManager
// constant in the parent kubernetes package (duplicated here to avoid an
// import cycle).
const ConfigHubFieldManager = "confighub-bridge-worker"

// managerInfo is a registry entry for a known field manager.
type managerInfo struct {
	category Category
	// display is a friendly tool/controller name (e.g. "ArgoCD", "Flux").
	display string
}

// exactManagers maps exact field-manager strings to their classification.
//
// The async + admission entries here are exactly the set the bridge historically
// treated as "ignored field managers" (controllers whose fields are stripped on
// import/refresh); the bridge now derives that set from IsIgnored, so this map
// must stay in sync with that intent. The applier entries are the whole-resource
// tools a user may transition between.
var exactManagers = map[string]managerInfo{
	// --- Appliers (whole-resource owners) ---
	"argocd-controller":             {CategoryApplier, "ArgoCD"}, // ArgoCD default SSA manager (ArgoCDSSAManager)
	"argocd-application-controller": {CategoryApplier, "ArgoCD"}, // ArgoCD application controller (seen on some resources)
	"helm":                          {CategoryApplier, "Helm"},
	"helm-controller":               {CategoryApplier, "Flux"}, // Flux HelmRelease
	"kustomize-controller":          {CategoryApplier, "Flux"}, // Flux Kustomization
	"flux":                          {CategoryApplier, "Flux"},
	// Projectsveltos's addon-controller applies via controller-runtime SSA
	// without setting an explicit FieldOwner, so its field manager defaults to
	// the apply-patch content type. Note this generic name can also appear for
	// other controller-runtime SSA clients that likewise omit FieldOwner.
	"application/apply-patch": {CategoryApplier, "Sveltos"},
	"sveltos":                 {CategoryApplier, "Sveltos"}, // defensive alias in case a version sets an explicit manager
	"tanka":                   {CategoryApplier, "Tanka"},
	"before-first-apply":      {CategoryApplier, "legacy default"}, // Legacy default field manager

	// --- Admission controllers (write-time injectors / mutating webhooks) ---
	"vpa-admission-controller": {CategoryAdmissionController, "VPA"},
	"istio-pilot":              {CategoryAdmissionController, "Istio"},
	"istiod":                   {CategoryAdmissionController, "Istio"},
	"istio-galley":             {CategoryAdmissionController, "Istio"},
	"linkerd-proxy-injector":   {CategoryAdmissionController, "Linkerd"},
	"linkerd-destination":      {CategoryAdmissionController, "Linkerd"},

	// --- Async controllers (reconcile loops) ---
	"horizontal-pod-autoscaler-controller": {CategoryAsyncController, "HPA"},
	"vpa-recommender":                      {CategoryAsyncController, "VPA"},
	"vpa-updater":                          {CategoryAsyncController, "VPA"},

	"endpoint-controller":         {CategoryAsyncController, "Kubernetes"},
	"endpointslice-controller":    {CategoryAsyncController, "Kubernetes"},
	"service-controller":          {CategoryAsyncController, "Kubernetes"},
	"deployment-controller":       {CategoryAsyncController, "Kubernetes"},
	"replicaset-controller":       {CategoryAsyncController, "Kubernetes"},
	"daemonset-controller":        {CategoryAsyncController, "Kubernetes"},
	"statefulset-controller":      {CategoryAsyncController, "Kubernetes"},
	"job-controller":              {CategoryAsyncController, "Kubernetes"},
	"cronjob-controller":          {CategoryAsyncController, "Kubernetes"},
	"pv-protection-controller":    {CategoryAsyncController, "Kubernetes"},
	"pvc-protection-controller":   {CategoryAsyncController, "Kubernetes"},
	"attach-detach-controller":    {CategoryAsyncController, "Kubernetes"},
	"persistentvolume-controller": {CategoryAsyncController, "Kubernetes"},

	"node-controller":           {CategoryAsyncController, "Kubernetes"},
	"taint-controller":          {CategoryAsyncController, "Kubernetes"},
	"scheduler":                 {CategoryAsyncController, "Kubernetes"},
	"kube-scheduler":            {CategoryAsyncController, "Kubernetes"},
	"cluster-autoscaler":        {CategoryAsyncController, "Cluster Autoscaler"},
	"descheduler":               {CategoryAsyncController, "Descheduler"},
	"node-problem-detector":     {CategoryAsyncController, "Node Problem Detector"},
	"node-local-dns-sidecar":    {CategoryAsyncController, "NodeLocal DNS"},
	"node-lifecycle-controller": {CategoryAsyncController, "Kubernetes"},

	"ingress-nginx-controller": {CategoryAsyncController, "NGINX Ingress"},
	"traefik":                  {CategoryAsyncController, "Traefik"},

	"cert-manager-certificates-trigger":         {CategoryAsyncController, "cert-manager"},
	"cert-manager-certificates-issuing":         {CategoryAsyncController, "cert-manager"},
	"cert-manager-certificates-key-manager":     {CategoryAsyncController, "cert-manager"},
	"cert-manager-certificates-request-manager": {CategoryAsyncController, "cert-manager"},
	"cert-manager-certificates-readiness":       {CategoryAsyncController, "cert-manager"},
	"cert-manager-orders":                       {CategoryAsyncController, "cert-manager"},
	"cert-manager-challenges":                   {CategoryAsyncController, "cert-manager"},
	"cert-manager-ingress-shim":                 {CategoryAsyncController, "cert-manager"},
	"cert-manager-controller":                   {CategoryAsyncController, "cert-manager"},
	"external-secrets":                          {CategoryAsyncController, "External Secrets"},
	"sealed-secrets-controller":                 {CategoryAsyncController, "Sealed Secrets"},

	"operator-sdk":            {CategoryAsyncController, "Operator SDK"},
	"kopf":                    {CategoryAsyncController, "Kopf"},
	"kube-controller-manager": {CategoryAsyncController, "Kubernetes"},
}

// prefixManagers classifies managers by name prefix when there is no exact
// match. Order matters only in that exact matches always win (checked first).
var prefixManagers = []struct {
	prefix string
	info   managerInfo
}{
	// kubectl-client-side-apply, kubectl-edit, kubectl-create, kubectl,
	// kubectl-last-applied, …
	{"kubectl", managerInfo{CategoryApplier, "kubectl"}},
	// confighub-bridge-worker and any older confighub* managers.
	{"confighub", managerInfo{CategoryApplier, "ConfigHub"}},
}

// ClassifyManager categorizes a field manager by name alone (exact match, then
// prefix). It returns CategoryUnknown with the raw name as display when the
// manager is not recognized. It never applies operation-based heuristics — use
// Classify for that. The bridge's take-over and ignored-manager decisions are
// built on this name-based classification.
func ClassifyManager(manager string) (Category, string) {
	if info, ok := exactManagers[manager]; ok {
		return info.category, info.display
	}
	for _, pm := range prefixManagers {
		if strings.HasPrefix(manager, pm.prefix) {
			return pm.info.category, pm.info.display
		}
	}
	return CategoryUnknown, manager
}

// Classification is the full result of classifying a single managedFields entry.
type Classification struct {
	Manager     string
	Category    Category
	Display     string
	Operation   metav1.ManagedFieldsOperationType
	Subresource string
	// Heuristic is true when Category was inferred from the entry's operation
	// rather than from the name registry (the manager was unknown).
	Heuristic bool
}

// Classify categorizes a managedFields entry. It first tries the name registry;
// for unknown managers it falls back to a conservative operation-based guess:
//
//	operation == Apply              -> Applier  (whole-object SSA owner)
//	operation == Update, status     -> AsyncController (a controller writing status)
//	otherwise                       -> Unknown  (a one-off Update; don't guess)
//
// Heuristic guesses are flagged so callers can mark them as uncertain.
func Classify(e metav1.ManagedFieldsEntry) Classification {
	cat, display := ClassifyManager(e.Manager)
	c := Classification{
		Manager:     e.Manager,
		Category:    cat,
		Display:     display,
		Operation:   e.Operation,
		Subresource: e.Subresource,
	}
	if cat != CategoryUnknown {
		return c
	}
	switch e.Operation {
	case metav1.ManagedFieldsOperationApply:
		c.Category = CategoryApplier
		c.Heuristic = true
	case metav1.ManagedFieldsOperationUpdate:
		if e.Subresource == "status" {
			c.Category = CategoryAsyncController
			c.Heuristic = true
		}
	}
	return c
}

// IsApplier reports whether a manager is a whole-resource applier by name.
func IsApplier(manager string) bool {
	cat, _ := ClassifyManager(manager)
	return cat == CategoryApplier
}

// IsIgnored reports whether a manager is a controller whose fields the bridge
// strips on import/refresh (async or admission controllers). This is the
// successor to the bridge's former ignoredFieldManagers map.
func IsIgnored(manager string) bool {
	cat, _ := ClassifyManager(manager)
	return cat == CategoryAsyncController || cat == CategoryAdmissionController
}

// ShouldTakeOver reports whether keeper should take ownership away from manager.
// We take over every other applier (kubectl, old ConfigHub managers, ArgoCD,
// Flux, Helm, Sveltos, Tanka, …) so SSA can fully manage the resource, while
// preserving controller-owned fields (HPA/VPA and friends). keeper never takes
// over itself.
//
// See https://kubernetes.io/docs/reference/using-api/server-side-apply/#transferring-ownership
func ShouldTakeOver(manager, keeper string) bool {
	if manager == keeper {
		return false
	}
	return IsApplier(manager)
}
