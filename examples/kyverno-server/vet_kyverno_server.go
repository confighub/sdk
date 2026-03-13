// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
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
	envKyvernoValidatePath  = "KYVERNO_VALIDATE_PATH"

	defaultValidatePath = "/validate/fail"
)

// kyvernoClient holds the HTTP client and URL for the Kyverno webhook.
type kyvernoClient struct {
	httpClient   *http.Client
	baseURL      string
	validatePath string
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

	validatePath := os.Getenv(envKyvernoValidatePath)
	if validatePath == "" {
		validatePath = defaultValidatePath
	}

	return &kyvernoClient{
		httpClient: &http.Client{
			Transport: &http.Transport{TLSClientConfig: tlsConfig},
		},
		baseURL:      baseURL,
		validatePath: validatePath,
	}, nil
}

// validate sends an AdmissionReview to the Kyverno webhook and returns the response.
func (c *kyvernoClient) validate(reviewJSON []byte) (*admissionResponse, error) {
	resp, err := c.httpClient.Post(c.baseURL+c.validatePath, "application/json", bytes.NewReader(reviewJSON))
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
	return vetKyvernoServer(client, k8skit.NewK8sResourceProvider(), fArgs.ParsedData)
}

func vetKyvernoServer(client *kyvernoClient, rp *k8skit.K8sResourceProviderType, parsedData gaby.Container) (gaby.Container, any, error) {
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

		// Build and send AdmissionReview request.
		// RequestKind and RequestResource mirror Kind and Resource — Kyverno expects them.
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

		admResp, err := client.validate(reviewJSON)
		if err != nil {
			return parsedData, nil, errors.Wrapf(err, "validation request failed for %s/%s", kind, name)
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
								Details: &api.AttributeDetails{
									Description: detail,
								},
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
	UID             string               `json:"uid"`
	Kind            groupVersionKind     `json:"kind"`
	Resource        groupVersionResource `json:"resource"`
	RequestKind     *groupVersionKind    `json:"requestKind,omitempty"`
	RequestResource *groupVersionResource `json:"requestResource,omitempty"`
	Namespace       string               `json:"namespace"`
	Name            string               `json:"name"`
	Operation       string               `json:"operation"`
	Object          json.RawMessage      `json:"object"`
	DryRun          *bool                `json:"dryRun,omitempty"`
	UserInfo        userInfo             `json:"userInfo"`
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

// parseKyvernoMessage parses the Kyverno validation failure message.
// The message format from Kyverno's block.go is:
//
//	resource Pod/default/test-pod was blocked due to the following policies
//
//	policy-name:
//	  rule-name: message text
var (
	policyHeaderRE = regexp.MustCompile(`^(\S+):$`)
	ruleMessageRE  = regexp.MustCompile(`^\s+(\S+): (.+)$`)
)

func parseKyvernoMessage(msg string) []kyvernoFailure {
	var failures []kyvernoFailure
	var currentPolicy string

	for _, line := range strings.Split(msg, "\n") {
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
