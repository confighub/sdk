// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kyvernoserver

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/third_party/gaby"
)

// Environment variable names for Kyverno server configuration.
const (
	envKyvernoURL           = "KYVERNO_URL"
	envKyvernoCACertPath    = "KYVERNO_CA_CERT_PATH"
	envKyvernoSkipTLSVerify = "KYVERNO_SKIP_TLS_VERIFY"
)

// kyvernoClient holds the HTTP client and URL for the Kyverno webhook.
type kyvernoClient struct {
	httpClient *http.Client
	baseURL    string
}

func newKyvernoClient() (*kyvernoClient, error) {
	baseURL := os.Getenv(envKyvernoURL)
	if baseURL == "" {
		return nil, fmt.Errorf("%s environment variable is required", envKyvernoURL)
	}
	baseURL = strings.TrimRight(baseURL, "/")

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}

	if os.Getenv(envKyvernoSkipTLSVerify) == "true" {
		tlsConfig.InsecureSkipVerify = true //nolint:gosec // user-controlled for dev/testing
	}

	if caPath := os.Getenv(envKyvernoCACertPath); caPath != "" {
		caCert, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert from %s: %w", caPath, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA cert from %s", caPath)
		}
		tlsConfig.RootCAs = pool
	}

	return &kyvernoClient{
		httpClient: &http.Client{
			Transport: &http.Transport{TLSClientConfig: tlsConfig},
		},
		baseURL: baseURL,
	}, nil
}

// callWebhook sends an AdmissionReview to a specific webhook path and returns the response.
func (c *kyvernoClient) callWebhook(path string, reviewJSON []byte) (*admissionResponse, error) {
	resp, err := c.httpClient.Post(c.baseURL+path, "application/json", bytes.NewReader(reviewJSON))
	if err != nil {
		return nil, errors.Wrap(err, "failed to send request to Kyverno server")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read Kyverno response")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Newf("Kyverno returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var reviewResp admissionReview
	if err := json.Unmarshal(body, &reviewResp); err != nil {
		return nil, errors.Wrapf(err, "failed to parse Kyverno response: %s", string(body))
	}

	if reviewResp.Response == nil {
		return nil, errors.Newf("Kyverno returned empty response")
	}

	return reviewResp.Response, nil
}

// webhookEndpoint represents a discovered Kyverno webhook with its path and matching rules.
type webhookEndpoint struct {
	name  string
	path  string
	rules []webhookRuleSpec
}

// webhookRuleSpec defines which resources a webhook handles.
type webhookRuleSpec struct {
	APIGroups   []string `json:"apiGroups"`
	APIVersions []string `json:"apiVersions"`
	Operations  []string `json:"operations"`
	Resources   []string `json:"resources"`
}

// matchesResource checks if this webhook should be called for the given resource.
func (e *webhookEndpoint) matchesResource(group, version, resource, operation string) bool {
	for _, rule := range e.rules {
		if matchesAny(rule.APIGroups, group) &&
			matchesAny(rule.APIVersions, version) &&
			matchesAny(rule.Resources, resource) &&
			matchesAny(rule.Operations, operation) {
			return true
		}
	}
	return false
}

// matchesAny returns true if value matches any pattern, where "*" is a wildcard.
func matchesAny(patterns []string, value string) bool {
	for _, p := range patterns {
		if p == "*" || p == value {
			return true
		}
	}
	return false
}

// Kubernetes API types for ValidatingWebhookConfiguration discovery.

type vwcList struct {
	Items []vwcItem `json:"items"`
}

type vwcItem struct {
	Metadata vwcMetadata  `json:"metadata"`
	Webhooks []vwcWebhook `json:"webhooks"`
}

type vwcMetadata struct {
	Name string `json:"name"`
}

type vwcWebhook struct {
	Name         string            `json:"name"`
	ClientConfig vwcClientConfig   `json:"clientConfig"`
	Rules        []webhookRuleSpec `json:"rules"`
}

type vwcClientConfig struct {
	Service *vwcServiceRef `json:"service"`
}

type vwcServiceRef struct {
	Path *string `json:"path"`
}

// discoverKyvernoWebhooks queries the Kubernetes API to find all Kyverno-managed
// ValidatingWebhookConfigurations and returns their webhook endpoints.
func discoverKyvernoWebhooks() ([]webhookEndpoint, error) {
	tokenBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return nil, errors.Wrap(err, "failed to read service account token (not running in-cluster?)")
	}
	token := strings.TrimSpace(string(tokenBytes))

	caCertBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
	if err != nil {
		return nil, errors.Wrap(err, "failed to read cluster CA cert")
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCertBytes) {
		return nil, errors.New("failed to parse cluster CA cert")
	}

	k8sClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				MinVersion: tls.VersionTLS12,
			},
		},
	}

	apiHost := os.Getenv("KUBERNETES_SERVICE_HOST")
	apiPort := os.Getenv("KUBERNETES_SERVICE_PORT")
	if apiHost == "" || apiPort == "" {
		return nil, errors.New("KUBERNETES_SERVICE_HOST/PORT not set (not running in-cluster?)")
	}

	url := fmt.Sprintf(
		"https://%s:%s/apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations?labelSelector=%s",
		apiHost, apiPort, "webhook.kyverno.io/managed-by%3Dkyverno",
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := k8sClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list ValidatingWebhookConfigurations")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read Kubernetes API response")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Newf("Kubernetes API returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var list vwcList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, errors.Wrapf(err, "failed to parse ValidatingWebhookConfigurations response")
	}

	return parseWebhookEndpoints(list), nil
}

