// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package ocibundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const sampleDeployment = "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: app\n"
const sampleService = "apiVersion: v1\nkind: Service\nmetadata:\n  name: app\n"

func isYAML(p string) bool {
	return strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml")
}

func tarBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
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
	return buf.Bytes()
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func filesByPath(files []File) map[string]string {
	out := map[string]string{}
	for _, f := range files {
		out[f.Path] = string(f.Content)
	}
	return out
}

func TestExtractTarGzipSelectsIncludedFiles(t *testing.T) {
	x := &extractor{opts: Options{Include: isYAML}}
	blob := gzipBytes(t, tarBytes(t, map[string]string{
		"backend.yaml":   sampleDeployment,
		"rbac/role.yaml": sampleService,
		"README.md":      "not yaml",
	}))
	if err := x.layer(0, ocispec.Descriptor{}, blob); err != nil {
		t.Fatal(err)
	}
	got := filesByPath(x.files)
	if len(got) != 2 || got["backend.yaml"] != sampleDeployment || got["rbac/role.yaml"] != sampleService {
		t.Fatalf("files = %v", got)
	}
}

func TestExtractSingleFileLayers(t *testing.T) {
	x := &extractor{}
	titled := ocispec.Descriptor{Annotations: map[string]string{ocispec.AnnotationTitle: "config/app.properties"}}
	if err := x.layer(0, titled, []byte("a=b\n")); err != nil {
		t.Fatal(err)
	}
	if err := x.layer(5, ocispec.Descriptor{}, []byte(sampleService)); err != nil {
		t.Fatal(err)
	}
	got := filesByPath(x.files)
	if got["config/app.properties"] != "a=b\n" || got["layer-5.yaml"] != sampleService {
		t.Fatalf("files = %v", got)
	}
}

func TestExtractKeepsPathsInsideTheBundle(t *testing.T) {
	x := &extractor{}
	if err := x.layer(0, ocispec.Descriptor{}, tarBytes(t, map[string]string{
		"../escape.yaml":  sampleService,
		"/abs/place.yaml": sampleDeployment,
		"a/../../up.yaml": sampleService,
	})); err != nil {
		t.Fatal(err)
	}
	for _, f := range x.files {
		if strings.HasPrefix(f.Path, "/") || strings.Contains(f.Path, "..") {
			t.Errorf("path %q escapes the bundle", f.Path)
		}
	}
}

func TestExtractLimits(t *testing.T) {
	// A small compressed layer expanding past the total is refused while it is
	// read, not after it has all been held in memory.
	big := strings.Repeat("a", 10000)
	x := &extractor{opts: Options{Limits: Limits{MaxTotalBytes: 1000}}}
	err := x.layer(0, ocispec.Descriptor{}, gzipBytes(t, tarBytes(t, map[string]string{"big.yaml": big})))
	if err == nil || !strings.Contains(err.Error(), "exceed") {
		t.Fatalf("err = %v, want a size refusal", err)
	}

	x = &extractor{opts: Options{Limits: Limits{MaxFiles: 1}}}
	err = x.layer(0, ocispec.Descriptor{}, tarBytes(t, map[string]string{"a.yaml": "a: 1", "b.yaml": "b: 1"}))
	if err == nil || !strings.Contains(err.Error(), "more than the 1 files") {
		t.Fatalf("err = %v, want a file-count refusal", err)
	}
}

func TestIsRef(t *testing.T) {
	if !IsRef("oci://ghcr.io/x/y:tag") {
		t.Error("oci:// ref not detected")
	}
	for _, s := range []string{"./dir", "-", "file.yaml", "https://x/y"} {
		if IsRef(s) {
			t.Errorf("%q should not be an OCI ref", s)
		}
	}
}

func TestIsLoopbackRegistry(t *testing.T) {
	for _, r := range []string{"localhost", "localhost:5001", "127.0.0.1:5000"} {
		if !isLoopbackRegistry(r) {
			t.Errorf("%q should be loopback", r)
		}
	}
	for _, r := range []string{"ghcr.io", "registry-1.docker.io", "example.com:443"} {
		if isLoopbackRegistry(r) {
			t.Errorf("%q should not be loopback", r)
		}
	}
}

func TestIsPublicAddress(t *testing.T) {
	for _, s := range []string{"140.82.112.33", "8.8.8.8", "2606:4700::1111"} {
		if !IsPublicAddress(netip.MustParseAddr(s)) {
			t.Errorf("%s should be public", s)
		}
	}
	for _, s := range []string{
		"127.0.0.1", "::1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254",
		"100.64.0.1", "0.0.0.0", "::", "fd00::1", "fe80::1", "224.0.0.1", "::ffff:127.0.0.1", "::ffff:10.0.0.1",
	} {
		if IsPublicAddress(netip.MustParseAddr(s)) {
			t.Errorf("%s should not be public", s)
		}
	}
}

// fakeRegistry serves one repository holding one manifest, under a tag and its
// digest, optionally behind basic auth.
type fakeRegistry struct {
	manifest       []byte
	manifestDigest digest.Digest
	blobs          map[digest.Digest][]byte
	username       string
	password       string
}

