// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"os"
	"sync"

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
		// Version rather than a bare "cub": the server records the User-Agent on
		// revisions and queued operations, so which cub made a change is worth
		// knowing after the fact, and it lets the server flag a skewed client.
		UserAgent: "cub/" + Version,
		Debug:     debug,
		// A --where bulk command acts on any number of entities, more than one request can
		// within the server's timeout.
		PageBulkOperations: true,
		WarningHandler:     printWarning,
	}
	// A missing token is not fatal: some commands run unauthenticated, and the
	// client simply omits the Authorization header in that case.
	if tokenData, err := contextManager.LoadTokenData(ctx); err == nil {
		opts.Token = tokenData.AccessToken
		// A context that can renew its session does so when the server rejects
		// it, so an expired session costs a command nothing. One that has
		// expired already is renewed before its first request rather than after
		// a 401 for it, and only once a request is made, so a command that makes
		// none does not renew.
		if canRenewSession(ctx, tokenData) {
			opts.Authenticator = sessionAuthenticator(ctx, tokenData.AccessToken)
			if tokenNeedsRenewal(tokenData.AccessToken) {
				opts.Token = ""
			}
		}
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

var (
	printedWarnings   = map[string]bool{}
	printedWarningsMu sync.Mutex
)

// printWarning prints a warning the server sent, such as that it ignored a field of the request
// it does not have, to stderr, as kubectl does. A command that makes many requests, such as a
// bulk one sent in pages, prints each warning once.
func printWarning(warning string) {
	printedWarningsMu.Lock()
	defer printedWarningsMu.Unlock()
	if printedWarnings[warning] {
		return
	}
	printedWarnings[warning] = true
	fmt.Fprintf(os.Stderr, "Warning: %s\n", warning)
}
