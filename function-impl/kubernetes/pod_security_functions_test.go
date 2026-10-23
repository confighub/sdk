// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// A Deployment with no securityContext meets baseline but not restricted.
const podSecurityPlainDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
      - name: web
        image: nginx:1.27
`

const podSecurityPrivilegedDaemonSet = `apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: agent
spec:
  selector:
    matchLabels:
      app: agent
  template:
    metadata:
      labels:
        app: agent
    spec:
      hostNetwork: true
      containers:
      - name: agent
        image: agent:1.0
        securityContext:
          privileged: true
`

const podSecurityRestrictedCronJob = `apiVersion: batch/v1
kind: CronJob
metadata:
  name: report
spec:
  schedule: "0 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: OnFailure
          securityContext:
            runAsNonRoot: true
            seccompProfile:
              type: RuntimeDefault
          containers:
          - name: report
            image: report:1.0
            securityContext:
              allowPrivilegeEscalation: false
              capabilities:
                drop: ["ALL"]
`

// The legacy AppArmor annotation is in the pod template's metadata, not its spec.
const podSecurityUnconfinedAppArmor = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      annotations:
        container.apparmor.security.beta.kubernetes.io/web: unconfined
    spec:
      containers:
      - name: web
        image: nginx:1.27
`

const podSecurityNoWorkloads = `apiVersion: v1
kind: ConfigMap
metadata:
  name: settings
data:
  a: b
---
apiVersion: v1
kind: Service
metadata:
  name: web
spec:
  ports:
  - port: 80
`

func vetPodSecurity(t *testing.T, data, level, version string) api.ValidationResult {
	t.Helper()
	docs, err := gaby.ParseAll([]byte(data))
	require.NoError(t, err)
	_, output, err := k8sFnVetPodSecurityStandard(testResourceProvider, docs, level, version, nil)
	require.NoError(t, err)
	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	return vr
}

func issueMessages(vr api.ValidationResult) string {
	var messages []string
	for _, fa := range vr.FailedAttributes {
		for _, issue := range fa.Issues {
			messages = append(messages, issue.Message)
		}
	}
	return strings.Join(messages, "\n")
}

func TestVetPodSecurityStandard_NoWorkloadsPass(t *testing.T) {
	for _, data := range []string{podSecurityNoWorkloads, ""} {
		vr := vetPodSecurity(t, data, "restricted", "latest")
		assert.True(t, vr.Passed)
		assert.Empty(t, vr.FailedAttributes)
	}
}

func TestVetPodSecurityStandard_Levels(t *testing.T) {
	vr := vetPodSecurity(t, podSecurityPlainDeployment, "baseline", "latest")
	assert.True(t, vr.Passed, issueMessages(vr))

	vr = vetPodSecurity(t, podSecurityPlainDeployment, "restricted", "latest")
	assert.False(t, vr.Passed)
	require.Len(t, vr.FailedAttributes, 1)
	fa := vr.FailedAttributes[0]
	assert.Equal(t, api.ResolvedPath("spec.template.spec"), fa.Path)
	assert.Equal(t, api.ResourceName("/web"), fa.ResourceName)
	messages := issueMessages(vr)
	assert.Contains(t, messages, "allowPrivilegeEscalation != false")
	assert.Contains(t, messages, "runAsNonRoot != true")
	assert.Contains(t, messages, `violates PodSecurity "restricted:latest"`)
	var identifiers []string
	for _, issue := range fa.Issues {
		identifiers = append(identifiers, issue.Identifier)
	}
	assert.Contains(t, identifiers, "runAsNonRoot != true")

	// privileged allows everything.
	vr = vetPodSecurity(t, podSecurityPrivilegedDaemonSet, "privileged", "latest")
	assert.True(t, vr.Passed)
}

func TestVetPodSecurityStandard_BaselineViolations(t *testing.T) {
	vr := vetPodSecurity(t, podSecurityPrivilegedDaemonSet, "baseline", "")
	assert.False(t, vr.Passed)
	messages := issueMessages(vr)
	assert.Contains(t, messages, "host namespaces")
	assert.Contains(t, messages, "privileged")
	assert.Contains(t, messages, `container "agent"`)
}

// Each workload type's PodSpec is found where the type keeps it, here three levels down a CronJob.
func TestVetPodSecurityStandard_CronJobRestricted(t *testing.T) {
	vr := vetPodSecurity(t, podSecurityRestrictedCronJob, "restricted", "v1.30")
	assert.True(t, vr.Passed, issueMessages(vr))

	failing := strings.Replace(podSecurityRestrictedCronJob, "runAsNonRoot: true", "runAsNonRoot: false", 1)
	vr = vetPodSecurity(t, failing, "restricted", "latest")
	assert.False(t, vr.Passed)
	require.Len(t, vr.FailedAttributes, 1)
	assert.Equal(t, api.ResolvedPath("spec.jobTemplate.spec.template.spec"), vr.FailedAttributes[0].Path)
}

func TestVetPodSecurityStandard_ReadsPodMetadata(t *testing.T) {
	vr := vetPodSecurity(t, podSecurityUnconfinedAppArmor, "baseline", "latest")
	assert.False(t, vr.Passed)
	assert.Contains(t, issueMessages(vr), "AppArmor")
}

// Only the failing workload is reported when a Unit holds several.
func TestVetPodSecurityStandard_ReportsEachWorkload(t *testing.T) {
	vr := vetPodSecurity(t, podSecurityRestrictedCronJob+"---\n"+podSecurityPlainDeployment, "restricted", "latest")
	assert.False(t, vr.Passed)
	require.Len(t, vr.FailedAttributes, 1)
	assert.Equal(t, api.ResourceName("/web"), vr.FailedAttributes[0].ResourceName)
}

// A base Unit's placeholders in fields no check reads, such as resource requests,
// leave the workload checkable.
func TestVetPodSecurityStandard_PlaceholderQuantities(t *testing.T) {
	data := strings.Replace(podSecurityRestrictedCronJob, "          - name: report\n            image: report:1.0\n",
		"          - name: report\n            image: report:1.0\n            resources:\n              requests:\n                cpu: confighubplaceholder\n                memory: confighubplaceholder\n", 1)
	data = strings.Replace(data, "          restartPolicy: OnFailure\n",
		"          restartPolicy: OnFailure\n          volumes:\n          - name: scratch\n            emptyDir:\n              sizeLimit: confighubplaceholder\n", 1)
	require.Contains(t, data, "cpu: confighubplaceholder")
	require.Contains(t, data, "sizeLimit: confighubplaceholder")
	vr := vetPodSecurity(t, data, "restricted", "latest")
	assert.True(t, vr.Passed, issueMessages(vr))
}

func TestVetPodSecurityStandard_InvalidArguments(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(podSecurityPlainDeployment))
	require.NoError(t, err)
	_, _, err = k8sFnVetPodSecurityStandard(testResourceProvider, docs, "strict", "latest", nil)
	assert.ErrorContains(t, err, `invalid level "strict"`)
	_, _, err = k8sFnVetPodSecurityStandard(testResourceProvider, docs, "baseline", "1.30", nil)
	assert.ErrorContains(t, err, `invalid version "1.30"`)
}
