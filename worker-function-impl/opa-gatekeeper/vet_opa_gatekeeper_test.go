// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package opagatekeeper

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	admissionwebhook "github.com/confighub/sdk/worker-function-impl/k8s-admission-webhook"
)

const testDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
  namespace: default
  labels:
    app: test
spec:
  replicas: 3
  selector:
    matchLabels:
      app: test
  template:
    metadata:
      labels:
        app: test
    spec:
      containers:
      - name: test-container
        image: nginx:1.21
        ports:
        - containerPort: 80
`

const testDeploymentNoLabels = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: no-labels-deployment
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: test
  template:
    metadata:
      labels:
        app: test
    spec:
      containers:
      - name: test-container
        image: nginx:latest
`

// catchAllWebhooks returns a webhook endpoint that matches all resources.
func catchAllWebhooks() []admissionwebhook.WebhookEndpoint {
	return []admissionwebhook.WebhookEndpoint{{
		Name: "catch-all",
		Path: "/v1/admit",
		Rules: []admissionwebhook.WebhookRuleSpec{{
			APIGroups:   []string{"*"},
			APIVersions: []string{"*"},
			Operations:  []string{"*"},
			Resources:   []string{"*"},
		}},
	}}
}

func newMockGatekeeperServer(t *testing.T, handler func(path string, req admissionwebhook.AdmissionReview) admissionwebhook.AdmissionReview) (*httptest.Server, *admissionwebhook.WebhookClient) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req admissionwebhook.AdmissionReview
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		resp := handler(r.URL.Path, req)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))

	client := admissionwebhook.NewWebhookClientForTesting(server.Client(), server.URL)
	return server, client
}

func TestVetOPAGatekeeper_AllAllowed(t *testing.T) {
	server, client := newMockGatekeeperServer(t, func(_ string, req admissionwebhook.AdmissionReview) admissionwebhook.AdmissionReview {
		return admissionwebhook.AdmissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &admissionwebhook.AdmissionResponse{
				UID:     req.Request.UID,
				Allowed: true,
			},
		}
	})
	defer server.Close()

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := admissionwebhook.ValidateResources(client, catchAllWebhooks(), rp, parsedData, gatekeeperResponseConverter)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok, "expected ValidationResult, got %T", output)
	assert.True(t, vr.Passed, "expected validation to pass")
}

func TestVetOPAGatekeeper_Denied(t *testing.T) {
	server, client := newMockGatekeeperServer(t, func(_ string, req admissionwebhook.AdmissionReview) admissionwebhook.AdmissionReview {
		return admissionwebhook.AdmissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &admissionwebhook.AdmissionResponse{
				UID:     req.Request.UID,
				Allowed: false,
				Status: &admissionwebhook.Status{
					Status:  "Failure",
					Message: "[require-labels] The label 'team' is required on all deployments.",
				},
			},
		}
	})
	defer server.Close()

	parsedData, err := gaby.ParseAll([]byte(testDeploymentNoLabels))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := admissionwebhook.ValidateResources(client, catchAllWebhooks(), rp, parsedData, gatekeeperResponseConverter)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok, "expected ValidationResult, got %T", output)
	assert.False(t, vr.Passed, "expected validation to fail")
	assert.NotEmpty(t, vr.Details)
	assert.Contains(t, vr.Details[0], "require-labels")
	assert.NotEmpty(t, vr.FailedAttributes)
	assert.Equal(t, "require-labels", vr.FailedAttributes[0].Issues[0].Identifier)
}

func TestVetOPAGatekeeper_MultipleConstraints(t *testing.T) {
	server, client := newMockGatekeeperServer(t, func(_ string, req admissionwebhook.AdmissionReview) admissionwebhook.AdmissionReview {
		return admissionwebhook.AdmissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &admissionwebhook.AdmissionResponse{
				UID:     req.Request.UID,
				Allowed: false,
				Status: &admissionwebhook.Status{
					Status: "Failure",
					Message: "[require-labels] The label 'team' is required.\n" +
						"[disallow-latest-tag] Using 'latest' tag is not allowed.",
				},
			},
		}
	})
	defer server.Close()

	parsedData, err := gaby.ParseAll([]byte(testDeploymentNoLabels))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := admissionwebhook.ValidateResources(client, catchAllWebhooks(), rp, parsedData, gatekeeperResponseConverter)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed)
	assert.Len(t, vr.Details, 2)
	assert.Contains(t, vr.Details[0], "require-labels")
	assert.Contains(t, vr.Details[1], "disallow-latest-tag")
	assert.Len(t, vr.FailedAttributes, 2)
}

func TestVetOPAGatekeeper_WithWarnings(t *testing.T) {
	server, client := newMockGatekeeperServer(t, func(_ string, req admissionwebhook.AdmissionReview) admissionwebhook.AdmissionReview {
		return admissionwebhook.AdmissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &admissionwebhook.AdmissionResponse{
				UID:      req.Request.UID,
				Allowed:  true,
				Warnings: []string{"constraint audit-constraint: Missing recommended annotation"},
			},
		}
	})
	defer server.Close()

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := admissionwebhook.ValidateResources(client, catchAllWebhooks(), rp, parsedData, gatekeeperResponseConverter)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed, "warnings should not cause failure")
	assert.NotEmpty(t, vr.Details)
	assert.Contains(t, vr.Details[0], "warning:")
}

func TestVetOPAGatekeeper_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := admissionwebhook.NewWebhookClientForTesting(server.Client(), server.URL)

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, _, err = admissionwebhook.ValidateResources(client, catchAllWebhooks(), rp, parsedData, gatekeeperResponseConverter)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
}

func TestVetOPAGatekeeper_ConnectionError(t *testing.T) {
	client := admissionwebhook.NewWebhookClientForTesting(http.DefaultClient, "http://localhost:1")

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, _, err = admissionwebhook.ValidateResources(client, catchAllWebhooks(), rp, parsedData, gatekeeperResponseConverter)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send request to webhook server")
}

// Unit tests for message parsing.

func TestParseGatekeeperMessage(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		wantCount  int
		wantFirst  gatekeeperViolation
	}{
		{
			"single constraint",
			"[require-labels] The label 'team' is required.",
			1,
			gatekeeperViolation{constraintName: "require-labels", message: "The label 'team' is required."},
		},
		{
			"multiple constraints",
			"[require-labels] The label 'team' is required.\n[disallow-latest-tag] Using 'latest' tag is not allowed.",
			2,
			gatekeeperViolation{constraintName: "require-labels", message: "The label 'team' is required."},
		},
		{
			"with empty lines",
			"\n[require-labels] The label 'team' is required.\n\n[disallow-latest-tag] Using 'latest' is not allowed.\n",
			2,
			gatekeeperViolation{constraintName: "require-labels", message: "The label 'team' is required."},
		},
		{
			"empty message",
			"",
			0,
			gatekeeperViolation{},
		},
		{
			"no bracket format",
			"some unstructured error message",
			0,
			gatekeeperViolation{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			violations := parseGatekeeperMessage(tt.message)
			assert.Len(t, violations, tt.wantCount)
			if tt.wantCount > 0 {
				assert.Equal(t, tt.wantFirst.constraintName, violations[0].constraintName)
				assert.Equal(t, tt.wantFirst.message, violations[0].message)
			}
		})
	}
}
