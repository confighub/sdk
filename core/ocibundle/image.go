// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package ocibundle

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
)

// ReadImageFile reads one file from a container image without running it, such
// as configuration an image ships beside its binary. Layers are read from the
// newest down, and the first that holds the file wins, as in the image's
// filesystem, so an image that adds the file in its last layer costs one small
// fetch. An image index is read through its first platform's image, so the file
// has to be the same on every platform. It returns the file and the digest of
// the manifest the reference resolved to. opts.Include is ignored.
func ReadImageFile(ctx context.Context, rawRef, filePath string, opts Options) ([]byte, string, error) {
	want, ok := cleanPath(filePath)
	if !ok {
		return nil, "", fmt.Errorf("invalid path %q", filePath)
	}
	repo, err := newRepository(rawRef, opts)
	if err != nil {
		return nil, "", err
	}
	reference := repo.Reference.Reference
	if reference == "" {
		reference = "latest"
	}
	fetch := oras.FetchBytesOptions{MaxBytes: opts.Limits.MaxManifestBytes}
	desc, manifestBytes, err := oras.FetchBytes(ctx, repo, reference, fetch)
	if err != nil {
		return nil, "", fmt.Errorf("fetch OCI manifest %q: %w", rawRef, err)
	}
	resolved := desc.Digest.String()
	if isIndex(desc.MediaType) {
		platform, err := firstPlatform(manifestBytes)
		if err != nil {
			return nil, "", fmt.Errorf("%q: %w", rawRef, err)
		}
		if _, manifestBytes, err = oras.FetchBytes(ctx, repo, platform.Digest.String(), fetch); err != nil {
			return nil, "", fmt.Errorf("fetch OCI manifest %q for %s/%s: %w", rawRef, platform.Platform.OS, platform.Platform.Architecture, err)
		}
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, "", fmt.Errorf("parse OCI manifest %q: %w", rawRef, err)
	}

	opts.Include = func(name string) bool { return name == want }
	for i := len(manifest.Layers) - 1; i >= 0; i-- {
		layer := manifest.Layers[i]
		if opts.Limits.MaxLayerBytes > 0 && layer.Size > opts.Limits.MaxLayerBytes {
			return nil, "", fmt.Errorf("OCI layer %s is %d bytes, more than the %d allowed", layer.Digest, layer.Size, opts.Limits.MaxLayerBytes)
		}
		blob, err := content.FetchAll(ctx, repo, layer)
		if err != nil {
			return nil, "", fmt.Errorf("fetch OCI layer %s: %w", layer.Digest, err)
		}
		x := &extractor{opts: opts}
		if err := x.layer(i, layer, blob); err != nil {
			return nil, "", fmt.Errorf("extract OCI layer %s: %w", layer.Digest, err)
		}
		if len(x.files) > 0 {
			return x.files[0].Content, resolved, nil
		}
	}
	return nil, "", fmt.Errorf("image %q has no file %s", rawRef, want)
}

func isIndex(mediaType string) bool {
	return mediaType == ocispec.MediaTypeImageIndex || mediaType == "application/vnd.docker.distribution.manifest.list.v2+json"
}

// firstPlatform returns the first image of an index that is for a platform,
// passing over the attestation manifests a build adds beside them, which have
// none.
func firstPlatform(indexBytes []byte) (ocispec.Descriptor, error) {
	var index ocispec.Index
	if err := json.Unmarshal(indexBytes, &index); err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("parse OCI index: %w", err)
	}
	for _, m := range index.Manifests {
		if m.Platform == nil || m.Platform.OS == "" || m.Platform.OS == "unknown" {
			continue
		}
		return m, nil
	}
	return ocispec.Descriptor{}, fmt.Errorf("the index has no image for a platform")
}

// NewestRelease returns the newest tag of a repository that names a release:
// X.Y.Z, with or without a leading "v". Other tags, such as latest or a
// pre-release, are passed over. rawRepo names the repository, without a tag.
func NewestRelease(ctx context.Context, rawRepo string, opts Options) (string, error) {
	repo, err := newRepository(rawRepo, opts)
	if err != nil {
		return "", err
	}
	if repo.Reference.Reference != "" {
		return "", fmt.Errorf("%q names a tag or digest; expected a repository", rawRepo)
	}
	newest, newestVersion := "", [3]int{}
	err = repo.Tags(ctx, "", func(tags []string) error {
		for _, tag := range tags {
			v, ok := releaseVersion(tag)
			if ok && (newest == "" || compareVersions(v, newestVersion) > 0) {
				newest, newestVersion = tag, v
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("list the tags of %q: %w", rawRepo, err)
	}
	if newest == "" {
		return "", fmt.Errorf("%q has no release tag (X.Y.Z or vX.Y.Z)", rawRepo)
	}
	return newest, nil
}

// releaseVersion parses a release tag, X.Y.Z or vX.Y.Z.
func releaseVersion(tag string) ([3]int, bool) {
	parts := strings.Split(strings.TrimPrefix(tag, "v"), ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var v [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p != strconv.Itoa(n) {
			return [3]int{}, false
		}
		v[i] = n
	}
	return v, true
}

func compareVersions(a, b [3]int) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
