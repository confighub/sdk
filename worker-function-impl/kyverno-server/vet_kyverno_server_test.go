// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kyvernoserver

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

// catchAllWebhooks returns a webhook endpoint that matches all resources.
func catchAllWebhooks() []webhookEndpoint {
	return []webhookEndpoint{{
		name: "catch-all",
		path: "/validate/test",
		rules: []webhookRuleSpec{{
			APIGroups:   []string{"*"},
			APIVersions: []string{"*"},
			Operations:  []string{"*"},
			Resources:   []string{"*"},
		}},
	}}
}

// newMockKyvernoServer creates a test HTTP server that simulates the Kyverno webhook.
// The handler function receives the request path and decoded AdmissionReview request.
func newMockKyvernoServer(t *testing.T, handler func(path string, req admissionReview) admissionReview) (*httptest.Server, *kyvernoClient) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req admissionReview
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		resp := handler(r.URL.Path, req)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))

	client := &kyvernoClient{
		httpClient: server.Client(),
		baseURL:    server.URL,
	}
	return server, client
}

func TestVetKyvernoServer_AllAllowed(t *testing.T) {
	server, client := newMockKyvernoServer(t, func(_ string, req admissionReview) admissionReview {
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
	_, output, err := vetKyvernoServer(client, catchAllWebhooks(), rp, parsedData)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok, "expected ValidationResult, got %T", output)
	assert.True(t, vr.Passed, "expected validation to pass")
}

func TestVetKyvernoServer_Denied(t *testing.T) {
	server, client := newMockKyvernoServer(t, func(_ string, req admissionReview) admissionReview {
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
	_, output, err := vetKyvernoServer(client, catchAllWebhooks(), rp, parsedData)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok, "expected ValidationResult, got %T", output)
	assert.False(t, vr.Passed, "expected validation to fail")
	assert.NotEmpty(t, vr.Details)
	assert.Contains(t, vr.Details[0], "require-labels")
	assert.Contains(t, vr.Details[0], "check-for-labels")
	assert.NotEmpty(t, vr.FailedAttributes)
}

func TestVetKyvernoServer_DeniedValidatingPolicy(t *testing.T) {
	server, client := newMockKyvernoServer(t, func(_ string, req admissionReview) admissionReview {
		return admissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &admissionResponse{
				UID:     req.Request.UID,
				Allowed: false,
				Status: &status{
					Status:  "Failure",
					Message: "Policy require-labels failed: The label 'team' is required.",
				},
			},
		}
	})
	defer server.Close()

	parsedData, err := gaby.ParseAll([]byte(testDeploymentNoLabels))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKyvernoServer(client, catchAllWebhooks(), rp, parsedData)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok, "expected ValidationResult, got %T", output)
	assert.False(t, vr.Passed, "expected validation to fail")
	assert.NotEmpty(t, vr.Details)
	assert.Contains(t, vr.Details[0], "require-labels")
	assert.Contains(t, vr.Details[0], "The label 'team' is required.")
	assert.NotEmpty(t, vr.FailedAttributes)
	assert.Equal(t, "require-labels/validation", vr.FailedAttributes[0].Issues[0].Identifier)
}

