// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/fluxcd/pkg/oci"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockOCIClient mocks the OCIClient for testing
type MockOCIClient struct {
	mock.Mock
}

func (m *MockOCIClient) Delete(ctx context.Context, url string) error {
	args := m.Called(ctx, url)
	return args.Error(0)
}

func (m *MockOCIClient) GetOptions() []crane.Option {
	args := m.Called()
	return args.Get(0).([]crane.Option)
}

func (m *MockOCIClient) LoginWithCredentials(cred string) error {
	args := m.Called(cred)
	return args.Error(0)
}

func (m *MockOCIClient) LoginWithProvider(ctx context.Context, url string, provider oci.Provider) error {
	args := m.Called(url, provider)
	return args.Error(0)
}

func TestParseFluxOCIParams_ValidPayloadWithRepositoryAndTag(t *testing.T) {
	payload := api.BridgeWorkerPayload{
		TargetParams: json.RawMessage(`{"Repository":"ghcr.io/test","Tag":"v1.0"}`),
		UnitSlug:     "repo",
		RevisionNum:  1, // Add RevisionNum to satisfy the new requirement
	}

	expected := &FluxOCIParams{
		Repository: "ghcr.io/test/repo",
		RevTag:     "rev1", // Add RevTag to match the new behavior
		Tag:        "v1.0",
	}

	result, err := ParseFluxOCIParams(payload)
	assert.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestParseFluxOCIParams_ValidPayloadWithRevisionNumFallback(t *testing.T) {
	payload := api.BridgeWorkerPayload{
		TargetParams: json.RawMessage(`{"Repository":"ghcr.io/test"}`),
		RevisionNum:  42,
		UnitSlug:     "repo",
	}

	expected := &FluxOCIParams{
		Repository: "ghcr.io/test/repo",
		RevTag:     "rev42", // Add RevTag to match the new behavior
	}

	result, err := ParseFluxOCIParams(payload)
	assert.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestParseFluxOCIParams_MissingRepository(t *testing.T) {
	payload := api.BridgeWorkerPayload{
		TargetParams: json.RawMessage(`{"Tag":"v1.0"}`),
		UnitSlug:     "repo",
		RevisionNum:  1, // Add RevisionNum to satisfy the new requirement
	}

	result, err := ParseFluxOCIParams(payload)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "repository and Unit slug are required")
}

func TestParseFluxOCIParams_MissingTagAndRevisionNum(t *testing.T) {
	payload := api.BridgeWorkerPayload{
		TargetParams: json.RawMessage(`{"Repository":"ghcr.io/test/repo"}`),
		UnitSlug:     "repo",
	}

	result, err := ParseFluxOCIParams(payload)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "revision number is required")
}

func TestParseFluxOCIParams_InvalidJSONInTargetParams(t *testing.T) {
	payload := api.BridgeWorkerPayload{
		TargetParams: json.RawMessage(`{"Repository":"ghcr.io/test/repo",`),
	}

	result, err := ParseFluxOCIParams(payload)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to parse target params")
}

func TestParseFluxOCIParams_ValidPayloadWithRepositoryAndUnitSlug(t *testing.T) {
	payload := api.BridgeWorkerPayload{
		TargetParams: json.RawMessage(`{"Repository":"ghcr.io/test","Tag":"v1.0"}`),
		UnitSlug:     "repo",
		RevisionNum:  1, // Add RevisionNum to satisfy the new requirement
	}

	expected := &FluxOCIParams{
		Repository: "ghcr.io/test/repo",
		RevTag:     "rev1", // Add RevTag to match the new behavior
		Tag:        "v1.0",
	}

	result, err := ParseFluxOCIParams(payload)
	assert.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestParseFluxOCIParams_MissingUnitSlug(t *testing.T) {
	payload := api.BridgeWorkerPayload{
		TargetParams: json.RawMessage(`{"Repository":"ghcr.io/test","Tag":"v1.0"}`),
		RevisionNum:  1, // Add RevisionNum to satisfy the new requirement
	}

	result, err := ParseFluxOCIParams(payload)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "repository and Unit slug are required")
}

func TestParseFluxOCIParams_EmptyRepositoryAndUnitSlug(t *testing.T) {
	payload := api.BridgeWorkerPayload{
		TargetParams: json.RawMessage(`{"Tag":"v1.0"}`),
		UnitSlug:     "",
		RevisionNum:  1, // Add RevisionNum to satisfy the new requirement
	}

	result, err := ParseFluxOCIParams(payload)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "repository and Unit slug are required")
}

