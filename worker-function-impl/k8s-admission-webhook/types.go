// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package admissionwebhook provides a shared library for validating Kubernetes
// resources by calling admission webhooks (e.g., Kyverno, OPA Gatekeeper).
package admissionwebhook

import (
	"encoding/json"

	"github.com/google/uuid"
)

// AdmissionReview is a minimal local definition to avoid pulling in k8s.io/api.
type AdmissionReview struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Request    *AdmissionRequest  `json:"request,omitempty"`
	Response   *AdmissionResponse `json:"response,omitempty"`
}

// AdmissionRequest contains the request details for an AdmissionReview.
type AdmissionRequest struct {
	UID             string                `json:"uid"`
	Kind            GroupVersionKind      `json:"kind"`
	Resource        GroupVersionResource  `json:"resource"`
	RequestKind     *GroupVersionKind     `json:"requestKind,omitempty"`
	RequestResource *GroupVersionResource `json:"requestResource,omitempty"`
	Namespace       string                `json:"namespace"`
	Name            string                `json:"name"`
	Operation       string                `json:"operation"`
	Object          json.RawMessage       `json:"object"`
	DryRun          *bool                 `json:"dryRun,omitempty"`
	UserInfo        UserInfo              `json:"userInfo"`
}

// AdmissionResponse contains the response from a webhook.
type AdmissionResponse struct {
	UID      string   `json:"uid"`
	Allowed  bool     `json:"allowed"`
	Status   *Status  `json:"status,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// GroupVersionKind identifies a Kubernetes resource kind.
type GroupVersionKind struct {
	Group   string `json:"group"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

// GroupVersionResource identifies a Kubernetes resource type.
type GroupVersionResource struct {
	Group    string `json:"group"`
	Version  string `json:"version"`
	Resource string `json:"resource"`
}

// Status contains the status message from an admission response.
type Status struct {
	Message string `json:"message"`
	Status  string `json:"status"`
}

// UserInfo contains the user information for an admission request.
type UserInfo struct {
	Username string `json:"username"`
}

// NewUID generates a random UUID for AdmissionReview requests.
func NewUID() string {
	return uuid.New().String()
}