func TestVetKyvernoServer_MultipleResources(t *testing.T) {
	server, client := newMockKyvernoServer(t, func(_ string, req admissionReview) admissionReview {
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
	_, output, err := vetKyvernoServer(client, catchAllWebhooks(), rp, parsedData)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed, "expected both resources to pass")
}

func TestVetKyvernoServer_MixedResults(t *testing.T) {
	callCount := 0
	server, client := newMockKyvernoServer(t, func(_ string, req admissionReview) admissionReview {
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
	_, output, err := vetKyvernoServer(client, catchAllWebhooks(), rp, parsedData)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed, "expected overall validation to fail")
	assert.NotEmpty(t, vr.Details)
	assert.Equal(t, 2, callCount, "expected one request per resource")
}

func TestVetKyvernoServer_MultipleWebhooksPerResource(t *testing.T) {
	var calledPaths []string
	server, client := newMockKyvernoServer(t, func(path string, req admissionReview) admissionReview {
		calledPaths = append(calledPaths, path)
		allowed := true
		var st *status
		// Second webhook denies.
		if path == "/vpol/disallow-latest" {
			allowed = false
			st = &status{
				Status:  "Failure",
				Message: "Policy disallow-latest failed: Using latest tag is not allowed.",
			}
		}
		return admissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &admissionResponse{
				UID:     req.Request.UID,
				Allowed: allowed,
				Status:  st,
			},
		}
	})
	defer server.Close()

	webhooks := []webhookEndpoint{
		{
			name: "require-labels",
			path: "/vpol/require-labels",
			rules: []webhookRuleSpec{{
				APIGroups:   []string{"apps"},
				APIVersions: []string{"v1"},
				Operations:  []string{"CREATE", "UPDATE"},
				Resources:   []string{"deployments"},
			}},
		},
		{
			name: "disallow-latest",
			path: "/vpol/disallow-latest",
			rules: []webhookRuleSpec{{
				APIGroups:   []string{"apps"},
				APIVersions: []string{"v1"},
				Operations:  []string{"CREATE", "UPDATE"},
				Resources:   []string{"deployments"},
			}},
		},
	}

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKyvernoServer(client, webhooks, rp, parsedData)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed, "expected validation to fail due to second webhook")
	assert.Len(t, calledPaths, 2, "expected both webhooks to be called")
	assert.Equal(t, "/vpol/require-labels", calledPaths[0])
	assert.Equal(t, "/vpol/disallow-latest", calledPaths[1])
}

func TestVetKyvernoServer_NoMatchingWebhooks(t *testing.T) {
	callCount := 0
	server, client := newMockKyvernoServer(t, func(_ string, req admissionReview) admissionReview {
		callCount++
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

	// Webhook only matches pods, not deployments.
	webhooks := []webhookEndpoint{{
		name: "pod-only",
		path: "/vpol/pod-policy",
		rules: []webhookRuleSpec{{
			APIGroups:   []string{""},
			APIVersions: []string{"v1"},
			Operations:  []string{"CREATE"},
			Resources:   []string{"pods"},
		}},
	}}

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKyvernoServer(client, webhooks, rp, parsedData)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed, "no matching webhooks means pass")
	assert.Equal(t, 0, callCount, "no webhooks should be called")
}

func TestVetKyvernoServer_WithWarnings(t *testing.T) {
	server, client := newMockKyvernoServer(t, func(_ string, req admissionReview) admissionReview {
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
	_, output, err := vetKyvernoServer(client, catchAllWebhooks(), rp, parsedData)
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
		httpClient: server.Client(),
		baseURL:    server.URL,
	}

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, _, err = vetKyvernoServer(client, catchAllWebhooks(), rp, parsedData)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
}

func TestVetKyvernoServer_ConnectionError(t *testing.T) {
	client := &kyvernoClient{
		httpClient: http.DefaultClient,
		baseURL:    "http://localhost:1", // nothing listening
	}

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, _, err = vetKyvernoServer(client, catchAllWebhooks(), rp, parsedData)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send request to Kyverno server")
}

func TestVetKyvernoServer_AdmissionReviewFormat(t *testing.T) {
	var capturedReq admissionReview
	server, client := newMockKyvernoServer(t, func(_ string, req admissionReview) admissionReview {
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
	_, _, err = vetKyvernoServer(client, catchAllWebhooks(), rp, parsedData)
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

// Unit tests for webhook matching.

func TestMatchesAny(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		value    string
		want     bool
	}{
		{"exact match", []string{"apps"}, "apps", true},
		{"no match", []string{"apps"}, "batch", false},
		{"wildcard", []string{"*"}, "apps", true},
		{"wildcard empty", []string{"*"}, "", true},
		{"multiple patterns", []string{"apps", "batch"}, "batch", true},
		{"empty patterns", []string{}, "apps", false},
		{"empty string match", []string{""}, "", true},
		{"core group", []string{""}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, matchesAny(tt.patterns, tt.value))
		})
	}
}

func TestWebhookEndpointMatchesResource(t *testing.T) {
	wh := webhookEndpoint{
		name: "test",
		path: "/test",
		rules: []webhookRuleSpec{
			{
				APIGroups:   []string{"apps"},
				APIVersions: []string{"v1"},
				Operations:  []string{"CREATE", "UPDATE"},
				Resources:   []string{"deployments", "statefulsets"},
			},
			{
				APIGroups:   []string{""},
				APIVersions: []string{"v1"},
				Operations:  []string{"CREATE"},
				Resources:   []string{"pods"},
			},
		},
	}

	tests := []struct {
		name      string
		group     string
		version   string
		resource  string
		operation string
		want      bool
	}{
		{"deployment CREATE", "apps", "v1", "deployments", "CREATE", true},
		{"deployment UPDATE", "apps", "v1", "deployments", "UPDATE", true},
		{"deployment DELETE", "apps", "v1", "deployments", "DELETE", false},
		{"statefulset", "apps", "v1", "statefulsets", "CREATE", true},
		{"pod", "", "v1", "pods", "CREATE", true},
		{"pod UPDATE", "", "v1", "pods", "UPDATE", false},
		{"service", "", "v1", "services", "CREATE", false},
		{"wrong group", "batch", "v1", "deployments", "CREATE", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, wh.matchesResource(tt.group, tt.version, tt.resource, tt.operation))
		})
	}
}

