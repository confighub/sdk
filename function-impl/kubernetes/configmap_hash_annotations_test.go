// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

const hashAnnotationWorkloads = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: web
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
          image: nginx
          envFrom:
            - configMapRef:
                name: app-config
            - configMapRef:
                name: confighubplaceholderapp_env
          env:
            - name: LEVEL
              valueFrom:
                configMapKeyRef:
                  name: app-config
                  key: LOG_LEVEL
      volumes:
        - name: props
          configMap:
            name: app-props
        - name: certs
          secret:
            secretName: certs
---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: report
  namespace: web
spec:
  schedule: "0 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: Never
          containers:
            - name: report
              image: busybox
              envFrom:
                - configMapRef:
                    name: app-config
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: db
  namespace: web
spec:
  selector:
    matchLabels:
      app: db
  template:
    metadata:
      labels:
        app: db
      annotations:
        confighub.com/Hash: confighubplaceholder
    spec:
      containers:
        - name: db
          image: postgres
          envFrom:
            - configMapRef:
                name: app-config
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: web
data:
  LOG_LEVEL: info
`

func setConfigMapHashAnnotations(t *testing.T, input string, names ...string) gaby.Container {
	t.Helper()
	parsed, err := gaby.ParseAll([]byte(input))
	require.NoError(t, err)
	var args []api.FunctionArgument
	for _, name := range names {
		args = append(args, api.FunctionArgument{Value: name})
	}
	out, _, err := k8sFnSetConfigMapHashAnnotations(testResourceProvider, nil, parsed, args)
	require.NoError(t, err)
	return out
}

func podTemplateAnnotations(t *testing.T, docs gaby.Container, index int, templatePath string) map[string]any {
	t.Helper()
	got, _ := docs[index].Path(templatePath + ".metadata.annotations").Data().(map[string]any)
	return got
}

func TestSetConfigMapHashAnnotations(t *testing.T) {
	out := setConfigMapHashAnnotations(t, hashAnnotationWorkloads)

	assert.Equal(t, map[string]any{
		"hash.confighub.com/app-config": "confighubplaceholderapp_config",
		"hash.confighub.com/app-props":  "confighubplaceholderapp_props",
	}, podTemplateAnnotations(t, out, 0, "spec.template"),
		"one annotation per ConfigMap however many times it is referenced, none for a reference that is a placeholder, and none for a Secret")
	assert.Equal(t, map[string]any{
		"hash.confighub.com/app-config": "confighubplaceholderapp_config",
	}, podTemplateAnnotations(t, out, 1, "spec.jobTemplate.spec.template"),
		"a CronJob's pod template is inside its job template")
	assert.Equal(t, map[string]any{
		"confighub.com/Hash": "confighubplaceholder",
	}, podTemplateAnnotations(t, out, 2, "spec.template"),
		"a pod template annotated with confighub.com/Hash is left alone")
	assert.Nil(t, out[3].Path("metadata.annotations"), "a ConfigMap is not a workload")

	again := setConfigMapHashAnnotations(t, out.String())
	assert.Equal(t, out.String(), again.String(), "running it again changes nothing")
}

func TestSetConfigMapHashAnnotations_Named(t *testing.T) {
	out := setConfigMapHashAnnotations(t, hashAnnotationWorkloads, "app-props", "not-referenced")
	assert.Equal(t, map[string]any{
		"hash.confighub.com/app-props": "confighubplaceholderapp_props",
	}, podTemplateAnnotations(t, out, 0, "spec.template"), "only the ConfigMaps named are annotated")
	assert.Nil(t, podTemplateAnnotations(t, out, 1, "spec.jobTemplate.spec.template"),
		"a pod template referencing none of them is not annotated")
}

func TestSetConfigMapHashAnnotations_KeepsExisting(t *testing.T) {
	input := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  template:
    metadata:
      annotations:
        hash.confighub.com/app-config: "0123456789"
    spec:
      containers:
        - name: web
          image: nginx
          envFrom:
            - configMapRef:
                name: app-config
            - configMapRef:
                name: feature-flags
`
	out := setConfigMapHashAnnotations(t, input)
	assert.Equal(t, map[string]any{
		"hash.confighub.com/app-config":    "0123456789",
		"hash.confighub.com/feature-flags": "confighubplaceholderfeature_flags",
	}, podTemplateAnnotations(t, out, 0, "spec.template"),
		"an annotation already present keeps its value, and a ConfigMap referenced since gets one")
}
