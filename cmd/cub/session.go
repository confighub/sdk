// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

const (
	AuthTypeBasic = "Basic"
	AuthTypeJWT   = "JWT"
)

type AuthSession struct {
	User              User   `json:"user"`
	AccessToken       string `json:"access_token"`
	OrganizationID    string `json:"organization_id"`
	AuthType          string `json:"auth_type"`
	BasicAuthPassword string `json:"basic_auth_password"`
}

// This is not the same as the User API!

type User struct {
	ID                string `json:"id"`
	Email             string `json:"email"`
	FirstName         string `json:"first_name"`
	LastName          string `json:"last_name"`
	ProfilePictureURL string `json:"profile_picture_url"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}
