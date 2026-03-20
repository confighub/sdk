// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kyvernoserver

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
	admissionwebhook "github.com/confighub/sdk/worker-function-impl/k8s-admission-webhook"
)

// Environment variable names for Kyverno server configuration.
const (
	envKyvernoURL           = "KYVERNO_URL"
	envKyvernoCACertPath    = "KYVERNO_CA_CERT_PATH"
	envKyvernoSkipTLSVerify = "KYVERNO_SKIP_TLS_VERIFY"
)

// kyvernoResourceWebhookConfigName is the standard name of the Kyverno
// ValidatingWebhookConfiguration that handles resource validation.
// Other configurations (cleanup, policy CRDs, exceptions) are served by
// different controllers and are not relevant for validating user resources.
const kyvernoResourceWebhookConfigName = "kyverno-resource-validating-webhook-cfg"

// kyvernoLabelSelector is the label selector for Kyverno-managed VWCs.
const kyvernoLabelSelector = "webhook.kyverno.io/managed-by%3Dkyverno"

// Package-level state initialized once by InitKyvernoServer.
var (
	kyvernoWebhookClient *admissionwebhook.WebhookClient
	kyvernoWatcher       *admissionwebhook.WebhookWatcher
)

// InitKyvernoServer initializes the webhook client and starts watching for
// Kyverno webhook configuration changes. It should be called once at startup.
func InitKyvernoServer() error {
	client, err := admissionwebhook.NewWebhookClient(envKyvernoURL, envKyvernoCACertPath, envKyvernoSkipTLSVerify)
	if err != nil {
		return err
	}

	discoveryClient, err := admissionwebhook.NewK8sDiscoveryClient()
	if err != nil {
		return errors.Wrap(err, "failed to create K8s discovery client")
	}

	selector := admissionwebhook.WebhookSelector{
		LabelSelector: kyvernoLabelSelector,
		ConfigNames:   []string{kyvernoResourceWebhookConfigName},
	}

	watcher := admissionwebhook.NewWebhookWatcher(discoveryClient, selector)
	if err := watcher.Start(context.Background()); err != nil {
		return errors.Wrap(err, "failed to start Kyverno webhook watcher")
	}

	kyvernoWebhookClient = client
	kyvernoWatcher = watcher

	slog.Info("Kyverno server initialized", "webhooks", len(watcher.GetEndpoints()))
	return nil
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
	if kyvernoWebhookClient == nil || kyvernoWatcher == nil {
		return fArgs.ParsedData, nil, errors.New("kyverno server not initialized; call InitKyvernoServer first")
	}

	webhooks := kyvernoWatcher.GetEndpoints()

	slog.Info("using Kyverno webhooks", "count", len(webhooks))

	return admissionwebhook.ValidateResources(kyvernoWebhookClient, webhooks, k8skit.NewK8sResourceProvider(), fArgs.ParsedData, kyvernoResponseConverter)
}

// kyvernoFailure represents a parsed failure from a Kyverno response message.
type kyvernoFailure struct {
	policyName string
	ruleName   string
	message    string
	path       string // converted from JSON Pointer to dot notation, empty if not present
}

// kyvernoResponseConverter converts a Kyverno AdmissionResponse into validation details
// and failed attributes.
func kyvernoResponseConverter(resp *admissionwebhook.AdmissionResponse, resourceInfo api.ResourceInfo) ([]string, []api.AttributeValue) {
	var details []string
	var failedAttrs []api.AttributeValue

	if resp.Status != nil && resp.Status.Message != "" {
		failures := parseKyvernoMessage(resp.Status.Message)
		for _, f := range failures {
			detail := fmt.Sprintf("policy %q rule %q: %s", f.policyName, f.ruleName, f.message)
			details = append(details, detail)

			av := api.AttributeValue{
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
			}
			if f.path != "" {
				av.AttributeInfo.AttributeIdentifier.Path = api.ResolvedPath(f.path)
			}
			failedAttrs = append(failedAttrs, av)
		}
		if len(failures) == 0 {
			// Couldn't parse structured failures, use raw message.
			details = append(details, resp.Status.Message)
		}
	}

	return details, failedAttrs
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
//
// It also extracts paths from messages like:
//
//	"validation error: ... rule disallow-latest-tag failed at path /spec/template/spec/containers/0/image/"
var (
	policyHeaderRE     = regexp.MustCompile(`^(\S+):$`)
	ruleMessageRE      = regexp.MustCompile(`^\s+(\S+): (.+)$`)
	validatingPolicyRE = regexp.MustCompile(`^Policy (\S+) failed: (.+)$`)
	failedAtPathRE     = regexp.MustCompile(`failed at path (/\S+)`)
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
			message := m[2]
			path := extractPathFromMessage(message)
			failures = append(failures, kyvernoFailure{
				policyName: currentPolicy,
				ruleName:   m[1],
				message:    message,
				path:       path,
			})
		}
	}
	return failures
}

// extractPathFromMessage extracts a JSON Pointer path from a Kyverno message
// and converts it to dot notation. Returns empty string if no path found.
func extractPathFromMessage(msg string) string {
	m := failedAtPathRE.FindStringSubmatch(msg)
	if m == nil {
		return ""
	}
	return jsonPointerToDotNotation(m[1])
}

// jsonPointerToDotNotation converts a JSON Pointer path like
// "/spec/template/spec/containers/0/image/" to dot notation
// "spec.template.spec.containers.0.image".
func jsonPointerToDotNotation(ptr string) string {
	// Remove leading and trailing slashes.
	ptr = strings.Trim(ptr, "/")
	if ptr == "" {
		return ""
	}
	return strings.ReplaceAll(ptr, "/", ".")
}