func newFakeRegistry(t *testing.T, layers ...[]byte) *fakeRegistry {
	t.Helper()
	r := &fakeRegistry{blobs: map[digest.Digest][]byte{}}
	config := []byte("{}")
	r.blobs[digest.FromBytes(config)] = config
	m := ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    ocispec.Descriptor{MediaType: "application/vnd.oci.empty.v1+json", Digest: digest.FromBytes(config), Size: int64(len(config))},
	}
	m.SchemaVersion = 2
	for _, l := range layers {
		d := digest.FromBytes(l)
		r.blobs[d] = l
		m.Layers = append(m.Layers, ocispec.Descriptor{MediaType: ocispec.MediaTypeImageLayerGzip, Digest: d, Size: int64(len(l))})
	}
	var err error
	if r.manifest, err = json.Marshal(m); err != nil {
		t.Fatal(err)
	}
	r.manifestDigest = digest.FromBytes(r.manifest)
	return r
}

func (r *fakeRegistry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if r.username != "" {
		if u, p, ok := req.BasicAuth(); !ok || u != r.username || p != r.password {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}
	const prefix = "/v2/configs/app/"
	switch {
	case strings.HasPrefix(req.URL.Path, prefix+"manifests/"):
		ref := strings.TrimPrefix(req.URL.Path, prefix+"manifests/")
		if ref != "v1" && ref != r.manifestDigest.String() {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", ocispec.MediaTypeImageManifest)
		w.Header().Set("Docker-Content-Digest", r.manifestDigest.String())
		_, _ = w.Write(r.manifest)
	case strings.HasPrefix(req.URL.Path, prefix+"blobs/"):
		blob, ok := r.blobs[digest.Digest(strings.TrimPrefix(req.URL.Path, prefix+"blobs/"))]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(blob)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestPull(t *testing.T) {
	layer := gzipBytes(t, tarBytes(t, map[string]string{"manifests/app.yaml": sampleDeployment, "notes.txt": "x"}))
	reg := newFakeRegistry(t, layer)
	reg.username, reg.password = "robot", "s3cret"
	srv := httptest.NewServer(reg)
	defer srv.Close()
	ref := Scheme + strings.TrimPrefix(srv.URL, "http://") + "/configs/app:v1"

	opts := Options{PlainHTTPLoopback: true, Include: isYAML, Credentials: &Credentials{Username: "robot", Password: "s3cret"}}
	bundle, err := Pull(context.Background(), ref, "", opts)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Digest != reg.manifestDigest.String() {
		t.Errorf("Digest = %s, want %s", bundle.Digest, reg.manifestDigest)
	}
	if got := filesByPath(bundle.Files); len(got) != 1 || got["manifests/app.yaml"] != sampleDeployment {
		t.Errorf("files = %v", got)
	}

	// Pinned to the digest, the pull reads that manifest whatever the tag says.
	pinned, err := Pull(context.Background(), ref, bundle.Digest, opts)
	if err != nil {
		t.Fatal(err)
	}
	if pinned.Digest != bundle.Digest {
		t.Errorf("pinned Digest = %s, want %s", pinned.Digest, bundle.Digest)
	}

	opts.Credentials = &Credentials{Username: "robot", Password: "wrong"}
	if _, err := Pull(context.Background(), ref, "", opts); err == nil {
		t.Error("pull with wrong credentials succeeded")
	} else if strings.Contains(err.Error(), "wrong") {
		t.Errorf("error %q reveals the password", err)
	}

	opts.Credentials = &Credentials{Username: "robot", Password: "s3cret"}
	opts.Limits = Limits{MaxLayers: 0, MaxLayerBytes: 10}
	if _, err := Pull(context.Background(), ref, "", opts); err == nil || !strings.Contains(err.Error(), "more than the 10 allowed") {
		t.Errorf("err = %v, want a layer-size refusal", err)
	}
}

func TestPublicHTTPClientRefusesLoopbackAndHTTP(t *testing.T) {
	reg := newFakeRegistry(t, gzipBytes(t, tarBytes(t, map[string]string{"app.yaml": sampleDeployment})))
	srv := httptest.NewTLSServer(reg)
	defer srv.Close()
	ref := Scheme + strings.TrimPrefix(srv.URL, "https://") + "/configs/app:v1"

	_, err := Pull(context.Background(), ref, "", Options{HTTPClient: NewPublicHTTPClient()})
	if err == nil || !strings.Contains(err.Error(), "not a public address") {
		t.Errorf("err = %v, want a non-public address refusal", err)
	}

	plain := httptest.NewServer(reg)
	defer plain.Close()
	plainRef := Scheme + strings.TrimPrefix(plain.URL, "http://") + "/configs/app:v1"
	_, err = Pull(context.Background(), plainRef, "", Options{HTTPClient: NewPublicHTTPClient(), PlainHTTPLoopback: true})
	if err == nil || !strings.Contains(err.Error(), "only https") {
		t.Errorf("err = %v, want an https-only refusal", err)
	}
}
