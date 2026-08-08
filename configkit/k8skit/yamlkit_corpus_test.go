// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit_test

import (
	"fmt"
	"maps"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// A corpus of three-way merges, organized by the structure that makes each one hard, and a
// set of properties checked over every case in it.
//
// Coverage is driven by what teams actually vary between an upstream and a downstream
// variant — the things people patch with Kustomize and conditionalize or parameterize in
// Helm charts — because those are exactly the paths a merge has to get right: images,
// replicas, env vars, resource limits, probes, sidecars, node selectors, annotations,
// optional blocks that exist in one environment and not another, and the unkeyed lists
// (args, tolerations, RBAC rules, ingress paths) that have no identity to merge on.
//
// Each case states the values it cares about; the properties in checkMergeProperties are
// checked for every case whether it states them or not, and are where a regression in a
// path nobody thought to assert on shows up.

// ---------------------------------------------------------------------------
// Case definition
// ---------------------------------------------------------------------------

// absent is the wanted "value" for a path that must not exist in the merged result.
const absent = "\x00absent"

type mergeCase struct {
	name string
	// base is the shared ancestor. upstream and downstream are each an edit of it.
	base, upstream, downstream string
	// want maps a path to the value expected in the merged result when subtraction is
	// off — the default merge, in which the upstream change wins on a path both sides
	// changed. Paths are resolved against the merged document, so array indices are
	// positions in the result.
	want map[string]string
	// wantSubtracted overrides individual want entries for the run with subtraction on,
	// in which the downstream's difference from the base wins instead. Paths not
	// mentioned are expected to be unchanged from want.
	wantSubtracted map[string]string
	// wantConflicts are the conflict reasons the merge must report, for the run with
	// subtraction off. Order-insensitive; reasons not listed are not checked.
	wantConflicts []api.ConflictReason
	// wantConflictsSubtracted is the same for the run with subtraction on, which is
	// where Subtracted and DeleteShadowed can arise at all — both are products of
	// comparing the patch with the target's own diff.
	wantConflictsSubtracted []api.ConflictReason
}

// ---------------------------------------------------------------------------
// Document builders
// ---------------------------------------------------------------------------

// dep renders a Deployment from its varying parts, so a case can be written as the small
// difference between a base and an edit of it. Empty fields are omitted.
type dep struct {
	Replicas int
	// Meta is extra metadata YAML, indented 2 spaces.
	Meta string
	// SpecExtra is extra Deployment-spec YAML, indented 2 spaces.
	SpecExtra string
	// PodExtra is extra pod-spec YAML, indented 6 spaces.
	PodExtra string
	// InitContainers and Containers are list YAML, indented 6 spaces.
	InitContainers string
	Containers     string
}

func (d dep) String() string {
	var b strings.Builder
	b.WriteString("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n  namespace: ns\n")
	b.WriteString(d.Meta)
	fmt.Fprintf(&b, "spec:\n  replicas: %d\n", d.Replicas)
	b.WriteString(d.SpecExtra)
	b.WriteString("  selector:\n    matchLabels:\n      app: web\n  template:\n    metadata:\n      labels:\n        app: web\n    spec:\n")
	b.WriteString(d.PodExtra)
	if d.InitContainers != "" {
		b.WriteString("      initContainers:\n")
		b.WriteString(d.InitContainers)
	}
	b.WriteString("      containers:\n")
	b.WriteString(d.Containers)
	return b.String()
}

// oneContainer is the default containers list: a single container named app.
const oneContainer = `      - name: app
        image: nginx:1.19
        ports:
        - containerPort: 8080
          protocol: TCP
        env:
        - name: LOG_LEVEL
          value: info
        - name: REGION
          value: us-east
`

func baseDep() dep {
	return dep{Replicas: 2, Containers: oneContainer}
}

// ---------------------------------------------------------------------------
// The corpus
// ---------------------------------------------------------------------------

func TestMergeCorpusKeyedArrays(t *testing.T) {
	runMergeCases(t, []mergeCase{
		{
			// The bread-and-butter promotion: upstream bumps the image, downstream
			// runs a different replica count. Neither may disturb the other.
			name: "image bump with downstream replicas",
			base: baseDep().String(),
			upstream: func() string {
				d := baseDep()
				d.Containers = strings.Replace(oneContainer, "nginx:1.19", "nginx:1.21", 1)
				return d.String()
			}(),
			downstream: func() string { d := baseDep(); d.Replicas = 5; return d.String() }(),
			want: map[string]string{
				"spec.replicas":                         "5",
				"spec.template.spec.containers.0.image": "nginx:1.21",
			},
		},
		{
			// Sidecar injection upstream, container customized downstream.
			name: "sidecar added upstream",
			base: baseDep().String(),
			upstream: func() string {
				d := baseDep()
				d.Containers = oneContainer + "      - name: sidecar\n        image: envoy:1.28\n"
				return d.String()
			}(),
			downstream: func() string {
				d := baseDep()
				d.Containers = strings.Replace(oneContainer, "value: us-east", "value: eu-west", 1)
				return d.String()
			}(),
			want: map[string]string{
				"spec.template.spec.containers.1.name":        "sidecar",
				"spec.template.spec.containers.0.env.1.value": "eu-west",
				"spec.template.spec.containers.0.image":       "nginx:1.19",
			},
		},
		{
			// An env var added on each side of the same container. Merge keys mean
			// both land; without them one would clobber the other.
			name: "env var added on both sides",
			base: baseDep().String(),
			upstream: func() string {
				d := baseDep()
				d.Containers = oneContainer + "" // container list edited below
				d.Containers = strings.Replace(oneContainer,
					"        - name: REGION\n          value: us-east\n",
					"        - name: REGION\n          value: us-east\n        - name: FEATURE_X\n          value: \"on\"\n", 1)
				return d.String()
			}(),
			downstream: func() string {
				d := baseDep()
				d.Containers = strings.Replace(oneContainer,
					"        - name: REGION\n          value: us-east\n",
					"        - name: REGION\n          value: us-east\n        - name: DEBUG\n          value: \"true\"\n", 1)
				return d.String()
			}(),
			// The reorder pass puts the array in the source's order, so the upstream's
			// new element precedes the downstream's own.
			want: map[string]string{
				"spec.template.spec.containers.0.env.2.name": "FEATURE_X",
				"spec.template.spec.containers.0.env.3.name": "DEBUG",
			},
		},
		{
			// Both sides changed the same env var. Upstream wins by default; the
			// downstream override wins when subtraction is enabled.
			name: "same env var changed on both sides",
			base: baseDep().String(),
			upstream: func() string {
				d := baseDep()
				d.Containers = strings.Replace(oneContainer, "value: info", "value: warn", 1)
				return d.String()
			}(),
			downstream: func() string {
				d := baseDep()
				d.Containers = strings.Replace(oneContainer, "value: info", "value: debug", 1)
				return d.String()
			}(),
			want:           map[string]string{"spec.template.spec.containers.0.env.0.value": "warn"},
			wantSubtracted: map[string]string{"spec.template.spec.containers.0.env.0.value": "debug"},
		},
		{
			// An init container removed upstream while downstream customized a
			// different one. The removal propagates; the customization survives.
			name: "init container removed upstream",
			base: func() string {
				d := baseDep()
				d.InitContainers = "      - name: migrate\n        image: migrate:1.0\n      - name: warm\n        image: warm:1.0\n"
				return d.String()
			}(),
			upstream: func() string {
				d := baseDep()
				d.InitContainers = "      - name: warm\n        image: warm:1.0\n"
				return d.String()
			}(),
			downstream: func() string {
				d := baseDep()
				d.InitContainers = "      - name: migrate\n        image: migrate:1.0\n      - name: warm\n        image: warm:2.0-custom\n"
				return d.String()
			}(),
			want: map[string]string{
				"spec.template.spec.initContainers.0.name":  "warm",
				"spec.template.spec.initContainers.0.image": "warm:2.0-custom",
				"spec.template.spec.initContainers.1.name":  absent,
			},
		},
		{
			// Two ports share a number and differ only in protocol. Keyed on the
			// number alone they are the same element, so an upstream change to one
			// lands on whichever the target happens to hold — and a downstream change
			// to the other is lost. The port's identity is the pair.
			name: "same port number, different protocols",
			base: func() string {
				d := baseDep()
				d.Containers = strings.Replace(oneContainer,
					"        - containerPort: 8080\n          protocol: TCP\n",
					"        - containerPort: 8080\n          protocol: TCP\n          name: web\n"+
						"        - containerPort: 8080\n          protocol: UDP\n          name: quic\n", 1)
				return d.String()
			}(),
			upstream: func() string {
				d := baseDep()
				d.Containers = strings.Replace(oneContainer,
					"        - containerPort: 8080\n          protocol: TCP\n",
					"        - containerPort: 8080\n          protocol: TCP\n          name: web-v2\n"+
						"        - containerPort: 8080\n          protocol: UDP\n          name: quic\n", 1)
				return d.String()
			}(),
			downstream: func() string {
				d := baseDep()
				d.Containers = strings.Replace(oneContainer,
					"        - containerPort: 8080\n          protocol: TCP\n",
					"        - containerPort: 8080\n          protocol: TCP\n          name: web\n"+
						"        - containerPort: 8080\n          protocol: UDP\n          name: quic-custom\n", 1)
				return d.String()
			}(),
			want: map[string]string{
				"spec.template.spec.containers.0.ports.0.name":     "web-v2",
				"spec.template.spec.containers.0.ports.0.protocol": "TCP",
				"spec.template.spec.containers.0.ports.1.name":     "quic-custom",
				"spec.template.spec.containers.0.ports.1.protocol": "UDP",
			},
		},
		{
			// A volume and its mount added together upstream.
			name: "volume and volumeMount added upstream",
			base: baseDep().String(),
			upstream: func() string {
				d := baseDep()
				d.PodExtra = "      volumes:\n      - name: cache\n        emptyDir: {}\n"
				d.Containers = oneContainer + "        volumeMounts:\n        - name: cache\n          mountPath: /cache\n"
				return d.String()
			}(),
			downstream: func() string { d := baseDep(); d.Replicas = 4; return d.String() }(),
			want: map[string]string{
				"spec.template.spec.volumes.0.name":                        "cache",
				"spec.template.spec.containers.0.volumeMounts.0.mountPath": "/cache",
				"spec.replicas": "4",
			},
		},
	})
}

func TestMergeCorpusUnkeyedArrays(t *testing.T) {
	runMergeCases(t, []mergeCase{
		{
			// args has no merge key: elements are identified positionally, which is
			// why inserting one in the middle used to renumber every downstream
			// customization after it.
			name: "arg inserted in the middle",
			base: func() string {
				d := baseDep()
				d.Containers = oneContainer + "        args:\n        - --a\n        - --b\n        - --c\n"
				return d.String()
			}(),
			upstream: func() string {
				d := baseDep()
				d.Containers = oneContainer + "        args:\n        - --a\n        - --new\n        - --b\n        - --c\n"
				return d.String()
			}(),
			downstream: func() string {
				d := baseDep()
				d.Containers = oneContainer + "        args:\n        - --a\n        - --b=custom\n        - --c\n"
				return d.String()
			}(),
			want: map[string]string{
				"spec.template.spec.containers.0.args.0": "--a",
				"spec.template.spec.containers.0.args.1": "--new",
				"spec.template.spec.containers.0.args.2": "--b=custom",
				"spec.template.spec.containers.0.args.3": "--c",
			},
			// Subtraction leaves the insertion alone. It compares the patch and the
			// downstream diff path by path, and before anchors both the insertion and
			// the downstream's edit to a different element were "args.1", so the one was
			// taken for the other and the insertion was subtracted away. The anchors
			// name the two elements by their content, so they no longer collide.
		},
		{
			name: "toleration removed upstream",
			base: func() string {
				d := baseDep()
				d.PodExtra = "      tolerations:\n      - key: spot\n        effect: NoSchedule\n      - key: gpu\n        effect: NoSchedule\n"
				return d.String()
			}(),
			upstream: func() string {
				d := baseDep()
				d.PodExtra = "      tolerations:\n      - key: gpu\n        effect: NoSchedule\n"
				return d.String()
			}(),
			downstream: func() string {
				d := baseDep()
				d.PodExtra = "      tolerations:\n      - key: spot\n        effect: NoSchedule\n      - key: gpu\n        effect: NoExecute\n"
				return d.String()
			}(),
			want: map[string]string{
				"spec.template.spec.tolerations.0.key":    "gpu",
				"spec.template.spec.tolerations.0.effect": "NoExecute",
				"spec.template.spec.tolerations.1.key":    absent,
			},
		},
		{
			// RBAC rules are an unkeyed list of maps whose elements contain lists.
			name: "rbac rule appended upstream, verbs customized downstream",
			base: `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: app
  namespace: ns
rules:
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get", "list"]
`,
			upstream: `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: app
  namespace: ns
rules:
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get", "list"]
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get"]
`,
			downstream: `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: app
  namespace: ns
rules:
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get", "list", "watch"]
`,
			want: map[string]string{
				"rules.0.verbs.2":     "watch",
				"rules.1.resources.0": "secrets",
			},
		},
	})
}

// TestMergeCorpusCustomResources covers a CRD whose spec holds an unkeyed array of maps.
// The resource provider declares no merge key for it — it cannot, since it knows nothing
// about the type — so the elements are matched by alignment and named by anchor. This is
// the shape that started the work: a Traefik IngressRoute with two routes, both customized
// downstream, whose first route is dropped upstream.
// TestMergeCorpusCommandLineArgs covers the most common unkeyed array in a Kubernetes
// workload: a container's args. The list in test-data/traefik-deployment.yaml is the shape
// these are drawn from — twenty-odd pflags-style flags, most with an inline value, a few
// bare.
//
// A flag with an inline value carries its own identity: `--log.level=INFO` and
// `--log.level=DEBUG` are the same flag. That is what lets a merge find the flag a variant
// customized wherever either side has since moved it, instead of writing over whatever now
// occupies its old index. Bare flags, separate flag/value pairs, positional arguments and
// the `--` separator have no such identity and fall back to position, which is what a
// textual patch does too.
func TestMergeCorpusCommandLineArgs(t *testing.T) {
	// Drawn from test-data/traefik-deployment.yaml.
	traefikArgs := []string{
		"--entryPoints.metrics.address=:9100/tcp",
		"--entryPoints.web.address=:8000/tcp",
		"--entryPoints.websecure.address=:8443/tcp",
		"--api.dashboard=true",
		"--ping=true",
		"--providers.kubernetescrd",
		"--providers.kubernetesingress",
		"--log.level=INFO",
		"--accesslog=true",
		"--accesslog.format=json",
	}
	withArgs := func(args ...string) string {
		var b strings.Builder
		b.WriteString("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: traefik\n  namespace: ns\nspec:\n  template:\n    spec:\n      containers:\n      - name: traefik\n        image: traefik:v3.3\n        args:\n")
		for _, arg := range args {
			fmt.Fprintf(&b, "        - %q\n", arg)
		}
		return b.String()
	}
	replaceArg := func(args []string, old, replacement string) []string {
		out := append([]string(nil), args...)
		for i, arg := range out {
			if arg == old {
				out[i] = replacement
			}
		}
		return out
	}
	removeArg := func(args []string, drop string) []string {
		out := []string{}
		for _, arg := range args {
			if arg != drop {
				out = append(out, arg)
			}
		}
		return out
	}
	insertArgAt := func(args []string, index int, arg string) []string {
		out := append([]string(nil), args[:index]...)
		out = append(out, arg)
		return append(out, args[index:]...)
	}
	argPath := func(index int) string {
		return fmt.Sprintf("spec.template.spec.containers.0.args.%d", index)
	}

	runMergeCases(t, []mergeCase{
		{
			// The everyday case: each side tuned a different flag.
			name:       "each side changed a different flag",
			base:       withArgs(traefikArgs...),
			upstream:   withArgs(replaceArg(traefikArgs, "--log.level=INFO", "--log.level=DEBUG")...),
			downstream: withArgs(replaceArg(traefikArgs, "--api.dashboard=true", "--api.dashboard=false")...),
			want: map[string]string{
				argPath(3): "--api.dashboard=false",
				argPath(7): "--log.level=DEBUG",
			},
		},
		{
			// Both sides changed the same flag. The upstream wins by default; the
			// downstream's own value wins when subtraction is on.
			name:           "both sides changed the same flag",
			base:           withArgs(traefikArgs...),
			upstream:       withArgs(replaceArg(traefikArgs, "--log.level=INFO", "--log.level=DEBUG")...),
			downstream:     withArgs(replaceArg(traefikArgs, "--log.level=INFO", "--log.level=WARN")...),
			want:           map[string]string{argPath(7): "--log.level=DEBUG"},
			wantSubtracted: map[string]string{argPath(7): "--log.level=WARN"},
		},
		{
			// A flag added upstream, ahead of one the downstream customized.
			name:       "flag added upstream ahead of a customized one",
			base:       withArgs(traefikArgs...),
			upstream:   withArgs(insertArgAt(traefikArgs, 3, "--metrics.prometheus=true")...),
			downstream: withArgs(replaceArg(traefikArgs, "--log.level=INFO", "--log.level=WARN")...),
			want: map[string]string{
				argPath(3): "--metrics.prometheus=true",
				argPath(8): "--log.level=WARN",
			},
		},
		{
			// A flag removed upstream, ahead of one the downstream customized.
			name:       "flag removed upstream ahead of a customized one",
			base:       withArgs(traefikArgs...),
			upstream:   withArgs(removeArg(traefikArgs, "--ping=true")...),
			downstream: withArgs(replaceArg(traefikArgs, "--log.level=INFO", "--log.level=WARN")...),
			want: map[string]string{
				argPath(4):                    "--providers.kubernetescrd",
				argPath(6):                    "--log.level=WARN",
				argPath(len(traefikArgs) - 1): absent,
			},
		},
		{
			// The case an anchor alone cannot reach: the downstream changed a flag's
			// value *and* moved it. No digest matches it any more, and the index it
			// used to occupy now holds something else — which is how an upstream change
			// to that flag used to overwrite an unrelated one and lose it. The flag name
			// is what finds it.
			name:       "downstream changed a flag's value and moved it",
			base:       withArgs(traefikArgs...),
			upstream:   withArgs(replaceArg(traefikArgs, "--log.level=INFO", "--log.level=DEBUG")...),
			downstream: withArgs(append([]string{"--log.level=WARN"}, removeArg(traefikArgs, "--log.level=INFO")...)...),
			want: map[string]string{
				argPath(0): "--log.level=DEBUG",
				argPath(1): "--entryPoints.metrics.address=:9100/tcp",
				argPath(8): "--accesslog=true",
				argPath(9): "--accesslog.format=json",
			},
			// With subtraction the downstream's own value for that flag wins, and the
			// identity is what makes the two sides agree they are talking about it.
			wantSubtracted: map[string]string{argPath(0): "--log.level=WARN"},
		},
		{
			// Bare flags have no inline value to separate a name from, so they are
			// identified by their content like any other scalar.
			name:       "bare flag removed upstream",
			base:       withArgs(traefikArgs...),
			upstream:   withArgs(removeArg(traefikArgs, "--providers.kubernetesingress")...),
			downstream: withArgs(replaceArg(traefikArgs, "--accesslog.format=json", "--accesslog.format=common")...),
			want: map[string]string{
				argPath(5): "--providers.kubernetescrd",
				argPath(6): "--log.level=INFO",
				argPath(8): "--accesslog.format=common",
			},
		},
		{
			// A flag and its value as separate elements, which is the harder shape: the
			// value is an element of its own with nothing to identify it but its
			// position after the flag. Position is what a textual patch uses too.
			name:       "separate flag and value, upstream changes the value",
			base:       withArgs("--provider", "kubernetes", "--log-level", "info", "--port", "8080"),
			upstream:   withArgs("--provider", "kubernetes", "--log-level", "debug", "--port", "8080"),
			downstream: withArgs("--provider", "kubernetes", "--log-level", "info", "--port", "9090"),
			want: map[string]string{
				argPath(3): "debug",
				argPath(5): "9090",
			},
		},
		{
			// A flag/value pair inserted upstream, while the downstream changed a later
			// value.
			name:       "separate flag and value, upstream inserts a pair",
			base:       withArgs("--provider", "kubernetes", "--log-level", "info", "--port", "8080"),
			upstream:   withArgs("--provider", "kubernetes", "--verbose", "true", "--log-level", "info", "--port", "8080"),
			downstream: withArgs("--provider", "kubernetes", "--log-level", "warn", "--port", "8080"),
			want: map[string]string{
				argPath(2): "--verbose",
				argPath(3): "true",
				argPath(5): "warn",
			},
		},
		{
			// Positional arguments and the `--` separator, none of which carry identity.
			name:       "positional arguments and the -- separator",
			base:       withArgs("serve", "--config", "/etc/app.yaml", "--", "extra1", "extra2"),
			upstream:   withArgs("serve", "--config", "/etc/app.yaml", "--verbose", "--", "extra1", "extra2"),
			downstream: withArgs("serve", "--config", "/etc/custom.yaml", "--", "extra1", "extra2"),
			want: map[string]string{
				argPath(2): "/etc/custom.yaml",
				argPath(3): "--verbose",
				argPath(4): "--",
				argPath(5): "extra1",
			},
		},
	})
}

func TestMergeCorpusCustomResources(t *testing.T) {
	route := func(match, service string, priority int) string {
		return fmt.Sprintf("  - match: %s\n    kind: Rule\n    priority: %d\n    services:\n    - name: %s\n      port: 80\n",
			match, priority, service)
	}
	ingressRoute := func(routes ...string) string {
		return "apiVersion: traefik.io/v1alpha1\nkind: IngressRoute\nmetadata:\n  name: web\n  namespace: ns\nspec:\n  entryPoints:\n  - websecure\n  routes:\n" +
			strings.Join(routes, "") + "  tls:\n    certResolver: letsencrypt\n"
	}

	runMergeCases(t, []mergeCase{
		{
			// The reported case. Both routes' match rules differ downstream — host and
			// path expressions are exactly what varies between variants — so no anchor
			// matches and every element falls back to its index. The removal still has
			// to take the route it means and leave the other one alone.
			name:       "route removed upstream, both routes customized downstream",
			base:       ingressRoute(route("Host(`a.example.com`)", "svc-a", 100), route("Host(`b.example.com`)", "svc-b", 50)),
			upstream:   ingressRoute(route("Host(`b.example.com`)", "svc-b", 50)),
			downstream: ingressRoute(route("Host(`a.internal`)", "svc-a", 100), route("Host(`b.internal`)", "svc-b", 50)),
			want: map[string]string{
				"spec.routes.0.match":           "Host(`b.internal`)",
				"spec.routes.0.services.0.name": "svc-b",
				"spec.routes.1.match":           absent,
				"spec.tls.certResolver":         "letsencrypt",
			},
		},
		{
			// A route inserted upstream, in front of one the downstream customized.
			name:       "route inserted upstream ahead of a customized one",
			base:       ingressRoute(route("Host(`a.example.com`)", "svc-a", 100)),
			upstream:   ingressRoute(route("Host(`new.example.com`)", "svc-new", 200), route("Host(`a.example.com`)", "svc-a", 100)),
			downstream: ingressRoute(route("Host(`a.internal`)", "svc-a", 100)),
			want: map[string]string{
				"spec.routes.0.match":           "Host(`new.example.com`)",
				"spec.routes.1.match":           "Host(`a.internal`)",
				"spec.routes.1.services.0.name": "svc-a",
			},
		},
		{
			// A field of one route changed upstream while the downstream customized a
			// different field of the same route. The route is unedited apart from that
			// field on the downstream side, so its anchor still matches.
			name:       "route priority changed upstream, service customized downstream",
			base:       ingressRoute(route("Host(`a.example.com`)", "svc-a", 100), route("Host(`b.example.com`)", "svc-b", 50)),
			upstream:   ingressRoute(route("Host(`a.example.com`)", "svc-a", 100), route("Host(`b.example.com`)", "svc-b", 75)),
			downstream: ingressRoute(route("Host(`a.example.com`)", "svc-a", 100), route("Host(`b.example.com`)", "svc-b-custom", 50)),
			want: map[string]string{
				"spec.routes.1.priority":        "75",
				"spec.routes.1.services.0.name": "svc-b-custom",
			},
		},
	})
}

// TestMergeCorpusPolicyRules covers the unkeyed arrays of maps that policy resources are
// made of, where an element is itself full of arrays: NetworkPolicy ingress rules and
// Ingress paths. Nothing in them is a merge key, so they are matched by alignment.
func TestMergeCorpusPolicyRules(t *testing.T) {
	netpol := func(rules ...string) string {
		return "apiVersion: networking.k8s.io/v1\nkind: NetworkPolicy\nmetadata:\n  name: web\n  namespace: ns\nspec:\n  podSelector:\n    matchLabels:\n      app: web\n  policyTypes:\n  - Ingress\n  ingress:\n" +
			strings.Join(rules, "")
	}
	ingressRule := func(fromApp string, port int) string {
		return fmt.Sprintf("  - from:\n    - podSelector:\n        matchLabels:\n          app: %s\n    ports:\n    - protocol: TCP\n      port: %d\n", fromApp, port)
	}

	ingress := func(paths ...string) string {
		return "apiVersion: networking.k8s.io/v1\nkind: Ingress\nmetadata:\n  name: web\n  namespace: ns\nspec:\n  rules:\n  - host: example.com\n    http:\n      paths:\n" +
			strings.Join(paths, "")
	}
	httpPath := func(path, service string) string {
		return fmt.Sprintf("      - path: %s\n        pathType: Prefix\n        backend:\n          service:\n            name: %s\n            port:\n              number: 80\n", path, service)
	}

	runMergeCases(t, []mergeCase{
		{
			name:       "network policy rule added upstream, port customized downstream",
			base:       netpol(ingressRule("api", 8080)),
			upstream:   netpol(ingressRule("api", 8080), ingressRule("metrics", 9090)),
			downstream: netpol(ingressRule("api", 8443)),
			want: map[string]string{
				"spec.ingress.0.ports.0.port":                       "8443",
				"spec.ingress.1.from.0.podSelector.matchLabels.app": "metrics",
			},
		},
		{
			name:       "network policy rule removed upstream, another customized downstream",
			base:       netpol(ingressRule("api", 8080), ingressRule("metrics", 9090)),
			upstream:   netpol(ingressRule("metrics", 9090)),
			downstream: netpol(ingressRule("api", 8080), ingressRule("metrics", 9443)),
			want: map[string]string{
				"spec.ingress.0.from.0.podSelector.matchLabels.app": "metrics",
				"spec.ingress.0.ports.0.port":                       "9443",
				"spec.ingress.1.from":                               absent,
			},
		},
		{
			name:       "ingress path added upstream, backend customized downstream",
			base:       ingress(httpPath("/", "web")),
			upstream:   ingress(httpPath("/", "web"), httpPath("/api", "api")),
			downstream: ingress(httpPath("/", "web-custom")),
			want: map[string]string{
				"spec.rules.0.http.paths.0.backend.service.name": "web-custom",
				"spec.rules.0.http.paths.1.path":                 "/api",
			},
		},
	})
}

func TestMergeCorpusMaps(t *testing.T) {
	runMergeCases(t, []mergeCase{
		{
			// Annotations merge per key, so an annotation added on each side survives.
			name: "annotation added on both sides",
			base: baseDep().String(),
			upstream: func() string {
				d := baseDep()
				d.Meta = "  annotations:\n    owner: platform\n"
				return d.String()
			}(),
			downstream: func() string {
				d := baseDep()
				d.Meta = "  annotations:\n    cost-center: \"42\"\n"
				return d.String()
			}(),
			want: map[string]string{
				"metadata.annotations.owner":       "platform",
				"metadata.annotations.cost-center": "42",
			},
			// Both keys survive subtraction too. Neither side's annotations existed in
			// the base, and a map that did not exist used to be diffed as one Add of
			// the whole map — so the downstream owned metadata.annotations entire and
			// the upstream's key was subtracted along with it. A map's keys are its
			// identity, so the addition is recorded per key and each side owns only
			// what it added.
		},
		{
			// The config-checksum annotation people put on pod templates changes on
			// every upstream release and must propagate.
			name: "pod template checksum annotation churns upstream",
			base: func() string {
				d := baseDep()
				d.Containers = oneContainer
				return strings.Replace(d.String(), "    metadata:\n      labels:",
					"    metadata:\n      annotations:\n        checksum/config: aaa\n      labels:", 1)
			}(),
			upstream: func() string {
				d := baseDep()
				return strings.Replace(d.String(), "    metadata:\n      labels:",
					"    metadata:\n      annotations:\n        checksum/config: bbb\n      labels:", 1)
			}(),
			downstream: func() string {
				d := baseDep()
				d.Replicas = 3
				return strings.Replace(d.String(), "    metadata:\n      labels:",
					"    metadata:\n      annotations:\n        checksum/config: aaa\n      labels:", 1)
			}(),
			want: map[string]string{
				"spec.template.metadata.annotations.checksum/config": "bbb",
				"spec.replicas": "3",
			},
		},
		{
			// A map created on one side and extended on the other, which is the
			// asymmetric half of the case above: the downstream created the
			// annotations block, and the upstream's later annotation has to land in
			// the map the downstream made rather than be blocked by it.
			name: "annotation added upstream to a map the downstream created",
			base: baseDep().String(),
			upstream: func() string {
				d := baseDep()
				d.Meta = "  annotations:\n    owner: platform\n"
				return d.String()
			}(),
			downstream: func() string {
				d := baseDep()
				d.Meta = "  annotations:\n    cost-center: \"42\"\n"
				d.Replicas = 4
				return d.String()
			}(),
			want: map[string]string{
				"metadata.annotations.owner":       "platform",
				"metadata.annotations.cost-center": "42",
				"spec.replicas":                    "4",
			},
		},
		{
			name: "node selector key added upstream",
			base: baseDep().String(),
			upstream: func() string {
				d := baseDep()
				d.PodExtra = "      nodeSelector:\n        kubernetes.io/os: linux\n"
				return d.String()
			}(),
			downstream: func() string {
				d := baseDep()
				d.PodExtra = "      nodeSelector:\n        pool: batch\n"
				return d.String()
			}(),
			want: map[string]string{
				"spec.template.spec.nodeSelector.kubernetes~1io/os": "linux",
				"spec.template.spec.nodeSelector.pool":              "batch",
			},
			// Per-key ownership of a created map, as in the annotations case above:
			// each side keeps its own selector key.
		},
	})
}

// TestMergeCorpusCrossCuttingRewrites covers the rewrites a variant applies to every
// resource it owns — a namespace, a set of common labels, a name prefix. They are the
// changes most likely to defeat resource matching, because they alter the very fields the
// match is made on.
func TestMergeCorpusCrossCuttingRewrites(t *testing.T) {
	deployment := func(name, namespace, image string, extraMeta string) string {
		return fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
%sspec:
  replicas: 2
  template:
    spec:
      containers:
      - name: app
        image: %s
`, name, namespace, extraMeta, image)
	}
	service := func(name, namespace string, port int, extraMeta string) string {
		return fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
%sspec:
  ports:
  - port: %d
    protocol: TCP
`, name, namespace, extraMeta, port)
	}
	const commonLabels = "  labels:\n    owner: platform\n    tier: web\n"

	runMergeCases(t, []mergeCase{
		{
			// The downstream moved everything to its own namespace. The resource name
			// carries the namespace as a scope, so the names differ — but matching
			// falls back to the name without the scope, which still agrees.
			name:       "downstream moved the resource to its own namespace",
			base:       deployment("web", "ns", "nginx:1.19", ""),
			upstream:   deployment("web", "ns", "nginx:1.21", ""),
			downstream: deployment("web", "prod", "nginx:1.19", ""),
			want: map[string]string{
				"metadata.namespace":                    "prod",
				"spec.template.spec.containers.0.image": "nginx:1.21",
			},
		},
		{
			// commonLabels, applied to every resource the downstream owns. They are a
			// map the upstream never touches, so they survive, and the upstream's own
			// changes still land on both resources.
			name: "downstream applied common labels to every resource",
			base: deployment("web", "ns", "nginx:1.19", "") + "---\n" +
				service("web", "ns", 80, ""),
			upstream: deployment("web", "ns", "nginx:1.21", "") + "---\n" +
				service("web", "ns", 8080, ""),
			downstream: deployment("web", "ns", "nginx:1.19", commonLabels) + "---\n" +
				service("web", "ns", 80, commonLabels),
			want: map[string]string{
				"metadata.labels.owner":                 "platform",
				"metadata.labels.tier":                  "web",
				"spec.template.spec.containers.0.image": "nginx:1.21",
			},
		},
		{
			// A name prefix, with no accumulated provenance to match on. The patch names
			// a resource this Unit does not have, so nothing lands — and since a
			// resource-level mutation with nowhere to land is reported rather than
			// dropped, the merge says so. TestMergeAcrossADownstreamRename covers the
			// other half, where the Unit's own history carries the old name.
			name:       "downstream renamed the resource, with no history to match on",
			base:       deployment("web", "ns", "nginx:1.19", ""),
			upstream:   deployment("web", "ns", "nginx:1.21", ""),
			downstream: deployment("prod-web", "ns", "nginx:1.19", ""),
			want: map[string]string{
				"metadata.name":                         "prod-web",
				"spec.template.spec.containers.0.image": "nginx:1.19",
			},
			wantConflicts: []api.ConflictReason{api.ConflictReasonUnresolvedPath},
		},
	})
}

