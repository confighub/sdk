// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package admissionwebhook

// WebhookEndpoint represents a discovered webhook with its path and matching rules.
type WebhookEndpoint struct {
	Name  string
	Path  string
	Rules []WebhookRuleSpec
}

// WebhookRuleSpec defines which resources a webhook handles.
type WebhookRuleSpec struct {
	APIGroups   []string `json:"apiGroups"`
	APIVersions []string `json:"apiVersions"`
	Operations  []string `json:"operations"`
	Resources   []string `json:"resources"`
}

// MatchesResource checks if this webhook should be called for the given resource.
func (e *WebhookEndpoint) MatchesResource(group, version, resource, operation string) bool {
	for _, rule := range e.Rules {
		if MatchesAny(rule.APIGroups, group) &&
			MatchesAny(rule.APIVersions, version) &&
			MatchesAny(rule.Resources, resource) &&
			MatchesAny(rule.Operations, operation) {
			return true
		}
	}
	return false
}

// MatchesAny returns true if value matches any pattern, where "*" is a wildcard.
func MatchesAny(patterns []string, value string) bool {
	for _, p := range patterns {
		if p == "*" || p == value {
			return true
		}
	}
	return false
}

// Kubernetes API types for ValidatingWebhookConfiguration discovery.

// VWCList is a list of ValidatingWebhookConfigurations.
type VWCList struct {
	Items []VWCItem `json:"items"`
}

// VWCItem represents a ValidatingWebhookConfiguration.
type VWCItem struct {
	Metadata VWCMetadata  `json:"metadata"`
	Webhooks []VWCWebhook `json:"webhooks"`
}

// VWCMetadata contains metadata for a VWC.
type VWCMetadata struct {
	Name string `json:"name"`
}

// VWCWebhook represents a single webhook within a VWC.
type VWCWebhook struct {
	Name         string          `json:"name"`
	ClientConfig VWCClientConfig `json:"clientConfig"`
	Rules        []WebhookRuleSpec `json:"rules"`
}

// VWCClientConfig contains the client configuration for a webhook.
type VWCClientConfig struct {
	Service *VWCServiceRef `json:"service"`
}

// VWCServiceRef references a service for a webhook.
type VWCServiceRef struct {
	Path *string `json:"path"`
}

// WebhookSelector defines criteria for filtering ValidatingWebhookConfigurations.
type WebhookSelector struct {
	LabelSelector string
	ConfigNames   []string
}

// ParseWebhookEndpoints extracts webhook endpoints from a VWC list,
// filtering by the selector's ConfigNames.
func ParseWebhookEndpoints(list VWCList, selector WebhookSelector) []WebhookEndpoint {
	configNameSet := make(map[string]struct{}, len(selector.ConfigNames))
	for _, name := range selector.ConfigNames {
		configNameSet[name] = struct{}{}
	}

	var endpoints []WebhookEndpoint
	for _, item := range list.Items {
		if len(configNameSet) > 0 {
			if _, ok := configNameSet[item.Metadata.Name]; !ok {
				continue
			}
		}
		for _, wh := range item.Webhooks {
			if wh.ClientConfig.Service == nil || wh.ClientConfig.Service.Path == nil {
				continue
			}
			path := *wh.ClientConfig.Service.Path
			if path == "" {
				continue
			}
			endpoints = append(endpoints, WebhookEndpoint{
				Name:  wh.Name,
				Path:  path,
				Rules: wh.Rules,
			})
		}
	}
	return endpoints
}
