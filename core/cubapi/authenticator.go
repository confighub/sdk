// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// Authenticator mints a session for a client. A client built with one keeps the session's
// credentials behind a lock, sets them on every request, and when a request comes back 401
// calls the authenticator once, for every request that saw the 401 at the same time, and
// retries. The client is built once and never rebuilt, however long it lives.
type Authenticator func(ctx context.Context) (*AuthSession, error)

// AssertionAuthenticator authenticates as the identity the signer's key belongs to, by signing
// an assertion and exchanging it for a session, each time the client needs one.
func AssertionAuthenticator(serverURL string, signer *AssertionSigner) Authenticator {
	return func(ctx context.Context) (*AuthSession, error) {
		return PerformAssertionAuthContext(ctx, nil, serverURL, signer)
	}
}

// authTransport sets the session's credentials on every request and renews them on a 401.
//
// The renewal is single-flight: a request that saw a 401 renews only if the credentials it
// sent are still the current ones. If another request renewed them first, it just retries with
// the new ones. Concurrent 401s thus cost one authentication, and a credential the server
// rejects for good costs one authentication per rejected request, never a storm.
type authTransport struct {
	base         http.RoundTripper
	authenticate Authenticator

	mu     sync.RWMutex
	header string // the current Authorization value; "" until the first authentication
}

func (t *authTransport) current() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.header
}

// renew replaces the credentials unless they already differ from the ones the caller sent, in
// which case someone else renewed them and theirs are returned.
func (t *authTransport) renew(ctx context.Context, sent string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.header != sent {
		return t.header, nil
	}
	session, err := t.authenticate(ctx)
	if err != nil {
		return "", err
	}
	header := authHeaderValue(ClientOptions{Session: session})
	if header == "" {
		return "", fmt.Errorf("cubapi: the authenticator returned a session with no credentials")
	}
	t.header = header
	return header, nil
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	header := t.current()
	if header == "" {
		var err error
		if header, err = t.renew(req.Context(), ""); err != nil {
			return nil, err
		}
	}
	// A retry needs the body again. The generated client builds bodies the standard library can
	// rewind; a request whose body it cannot is sent once, and its 401 is the caller's.
	var rewind func() (io.ReadCloser, error)
	if req.Body != nil && req.Body != http.NoBody {
		rewind = req.GetBody
	}
	attempt := req.Clone(req.Context())
	attempt.Header.Set("Authorization", header)
	res, err := t.base.RoundTrip(attempt)
	if err != nil || res.StatusCode != http.StatusUnauthorized {
		return res, err
	}
	if req.Body != nil && req.Body != http.NoBody && rewind == nil {
		return res, nil
	}
	renewed, rerr := t.renew(req.Context(), header)
	if rerr != nil {
		// The 401 stands, and what stopped the renewal is reported with it.
		res.Body.Close()
		return nil, fmt.Errorf("cubapi: re-authentication after a 401 failed: %w", rerr)
	}
	res.Body.Close()
	retry := req.Clone(req.Context())
	if rewind != nil {
		body, berr := rewind()
		if berr != nil {
			return nil, berr
		}
		retry.Body = body
	}
	retry.Header.Set("Authorization", renewed)
	return t.base.RoundTrip(retry)
}
