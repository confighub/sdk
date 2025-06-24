// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/fluxcd/pkg/oci"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	gcrv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/confighub/sdk/bridge-worker/lib"
	"github.com/confighub/sdk/workerapi"
)

type FluxOCIWorker struct {
	Config              *FluxOCIWorkerConfig
	LoginToRegistryFunc func(ctx context.Context, workerConfig *FluxOCIWorkerConfig, params *FluxOCIParams, newClientFunc NewClientFunc) (OCIClient, error)
}

type applyOutput struct {
	// Digest is the digest of the pushed image
	Digest string `json:","`
	// URL is the URL of the pushed image
	URL string `json:","`
}

// Default implementation of LoginToRegistryFunc
func NewFluxOCIWorker() *FluxOCIWorker {
	return &FluxOCIWorker{
		LoginToRegistryFunc: LoginToRegistry,
	}
}

var _ api.BridgeWorker = (*FluxOCIWorker)(nil)

var (
	ErrImageHasNotBeenApplied  = errors.New("image has not been applied")
	ErrImageNotFound           = errors.New("image not found")
	ErrImageDeletionNotAllowed = errors.New("image is not allowed to be deleted")
)

const (
	ProviderNone    = "None"
	ProviderGeneric = "Generic"
	ProviderAWS     = "AWS"
	ProviderAzure   = "Azure"
	ProviderGCP     = "GCP"
)

// FluxOCIParams represents the target parameters for OCI operations
type FluxOCIParams struct {
	Repository    string `json:",omitempty"`
	RevTag        string `json:"-"` // internal use for revision tag: rev1, rev2, etc.
	Tag           string `json:",omitempty"`
	Provider      string `json:",omitempty"`
	AllowDeletion string `json:",omitempty"`
	// Optional Kubernetes Secret for Docker credentials
	KubernetesSecretName      string `json:",omitempty"`
	KubernetesSecretNamespace string `json:",omitempty"`
}

func (p *FluxOCIParams) ToMap() map[string]interface{} {
	var result map[string]interface{}
	data, _ := json.Marshal(p)
	_ = json.Unmarshal(data, &result)
	return result
}

// Info shows FluxOCIWorker api.BridgeWorkerInfo.
// If the worker is not configured with REPO and TAG,
// we will not provide any default targets
// Repository and Tag are generally passed via FluxOCIParams
// and read from the BridgeWorkerPayload
func (f FluxOCIWorker) Info(_ api.InfoOptions) api.BridgeWorkerInfo {
	repository := os.Getenv("REPO")
	if repository == "" {
		repository = "// rerun with -e REPO=<your registry>"
	}
	tag := os.Getenv("TAG")
	if tag == "" {
		tag = "latest"
	}

	availableTargets := make([]api.Target, 0)
	if os.Getenv("REPO") != "" && os.Getenv("TAG") != "" {
		availableTargets = defaultTargets(repository, tag)
	}

	return api.BridgeWorkerInfo{
		SupportedConfigTypes: []*api.ConfigType{
			{
				ToolchainType:    workerapi.ToolchainKubernetesYAML,
				ProviderType:     api.ProviderFluxOCIWriter,
				AvailableTargets: availableTargets,
			},
		},
	}
}

func defaultTargets(repository string, tag string) []api.Target {
	return []api.Target{
		{
			Name: "flux-oci-writer",
			Params: (&FluxOCIParams{
				Repository:    repository,
				Tag:           tag,
				Provider:      ProviderNone, // provider None means reading credentials from .docker/config.json
				AllowDeletion: "false",
			}).ToMap(),
		},
		{
			Name: "flux-oci-writer-with-aws-provider",
			Params: (&FluxOCIParams{
				Repository:    repository,
				Tag:           tag,
				Provider:      ProviderAWS,
				AllowDeletion: "false",
			}).ToMap(),
		},
		{
			Name: "flux-oci-writer-with-gcp-provider",
			Params: (&FluxOCIParams{
				Repository:    repository,
				Tag:           tag,
				Provider:      ProviderGCP,
				AllowDeletion: "false",
			}).ToMap(),
		},
		{
			Name: "flux-oci-writer-with-azure-provider",
			Params: (&FluxOCIParams{
				Repository:    repository,
				Tag:           tag,
				Provider:      ProviderAzure,
				AllowDeletion: "false",
			}).ToMap(),
		},
	}
}

