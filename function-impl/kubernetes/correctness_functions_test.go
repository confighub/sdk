// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// correctnessFindingsOf runs vet-correctness's selected checks (all when nil) on data, and
// returns each failure as "<check> <path>".
func correctnessFindingsOf(t *testing.T, data string, selected ...string) []string {
	t.Helper()
	if selected == nil {
		selected = correctnessCheckNames
	}
	docs, err := gaby.ParseAll([]byte(data))
	require.NoError(t, err)
	_, output, err := k8sFnVetCorrectness(testResourceProvider, docs, selected, nil)
	require.NoError(t, err)
	result := output.(api.ValidationResult)
	var found []string
	for _, a := range result.FailedAttributes {
		require.Len(t, a.Issues, 1)
		require.NotEmpty(t, a.Issues[0].Message)
		found = append(found, a.Issues[0].Identifier+" "+string(a.Path))
	}
	assert.Equal(t, len(found) == 0, result.Passed)
	return found
}

func TestCorrectnessSelectorMatchesTemplate(t *testing.T) {
	const deployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  selector:
    matchLabels:
      app: web
      app.kubernetes.io/part-of: shop
    matchExpressions:
      - {key: tier, operator: In, values: [front]}
      - {key: canary, operator: DoesNotExist}
  template:
    metadata:
      labels:
        app: web
        tier: back
    spec:
      containers:
        - name: web
          image: nginx
`
	assert.Equal(t, []string{
		"selector-matches-template spec.selector.matchLabels.app~1kubernetes~1io/part-of",
		"selector-matches-template spec.selector.matchExpressions.0",
	}, correctnessFindingsOf(t, deployment))

	const matching = `apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: db
spec:
  selector:
    matchLabels: {app: db}
  template:
    metadata:
      labels: {app: db, tier: data}
`
	assert.Empty(t, correctnessFindingsOf(t, matching))

	// A Job's selector is usually generated, and one with none has nothing to check.
	const job = `apiVersion: batch/v1
kind: Job
metadata:
  name: once
spec:
  template:
    spec:
      restartPolicy: Never
`
	assert.Empty(t, correctnessFindingsOf(t, job))
}

func TestCorrectnessLabelSyntax(t *testing.T) {
	data := fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  labels:
    good: confighubplaceholder
    "bad key!": x
    long: %s
    version: 1.0
spec:
  selector:
    matchLabels: {app: web}
  template:
    metadata:
      labels:
        app: web
        team: "web team"
`, strings.Repeat("a", 64))
	assert.Equal(t, []string{
		"label-syntax metadata.labels.bad key!",
		"label-syntax metadata.labels.long",
		"label-syntax metadata.labels.version",
		"label-syntax spec.template.metadata.labels.team",
	}, correctnessFindingsOf(t, data))
}

func TestCorrectnessConfigMapSize(t *testing.T) {
	small := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: small\ndata:\n  a: b\n"
	assert.Empty(t, correctnessFindingsOf(t, small))

	large := fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: large\ndata:\n  a: %s\n  b: %s\n",
		strings.Repeat("x", maxConfigMapSize/2), strings.Repeat("y", maxConfigMapSize/2+1))
	assert.Equal(t, []string{"configmap-size data"}, correctnessFindingsOf(t, large))
}

