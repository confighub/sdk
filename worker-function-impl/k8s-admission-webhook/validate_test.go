// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package admissionwebhook

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func TestParseResourceType(t *testing.T) {
	tests := []struct {
		input      api.ResourceType
		apiVersion string
		kind       string
	}{
		{"apps/v1/Deployment", "apps/v1", "Deployment"},
		{"v1/Pod", "v1", "Pod"},
		{"rbac.authorization.k8s.io/v1/Role", "rbac.authorization.k8s.io/v1", "Role"},
		{"Deployment", "", "Deployment"},
	}
	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			av, k := ParseResourceType(tt.input)
			assert.Equal(t, tt.apiVersion, av)
			assert.Equal(t, tt.kind, k)
		})
	}
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
			g, v := ParseAPIVersion(tt.input)
			assert.Equal(t, tt.group, g)
			assert.Equal(t, tt.version, v)
		})
	}
}

func TestParseResourceMetadataFromResourceInfo(t *testing.T) {
	tests := []struct {
		name      string
		info      api.ResourceInfo
		expected  ResourceMetadata
	}{
		{
			name: "namespaced deployment",
			info: api.ResourceInfo{
				ResourceType: "apps/v1/Deployment",
				ResourceName: "default/my-deploy",
			},
			expected: ResourceMetadata{
				Kind: "Deployment", APIVersion: "apps/v1",
				Name: "my-deploy", Namespace: "default",
				Group: "apps", Version: "v1", Resource: "deployments",
			},
		},
		{
			name: "cluster-scoped namespace",
			info: api.ResourceInfo{
				ResourceType: "v1/Namespace",
				ResourceName: "/my-ns",
			},
			expected: ResourceMetadata{
				Kind: "Namespace", APIVersion: "v1",
				Name: "my-ns", Namespace: "",
				Group: "", Version: "v1", Resource: "namespaces",
			},
		},
		{
			name: "namespaced pod empty namespace gets default",
			info: api.ResourceInfo{
				ResourceType: "v1/Pod",
				ResourceName: "/my-pod",
			},
			expected: ResourceMetadata{
				Kind: "Pod", APIVersion: "v1",
				Name: "my-pod", Namespace: "default",
				Group: "", Version: "v1", Resource: "pods",
			},
		},
		{
			name: "cluster-scoped clusterrole",
			info: api.ResourceInfo{
				ResourceType: "rbac.authorization.k8s.io/v1/ClusterRole",
				ResourceName: "/my-role",
			},
			expected: ResourceMetadata{
				Kind: "ClusterRole", APIVersion: "rbac.authorization.k8s.io/v1",
				Name: "my-role", Namespace: "",
				Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := ParseResourceMetadataFromResourceInfo(&tt.info)
			assert.Equal(t, tt.expected, meta)
		})
	}
}

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
			assert.Equal(t, tt.want, MatchesAny(tt.patterns, tt.value))
		})
	}
}

func TestWebhookEndpointMatchesResource(t *testing.T) {
	wh := WebhookEndpoint{
		Name: "test",
		Path: "/test",
		Rules: []WebhookRuleSpec{
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
			assert.Equal(t, tt.want, wh.MatchesResource(tt.group, tt.version, tt.resource, tt.operation))
		})
	}
}

func TestWebhookEndpointMatchesResource_Wildcard(t *testing.T) {
	wh := WebhookEndpoint{
		Name: "catch-all",
		Path: "/validate/all",
		Rules: []WebhookRuleSpec{{
			APIGroups:   []string{"*"},
			APIVersions: []string{"*"},
			Operations:  []string{"*"},
			Resources:   []string{"*"},
		}},
	}

	assert.True(t, wh.MatchesResource("apps", "v1", "deployments", "CREATE"))
	assert.True(t, wh.MatchesResource("", "v1", "pods", "DELETE"))
	assert.True(t, wh.MatchesResource("batch", "v1beta1", "cronjobs", "UPDATE"))
}

func TestParseWebhookEndpoints(t *testing.T) {
	path1 := "/vpol/require-labels"
	path2 := "/validate/fail"

	list := VWCList{
		Items: []VWCItem{
			{
				Metadata: VWCMetadata{Name: "resource-config"},
				Webhooks: []VWCWebhook{
					{
						Name: "vpol.require-labels",
						ClientConfig: VWCClientConfig{
							Service: &VWCServiceRef{Path: &path1},
						},
						Rules: []WebhookRuleSpec{{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Operations:  []string{"CREATE", "UPDATE"},
							Resources:   []string{"pods"},
						}},
					},
					{
						Name: "validate.svc",
						ClientConfig: VWCClientConfig{
							Service: &VWCServiceRef{Path: &path2},
						},
						Rules: []WebhookRuleSpec{{
							APIGroups:   []string{"*"},
							APIVersions: []string{"*"},
							Operations:  []string{"CREATE", "UPDATE"},
							Resources:   []string{"*"},
						}},
					},
				},
			},
			{
				// Non-matching config name.
				Metadata: VWCMetadata{Name: "other-config"},
				Webhooks: []VWCWebhook{
					{
						Name: "other-webhook",
						ClientConfig: VWCClientConfig{
							Service: &VWCServiceRef{Path: &path1},
						},
						Rules: []WebhookRuleSpec{{
							APIGroups:   []string{"*"},
							APIVersions: []string{"*"},
							Operations:  []string{"*"},
							Resources:   []string{"*"},
						}},
					},
				},
			},
		},
	}

	selector := WebhookSelector{ConfigNames: []string{"resource-config"}}
	endpoints := ParseWebhookEndpoints(list, selector)
	require.Len(t, endpoints, 2, "only webhooks from the resource config should be included")
	assert.Equal(t, "vpol.require-labels", endpoints[0].Name)
	assert.Equal(t, "/vpol/require-labels", endpoints[0].Path)
	assert.Len(t, endpoints[0].Rules, 1)
	assert.Equal(t, "validate.svc", endpoints[1].Name)
	assert.Equal(t, "/validate/fail", endpoints[1].Path)
}

