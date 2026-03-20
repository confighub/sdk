// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httputil"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

var (
	// Global API client instance
	cubClientNew *goclientnew.ClientWithResponses

	// Global context manager instance
	contextManager *ContextManager

	ctx = context.Background()
)

type CubTransport struct {
	RoundTripper http.RoundTripper
	Agent        string
	Debug        bool
}

func (ct *CubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("User-Agent", ct.Agent)

	if ct.Debug {
		dump, err := httputil.DumpRequestOut(r, true)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(dump))
	}
	res, err := ct.RoundTripper.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if ct.Debug {
		dump, err := httputil.DumpResponse(res, true)
		if err != nil {
			return res, err
		}
		fmt.Println(string(dump))
	}
	return res, nil
}

func setAuthHeader(authSession *cubapi.AuthSession) goclientnew.RequestEditorFn {
	return func(ctx context.Context, r *http.Request) error {
		authHeaderToken := setAuthHeaderToken(authSession)
		if authHeaderToken != "" {
			r.Header.Set("Authorization", authHeaderToken)
		}
		return nil
	}
}

func setAuthHeaderToken(authSession *cubapi.AuthSession) string {
	var authHeaderToken string
	if authSession.AuthType == cubapi.AuthTypeBasic {
		encoded := base64.StdEncoding.EncodeToString([]byte(authSession.User.Email + ":" + authSession.BasicAuthPassword))
		authHeaderToken = fmt.Sprintf("Basic %s", encoded)
	} else {
		authHeaderToken = fmt.Sprintf("Bearer %s", authSession.AccessToken)
	}
	return authHeaderToken
}

// InitializeClient initializes the API client for the given context.
// It sets the base URL and the authentication header if a token is present.
// If the context is updated during the course of execution and further API calls are made,
// then this function should be called again to update the API client.
func InitializeClient(ctx *Context) (*goclientnew.ClientWithResponses, error) {
	ct := &CubTransport{
		RoundTripper: http.DefaultTransport,
		Agent:        "cub",
		Debug:        debug,
	}
	baseURL := ctx.Coordinate.ServerURL + "/api"
	hasToken := true
	var authHeader string
	tokenData, err := contextManager.LoadTokenData(ctx)
	if err != nil {
		hasToken = false
	} else {
		authHeader = fmt.Sprintf("Bearer %s", tokenData.AccessToken)
	}

	return goclientnew.NewClientWithResponses(baseURL, func(c *goclientnew.Client) error {
		c.Client = &http.Client{Transport: ct}
		if hasToken {
			c.RequestEditors = append(c.RequestEditors, func(ctx context.Context, req *http.Request) error {
				req.Header.Set("Authorization", authHeader)
				return nil
			})
		}
		return nil
	})
}