func TestCorrectnessCronJob(t *testing.T) {
	cronJob := func(schedule, zone, restartPolicy string) string {
		data := "apiVersion: batch/v1\nkind: CronJob\nmetadata:\n  name: nightly\nspec:\n  schedule: " + schedule + "\n"
		if zone != "" {
			data += "  timeZone: " + zone + "\n"
		}
		data += "  jobTemplate:\n    spec:\n      template:\n        spec:\n"
		if restartPolicy != "" {
			data += "          restartPolicy: " + restartPolicy + "\n"
		}
		return data + "          containers:\n            - name: job\n              image: busybox\n"
	}
	for _, ok := range []string{`"0 3 * * *"`, `"*/15 9-17 * * MON-FRI"`, `"0 0 1 JAN,JUL ?"`, "'@daily'", `"0 0 * * 0"`} {
		assert.Empty(t, correctnessFindingsOf(t, cronJob(ok, "Europe/Berlin", "OnFailure")), ok)
	}
	for _, bad := range []string{`"0 3 * *"`, `"60 * * * *"`, `"0 25 * * *"`, `"5-1 * * * *"`, `"0 0 * * FUNDAY"`, "'@sometimes'", `"CRON_TZ=UTC 0 3 * * *"`, `"0 0 * * 7"`} {
		assert.Equal(t, []string{"cronjob spec.schedule"}, correctnessFindingsOf(t, cronJob(bad, "", "Never")), bad)
	}
	assert.Equal(t, []string{"cronjob spec.timeZone"}, correctnessFindingsOf(t, cronJob(`"0 3 * * *"`, "Mars/Olympus", "Never")))
	assert.Equal(t, []string{"cronjob spec.jobTemplate.spec.template.spec.restartPolicy"},
		correctnessFindingsOf(t, cronJob(`"0 3 * * *"`, "", "Always")))
	assert.Equal(t, []string{"cronjob spec.jobTemplate.spec.template.spec.restartPolicy"},
		correctnessFindingsOf(t, cronJob(`"0 3 * * *"`, "", "")))
}

func TestCorrectnessCheckSelection(t *testing.T) {
	const both = `apiVersion: batch/v1
kind: CronJob
metadata:
  name: nightly
  labels: {"bad key!": x}
spec:
  schedule: "0 3 * * *"
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: Never
`
	selected, err := selectChecks(correctnessCheckNames, "", "label-syntax")
	require.NoError(t, err)
	assert.Empty(t, correctnessFindingsOf(t, both, selected...))
	selected, err = selectChecks(correctnessCheckNames, "label-syntax", "")
	require.NoError(t, err)
	assert.Equal(t, []string{"label-syntax metadata.labels.bad key!"}, correctnessFindingsOf(t, both, selected...))

	_, err = selectChecks(correctnessCheckNames, "nonexistent", "")
	assert.ErrorContains(t, err, `unknown check "nonexistent"`)
	all, err := selectChecks(correctnessCheckNames, "", "")
	require.NoError(t, err)
	assert.True(t, slices.Equal(all, correctnessCheckNames))
}

func TestCorrectnessNameSyntax(t *testing.T) {
	named := func(kind, apiVersion, name string) string {
		return "apiVersion: " + apiVersion + "\nkind: " + kind + "\nmetadata:\n  name: " + name + "\n"
	}
	for _, tc := range []struct {
		kind, apiVersion, name string
		bad                    bool
	}{
		{"ConfigMap", "v1", "web.settings", false},
		{"ConfigMap", "v1", "Web_Settings", true},
		{"Namespace", "v1", "shop", false},
		{"Namespace", "v1", "shop.prod", true},
		{"Service", "v1", "web", false},
		{"Service", "v1", "1web", true},
		{"ClusterRole", "rbac.authorization.k8s.io/v1", "system:aggregate-to-edit", false},
		{"ClusterRole", "rbac.authorization.k8s.io/v1", "a/b", true},
		{"Deployment", "apps/v1", "confighubplaceholder_web", false},
	} {
		found := correctnessFindingsOf(t, named(tc.kind, tc.apiVersion, tc.name), checkNameSyntax)
		if tc.bad {
			assert.Equal(t, []string{"name-syntax metadata.name"}, found, "%s %s", tc.kind, tc.name)
		} else {
			assert.Empty(t, found, "%s %s", tc.kind, tc.name)
		}
	}
}

const containerReferences = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  selector:
    matchLabels: {app: web}
  template:
    metadata:
      labels: {app: web}
    spec:
      serviceAccount: web
      volumes:
        - name: data
          emptyDir: {}
      initContainers:
        - name: init
          image: busybox
          volumeMounts:
            - {name: data, mountPath: /data}
      containers:
        - name: web
          image: nginx
          ports:
            - {name: http, containerPort: 8080}
            - {name: Metrics_Port, containerPort: 9090}
            - {name: http, containerPort: 8081}
          livenessProbe:
            httpGet: {port: http, path: /healthz}
          readinessProbe:
            tcpSocket: {port: grpc}
          startupProbe:
            httpGet: {port: 8080}
          volumeMounts:
            - {name: data, mountPath: /data}
            - {name: cache, mountPath: /cache}
          volumeDevices:
            - {name: block, devicePath: /dev/xvda}