// TestMergeAcrossADownstreamRename covers the rename that a variant actually performs: one
// recorded in the Unit's own accumulated mutations. Matching a patch to a target resource
// tries the name, then the name without its scope, then the aliases the target's
// MutationSources carry — which is where a renamed resource's previous name lives. Without
// that history the patch has nothing to match on and the change is withheld and reported
// (see the corpus case above); with it, the merge lands on the renamed resource.
func TestMergeAcrossADownstreamRename(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	deployment := func(name, image string) string {
		return fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: ns
spec:
  replicas: 2
  template:
    spec:
      containers:
      - name: app
        image: %s
`, name, image)
	}
	base := deployment("web", "nginx:1.19")
	upstream := deployment("web", "nginx:1.21")
	downstream := deployment("prod-web", "nginx:1.19")

	patch, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, upstream), 1, provider)
	require.NoError(t, err)

	// The downstream's diff from the base is what its MutationSources hold, and a rename
	// records both names as aliases.
	protection, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, downstream), 2, provider)
	require.NoError(t, err)
	require.Len(t, protection, 1)
	assert.Contains(t, protection[0].AliasesWithoutScopes, api.ResourceName("web"),
		"the previous name is what a later merge matches on")

	merged, conflicts, err := yamlkit.PatchMutations(parseCorpus(t, downstream), protection, patch, nil, provider, nil)
	require.NoError(t, err)

	assert.Equal(t, "prod-web", merged[0].Path("metadata.name").Data(),
		"the downstream keeps its own name")
	assert.Equal(t, "nginx:1.21", merged[0].Path("spec.template.spec.containers.0.image").Data(),
		"and still receives the upstream change")
	assert.Empty(t, conflicts)
}

func TestMergeCorpusOptionalBlocks(t *testing.T) {
	runMergeCases(t, []mergeCase{
		{
			// The Helm "resources: {}" story: the block exists in prod and not in dev,
			// and upstream starts setting it.
			name: "resources block added upstream",
			base: baseDep().String(),
			upstream: func() string {
				d := baseDep()
				d.Containers = oneContainer + "        resources:\n          limits:\n            cpu: \"1\"\n            memory: 512Mi\n"
				return d.String()
			}(),
			downstream: func() string { d := baseDep(); d.Replicas = 3; return d.String() }(),
			want: map[string]string{
				"spec.template.spec.containers.0.resources.limits.cpu": "1",
				"spec.replicas": "3",
			},
		},
		{
			// Upstream removes a block the downstream customized inside. The removal
			// still applies — the parent is gone, so the downstream's edits have
			// nowhere to live — and the merge reports what it displaced.
			name: "resources block removed upstream after downstream customized it",
			base: func() string {
				d := baseDep()
				d.Containers = oneContainer + "        resources:\n          limits:\n            cpu: \"1\"\n            memory: 512Mi\n"
				return d.String()
			}(),
			upstream: baseDep().String(),
			downstream: func() string {
				d := baseDep()
				d.Containers = oneContainer + "        resources:\n          limits:\n            cpu: \"4\"\n            memory: 512Mi\n"
				return d.String()
			}(),
			want: map[string]string{
				"spec.template.spec.containers.0.resources": absent,
			},
			// The removal wins even with subtraction on: the parent is gone, so the
			// downstream's edit inside it has nowhere to live. What subtraction adds is
			// the report — DeleteShadowed names each displaced target mutation — which
			// is only computable when the target's diff is supplied.
			wantConflictsSubtracted: []api.ConflictReason{api.ConflictReasonDeleteShadowed},
		},
		{
			name: "probes added upstream",
			base: baseDep().String(),
			upstream: func() string {
				d := baseDep()
				d.Containers = oneContainer + "        readinessProbe:\n          httpGet:\n            path: /healthz\n            port: 8080\n          periodSeconds: 10\n"
				return d.String()
			}(),
			downstream: func() string {
				d := baseDep()
				d.Containers = strings.Replace(oneContainer, "nginx:1.19", "nginx:1.19-custom", 1)
				return d.String()
			}(),
			want: map[string]string{
				"spec.template.spec.containers.0.readinessProbe.httpGet.path": "/healthz",
				"spec.template.spec.containers.0.image":                       "nginx:1.19-custom",
			},
		},
	})
}

func TestMergeCorpusMultiResource(t *testing.T) {
	const service = `apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: ns
spec:
  type: ClusterIP
  ports:
  - port: 80
    targetPort: 8080
    protocol: TCP
`
	const hpa = `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: web
  namespace: ns
spec:
  minReplicas: 2
  maxReplicas: 10
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: web
`
	runMergeCases(t, []mergeCase{
		{
			// A sibling resource appears upstream while the downstream has edited the
			// resources it already had.
			name:     "hpa added upstream",
			base:     baseDep().String() + "---\n" + service,
			upstream: baseDep().String() + "---\n" + service + "---\n" + hpa,
			downstream: func() string {
				d := baseDep()
				d.Replicas = 6
				return d.String() + "---\n" + strings.Replace(service, "type: ClusterIP", "type: LoadBalancer", 1)
			}(),
			want: map[string]string{
				"spec.replicas": "6",
			},
		},
		{
			// The Service's ports are keyed on port number; changing targetPort
			// upstream must find the same element downstream.
			name:       "service target port changed upstream",
			base:       service,
			upstream:   strings.Replace(service, "targetPort: 8080", "targetPort: 9090", 1),
			downstream: strings.Replace(service, "type: ClusterIP", "type: NodePort", 1),
			want: map[string]string{
				"spec.ports.0.targetPort": "9090",
				"spec.type":               "NodePort",
			},
		},
	})
}

// TestMergeCorpusRegressions pins the merges that were producing wrong output, each of
// which the property harness or the fuzzer surfaced rather than a bug report. They are
// kept as their own group because what each one is guarding is a specific way for a
// source-side index to land somewhere it does not belong in a target that has drifted.
func TestMergeCorpusRegressions(t *testing.T) {
	envDoc := func(entries ...string) string {
		d := baseDep()
		var b strings.Builder
		b.WriteString("      - name: app\n        image: nginx:1.19\n        env:\n")
		for _, entry := range entries {
			name, value, _ := strings.Cut(entry, "=")
			fmt.Fprintf(&b, "        - name: %s\n          value: %q\n", name, value)
		}
		d.Containers = b.String()
		return d.String()
	}
	argsDoc := func(args ...string) string {
		d := baseDep()
		var b strings.Builder
		b.WriteString("      - name: app\n        image: nginx:1.19\n        args:\n")
		for _, arg := range args {
			fmt.Fprintf(&b, "        - %s\n", arg)
		}
		d.Containers = b.String()
		return d.String()
	}

	runMergeCases(t, []mergeCase{
		{
			// The upstream adds a third element to an array the downstream has trimmed
			// to one. The Add's source-side index is two positions past the end of the
			// target's array, and writing there asks for a gap the setter cannot
			// express: it used to replace the whole document with a null.
			name:       "element added past the end of a shortened array",
			base:       envDoc("VAR_0=v2", "VAR_1=v4"),
			upstream:   envDoc("VAR_0=e13", "VAR_1=v4", "NEW=n1"),
			downstream: envDoc("VAR_1=v4"),
			want: map[string]string{
				"spec.template.spec.containers.0.env.0.name": "VAR_1",
				"spec.template.spec.containers.0.env.1.name": "NEW",
				"spec.template.spec.containers.0.env.2.name": absent,
			},
		},
		{
			// Each side removed the element the other kept. The upstream's update to
			// VAR_1 cannot land — the downstream deleted it — and the fallback index
			// for its merge key is the append position, so the update used to be
			// applied there, producing an element with a value and no name.
			name:       "each side removed the element the other changed",
			base:       envDoc("VAR_0=v1", "VAR_1=v4"),
			upstream:   envDoc("VAR_1=e57"),
			downstream: envDoc("VAR_0=e64"),
			want: map[string]string{
				"spec.template.spec.containers.0.env.0": absent,
			},
			wantConflicts: []api.ConflictReason{api.ConflictReasonUnresolvedPath},
		},
		{
			// Several insertions in one patch, into an array the downstream is shorter
			// than. Each insertion renumbers the ones after it, so they have to be
			// applied front to back and an index past the end has to append rather
			// than be clamped ahead of an insertion already made.
			name:       "several insertions into a shorter array keep their order",
			base:       argsDoc("--flag-0", "--flag-1"),
			upstream:   argsDoc("--added-a", "--flag-0", "--added-b", "--added-c", "--flag-1"),
			downstream: argsDoc("--flag-0"),
			want: map[string]string{
				"spec.template.spec.containers.0.args.0": "--added-a",
				"spec.template.spec.containers.0.args.1": "--flag-0",
				"spec.template.spec.containers.0.args.2": "--added-b",
				"spec.template.spec.containers.0.args.3": "--added-c",
			},
		},
		{
			// Both sides changed VAR_2, which sits at a different index on each side
			// because the downstream removed an earlier element. Subtraction compares
			// the two diffs path by path, and the paths carry that index, so the
			// overlap used to go unnoticed and the downstream's override was
			// overwritten by a merge that had been asked to preserve it.
			name:       "subtraction sees an element that moved",
			base:       envDoc("VAR_0=v1", "VAR_1=v0", "VAR_2=v1"),
			upstream:   envDoc("VAR_0=v1", "VAR_1=v0", "VAR_2=e11"),
			downstream: envDoc("VAR_0=v1", "VAR_2=e28"),
			want: map[string]string{
				"spec.template.spec.containers.0.env.1.value": "e11",
			},
			wantSubtracted: map[string]string{
				"spec.template.spec.containers.0.env.1.value": "e28",
			},
			wantConflictsSubtracted: []api.ConflictReason{api.ConflictReasonSubtracted},
		},
	})
}

func TestMergeCorpusOpaqueStrings(t *testing.T) {
	configMap := func(body string) string {
		return "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n  namespace: ns\ndata:\n  app.conf: |\n" + body
	}
	annotated := func(body string) string {
		return "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n  namespace: ns\n  annotations:\n    kubectl.kubernetes.io/last-applied-configuration: |\n" +
			body + "data:\n  keep: \"1\"\n"
	}
	embedded := func(replicas int, image string) string {
		return fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n  namespace: ns\ndata:\n  settings.json: '{\"image\": \"%s\", \"replicas\": %d}'\n", image, replicas)
	}

	runMergeCases(t, []mergeCase{
		{
			// A large annotation is an opaque string like any other, and merges by line.
			name:       "different lines of an annotation edited on each side",
			base:       annotated("      first: one\n      second: two\n      third: three\n"),
			upstream:   annotated("      first: ONE\n      second: two\n      third: three\n"),
			downstream: annotated("      first: one\n      second: two\n      third: THREE\n"),
			want: map[string]string{
				"metadata.annotations.kubectl~1kubernetes~1io/last-applied-configuration": "first: ONE\nsecond: two\nthird: THREE",
			},
			wantSubtracted: map[string]string{
				"metadata.annotations.kubectl~1kubernetes~1io/last-applied-configuration": "first: one\nsecond: two\nthird: THREE",
			},
			wantConflictsSubtracted: []api.ConflictReason{api.ConflictReasonSubtracted},
		},
		{
			// Structured data embedded in a string field: each side changed a different
			// key of the same JSON document, and both changes land. The document is
			// re-serialized compactly on the way through, so the author's spacing does
			// not survive — the same kind of fidelity loss as the trailing newline
			// below, and worth fixing for the same reason: a merge rewrites bytes it
			// was not asked to change.
			name:       "different keys of an embedded JSON document edited on each side",
			base:       embedded(2, "nginx:1.19"),
			upstream:   embedded(2, "nginx:1.21"),
			downstream: embedded(5, "nginx:1.19"),
			want: map[string]string{
				"data.settings~1json": `{"image":"nginx:1.21","replicas":5}`,
			},
			// Subtraction removes the upstream's change: both sides changed the one
			// path, which is the whole document.
			wantSubtracted: map[string]string{
				"data.settings~1json": `{"image": "nginx:1.19", "replicas": 5}`,
			},
			wantConflictsSubtracted: []api.ConflictReason{api.ConflictReasonSubtracted},
		},
		{
			// A multi-line string value is merged as text, so an upstream edit to one
			// line and a downstream edit to another both survive.
			name:       "different lines of a config file edited on each side",
			base:       configMap("    listen = 8080\n    workers = 4\n    log = info\n"),
			upstream:   configMap("    listen = 8080\n    workers = 8\n    log = info\n"),
			downstream: configMap("    listen = 8080\n    workers = 4\n    log = debug\n"),
			// Both edits land. The trailing newline of the block scalar is not
			// preserved through the line-level sub-merge, which is a fidelity loss
			// worth fixing but not a merge error.
			want: map[string]string{
				"data.app~1conf": "listen = 8080\nworkers = 8\nlog = debug",
			},
			// Subtraction discards the upstream's edit: both sides changed the same
			// path, and the path is the whole file. The line-level sub-merge that makes
			// the default merge work on this case never runs, because subtraction
			// removes the mutation before the patch is applied.
			wantSubtracted: map[string]string{
				"data.app~1conf": "listen = 8080\nworkers = 4\nlog = debug",
			},
			wantConflictsSubtracted: []api.ConflictReason{api.ConflictReasonSubtracted},
		},
	})
}

// TestMergeHonorsDownstreamDeletions covers deletions as provenance. A path or resource the
// downstream deleted is a local override like any other: it is recorded in MutationSources
// with its protection, and a merge must not re-add what a protected deletion removed — nor
// drop the upstream's change to it without a word.
func TestMergeHonorsDownstreamDeletions(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	const base = `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
  namespace: ns
data:
  keep: "1"
  gone: "2"
---
apiVersion: v1
kind: Secret
metadata:
  name: sec
  namespace: ns
type: Opaque
`
	// The downstream removed a key and a whole resource.
	const downstream = `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
  namespace: ns
data:
  keep: "1"
`
	// The upstream changed both of the things the downstream removed.
	const upstream = `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
  namespace: ns
data:
  keep: "9"
  gone: "22"
---
apiVersion: v1
kind: Secret
metadata:
  name: sec
  namespace: ns
type: Opaque
stringData:
  added: upstream
`

	patch, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, upstream), 1, provider)
	require.NoError(t, err)

	// The downstream's own diff from the base is what its MutationSources hold. Mark the
	// deletions protected and leave everything else overwritable, the way an edit that did
	// not come from a clone or a merge is marked.
	protection, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, downstream), 2, provider)
	require.NoError(t, err)
	sawResourceDelete, sawPathDelete := false, false
	for i := range protection {
		if protection[i].ResourceMutationInfo.MutationType == api.MutationTypeDelete {
			sawResourceDelete = true
		}
		protection[i].ResourceMutationInfo.Protected =
			protection[i].ResourceMutationInfo.MutationType == api.MutationTypeDelete
		for path, info := range protection[i].PathMutationMap {
			if info.MutationType == api.MutationTypeDelete {
				sawPathDelete = true
			}
			info.Protected = info.MutationType == api.MutationTypeDelete
			protection[i].PathMutationMap[path] = info
		}
	}
	assert.True(t, sawPathDelete, "a deleted path is recorded in the accumulated mutations")
	assert.True(t, sawResourceDelete, "a deleted resource is recorded in the accumulated mutations")

	merged, conflicts, err := yamlkit.PatchMutations(parseCorpus(t, downstream), protection, patch, nil, provider, nil)
	require.NoError(t, err)

	assert.Nil(t, merged[0].Path("data.gone"),
		"a key the downstream deleted and protected must not come back")
	assert.Equal(t, "9", merged[0].Path("data.keep").Data(),
		"the rest of the upstream change still lands")
	assert.Len(t, merged, 1, "a resource the downstream deleted must not come back")
	assertConflictReasons(t, conflicts, []api.ConflictReason{api.ConflictReasonProtectedPath})

	// The upstream's change to the deleted resource is reported rather than dropped in
	// silence: once for the resource, once for each path it carried.
	var secretConflicts int
	for _, conflict := range conflicts {
		if conflict.Resource.ResourceName == "ns/sec" {
			secretConflicts++
			assert.Equal(t, api.ConflictReasonProtectedPath, conflict.Reason,
				"the target owns the deletion, so the reason is its protection, not a lost path")
		}
	}
	assert.GreaterOrEqual(t, secretConflicts, 1,
		"a whole resource's worth of upstream change must not disappear without a word")
}

// TestMergeDoesNotResurrectDeletedResource covers the other half of the same rule: an
// upstream Add or Replace of a resource the downstream deleted and protected. The
// unmatched-resource branch of PatchMutations used to append such a resource without
// consulting the protection list at all — the matched branch got it, this one was passed
// nil — so a delete-and-recreate upstream brought back what the downstream had removed.
func TestMergeDoesNotResurrectDeletedResource(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	const base = `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
  namespace: ns
data:
  keep: "1"
---
apiVersion: v1
kind: Secret
metadata:
  name: sec
  namespace: ns
type: Opaque
`
	// The downstream removed the Secret.
	const downstream = `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
  namespace: ns
data:
  keep: "1"
`
	// The upstream removed and recreated it, which is what produces an Add rather than an
	// Update on the patch side.
	const upstream = `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
  namespace: ns
data:
  keep: "1"
---
apiVersion: v1
kind: Secret
metadata:
  name: sec-v2
  namespace: ns
type: Opaque
`
	patch, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, upstream), 1, provider)
	require.NoError(t, err)

	protection, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, downstream), 2, provider)
	require.NoError(t, err)
	protectedDeletion := false
	for i := range protection {
		if protection[i].ResourceMutationInfo.MutationType == api.MutationTypeDelete {
			protection[i].ResourceMutationInfo.Protected = true
			protectedDeletion = true
		}
	}
	require.True(t, protectedDeletion, "the downstream's deletion is what this is about")

	merged, conflicts, err := yamlkit.PatchMutations(parseCorpus(t, downstream), protection, patch, nil, provider, nil)
	require.NoError(t, err)

	assert.Len(t, merged, 1, "the resource the downstream deleted and protected must not come back")
	assertConflictReasons(t, conflicts, []api.ConflictReason{api.ConflictReasonProtectedPath})
}

// TestMergeMarkupIdentifiesAnEditedAndMovedElement covers the identity a person writes.
//
// This is the case nothing the engine infers can reach: the downstream edited the field the
// element would be recognized by *and* moved it. The digest is stale, there is no merge key,
// a route's match expression is exactly what varies between variants so it is no use as an
// identity, and the position now holds a different route. The upstream's change lands on the
// wrong route, silently.
//
// A `# confighub:id=` comment on each route settles it. It is legible, it is the person's
// own, and it is the one mechanism on the list that lets them correct the engine rather than
// work around it. Both sides have to carry it — the source records it in the path and the
// target is matched against it — which is why it belongs upstream, where a clone inherits it.
func TestMergeMarkupIdentifiesAnEditedAndMovedElement(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	route := func(id, match, service string, priority int) string {
		marking := ""
		if id != "" {
			marking = fmt.Sprintf("    # confighub:id=%s\n", id)
		}
		return fmt.Sprintf("  - match: %s\n%s    kind: Rule\n    priority: %d\n    services:\n    - name: %s\n      port: 80\n",
			match, marking, priority, service)
	}
	ingressRoute := func(routes ...string) string {
		return "apiVersion: traefik.io/v1alpha1\nkind: IngressRoute\nmetadata:\n  name: web\n  namespace: ns\nspec:\n  routes:\n" +
			strings.Join(routes, "")
	}
	priorityOf := func(t *testing.T, merged gaby.Container, service string) string {
		t.Helper()
		routes := merged[0].Path("spec.routes")
		require.NotNil(t, routes)
		for _, element := range routes.Children() {
			if fmt.Sprintf("%v", element.Path("services.0.name").Data()) == service {
				return fmt.Sprintf("%v", element.Path("priority").Data())
			}
		}
		t.Fatalf("no route for service %s in %s", service, merged.String())
		return ""
	}

	// The upstream raises the priority of the second route. The downstream customized both
	// match expressions and swapped the order.
	run := func(t *testing.T, idA, idB string) gaby.Container {
		t.Helper()
		base := ingressRoute(
			route(idA, "Host(`a.example.com`)", "svc-a", 100),
			route(idB, "Host(`b.example.com`)", "svc-b", 50))
		upstream := ingressRoute(
			route(idA, "Host(`a.example.com`)", "svc-a", 100),
			route(idB, "Host(`b.example.com`)", "svc-b", 75))
		downstream := ingressRoute(
			route(idB, "Host(`b.internal`)", "svc-b", 50),
			route(idA, "Host(`a.internal`)", "svc-a", 100))

		patch, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, upstream), 1, provider)
		require.NoError(t, err)
		merged, _, err := yamlkit.PatchMutations(parseCorpus(t, downstream), nil, patch, nil, provider, nil)
		require.NoError(t, err)
		return merged
	}

	t.Run("without markup the change lands on the wrong route", func(t *testing.T) {
		merged := run(t, "", "")
		assert.Equal(t, "50", priorityOf(t, merged, "svc-b"),
			"the route the upstream meant keeps its old priority")
		assert.Equal(t, "75", priorityOf(t, merged, "svc-a"),
			"and the route that took its index gets the change instead")
	})

	t.Run("markup finds the route the upstream meant", func(t *testing.T) {
		merged := run(t, "a", "b")
		assert.Equal(t, "75", priorityOf(t, merged, "svc-b"))
		assert.Equal(t, "100", priorityOf(t, merged, "svc-a"))
		assert.Contains(t, merged.String(), "# confighub:id=b",
			"the markup survives the merge, or it would only work once")
	})

	// The default merge runs on the stored protection, so identity has to reach the *diff*
	// and not only the patch. The alignment is order-preserving: two routes that swap places
	// can never both be paired, so without identity the loser is recorded as a removal and an
	// insertion, the whole route becomes the downstream's, and the upstream's change to a
	// field of it is filtered out as an override — with the merge reporting nothing wrong.
	t.Run("markup survives the protection filter", func(t *testing.T) {
		base := ingressRoute(
			route("a", "Host(`a.example.com`)", "svc-a", 100),
			route("b", "Host(`b.example.com`)", "svc-b", 50))
		upstream := ingressRoute(
			route("a", "Host(`a.example.com`)", "svc-a", 100),
			route("b", "Host(`b.example.com`)", "svc-b", 75))
		downstream := ingressRoute(
			route("b", "Host(`b.internal`)", "svc-b", 50),
			route("a", "Host(`a.internal`)", "svc-a", 100))

		patch, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, upstream), 1, provider)
		require.NoError(t, err)
		// The downstream's own diff, with every path it touched protected, which is what
		// the stored protection amount to.
		protection, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, downstream), 2, provider)
		require.NoError(t, err)
		for _, resourceMutation := range protection {
			for path, mutation := range resourceMutation.PathMutationMap {
				mutation.Protected = true
				resourceMutation.PathMutationMap[path] = mutation
			}
		}

		merged, conflicts, err := yamlkit.PatchMutations(parseCorpus(t, downstream), protection, patch, nil, provider, nil)
		require.NoError(t, err)
		assert.Equal(t, "75", priorityOf(t, merged, "svc-b"),
			"the downstream owns the match it rewrote, not the whole route")
		assert.Equal(t, "Host(`b.internal`)", fmt.Sprintf("%v",
			merged[0].Path("spec.routes.0.match").Data()),
			"and its own match survives")
		assertConflictReasons(t, conflicts, nil)
	})
}

