# FluxOCIWorker Authentication

The `FluxOCIWorker` is a specialized Bridge Worker designed to interact with OCI-compatible container registries. It supports multiple authentication mechanisms to ensure compatibility with various registry configurations and security requirements. This document outlines the key differences of the `FluxOCIWorker` authentication mechanisms and examples of how to run the worker.

---

## Confighub Target configuration

Confighub expects certain parameters to be set for a Target for FluxOCI. Based on the parameters set, Confighub will automatically handle specific Repository creation and updating in your OCI.

```bash
cub target create my-target-name --provider FluxOCIWriter \
"{\"Repository\":\"my-registry.io/company-name\",\"Tag\":\"\",\"Provider\":\"Generic\", \"AllowDeletion\":\"false\"}" \
bridge-worker-name-optional
```

### Target Parameters

* `Repository`: Expects the OCI hosting service hostname and Repository Prefix. e.g. `ghcr.io/company-1`, `https://123456789012.dkr.ecr.us-east-1.amazonaws.com/org-2`
* `Tag` (Optional): Confighub will automatically set the tag on each push using the Unit's current RevisionNum being applied. This will have `rev` prefixed to the number, like `rev42`. When Tag is populated, it can be used to explicitly set a value that you want Confighub to publish to the OCI in addition to the default RevisionNum tag. For example `latest` or `trunk`.
* `Provider`: A value dictating how we authenticate to your OCI. Supported values are:
  * `Generic`: Expects either the worker or the Target parameters to have a secret to use
  * `AWS`: AWS IAM profile authentication
  * `GCP`: GCP Service Account associated to the Pod/Cluser
  * `Azure`: Azure Service Principal associated to the Pod/Cluser
  * `None`: meaning however the worker authenticates
* Kubernetes Secret via `KubernetesSecretName` and `KubernetesSecretNamespace` (optional): If populated, the Confighub Worker will attempt to load the kubernetes secret specified and use those credentials as authentication material.

## Authentication Mechanisms

OCI-compatible registries require authentication to push or pull container images. The `FluxOCIWorker` supports multiple authentication mechanisms to handle different use cases:

1. **Keychains:**
   - Used for system-level credential management.
   - Allows seamless integration with tools like Docker and Kubernetes.
   - Example: [Docker Login](https://docs.docker.com/reference/cli/docker/login/).

2. **Docker-Secret Format:**
   - Kubernetes-native format for storing registry credentials.
   - Commonly used with `kubectl create secret docker-registry` or `flux create secret`.
   - Example: [Flux Source Secret](https://github.com/fluxcd/flux2/blob/main/pkg/manifestgen/sourcesecret/sourcesecret.go#L252).

3. **Environment Variables:**
   - Directly pass credentials via environment variables for simplicity in local or CI/CD setups.

### 1. Keychains

Keychains are used for system-level credential management. The `FluxOCIWorker` leverages the `authn.Keychain` interface to resolve credentials for a given registry.

#### Example: Docker Login

To authenticate with a registry using Docker, run:

```bash
docker login ghcr.io -u <username> -p <password>
```

This stores the credentials in the Docker keychain, which the `FluxOCIWorker` can access.

---

### 2. Docker-Secret Format

The `FluxOCIWorker` supports the Kubernetes `docker-registry` secret format. This format is commonly used with `kubectl create secret docker-registry` or `flux create secret`.

#### Example: Creating a Docker-Registry Secret

```bash
kubectl create secret docker-registry my-docker-secret \
  --docker-server=ghcr.io \
  --docker-username=<username> \
  --docker-password=<password> \
  --docker-email=<email>
```

The secret will contain a `.dockerconfigjson` key with the following structure:

```json
{
  "auths": {
    "ghcr.io": {
      "username": "user",
      "password": "password",
      "auth": "dXNlcjpwYXNzd29yZAo="
    }
  }
}
```

The `FluxOCIWorker` will decode and parse this format to extract the credentials.

#### Example: Using Docker-Registry Secret

1. Create a Kubernetes secret as shown above.
2. Run the worker with the secret path:

```bash
cub worker run flux-oci-worker -t flux-oci-writer \
-e KUBERNETES_SECRET_PATH=/path/to/secret
-e AUTH_METHOD=k8s
```

Where the `/path/to/secret` is the location the `volumeMount` is placed on the Kubernetes pod.
The worker will read the `.dockerconfigjson` file or fallback to `username` and `password` files. If
`KUBERNETES_SECRET_PATH` is not specified, it will attempt to fall back to the kubernetes secret name and namespace in the Confighub Target available for acquiring credentials.

#### Example: Using Keychains

```bash
cub worker run flux-oci-worker -t flux-oci-writer
```

The worker will automatically resolve credentials from the system keychain.

#### Example: Using Docker-Config Environment Variable

1. Login to your registry via `docker login` shown above.
2. Run the worker with the `docker-config` authentication method:

```bash
export CONFIGHUB_WORKER_ID=flux-oci-worker
export CONFIGHUB_WORKER_SECRET=<worker-secret>
export DOCKER_CONFIG=$HOME/.docker/config.json

cub worker run flux-oci-worker -t flux-oci-writer \
-e AUTH_METHOD=docker-config
```

The worker will read the `$HOME/.docker/config.json` file or fallback to `username` and `password` files
unless a different path is specified by the `DOCKER_CONFIG` environment variable.

## References

- [Docker Login Documentation](https://docs.docker.com/reference/cli/docker/login/)
- [Flux Source Secret Implementation](https://github.com/fluxcd/flux2/blob/main/pkg/manifestgen/sourcesecret/sourcesecret.go#L252)
- [Flux Push Artifact Implementation](https://github.com/fluxcd/flux2/blob/main/cmd/flux/push_artifact.go#L217-L265)
