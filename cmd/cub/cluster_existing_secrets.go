// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/confighub/sdk/core/cubapi"
	"github.com/google/uuid"
	"sigs.k8s.io/yaml"
)

// clusterExistingSecretsTimeoutDefault is how long attach --existing-secrets
// waits by default for the secret store to put each Secret in the cluster. The
// store can only do so once the namespace exists, and the controller's is
// created when attach installs the controller.
const clusterExistingSecretsTimeoutDefault = 5 * time.Minute

// clusterSecretPollInterval is how often attach looks for a Secret it is
// waiting for. A variable so tests need not wait.
var clusterSecretPollInterval = 5 * time.Second

// clusterSecretObject is what attach reads of a Secret in the cluster. data is
// base64 in the Secret, which encoding/json decodes into []byte.
type clusterSecretObject struct {
	Type     string `json:"type"`
	Metadata struct {
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
	Data map[string][]byte `json:"data"`
}

// clusterGetSecret reads a Secret from the cluster; nil when it, or its
// namespace, is not there.
func clusterGetSecret(kubeconfigPath, namespace, name string) (*clusterSecretObject, error) {
	raw, err := clusterKubectlOutput(ctx, kubeconfigPath, "get", "secret", name, "-n", namespace, "--ignore-not-found", "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("read Secret %s/%s: %w", namespace, name, err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, nil
	}
	var s clusterSecretObject
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("read Secret %s/%s: %w", namespace, name, err)
	}
	return &s, nil
}

// clusterWaitForSecret returns the Secret once it is in the cluster, waiting up
// to timeout for a secret store to create it.
func clusterWaitForSecret(out io.Writer, kubeconfigPath, namespace, name string, timeout time.Duration) (*clusterSecretObject, error) {
	deadline := time.Now().Add(timeout)
	waiting := false
	for {
		s, err := clusterGetSecret(kubeconfigPath, namespace, name)
		if err != nil || s != nil {
			return s, err
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("--existing-secrets: Secret %s/%s is not in the cluster after %s; the secret store has to create it (see 'cub cluster attach --help'), or pass a longer --existing-secrets-timeout",
				namespace, name, timeout)
		}
		if !waiting {
			fmt.Fprintf(out, "Waiting up to %s for Secret %s/%s, which the secret store creates...\n", timeout, namespace, name)
			waiting = true
		}
		time.Sleep(clusterSecretPollInterval)
	}
}

// clusterPullTokenFromSecret checks that s, found in the cluster as the
// controller's registry credentials Secret, is one the controller can pull
// from ociURL with, as the Secret attach would apply is (clusterControllerCredsSecretFor),
// and returns the pull token in it. insecureHTTP is whether the registry is plain
// HTTP, which Argo CD has to be told.
func clusterPullTokenFromSecret(controller string, s *clusterSecretObject, ociURL string, insecureHTTP bool) (string, error) {
	var problems []string
	want := func(ok bool, format string, args ...any) {
		if !ok {
			problems = append(problems, fmt.Sprintf(format, args...))
		}
	}
	var token string
	switch controller {
	case clusterControllerSveltos:
		want(s.Type == "addons.projectsveltos.io/cluster-profile", "its type is %q, not addons.projectsveltos.io/cluster-profile, which Sveltos reads", s.Type)
		want(string(s.Data["username"]) == "token", `its username is %q, not "token", which the registry takes a token with`, s.Data["username"])
		token = string(s.Data["password"])
	case clusterControllerFlux:
		want(s.Type == "kubernetes.io/dockerconfigjson", "its type is %q, not kubernetes.io/dockerconfigjson, which Flux reads", s.Type)
		var err error
		if token, err = clusterFluxPullToken(s.Data[".dockerconfigjson"], ociURL); err != nil {
			problems = append(problems, err.Error())
		}
	default:
		want(s.Metadata.Labels["argocd.argoproj.io/secret-type"] == "repo-creds",
			"it has no label argocd.argoproj.io/secret-type=repo-creds, which is how Argo CD finds it")
		want(string(s.Data["type"]) == "oci", `its type key is %q, not "oci"`, s.Data["type"])
		want(clusterArgoCredsForRegistry(string(s.Data["url"]), ociURL), "its url is %q, not %s, the registry the root pulls from", s.Data["url"], ociURL)
		want(string(s.Data["username"]) == "token", `its username is %q, not "token", which the registry takes a token with`, s.Data["username"])
		if insecureHTTP {
			want(string(s.Data["insecureOCIForceHttp"]) == "true", `the registry is plain HTTP, and its insecureOCIForceHttp is not "true"`)
		}
		token = string(s.Data["password"])
	}
	want(token != "", "it holds no pull token")
	// A value loaded from a file often keeps the file's newline, and the
	// controller sends the token exactly as it is.
	want(token == strings.TrimSpace(token), "its pull token starts or ends with whitespace, such as the newline of a file it was read from, which the controller would send as part of it")
	if len(problems) > 0 {
		return "", errors.New(strings.Join(problems, "; "))
	}
	return token, nil
}