// TestMergeCoarsePatchProtectsALeaf covers what a target can protect inside a subtree the
// source replaces wholesale.
//
// Protection is found by walking up from the patch's path to the closest ancestor that has
// an entry. A coarse patch entry is therefore matched by a coarse one, and the finer ones
// underneath never get a say: protecting one field of a block did nothing when the source's
// own diff recorded a single Update of the whole block, and the block was written entire
// with no conflict reported. The advice used to be to protect the whole subtree.
//
// The source's diff goes coarse whenever the base held something other than a mapping at that
// path -- here a placeholder string that both sides replaced with a block, which is how a
// variant and its base diverge on a field that was stubbed out.
func TestMergeCoarsePatchProtectsALeaf(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	route := func(tls string) string {
		return "apiVersion: traefik.io/v1alpha1\nkind: IngressRoute\nmetadata:\n  name: web\n  namespace: ns\nspec:\n  routes:\n  - match: Host(`a.example.com`)\n  tlsConfig: " + tls
	}
	base := route("none\n")
	downstream := route("\n    secretName: local-tls\n")
	upstream := route("\n    secretName: upstream-tls\n    caBundle: up-ca\n")

	patch, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, upstream), 1, provider)
	require.NoError(t, err)
	require.Contains(t, patch[0].PathMutationMap, api.ResolvedPath("spec.tlsConfig"),
		"the case only means anything while the source's entry is the whole block")

	// The downstream's own diff is coarse too. The operator narrows it the way
	// cub unit set-protection does: reopen the block, protect the one field.
	protection, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, downstream), 2, provider)
	require.NoError(t, err)
	for _, resource := range protection {
		for path, mutation := range resource.PathMutationMap {
			mutation.Protected = false
			resource.PathMutationMap[path] = mutation
		}
		resource.PathMutationMap["spec.tlsConfig.secretName"] = api.MutationInfo{
			MutationType: api.MutationTypeUpdate, Index: 2, Protected: true, Value: "local-tls\n",
		}
	}

	merged, conflicts, err := yamlkit.PatchMutations(parseCorpus(t, downstream), protection, patch, nil, provider, nil)
	require.NoError(t, err)
	tls := merged[0].Path("spec.tlsConfig")
	require.NotNil(t, tls)
	assert.Equal(t, "local-tls", fmt.Sprintf("%v", tls.Path("secretName").Data()),
		"the protected field is the downstream's")
	assert.Equal(t, "up-ca", fmt.Sprintf("%v", tls.Path("caBundle").Data()),
		"and the rest of the block still comes from the source")
	assertConflictReasons(t, conflicts, []api.ConflictReason{api.ConflictReasonProtectedPath})
	for _, conflict := range conflicts {
		assert.Equal(t, api.ResolvedPath("spec.tlsConfig.secretName"), conflict.Path,
			"the conflict names the field that was withheld, not the block it sat in")
	}
}

