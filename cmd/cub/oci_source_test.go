// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const sampleDeployment = "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: app\n"
const sampleService = "apiVersion: v1\nkind: Service\nmetadata:\n  name: app\n"

func tarGzip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return gzBuf.Bytes()
}

func readAll(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		out[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestWriteYAMLFromLayer_TarGzip(t *testing.T) {
	dir := t.TempDir()
	blob := tarGzip(t, map[string]string{
		"backend.yaml":   sampleDeployment,
		"rbac/role.yaml": sampleService,
		"README.md":      "not yaml",
		"chart.tgz":      "binary-ish",
	})
	desc := ocispec.Descriptor{MediaType: ocispec.MediaTypeImageLayerGzip}

	n, err := writeYAMLFromLayer(dir, 0, desc, blob)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("wrote %d files, want 2", n)
	}
	got := readAll(t, dir)
	if got["backend.yaml"] != sampleDeployment {
		t.Errorf("backend.yaml = %q", got["backend.yaml"])
	}
	if got[filepath.Join("rbac", "role.yaml")] != sampleService {
		t.Errorf("rbac/role.yaml = %q", got[filepath.Join("rbac", "role.yaml")])
	}
	if _, ok := got["README.md"]; ok {
		t.Error("non-YAML README.md should be skipped")
	}
}

func TestWriteYAMLFromLayer_PlainTar(t *testing.T) {
	// A tar that is not gzip-compressed.
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Name: "svc.yaml", Mode: 0o644, Size: int64(len(sampleService)), Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte(sampleService))
	_ = tw.Close()

	dir := t.TempDir()
	n, err := writeYAMLFromLayer(dir, 0, ocispec.Descriptor{MediaType: "application/vnd.oci.image.layer.v1.tar"}, buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || readAll(t, dir)["svc.yaml"] != sampleService {
		t.Fatalf("plain tar not extracted: n=%d", n)
	}
}

func TestWriteYAMLFromLayer_SingleFile(t *testing.T) {
	// An individual-file layer (as `oras push file.yaml` produces): raw YAML with
	// a title annotation, not an archive.
	dir := t.TempDir()
	desc := ocispec.Descriptor{
		MediaType:   "application/vnd.oci.image.layer.v1.tar",
		Annotations: map[string]string{ocispec.AnnotationTitle: "deployment.yaml"},
	}
	n, err := writeYAMLFromLayer(dir, 3, desc, []byte(sampleDeployment))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || readAll(t, dir)["deployment.yaml"] != sampleDeployment {
		t.Fatalf("single file not written: n=%d files=%v", n, readAll(t, dir))
	}
}

func TestWriteYAMLFromLayer_SingleFileNoTitle(t *testing.T) {
	dir := t.TempDir()
	n, err := writeYAMLFromLayer(dir, 5, ocispec.Descriptor{}, []byte(sampleService))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || readAll(t, dir)["layer-5.yaml"] != sampleService {
		t.Fatalf("untitled single file not written: files=%v", readAll(t, dir))
	}
}

func TestWriteYAMLFromLayer_SkipsTitledCompanionRecord(t *testing.T) {
	dir := t.TempDir()
	desc := ocispec.Descriptor{
		MediaType:   "application/vnd.confighub.check-results.v1+json",
		Annotations: map[string]string{ocispec.AnnotationTitle: "records/checks.json"},
	}
	n, err := writeYAMLFromLayer(dir, 6, desc, []byte(`{"result":"pass"}`))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(readAll(t, dir)) != 0 {
		t.Fatalf("companion record written as YAML: n=%d files=%v", n, readAll(t, dir))
	}
}

func TestWriteYAMLFromTar_RejectsTraversal(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Name: "../escape.yaml", Mode: 0o644, Size: int64(len(sampleService)), Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte(sampleService))
	_ = tw.Close()

	dir := t.TempDir()
	if _, err := writeYAMLFromTar(dir, buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	// The escaping name is cleaned to a path inside dir; nothing lands in the parent.
	parent := filepath.Dir(dir)
	if _, err := os.Stat(filepath.Join(parent, "escape.yaml")); err == nil {
		t.Fatal("path traversal escaped the destination directory")
	}
}

func TestIsLocalRegistry(t *testing.T) {
	for _, r := range []string{"localhost", "localhost:5001", "127.0.0.1:5000"} {
		if !isLocalRegistry(r) {
			t.Errorf("%q should be local", r)
		}
	}
	for _, r := range []string{"ghcr.io", "registry-1.docker.io", "example.com:443"} {
		if isLocalRegistry(r) {
			t.Errorf("%q should not be local", r)
		}
	}
}

func TestIsOCIRef(t *testing.T) {
	if !isOCIRef("oci://ghcr.io/x/y:tag") {
		t.Error("oci:// ref not detected")
	}
	for _, s := range []string{"./dir", "-", "file.yaml", "https://x/y"} {
		if isOCIRef(s) {
			t.Errorf("%q should not be an OCI ref", s)
		}
	}
}