// kyvernoResourceWebhookConfigName is the standard name of the Kyverno
// ValidatingWebhookConfiguration that handles resource validation.
// Other configurations (cleanup, policy CRDs, exceptions) are served by
// different controllers and are not relevant for validating user resources.
const kyvernoResourceWebhookConfigName = "kyverno-resource-validating-webhook-cfg"

// parseWebhookEndpoints extracts webhook endpoints from a ValidatingWebhookConfiguration list.
// Only endpoints from the resource validation configuration are included.
func parseWebhookEndpoints(list vwcList) []webhookEndpoint {
	var endpoints []webhookEndpoint
	for _, item := range list.Items {
		if item.Metadata.Name != kyvernoResourceWebhookConfigName {
			continue
		}
		for _, wh := range item.Webhooks {
			if wh.ClientConfig.Service == nil || wh.ClientConfig.Service.Path == nil {
				continue
			}
			path := *wh.ClientConfig.Service.Path
			if path == "" {
				continue
			}
			endpoints = append(endpoints, webhookEndpoint{
				name:  wh.Name,
				path:  path,
				rules: wh.Rules,
			})
		}
	}
	return endpoints
}

// GetVetKyvernoServerSignature returns the function signature for vet-kyverno-server.
func GetVetKyvernoServerSignature() api.FunctionSignature {
	return api.FunctionSignature{
		FunctionName:       "vet-kyverno-server",
		Parameters:         []api.FunctionParameter{},
		RequiredParameters: 0,
		OutputInfo: &api.FunctionOutput{
			ResultName:  "passed",
			Description: "True if all resources pass Kyverno policy validation, false otherwise",
			OutputType:  api.OutputTypeValidationResult,
		},
		Mutating:              false,
		Validating:            true,
		Hermetic:              false,
		Idempotent:            true,
		Description:           "Validates Kubernetes resources against Kyverno policies by sending AdmissionReview requests to a running Kyverno server. Policies must be deployed in the Kyverno cluster.",
		FunctionType:          api.FunctionTypeCustom,
		AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
	}
}

// VetKyvernoServerFunction validates Kubernetes resources against a running Kyverno server.
func VetKyvernoServerFunction(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
	client, err := newKyvernoClient()
	if err != nil {
		return fArgs.ParsedData, nil, err
	}

	webhooks, err := discoverKyvernoWebhooks()
	if err != nil {
		return fArgs.ParsedData, nil, errors.Wrap(err, "failed to discover Kyverno webhooks")
	}

	slog.Info("discovered Kyverno webhooks", "count", len(webhooks))
	for _, wh := range webhooks {
		slog.Info("webhook", "name", wh.name, "path", wh.path, "rules", len(wh.rules))
	}

	return vetKyvernoServer(client, webhooks, k8skit.NewK8sResourceProvider(), fArgs.ParsedData)
}