// TestMergeCoarsePatchSplitPreservesRemovals covers the reason the split is a sub-diff of the
// entry's value against the target rather than a walk of the value alone: a coarse Update
// *replaces* a subtree, while a set of leaf Updates merges into it. A field the source
// removed has to keep being removed.
func TestMergeCoarsePatchSplitPreservesRemovals(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	route := func(tls string) string {
		return "apiVersion: traefik.io/v1alpha1\nkind: IngressRoute\nmetadata:\n  name: web\n  namespace: ns\nspec:\n  tlsConfig: " + tls
	}
	base := route("none\n")
	// The downstream has a field the source's block does not.
	downstream := route("\n    secretName: local-tls\n    insecure: true\n")
	upstream := route("\n    secretName: upstream-tls\n    caBundle: up-ca\n")

	patch, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, upstream), 1, provider)
	require.NoError(t, err)
	protection, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, downstream), 2, provider)
	require.NoError(t, err)
	for _, resource := range protection {
		for path, mutation := range resource.PathMutationMap {
			mutation.Protected = false
			resource.PathMutationMap[path] = mutation
		}
		resource.PathMutationMap["spec.tlsConfig.secretName"] = api.MutationInfo{
			MutationType: api.MutationTypeUpdate, Index: 2, Protected: true, Value: "local-tls\n",
		}
	}

	merged, _, err := yamlkit.PatchMutations(parseCorpus(t, downstream), protection, patch, nil, provider, nil)
	require.NoError(t, err)
	tls := merged[0].Path("spec.tlsConfig")
	require.NotNil(t, tls)
	assert.Equal(t, "local-tls", fmt.Sprintf("%v", tls.Path("secretName").Data()))
	assert.Equal(t, "up-ca", fmt.Sprintf("%v", tls.Path("caBundle").Data()))
	assert.Nil(t, tls.Path("insecure"),
		"the source replaced the block, so a field only the target had is still removed")
}

