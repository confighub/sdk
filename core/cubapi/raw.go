// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// Do sends a request the generated client has no method for, or one the caller wants to
// shape by hand, with everything the client applies to its own: the server, the
// Authorization header, the User-Agent, and debug dumping.
//
// path is relative to the API root, with or without a leading "/api" -- "/upload",
// "space/<id>/unit". query is added to any query string the path already carries. body
// may be nil; contentType is sent when it is not empty. The response is returned as it
// came, whatever its status, and the caller closes its body.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string) (*http.Response, error) {
	raw, ok := c.API.ClientInterface.(*goclientnew.Client)
	if !ok {
		return nil, fmt.Errorf("cubapi: raw requests need the generated client, not a %T", c.API.ClientInterface)
	}
	path = strings.TrimPrefix(strings.TrimPrefix(path, "/"), "api/")
	target, err := url.Parse(strings.TrimRight(raw.Server, "/") + "/" + path)
	if err != nil {
		return nil, fmt.Errorf("cubapi: bad path %q: %w", path, err)
	}
	if len(query) > 0 {
		merged := target.Query()
		for key, values := range query {
			for _, value := range values {
				merged.Add(key, value)
			}
		}
		target.RawQuery = merged.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(method), target.String(), body)
	if err != nil {
		return nil, fmt.Errorf("cubapi: build request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for _, edit := range raw.RequestEditors {
		if err := edit(ctx, req); err != nil {
			return nil, err
		}
	}
	return raw.Client.Do(req)
}