func vetKyvernoServer(client *kyvernoClient, webhooks []webhookEndpoint, rp *k8skit.K8sResourceProviderType, parsedData gaby.Container) (gaby.Container, any, error) {
	resourceInfoMap := buildResourceInfoMap(parsedData, rp)

	var allDetails []string
	var allFailedAttributes api.AttributeValueList
	passed := true

	for _, doc := range parsedData {
		// Convert YAML to JSON for the AdmissionReview object payload.
		jsonBytes, err := doc.MarshalJSON()
		if err != nil {
			return parsedData, nil, errors.Wrap(err, "failed to convert YAML to JSON")
		}

		// Extract resource metadata.
		kind, _ := doc.Path("kind").Data().(string)
		apiVersion, _ := doc.Path("apiVersion").Data().(string)
		name, _ := doc.Path("metadata.name").Data().(string)
		ns, _ := doc.Path("metadata.namespace").Data().(string)
		if ns == "" {
			ns = "default"
		}

		group, version := parseAPIVersion(apiVersion)
		resource := strings.ToLower(kind) + "s"

		// Find all webhooks that match this resource.
		var matchingWebhooks []webhookEndpoint
		for _, wh := range webhooks {
			if wh.matchesResource(group, version, resource, "CREATE") {
				matchingWebhooks = append(matchingWebhooks, wh)
			}
		}

		if len(matchingWebhooks) == 0 {
			continue // No policies apply to this resource.
		}

		// Build the AdmissionReview request once; send to each matching webhook.
		gvk := groupVersionKind{Group: group, Version: version, Kind: kind}
		gvr := groupVersionResource{Group: group, Version: version, Resource: resource}
		dryRun := true
		review := admissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Request: &admissionRequest{
				UID:             newUID(),
				Kind:            gvk,
				Resource:        gvr,
				RequestKind:     &gvk,
				RequestResource: &gvr,
				Namespace:       ns,
				Name:            name,
				Operation:       "CREATE",
				Object:          jsonBytes,
				DryRun:          &dryRun,
				UserInfo:        userInfo{Username: "confighub"},
			},
		}

		reviewJSON, err := json.Marshal(review)
		if err != nil {
			return parsedData, nil, errors.Wrap(err, "failed to marshal AdmissionReview")
		}

		for _, wh := range matchingWebhooks {
			admResp, err := client.callWebhook(wh.path, reviewJSON)
			if err != nil {
				return parsedData, nil, errors.Wrapf(err, "validation request to webhook %q failed for %s/%s", wh.name, kind, name)
			}

			// Process the response.
			if !admResp.Allowed {
				passed = false
				resourceKey := fmt.Sprintf("%s/%s/%s", ns, kind, name)
				resourceInfo := lookupResourceInfo(resourceInfoMap, resourceKey)

				if admResp.Status != nil && admResp.Status.Message != "" {
					failures := parseKyvernoMessage(admResp.Status.Message)
					for _, f := range failures {
						detail := fmt.Sprintf("policy %q rule %q: %s", f.policyName, f.ruleName, f.message)
						allDetails = append(allDetails, detail)
						allFailedAttributes = append(allFailedAttributes, api.AttributeValue{
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
									Identifier: f.policyName + "/" + f.ruleName,
									Message:    f.message,
								},
							},
						})
					}
					if len(failures) == 0 {
						// Couldn't parse structured failures, use raw message.
						allDetails = append(allDetails, admResp.Status.Message)
					}
				}
			}

			// Collect warnings even for allowed resources.
			for _, w := range admResp.Warnings {
				allDetails = append(allDetails, "warning: "+w)
			}
		}
	}

	if passed && len(allDetails) == 0 {
		return parsedData, api.ValidationResultTrue, nil
	}

	result := api.ValidationResult{
		Passed:           passed,
		Details:          allDetails,
		FailedAttributes: allFailedAttributes,
	}

	return parsedData, result, nil
}

// AdmissionReview types — minimal local definitions to avoid pulling in k8s.io/api.

type admissionReview struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Request    *admissionRequest  `json:"request,omitempty"`
	Response   *admissionResponse `json:"response,omitempty"`
}

