// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
	"golang.org/x/crypto/hkdf"
)

const (
	// AnnotationGenerateKubecontext must be set to "allow" on the ServiceAccount
	// for the function to issue a token.
	AnnotationGenerateKubecontext = "confighub.com/generate-kubecontext"

	// AnnotationAccessTTL controls the token lifetime.
	AnnotationAccessTTL = "confighub.com/access-ttl"

	// AnnotationAccessMaxTTL caps user-requested TTL overrides.
	AnnotationAccessMaxTTL = "confighub.com/access-max-ttl"

	// AnnotationKubernetesAPIEndpoint is the external API server endpoint to use in the
	// generated kubeconfig. Required, since the function will not be able to determine it
	// itself from inside the cluster.
	AnnotationKubernetesAPIEndpoint = "confighub.com/kubernetes-api-endpoint"

	// hkdfSalt is used in key derivation.
	hkdfSalt = "confighub-access-v1"

	// defaultTTL is used when no TTL annotation is present.
	defaultTTL = 1 * time.Hour
)

// k8sClient holds the state needed to make Kubernetes API calls.
type k8sClient struct {
	httpClient *http.Client
	apiHost    string
	token      string
	caData     []byte // PEM-encoded CA certificate
}

var client *k8sClient

// InitAccess initializes the Kubernetes client for token requests.
// It tries in-cluster config first, then falls back to kubeconfig.
func InitAccess() error {
	c, err := newK8sClient()
	if err != nil {
		return errors.Wrap(err, "failed to initialize access function k8s client")
	}
	client = c
	slog.Info("Access function initialized", "apiHost", client.apiHost)
	return nil
}

func newK8sClient() (*k8sClient, error) {
	tokenBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return nil, errors.Wrap(err, "not running in-cluster: failed to read service account token")
	}
	token := strings.TrimSpace(string(tokenBytes))

	caData, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
	if err != nil {
		return nil, errors.Wrap(err, "failed to read cluster CA cert")
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caData) {
		return nil, errors.New("failed to parse cluster CA cert")
	}

	apiHost := os.Getenv("KUBERNETES_SERVICE_HOST")
	apiPort := os.Getenv("KUBERNETES_SERVICE_PORT")
	if apiHost == "" || apiPort == "" {
		return nil, errors.New("KUBERNETES_SERVICE_HOST/PORT not set")
	}

	return &k8sClient{
		httpClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs:    pool,
					MinVersion: tls.VersionTLS12,
				},
			},
		},
		apiHost: fmt.Sprintf("https://%s:%s", apiHost, apiPort),
		token:   token,
		caData:  caData,
	}, nil
}

// GetGenerateKubecontextSignature returns the function signature.
func GetGenerateKubecontextSignature() api.FunctionSignature {
	return api.FunctionSignature{
		FunctionName: "generate-kubecontext",
		Parameters: []api.FunctionParameter{
			{
				ParameterName: "public-key",
				Description:   "Base64-encoded X25519 public key for encrypting the kubeconfig response",
				Required:      true,
				DataType:      api.DataTypeString,
			},
			{
				ParameterName: "ttl",
				Description:   "Requested token TTL (e.g., 30m, 1h); capped by access-max-ttl annotation",
				Required:      false,
				DataType:      api.DataTypeString,
			},
		},
		RequiredParameters: 1,
		OutputInfo: &api.FunctionOutput{
			ResultName:  "encrypted-kubeconfig",
			Description: "ECIES-encrypted kubeconfig containing a time-bound ServiceAccount token",
			OutputType:  api.OutputTypeOpaque,
		},
		Mutating:              false, // Read-only: View permission suffices. Edit would let users expand their own access.
		Validating:            false,
		Hermetic:              false,
		Idempotent:            false,
		Description:           "Generates a time-bound kubeconfig for an existing ServiceAccount. The ServiceAccount must have the confighub.com/generate-kubecontext: allow annotation. The kubeconfig is encrypted with the caller's X25519 public key.",
		FunctionType:          api.FunctionTypeCustom,
		AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
	}
}

