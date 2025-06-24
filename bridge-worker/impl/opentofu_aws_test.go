// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/hashicorp/terraform-exec/tfexec"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

type mockWorkerContext struct {
	opName   string
	statuses []string
}

func (m *mockWorkerContext) Context() context.Context {
	return context.Background()
}

func (m *mockWorkerContext) GetServerURL() string {
	return "https://localhost"
}

func (m *mockWorkerContext) GetWorkerID() string {
	return "mock-worker"
}

func (m *mockWorkerContext) SendStatus(status *api.ActionResult) error {
	status.Action = api.ActionType(m.opName)
	s := fmt.Sprintf("%s/%s/%s", status.Action, status.Status, status.Message)
	m.statuses = append(m.statuses, s)
	return nil
}

var _ api.BridgeWorkerContext = (*mockWorkerContext)(nil)

func TestPrepareWorkspace(t *testing.T) {
	tests := []struct {
		name    string
		payload api.BridgeWorkerPayload
		wantErr bool
	}{
		{
			name: "valid terraform config with basic AWS",
			payload: api.BridgeWorkerPayload{
				Data: []byte(`resource "null_resource" "test" {}`),
				TargetParams: []byte(`{
					"type": "opentofu-aws",
					"profile": "default",
					"region": "us-west-2"
				}`),
			},
			wantErr: false,
		},
		{
			name: "valid terraform config with AWS keys",
			payload: api.BridgeWorkerPayload{
				Data: []byte(`resource "null_resource" "test" {}`),
				TargetParams: []byte(`{
					"type": "opentofu-aws-with-keys",
					"profile": "default",
					"region": "us-west-2",
					"aws_access_key_id": "AKIATEST",
					"aws_secret_access_key": "secret"
				}`),
			},
			wantErr: false,
		},
		{
			name: "valid terraform config with assume role",
			payload: api.BridgeWorkerPayload{
				Data: []byte(`resource "null_resource" "test" {}`),
				TargetParams: []byte(`{
					"type": "opentofu-aws-with-assume-role", 
					"profile": "default",
					"region": "us-west-2",
					"role_arn": "arn:aws:iam::123456789012:role/test",
					"session_name": "test-session",
					"external_id": "test-external-id"
				}`),
			},
			wantErr: false,
		},
		{
			name: "invalid target params",
			payload: api.BridgeWorkerPayload{
				Data:         []byte(`resource "null_resource" "test" {}`),
				TargetParams: []byte(`invalid json`),
			},
			wantErr: true,
		},
		{
			name: "empty config",
			payload: api.BridgeWorkerPayload{
				Data: []byte{},
				TargetParams: []byte(`{
					"type": "opentofu-aws",
					"profile": "default", 
					"region": "us-west-2"
				}`),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create in-memory filesystem
			fs := afero.NewMemMapFs()

			// Run prepareWorkspace
			tmpDir, err := prepareWorkspace(fs, tt.payload)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.NotEmpty(t, tmpDir)

			// Verify main.tf exists and has correct content
			mainTFPath := filepath.Join(tmpDir, "main.tf")
			exists, err := afero.Exists(fs, mainTFPath)
			assert.NoError(t, err)
			assert.True(t, exists)

			content, err := afero.ReadFile(fs, mainTFPath)
			assert.NoError(t, err)
			assert.Equal(t, tt.payload.Data, content)

			// Verify provider.tf exists
			providerTFPath := filepath.Join(tmpDir, "provider.tf")
			exists, err = afero.Exists(fs, providerTFPath)
			assert.NoError(t, err)
			assert.True(t, exists)

			// Verify provider.tf content matches template
			content, err = afero.ReadFile(fs, providerTFPath)
			assert.NoError(t, err)
			assert.NotEmpty(t, content)

			// Parse target params to verify template data
			var params map[string]string
			err = json.Unmarshal(tt.payload.TargetParams, &params)
			if !tt.wantErr {
				assert.NoError(t, err)
				assert.Contains(t, string(content), params["profile"])
				assert.Contains(t, string(content), params["region"])
			}

			// Cleanup
			fs.RemoveAll(tmpDir)
		})
	}
}