func TestParseFluxOCIParams_ValidPayloadWithUnitSlugOnly(t *testing.T) {
	payload := api.BridgeWorkerPayload{
		TargetParams: json.RawMessage(`{"Repository":"ghcr.io/test"}`),
		UnitSlug:     "repo",
		RevisionNum:  1,
	}

	expected := &FluxOCIParams{
		Repository: "ghcr.io/test/repo",
		RevTag:     "rev1", // Update to match the implementation
		Tag:        "",     // Tag is not set in this case
	}

	result, err := ParseFluxOCIParams(payload)
	assert.NoError(t, err)
	assert.Equal(t, expected, result)
}

// Test Apply Method
func TestFluxOCIWorker_Apply_Success(t *testing.T) {
	mockCtx := new(MockBridgeWorkerContext)
	mockCtx.On("Context").Return(context.Background())

	// Mock SendStatus for the first call (Progressing state)
	mockCtx.On("SendStatus", mock.MatchedBy(func(result *api.ActionResult) bool {
		return result.Status == api.ActionStatusProgressing &&
			result.Result == api.ActionResultNone &&
			result.Message == "Pushing to OCI repository..."
	})).Return(nil).Once()

	// Mock SendStatus for the second call (Completed state)
	mockCtx.On("SendStatus", mock.MatchedBy(func(result *api.ActionResult) bool {
		if result.Status != api.ActionStatusCompleted ||
			result.Result != api.ActionResultApplyCompleted ||
			!strings.Contains(result.Message, "Successfully pushed to OCI repository") ||
			result.Outputs == nil {
			return false
		}
		// Unmarshal outputs and check digests are the same for all tags
		var outputs []map[string]string
		err := json.Unmarshal(result.Outputs, &outputs)
		if err != nil {
			return false
		}
		if len(outputs) < 1 {
			return false
		}
		firstDigest := outputs[0]["Digest"]
		for _, o := range outputs {
			if o["Digest"] != firstDigest {
				return false
			}
		}
		return true
	})).Return(nil).Once()

	mockOCIClient := new(MockOCIClient)
	// Mock the push function
	originalPushFunc := pushFunc
	pushFunc = func(cli OCIClient, tarGz []byte, url string, tags ...string) (string, error) {
		assert.NotNil(t, cli)
		// Always return the same digest regardless of tag
		return "digest-same-for-all-tags", nil
	}
	defer func() { pushFunc = originalPushFunc }() // Restore the original function after the test

	mockLoginToRegistry := func(ctx context.Context, workerConfig *FluxOCIWorkerConfig, params *FluxOCIParams, newClientFunc NewClientFunc) (OCIClient, error) {
		// skips LoginWithCredentials and LoginWithProvider
		// and directly returns the mock client
		return mockOCIClient, nil
	}

	worker := FluxOCIWorker{
		LoginToRegistryFunc: mockLoginToRegistry,
	}
	payload := api.BridgeWorkerPayload{
		TargetParams: []byte(`{"Repository":"ghcr.io/test","Tag":"v1.0","Provider":"Generic"}`),
		UnitSlug:     "repo",
		RevisionNum:  1,
		Data:         []byte("mock manifest data"),
	}

	err := worker.Apply(mockCtx, payload)

	assert.NoError(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2) // Ensure SendStatus is called twice

	// Validate that pushFunc was called
	mockOCIClient.AssertExpectations(t)
}

// Test Apply Method with Error
func TestFluxOCIWorker_Apply_Error(t *testing.T) {
	mockCtx := new(MockBridgeWorkerContext)
	mockCtx.On("Context").Return(context.Background())

	// Mock SendStatus to assert exact values
	mockCtx.On("SendStatus", mock.MatchedBy(func(result *api.ActionResult) bool {
		return result.Status == api.ActionStatusFailed &&
			result.Result == api.ActionResultApplyFailed &&
			assert.Contains(t, result.Message, "revision number is required, got 0")
	})).Return(nil).Once()

	mockOCIClient := new(MockOCIClient)

	mockLoginToRegistry := func(ctx context.Context, workerConfig *FluxOCIWorkerConfig, params *FluxOCIParams, newClientFunc NewClientFunc) (OCIClient, error) {
		return mockOCIClient, nil
	}

	worker := FluxOCIWorker{
		LoginToRegistryFunc: mockLoginToRegistry,
	}
	payload := api.BridgeWorkerPayload{
		TargetParams: []byte(`{"Repository":"","Tag":""}`), // Invalid params
		Data:         []byte("mock manifest data"),
	}

	err := worker.Apply(mockCtx, payload)

	assert.Error(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 1) // Ensure SendStatus is called once
}

