// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package admissionwebhook

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
)

// ResponseConverter converts an AdmissionResponse for a resource into
// validation details and failed attributes.
type ResponseConverter func(resp *AdmissionResponse, resourceInfo api.ResourceInfo) (details []string, failedAttrs []api.AttributeValue)

// ResourceMetadata holds parsed K8s metadata for building AdmissionReview.
type ResourceMetadata struct {
	Kind       string
	APIVersion string
	Name       string
	Namespace  string
	Group      string
	Version    string
	Resource   string
}

// ParseResourceMetadataFromResourceInfo derives metadata from ResourceInfo.
// ResourceType="apps/v1/Deployment" -> apiVersion="apps/v1", kind="Deployment"
// ResourceName="default/my-deploy" -> namespace="default", name="my-deploy"
// Cluster-scoped resources have namespace="".
func ParseResourceMetadataFromResourceInfo(info *api.ResourceInfo) ResourceMetadata {
	apiVersion, kind := ParseResourceType(info.ResourceType)
	group, version := ParseAPIVersion(apiVersion)
	resource := strings.ToLower(kind) + "s"

	// Parse namespace and name from ResourceName ("namespace/name" or "/name" for cluster-scoped).
	var namespace, name string
	rn := string(info.ResourceName)
	if ns, n, ok := strings.Cut(rn, "/"); ok {
		namespace = ns
		name = n
	} else {
		name = rn
	}

	// Only apply default namespace for namespaced resources.
	if namespace == "" && !k8skit.IsClusterScoped(apiVersion, kind) {
		namespace = "default"
	}

	return ResourceMetadata{
		Kind:       kind,
		APIVersion: apiVersion,
		Name:       name,
		Namespace:  namespace,
		Group:      group,
		Version:    version,
		Resource:   resource,
	}
}

// ParseResourceType splits "apps/v1/Deployment" -> ("apps/v1", "Deployment").
// For core types: "v1/Pod" -> ("v1", "Pod").
func ParseResourceType(rt api.ResourceType) (apiVersion, kind string) {
	s := string(rt)
	// Find the last "/" — everything after is the kind, everything before is the apiVersion.
	idx := strings.LastIndex(s, "/")
	if idx < 0 {
		return "", s
	}
	return s[:idx], s[idx+1:]
}

// ParseAPIVersion splits "apps/v1" into ("apps", "v1") or "v1" into ("", "v1").
func ParseAPIVersion(apiVersion string) (group, version string) {
	parts := strings.SplitN(apiVersion, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", parts[0]
}

// ValidateResources uses VisitResources to iterate over parsed K8s resources,
// match against webhooks, call them, and aggregate into a ValidationResult.
func ValidateResources(
	client *WebhookClient,
	webhooks []WebhookEndpoint,
	rp yamlkit.ResourceProvider,
	parsedData gaby.Container,
	converter ResponseConverter,
) (gaby.Container, any, error) {
	var allDetails []string
	var allFailedAttributes api.AttributeValueList
	passed := true

	type validationState struct {
		details          []string
		failedAttributes api.AttributeValueList
		passed           bool
	}

	state := &validationState{passed: true}

	visitor := func(doc *gaby.YamlDoc, output any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
		meta := ParseResourceMetadataFromResourceInfo(resourceInfo)

		// Convert YAML to JSON for the AdmissionReview object payload.
		jsonBytes, err := doc.MarshalJSON()
		if err != nil {
			return output, []error{errors.Wrap(err, "failed to convert YAML to JSON")}
		}

		// Find all webhooks that match this resource.
		var matchingWebhooks []WebhookEndpoint
		for _, wh := range webhooks {
			if wh.MatchesResource(meta.Group, meta.Version, meta.Resource, "CREATE") {
				matchingWebhooks = append(matchingWebhooks, wh)
			}
		}

		if len(matchingWebhooks) == 0 {
			return output, nil // No policies apply to this resource.
		}

		// Build the AdmissionReview request once; send to each matching webhook.
		gvk := GroupVersionKind{Group: meta.Group, Version: meta.Version, Kind: meta.Kind}
		gvr := GroupVersionResource{Group: meta.Group, Version: meta.Version, Resource: meta.Resource}
		dryRun := true
		review := AdmissionReview{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
			Request: &AdmissionRequest{
				UID:             NewUID(),
				Kind:            gvk,
				Resource:        gvr,
				RequestKind:     &gvk,
				RequestResource: &gvr,
				Namespace:       meta.Namespace,
				Name:            meta.Name,
				Operation:       "CREATE",
				Object:          jsonBytes,
				DryRun:          &dryRun,
				UserInfo:        UserInfo{Username: "confighub"},
			},
		}

		reviewJSON, err := json.Marshal(review)
		if err != nil {
			return output, []error{errors.Wrap(err, "failed to marshal AdmissionReview")}
		}

		for _, wh := range matchingWebhooks {
			admResp, err := client.CallWebhook(wh.Path, reviewJSON)
			if err != nil {
				return output, []error{errors.Wrapf(err, "validation request to webhook %q failed for %s/%s", wh.Name, meta.Kind, meta.Name)}
			}

			// Process the response using the converter.
			if !admResp.Allowed {
				state.passed = false
				details, failedAttrs := converter(admResp, *resourceInfo)
				state.details = append(state.details, details...)
				state.failedAttributes = append(state.failedAttributes, failedAttrs...)
			}

			// Collect warnings even for allowed resources.
			for _, w := range admResp.Warnings {
				state.details = append(state.details, "warning: "+w)
			}
		}

		return output, nil
	}

	_, err := yamlkit.VisitResources(parsedData, nil, rp, visitor)
	if err != nil {
		return parsedData, nil, err
	}

	passed = state.passed
	allDetails = state.details
	allFailedAttributes = state.failedAttributes

	if passed && len(allDetails) == 0 {
		return parsedData, api.ValidationResultTrue, nil
	}

	result := api.ValidationResult{
		Passed:           passed,
		Details:          allDetails,
		FailedAttributes: allFailedAttributes,
	}

	slog.Info("validation complete", "passed", passed, "details", len(allDetails), "failedAttributes", len(allFailedAttributes))

	return parsedData, result, nil
}

// FormatResourceKey returns a human-readable key for error messages.
func FormatResourceKey(meta ResourceMetadata) string {
	if meta.Namespace != "" {
		return fmt.Sprintf("%s/%s/%s", meta.Namespace, meta.Kind, meta.Name)
	}
	return fmt.Sprintf("%s/%s", meta.Kind, meta.Name)
}