// TestMergeCorpusExclusiveFields covers the sets of sibling fields of which at most one may
// be present — a volume's source, a rollout strategy's rollingUpdate under its type. This is
// what Kubernetes handles with patchStrategy:"retainKeys", and a field-level merge does not
// get it for free: the addition of the new member applies and the removal of the old one is
// withheld against the target's ownership or subtracted as one of the target's own
// differences, leaving a resource the API server rejects.
func TestMergeCorpusExclusiveFields(t *testing.T) {
	volume := func(source string) string {
		return "      volumes:\n      - name: data\n" + source
	}
	emptyDir := "        emptyDir: {}\n"
	emptyDirSized := "        emptyDir:\n          sizeLimit: 2Gi\n"
	configMap := "        configMap:\n          name: settings\n"
	rollingUpdate := func(maxUnavailable int) string {
		return fmt.Sprintf("  strategy:\n    type: RollingUpdate\n    rollingUpdate:\n      maxSurge: 1\n      maxUnavailable: %d\n", maxUnavailable)
	}
	recreate := "  strategy:\n    type: Recreate\n"
	withPod := func(podExtra string) string {
		d := baseDep()
		d.PodExtra = podExtra
		return d.String()
	}
	withStrategy := func(strategy string) string {
		d := baseDep()
		d.SpecExtra = strategy
		return d.String()
	}

	runMergeCases(t, []mergeCase{
		{
			// The most recognized case. The downstream sized the emptyDir, so the
			// upstream's removal of it is withheld — and the volume ends up with two
			// sources unless the addition of configMap clears it.
			name:       "volume source switched upstream, downstream sized the old source",
			base:       withPod(volume(emptyDir)),
			upstream:   withPod(volume(configMap)),
			downstream: withPod(volume(emptyDirSized)),
			want: map[string]string{
				"spec.template.spec.volumes.0.configMap.name":     "settings",
				"spec.template.spec.volumes.0.emptyDir":           absent,
				"spec.template.spec.volumes.0.emptyDir.sizeLimit": absent,
			},
			// With subtraction off the removal of emptyDir applies on its own — the
			// corpus supplies no stored protection, so nothing withholds it.
			//
			// With subtraction on the downstream owns the emptyDir, so the union is
			// settled the same way every other overlap is: the downstream's choice
			// stands and the upstream's configMap is withheld and reported. Applying
			// that conflict later performs the switch, because replayed on its own
			// there is no ownership left to withhold it.
			wantSubtracted: map[string]string{
				"spec.template.spec.volumes.0.configMap.name":     absent,
				"spec.template.spec.volumes.0.emptyDir":           "map[sizeLimit:2Gi]",
				"spec.template.spec.volumes.0.emptyDir.sizeLimit": "2Gi",
			},
			wantConflictsSubtracted: []api.ConflictReason{
				api.ConflictReasonSubtracted, api.ConflictReasonExclusiveWithheld,
			},
		},
		{
			// A switch onto a target that has not touched the volume needs nothing
			// special: the removal applies on its own. Here to show the mechanism does
			// not fire when it is not needed.
			name:       "volume source switched upstream, downstream elsewhere",
			base:       withPod(volume(emptyDir)),
			upstream:   withPod(volume(configMap)),
			downstream: func() string { d := baseDep(); d.PodExtra = volume(emptyDir); d.Replicas = 5; return d.String() }(),
			want: map[string]string{
				"spec.template.spec.volumes.0.configMap.name": "settings",
				"spec.template.spec.volumes.0.emptyDir":       absent,
				"spec.replicas":                               "5",
			},
		},
		{
			// The discriminated shape, and the direction that needs the document to
			// decide rather than the patch: the upstream tunes rollingUpdate, the
			// downstream has already moved to Recreate, and nothing withholds the
			// upstream's change. Recreate permits no rollingUpdate, so the field the
			// update would have re-created has to go.
			name:       "rollingUpdate tuned upstream, downstream moved to Recreate",
			base:       withStrategy(rollingUpdate(0)),
			upstream:   withStrategy(rollingUpdate(3)),
			downstream: withStrategy(recreate),
			want: map[string]string{
				"spec.strategy.type":          "Recreate",
				"spec.strategy.rollingUpdate": absent,
			},
		},
		{
			// The other direction: the upstream moves off RollingUpdate while the
			// downstream had tuned it.
			name:       "strategy moved to Recreate upstream, downstream tuned rollingUpdate",
			base:       withStrategy(rollingUpdate(0)),
			upstream:   withStrategy(recreate),
			downstream: withStrategy(rollingUpdate(2)),
			want: map[string]string{
				"spec.strategy.type":          "Recreate",
				"spec.strategy.rollingUpdate": absent,
			},
			wantConflictsSubtracted: []api.ConflictReason{api.ConflictReasonDeleteShadowed},
		},
	})
}