// Define shared constants and helper functions to reduce duplicate code
const (
	repositoryBaseURL = "ghcr.io/test"
	repoUnitSlug      = "repo"
	revTagPrefix      = "rev"
)

func createMockBridgeWorkerPayload(repository, tag, allowDeletion string, revisionNum int64) api.BridgeWorkerPayload {
	targetParams := map[string]string{
		"Repository":    repository,
		"Tag":           tag,
		"AllowDeletion": allowDeletion,
	}
	targetParamsJSON, _ := json.Marshal(targetParams)

	return api.BridgeWorkerPayload{
		TargetParams: targetParamsJSON,
		UnitSlug:     repoUnitSlug,
		RevisionNum:  revisionNum,
	}
}

func createMockSendStatusMatcher(status api.ActionStatusType, result api.ActionResultType, messageContains string) func(*api.ActionResult) bool {
	return func(resultObj *api.ActionResult) bool {
		return resultObj.Status == status &&
			resultObj.Result == result &&
			strings.Contains(resultObj.Message, messageContains)
	}
}

// Refactor TestFluxOCIWorker_Destroy_Success to use shared constants and helper functions
func TestFluxOCIWorker_Destroy_Success(t *testing.T) {
	mockCtx := new(MockBridgeWorkerContext)
	mockCtx.On("Context").Return(context.Background())

	// Mock SendStatus for the first call (Progressing state)
	mockCtx.On("SendStatus", mock.MatchedBy(createMockSendStatusMatcher(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Deleting from OCI repository...",
	))).Return(nil).Once()

	// Mock SendStatus for the second call (Completed state)
	mockCtx.On("SendStatus", mock.MatchedBy(createMockSendStatusMatcher(
		api.ActionStatusCompleted,
		api.ActionResultDestroyCompleted,
		"Successfully deleted from OCI repository",
	))).Return(nil).Once()

	mockOCIClient := new(MockOCIClient)

	// Mock Delete calls for both RevTag and Tag URLs
	revTagURL := repositoryBaseURL + "/" + repoUnitSlug + ":" + revTagPrefix + "1"
	tagURL := repositoryBaseURL + "/" + repoUnitSlug + ":v1.0"
	mockOCIClient.On("Delete", mock.Anything, revTagURL).Return(nil).Once()
	mockOCIClient.On("Delete", mock.Anything, tagURL).Return(nil).Once()

	mockLoginToRegistry := func(ctx context.Context, workerConfig *FluxOCIWorkerConfig, params *FluxOCIParams, newClientFunc NewClientFunc) (OCIClient, error) {
		return mockOCIClient, nil
	}

	worker := FluxOCIWorker{
		LoginToRegistryFunc: mockLoginToRegistry,
	}
	payload := createMockBridgeWorkerPayload(repositoryBaseURL, "v1.0", "true", 1)

	err := worker.Destroy(mockCtx, payload)

	assert.NoError(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2) // Ensure SendStatus is called twice
	mockOCIClient.AssertCalled(t, "Delete", mock.Anything, revTagURL)
	mockOCIClient.AssertCalled(t, "Delete", mock.Anything, tagURL)
}

// Refactor TestFluxOCIWorker_Destroy_DeletionNotAllowed to use shared constants and helper functions
func TestFluxOCIWorker_Destroy_DeletionNotAllowed(t *testing.T) {
	mockCtx := new(MockBridgeWorkerContext)
	mockCtx.On("Context").Return(context.Background())

	// Mock SendStatus to assert exact values
	mockCtx.On("SendStatus", mock.MatchedBy(createMockSendStatusMatcher(
		api.ActionStatusFailed,
		api.ActionResultDestroyFailed,
		"image deletion not allowed",
	))).Return(nil).Once()

	worker := FluxOCIWorker{}
	payload := createMockBridgeWorkerPayload(repositoryBaseURL, "v1.0", "false", 1)

	err := worker.Destroy(mockCtx, payload)

	assert.Error(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 1) // Ensure SendStatus is called once
}