func TestWebhookEndpointMatchesResource_Wildcard(t *testing.T) {
	wh := webhookEndpoint{
		name: "catch-all",
		path: "/validate/all",
		rules: []webhookRuleSpec{{
			APIGroups:   []string{"*"},
			APIVersions: []string{"*"},
			Operations:  []string{"*"},
			Resources:   []string{"*"},
		}},
	}

	assert.True(t, wh.matchesResource("apps", "v1", "deployments", "CREATE"))
	assert.True(t, wh.matchesResource("", "v1", "pods", "DELETE"))
	assert.True(t, wh.matchesResource("batch", "v1beta1", "cronjobs", "UPDATE"))
}

// Unit tests for webhook discovery parsing.

func TestParseWebhookEndpoints(t *testing.T) {
	path1 := "/vpol/require-labels"
	path2 := "/validate/fail"

	list := vwcList{
		Items: []vwcItem{
			{
				Metadata: vwcMetadata{Name: kyvernoResourceWebhookConfigName},
				Webhooks: []vwcWebhook{
					{
						Name: "vpol.require-labels",
						ClientConfig: vwcClientConfig{
							Service: &vwcServiceRef{Path: &path1},
						},
						Rules: []webhookRuleSpec{{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Operations:  []string{"CREATE", "UPDATE"},
							Resources:   []string{"pods"},
						}},
					},
					{
						Name: "validate.kyverno.svc",
						ClientConfig: vwcClientConfig{
							Service: &vwcServiceRef{Path: &path2},
						},
						Rules: []webhookRuleSpec{{
							APIGroups:   []string{"*"},
							APIVersions: []string{"*"},
							Operations:  []string{"CREATE", "UPDATE"},
							Resources:   []string{"*"},
						}},
					},
				},
			},
			{
				// Non-resource webhook config (for policy exceptions) - filtered out
				// because it's not the resource validation config.
				Metadata: vwcMetadata{Name: "kyverno-exception-webhook"},
				Webhooks: []vwcWebhook{
					{
						Name: "exception-webhook",
						ClientConfig: vwcClientConfig{
							Service: &vwcServiceRef{Path: &path1},
						},
						Rules: []webhookRuleSpec{{
							APIGroups:   []string{"kyverno.io"},
							APIVersions: []string{"*"},
							Operations:  []string{"*"},
							Resources:   []string{"*"},
						}},
					},
				},
			},
		},
	}

	endpoints := parseWebhookEndpoints(list)
	require.Len(t, endpoints, 2, "only webhooks from the resource config should be included")
	assert.Equal(t, "vpol.require-labels", endpoints[0].name)
	assert.Equal(t, "/vpol/require-labels", endpoints[0].path)
	assert.Len(t, endpoints[0].rules, 1)
	assert.Equal(t, "validate.kyverno.svc", endpoints[1].name)
	assert.Equal(t, "/validate/fail", endpoints[1].path)
}

func TestParseWebhookEndpoints_Empty(t *testing.T) {
	endpoints := parseWebhookEndpoints(vwcList{})
	assert.Empty(t, endpoints)
}

// Unit tests for message parsing.

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

func TestParseKyvernoMessage_ValidatingPolicyFormat(t *testing.T) {
	msg := "Policy require-labels failed: The label 'team' is required."
	failures := parseKyvernoMessage(msg)
	require.Len(t, failures, 1)
	assert.Equal(t, "require-labels", failures[0].policyName)
	assert.Equal(t, "validation", failures[0].ruleName)
	assert.Equal(t, "The label 'team' is required.", failures[0].message)
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