func build(data []byte, filename string) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	header := &tar.Header{
		Name:       filename,
		Size:       int64(len(data)),
		Mode:       0644,
		Uid:        0,
		Gid:        0,
		Uname:      "",
		Gname:      "",
		ModTime:    time.Time{},
		AccessTime: time.Time{},
		ChangeTime: time.Time{},
	}

	if err := tw.WriteHeader(header); err != nil {
		tw.Close()
		gw.Close()
		return nil, err
	}

	if _, err := tw.Write(data); err != nil {
		tw.Close()
		gw.Close()
		return nil, err
	}

	if err := tw.Close(); err != nil {
		gw.Close()
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// Define a global variable for the push function for dependency injection
// This allows us to mock the push function in tests
// and use a real implementation in production
var pushFunc = push

func push(cli OCIClient, tarGz []byte, url string, tags ...string) (string, error) {
	ref, err := name.ParseReference(url)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	layer, err := createLayer(tarGz)
	if err != nil {
		return "", fmt.Errorf("error creating layer: %w", err)
	}

	img := mutate.MediaType(empty.Image, types.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, oci.CanonicalConfigMediaType)
	img = mutate.Annotations(img, map[string]string{
		"org.opencontainers.image.source":   "source",
		"org.opencontainers.image.revision": "latest",
		"org.opencontainers.image.created":  time.Now().UTC().Format(time.RFC3339),
	}).(gcrv1.Image)

	img, err = mutate.Append(img, mutate.Addendum{Layer: layer})
	if err != nil {
		return "", fmt.Errorf("appending content to artifact failed: %w", err)
	}

	options := []crane.Option{
		crane.WithContext(context.Background()),
	}
	options = append(cli.GetOptions(), options...)

	if err := crane.Push(img, url, options...); err != nil {
		return "", fmt.Errorf("pushing artifact failed: %w", err)
	}
	if len(tags) > 0 {
		for _, tag := range tags {
			if err := crane.Tag(url, tag, options...); err != nil {
				return "", fmt.Errorf("tagging artifact failed: %w", err)
			}
		}
	}

	digest, err := img.Digest()
	if err != nil {
		return "", fmt.Errorf("parsing artifact digest failed: %w", err)
	}

	return ref.Context().Digest(digest.String()).String(), err
}

func createLayer(tarGz []byte) (gcrv1.Layer, error) {
	op := func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(tarGz)), nil
	}
	layer, err := tarball.LayerFromOpener(op, tarball.WithMediaType(oci.CanonicalContentMediaType), tarball.WithCompressedCaching)
	if err != nil {
		return nil, err
	}
	return layer, nil
}

// ParseTargetParams extracts and parses target parameters for OCI
func ParseFluxOCIParams(payload api.BridgeWorkerPayload) (*FluxOCIParams, error) {
	params := &FluxOCIParams{}
	if len(payload.TargetParams) > 0 {
		if err := json.Unmarshal(payload.TargetParams, params); err != nil {
			return nil, fmt.Errorf("failed to parse target params: %v", err)
		}
	}

	// RevisionNum == 0 should not be possible with increasing counter, check if zero for presence
	if payload.RevisionNum == 0 {
		return nil, fmt.Errorf("revision number is required, got 0")
	}
	params.RevTag = fmt.Sprintf("rev%d", payload.RevisionNum)

	// params.Repository and UnitSlug combine to make the full repository URL
	// Repository is the prefix e.g. ghcr.io/test and UnitSlug is the suffix e.g. repo
	if params.Repository != "" && payload.UnitSlug != "" {
		params.Repository = fmt.Sprintf("%s/%s", params.Repository, payload.UnitSlug)
	}

	if params.Repository == "" || payload.UnitSlug == "" {
		return nil, fmt.Errorf("repository and Unit slug are required: %s, slug: %s",
			payload.TargetParams,
			payload.UnitSlug)
	}

	return params, nil
}