// TestMergeAnchorLocatesMovedElement covers what the anchor on a positional array element
// is for. The elements of an array with no declared merge key have no identity beyond
// where they sit, so a patch computed against one arrangement used to land on whatever
// occupied that position in the target. The anchor carries a digest of the element's
// content in the merge base, so a target that reordered the array — without editing the
// element itself — is still patched in the right place.
func TestMergeAnchorLocatesMovedElement(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	withArgs := func(args ...string) string {
		d := baseDep()
		var b strings.Builder
		b.WriteString("      - name: app\n        image: nginx:1.19\n        args:\n")
		for _, arg := range args {
			fmt.Fprintf(&b, "        - %s\n", arg)
		}
		d.Containers = b.String()
		return d.String()
	}

	base := withArgs("--alpha", "--beta", "--gamma")
	// Upstream changes the third argument.
	upstream := withArgs("--alpha", "--beta", "--gamma=2")
	// Downstream reordered, leaving every element's content alone.
	downstream := withArgs("--gamma", "--alpha", "--beta")

	patch, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, upstream), 1, provider)
	require.NoError(t, err)

	merged, _, err := yamlkit.PatchMutations(parseCorpus(t, downstream), nil, patch, nil, provider, nil)
	require.NoError(t, err)

	args := merged[0].Path("spec.template.spec.containers.0.args")
	require.NotNil(t, args)
	values := []string{}
	for _, element := range args.Children() {
		values = append(values, fmt.Sprintf("%v", element.Data()))
	}
	assert.Equal(t, []string{"--gamma=2", "--alpha", "--beta"}, values,
		"the change belongs to the element the upstream edited, wherever the downstream moved it")
}

