// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

var (
	// Global API client instance
	cubClientNew *goclientnew.ClientWithResponses

	// cubClient is the cubapi.Client wrapper around the same underlying client
	// as cubClientNew (cubClient.API == cubClientNew). Commands migrated to the
	// cubapi query/mutation helpers use this; the rest still use cubClientNew.
	cubClient *cubapi.Client

	// Global context manager instance
	contextManager *ContextManager

	ctx = context.Background()
)

// InitializeClient initializes the API client for the given context.
// It sets the base URL and the authentication header if a token is present.
// If the context is updated during the course of execution and further API calls are made,
// then this function should be called again to update the API client.
//
// Transport and auth plumbing live in github.com/confighub/sdk/core/cubapi so
// that other Go tools and cub plugins build their clients the same way.
func InitializeClient(ctx *Context) (*goclientnew.ClientWithResponses, error) {
	opts := cubapi.ClientOptions{
		ServerURL: ctx.Coordinate.ServerURL,
		UserAgent: "cub",
		Debug:     debug,
	}
	// A missing token is not fatal: some commands run unauthenticated, and the
	// client simply omits the Authorization header in that case.
	if tokenData, err := contextManager.LoadTokenData(ctx); err == nil {
		opts.Token = tokenData.AccessToken
	}
	c, err := cubapi.NewClient(opts)
	if err != nil {
		return nil, err
	}
	// Also publish the wrapper for commands migrated to the cubapi helpers; it
	// shares the same underlying client as the returned *ClientWithResponses.
	cubClient = c
	return c.API, nil
}
