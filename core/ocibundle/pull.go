// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package ocibundle pulls a bundle of configuration files from an OCI registry.
// It is shared by the clients that pull on the user's machine and by the server,
// which pulls on the caller's behalf; the server passes the limits and the
// restricted HTTP client from [NewPublicHTTPClient].
package ocibundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// Scheme prefixes an OCI reference given where a file or directory could be.
const Scheme = "oci://"

// IsRef reports whether an input is an OCI reference rather than a file,
// directory, or "-".
func IsRef(input string) bool {
	return strings.HasPrefix(input, Scheme)
}

// Credentials authenticate a pull. Registries that issue access tokens take the
// token as the password.
type Credentials struct {
	Username string
	Password string
}

// Limits bound what a pull reads. A zero field is unlimited.
type Limits struct {
	MaxManifestBytes int64
	MaxLayers        int
	// MaxLayerBytes bounds each layer as stored, before decompression.
	MaxLayerBytes int64
	// MaxFiles and MaxTotalBytes bound the files extracted, after decompression,
	// so a small compressed layer cannot expand without bound.
	MaxFiles      int
	MaxTotalBytes int64
}

// Options configure a pull.
type Options struct {
	// Credentials are used for this pull only. Nil pulls anonymously.
	Credentials *Credentials
	UserAgent   string
	// HTTPClient carries every request, including token and redirect requests.
	// Nil uses a client that retries transient failures.
	HTTPClient *http.Client
	// PlainHTTPLoopback pulls from a loopback registry over plain HTTP, as local
	// development registries are served.
	PlainHTTPLoopback bool
	// Include selects the files to extract by path. Nil extracts every file.
	Include func(path string) bool
	Limits  Limits
}

// File is one extracted file, with its path within the bundle.
type File struct {
	Path    string
	Content []byte
}

// Bundle is what a pull extracted.
type Bundle struct {
	// Digest is the digest of the manifest the reference resolved to. Pulling
	// Ref@Digest later reads exactly the same bundle.
	Digest string
	Files  []File
}

// Pull fetches an OCI artifact and extracts the files it carries. The artifact
// may carry them as a tar or tar+gzip layer (as `cub release publish` and Flux
// produce) or as individual file layers (as `oras push <files>` produces), so any
// bundle a GitOps toolchain can consume works here. A non-empty digest pulls that
// manifest instead of the one the reference's tag names now.
func Pull(ctx context.Context, rawRef, digest string, opts Options) (*Bundle, error) {
	repo, err := remote.NewRepository(strings.TrimPrefix(rawRef, Scheme))
	if err != nil {
		return nil, fmt.Errorf("invalid OCI reference %q: %w", rawRef, err)
	}
	client := &auth.Client{
		Client: opts.HTTPClient,
		Cache:  auth.NewCache(),
	}
	if client.Client == nil {
		client.Client = retry.DefaultClient
	}
	if opts.UserAgent != "" {
		client.SetUserAgent(opts.UserAgent)
	}
	if opts.Credentials != nil {
		client.Credential = auth.StaticCredential(repo.Reference.Registry, auth.Credential{
			Username: opts.Credentials.Username,
			Password: opts.Credentials.Password,
		})
	}
	repo.Client = client
	if opts.PlainHTTPLoopback && isLoopbackRegistry(repo.Reference.Registry) {
		repo.PlainHTTP = true
	}
	reference := repo.Reference.Reference
	if digest != "" {
		reference = digest
	} else if reference == "" {
		reference = "latest"
	}

	manifestDesc, manifestBytes, err := oras.FetchBytes(ctx, repo, reference, oras.FetchBytesOptions{
		MaxBytes: opts.Limits.MaxManifestBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch OCI manifest %q: %w", rawRef, err)
	}
	if manifestDesc.MediaType == ocispec.MediaTypeImageIndex ||
		manifestDesc.MediaType == "application/vnd.docker.distribution.manifest.list.v2+json" {
		return nil, fmt.Errorf("%q resolves to a multi-manifest index; expected a single image manifest holding configuration files", rawRef)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("parse OCI manifest %q: %w", rawRef, err)
	}
	if opts.Limits.MaxLayers > 0 && len(manifest.Layers) > opts.Limits.MaxLayers {
		return nil, fmt.Errorf("OCI artifact %q has %d layers, more than the %d allowed", rawRef, len(manifest.Layers), opts.Limits.MaxLayers)
	}

	x := &extractor{opts: opts}
	for i, layer := range manifest.Layers {
		if opts.Limits.MaxLayerBytes > 0 && layer.Size > opts.Limits.MaxLayerBytes {
			return nil, fmt.Errorf("OCI layer %s is %d bytes, more than the %d allowed", layer.Digest, layer.Size, opts.Limits.MaxLayerBytes)
		}
		// FetchAll reads exactly the descriptor's size and verifies the digest, so a
		// registry cannot send more than the manifest declared.
		blob, err := content.FetchAll(ctx, repo, layer)
		if err != nil {
			return nil, fmt.Errorf("fetch OCI layer %s: %w", layer.Digest, err)
		}
		if err := x.layer(i, layer, blob); err != nil {
			return nil, fmt.Errorf("extract OCI layer %s: %w", layer.Digest, err)
		}
	}
	if len(x.files) == 0 {
		return nil, fmt.Errorf("no configuration files found in OCI artifact %q", rawRef)
	}
	return &Bundle{Digest: manifestDesc.Digest.String(), Files: x.files}, nil
}

// extractor accumulates the files of every layer against the pull's limits.
type extractor struct {
	opts  Options
	files []File
	total int64
}

// layer extracts one layer blob. A gzip-compressed blob is decompressed first; a
// tar archive is unpacked; anything else is a single file named by its title
// annotation.
func (x *extractor) layer(index int, layer ocispec.Descriptor, blob []byte) error {
	if len(blob) >= 2 && blob[0] == 0x1f && blob[1] == 0x8b {
		gz, err := gzip.NewReader(bytes.NewReader(blob))
		if err != nil {
			return fmt.Errorf("gunzip layer: %w", err)
		}
		defer gz.Close()
		un, err := x.readBounded(gz)
		if err != nil {
			return fmt.Errorf("gunzip layer: %w", err)
		}
		blob = un
	}

	if looksLikeTar(blob) {
		return x.tar(blob)
	}

	// A file layer without a title, as some tools push, is assumed to be YAML so
	// that a caller selecting configuration files still sees it.
	name := layer.Annotations[ocispec.AnnotationTitle]
	if name == "" {
		name = fmt.Sprintf("layer-%d.yaml", index)
	}
	return x.add(name, blob)
}

// tar adds the regular files of a tar archive.
func (x *extractor) tar(data []byte) error {
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name, ok := cleanPath(hdr.Name)
		if !ok || (x.opts.Include != nil && !x.opts.Include(name)) {
			continue
		}
		buf, err := x.readBounded(tr)
		if err != nil {
			return fmt.Errorf("read tar entry %s: %w", hdr.Name, err)
		}
		if err := x.add(name, buf); err != nil {
			return err
		}
	}
}