// clusterFluxPullToken returns the password of the auth for ociURL's host in a
// docker config, as Flux looks it up, from either its password or its auth.
func clusterFluxPullToken(dockerConfig []byte, ociURL string) (string, error) {
	if len(dockerConfig) == 0 {
		return "", errors.New("it has no .dockerconfigjson")
	}
	var cfg struct {
		Auths map[string]struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Auth     string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(dockerConfig, &cfg); err != nil {
		return "", fmt.Errorf("its .dockerconfigjson is not a docker config: %v", err)
	}
	u, err := url.Parse(ociURL)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("the registry URL %q has no host", ociURL)
	}
	auth, ok := cfg.Auths[u.Host]
	if !ok {
		return "", fmt.Errorf("its .dockerconfigjson has no auth for %s, the registry the root pulls from", u.Host)
	}
	username, password := auth.Username, auth.Password
	if password == "" && auth.Auth != "" {
		decoded, err := base64.StdEncoding.DecodeString(auth.Auth)
		if err != nil {
			return "", fmt.Errorf("its auth for %s is not base64", u.Host)
		}
		username, password, _ = strings.Cut(string(decoded), ":")
	}
	if username != "token" {
		return "", fmt.Errorf(`its username for %s is %q, not "token", which the registry takes a token with`, u.Host, username)
	}
	return password, nil
}

// clusterRequireIdentity fails unless the bearer token authenticates to this
// ConfigHub as userID, the user of the cluster's service account.
func clusterRequireIdentity(token string, userID uuid.UUID, serviceAccountSlug string) error {
	client, err := cubapi.NewClient(cubapi.ClientOptions{ServerURL: clusterServerURL(), Token: token, UserAgent: "cub/" + Version,
		HTTPClient: clusterCredentialCheckClient})
	if err != nil {
		return err
	}
	res, err := client.API.GetMeWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("authenticate with it: %w", err)
	}
	switch {
	case res.StatusCode() == http.StatusUnauthorized:
		return fmt.Errorf("it does not authenticate to %s: it may be revoked or expired, or issued by another ConfigHub", clusterServerURL())
	case res.JSON200 == nil:
		// Not a verdict on the credential: the server could not say.
		return fmt.Errorf("check who it authenticates as: %s answered HTTP %d", clusterServerURL(), res.StatusCode())
	}
	if got := uuid.UUID(res.JSON200.UserID); got != userID {
		return fmt.Errorf("it authenticates as user %s, not as service account %s", got, serviceAccountSlug)
	}
	return nil
}

// clusterCredentialCheckClient is what attach tries the credentials in the
// cluster's Secrets with, bounded so that an address nothing answers at fails
// the attach instead of hanging it.
var clusterCredentialCheckClient = &http.Client{Timeout: 30 * time.Second}

// clusterRequireRegistryToken fails unless the registry takes the pull token,
// as the controller presents it, and the token is of userID, the user of the
// cluster's service account. A long-lived token works only at the registry's
// token exchange, so that is where it is tried, at the address cub reaches the
// registry at (the controller's may differ only by host, inside the cluster).
func clusterRequireRegistryToken(token string, userID uuid.UUID, serviceAccountSlug string) error {
	oci, err := clusterResolveControllerOCI()
	if err != nil {
		return err
	}
	endpoint, err := clusterOCIEndpoint(oci.advertised, oci.apiURL)
	if err != nil {
		return err
	}
	scheme := "https"
	if oci.insecure {
		scheme = "http"
	}
	tokenURL := scheme + "://" + strings.TrimPrefix(endpoint, "oci://") + "/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth("token", token)
	req.Header.Set("User-Agent", "cub/"+Version)
	resp, err := clusterCredentialCheckClient.Do(req)
	if err != nil {
		return fmt.Errorf("try it at the registry: %w", err)
	}
	_ = resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("the registry at %s refuses it: it may be revoked or expired, of a service account deleted since, or issued by another ConfigHub", tokenURL)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("try it at the registry: %s answered HTTP %d", tokenURL, resp.StatusCode)
	}
	// The registry has just verified the token, so its claims are the server's.
	got, err := clusterTokenUserID(token)
	if err != nil {
		return err
	}
	if got != userID {
		return fmt.Errorf("it is a token of user %s, not of service account %s", got, serviceAccountSlug)
	}
	return nil
}