// TestMergeAnchorPrefersRecordedIndexAmongDuplicates covers an array holding several
// identical elements. They all match the same anchor, and the one the patch means is the
// one at the index it recorded — taking the first match would send a change meant for the
// second occurrence to the first.
func TestMergeAnchorPrefersRecordedIndexAmongDuplicates(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	withArgs := func(args ...string) string {
		d := baseDep()
		var b strings.Builder
		b.WriteString("      - name: app\n        image: nginx:1.19\n        args:\n")
		for _, arg := range args {
			fmt.Fprintf(&b, "        - %s\n", arg)
		}
		d.Containers = b.String()
		return d.String()
	}

	base := withArgs("--verbose", "--verbose", "--quiet")
	// Upstream changes the second of the two identical arguments.
	upstream := withArgs("--verbose", "--loud", "--quiet")

	patch, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, upstream), 1, provider)
	require.NoError(t, err)
	merged, _, err := yamlkit.PatchMutations(parseCorpus(t, base), nil, patch, nil, provider, nil)
	require.NoError(t, err)

	args := merged[0].Path("spec.template.spec.containers.0.args")
	require.NotNil(t, args)
	values := []string{}
	for _, element := range args.Children() {
		values = append(values, fmt.Sprintf("%v", element.Data()))
	}
	assert.Equal(t, []string{"--verbose", "--loud", "--quiet"}, values)
}

// TestMergeArrayContextRefusesAShiftedIndex covers the other half of an anchor: what it
// records about the array around its element, and what that stops the resolver from doing.
//
// A positional removal names its element by index. Once the removal has been applied the
// element is gone, and the index it named holds the element that followed it — so replaying
// the same patch used to remove that one too, and the one after it on the next replay. A
// merge is not supposed to be an operation you can only afford to run once.
//
// The anchor records the length of the array the patch was computed against and the digests
// of the elements on either side. An array that has lost an element is no longer that
// length, and the elements around the index are no longer the recorded ones, so the removal
// finds nothing it can vouch for and reports an unresolved path instead of taking a
// bystander with it.
func TestMergeArrayContextRefusesAShiftedIndex(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	withArgs := func(args ...string) string {
		d := baseDep()
		var b strings.Builder
		b.WriteString("      - name: app\n        image: nginx:1.19\n        args:\n")
		for _, arg := range args {
			fmt.Fprintf(&b, "        - %s\n", arg)
		}
		d.Containers = b.String()
		return d.String()
	}
	argsOf := func(t *testing.T, doc gaby.Container) []string {
		t.Helper()
		array := doc[0].Path("spec.template.spec.containers.0.args")
		require.NotNil(t, array)
		values := []string{}
		for _, element := range array.Children() {
			values = append(values, fmt.Sprintf("%v", element.Data()))
		}
		return values
	}

	base := withArgs("--alpha", "--beta", "--gamma")
	// Upstream drops the first argument.
	upstream := withArgs("--beta", "--gamma")
	// The downstream customized every argument, so no digest in the patch matches anything
	// in it — the case the positional-array machinery exists for. The removal still has to
	// take the argument it means.
	downstream := withArgs("--alpha=1", "--beta=2", "--gamma=3")

	patch, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, upstream), 1, provider)
	require.NoError(t, err)

	merged, conflicts, err := yamlkit.PatchMutations(parseCorpus(t, downstream), nil, patch, nil, provider, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"--beta=2", "--gamma=3"}, argsOf(t, merged),
		"the array is the length the patch was computed against, so its indices still mean what the patch meant")
	assert.Empty(t, conflicts)

	// Replaying the same patch onto its own result must do nothing at all.
	again, replayConflicts, err := yamlkit.PatchMutations(reparse(t, merged), nil, patch, nil, provider, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"--beta=2", "--gamma=3"}, argsOf(t, again),
		"the element the removal names is gone, and the argument that took its index is not it")
	assertConflictReasons(t, replayConflicts, []api.ConflictReason{api.ConflictReasonUnresolvedPath})
}

// TestMergeProtectionProtectsMergeKeyedPath covers the default override-preservation
// mechanism on a path inside a merge-keyed array — a container image, which is the single
// most commonly customized field in a variant.
//
// The stored MutationSources name such a path by merge key, while the path being applied
// has been resolved into numeric indices against the target document. Comparing the two
// forms directly never matches, so a protected path under any merge-keyed array used to be
// overwritten silently, with no ProtectedPath conflict to show for it. The e2e coverage
// missed it because the path it protects is spec.replicas, which has no array in it.
func TestMergeProtectionProtectsMergeKeyedPath(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	image := func(tag string) string {
		d := baseDep()
		d.Containers = strings.Replace(oneContainer, "nginx:1.19", tag, 1)
		return d.String()
	}
	base, upstream, downstream := image("nginx:1.19"), image("nginx:1.21"), image("nginx:custom")

	patch, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, upstream), 1, provider)
	require.NoError(t, err)

	// The downstream's own diff from the base is what its MutationSources hold; mark it
	// protected, the way an edit that did not come from a clone or a merge is marked.
	protection, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, downstream), 2, provider)
	require.NoError(t, err)
	for i := range protection {
		for path, info := range protection[i].PathMutationMap {
			info.Protected = true
			protection[i].PathMutationMap[path] = info
		}
	}

	merged, conflicts, err := yamlkit.PatchMutations(parseCorpus(t, downstream), protection, patch, nil, provider, nil)
	require.NoError(t, err)

	assert.Equal(t, "nginx:custom", merged[0].Path("spec.template.spec.containers.0.image").Data(),
		"a protected path inside a merge-keyed array must survive the merge")
	assertConflictReasons(t, conflicts, []api.ConflictReason{api.ConflictReasonProtectedPath})
}

// ---------------------------------------------------------------------------
// Properties
// ---------------------------------------------------------------------------

// TestMergeIdempotenceAndDisjointCommutativity checks the two properties that need more
// than one merge to state: replaying a patch that has already been applied changes
// nothing, and two upstream changes that touch disjoint paths produce the same result in
// either order.
func TestMergeCommutativityForDisjointChanges(t *testing.T) {
	base := baseDep().String()

	// Change A: the image. Change B: an added annotation. Disjoint paths.
	changeA := strings.Replace(base, "nginx:1.19", "nginx:1.21", 1)
	changeB := strings.Replace(base, "metadata:\n  name: web\n  namespace: ns\n",
		"metadata:\n  name: web\n  namespace: ns\n  annotations:\n    owner: platform\n", 1)

	provider := k8skit.NewK8sResourceProvider()
	baseDocs := parseCorpus(t, base)
	patchA, err := yamlkit.ComputeMutations(baseDocs, parseCorpus(t, changeA), 1, provider)
	require.NoError(t, err)
	patchB, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, changeB), 2, provider)
	require.NoError(t, err)

	applyBoth := func(first, second api.ResourceMutationList) string {
		target := parseCorpus(t, base)
		afterFirst, _, err := yamlkit.PatchMutations(target, nil, first, nil, provider, nil)
		require.NoError(t, err)
		afterSecond, _, err := yamlkit.PatchMutations(afterFirst, nil, second, nil, provider, nil)
		require.NoError(t, err)
		return afterSecond.String()
	}

	assert.Equal(t, applyBoth(patchA, patchB), applyBoth(patchB, patchA),
		"merging disjoint changes in either order must produce the same result")
}

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

func runMergeCases(t *testing.T, cases []mergeCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { runMergeCase(t, c) })
	}
}

func runMergeCase(t *testing.T, c mergeCase) {
	t.Helper()
	provider := k8skit.NewK8sResourceProvider()

	patch, err := yamlkit.ComputeMutations(parseCorpus(t, c.base), parseCorpus(t, c.upstream), 1, provider)
	require.NoError(t, err, "computing the upstream patch")
	targetDiff, err := yamlkit.ComputeMutations(parseCorpus(t, c.base), parseCorpus(t, c.downstream), 2, provider)
	require.NoError(t, err, "computing the downstream diff")

	// Default merge: subtraction off, no stored protection, so the upstream change is
	// eligible everywhere and wins on any path both sides touched.
	merged, conflicts, err := yamlkit.PatchMutations(parseCorpus(t, c.downstream), nil, patch, nil, provider, nil)
	require.NoError(t, err)
	assertWantedValues(t, "subtraction off", merged, c.want)
	assertConflictReasons(t, conflicts, c.wantConflicts)
	checkMergeProperties(t, "subtraction off", provider, patch, targetDiff,
		parseCorpus(t, c.upstream), parseCorpus(t, c.downstream), merged, conflicts,
		false, false)

	// Same merge with subtraction on: the downstream's differences from the base are
	// removed from the patch first, so the downstream wins on an overlapping path.
	mergedSub, conflictsSub, err := yamlkit.PatchMutations(parseCorpus(t, c.downstream), nil, patch, targetDiff, provider, nil)
	require.NoError(t, err)
	wantSub := map[string]string{}
	maps.Copy(wantSub, c.want)
	maps.Copy(wantSub, c.wantSubtracted)
	assertWantedValues(t, "subtraction on", mergedSub, wantSub)
	assertConflictReasons(t, conflictsSub, c.wantConflictsSubtracted)
	checkMergeProperties(t, "subtraction on", provider, patch, targetDiff,
		parseCorpus(t, c.upstream), parseCorpus(t, c.downstream), mergedSub, conflictsSub,
		true, false)
}