type admissionRequest struct {
	UID             string                `json:"uid"`
	Kind            groupVersionKind      `json:"kind"`
	Resource        groupVersionResource  `json:"resource"`
	RequestKind     *groupVersionKind     `json:"requestKind,omitempty"`
	RequestResource *groupVersionResource `json:"requestResource,omitempty"`
	Namespace       string                `json:"namespace"`
	Name            string                `json:"name"`
	Operation       string                `json:"operation"`
	Object          json.RawMessage       `json:"object"`
	DryRun          *bool                 `json:"dryRun,omitempty"`
	UserInfo        userInfo              `json:"userInfo"`
}

type admissionResponse struct {
	UID      string   `json:"uid"`
	Allowed  bool     `json:"allowed"`
	Status   *status  `json:"status,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

type groupVersionKind struct {
	Group   string `json:"group"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

type groupVersionResource struct {
	Group    string `json:"group"`
	Version  string `json:"version"`
	Resource string `json:"resource"`
}

type status struct {
	Message string `json:"message"`
	Status  string `json:"status"`
}

type userInfo struct {
	Username string `json:"username"`
}

// parseAPIVersion splits "apps/v1" into ("apps", "v1") or "v1" into ("", "v1").
func parseAPIVersion(apiVersion string) (group, version string) {
	parts := strings.SplitN(apiVersion, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", parts[0]
}

// newUID generates a random UID for AdmissionReview requests.
func newUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// kyvernoFailure represents a parsed failure from a Kyverno response message.
type kyvernoFailure struct {
	policyName string
	ruleName   string
	message    string
}

// parseKyvernoMessage parses validation failure messages from Kyverno.
// It handles both the ClusterPolicy format:
//
//	resource Pod/default/test-pod was blocked due to the following policies
//
//	policy-name:
//	  rule-name: message text
//
// and the ValidatingPolicy format:
//
//	Policy policy-name failed: message text
var (
	policyHeaderRE      = regexp.MustCompile(`^(\S+):$`)
	ruleMessageRE       = regexp.MustCompile(`^\s+(\S+): (.+)$`)
	validatingPolicyRE  = regexp.MustCompile(`^Policy (\S+) failed: (.+)$`)
)

func parseKyvernoMessage(msg string) []kyvernoFailure {
	var failures []kyvernoFailure
	var currentPolicy string

	for _, line := range strings.Split(msg, "\n") {
		trimmed := strings.TrimSpace(line)

		// Try ValidatingPolicy format: "Policy <name> failed: <message>"
		if m := validatingPolicyRE.FindStringSubmatch(trimmed); m != nil {
			failures = append(failures, kyvernoFailure{
				policyName: m[1],
				ruleName:   "validation",
				message:    m[2],
			})
			continue
		}

		// Try ClusterPolicy format.
		if m := policyHeaderRE.FindStringSubmatch(line); m != nil {
			currentPolicy = m[1]
			continue
		}
		if m := ruleMessageRE.FindStringSubmatch(line); m != nil && currentPolicy != "" {
			failures = append(failures, kyvernoFailure{
				policyName: currentPolicy,
				ruleName:   m[1],
				message:    m[2],
			})
		}
	}
	return failures
}

// buildResourceInfoMap creates a map from "namespace/Kind/name" to ResourceInfo
// by visiting all resources in the parsed data.
func buildResourceInfoMap(parsedData gaby.Container, rp *k8skit.K8sResourceProviderType) map[string]api.ResourceInfo {
	infoMap := make(map[string]api.ResourceInfo)
	visitor := func(doc *gaby.YamlDoc, output any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
		kind := doc.Path("kind").Data()
		name := doc.Path("metadata.name").Data()
		ns := doc.Path("metadata.namespace").Data()
		nsStr := "default"
		if s, ok := ns.(string); ok && s != "" {
			nsStr = s
		}
		kindStr, _ := kind.(string)
		nameStr, _ := name.(string)
		key := fmt.Sprintf("%s/%s/%s", nsStr, kindStr, nameStr)
		infoMap[key] = *resourceInfo
		return output, nil
	}
	yamlkit.VisitResources(parsedData, nil, rp, visitor)
	return infoMap
}

// lookupResourceInfo finds a ResourceInfo by the "ns/Kind/name" key.
func lookupResourceInfo(infoMap map[string]api.ResourceInfo, key string) api.ResourceInfo {
	if info, ok := infoMap[key]; ok {
		return info
	}
	return api.ResourceInfo{}
}
