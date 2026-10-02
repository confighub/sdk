# ConfigHub SDK

1. [Function development guide](./function-impl/README.md)
2. [Bridge development guide](./examples/hello-world-bridge/README.md)
3. Go client library is in ./core/openapi/goclient-new
4. OpenAPI spec is in ./core/openapi

Find other links to developer documentation on [our documentation site](https://docs.confighub.com/developer/overview/).

## Release assets

Each [release](https://github.com/confighub/sdk/releases) publishes, next to the `cub` binaries:

- `THIRD_PARTY_LICENSES-cub.txt`: the third-party Go modules linked into the binary, with their license texts.
- `cub-<os>-<arch>.spdx.json`: an SPDX software bill of materials generated from each published binary.
- `cub-<os>-<arch>.sigstore.json`: a [cosign](https://docs.sigstore.dev/cosign/) signature of each published binary, as a Sigstore bundle.
- `cub-<os>-<arch>.sbom.sigstore.json`: the same SBOM as a signed attestation about the binary, as a Sigstore bundle.

The release fails if a dependency with a forbidden or restricted (copyleft) license is linked in. The generator is `.github/scripts/gen-third-party-licenses.sh`.

### Verifying a binary

The binaries are signed in the release workflow with cosign's keyless signing: there is no ConfigHub key, and the certificate in each bundle names the workflow and the release tag that produced the binary. To verify a download with cosign v3, with the binary and its bundles in the current directory:

```
VERSION=vX.Y.Z   # the release tag
cosign verify-blob \
  --bundle cub-linux-amd64.sigstore.json \
  --certificate-identity "https://github.com/confighub/sdk/.github/workflows/cli-release.yml@refs/tags/${VERSION}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  cub-linux-amd64
```

The SBOM attestation is verified against the binary the same way. It passes only for the binary the SBOM was generated from:

```
cosign verify-blob-attestation \
  --type spdxjson \
  --bundle cub-linux-amd64.sbom.sigstore.json \
  --certificate-identity "https://github.com/confighub/sdk/.github/workflows/cli-release.yml@refs/tags/${VERSION}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  cub-linux-amd64
```

Releases up to v0.8.0 are not signed.