// registerAccessFunctions registers the generate-kubecontext function.
func registerAccessFunctions(fh handler.FunctionRegistry) {
	fh.RegisterFunction("generate-kubecontext", &handler.FunctionRegistration{
		FunctionSignature: GetGenerateKubecontextSignature(),
		Function:          GenerateKubecontextFunction,
	})
}

// GenerateKubecontextFunction is the function implementation.
func GenerateKubecontextFunction(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
	// Lazy initialization of the Kubernetes client on first invocation.
	if client == nil {
		if err := InitAccess(); err != nil {
			return fArgs.ParsedData, nil, errors.Wrap(err, "failed to initialize access function")
		}
	}

	// Parse arguments.
	if len(fArgs.Arguments) < 1 {
		return fArgs.ParsedData, nil, errors.New("public-key argument is required")
	}
	pubKeyB64 := fmt.Sprintf("%v", fArgs.Arguments[0].Value)

	var requestedTTL string
	if len(fArgs.Arguments) > 1 {
		requestedTTL = fmt.Sprintf("%v", fArgs.Arguments[1].Value)
	}

	// Parse the config data to find the ServiceAccount.
	sa, err := findServiceAccount(fArgs.ParsedData)
	if err != nil {
		return fArgs.ParsedData, nil, err
	}

	// Check the allow annotation.
	allowValue, ok := sa.annotations[AnnotationGenerateKubecontext]
	if !ok || allowValue != "allow" {
		return fArgs.ParsedData, nil, errors.Newf("ServiceAccount %s/%s does not have annotation %s: allow",
			sa.namespace, sa.name, AnnotationGenerateKubecontext)
	}

	// Determine TTL.
	ttl, err := resolveTTL(sa.annotations, requestedTTL)
	if err != nil {
		return fArgs.ParsedData, nil, err
	}

	// Request a token.
	token, err := requestToken(sa.namespace, sa.name, ttl)
	if err != nil {
		return fArgs.ParsedData, nil, errors.Wrap(err, "failed to request token")
	}

	// Build kubeconfig.
	kubeconfig, err := buildKubeconfig(sa, token)
	if err != nil {
		return fArgs.ParsedData, nil, errors.Wrap(err, "failed to build kubeconfig")
	}

	// Encrypt the kubeconfig.
	encrypted, err := encryptResponse(pubKeyB64, kubeconfig)
	if err != nil {
		return fArgs.ParsedData, nil, errors.Wrap(err, "failed to encrypt kubeconfig")
	}

	slog.Info("Generated kubecontext",
		"serviceAccount", sa.name,
		"namespace", sa.namespace,
		"ttl", ttl.String())

	return fArgs.ParsedData, encrypted, nil
}

// serviceAccountInfo holds the parsed ServiceAccount details from ConfigData.
type serviceAccountInfo struct {
	name        string
	namespace   string
	annotations map[string]string
}

// findServiceAccount iterates over YAML documents in ConfigData to find a ServiceAccount.
func findServiceAccount(parsedData gaby.Container) (*serviceAccountInfo, error) {
	for _, doc := range parsedData {
		kind, _, err := yamlkit.YamlSafePathGetValue[string](doc, "kind", true)
		if err != nil || kind != "ServiceAccount" {
			continue
		}
		apiVersion, _, _ := yamlkit.YamlSafePathGetValue[string](doc, "apiVersion", true)
		if apiVersion != "v1" {
			continue
		}

		name, _, err := yamlkit.YamlSafePathGetValue[string](doc, "metadata.name", false)
		if err != nil {
			return nil, errors.Wrap(err, "ServiceAccount missing metadata.name")
		}

		namespace, _, _ := yamlkit.YamlSafePathGetValue[string](doc, "metadata.namespace", true)

		// Extract annotations.
		annotations := make(map[string]string)
		annNode := doc.Search("metadata", "annotations")
		if annNode != nil {
			if annMap, ok := annNode.Data().(map[string]interface{}); ok {
				for k, v := range annMap {
					annotations[k] = fmt.Sprintf("%v", v)
				}
			}
		}

		return &serviceAccountInfo{
			name:        name,
			namespace:   namespace,
			annotations: annotations,
		}, nil
	}
	return nil, errors.New("no ServiceAccount found in config data")
}