// checkMergeProperties checks the invariants that must hold for every merge, whatever the
// case asserted.
//
// Both properties are checked only on stable paths — those with no bare numeric segment.
// A numeric segment is a position in a positional array, and a position means different
// things in the patch (where it indexes the source) and in the merged result (where the
// same array may have gained or lost elements), so comparing them here would be
// re-deriving the engine's own coordinate mapping and asserting it against itself. Cases
// covering positional arrays state their expected values explicitly instead.
func checkMergeProperties(t *testing.T, mode string, provider yamlkit.ResourceProvider,
	patch, targetDiff api.ResourceMutationList,
	upstream, downstream, merged gaby.Container, conflicts api.MutationConflictList,
	subtracted, skipIdempotence bool) {
	t.Helper()

	// Property: conservation. Every path the upstream change wanted is either applied or
	// reported. Nothing disappears quietly.
	for _, resourceMutation := range patch {
		if resourceMutation.ResourceMutationInfo.MutationType != api.MutationTypeUpdate {
			continue
		}
		for path, info := range resourceMutation.PathMutationMap {
			if !isStablePath(string(path)) {
				continue
			}
			if reportedInConflicts(conflicts, resourceMutation.Resource, path) {
				continue
			}
			// A mutation carrying a line-level Patch is applied as a sub-merge against
			// the target's own value, so the result deliberately differs from the
			// mutation's Value, which is only the fallback for when the sub-merge
			// cannot be applied. Conservation for those is that the path is still
			// there; what the sub-merge produced is the opaque-string cases' business.
			if info.Patch != "" {
				_, present := dataAtPath(t, provider, merged, resourceMutation.Resource,
					resourceMutation.AliasesWithoutScopes, path)
				assert.Truef(t, present,
					"[%s] conservation: the patch sub-merges %s but it is absent from the result", mode, path)
				continue
			}
			wanted, wantedFound := dataAtPath(t, provider, upstream, resourceMutation.Resource,
				resourceMutation.AliasesWithoutScopes, path)
			got, gotFound := dataAtPath(t, provider, merged, resourceMutation.Resource,
				resourceMutation.AliasesWithoutScopes, path)
			switch info.MutationType {
			case api.MutationTypeDelete:
				assert.Falsef(t, gotFound,
					"[%s] conservation: the patch deletes %s but it is still present and no conflict was reported",
					mode, path)
			default:
				if !assert.Truef(t, gotFound,
					"[%s] conservation: the patch sets %s but it is absent from the result and no conflict was reported",
					mode, path) {
					continue
				}
				if !wantedFound {
					continue
				}
				assert.Truef(t, dataContains(got, wanted),
					"[%s] conservation: the patch sets %s to %v but the result has %v and no conflict was reported",
					mode, path, wanted, got)
			}
		}
	}

	// Property: override preservation. A downstream change to a path the upstream change
	// did not touch always survives. (With subtraction on, a path the upstream change did
	// touch survives too, but that is the subtraction feature rather than an invariant.)
	for _, resourceMutation := range targetDiff {
		if resourceMutation.ResourceMutationInfo.MutationType != api.MutationTypeUpdate {
			continue
		}
		patchIndex := api.NewResourceMutationIndex(patch)
		patchResource, found := patchIndex.Find(resourceMutation.Resource, resourceMutation.AliasesWithoutScopes)
		// Compare on canonical paths: the index in an associative segment records where
		// the element sat on the side the path came from, and the two sides disagree
		// whenever one of them added or removed an earlier element.
		patchPaths := api.MutationMap{}
		if found {
			for patchPath, patchInfo := range patch[patchResource].PathMutationMap {
				patchPaths[yamlkit.CanonicalMutationPath(patchPath)] = patchInfo
			}
		}
		for path, info := range resourceMutation.PathMutationMap {
			if !isStablePath(string(path)) || info.MutationType == api.MutationTypeDelete {
				continue
			}
			if _, _, touched := api.FindAncestorPath(patchPaths, yamlkit.CanonicalMutationPath(path)); touched {
				continue
			}
			wanted, wantedFound := dataAtPath(t, provider, downstream, resourceMutation.Resource,
				resourceMutation.AliasesWithoutScopes, path)
			if !wantedFound {
				continue
			}
			got, gotFound := dataAtPath(t, provider, merged, resourceMutation.Resource,
				resourceMutation.AliasesWithoutScopes, path)
			if !assert.Truef(t, gotFound,
				"[%s] override preservation: the downstream set %s and the upstream change did not touch it, but it is gone",
				mode, path) {
				continue
			}
			assert.Truef(t, dataContains(got, wanted),
				"[%s] override preservation: the downstream set %s to %v and the upstream change did not touch it, but the result has %v",
				mode, path, wanted, got)
		}
	}

	// Property: idempotence. Replaying the same patch onto the merged result is a no-op.
	// Only meaningful without subtraction: with it, the second pass has a different
	// subtrahend, which is a different merge rather than a repeat of this one.
	//
	// Positional-array element removals used to be exempt: the patch said "remove the
	// element at index N", the element at index N after the first application was a
	// different one, and nothing in the patch said which element was meant. The array
	// context an anchored segment carries is what says so now — an array that has lost an
	// element no longer has the length the patch was computed against, so the removal
	// finds nothing to remove rather than taking whatever moved up into place.
	if !subtracted && !skipIdempotence {
		again, _, err := yamlkit.PatchMutations(reparse(t, merged), nil, patch, nil, provider, nil)
		require.NoError(t, err)
		assert.Equalf(t, merged.String(), again.String(),
			"[%s] idempotence: replaying the patch changed the result", mode)
	}
}

// ---------------------------------------------------------------------------
// Harness helpers
// ---------------------------------------------------------------------------

func parseCorpus(t *testing.T, data string) gaby.Container {
	t.Helper()
	parsed, err := gaby.ParseAll([]byte(data))
	require.NoError(t, err)
	return parsed
}

func reparse(t *testing.T, docs gaby.Container) gaby.Container {
	t.Helper()
	return parseCorpus(t, docs.String())
}

// isStablePath reports whether every segment of the path identifies its element by name or
// by merge key rather than by position. A bare numeric segment is a position; so is an
// anchored segment, which names an element of a positional array by the content it had in
// the merge base — good enough to find the element while patching, not good enough to
// resolve against a merged result the way these properties would need to.
func isStablePath(path string) bool {
	for segment := range strings.SplitSeq(path, ".") {
		if _, err := strconv.Atoi(segment); err == nil {
			return false
		}
		if strings.HasPrefix(segment, "?~") {
			return false
		}
	}
	return true
}

// docForResource finds the document holding the given resource, the way the engine does:
// the type has to agree, and the name is tried in full, then without its scope, then
// against the aliases the mutation carries. A variant that moved its resources to another
// namespace or renamed them still resolves — and two resources of different types sharing
// a name do not get confused for each other, which is ordinary in Kubernetes (a Deployment
// and its Service).
func docForResource(t *testing.T, provider yamlkit.ResourceProvider, docs gaby.Container,
	resource api.ResourceInfo, aliases map[api.ResourceName]struct{}) *gaby.YamlDoc {
	t.Helper()
	names := map[api.ResourceName]struct{}{
		resource.ResourceName: {},
	}
	if resource.ResourceNameWithoutScope != "" {
		names[resource.ResourceNameWithoutScope] = struct{}{}
	}
	for alias := range aliases {
		names[alias] = struct{}{}
	}
	for _, doc := range docs {
		info, err := yamlkit.GetResourceInfo(doc, provider)
		require.NoError(t, err)
		if !provider.ResourceTypesAreSimilar(info.ResourceType, resource.ResourceType) {
			continue
		}
		if _, found := names[info.ResourceName]; found {
			return doc
		}
		if info.ResourceNameWithoutScope != "" {
			if _, found := names[info.ResourceNameWithoutScope]; found {
				return doc
			}
		}
	}
	return nil
}

// dataAtPath returns the decoded value at a resolved path of a resource, and whether the
// path exists.
func dataAtPath(t *testing.T, provider yamlkit.ResourceProvider, docs gaby.Container,
	resource api.ResourceInfo, aliases map[api.ResourceName]struct{}, path api.ResolvedPath) (any, bool) {
	t.Helper()
	doc := docForResource(t, provider, docs, resource, aliases)
	if doc == nil {
		return nil, false
	}
	resolved, ok := yamlkit.ResolveAssociativeSegments(doc, string(path))
	if !ok {
		return nil, false
	}
	node := doc.Path(resolved)
	if node == nil {
		return nil, false
	}
	return node.Data(), true
}

// dataContains reports whether got holds everything want holds: a mapping must have every
// key want has, recursively; sequences and scalars must be equal.
//
// Containment rather than equality is the right test for both properties below, because a
// merge may legitimately leave more at a path than either side put there. Two variants that
// each add an annotation both end up in the merged map, so neither "the patch's value is
// what is there now" nor "the target's value is what is there now" is true, while "both are
// still there" is exactly what we mean by conserving a change and preserving an override.
func dataContains(got, want any) bool {
	wantMap, wantIsMap := want.(map[string]any)
	if !wantIsMap {
		return reflect.DeepEqual(got, want)
	}
	gotMap, gotIsMap := got.(map[string]any)
	if !gotIsMap {
		return false
	}
	for key, wantValue := range wantMap {
		gotValue, present := gotMap[key]
		if !present || !dataContains(gotValue, wantValue) {
			return false
		}
	}
	return true
}

// reportedInConflicts reports whether the conflict list mentions the path or an ancestor of
// it for the resource — either is a report that the patch did not fully land there.
func reportedInConflicts(conflicts api.MutationConflictList, resource api.ResourceInfo, path api.ResolvedPath) bool {
	for _, conflict := range conflicts {
		if conflict.Resource.ResourceName != resource.ResourceName {
			continue
		}
		if conflict.Path == "" || conflict.Path == path {
			return true
		}
		if strings.HasPrefix(string(path), string(conflict.Path)+".") {
			return true
		}
	}
	return false
}

func assertWantedValues(t *testing.T, mode string, merged gaby.Container, want map[string]string) {
	t.Helper()
	require.NotEmpty(t, merged)
	for path, wanted := range want {
		node := merged[0].Path(path)
		if wanted == absent {
			assert.Nilf(t, node, "[%s] expected %s to be absent, found %v", mode, path, node)
			continue
		}
		if !assert.NotNilf(t, node, "[%s] expected %s to be present", mode, path) {
			continue
		}
		assert.Equalf(t, wanted, strings.TrimSpace(fmt.Sprintf("%v", node.Data())),
			"[%s] value at %s", mode, path)
	}
}

func assertConflictReasons(t *testing.T, conflicts api.MutationConflictList, want []api.ConflictReason) {
	t.Helper()
	if len(want) == 0 {
		return
	}
	got := map[api.ConflictReason]bool{}
	for _, conflict := range conflicts {
		got[conflict.Reason] = true
	}
	for _, reason := range want {
		assert.Truef(t, got[reason], "expected a %s conflict, got %v", reason, conflicts)
	}
}
