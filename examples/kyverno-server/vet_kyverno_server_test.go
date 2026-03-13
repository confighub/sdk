// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
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

const multiDocResources = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: deploy-one
  namespace: default
  labels:
    app: one
spec:
  replicas: 1
  selector:
    matchLabels:
      app: one
  template:
    metadata:
      labels:
        app: one
    spec:
      containers:
      - name: c1
        image: nginx:1.21
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: deploy-two
  namespace: default
  labels:
    app: two
spec:
  replicas: 1
  selector:
    matchLabels:
      app: two
  template:
    metadata:
      labels:
        app: two
    spec:
      containers:
      - name: c2
        image: nginx:1.22
`

// newMockKyvernoServer creates a test HTTP server that simulates the Kyverno webhook.
// The handler function receives the decoded AdmissionReview request and returns the response.
func newMockKyvernoServer(t *testing.T, handler func(req admissionReview) admissionReview) (*httptest.Server, *kyvernoClient) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, defaultValidatePath, r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req admissionReview
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		resp := handler(req)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))

	client := &kyvernoClient{
		httpClient:   server.Client(),
		baseURL:      server.URL,
		validatePath: defaultValidatePath,
	}
	return server, client
}

func TestVetKyvernoServer_AllAllowed(t *testing.T) {
	server, client := newMockKyvernoServer(t, func(req admissionReview) admissionReview {
		return admissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &admissionResponse{
				UID:     req.Request.UID,
				Allowed: true,
			},
		}
	})
	defer server.Close()

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKyvernoServer(client, rp, parsedData)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok, "expected ValidationResult, got %T", output)
	assert.True(t, vr.Passed, "expected validation to pass")
}

func TestVetKyvernoServer_Denied(t *testing.T) {
	server, client := newMockKyvernoServer(t, func(req admissionReview) admissionReview {
		return admissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &admissionResponse{
				UID:     req.Request.UID,
				Allowed: false,
				Status: &status{
					Status:  "Failure",
					Message: "\n\nresource Deployment/default/no-labels-deployment was blocked due to the following policies\n\nrequire-labels:\n  check-for-labels: The label 'app' is required.\n",
				},
			},
		}
	})
	defer server.Close()

	parsedData, err := gaby.ParseAll([]byte(testDeploymentNoLabels))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKyvernoServer(client, rp, parsedData)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok, "expected ValidationResult, got %T", output)
	assert.False(t, vr.Passed, "expected validation to fail")
	assert.NotEmpty(t, vr.Details)
	assert.Contains(t, vr.Details[0], "require-labels")
	assert.Contains(t, vr.Details[0], "check-for-labels")
	assert.NotEmpty(t, vr.FailedAttributes)
}

func TestVetKyvernoServer_MultipleResources(t *testing.T) {
	server, client := newMockKyvernoServer(t, func(req admissionReview) admissionReview {
		return admissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &admissionResponse{
				UID:     req.Request.UID,
				Allowed: true,
			},
		}
	})
	defer server.Close()

	parsedData, err := gaby.ParseAll([]byte(multiDocResources))
	require.NoError(t, err)
	require.Len(t, parsedData, 2, "expected 2 documents")

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKyvernoServer(client, rp, parsedData)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed, "expected both resources to pass")
}

func TestVetKyvernoServer_MixedResults(t *testing.T) {
	callCount := 0
	server, client := newMockKyvernoServer(t, func(req admissionReview) admissionReview {
		callCount++
		// First resource passes, second fails.
		if callCount == 1 {
			return admissionReview{
				APIVersion: "admission.k8s.io/v1",
				Kind:       "AdmissionReview",
				Response: &admissionResponse{
					UID:     req.Request.UID,
					Allowed: true,
				},
			}
		}
		return admissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &admissionResponse{
				UID:     req.Request.UID,
				Allowed: false,
				Status: &status{
					Status:  "Failure",
					Message: "\n\nresource Deployment/default/deploy-two was blocked due to the following policies\n\ndisallow-latest:\n  validate-tag: Using latest tag is not allowed.\n",
				},
			},
		}
	})
	defer server.Close()

	parsedData, err := gaby.ParseAll([]byte(multiDocResources))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKyvernoServer(client, rp, parsedData)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed, "expected overall validation to fail")
	assert.NotEmpty(t, vr.Details)
	assert.Equal(t, 2, callCount, "expected one request per resource")
}

func TestVetKyvernoServer_WithWarnings(t *testing.T) {
	server, client := newMockKyvernoServer(t, func(req admissionReview) admissionReview {
		return admissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &admissionResponse{
				UID:      req.Request.UID,
				Allowed:  true,
				Warnings: []string{"policy audit-policy.audit-rule: Missing recommended annotation"},
			},
		}
	})
	defer server.Close()

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKyvernoServer(client, rp, parsedData)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed, "warnings should not cause failure")
	assert.NotEmpty(t, vr.Details)
	assert.Contains(t, vr.Details[0], "warning:")
}

func TestVetKyvernoServer_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := &kyvernoClient{
		httpClient:   server.Client(),
		baseURL:      server.URL,
		validatePath: defaultValidatePath,
	}

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, _, err = vetKyvernoServer(client, rp, parsedData)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
}

func TestVetKyvernoServer_ConnectionError(t *testing.T) {
	client := &kyvernoClient{
		httpClient:   http.DefaultClient,
		baseURL:      "http://localhost:1", // nothing listening
		validatePath: defaultValidatePath,
	}

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, _, err = vetKyvernoServer(client, rp, parsedData)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send request to Kyverno server")
}

func TestVetKyvernoServer_AdmissionReviewFormat(t *testing.T) {
	var capturedReq admissionReview
	server, client := newMockKyvernoServer(t, func(req admissionReview) admissionReview {
		capturedReq = req
		return admissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &admissionResponse{
				UID:     req.Request.UID,
				Allowed: true,
			},
		}
	})
	defer server.Close()

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, _, err = vetKyvernoServer(client, rp, parsedData)
	require.NoError(t, err)

	// Verify the AdmissionReview request was well-formed.
	assert.Equal(t, "admission.k8s.io/v1", capturedReq.APIVersion)
	assert.Equal(t, "AdmissionReview", capturedReq.Kind)
	require.NotNil(t, capturedReq.Request)
	assert.NotEmpty(t, capturedReq.Request.UID)
	assert.Equal(t, "Deployment", capturedReq.Request.Kind.Kind)
	assert.Equal(t, "apps", capturedReq.Request.Kind.Group)
	assert.Equal(t, "v1", capturedReq.Request.Kind.Version)
	assert.Equal(t, "deployments", capturedReq.Request.Resource.Resource)
	assert.Equal(t, "default", capturedReq.Request.Namespace)
	assert.Equal(t, "test-deployment", capturedReq.Request.Name)
	assert.Equal(t, "CREATE", capturedReq.Request.Operation)
	assert.Equal(t, "confighub", capturedReq.Request.UserInfo.Username)

	// Verify the object is valid JSON containing the resource.
	var obj map[string]any
	err = json.Unmarshal(capturedReq.Request.Object, &obj)
	require.NoError(t, err)
	assert.Equal(t, "Deployment", obj["kind"])
	assert.Equal(t, "apps/v1", obj["apiVersion"])
}

// Unit tests for parsing (no server needed).

func TestParseKyvernoMessage(t *testing.T) {
	msg := `

