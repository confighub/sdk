# Go parameters
GOCMD=go
OS?=$(shell go env GOOS)
ARCH?=$(shell go env GOARCH)
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
BINARY_DIR=../../bin
# Use abspath so the output path survives the cd into cmd/cub-worker
WORKER_CMD?=$(BINARY_DIR)/cub-worker-run
WORKER_CMD_ABS=$(abspath $(WORKER_CMD))
VERSION?=
LDFLAGS :=
ifneq ($(VERSION),)
  LDFLAGS := -ldflags "\
    -X main.Version=$(VERSION) \
    -X main.BuildTag=$$(git rev-parse HEAD) \
    -X main.BuildDate=$$(date -u +%Y-%m-%dT%H:%M:%SZ)"
endif

.PHONY: clean all cub-worker-run

# Clean build artifacts
clean:
	$(GOCLEAN)
	rm $(BINARY_DIR)/cub-worker-run*

# RELEASE is for non-container builds
# Build cub-worker-run
cub-worker-run:
	@mkdir -p $(BINARY_DIR)
ifdef RELEASE
	cd ../cmd/cub-worker && $(GOBUILD) $(LDFLAGS) -v -o $(abspath $(BINARY_DIR))/cub-worker-run-${OS}-${ARCH} .
else
	cd ../cmd/cub-worker && $(GOBUILD) $(LDFLAGS) -v -o $(WORKER_CMD_ABS) .
endif

all: cub-worker-run
