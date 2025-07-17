// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

const (
	AuthTypeBasic = "Basic"
	AuthTypeJWT   = "JWT"
)

// See also https://pkg.go.dev/github.com/workos/workos-go/v4@v4.37.0/pkg/usermanagement
// https://workos.com/docs/reference/user-management/authentication/code

type AuthSession struct {
	User           User   `json:"user"`
	AccessToken    string `json:"access_token"`
	RefreshToken   string `json:"refresh_token,omitempty"`
	OrganizationID string `json:"organization_id"`
	AuthType       string `json:"auth_type"`

	// Note: This field is not part of the API. We just use it to pass the password to setAuthHeaderToken.
	BasicAuthPassword string `json:"basic_auth_password,omitempty"`
}

// This is not the same as the ConfigHub User API! It's the WorkOS API.
// https://workos.com/docs/reference/user-management/user

type User struct {
	ID                string            `json:"id"`
	Email             string            `json:"email"`
	FirstName         string            `json:"first_name"`
	LastName          string            `json:"last_name"`
	ProfilePictureURL string            `json:"profile_picture_url"`
	CreatedAt         string            `json:"created_at"`
	UpdatedAt         string            `json:"updated_at"`
	ExternalID        string            `json:"external_id,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}
