// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/fluxcd/pkg/oci"
	"github.com/fluxcd/pkg/oci/client"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	corev1 "k8s.io/api/core/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlConfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	AuthMethodKubernetes   = "kubernetes"
	AuthMethodCloud        = "cloud"
	AuthMethodDockerConfig = "docker-config"
	AuthMethodKeychain     = "keychain"
)

// DockerConfig represents the structure of a Docker config.json file
type DockerConfig struct {
	Auths map[string]DockerAuth `json:"auths"`
}

// DockerAuth represents the auth configuration for a registry
type DockerAuth struct {
	Auth     string `json:"auth"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type OCIClient interface {
	LoginWithCredentials(cred string) error
	LoginWithProvider(ctx context.Context, url string, provider oci.Provider) error
	Delete(ctx context.Context, url string) error
	GetOptions() []crane.Option
}

type RealOCIClient struct {
	client *client.Client
}

func NewRealOCIClient() OCIClient {
	return &RealOCIClient{
		client: client.NewClient(client.DefaultOptions()),
	}
}

func (r *RealOCIClient) LoginWithCredentials(cred string) error {
	return r.client.LoginWithCredentials(cred)
}

func (r *RealOCIClient) LoginWithProvider(ctx context.Context, url string, provider oci.Provider) error {
	return r.client.LoginWithProvider(ctx, url, provider)
}

func (r *RealOCIClient) Delete(ctx context.Context, url string) error {
	return r.client.Delete(ctx, url)
}

func (r *RealOCIClient) GetOptions() []crane.Option {
	return r.client.GetOptions()
}

type NewClientFunc func() OCIClient

type FluxOCIWorkerConfig struct {
	InCluster                   bool
	AuthMethod                  string
	KubernetesSecretPath        string
	KubernetesSecretCredentials string
}

func NewFluxOCIWorkerConfig(worker *FluxOCIWorker, inCluster bool, authMethod, k8sSecretPath string) error {
	creds := ""
	if authMethod == AuthMethodKubernetes && k8sSecretPath != "" {
		var err error
		creds, err = validateK8sSecretPath(k8sSecretPath)
		if err != nil {
			return fmt.Errorf("invalid Kubernetes secret path: %w", err)
		}
	}
	worker.Config = &FluxOCIWorkerConfig{
		InCluster:                   inCluster,
		AuthMethod:                  authMethod,
		KubernetesSecretPath:        k8sSecretPath,
		KubernetesSecretCredentials: creds,
	}
	return nil
}

func validateK8sSecretPath(k8sSecretPath string) (string, error) {
	// Check for `.dockerconfigjson` file
	dockerConfigJSONPath := filepath.Join(k8sSecretPath, ".dockerconfigjson")
	if _, err := os.Stat(dockerConfigJSONPath); err == nil {
		dockerConfigJSON, err := os.ReadFile(dockerConfigJSONPath)
		if err != nil {
			return "", fmt.Errorf("failed to read .dockerconfigjson from Kubernetes secret path: %s, error: %w", dockerConfigJSONPath, err)
		}

		// corev1.Secret object with `.dockerconfigjson` for reuse
		secret := corev1.Secret{
			Data: map[string][]byte{
				".dockerconfigjson": dockerConfigJSON,
			},
		}

		// reuse to parse credentials
		return ExtractCredentialsFromSecret(secret), nil
	}

	// Fallback to `username` and `password` files
	usernamePath := filepath.Join(k8sSecretPath, "username")
	passwordPath := filepath.Join(k8sSecretPath, "password")

	username, err := os.ReadFile(usernamePath)
	if err != nil {
		return "", fmt.Errorf("failed to read username from Kubernetes secret path: %s, error: %w", usernamePath, err)
	}

	password, err := os.ReadFile(passwordPath)
	if err != nil {
		return "", fmt.Errorf("failed to read password from Kubernetes secret path: %s, error: %w", passwordPath, err)
	}

	trimmedUsername := strings.TrimSpace(string(username))
	trimmedPassword := strings.TrimSpace(string(password))

	if trimmedUsername == "" || trimmedPassword == "" {
		return "", fmt.Errorf("username or password is empty in Kubernetes secret path: %s", k8sSecretPath)
	}

	return fmt.Sprintf("%s:%s", trimmedUsername, trimmedPassword), nil
}

// GetDockerConfigCredentials attempts to find credentials for a given repository
// by inspecting the local Docker config file (~/.docker/config.json or DOCKER_CONFIG).
func GetDockerConfigCredentials(repository string) string {
	// 1. Locate the Docker config.json
	var configFile string
	dockerConfig := os.Getenv("DOCKER_CONFIG")
	if dockerConfig != "" {
		configFile = filepath.Join(dockerConfig, "config.json")
	} else {
		// On macOS, Docker config is in ~/Library/Containers/com.docker.docker/Data/docker.json
		// On other platforms, it's in ~/.docker/config.json
		home := os.Getenv("HOME")
		if runtime.GOOS == "darwin" {
			macConfigFile := filepath.Join(home, "Library/Containers/com.docker.docker/Data/docker.json")
			if _, err := os.Stat(macConfigFile); err == nil {
				configFile = macConfigFile
			} else {
				// Fall back to ~/.docker/config.json
				configFile = filepath.Join(home, ".docker/config.json")
			}
		} else {
			configFile = filepath.Join(home, ".docker/config.json")
		}
	}

	// 2. Check if config file exists
	if _, err := os.Stat(configFile); err != nil {
		return ""
	}

	// 3. Read and parse config.json
	data, err := os.ReadFile(configFile)
	if err != nil {
		return ""
	}
	var config DockerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return ""
	}

	// 4. Parse registry from "repository" (which might look like "my-registry.com/namespace/image:tag")
	//    We only need the first slash-part to determine the registry domain.
	parts := strings.SplitN(repository, "/", 2)
	registry := parts[0]
	// If there's no dot or colon, assume it's Docker Hub (e.g. "busybox" or "library/busybox").
	if !strings.Contains(registry, ".") && !strings.Contains(registry, ":") {
		registry = "docker.io"
	}

	// 5. Check if it appears to be Docker Hub in any known naming forms
	dockerHubAliases := []string{
		"docker.io",
		"index.docker.io",
		"registry-1.docker.io",
		"registry.hub.docker.com",
		"https://index.docker.io/v1/",
	}

	isDockerHub := false
	// A quick check: If the parsed registry is exactly one of these known hub aliases, treat it as Docker Hub.
	// (Though you can make this logic even more lenient if you want to treat partial matches as well.)
	for _, alias := range dockerHubAliases {
		if registry == alias {
			isDockerHub = true
			break
		}
	}

	// Another heuristic: If the user has no domain (already caught above) or ".docker.io" is in the string,
	// we also treat it as Docker Hub:
	if strings.Contains(registry, "docker.io") {
		isDockerHub = true
	}

	// 6. If it's Docker Hub, try all known aliases. Return on the first match that works.
	if isDockerHub {
		for _, alias := range dockerHubAliases {
			if cred := TryAuth(config.Auths, alias); cred != "" {
				return cred
			}
		}
		// If none of the known aliases matched, return empty.
		return ""
	}

	// 7. Otherwise, for non-Docker-Hub, we try:
	//    - exact registry string
	//    - "https://<registry>"
	// You could extend this to try "http://" or other variants if your environment requires it.
	if cred := TryAuth(config.Auths, registry); cred != "" {
		return cred
	}
	if cred := TryAuth(config.Auths, "https://"+registry); cred != "" {
		return cred
	}

	// 8. If still nothing, return empty string
	return ""
}

// Helper to look up an auth entry in the map and base64-decode it
func TryAuth(auths map[string]DockerAuth, key string) string {
	if entry, ok := auths[key]; ok {
		if decoded, err := base64.StdEncoding.DecodeString(entry.Auth); err == nil {
			return string(decoded)
		}
	}
	return ""
}

// LoginToRegistry attempts registry authentication in multiple ways:
// 0) Kubernetes secret (if specified)
// 1) DefaultKeychain (system keychain & credential helpers)
// 2) Docker config.json base64 auth
// 3) Cloud-native provider if specified
func LoginToRegistry(ctx context.Context, workerConfig *FluxOCIWorkerConfig, params *FluxOCIParams, newClientFunc NewClientFunc) (OCIClient, error) {
	var cred string
	var provider oci.Provider

	// 1. Attempt cloud provider authentication if specified
	// Currently supported providers: AWS, Azure, GCP
	if !slices.Contains([]string{"", ProviderGeneric, ProviderNone}, params.Provider) {
		provider = GetCloudProvider(params.Provider)
		url := params.Repository + ":" + params.Tag
		cli := newClientFunc()
		if err := cli.LoginWithProvider(ctx, url, provider); err == nil {
			return cli, nil
		}
		log.Log.Info("Cloud provider authentication failed, falling back", "provider", params.Provider)
	}

	// 2. Attempt Kubernetes secret credentials
	if params.KubernetesSecretName != "" && params.KubernetesSecretNamespace != "" {
		cred = GetK8sSecretCredentials(ctx, params)
		if cred != "" {
			cli := newClientFunc()
			if err := cli.LoginWithCredentials(cred); err == nil {
				return cli, nil
			}
			log.Log.Info("Kubernetes secret name and namespace authentication failed, falling back",
				"secretName", params.KubernetesSecretName,
				"namespace", params.KubernetesSecretNamespace)
		} else {
			log.Log.Info("Failed to load Kubernetes secret credentials from params, falling back")
		}
	}

	// 3. Attempt workerConfig.AuthMethod
	switch workerConfig.AuthMethod {
	case AuthMethodKubernetes:
		if workerConfig.KubernetesSecretCredentials != "" {
			cred = workerConfig.KubernetesSecretCredentials
		} else {
			cfg, err := rest.InClusterConfig()
			if err != nil {
				log.Log.Info("Failed to load in-cluster configuration", "error", err.Error())
				break
			}

			k8sClient, err := ctrlclient.New(cfg, ctrlclient.Options{})
			if err != nil {
				log.Log.Info("Failed to create Kubernetes client", "error", err.Error())
				break
			}
			cred = GetCredentialsFromImagePullSecrets(ctx, k8sClient)
		}
		if cred != "" {
			cli := newClientFunc()
			if err := cli.LoginWithCredentials(cred); err == nil {
				return cli, nil
			}
		}
	case AuthMethodCloud:
		provider = GetCloudProvider(params.Provider)
		url := params.Repository + ":" + params.Tag
		cli := newClientFunc()
		if err := cli.LoginWithProvider(ctx, url, provider); err == nil {
			return cli, nil
		}
	case AuthMethodDockerConfig:
		cred = GetDockerConfigCredentials(params.Repository)
		if cred != "" {
			cli := newClientFunc()
			if err := cli.LoginWithCredentials(cred); err == nil {
				return cli, nil
			}
		}
	default:
		cred = GetDefaultKeychainCredentials(params, authn.DefaultKeychain)
		if cred != "" {
			cli := newClientFunc()
			if err := cli.LoginWithCredentials(cred); err == nil {
				return cli, nil
			}
		}
	}

	return nil, fmt.Errorf("all authentication methods failed")
}

func GetDefaultKeychainCredentials(params *FluxOCIParams, keychain authn.Keychain) string {
	ref, err := name.ParseReference(params.Repository + ":" + params.Tag)
	if err != nil {
		return ""
	}

	authnAuth, err := keychain.Resolve(ref.Context())
	if err != nil {
		return ""
	}
	// possible when no credentials are found in the keychain
	if authnAuth == authn.Anonymous {
		return ""
	}

	ac, authErr := authnAuth.Authorization()
	if authErr != nil {
		log.Log.Info("Keychain authentication failed", "error", authErr.Error())
		return ""
	}

	return ac.Username + ":" + ac.Password
}

func GetCloudProvider(provider string) oci.Provider {
	switch provider {
	case ProviderAWS:
		return oci.ProviderAWS
	case ProviderAzure:
		return oci.ProviderAzure
	case ProviderGCP:
		return oci.ProviderGCP
	default:
		return oci.ProviderGeneric
	}
}

func GetK8sSecretCredentials(ctx context.Context, params *FluxOCIParams) string {
	if params.KubernetesSecretName == "" || params.KubernetesSecretNamespace == "" {
		return ""
	}

	cfg, err := ctrlConfig.GetConfig()
	if err != nil {
		log.Log.Info("Kubernetes configuration retrieval failed", "error", err.Error())
		return ""
	}

	k8sClient, err := ctrlclient.New(cfg, ctrlclient.Options{})
	if err != nil {
		log.Log.Info("Kubernetes client creation failed", "error", err.Error())
		return ""
	}

	var secret corev1.Secret
	key := k8stypes.NamespacedName{Name: params.KubernetesSecretName, Namespace: params.KubernetesSecretNamespace}
	if err := k8sClient.Get(ctx, key, &secret); err != nil {
		log.Log.Info("Kubernetes secret retrieval failed", "error", err.Error())
		return ""
	}

	return ExtractCredentialsFromSecret(secret)
}

func ExtractCredentialsFromSecret(secret corev1.Secret) string {
	// Check for `.dockerconfigjson` key
	if dockerConfigJSON, ok := secret.Data[".dockerconfigjson"]; ok {
		decoded, err := base64.StdEncoding.DecodeString(string(dockerConfigJSON))
		// When mounting a secret as a volume or environment variable,
		// the kubernetes decodes the base64 string.
		if err != nil {
			log.Log.Info("Failed to base64 decode .dockerconfigjson. Attempting JSON unmarshal", "error", err.Error())
			decoded = dockerConfigJSON
		}

		var dockerConfig DockerConfig
		if err := json.Unmarshal(decoded, &dockerConfig); err != nil {
			log.Log.Info("Failed to parse .dockerconfigjson", "error", err.Error())
			return ""
		}

		// Extract credentials for the first registry found
		for _, auth := range dockerConfig.Auths {
			if auth.Username != "" && auth.Password != "" {
				return fmt.Sprintf("%s:%s", auth.Username, auth.Password)
			}
		}
	}

	// Fallback to `username` and `password` keys
	if username, usernameExists := secret.Data["username"]; usernameExists {
		if password, passwordExists := secret.Data["password"]; passwordExists {
			return fmt.Sprintf("%s:%s", string(username), string(password))
		}
	}

	log.Log.Info("No valid credentials found in Kubernetes secret", "name", secret.Name, "namespace", secret.Namespace)
	return ""
}

func GetCredentialsFromImagePullSecrets(ctx context.Context, k8sClient ctrlclient.Client) string {
	// Get the service account associated with the pod
	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		namespace = "default"
	}
	podName := os.Getenv("POD_NAME")
	if podName == "" {
		log.Log.Info("POD_NAME environment variable is not set")
		return ""
	}

	var pod corev1.Pod
	if err := k8sClient.Get(ctx, k8stypes.NamespacedName{Name: podName, Namespace: namespace}, &pod); err != nil {
		log.Log.Info("Failed to retrieve pod information", "error", err.Error())
		return ""
	}

	serviceAccountName := pod.Spec.ServiceAccountName
	if serviceAccountName == "" {
		serviceAccountName = "default"
	}

	var serviceAccount corev1.ServiceAccount
	if err := k8sClient.Get(ctx, k8stypes.NamespacedName{Name: serviceAccountName, Namespace: namespace}, &serviceAccount); err != nil {
		log.Log.Info("Failed to retrieve service account", "error", err.Error())
		return ""
	}

	// Iterate over imagePullSecrets and extract credentials
	for _, pullSecret := range serviceAccount.ImagePullSecrets {
		var secret corev1.Secret
		if err := k8sClient.Get(ctx, k8stypes.NamespacedName{Name: pullSecret.Name, Namespace: namespace}, &secret); err != nil {
			log.Log.Info("Failed to retrieve imagePullSecret", "secretName", pullSecret.Name, "error", err.Error())
			continue
		}

		cred := ExtractCredentialsFromSecret(secret)
		if cred != "" {
			return cred
		}
	}

	log.Log.Info("No valid credentials found in imagePullSecrets")
	return ""
}
