# Go parameters
GOCMD=go
OS?=$(shell go env GOOS)
ARCH?=$(shell go env GOARCH)
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
BINARY_NAME=bctl
BINARY_DIR=../../bin
# Use abspath so the output path survives the cd into cmd/cub-worker
WORKER_CMD?=$(BINARY_DIR)/cub-worker-run
WORKER_CMD_ABS=$(abspath $(WORKER_CMD))

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
	cd ../cmd/cub-worker && $(GOBUILD) \
	-ldflags "-X main.BuildTag=$$(git rev-parse HEAD) \
	-X main.BuildDate=$$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
	-v -o $(abspath $(BINARY_DIR))/cub-worker-run-${OS}-${ARCH} .
else
	cd ../cmd/cub-worker && $(GOBUILD) \
	-ldflags "-X main.BuildTag=$$(git rev-parse HEAD) \
	-X main.BuildDate=$$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
	-v -o $(WORKER_CMD_ABS) .
endif

all: cub-worker-run