func (f FluxOCIWorker) Apply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	params, err := ParseFluxOCIParams(payload)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			err.Error(),
		))
		return err
	}

	cli, err := f.LoginToRegistryFunc(wctx.Context(), f.Config, params, NewRealOCIClient)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			err.Error(),
		))
		return err
	}

	tarGz, err := build(payload.Data, "manifest.yaml")
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to build tar.gz: %v", err),
		))
		return err
	}

	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Pushing to OCI repository...",
	)); err != nil {
		return err
	}

	url := params.Repository + ":" + params.RevTag
	tags := make([]string, 0)
	if params.Tag != "" {
		tags = append(tags, params.Tag)
	}

	digest, err := pushFunc(cli, tarGz, url, tags...)
	if err != nil {
		log.Log.Error(err, "Failed to push to registry")
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("Failed to push to registry: %v", err),
		))
		return err
	}

	applyOutputs := make([]applyOutput, 0)
	applyOutputs = append(applyOutputs, applyOutput{
		Digest: digest,
		URL:    url,
	})
	if len(tags) > 0 {
		for _, tag := range tags {
			applyOutputs = append(applyOutputs, applyOutput{
				Digest: digest,
				URL:    params.Repository + ":" + tag,
			})
		}
	}

	jsonOutputs, err := json.Marshal(applyOutputs)
	if err != nil {
		log.Log.Error(err, "Failed to marshal outputs")
		status := newActionResult(
			api.ActionStatusCompleted,
			api.ActionResultApplyCompleted,
			fmt.Sprintf("Failed to marshal outputs after successful push: %v", err),
		)
		status.LiveState = payload.Data
		wctx.SendStatus(status)
		return err
	}

	status := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultApplyCompleted,
		fmt.Sprintf("Successfully pushed to OCI repository at %s", time.Now().Format(time.RFC3339)),
	)
	status.LiveState = payload.Data
	status.Outputs = jsonOutputs
	return wctx.SendStatus(status)
}

func (f FluxOCIWorker) Refresh(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	params, err := ParseFluxOCIParams(payload)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			err.Error(),
		))
		return err
	}

	cli, err := f.LoginToRegistryFunc(wctx.Context(), f.Config, params, NewRealOCIClient)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			err.Error(),
		))
		return err
	}

	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Starting to retrieve manifest...",
	)); err != nil {
		return err
	}

	url := params.Repository + ":" + params.Tag
	img, err := crane.Pull(url, cli.GetOptions()...)
	if err != nil {
		log.Log.Error(err, "Failed to pull image")
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("Failed to pull image: %v", err),
		))
		return err
	}

	layers, err := img.Layers()
	if err != nil {
		log.Log.Error(err, "Failed to get layers")
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("Failed to get layers: %v", err),
		))
		return err
	}

	if len(layers) == 0 {
		log.Log.Error(err, "No layers found in image")
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			"No layers found in image",
		))
		return err
	}

	// Get the first layer which contains our manifest
	layer := layers[0]
	rc, err := layer.Uncompressed()
	if err != nil {
		log.Log.Error(err, "Failed to uncompress layer")
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("Failed to uncompress layer: %v", err),
		))
		return err
	}
	defer rc.Close()

	tr := tar.NewReader(rc)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Log.Error(err, "Failed to read tar")
			wctx.SendStatus(newActionResult(
				api.ActionStatusFailed,
				api.ActionResultRefreshFailed,
				fmt.Sprintf("Failed to read tar: %v", err),
			))
			return err
		}

		if header.Name == "manifest.yaml" {
			content, err := io.ReadAll(tr)
			if err != nil {
				log.Log.Error(err, "Failed to read manifest")
				wctx.SendStatus(newActionResult(
					api.ActionStatusFailed,
					api.ActionResultRefreshFailed,
					fmt.Sprintf("Failed to read manifest: %v", err),
				))
				return err
			}

			result := newActionResult(
				api.ActionStatusCompleted,
				api.ActionResultRefreshAndNoDrift,
				fmt.Sprintf("Retrieved manifest successfully at %s", time.Now().Format(time.RFC3339)),
			)
			result.LiveState = content
			return wctx.SendStatus(result)
		}
	}

	log.Log.Error(err, "Manifest not found in image")
	wctx.SendStatus(newActionResult(
		api.ActionStatusFailed,
		api.ActionResultRefreshFailed,
		"manifest.yaml not found in image",
	))
	return err
}

