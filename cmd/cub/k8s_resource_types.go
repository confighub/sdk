// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"
)

// Kubernetes resource-type resolution for the `cub k8s` commands: it turns
// kubectl-style names ("deploy", "netpol", "ingressroutes.traefik.io") into the
// ConfigHub ResourceType form, which is "<group>/<version>/<Kind>" for grouped
// types and "<version>/<Kind>" for the core group.
//
// Resolution is deliberately version-agnostic: "hpa" matches autoscaling/v1 and
// autoscaling/v2 alike. Types that aren't in the table below still resolve —
// they match on Kind across any group, case-insensitively, with the plural "s"
// optional — so custom resources work without being enumerated here.

// k8sKind is a Kubernetes kind addressable by kubectl-style names.
type k8sKind struct {
	Group  string // empty for the core group
	Kind   string
	Plural string // lowercase plural, kubectl's primary name for the type
	Short  []string
}

// k8sKinds covers the built-in Kubernetes types plus the ecosystem types that
// have dedicated detail views (see public/cmd/cub/k8sdescribe). It exists to
// make familiar short names work, not to be exhaustive: anything missing is
// still addressable by Kind.
var k8sKinds = []k8sKind{
	// Core group.
	{Kind: "Pod", Plural: "pods", Short: []string{"po"}},
	{Kind: "Service", Plural: "services", Short: []string{"svc"}},
	{Kind: "ConfigMap", Plural: "configmaps", Short: []string{"cm"}},
	{Kind: "Secret", Plural: "secrets"},
	{Kind: "ServiceAccount", Plural: "serviceaccounts", Short: []string{"sa"}},
	{Kind: "Namespace", Plural: "namespaces", Short: []string{"ns"}},
	{Kind: "Node", Plural: "nodes", Short: []string{"no"}},
	{Kind: "PersistentVolume", Plural: "persistentvolumes", Short: []string{"pv"}},
	{Kind: "PersistentVolumeClaim", Plural: "persistentvolumeclaims", Short: []string{"pvc"}},
	{Kind: "Endpoints", Plural: "endpoints", Short: []string{"ep"}},
	{Kind: "Event", Plural: "events", Short: []string{"ev"}},
	{Kind: "LimitRange", Plural: "limitranges", Short: []string{"limits"}},
	{Kind: "ResourceQuota", Plural: "resourcequotas", Short: []string{"quota"}},
	{Kind: "ReplicationController", Plural: "replicationcontrollers", Short: []string{"rc"}},
	{Kind: "PodTemplate", Plural: "podtemplates"},

	// Workloads.
	{Group: "apps", Kind: "Deployment", Plural: "deployments", Short: []string{"deploy"}},
	{Group: "apps", Kind: "StatefulSet", Plural: "statefulsets", Short: []string{"sts"}},
	{Group: "apps", Kind: "DaemonSet", Plural: "daemonsets", Short: []string{"ds"}},
	{Group: "apps", Kind: "ReplicaSet", Plural: "replicasets", Short: []string{"rs"}},
	{Group: "apps", Kind: "ControllerRevision", Plural: "controllerrevisions"},
	{Group: "batch", Kind: "Job", Plural: "jobs"},
	{Group: "batch", Kind: "CronJob", Plural: "cronjobs", Short: []string{"cj"}},

	// Autoscaling and availability.
	{Group: "autoscaling", Kind: "HorizontalPodAutoscaler", Plural: "horizontalpodautoscalers", Short: []string{"hpa"}},
	{Group: "policy", Kind: "PodDisruptionBudget", Plural: "poddisruptionbudgets", Short: []string{"pdb"}},
	{Group: "scheduling.k8s.io", Kind: "PriorityClass", Plural: "priorityclasses", Short: []string{"pc"}},
	{Group: "node.k8s.io", Kind: "RuntimeClass", Plural: "runtimeclasses"},

	// Networking.
	{Group: "networking.k8s.io", Kind: "Ingress", Plural: "ingresses", Short: []string{"ing"}},
	{Group: "networking.k8s.io", Kind: "IngressClass", Plural: "ingressclasses"},
	{Group: "networking.k8s.io", Kind: "NetworkPolicy", Plural: "networkpolicies", Short: []string{"netpol"}},
	{Group: "discovery.k8s.io", Kind: "EndpointSlice", Plural: "endpointslices"},
	{Group: "gateway.networking.k8s.io", Kind: "Gateway", Plural: "gateways"},
	{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Plural: "httproutes"},
	{Group: "gateway.networking.k8s.io", Kind: "GatewayClass", Plural: "gatewayclasses"},

	// RBAC.
	{Group: "rbac.authorization.k8s.io", Kind: "Role", Plural: "roles"},
	{Group: "rbac.authorization.k8s.io", Kind: "RoleBinding", Plural: "rolebindings"},
	{Group: "rbac.authorization.k8s.io", Kind: "ClusterRole", Plural: "clusterroles"},
	{Group: "rbac.authorization.k8s.io", Kind: "ClusterRoleBinding", Plural: "clusterrolebindings"},

	// Storage.
	{Group: "storage.k8s.io", Kind: "StorageClass", Plural: "storageclasses", Short: []string{"sc"}},
	{Group: "storage.k8s.io", Kind: "VolumeAttachment", Plural: "volumeattachments"},
	{Group: "storage.k8s.io", Kind: "CSIDriver", Plural: "csidrivers"},

	// Cluster extension points.
	{Group: "apiextensions.k8s.io", Kind: "CustomResourceDefinition", Plural: "customresourcedefinitions", Short: []string{"crd", "crds"}},
	{Group: "apiregistration.k8s.io", Kind: "APIService", Plural: "apiservices"},
	{Group: "admissionregistration.k8s.io", Kind: "MutatingWebhookConfiguration", Plural: "mutatingwebhookconfigurations"},
	{Group: "admissionregistration.k8s.io", Kind: "ValidatingWebhookConfiguration", Plural: "validatingwebhookconfigurations"},
	{Group: "coordination.k8s.io", Kind: "Lease", Plural: "leases"},
	{Group: "certificates.k8s.io", Kind: "CertificateSigningRequest", Plural: "certificatesigningrequests", Short: []string{"csr"}},

	// Argo CD.
	{Group: "argoproj.io", Kind: "Application", Plural: "applications", Short: []string{"argoapp"}},
	{Group: "argoproj.io", Kind: "ApplicationSet", Plural: "applicationsets", Short: []string{"appset"}},
	{Group: "argoproj.io", Kind: "AppProject", Plural: "appprojects", Short: []string{"appproj"}},

	// Flux.
	{Group: "helm.toolkit.fluxcd.io", Kind: "HelmRelease", Plural: "helmreleases", Short: []string{"hr"}},
	{Group: "kustomize.toolkit.fluxcd.io", Kind: "Kustomization", Plural: "kustomizations", Short: []string{"ks"}},
	{Group: "source.toolkit.fluxcd.io", Kind: "GitRepository", Plural: "gitrepositories", Short: []string{"gitrepo"}},
	{Group: "source.toolkit.fluxcd.io", Kind: "HelmRepository", Plural: "helmrepositories", Short: []string{"helmrepo"}},
	{Group: "source.toolkit.fluxcd.io", Kind: "HelmChart", Plural: "helmcharts", Short: []string{"hc"}},
	{Group: "source.toolkit.fluxcd.io", Kind: "OCIRepository", Plural: "ocirepositories", Short: []string{"ocirepo"}},
	{Group: "source.toolkit.fluxcd.io", Kind: "Bucket", Plural: "buckets"},
	{Group: "notification.toolkit.fluxcd.io", Kind: "Alert", Plural: "alerts"},
	{Group: "notification.toolkit.fluxcd.io", Kind: "Provider", Plural: "providers"},
	{Group: "notification.toolkit.fluxcd.io", Kind: "Receiver", Plural: "receivers"},

	// cert-manager.
	{Group: "cert-manager.io", Kind: "Certificate", Plural: "certificates", Short: []string{"cert"}},
	{Group: "cert-manager.io", Kind: "CertificateRequest", Plural: "certificaterequests"},
	{Group: "cert-manager.io", Kind: "Issuer", Plural: "issuers"},
	{Group: "cert-manager.io", Kind: "ClusterIssuer", Plural: "clusterissuers"},

	// External Secrets Operator.
	{Group: "external-secrets.io", Kind: "ExternalSecret", Plural: "externalsecrets", Short: []string{"es"}},
	{Group: "external-secrets.io", Kind: "ClusterExternalSecret", Plural: "clusterexternalsecrets", Short: []string{"ces"}},
	{Group: "external-secrets.io", Kind: "SecretStore", Plural: "secretstores", Short: []string{"ss"}},
	{Group: "external-secrets.io", Kind: "ClusterSecretStore", Plural: "clustersecretstores", Short: []string{"css"}},

	// Traefik.
	{Group: "traefik.io", Kind: "IngressRoute", Plural: "ingressroutes"},
	{Group: "traefik.io", Kind: "IngressRouteTCP", Plural: "ingressroutetcps"},
	{Group: "traefik.io", Kind: "IngressRouteUDP", Plural: "ingressrouteudps"},
	{Group: "traefik.io", Kind: "Middleware", Plural: "middlewares"},
	{Group: "traefik.io", Kind: "MiddlewareTCP", Plural: "middlewaretcps"},
	{Group: "traefik.io", Kind: "TraefikService", Plural: "traefikservices"},

	// Prometheus Operator.
	{Group: "monitoring.coreos.com", Kind: "ServiceMonitor", Plural: "servicemonitors", Short: []string{"smon"}},
	{Group: "monitoring.coreos.com", Kind: "PodMonitor", Plural: "podmonitors", Short: []string{"pmon"}},
	{Group: "monitoring.coreos.com", Kind: "PrometheusRule", Plural: "prometheusrules", Short: []string{"promrule"}},
	{Group: "monitoring.coreos.com", Kind: "Prometheus", Plural: "prometheuses"},
	{Group: "monitoring.coreos.com", Kind: "Alertmanager", Plural: "alertmanagers"},
}

