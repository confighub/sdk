// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package flux

import (
	"context"
	"testing"

	"github.com/confighub/sdk/core/worker/api"
	"github.com/stretchr/testify/mock"
)

// Common test data for Flux tests
var (
	testConfigMapYAML = []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-configmap
  namespace: default
data:
  key: value
`)
)

// MockBridgeWorkerContext is a mock implementation of api.BridgeWorkerContext for testing.
type MockBridgeWorkerContext struct {
	mock.Mock
}

func (m *MockBridgeWorkerContext) Context() context.Context {
	return context.Background()
}

func (m *MockBridgeWorkerContext) SendStatus(result *api.ActionResult) error {
	args := m.Called(result)
	return args.Error(0)
}

func (m *MockBridgeWorkerContext) GetServerURL() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockBridgeWorkerContext) GetWorkerID() string {
	args := m.Called()
	return args.String(0)
}

func setupMockContext(t *testing.T) *MockBridgeWorkerContext {
	t.Helper()
	mockCtx := new(MockBridgeWorkerContext)
	mockCtx.On("SendStatus", mock.Anything).Return(nil).Maybe()
	return mockCtx
}