func (f FluxOCIWorker) Import(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	params, err := ParseFluxOCIParams(payload)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultImportFailed,
			err.Error(),
		))
		return err
	}

	cli, err := f.LoginToRegistryFunc(wctx.Context(), f.Config, params, NewRealOCIClient)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultImportFailed,
			err.Error(),
		))
		return err
	}

	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Starting to retrieve manifest...",
	)); err != nil {
		return err
	}

	url := params.Repository + ":" + params.Tag
	img, err := crane.Pull(url, cli.GetOptions()...)
	if err != nil {
		log.Log.Error(err, "Failed to pull image")
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultImportFailed,
			fmt.Sprintf("Failed to pull image: %v", err),
		))
		return err
	}

	layers, err := img.Layers()
	if err != nil {
		log.Log.Error(err, "Failed to get layers")
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultImportFailed,
			fmt.Sprintf("Failed to get layers: %v", err),
		))
		return err
	}

	if len(layers) == 0 {
		log.Log.Error(err, "No layers found in image")
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultImportFailed,
			"No layers found in image",
		))
		return err
	}

	// Get the first layer which contains our manifest
	layer := layers[0]
	rc, err := layer.Uncompressed()
	if err != nil {
		log.Log.Error(err, "Failed to uncompress layer")
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultImportFailed,
			fmt.Sprintf("Failed to uncompress layer: %v", err),
		))
		return err
	}
	defer rc.Close()

	tr := tar.NewReader(rc)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Log.Error(err, "Failed to read tar")
			wctx.SendStatus(newActionResult(
				api.ActionStatusFailed,
				api.ActionResultImportFailed,
				fmt.Sprintf("Failed to read tar: %v", err),
			))
			return err
		}

		if header.Name == "manifest.yaml" {
			content, err := io.ReadAll(tr)
			if err != nil {
				log.Log.Error(err, "Failed to read manifest")
				wctx.SendStatus(newActionResult(
					api.ActionStatusFailed,
					api.ActionResultImportFailed,
					fmt.Sprintf("Failed to read manifest: %v", err),
				))
				return err
			}

			result := newActionResult(
				api.ActionStatusCompleted,
				api.ActionResultImportCompleted,
				fmt.Sprintf("Imported manifest successfully at %s", time.Now().Format(time.RFC3339)),
			)

			// this is Import, so set both Data and LiveState
			result.Data = content
			result.LiveState = content
			return wctx.SendStatus(result)
		}
	}

	log.Log.Error(err, "Manifest not found in image")
	wctx.SendStatus(newActionResult(
		api.ActionStatusFailed,
		api.ActionResultImportFailed,
		"manifest.yaml not found in image",
	))
	return fmt.Errorf("manifest.yaml not found in image")
}

func (f FluxOCIWorker) Destroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	params, err := ParseFluxOCIParams(payload)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			err.Error(),
		))
		return err
	}

	if params.AllowDeletion == "false" {
		err = fmt.Errorf("image deletion not allowed")
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			err.Error(),
		), err)
	}

	cli, err := f.LoginToRegistryFunc(wctx.Context(), f.Config, params, NewRealOCIClient)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			err.Error(),
		))
		return err
	}

	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Deleting from OCI repository...",
	)); err != nil {
		return err
	}

	defaultUrl := params.Repository + ":" + params.RevTag
	pushUrls := []string{defaultUrl}
	if params.Tag != "" {
		pushUrls = append(pushUrls, params.Repository+":"+params.Tag)
	}

	for _, url := range pushUrls {
		if err := cli.Delete(wctx.Context(), url); err != nil {
			wctx.SendStatus(newActionResult(
				api.ActionStatusFailed,
				api.ActionResultDestroyFailed,
				fmt.Sprintf("failed to delete: %v", err),
			))
			return err
		}
	}

	result := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultDestroyCompleted,
		fmt.Sprintf("Successfully deleted from OCI repository at %s", time.Now().Format(time.RFC3339)),
	)
	result.LiveState = []byte{}
	return wctx.SendStatus(result)
}

func (f FluxOCIWorker) Finalize(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Starting finalization...",
	)); err != nil {
		return err
	}

	log.Log.Info("✅ Finalization completed successfully")

	result := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultNone,
		"Finalization completed successfully",
	)
	return wctx.SendStatus(result)
}