// add records a file, unless Include leaves it out.
func (x *extractor) add(name string, data []byte) error {
	name, ok := cleanPath(name)
	if !ok || (x.opts.Include != nil && !x.opts.Include(name)) {
		return nil
	}
	if x.opts.Limits.MaxFiles > 0 && len(x.files) >= x.opts.Limits.MaxFiles {
		return fmt.Errorf("the bundle has more than the %d files allowed", x.opts.Limits.MaxFiles)
	}
	x.total += int64(len(data))
	if x.opts.Limits.MaxTotalBytes > 0 && x.total > x.opts.Limits.MaxTotalBytes {
		return fmt.Errorf("the bundle's files exceed the %d bytes allowed", x.opts.Limits.MaxTotalBytes)
	}
	x.files = append(x.files, File{Path: name, Content: data})
	return nil
}

// readBounded reads r, refusing to read past what remains of MaxTotalBytes.
func (x *extractor) readBounded(r io.Reader) ([]byte, error) {
	limit := x.opts.Limits.MaxTotalBytes
	if limit <= 0 {
		return io.ReadAll(r)
	}
	remaining := limit - x.total
	data, err := io.ReadAll(io.LimitReader(r, remaining+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > remaining {
		return nil, fmt.Errorf("the bundle's files exceed the %d bytes allowed", limit)
	}
	return data, nil
}

// cleanPath makes an archive path relative to the bundle root, so it can never
// name a location outside the bundle.
func cleanPath(name string) (string, bool) {
	rel := strings.TrimPrefix(path.Clean("/"+name), "/")
	return rel, rel != "" && rel != "."
}

// looksLikeTar reports whether data begins with a POSIX tar header (the "ustar"
// magic at offset 257).
func looksLikeTar(data []byte) bool {
	return len(data) > 262 && string(data[257:262]) == "ustar"
}

// isLoopbackRegistry reports whether a registry host is loopback.
func isLoopbackRegistry(registry string) bool {
	host := registry
	if h, _, ok := strings.Cut(registry, ":"); ok {
		host = h
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