func TestParseWebhookEndpoints_Empty(t *testing.T) {
	endpoints := ParseWebhookEndpoints(VWCList{}, WebhookSelector{})
	assert.Empty(t, endpoints)
}

func TestParseWebhookEndpoints_NoFilter(t *testing.T) {
	path := "/validate"
	list := VWCList{
		Items: []VWCItem{
			{
				Metadata: VWCMetadata{Name: "any-config"},
				Webhooks: []VWCWebhook{
					{
						Name: "wh1",
						ClientConfig: VWCClientConfig{
							Service: &VWCServiceRef{Path: &path},
						},
						Rules: []WebhookRuleSpec{{
							APIGroups:   []string{"*"},
							APIVersions: []string{"*"},
							Operations:  []string{"*"},
							Resources:   []string{"*"},
						}},
					},
				},
			},
		},
	}

	// Empty ConfigNames means no filtering.
	endpoints := ParseWebhookEndpoints(list, WebhookSelector{})
	require.Len(t, endpoints, 1)
}

const testDeploymentYAML = `apiVersion: apps/v1
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

// catchAllWebhooks returns a webhook endpoint that matches all resources.
func catchAllWebhooks() []WebhookEndpoint {
	return []WebhookEndpoint{{
		Name: "catch-all",
		Path: "/validate/test",
		Rules: []WebhookRuleSpec{{
			APIGroups:   []string{"*"},
			APIVersions: []string{"*"},
			Operations:  []string{"*"},
			Resources:   []string{"*"},
		}},
	}}
}

// simpleConverter returns a basic ResponseConverter for testing.
func simpleConverter(resp *AdmissionResponse, resourceInfo api.ResourceInfo) ([]string, []api.AttributeValue) {
	var details []string
	var failedAttrs []api.AttributeValue

	if resp.Status != nil && resp.Status.Message != "" {
		details = append(details, resp.Status.Message)
		failedAttrs = append(failedAttrs, api.AttributeValue{
			AttributeInfo: api.AttributeInfo{
				AttributeIdentifier: api.AttributeIdentifier{
					ResourceInfo: resourceInfo,
				},
				AttributeMetadata: api.AttributeMetadata{
					AttributeName: api.AttributeNameNone,
				},
			},
			Issues: []api.Issue{
				{
					Identifier: "test-policy",
					Message:    resp.Status.Message,
				},
			},
		})
	}
	return details, failedAttrs
}

func newMockWebhookServer(t *testing.T, handler func(path string, req AdmissionReview) AdmissionReview) (*httptest.Server, *WebhookClient) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req AdmissionReview
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		resp := handler(r.URL.Path, req)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))

	client := NewWebhookClientForTesting(server.Client(), server.URL)
	return server, client
}

func TestValidateResources_AllAllowed(t *testing.T) {
	server, client := newMockWebhookServer(t, func(_ string, req AdmissionReview) AdmissionReview {
		return AdmissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &AdmissionResponse{
				UID:     req.Request.UID,
				Allowed: true,
			},
		}
	})
	defer server.Close()

	parsedData, err := gaby.ParseAll([]byte(testDeploymentYAML))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := ValidateResources(client, catchAllWebhooks(), rp, parsedData, simpleConverter)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok, "expected ValidationResult, got %T", output)
	assert.True(t, vr.Passed, "expected validation to pass")
}

func TestValidateResources_Denied(t *testing.T) {
	server, client := newMockWebhookServer(t, func(_ string, req AdmissionReview) AdmissionReview {
		return AdmissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &AdmissionResponse{
				UID:     req.Request.UID,
				Allowed: false,
				Status: &Status{
					Status:  "Failure",
					Message: "policy violation detected",
				},
			},
		}
	})
	defer server.Close()

	parsedData, err := gaby.ParseAll([]byte(testDeploymentYAML))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := ValidateResources(client, catchAllWebhooks(), rp, parsedData, simpleConverter)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed)
	assert.NotEmpty(t, vr.Details)
	assert.NotEmpty(t, vr.FailedAttributes)
}

func TestValidateResources_WithWarnings(t *testing.T) {
	server, client := newMockWebhookServer(t, func(_ string, req AdmissionReview) AdmissionReview {
		return AdmissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &AdmissionResponse{
				UID:      req.Request.UID,
				Allowed:  true,
				Warnings: []string{"audit warning: missing annotation"},
			},
		}
	})
	defer server.Close()

	parsedData, err := gaby.ParseAll([]byte(testDeploymentYAML))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := ValidateResources(client, catchAllWebhooks(), rp, parsedData, simpleConverter)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed)
	assert.NotEmpty(t, vr.Details)
	assert.Contains(t, vr.Details[0], "warning:")
}

func TestValidateResources_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewWebhookClientForTesting(server.Client(), server.URL)

	parsedData, err := gaby.ParseAll([]byte(testDeploymentYAML))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, _, err = ValidateResources(client, catchAllWebhooks(), rp, parsedData, simpleConverter)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
}

func TestValidateResources_AdmissionReviewFormat(t *testing.T) {
	var capturedReq AdmissionReview
	server, client := newMockWebhookServer(t, func(_ string, req AdmissionReview) AdmissionReview {
		capturedReq = req
		return AdmissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &AdmissionResponse{
				UID:     req.Request.UID,
				Allowed: true,
			},
		}
	})
	defer server.Close()

	parsedData, err := gaby.ParseAll([]byte(testDeploymentYAML))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, _, err = ValidateResources(client, catchAllWebhooks(), rp, parsedData, simpleConverter)
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

func TestValidateResources_NoMatchingWebhooks(t *testing.T) {
	callCount := 0
	server, client := newMockWebhookServer(t, func(_ string, req AdmissionReview) AdmissionReview {
		callCount++
		return AdmissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &AdmissionResponse{
				UID:     req.Request.UID,
				Allowed: true,
			},
		}
	})
	defer server.Close()

	// Webhook only matches pods, not deployments.
	webhooks := []WebhookEndpoint{{
		Name: "pod-only",
		Path: "/vpol/pod-policy",
		Rules: []WebhookRuleSpec{{
			APIGroups:   []string{""},
			APIVersions: []string{"v1"},
			Operations:  []string{"CREATE"},
			Resources:   []string{"pods"},
		}},
	}}

	parsedData, err := gaby.ParseAll([]byte(testDeploymentYAML))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := ValidateResources(client, webhooks, rp, parsedData, simpleConverter)
	require.NoError(t, err)

	// With no matching webhooks, result is "true" (shorthand).
	assert.Equal(t, api.ValidationResultTrue, output)
	assert.Equal(t, 0, callCount)
}

func TestValidateResources_MultipleWebhooks(t *testing.T) {
	var calledPaths []string
	server, client := newMockWebhookServer(t, func(path string, req AdmissionReview) AdmissionReview {
		calledPaths = append(calledPaths, path)
		allowed := true
		var st *Status
		if path == "/vpol/deny" {
			allowed = false
			st = &Status{Status: "Failure", Message: "denied by policy"}
		}
		return AdmissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &AdmissionResponse{
				UID:     req.Request.UID,
				Allowed: allowed,
				Status:  st,
			},
		}
	})
	defer server.Close()

	webhooks := []WebhookEndpoint{
		{
			Name: "allow-all",
			Path: "/vpol/allow",
			Rules: []WebhookRuleSpec{{
				APIGroups: []string{"*"}, APIVersions: []string{"*"},
				Operations: []string{"*"}, Resources: []string{"*"},
			}},
		},
		{
			Name: "deny-all",
			Path: "/vpol/deny",
			Rules: []WebhookRuleSpec{{
				APIGroups: []string{"*"}, APIVersions: []string{"*"},
				Operations: []string{"*"}, Resources: []string{"*"},
			}},
		},
	}

	parsedData, err := gaby.ParseAll([]byte(testDeploymentYAML))
	require.NoError(t, err)

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := ValidateResources(client, webhooks, rp, parsedData, simpleConverter)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed)
	assert.Len(t, calledPaths, 2)
	assert.Equal(t, "/vpol/allow", calledPaths[0])
	assert.Equal(t, "/vpol/deny", calledPaths[1])
}

func TestValidateResources_MultiDocMixed(t *testing.T) {
	multiDoc := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: deploy-one
  namespace: default
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
	callCount := 0
	server, client := newMockWebhookServer(t, func(_ string, req AdmissionReview) AdmissionReview {
		callCount++
		allowed := true
		var st *Status
		if callCount == 2 {
			allowed = false
			st = &Status{Status: "Failure", Message: fmt.Sprintf("denied: %s", req.Request.Name)}
		}
		return AdmissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Response: &AdmissionResponse{
				UID:     req.Request.UID,
				Allowed: allowed,
				Status:  st,
			},
		}
	})
	defer server.Close()

	parsedData, err := gaby.ParseAll([]byte(multiDoc))
	require.NoError(t, err)
	require.Len(t, parsedData, 2)

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := ValidateResources(client, catchAllWebhooks(), rp, parsedData, simpleConverter)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed)
	assert.Equal(t, 2, callCount)
}
