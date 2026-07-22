// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package helmutils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeValuesFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestMergeValuesPrecedence(t *testing.T) {
	dir := t.TempDir()
	file1 := writeValuesFile(t, dir, "values1.yaml", `
app:
  name: app1
  port: 8080
  replicas: 1
database:
  host: localhost
`)
	file2 := writeValuesFile(t, dir, "values2.yaml", `
app:
  port: 9090
  replicas: 3
database:
  host: prod.example.com
`)

	// Later files override earlier ones; --set overrides files.
	result, err := MergeValues([]string{file1, file2}, []string{"app.replicas=5", "extra=true"})
	require.NoError(t, err)

	app := result["app"].(map[string]any)
	assert.Equal(t, "app1", app["name"])
	assert.Equal(t, float64(9090), app["port"])
	assert.Equal(t, int64(5), app["replicas"])
	db := result["database"].(map[string]any)
	assert.Equal(t, "prod.example.com", db["host"])
	assert.Equal(t, true, result["extra"])
}

func TestMergeValuesErrors(t *testing.T) {
	_, err := MergeValues([]string{"/nonexistent/values.yaml"}, nil)
	require.Error(t, err)

	dir := t.TempDir()
	bad := writeValuesFile(t, dir, "bad.yaml", "\t- not valid yaml")
	_, err = MergeValues([]string{bad}, nil)
	require.Error(t, err)
}

func TestMergeValuesEmpty(t *testing.T) {
	result, err := MergeValues(nil, nil)
	require.NoError(t, err)
	assert.Empty(t, result)
}
