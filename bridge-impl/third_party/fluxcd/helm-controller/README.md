# Flux Helm Controller - Third Party Code

This directory contains code copied from the Flux helm-controller project.

## Source

- **Repository**: https://github.com/fluxcd/helm-controller
- **License**: Apache License 2.0
- **Files copied from**: `internal/loader/`

## Copied Files

### loader/loader.go

Contains:
- `SecureLoadChartFromURL()` - Downloads a Helm chart from a URL and verifies its digest
- `NewRetryableHTTPClient()` - Creates an HTTP client with retry logic
- `copyAndVerify()` - Helper for copying and verifying data against a digest
- `overwriteHostname()` - Helper for overwriting URL hostnames (for localhost access)

## Why Copied

The loader package is in `internal/` and cannot be imported as a Go module dependency.
We need `SecureLoadChartFromURL()` to load Helm charts from source controller artifacts
exactly as Flux does, including digest verification.

## Modifications

The code has been adapted to:
- Use the `loader` package name
- Remove controller-runtime logger dependency in favor of a simpler approach
- Combine `artifact_url.go` and `client.go` into a single file

## Updates

When updating, check the original files at:
- https://github.com/fluxcd/helm-controller/blob/main/internal/loader/artifact_url.go
- https://github.com/fluxcd/helm-controller/blob/main/internal/loader/client.go
