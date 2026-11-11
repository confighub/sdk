// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func invokeSetPodContainerSecurityContextDefaults(t *testing.T, input string) gaby.Container {
	t.Helper()
	req := &api.FunctionInvocationRequest{
		ConfigData:          input,
		FunctionInvocations: []api.FunctionInvocation{{FunctionName: "set-pod-container-security-context-defaults"}},
	}
	resp, err := testFunctionHandler.InvokeCore(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.Success, "set-pod-container-security-context-defaults should succeed; errors: %v", resp.ErrorMessages)
	docs, err := gaby.ParseAll([]byte(resp.ConfigData))
	require.NoError(t, err)
	return docs
}

func TestSetPodContainerSecurityContextDefaults_AllowPrivilegeEscalation(t *testing.T) {
	const input = `apiVersion: apps/v1
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
      initContainers:
      - name: setup
        image: setup:1.0
        securityContext:
          capabilities:
            add: ["SYS_ADMIN"]
      containers:
      - name: plain
        image: plain:1.0
      - name: privileged
        image: agent:1.0
        securityContext:
          privileged: true
      - name: explicit-false
        image: agent:1.0
        securityContext:
          privileged: true
          allowPrivilegeEscalation: false
      - name: sys-admin
        image: agent:1.0
        securityContext:
          capabilities:
            add: ["NET_ADMIN", "CAP_SYS_ADMIN"]
      - name: net-admin
        image: agent:1.0
        securityContext:
          capabilities:
            add: ["NET_ADMIN"]
---
apiVersion: batch/v1
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
          containers:
          - name: report
            image: report:1.0
            securityContext:
              privileged: true
`
	docs := invokeSetPodContainerSecurityContextDefaults(t, input)
	require.Len(t, docs, 2)

	allowed := func(doc *gaby.YamlDoc, containerPath string) any {
		return doc.Path(containerPath + ".securityContext.allowPrivilegeEscalation").Data()
	}
	daemonSet := docs[0]
	assert.Equal(t, true, allowed(daemonSet, "spec.template.spec.initContainers.0"), "SYS_ADMIN init container")
	assert.Equal(t, false, allowed(daemonSet, "spec.template.spec.containers.0"), "unprivileged container keeps the default")
	assert.Equal(t, true, allowed(daemonSet, "spec.template.spec.containers.1"), "privileged container")
	assert.Equal(t, true, allowed(daemonSet, "spec.template.spec.containers.2"), "privileged container that set false")
	assert.Equal(t, true, allowed(daemonSet, "spec.template.spec.containers.3"), "CAP_SYS_ADMIN container")
	assert.Equal(t, false, allowed(daemonSet, "spec.template.spec.containers.4"), "other capabilities keep the default")
	assert.Equal(t, false, daemonSet.Path("spec.template.spec.containers.3.securityContext.privileged").Data(),
		"the other defaults are still set")

	cronJob := docs[1]
	assert.Equal(t, true, allowed(cronJob, "spec.jobTemplate.spec.template.spec.containers.0"), "privileged CronJob container")
}