// k8sKindAliases maps every accepted spelling to its kind: the plural, the
// lowercased Kind, each short name, and the "<plural>.<group>" form kubectl
// uses to disambiguate.
var k8sKindAliases = buildK8sKindAliases()

func buildK8sKindAliases() map[string]*k8sKind {
	aliases := map[string]*k8sKind{}
	register := func(name string, kind *k8sKind) {
		// First registration wins, so built-in types (listed first) keep their
		// short names if an ecosystem type ever collides.
		if _, exists := aliases[name]; !exists {
			aliases[name] = kind
		}
	}
	for i := range k8sKinds {
		kind := &k8sKinds[i]
		register(kind.Plural, kind)
		register(strings.ToLower(kind.Kind), kind)
		for _, short := range kind.Short {
			register(short, kind)
		}
		if kind.Group != "" {
			register(kind.Plural+"."+kind.Group, kind)
			register(strings.ToLower(kind.Kind)+"."+kind.Group, kind)
		}
	}
	return aliases
}

// crdKind is the Kind excluded by the "all" pseudo-type.
const crdKind = "CustomResourceDefinition"

// resourceSelector matches ConfigHub ResourceTypes. Each user-supplied type
// argument resolves to one selector.
type resourceSelector struct {
	// all matches every type except CustomResourceDefinition. Unlike kubectl's
	// "all", which is a short hard-coded list, it really is everything: CRDs are
	// left out only because their schemas swamp any other resource.
	all bool
	// exact is a fully-qualified ResourceType the user typed verbatim.
	exact string
	// group is the required API group ("" for the core group), ignored when
	// anyGroup is set.
	group    string
	anyGroup bool
	// kinds are the accepted Kind spellings, compared case-insensitively.
	kinds []string
}

