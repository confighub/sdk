// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

const ociScheme = "oci://"

// isOCIRef reports whether an upload input is an OCI reference rather than a
// file, directory, or "-".
func isOCIRef(input string) bool {
	return strings.HasPrefix(input, ociScheme)
}

// ociAuthClient builds an anonymous auth client. Local Docker/registry
// credentials (e.g. a prior `docker login ghcr.io`) are deliberately NOT used:
// ghcr.io hard-denies (403) a request presenting an expired or revoked
// credential even for public packages, so an expired local login would break
// pulls — differently on every machine — that work fine with no credentials.
// Config bundles are public for now; if private-bundle support is added later,
// credential use must be an explicit opt-in, not an ambient default.
func ociAuthClient() *auth.Client {
	client := &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.NewCache(),
	}
	client.SetUserAgent("cub/" + Version)
	return client
}

// pullOCIManifests pulls an OCI artifact and writes the Kubernetes YAML it
// carries into destDir, returning the resolved manifest digest. The artifact may
// carry its YAML as a single tar or tar+gzip layer (as `cub release publish` and
// Flux produce) or as individual file layers (as `oras push <files>` produces);
// both are accepted, so any bundle a GitOps toolchain can consume works here.
// Pulls are always anonymous (see ociAuthClient).
func pullOCIManifests(ctx context.Context, rawRef, destDir string) (string, error) {
	repo, err := remote.NewRepository(strings.TrimPrefix(rawRef, ociScheme))
	if err != nil {
		return "", fmt.Errorf("invalid OCI reference %q: %w", rawRef, err)
	}
	repo.Client = ociAuthClient()
	if isLocalRegistry(repo.Reference.Registry) {
		repo.PlainHTTP = true
	}
	reference := repo.Reference.Reference
	if reference == "" {
		reference = "latest"
	}

	manifestDesc, manifestBytes, err := oras.FetchBytes(ctx, repo, reference, oras.FetchBytesOptions{})
	if err != nil {
		return "", fmt.Errorf("fetch OCI manifest %q: %w", rawRef, err)
	}
	if manifestDesc.MediaType == ocispec.MediaTypeImageIndex ||
		manifestDesc.MediaType == "application/vnd.docker.distribution.manifest.list.v2+json" {
		return "", fmt.Errorf("%q resolves to a multi-manifest index; expected a single image manifest holding config YAML", rawRef)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return "", fmt.Errorf("parse OCI manifest %q: %w", rawRef, err)
	}

	wrote := 0
	for i, layer := range manifest.Layers {
		blob, err := content.FetchAll(ctx, repo, layer)
		if err != nil {
			return "", fmt.Errorf("fetch OCI layer %s: %w", layer.Digest, err)
		}
		n, err := writeYAMLFromLayer(destDir, i, layer, blob)
		if err != nil {
			return "", fmt.Errorf("extract OCI layer %s: %w", layer.Digest, err)
		}
		wrote += n
	}
	if wrote == 0 {
		return "", fmt.Errorf("no YAML manifests found in OCI artifact %q", rawRef)
	}
	return manifestDesc.Digest.String(), nil
}

// writeYAMLFromLayer extracts the YAML documents from one layer blob into
// destDir and returns how many files it wrote. A gzip-compressed blob is
// decompressed first; a tar archive is unpacked (YAML entries only); anything
// else is treated as a single manifest file named from its title annotation.
func writeYAMLFromLayer(destDir string, index int, layer ocispec.Descriptor, blob []byte) (int, error) {
	if len(blob) >= 2 && blob[0] == 0x1f && blob[1] == 0x8b {
		gz, err := gzip.NewReader(bytes.NewReader(blob))
		if err != nil {
			return 0, fmt.Errorf("gunzip layer: %w", err)
		}
		defer gz.Close()
		un, err := io.ReadAll(gz)
		if err != nil {
			return 0, fmt.Errorf("gunzip layer: %w", err)
		}
		blob = un
	}

	if looksLikeTar(blob) {
		return writeYAMLFromTar(destDir, blob)
	}

	// A single, non-archived file. Use its title annotation if it names a YAML
	// file; otherwise synthesize a name and let the parser skip non-manifests.
	name := layer.Annotations[ocispec.AnnotationTitle]
	if !isYAMLName(name) {
		name = fmt.Sprintf("layer-%d.yaml", index)
	}
	if err := writeManifestFile(destDir, name, blob); err != nil {
		return 0, err
	}
	return 1, nil
}

// looksLikeTar reports whether data begins with a POSIX tar header (the "ustar"
// magic at offset 257).
func looksLikeTar(data []byte) bool {
	return len(data) > 262 && string(data[257:262]) == "ustar"
}

// writeYAMLFromTar unpacks the YAML entries of a tar archive into destDir,
// preserving relative paths but rejecting path traversal. It returns the number
// of files written.
func writeYAMLFromTar(destDir string, data []byte) (int, error) {
	tr := tar.NewReader(bytes.NewReader(data))
	wrote := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return wrote, fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || !isYAMLName(hdr.Name) {
			continue
		}
		buf, err := io.ReadAll(tr)
		if err != nil {
			return wrote, fmt.Errorf("read tar entry %s: %w", hdr.Name, err)
		}
		if err := writeManifestFile(destDir, hdr.Name, buf); err != nil {
			return wrote, err
		}
		wrote++
	}
	return wrote, nil
}

// writeManifestFile writes content to destDir/name, sanitizing name so it can
// never escape destDir.
func writeManifestFile(destDir, name string, content []byte) error {
	rel := strings.TrimPrefix(filepath.Clean("/"+name), "/")
	path := filepath.Join(destDir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

// isYAMLName reports whether name has a .yaml or .yml extension.
func isYAMLName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml")
}

// isLocalRegistry reports whether a registry host is loopback, in which case
// the registry is served over plain HTTP (as local development registries are).
func isLocalRegistry(registry string) bool {
	host := registry
	if h, _, ok := strings.Cut(registry, ":"); ok {
		host = h
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