`

func TestCorrectnessContainerReferences(t *testing.T) {
	assert.Equal(t, []string{
		"port-names spec.template.spec.containers.?name=web.ports.1.name",
		"port-names spec.template.spec.containers.?name=web.ports.2.name",
	}, correctnessFindingsOf(t, containerReferences, checkPortNames))
	assert.Equal(t, []string{
		"port-references spec.template.spec.containers.?name=web.readinessProbe.tcpSocket.port",
	}, correctnessFindingsOf(t, containerReferences, checkPortReferences))
	assert.Equal(t, []string{
		"volume-references spec.template.spec.containers.?name=web.volumeMounts.1.name",
		"volume-references spec.template.spec.containers.?name=web.volumeDevices.0.name",
	}, correctnessFindingsOf(t, containerReferences, checkVolumeReferences))
	assert.Equal(t, []string{
		"deprecated-fields spec.template.spec.serviceAccount",
	}, correctnessFindingsOf(t, containerReferences, checkDeprecatedFields))
}

func TestCorrectnessExactlyOneOf(t *testing.T) {
	budget := func(spec string) string {
		return "apiVersion: policy/v1\nkind: PodDisruptionBudget\nmetadata:\n  name: web\nspec:\n" + spec
	}
	assert.Equal(t, []string{"exactly-one-of spec"},
		correctnessFindingsOf(t, budget("  selector:\n    matchLabels: {app: web}\n"), checkExactlyOneOf))
	assert.Empty(t, correctnessFindingsOf(t, budget("  minAvailable: 1\n  selector:\n    matchLabels: {app: web}\n"), checkExactlyOneOf))
	assert.Empty(t, correctnessFindingsOf(t, budget("  maxUnavailable: 25%\n"), checkExactlyOneOf))
}

func TestCorrectnessDeprecatedAnnotations(t *testing.T) {
	ingress := `apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: web
  annotations:
    kubernetes.io/ingress.class: nginx
spec:
  rules: []
`
	assert.Equal(t, []string{"deprecated-fields metadata.annotations.kubernetes~1io/ingress~1class"},
		correctnessFindingsOf(t, ingress, checkDeprecatedFields))
	replaced := strings.Replace(ingress, "  rules: []", "  ingressClassName: nginx\n  rules: []", 1)
	assert.Empty(t, correctnessFindingsOf(t, replaced, checkDeprecatedFields))
	// The API server rejects an Ingress whose annotation and ingressClassName differ.
	conflicting := strings.Replace(ingress, "  rules: []", "  ingressClassName: traefik\n  rules: []", 1)
	assert.Equal(t, []string{"deprecated-fields metadata.annotations.kubernetes~1io/ingress~1class"},
		correctnessFindingsOf(t, conflicting, checkDeprecatedFields))
}

// A selector of matchExpressions alone is checked too.
func TestCorrectnessSelectorOfExpressionsOnly(t *testing.T) {
	const deployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  selector:
    matchExpressions:
      - {key: app, operator: In, values: [web]}
  template:
    metadata:
      labels: {app: api}
`
	assert.Equal(t, []string{"selector-matches-template spec.selector.matchExpressions.0"},
		correctnessFindingsOf(t, deployment, checkSelectorMatchesTemplate))
	assert.Empty(t, correctnessFindingsOf(t, strings.Replace(deployment, "{app: api}", "{app: web}", 1), checkSelectorMatchesTemplate))
}

func TestCorrectnessDeprecatedAPI(t *testing.T) {
	assert.Equal(t, []string{"deprecated-api apiVersion"},
		correctnessFindingsOf(t, "apiVersion: v1\nkind: ComponentStatus\nmetadata:\n  name: scheduler\n", checkDeprecatedAPI))
	assert.Empty(t, correctnessFindingsOf(t, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\n", checkDeprecatedAPI))
}
