// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

const pathCheckDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  template:
    spec:
      containers:
      - name: app
        image: web:1
        env:
        - name: A
          value: a
      volumes: []
`

func pathCheckDoc(t *testing.T) *gaby.YamlDoc {
	t.Helper()
	docs, err := gaby.ParseAll([]byte(pathCheckDeployment))
	require.NoError(t, err)
	return docs[0]
}

func TestFindPath(t *testing.T) {
	doc := pathCheckDoc(t)
	for path, want := range map[string]bool{
		"spec.template.spec.containers.0.image":               true,
		"spec.template.spec.containers.?name=app.image":       true,
		"spec.template.spec.containers.?name=app.env.0.value": true,
		"spec.template.spec.containers.0.env.1.value":         false,
		"spec.template.spec.containers.?name=side.image":      false,
		"spec.template.spec.containers.0.workingDir":          false,
		"spec.template.spec.containers.0.image#tag":           true,
	} {
		_, found := FindPath(doc, api.UnresolvedPath(path))
		assert.Equal(t, want, found, path)
	}
}

func TestCheckPathSettable(t *testing.T) {
	doc := pathCheckDoc(t)
	for path, wantErr := range map[string]string{
		"spec.template.spec.containers.0.image":                     "",
		"spec.template.spec.containers.?name=app.workingDir":        "",
		"spec.template.spec.containers.0.securityContext.runAsUser": "",
		"spec.template.spec.containers.?name=side":                  "", // a whole element is appended
		"spec.template.spec.containers.1":                           "", // the end of the list
		"spec.template.spec.volumes.0":                              "",
		"spec.template.spec.containers.?name=side.image":            "matches nothing",
		"spec.template.spec.containers.0.env.3.value":               "has 1 elements, so 3 names none of them",
		"spec.template.spec.containers.1.image":                     "has 1 elements, so 1 names none of them",
		"spec.template.spec.containers.0.args.0":                    "would be an index into a list that does not exist",
		"spec.template.spec.initContainers.0.image":                 "would be an index into a list that does not exist",
	} {
		err := CheckPathSettable(doc, api.UnresolvedPath(path))
		if wantErr == "" {
			assert.NoError(t, err, path)
		} else if assert.Error(t, err, path) {
			assert.Contains(t, err.Error(), wantErr, path)
		}
	}
}