// resolveTTL determines the effective TTL from annotations and optional user request.
func resolveTTL(annotations map[string]string, requestedTTL string) (time.Duration, error) {
	// Parse the configured TTL.
	ttl := defaultTTL
	if ttlStr, ok := annotations[AnnotationAccessTTL]; ok {
		parsed, err := time.ParseDuration(ttlStr)
		if err != nil {
			return 0, errors.Newf("invalid %s annotation value %q: %v", AnnotationAccessTTL, ttlStr, err)
		}
		ttl = parsed
	}

	// Parse max TTL.
	maxTTL := ttl
	if maxStr, ok := annotations[AnnotationAccessMaxTTL]; ok {
		parsed, err := time.ParseDuration(maxStr)
		if err != nil {
			return 0, errors.Newf("invalid %s annotation value %q: %v", AnnotationAccessMaxTTL, maxStr, err)
		}
		maxTTL = parsed
	}

	// Handle user-requested TTL.
	if requestedTTL != "" {
		requested, err := time.ParseDuration(requestedTTL)
		if err != nil {
			return 0, errors.Newf("invalid requested TTL %q: %v", requestedTTL, err)
		}
		if requested > maxTTL {
			return 0, errors.Newf("requested TTL %s exceeds maximum %s", requested, maxTTL)
		}
		ttl = requested
	}

	return ttl, nil
}

// tokenRequest is the Kubernetes TokenRequest API payload.
type tokenRequest struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Spec       struct {
		ExpirationSeconds int64 `json:"expirationSeconds"`
	} `json:"spec"`
}

// tokenResponse is the Kubernetes TokenRequest API response.
type tokenResponse struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}