// resourceTypeFilter is the resolved form of a whole type argument, which may
// name several types (kubectl-style "deploy,svc").
type resourceTypeFilter struct {
	selectors []resourceSelector
}

// parseResourceTypes resolves a comma-separated type argument.
func parseResourceTypes(arg string) (*resourceTypeFilter, error) {
	filter := &resourceTypeFilter{}
	for _, token := range strings.Split(arg, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		selector, err := parseResourceType(token)
		if err != nil {
			return nil, err
		}
		filter.selectors = append(filter.selectors, selector)
	}
	if len(filter.selectors) == 0 {
		return nil, fmt.Errorf("no resource type specified")
	}
	return filter, nil
}

func parseResourceType(token string) (resourceSelector, error) {
	lower := strings.ToLower(token)
	if lower == "all" {
		return resourceSelector{all: true}, nil
	}

	// A slash means the user wrote a ConfigHub ResourceType, or the
	// "<group>/<Kind>" prefix of one.
	if strings.Contains(token, "/") {
		parts := strings.Split(token, "/")
		switch {
		case len(parts) >= 3:
			return resourceSelector{exact: token}, nil
		case isAPIVersion(parts[0]):
			// "v1/Service": a complete core-group ResourceType.
			return resourceSelector{exact: token}, nil
		default:
			// "apps/Deployment": group and Kind, any version.
			return resourceSelector{group: parts[0], kinds: kindSpellings(parts[1])}, nil
		}
	}

	if kind, found := k8sKindAliases[lower]; found {
		return resourceSelector{group: kind.Group, kinds: []string{kind.Kind}}, nil
	}

	// "<name>.<group>" for a type not in the table, e.g. a custom resource:
	// "widgets.example.com".
	if name, group, hasGroup := strings.Cut(token, "."); hasGroup {
		return resourceSelector{group: group, kinds: kindSpellings(name)}, nil
	}

	// An unrecognized bare name matches that Kind in any group.
	return resourceSelector{anyGroup: true, kinds: kindSpellings(token)}, nil
}

