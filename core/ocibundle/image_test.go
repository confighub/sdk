// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package ocibundle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// imageRegistry serves one repository, images/app, holding manifests by tag
// and digest, and the blobs they name. It counts the blobs fetched.
type imageRegistry struct {
	t         *testing.T
	manifests map[string][]byte // by tag and by digest
	types     map[string]string
	blobs     map[digest.Digest][]byte
	tags      []string
	fetched   []digest.Digest
}

func newImageRegistry(t *testing.T) *imageRegistry {
	return &imageRegistry{t: t, manifests: map[string][]byte{}, types: map[string]string{}, blobs: map[digest.Digest][]byte{}}
}

// image adds an image of the layers, oldest first, and returns its descriptor.
func (r *imageRegistry) image(layers ...[]byte) ocispec.Descriptor {
	config := []byte("{}")
	r.blobs[digest.FromBytes(config)] = config
	m := ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    ocispec.Descriptor{MediaType: ocispec.MediaTypeImageConfig, Digest: digest.FromBytes(config), Size: int64(len(config))},
	}
	m.SchemaVersion = 2
	for _, l := range layers {
		d := digest.FromBytes(l)
		r.blobs[d] = l
		m.Layers = append(m.Layers, ocispec.Descriptor{MediaType: ocispec.MediaTypeImageLayerGzip, Digest: d, Size: int64(len(l))})
	}
	return r.manifest(ocispec.MediaTypeImageManifest, m)
}

func (r *imageRegistry) manifest(mediaType string, v any) ocispec.Descriptor {
	data, err := json.Marshal(v)
	if err != nil {
		r.t.Fatal(err)
	}
	d := digest.FromBytes(data)
	r.manifests[d.String()] = data
	r.types[d.String()] = mediaType
	return ocispec.Descriptor{MediaType: mediaType, Digest: d, Size: int64(len(data))}
}

func (r *imageRegistry) tag(tag string, desc ocispec.Descriptor) {
	r.manifests[tag] = r.manifests[desc.Digest.String()]
	r.types[tag] = desc.MediaType
	r.tags = append(r.tags, tag)
}

func (r *imageRegistry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	const prefix = "/v2/images/app/"
	switch {
	case req.URL.Path == prefix+"tags/list":
		if err := json.NewEncoder(w).Encode(struct {
			Name string   `json:"name"`
			Tags []string `json:"tags"`
		}{"images/app", r.tags}); err != nil {
			r.t.Error(err)
		}
	case strings.HasPrefix(req.URL.Path, prefix+"manifests/"):
		ref := strings.TrimPrefix(req.URL.Path, prefix+"manifests/")
		data, ok := r.manifests[ref]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", r.types[ref])
		w.Header().Set("Docker-Content-Digest", digest.FromBytes(data).String())
		_, _ = w.Write(data)
	case strings.HasPrefix(req.URL.Path, prefix+"blobs/"):
		d := digest.Digest(strings.TrimPrefix(req.URL.Path, prefix+"blobs/"))
		blob, ok := r.blobs[d]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		r.fetched = append(r.fetched, d)
		_, _ = w.Write(blob)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestReadImageFile(t *testing.T) {
	reg := newImageRegistry(t)
	base := gzipBytes(t, tarBytes(t, map[string]string{"app/binary": "ELF", "manifests/app.yaml": "old"}))
	top := gzipBytes(t, tarBytes(t, map[string]string{"manifests/app.yaml": sampleDeployment}))
	amd64 := reg.image(base, top)
	arm64 := reg.image(base, top)
	attestation := reg.image(gzipBytes(t, tarBytes(t, map[string]string{"sbom.json": "{}"})))
	amd64.Platform = &ocispec.Platform{OS: "linux", Architecture: "amd64"}
	arm64.Platform = &ocispec.Platform{OS: "linux", Architecture: "arm64"}
	attestation.Platform = &ocispec.Platform{OS: "unknown", Architecture: "unknown"}
	index := ocispec.Index{MediaType: ocispec.MediaTypeImageIndex, Manifests: []ocispec.Descriptor{attestation, amd64, arm64}}
	index.SchemaVersion = 2
	indexDesc := reg.manifest(ocispec.MediaTypeImageIndex, index)
	reg.tag("v1.0.0", indexDesc)
	reg.tag("single", reg.image(base))
	srv := httptest.NewServer(reg)
	defer srv.Close()
	repo := Scheme + strings.TrimPrefix(srv.URL, "http://") + "/images/app"
	opts := Options{PlainHTTPLoopback: true}

	// The newest layer holding the file wins, and older layers aren't fetched.
	got, resolved, err := ReadImageFile(context.Background(), repo+":v1.0.0", "/manifests/app.yaml", opts)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != sampleDeployment {
		t.Errorf("file = %q, want the top layer's", got)
	}
	if resolved != indexDesc.Digest.String() {
		t.Errorf("digest = %s, want the index's %s", resolved, indexDesc.Digest)
	}
	if len(reg.fetched) != 1 || reg.fetched[0] != digest.FromBytes(top) {
		t.Errorf("fetched %v, want only the top layer", reg.fetched)
	}

	// A file only in an older layer is found there.
	got, _, err = ReadImageFile(context.Background(), repo+":single", "manifests/app.yaml", opts)
	if err != nil || string(got) != "old" {
		t.Errorf("file = %q, err = %v, want the base layer's", got, err)
	}

	if _, _, err := ReadImageFile(context.Background(), repo+":v1.0.0", "manifests/missing.yaml", opts); err == nil || !strings.Contains(err.Error(), "has no file manifests/missing.yaml") {
		t.Errorf("err = %v, want a missing-file error", err)
	}
}

func TestNewestRelease(t *testing.T) {
	reg := newImageRegistry(t)
	img := reg.image(gzipBytes(t, tarBytes(t, map[string]string{"a.yaml": "a: 1"})))
	for _, tag := range []string{"latest", "v0.9.0", "v0.10.0", "v0.10.1-rc.1", "0.2.0", "main", "v01.0.0", "sha-abc"} {
		reg.tag(tag, img)
	}
	srv := httptest.NewServer(reg)
	defer srv.Close()
	repo := Scheme + strings.TrimPrefix(srv.URL, "http://") + "/images/app"

	got, err := NewestRelease(context.Background(), repo, Options{PlainHTTPLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	if got != "v0.10.0" {
		t.Errorf("NewestRelease = %q, want v0.10.0", got)
	}

	if _, err := NewestRelease(context.Background(), repo+":v0.9.0", Options{PlainHTTPLoopback: true}); err == nil {
		t.Error("a reference with a tag was accepted as a repository")
	}

	reg.tags = []string{"latest", "main"}
	if _, err := NewestRelease(context.Background(), repo, Options{PlainHTTPLoopback: true}); err == nil || !strings.Contains(err.Error(), "no release tag") {
		t.Errorf("err = %v, want no release tag", err)
	}
}