// requestToken calls the Kubernetes TokenRequest API to get a bound token.
func requestToken(namespace, name string, ttl time.Duration) (string, error) {
	req := tokenRequest{
		APIVersion: "authentication.k8s.io/v1",
		Kind:       "TokenRequest",
	}
	req.Spec.ExpirationSeconds = int64(ttl.Seconds())

	body, err := json.Marshal(req)
	if err != nil {
		return "", errors.Wrap(err, "failed to marshal token request")
	}

	url := fmt.Sprintf("%s/api/v1/namespaces/%s/serviceaccounts/%s/token",
		client.apiHost, namespace, name)

	httpReq, err := http.NewRequestWithContext(context.Background(), "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", errors.Wrap(err, "failed to create HTTP request")
	}
	httpReq.Header.Set("Authorization", "Bearer "+client.token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := client.httpClient.Do(httpReq)
	if err != nil {
		return "", errors.Wrap(err, "token request failed")
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", errors.Wrap(err, "failed to read token response")
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", errors.Newf("TokenRequest API returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", errors.Wrap(err, "failed to parse token response")
	}

	if tokenResp.Status.Token == "" {
		return "", errors.New("TokenRequest API returned empty token")
	}

	return tokenResp.Status.Token, nil
}

var kubeconfigTemplate = template.Must(template.New("kubeconfig").Parse(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: {{ .Server }}
    certificate-authority-data: {{ .CAData }}
  name: {{ .ClusterName }}
contexts:
- context:
    cluster: {{ .ClusterName }}
    user: confighub-access
{{- if .Namespace }}
    namespace: {{ .Namespace }}
{{- end }}
  name: {{ .ContextName }}
current-context: {{ .ContextName }}
users:
- name: confighub-access
  user:
    token: {{ .Token }}
`))

type kubeconfigData struct {
	Server      string
	CAData      string
	ClusterName string
	ContextName string
	Namespace   string
	Token       string
}

// buildKubeconfig builds a kubeconfig YAML string.
func buildKubeconfig(sa *serviceAccountInfo, token string) ([]byte, error) {
	contextName := fmt.Sprintf("confighub-%s-%s", sa.namespace, sa.name)
	clusterName := "confighub-cluster"

	server, ok := sa.annotations[AnnotationKubernetesAPIEndpoint]
	if !ok || server == "" {
		return nil, errors.Newf("ServiceAccount %s/%s missing required annotation %s",
			sa.namespace, sa.name, AnnotationKubernetesAPIEndpoint)
	}

	data := kubeconfigData{
		Server:      server,
		CAData:      base64.StdEncoding.EncodeToString(client.caData),
		ClusterName: clusterName,
		ContextName: contextName,
		Namespace:   sa.namespace,
		Token:       token,
	}

	var buf bytes.Buffer
	if err := kubeconfigTemplate.Execute(&buf, data); err != nil {
		return nil, errors.Wrap(err, "failed to render kubeconfig template")
	}
	return buf.Bytes(), nil
}

// EncryptedResponse is the JSON structure returned in function output.
type EncryptedResponse struct {
	PublicKey  string `json:"publicKey"`  // Worker's ephemeral public key (base64)
	Nonce      string `json:"nonce"`      // AES-GCM nonce (base64)
	Ciphertext string `json:"ciphertext"` // Encrypted kubeconfig (base64)
}

// encryptResponse encrypts the kubeconfig using ECIES (X25519 + HKDF + AES-256-GCM).
// Returns an EncryptedResponse struct (not []byte) so the framework serializes it correctly.
func encryptResponse(callerPubKeyB64 string, plaintext []byte) (any, error) {
	// Decode caller's public key.
	callerPubKeyBytes, err := base64.StdEncoding.DecodeString(callerPubKeyB64)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode caller public key")
	}

	callerPubKey, err := ecdh.X25519().NewPublicKey(callerPubKeyBytes)
	if err != nil {
		return nil, errors.Wrap(err, "invalid caller public key")
	}

	// Generate ephemeral keypair.
	ephemeralPrivKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate ephemeral key")
	}

	// ECDH to derive shared secret.
	sharedSecret, err := ephemeralPrivKey.ECDH(callerPubKey)
	if err != nil {
		return nil, errors.Wrap(err, "ECDH failed")
	}

	// Derive encryption key via HKDF.
	hkdfReader := hkdf.New(sha256.New, sharedSecret, []byte(hkdfSalt), nil)
	aesKey := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, aesKey); err != nil {
		return nil, errors.Wrap(err, "HKDF key derivation failed")
	}

	// Encrypt with AES-256-GCM.
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create AES cipher")
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create GCM")
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, errors.Wrap(err, "failed to generate nonce")
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// Return struct directly — the framework handles JSON serialization.
	// Returning []byte would cause double-encoding ([]byte → base64 string by json.Marshal).
	return &EncryptedResponse{
		PublicKey:  base64.StdEncoding.EncodeToString(ephemeralPrivKey.PublicKey().Bytes()),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}, nil
}

// DecryptResponse decrypts an EncryptedResponse using the caller's private key.
// This is used by the CLI plugin.
func DecryptResponse(privateKey *ecdh.PrivateKey, encryptedJSON []byte) ([]byte, error) {
	var resp EncryptedResponse
	if err := json.Unmarshal(encryptedJSON, &resp); err != nil {
		return nil, errors.Wrap(err, "failed to parse encrypted response")
	}

	// Decode worker's public key.
	workerPubKeyBytes, err := base64.StdEncoding.DecodeString(resp.PublicKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode worker public key")
	}

	workerPubKey, err := ecdh.X25519().NewPublicKey(workerPubKeyBytes)
	if err != nil {
		return nil, errors.Wrap(err, "invalid worker public key")
	}

	// ECDH to derive shared secret.
	sharedSecret, err := privateKey.ECDH(workerPubKey)
	if err != nil {
		return nil, errors.Wrap(err, "ECDH failed")
	}

	// Derive encryption key via HKDF (same as encrypt side).
	hkdfReader := hkdf.New(sha256.New, sharedSecret, []byte(hkdfSalt), nil)
	aesKey := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, aesKey); err != nil {
		return nil, errors.Wrap(err, "HKDF key derivation failed")
	}

	// Decrypt.
	nonce, err := base64.StdEncoding.DecodeString(resp.Nonce)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode nonce")
	}

	ciphertext, err := base64.StdEncoding.DecodeString(resp.Ciphertext)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode ciphertext")
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create AES cipher")
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create GCM")
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.Wrap(err, "decryption failed")
	}

	return plaintext, nil
}