// clusterTokenUserID reads the internal user ID a ConfigHub token is issued
// to, without verifying it: the caller has had the server accept it.
func clusterTokenUserID(token string) (uuid.UUID, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return uuid.Nil, errors.New("it is not a ConfigHub token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return uuid.Nil, errors.New("it is not a ConfigHub token")
	}
	var claims struct {
		InternalUserID string `json:"internal_user_id"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return uuid.Nil, errors.New("it is not a ConfigHub token")
	}
	id, err := uuid.Parse(claims.InternalUserID)
	if err != nil {
		return uuid.Nil, errors.New("it is a token that names no ConfigHub user")
	}
	return id, nil
}

// clusterRequirePullToken checks the controller's registry credentials Secret
// in the cluster: that the controller can use it, and that its token is one of
// the cluster's service account.
func clusterRequirePullToken(s *clusterSecretObject, controller, ociURL string, insecureHTTP bool, userID uuid.UUID, serviceAccountSlug string) error {
	namespace, name := clusterControllerNamespace(controller), clusterControllerCredsSecret(controller)
	token, err := clusterPullTokenFromSecret(controller, s, ociURL, insecureHTTP)
	if err == nil {
		err = clusterRequireRegistryToken(token, userID, serviceAccountSlug)
	}
	if err != nil {
		return fmt.Errorf("--existing-secrets: Secret %s/%s: %w", namespace, name, err)
	}
	return nil
}

// clusterRequireArgobotKey checks argobot's key Secret in the cluster with
// clusterCheckArgobotKey.
func clusterRequireArgobotKey(s *clusterSecretObject, userID uuid.UUID, serviceAccountSlug string) error {
	if err := clusterCheckArgobotKey(s.Data["CONFIGHUB_AUTH_PRIVATE_KEY"], userID, serviceAccountSlug); err != nil {
		return fmt.Errorf("--existing-secrets: Secret %s/%s: %w", clusterArgobotNamespace, clusterArgobotSecret, err)
	}
	return nil
}

// clusterCheckArgobotKey checks argobot's private key, the value of its
// CONFIGHUB_AUTH_PRIVATE_KEY, by authenticating with it as argobot will, and
// checking that it is the cluster's service account that it authenticates as.
func clusterCheckArgobotKey(key []byte, userID uuid.UUID, serviceAccountSlug string) error {
	if len(key) == 0 {
		return errors.New("it has no CONFIGHUB_AUTH_PRIVATE_KEY, which argobot authenticates with")
	}
	// argobot names no subject, so the key has to record its holder.
	signer, err := cubapi.NewAssertionSigner(key, "", "")
	if err != nil {
		return fmt.Errorf("CONFIGHUB_AUTH_PRIVATE_KEY: %w", err)
	}
	session, err := cubapi.PerformAssertionAuthContext(ctx, clusterCredentialCheckClient, clusterServerURL(), signer)
	if err != nil {
		return fmt.Errorf("CONFIGHUB_AUTH_PRIVATE_KEY: %w (is it registered on service account %s? 'cub serviceaccount key list %s')", err, serviceAccountSlug, serviceAccountSlug)
	}
	if err := clusterRequireIdentity(session.AccessToken, userID, serviceAccountSlug); err != nil {
		return fmt.Errorf("CONFIGHUB_AUTH_PRIVATE_KEY: %w", err)
	}
	return nil
}

// clusterArgobotKeyInManifest returns the CONFIGHUB_AUTH_PRIVATE_KEY of the
// argobot-secrets Secret in a manifest clusterArgobotSecretManifest wrote.
func clusterArgobotKeyInManifest(manifest []byte) ([]byte, error) {
	for _, doc := range strings.Split(string(manifest), "\n---") {
		var secret struct {
			Kind       string            `json:"kind"`
			StringData map[string]string `json:"stringData"`
			Data       map[string][]byte `json:"data"`
		}
		if yaml.Unmarshal([]byte(doc), &secret) != nil || secret.Kind != "Secret" {
			continue
		}
		if key := secret.StringData["CONFIGHUB_AUTH_PRIVATE_KEY"]; key != "" {
			return []byte(key), nil
		}
		return secret.Data["CONFIGHUB_AUTH_PRIVATE_KEY"], nil
	}
	return nil, errors.New("it holds no Secret")
}