resource Deployment/default/test was blocked due to the following policies

require-labels:
  check-for-labels: The label 'app' is required.
`
	failures := parseKyvernoMessage(msg)
	require.Len(t, failures, 1)
	assert.Equal(t, "require-labels", failures[0].policyName)
	assert.Equal(t, "check-for-labels", failures[0].ruleName)
	assert.Equal(t, "The label 'app' is required.", failures[0].message)
}

func TestParseKyvernoMessage_MultiplePolicies(t *testing.T) {
	msg := `

resource Deployment/default/test was blocked due to the following policies

require-labels:
  check-for-labels: The label 'app' is required.
disallow-latest-tag:
  validate-image-tag: Using 'latest' tag is not allowed.
`
	failures := parseKyvernoMessage(msg)
	require.Len(t, failures, 2)
	assert.Equal(t, "require-labels", failures[0].policyName)
	assert.Equal(t, "disallow-latest-tag", failures[1].policyName)
}

func TestParseKyvernoMessage_MultipleRules(t *testing.T) {
	msg := `

resource Pod/default/test was blocked due to the following policies

security-policy:
  require-non-root: Containers must not run as root.
  require-read-only-fs: Root filesystem must be read-only.
`
	failures := parseKyvernoMessage(msg)
	require.Len(t, failures, 2)
	assert.Equal(t, "security-policy", failures[0].policyName)
	assert.Equal(t, "require-non-root", failures[0].ruleName)
	assert.Equal(t, "security-policy", failures[1].policyName)
	assert.Equal(t, "require-read-only-fs", failures[1].ruleName)
}

func TestParseKyvernoMessage_Empty(t *testing.T) {
	failures := parseKyvernoMessage("")
	assert.Empty(t, failures)
}

func TestParseAPIVersion(t *testing.T) {
	tests := []struct {
		input   string
		group   string
		version string
	}{
		{"v1", "", "v1"},
		{"apps/v1", "apps", "v1"},
		{"batch/v1beta1", "batch", "v1beta1"},
		{"networking.k8s.io/v1", "networking.k8s.io", "v1"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			g, v := parseAPIVersion(tt.input)
			assert.Equal(t, tt.group, g)
			assert.Equal(t, tt.version, v)
		})
	}
}
