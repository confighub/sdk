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
func catchAllWebhooks() []admissionwebhook.WebhookEndpoint {
	return []admissionwebhook.WebhookEndpoint{{
		Name: "catch-all",
		Path: "/validate/test",
		Rules: []admissionwebhook.WebhookRuleSpec{{
			APIGroups:   []string{"*"},
			APIVersions: []string{"*"},
			Operations:  []string{"*"},
			Resources:   []string{"*"},
		}},
	}}
}

// newMockKyvernoServer creates a test HTTP server that simulates the Kyverno webhook.
func newMockKyvernoServer(t *testing.T, handler func(path string, req admissionwebhook.AdmissionReview) admissionwebhook.AdmissionReview) (*httptest.Server, *admissionwebhook.WebhookClient) {
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

func TestVetKyvernoServer_AllAllowed(t *testing.T) {
	server, client := newMockKyvernoServer(t, func(_ string, req admissionwebhook.AdmissionReview) admissionwebhook.AdmissionReview {
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
	_, output, err := admissionwebhook.ValidateResources(client, catchAllWebhooks(), rp, parsedData, kyvernoResponseConverter)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok, "expected ValidationResult, got %T", output)
	assert.True(t, vr.Passed, "expected validation to pass")
}

func TestVetKyvernoServer_Denied(t *testing.T) {
	server, client := newMockKyvernoServer(t, func(_ string, req admissionwebhook.AdmissionReview) admissionwebhook.AdmissionReview {
		return admissionwebhook.AdmissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &admissionwebhook.AdmissionResponse{
				UID:     req.Request.UID,
				Allowed: false,
				Status: &admissionwebhook.Status{
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
	_, output, err := admissionwebhook.ValidateResources(client, catchAllWebhooks(), rp, parsedData, kyvernoResponseConverter)
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
	server, client := newMockKyvernoServer(t, func(_ string, req admissionwebhook.AdmissionReview) admissionwebhook.AdmissionReview {
		return admissionwebhook.AdmissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &admissionwebhook.AdmissionResponse{
				UID:     req.Request.UID,
				Allowed: false,
				Status: &admissionwebhook.Status{
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
	_, output, err := admissionwebhook.ValidateResources(client, catchAllWebhooks(), rp, parsedData, kyvernoResponseConverter)
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
	server, client := newMockKyvernoServer(t, func(_ string, req admissionwebhook.AdmissionReview) admissionwebhook.AdmissionReview {
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

	parsedData, err := gaby.ParseAll([]byte(multiDocResources))
	require.NoError(t, err)
	require.Len(t, parsedData, 2, "expected 2 documents")

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := admissionwebhook.ValidateResources(client, catchAllWebhooks(), rp, parsedData, kyvernoResponseConverter)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed, "expected both resources to pass")
}

func TestVetKyvernoServer_MixedResults(t *testing.T) {
	callCount := 0
	server, client := newMockKyvernoServer(t, func(_ string, req admissionwebhook.AdmissionReview) admissionwebhook.AdmissionReview {
		callCount++
		// First resource passes, second fails.
		if callCount == 1 {
			return admissionwebhook.AdmissionReview{
				APIVersion: "admission.k8s.io/v1",
				Kind:       "AdmissionReview",
				Response: &admissionwebhook.AdmissionResponse{
					UID:     req.Request.UID,
					Allowed: true,
				},
			}
		}
		return admissionwebhook.AdmissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &admissionwebhook.AdmissionResponse{
				UID:     req.Request.UID,
				Allowed: false,
				Status: &admissionwebhook.Status{
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
	_, output, err := admissionwebhook.ValidateResources(client, catchAllWebhooks(), rp, parsedData, kyvernoResponseConverter)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed, "expected overall validation to fail")
	assert.NotEmpty(t, vr.Details)
	assert.Equal(t, 2, callCount, "expected one request per resource")
}

func TestVetKyvernoServer_MultipleWebhooksPerResource(t *testing.T) {
	var calledPaths []string
	server, client := newMockKyvernoServer(t, func(path string, req admissionwebhook.AdmissionReview) admissionwebhook.AdmissionReview {
		calledPaths = append(calledPaths, path)
		allowed := true
		var st *admissionwebhook.Status
		// Second webhook denies.
		if path == "/vpol/disallow-latest" {
			allowed = false
			st = &admissionwebhook.Status{
				Status:  "Failure",
				Message: "Policy disallow-latest failed: Using latest tag is not allowed.",
			}
		}
		return admissionwebhook.AdmissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &admissionwebhook.AdmissionResponse{
				UID:     req.Request.UID,
				Allowed: allowed,
				Status:  st,
			},
		}
	})
	defer server.Close()

	webhooks := []admissionwebhook.WebhookEndpoint{
		{
			Name: "require-labels",
			Path: "/vpol/require-labels",
			Rules: []admissionwebhook.WebhookRuleSpec{{
				APIGroups:   []string{"apps"},
				APIVersions: []string{"v1"},
				Operations:  []string{"CREATE", "UPDATE"},
				Resources:   []string{"deployments"},
			}},
		},
		{
			Name: "disallow-latest",
			Path: "/vpol/disallow-latest",
			Rules: []admissionwebhook.WebhookRuleSpec{{
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
	_, output, err := admissionwebhook.ValidateResources(client, webhooks, rp, parsedData, kyvernoResponseConverter)
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
	server, client := newMockKyvernoServer(t, func(_ string, req admissionwebhook.AdmissionReview) admissionwebhook.AdmissionReview {
		callCount++
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

	// Webhook only matches pods, not deployments.
	webhooks := []admissionwebhook.WebhookEndpoint{{
		Name: "pod-only",
		Path: "/vpol/pod-policy",
		Rules: []admissionwebhook.WebhookRuleSpec{{
			APIGroups:   []string{""},
			APIVersions: []string{"v1"},
			Operations:  []string{"CREATE"},
			Resources:   []string{"pods"},
		}},
	}}

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := admissionwebhook.ValidateResources(client, webhooks, rp, parsedData, kyvernoResponseConverter)
	require.NoError(t, err)

	// With no matching webhooks, the result is the "true" shorthand.
	assert.Equal(t, api.ValidationResultTrue, output)
	assert.Equal(t, 0, callCount, "no webhooks should be called")
}

func TestVetKyvernoServer_WithWarnings(t *testing.T) {
	server, client := newMockKyvernoServer(t, func(_ string, req admissionwebhook.AdmissionReview) admissionwebhook.AdmissionReview {
		return admissionwebhook.AdmissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &admissionwebhook.AdmissionResponse{
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
	_, output, err := admissionwebhook.ValidateResources(client, catchAllWebhooks(), rp, parsedData, kyvernoResponseConverter)
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

	client := admissionwebhook.NewWebhookClientForTesting(server.Client(), server.URL)

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, _, err = admissionwebhook.ValidateResources(client, catchAllWebhooks(), rp, parsedData, kyvernoResponseConverter)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
}

func TestVetKyvernoServer_ConnectionError(t *testing.T) {
	client := admissionwebhook.NewWebhookClientForTesting(http.DefaultClient, "http://localhost:1")

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, _, err = admissionwebhook.ValidateResources(client, catchAllWebhooks(), rp, parsedData, kyvernoResponseConverter)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send request to webhook server")
}

func TestVetKyvernoServer_AdmissionReviewFormat(t *testing.T) {
	var capturedReq admissionwebhook.AdmissionReview
	server, client := newMockKyvernoServer(t, func(_ string, req admissionwebhook.AdmissionReview) admissionwebhook.AdmissionReview {
		capturedReq = req
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
	_, _, err = admissionwebhook.ValidateResources(client, catchAllWebhooks(), rp, parsedData, kyvernoResponseConverter)
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

func TestParseKyvernoMessage_WithPath(t *testing.T) {
	msg := `

resource Deployment/default/test was blocked due to the following policies

disallow-latest-tag:
  validate-image-tag: validation error: Using 'latest' tag is not allowed. rule disallow-latest-tag failed at path /spec/template/spec/containers/0/image/
`
	failures := parseKyvernoMessage(msg)
	require.Len(t, failures, 1)
	assert.Equal(t, "disallow-latest-tag", failures[0].policyName)
	assert.Equal(t, "validate-image-tag", failures[0].ruleName)
	assert.Equal(t, "spec.template.spec.containers.0.image", failures[0].path)
}

func TestParseKyvernoMessage_WithPathNoTrailingSlash(t *testing.T) {
	msg := `

resource Pod/default/test was blocked due to the following policies

security-policy:
  require-non-root: validation error: Containers must not run as root. rule require-non-root failed at path /spec/containers/0/securityContext/runAsNonRoot
`
	failures := parseKyvernoMessage(msg)
	require.Len(t, failures, 1)
	assert.Equal(t, "spec.containers.0.securityContext.runAsNonRoot", failures[0].path)
}

func TestParseKyvernoMessage_ImageValidatingPolicy(t *testing.T) {
	// ImageValidatingPolicy format (handled by validatingPolicyRE)
	msg := "Policy verify-image failed: image signature verification failed"
	failures := parseKyvernoMessage(msg)
	require.Len(t, failures, 1)
	assert.Equal(t, "verify-image", failures[0].policyName)
	assert.Equal(t, "validation", failures[0].ruleName)
	assert.Equal(t, "image signature verification failed", failures[0].message)
}

func TestExtractPathFromMessage(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{
			"with path trailing slash",
			"validation error: Using 'latest' tag is not allowed. rule disallow-latest-tag failed at path /spec/template/spec/containers/0/image/",
			"spec.template.spec.containers.0.image",
		},
		{
			"with path no trailing slash",
			"validation error: test. rule test failed at path /spec/containers/0/securityContext/runAsNonRoot",
			"spec.containers.0.securityContext.runAsNonRoot",
		},
		{
			"no path",
			"The label 'app' is required.",
			"",
		},
		{
			"empty",
			"",
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractPathFromMessage(tt.message))
		})
	}
}

func TestKyvernoResponseConverter_WithPath(t *testing.T) {
	resp := &admissionwebhook.AdmissionResponse{
		Allowed: false,
		Status: &admissionwebhook.Status{
			Status:  "Failure",
			Message: "\n\nresource Deployment/default/test was blocked due to the following policies\n\ndisallow-latest-tag:\n  validate-image-tag: validation error: Using 'latest' tag is not allowed. rule disallow-latest-tag failed at path /spec/template/spec/containers/0/image/\n",
		},
	}
	resourceInfo := api.ResourceInfo{
		ResourceType: "apps/v1/Deployment",
		ResourceName: "default/test",
	}

	details, failedAttrs := kyvernoResponseConverter(resp, resourceInfo)
	require.NotEmpty(t, details)
	require.Len(t, failedAttrs, 1)
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.0.image"), failedAttrs[0].Path)
}
