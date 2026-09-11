// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

const (
	AuthTypeBasic = "Basic"
	AuthTypeJWT   = "JWT"
)

type AuthSession struct {
	User           User   `json:"user"`
	AccessToken    string `json:"access_token"`
	RefreshToken   string `json:"refresh_token,omitempty"`
	OrganizationID string `json:"organization_id"`
	AuthType       string `json:"auth_type"`

	// Note: This field is not part of the API. We just use it to pass the password to setAuthHeaderToken.
	BasicAuthPassword string `json:"basic_auth_password,omitempty"`
}

// This is not the ConfigHub User entity: it is the user as the server's login response describes it.

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
