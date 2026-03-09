// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package client is TODO.
package client

import (
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/workerapi"
)

type TransportConfig struct {
	Host      string
	BasePath  string
	Scheme    string
	UserAgent string
}

func (tc *TransportConfig) GetBaseURL() string {
	return tc.Scheme + "://" + tc.Host + tc.BasePath
}

const InvalidPath = "/badbadbad"

func (tc *TransportConfig) ToolchainToPath(toolchain workerapi.ToolchainType) string {
	path, valid := api.SupportedToolchains[toolchain]
	if valid {
		return path
	}
	return InvalidPath
}

func (tc *TransportConfig) GetToolchainURL(toolchain workerapi.ToolchainType) string {
	return tc.GetBaseURL() + tc.ToolchainToPath(toolchain)
}

func (tc *TransportConfig) GetUserAgent() string {
	if tc.UserAgent == "" {
		return "unknown-client"
	}
	return tc.UserAgent
}

func (tc *TransportConfig) GetContentType() string {
	return "application/json"
}