func TestOpenTofuAWSWorker_Apply(t *testing.T) {
	tests := []struct {
		name    string
		payload api.BridgeWorkerPayload
		wantErr bool
		status  string
	}{
		{
			name: "valid terraform config",
			payload: api.BridgeWorkerPayload{
				Data: []byte(`resource "null_resource" "test" {}`),
			},
			wantErr: false,
			status:  "Apply/success/applied successfully",
		},
		{
			name: "invalid terraform config",
			payload: api.BridgeWorkerPayload{
				Data: []byte(`invalid config`),
			},
			wantErr: true,
			status:  "Apply/error/failed to init terraform",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if os.Getenv("AWS_PROFILE") == "" {
				t.Skipf("Fails without proper AWS creds")
			}

			// Create test worker
			w := &OpenTofuAWSWorker{}

			// Create test context
			wctx := &mockWorkerContext{opName: "Apply"}

			// Run Apply
			err := w.Apply(wctx, tt.payload)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, tt.status, wctx.statuses[len(wctx.statuses)-1])
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.status, wctx.statuses[len(wctx.statuses)-1])
			}

			// Verify temp directory is cleaned up
			tempFiles, err := filepath.Glob(filepath.Join(os.TempDir(), "opentofu-aws-*"))
			assert.NoError(t, err)
			assert.Empty(t, tempFiles, "temporary directory should be cleaned up")
		})
	}
}

func TestOpenTofuAWSWorker_Destroy(t *testing.T) {
	tests := []struct {
		name    string
		payload api.BridgeWorkerPayload
		wantErr bool
		status  string
	}{
		{
			name: "valid terraform config",
			payload: api.BridgeWorkerPayload{
				Data: []byte(`resource "null_resource" "test" {}`),
			},
			wantErr: false,
			status:  "Destroy/success/destroyed successfully",
		},
		{
			name: "invalid terraform config",
			payload: api.BridgeWorkerPayload{
				Data: []byte(`invalid config`),
			},
			wantErr: true,
			status:  "Destroy/error/failed to init terraform",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if os.Getenv("AWS_PROFILE") == "" {
				t.Skipf("Fails without proper AWS creds")
			}

			// Create test worker
			w := &OpenTofuAWSWorker{}

			// Create test context
			wctx := &mockWorkerContext{opName: "Destroy"}

			// Run Destroy
			err := w.Destroy(wctx, tt.payload)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, tt.status, wctx.statuses[len(wctx.statuses)-1])
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.status, wctx.statuses[len(wctx.statuses)-1])
			}

			// Verify temp directory is cleaned up
			tempFiles, err := filepath.Glob(filepath.Join(os.TempDir(), "opentofu-aws-*"))
			assert.NoError(t, err)
			assert.Empty(t, tempFiles, "temporary directory should be cleaned up")
		})
	}
}

func TestConvertTofuOutputs(t *testing.T) {
	tests := []struct {
		name     string
		outputs  map[string]*tfexec.OutputMeta
		expected map[string]interface{}
		wantErr  bool
	}{
		{
			name: "simple string output",
			outputs: map[string]*tfexec.OutputMeta{
				"string_output": {
					Value: json.RawMessage(`"test value"`),
				},
			},
			expected: map[string]interface{}{
				"string_output": "test value",
			},
			wantErr: false,
		},
		{
			name: "number output",
			outputs: map[string]*tfexec.OutputMeta{
				"number_output": {
					Value: json.RawMessage(`42`),
				},
			},
			expected: map[string]interface{}{
				"number_output": float64(42),
			},
			wantErr: false,
		},
		{
			name: "complex output",
			outputs: map[string]*tfexec.OutputMeta{
				"complex_output": {
					Value: json.RawMessage(`{"key1": "value1", "key2": 42, "key3": [1, 2, 3]}`),
				},
			},
			expected: map[string]interface{}{
				"complex_output": map[string]interface{}{
					"key1": "value1",
					"key2": float64(42),
					"key3": []interface{}{float64(1), float64(2), float64(3)},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid json",
			outputs: map[string]*tfexec.OutputMeta{
				"invalid_output": {
					Value: json.RawMessage(`invalid json`),
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := convertTofuOutputs(tt.outputs)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)

			var decoded map[string]interface{}
			err = json.Unmarshal(result, &decoded)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, decoded)
		})
	}
}