// isAPIVersion reports whether a segment looks like a Kubernetes API version
// ("v1", "v2beta3"), which distinguishes the core-group ResourceType
// "v1/Service" from the group/Kind form "apps/Deployment".
func isAPIVersion(segment string) bool {
	if len(segment) < 2 || segment[0] != 'v' || segment[1] < '0' || segment[1] > '9' {
		return false
	}
	rest := strings.TrimLeft(segment[1:], "0123456789")
	return rest == "" || strings.HasPrefix(rest, "alpha") || strings.HasPrefix(rest, "beta")
}

// kindSpellings returns the spellings to accept for a name that isn't in the
// alias table. Kinds are matched case-insensitively, and a trailing "s" is
// optional so both "widget" and "widgets" find Kind "Widget".
func kindSpellings(name string) []string {
	spellings := []string{name}
	if singular := strings.TrimSuffix(name, "s"); singular != name && singular != "" {
		spellings = append(spellings, singular)
	}
	return spellings
}

// splitResourceType splits a ConfigHub ResourceType into its API group and
// Kind. The core group's types have no group segment ("v1/ConfigMap"), so the
// group comes back empty.
func splitResourceType(resourceType string) (group, kind string) {
	parts := strings.Split(resourceType, "/")
	if len(parts) < 2 {
		return "", resourceType
	}
	return strings.Join(parts[:len(parts)-2], "/"), parts[len(parts)-1]
}

// matches reports whether a ResourceType satisfies any of the selectors.
func (f *resourceTypeFilter) matches(resourceType string) bool {
	group, kind := splitResourceType(resourceType)
	for _, s := range f.selectors {
		switch {
		case s.all:
			if kind != crdKind {
				return true
			}
		case s.exact != "":
			if s.exact == resourceType {
				return true
			}
		default:
			if !s.anyGroup && s.group != group {
				continue
			}
			for _, spelling := range s.kinds {
				if strings.EqualFold(spelling, kind) {
					return true
				}
			}
		}
	}
	return false
}

// whereResourceExpression renders the filter as a WhereResource clause the
// server can apply, so that only matching resources come back. It is allowed to
// be broader than matches() — the caller filters the response again — but never
// narrower. An empty result means "no server-side narrowing is possible".
//
// WhereResource literals cannot contain backslashes, so the patterns below use
// unescaped "." (which matches any character) in group names. That is broader
// than intended and therefore safe.
func (f *resourceTypeFilter) whereResourceExpression() string {
	// "all" alone is expressed as a negative match; combined with anything else
	// the union is broader than "all", so no server-side filter applies.
	for _, s := range f.selectors {
		if s.all {
			if len(f.selectors) == 1 {
				return "ConfigHub.ResourceType !~ '/" + crdKind + "$'"
			}
			return ""
		}
	}

	alternatives := make([]string, 0, len(f.selectors))
	for _, s := range f.selectors {
		alternatives = append(alternatives, s.pattern())
	}
	return "ConfigHub.ResourceType ~* '^(" + strings.Join(alternatives, "|") + ")$'"
}

// resourceWhereExpression is whereResourceExpression written against the Resource entity, whose
// ResourceType is a column rather than the ConfigHub.ResourceType path of a WhereResource clause.
// The regular expression is identical; only the operand it applies to differs.
func (f *resourceTypeFilter) resourceWhereExpression() string {
	return strings.TrimPrefix(f.whereResourceExpression(), "ConfigHub.")
}

// pattern renders one selector as a regular-expression alternative, without
// anchors.
func (s resourceSelector) pattern() string {
	if s.exact != "" {
		return s.exact
	}
	var prefix string
	switch {
	case s.anyGroup:
		// Either "version/Kind" (core) or "group/version/Kind".
		prefix = "([^/]+/)?[^/]+/"
	case s.group == "":
		prefix = "[^/]+/"
	default:
		prefix = s.group + "/[^/]+/"
	}
	return prefix + "(" + strings.Join(s.kinds, "|") + ")"
}
