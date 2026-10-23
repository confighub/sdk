// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/confighub/sdk/core/cubapi"
)

// A session is renewed when it has expired or is about to, so a token is not
// sent that lapses while the request is in flight.
const renewalLeeway = time.Minute

// sessionRenewalError is a session that needed renewing and could not be. It
// carries the instructions a person or an agent acts on, which is the same advice
// as for a session that could never have renewed itself.
type sessionRenewalError struct {
	contextName  string
	instructions string
	err          error
}

func (e *sessionRenewalError) Error() string {
	return fmt.Sprintf("the session for context %q could not be renewed: %v. %s",
		e.contextName, e.err, e.instructions)
}

func (e *sessionRenewalError) Unwrap() error { return e.err }

// lastRenewal says how the session was renewed in this process, if it was, for
// `cub auth status` to report. Renewal is otherwise silent: someone who ran
// `cub space list` did not ask to hear about their credentials.
var lastRenewal string

// canRenewSession reports whether a context can renew its session without a
// person: it authenticates with a key, or it holds the refresh token a browser
// sign-in returns. Only an existing session is renewed. A context with no access
// token was never signed in, or was signed out, and is left that way.
func canRenewSession(ctx *Context, token *TokenData) bool {
	if ctx == nil || token == nil || token.AccessToken == "" {
		return false
	}
	return ctx.Metadata.PrivateKey != "" || token.RefreshToken != ""
}

// tokenNeedsRenewal reports whether an access token has expired or is about to.
// A token whose expiry cannot be read is left to the server to judge.
func tokenNeedsRenewal(accessToken string) bool {
	expiry, ok := tokenExpiry(accessToken)
	return ok && time.Now().Add(renewalLeeway).After(expiry)
}

// renewSession renews the context's session and saves the new credentials to its
// token file, where `cub auth login` saves them, so later commands use them too.
//
// The token file is read again first: another cub may have renewed the session
// since this one loaded it, and with a refresh token the identity provider can
// rotate, renewing again from the old one could fail where adopting theirs works.
// rejected is the access token this process last sent; a different, unexpired
// token in the file is someone else's renewal.
func renewSession(c context.Context, ctx *Context, rejected string) (*TokenData, error) {
	token, err := contextManager.LoadTokenData(ctx)
	if err != nil {
		return nil, renewalFailed(ctx, fmt.Errorf("failed to read the session: %w", err))
	}
	if token.AccessToken != "" && token.AccessToken != rejected && !tokenNeedsRenewal(token.AccessToken) {
		return token, nil
	}

	var renewed *TokenData
	switch {
	case ctx.Metadata.PrivateKey != "":
		renewed, err = renewFromKey(ctx)
		if err == nil {
			lastRenewal = fmt.Sprintf("from key %s", ctx.Metadata.PrivateKey)
		}
	case token.RefreshToken != "":
		renewed, err = renewFromRefreshToken(c, ctx, token)
		if err == nil {
			lastRenewal = "from the refresh token"
		}
	default:
		err = errors.New("the context holds neither a key nor a refresh token to renew it with")
	}
	if err != nil {
		return nil, renewalFailed(ctx, err)
	}
	if err := contextManager.SaveTokenData(ctx, renewed); err != nil {
		return nil, renewalFailed(ctx, fmt.Errorf("failed to save the renewed session: %w", err))
	}
	return renewed, nil
}

func renewalFailed(ctx *Context, err error) error {
	return &sessionRenewalError{contextName: ctx.Name, instructions: reauthInstructions(ctx), err: err}
}

// renewFromKey signs in again with the key the context last authenticated with,
// as `cub auth login` does with no flags.
//
// A key names one identity, but what it authenticates as is the server's to say,
// so a session for anyone other than the context's user and organization is
// refused rather than saved: renewal must not change who a context is.
func renewFromKey(ctx *Context) (*TokenData, error) {
	session, _, err := privateKeyAuth(ctx.Coordinate, ctx.Metadata.PrivateKey)
	if err != nil {
		return nil, err
	}
	if err := sameIdentity(ctx, session.User.Email, session.OrganizationID); err != nil {
		return nil, err
	}
	return &TokenData{AccessToken: session.AccessToken, RefreshToken: session.RefreshToken}, nil
}

// renewFromRefreshToken renews through the server's refresh endpoint, naming the
// context's organization so the session stays in it: the identity provider would
// otherwise pick the organization it selected at sign-in, which is not this one
// after a `cub auth switch`.
func renewFromRefreshToken(c context.Context, ctx *Context, token *TokenData) (*TokenData, error) {
	session, err := cubapi.RefreshSession(c, nil, ctx.Coordinate.ServerURL, token.RefreshToken, token.AccessToken, ctx.Coordinate.OrganizationID)
	if err != nil {
		return nil, err
	}
	if err := sameIdentity(ctx, "", session.OrganizationID); err != nil {
		return nil, err
	}
	renewed := &TokenData{AccessToken: session.AccessToken, RefreshToken: session.RefreshToken}
	if renewed.RefreshToken == "" {
		renewed.RefreshToken = token.RefreshToken
	}
	return renewed, nil
}

// sameIdentity refuses a session for a user or organization other than the
// context's. An empty value is one the server did not report, and a context that
// records none has nothing to compare it with.
func sameIdentity(ctx *Context, user, organizationID string) error {
	if user != "" && ctx.Coordinate.User != "" && user != ctx.Coordinate.User {
		return fmt.Errorf("the renewed session is for %s, not this context's user %s", user, ctx.Coordinate.User)
	}
	if organizationID != "" && ctx.Coordinate.OrganizationID != "" && organizationID != ctx.Coordinate.OrganizationID {
		return fmt.Errorf("the renewed session is for organization %s, not this context's organization %s",
			organizationID, ctx.Coordinate.OrganizationID)
	}
	return nil
}

// sessionAuthenticator renews the context's session for the API client when a
// request comes back 401, or before the first request when the session has
// already expired.
func sessionAuthenticator(ctx *Context, sent string) cubapi.Authenticator {
	return func(c context.Context) (*cubapi.AuthSession, error) {
		renewed, err := renewSession(c, ctx, sent)
		if err != nil {
			return nil, err
		}
		sent = renewed.AccessToken
		return &cubapi.AuthSession{AccessToken: renewed.AccessToken, AuthType: cubapi.AuthTypeJWT}, nil
	}
}

// currentTokenData loads the context's credentials for a command that sends the
// access token itself rather than through the API client, renewing them first
// when they have expired and the context can renew. A renewal that fails is
// reported on stderr and the stored token is returned, so the command behaves as
// it did before renewal existed and the server says what is wrong with it.
func currentTokenData(ctx *Context) (*TokenData, error) {
	token, err := contextManager.LoadTokenData(ctx)
	if err != nil {
		return nil, err
	}
	if !canRenewSession(ctx, token) || !tokenNeedsRenewal(token.AccessToken) {
		return token, nil
	}
	renewed, err := renewSession(context.Background(), ctx, token.AccessToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		return token, nil
	}
	return renewed, nil
}
