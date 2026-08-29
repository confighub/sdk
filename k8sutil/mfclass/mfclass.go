// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package mfclass classifies Kubernetes server-side-apply field managers into
// meaningful categories — appliers (kubectl, ArgoCD, Flux, Sveltos, Helm, …),
// asynchronous controllers, and default fields — and provides field-set
// utilities built on structured-merge-diff for reasoning about which fields
// each manager owns.
//
// It is the single source of truth for manager categorization, shared by
// "cub k8s refresh" (which uses it to decide which controller-set fields to
// strip before storing cluster state as configuration) and the k8s-mf
// diagnostic tool, so the tool mirrors what a refresh actually does.
//
// There is deliberately no admission-controller category. A mutating dynamic
// admission controller rewrites the object inside the API server's write path,
// and the API server attributes the resulting fields to the field manager of
// whichever client made the call — the injector's own name never reaches
// managedFields. Any manager that does appear is a client in its own right, so
// it is an applier or an async controller.
package mfclass

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Category is the kind of actor a field manager represents.
type Category string

const (
	// CategoryApplier is a whole-resource owner that a user manages config
	// with: kubectl, ArgoCD, Flux, Sveltos, Helm, etc. These are the managers
	// a "take over" operation contends with.
	CategoryApplier Category = "Applier"

	// CategoryAsyncController is a controller that writes fields out of band
	// rather than as the config's owner: the built-in workload controllers,
	// HPA/VPA, cert-manager, ingress controllers, and the API server's own
	// built-in admission plugins. Its fields are stripped on refresh and import
	// because they are cluster-owned, not user intent.
	CategoryAsyncController Category = "AsyncController"

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
		CategoryAsyncController,
		CategoryUnknown,
	}
}

// managerInfo is a registry entry for a known field manager.
type managerInfo struct {
	category Category
	// display is a friendly tool/controller name (e.g. "ArgoCD", "Flux").
	display string
}

// exactManagers maps exact field-manager strings to their classification.
//
// The async entries are the controllers whose fields are stripped on refresh and
// import, reached through IsIgnored. The applier entries are the whole-resource
// tools a user may transition between.
//
// Every name here has to be one the API server actually records. A tool that
// writes through a mutating webhook does not get one (see the package comment),
// and neither does a tool that shells out to kubectl.
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

	// --- Async controllers (reconcile loops) ---
	// Istio and Linkerd appear here for what their control planes reconcile
	// directly (istiod's per-namespace istio-ca-root-cert ConfigMaps and gateway
	// status, linkerd's destination controller), not for sidecar injection:
	// injection runs as a mutating webhook, so the injected fields are recorded
	// under whichever manager created the workload.
	"istio-pilot":         {CategoryAsyncController, "Istio"},
	"istiod":              {CategoryAsyncController, "Istio"},
	"istio-galley":        {CategoryAsyncController, "Istio"},
	"linkerd-destination": {CategoryAsyncController, "Linkerd"},

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

	// The API server writes under its own name for the fields its built-in
	// admission plugins set, which is how a Namespace ends up with
	// metadata.labels."kubernetes.io/metadata.name" owned by a real manager
	// rather than showing up as an unowned default.
	"kube-apiserver": {CategoryAsyncController, "Kubernetes"},
}

// prefixManagers classifies managers by name prefix when there is no exact
// match. Order matters only in that exact matches always win (checked first).
var prefixManagers = []struct {
	prefix string
	info   managerInfo
}{
	// kubectl-client-side-apply, kubectl-edit, kubectl-create, kubectl,
	// kubectl-last-applied, …
	//
	// Tools that deploy by shelling out to kubectl land here too and cannot be
	// told apart from a person at a terminal. Spinnaker is the notable one: its
	// clouddriver runs "kubectl apply" without --field-manager, so its writes
	// are recorded as kubectl-client-side-apply, or as kubectl when the
	// server-side-apply strategy is enabled.
	{"kubectl", managerInfo{CategoryApplier, "kubectl"}},
	// confighub-bridge-worker and the other confighub* managers, all legacy:
	// they were written by the removed bridge worker. Kept so resources it
	// applied still classify, but nothing writes under these names now.
	{"confighub", managerInfo{CategoryApplier, "ConfigHub (legacy)"}},
}

// ClassifyManager categorizes a field manager by name alone (exact match, then
// prefix). It returns CategoryUnknown with the raw name as display when the
// manager is not recognized. It never applies operation-based heuristics — use
// Classify for that. The take-over and ignored-manager decisions are built on
// this name-based classification.
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

// IsIgnored reports whether a manager is a controller whose fields are stripped
// on refresh and import, because what it wrote is cluster state rather than the
// configuration a user authored.
func IsIgnored(manager string) bool {
	cat, _ := ClassifyManager(manager)
	return cat == CategoryAsyncController
}

// ShouldTakeOver reports whether keeper should take ownership away from manager.
// We take over every other applier (kubectl, ArgoCD, Flux, Helm, Sveltos,
// Tanka, the legacy ConfigHub managers, …) so SSA can fully manage the resource, while
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
