// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package constants

// ServerVersionHeader is the response header the ConfigHub server stamps its own
// version onto, on every /api response. A client that reads it learns the server
// version from a call it was already making, rather than spending a round trip on
// /api/info. The value is what /api/info reports in Version: a release version such
// as "v0.2.34", or "v0.2-dev" for a build from a working tree.
//
// It lives here, and not in cubapi, so that the server can stamp the header without
// importing the API client. The server does not call its own API, and that import
// would pull the generated client into the server binary and into the OpenAPI spec
// generator, which links internal/views -> cubapi -> goclient-new: a broken client
// then blocks regenerating that same client.
const ServerVersionHeader = "ConfigHub-Version"

// ContinueHeader is the response header a List or Search returns the continue token in when
// there may be more entities after the page it returned. Pass the token as the next request's
// "continue" query parameter, with the request's other parameters unchanged, and keep reading
// until a response has no ContinueHeader: a page can be shorter than the limit, or empty, and
// still be followed by more.
const ContinueHeader = "ConfigHub-Continue"
