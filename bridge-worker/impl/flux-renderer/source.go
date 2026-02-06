// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package fluxrenderer

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
)

// getTempDir returns a writable temporary directory base path.
// It checks TMPDIR environment variable first, then falls back to /tmp,
// and finally uses the current working directory if /tmp is not writable.
func getTempDir() string {
	// Check TMPDIR environment variable first
	if dir := os.Getenv("TMPDIR"); dir != "" {
		return dir
	}

	// Try /tmp
	if _, err := os.Stat("/tmp"); err == nil {
		// Check if /tmp is writable by trying to create a test file
		testFile := "/tmp/.flux-renderer-test"
		if f, err := os.Create(testFile); err == nil {
			f.Close()
			os.Remove(testFile)
			return "/tmp"
		}
	}

	// Fall back to current working directory
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}

	// Last resort
	return "."
}

// FetchArtifact downloads an artifact from URL, verifies its digest, and extracts
// it to a temporary directory. It returns the path to the temporary directory
// and a cleanup function that should be called to remove the directory when done.
func FetchArtifact(ctx context.Context, url, digest string) (string, func(), error) {
	if url == "" {
		return "", nil, fmt.Errorf("artifact URL is required")
	}

	// Create temporary directory in a writable location
	tmpBase := getTempDir()
	tmpDir, err := os.MkdirTemp(tmpBase, "flux-renderer-")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	// Create retryable HTTP client
	client := retryablehttp.NewClient()
	client.RetryMax = 3
	client.RetryWaitMin = 1 * time.Second
	client.RetryWaitMax = 5 * time.Second
	client.Logger = nil // Disable logging

	// Create request with context
	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to fetch artifact from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cleanup()
		return "", nil, fmt.Errorf("failed to fetch artifact from %s: status %d", url, resp.StatusCode)
	}

	// Create a hash writer to verify digest if provided
	var reader io.Reader = resp.Body
	var hasher hash.Hash
	if digest != "" {
		// Parse digest - expect format "sha256:xxxx"
		if !strings.HasPrefix(digest, "sha256:") {
			cleanup()
			return "", nil, fmt.Errorf("unsupported digest algorithm, expected sha256: prefix")
		}
		hasher = sha256.New()
		reader = io.TeeReader(resp.Body, hasher)
	}

	// Create a temp file for the downloaded archive in the same writable location
	tmpFile, err := os.CreateTemp(tmpBase, "artifact-*.tar.gz")
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpFilePath := tmpFile.Name()
	defer os.Remove(tmpFilePath)

	// Copy the response body to the temp file
	if _, err := io.Copy(tmpFile, reader); err != nil {
		tmpFile.Close()
		cleanup()
		return "", nil, fmt.Errorf("failed to download artifact: %w", err)
	}
	tmpFile.Close()

	// Verify digest if provided
	if hasher != nil {
		computedDigest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
		if computedDigest != digest {
			cleanup()
			return "", nil, fmt.Errorf("digest mismatch: expected %s, got %s", digest, computedDigest)
		}
	}

	// Extract the tarball
	if err := extractTarGz(tmpFilePath, tmpDir); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to extract artifact: %w", err)
	}

	return tmpDir, cleanup, nil
}

// extractTarGz extracts a .tar.gz file to the destination directory.
func extractTarGz(src, dst string) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Clean the header name and skip current directory entries
		name := filepath.Clean(header.Name)
		if name == "." {
			continue
		}

		// Sanitize the path to prevent path traversal
		target := filepath.Join(dst, name)
		cleanDst := filepath.Clean(dst)
		if !strings.HasPrefix(target, cleanDst+string(os.PathSeparator)) && target != cleanDst {
			return fmt.Errorf("illegal path in archive: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}

			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}

			// Copy with size limit to prevent decompression bombs
			const maxSize = 500 * 1024 * 1024 // 500MB per file
			if _, err := io.CopyN(f, tr, maxSize); err != nil && err != io.EOF {
				f.Close()
				return err
			}
			f.Close()

		case tar.TypeSymlink:
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
		}
	}

	return nil
}
