# ConfigHub SDK

1. [Function development guide](./function-impl/README.md)
2. [Bridge development guide](./examples/hello-world-bridge/README.md)
3. Go client library is in ./core/openapi/goclient-new
4. OpenAPI spec is in ./core/openapi

Find other links to developer documentation on [our documentation site](https://docs.confighub.com/developer/overview/).

## Release assets

Each [release](https://github.com/confighub/sdk/releases) publishes, next to the `cub` and `cub-worker-run` binaries:

- `THIRD_PARTY_LICENSES-cub.txt` and `THIRD_PARTY_LICENSES-cub-worker-run.txt`: the third-party Go modules linked into each binary, with their license texts.
- `<binary>-<os>-<arch>.spdx.json`: an SPDX software bill of materials generated from each published binary.

The release fails if a dependency with a forbidden or restricted (copyleft) license is linked in. The generator is `.github/scripts/gen-third-party-licenses.sh`.
