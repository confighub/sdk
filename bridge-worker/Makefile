# Go parameters
GOCMD=go
OS?=$(shell go env GOOS)
ARCH?=$(shell go env GOARCH)
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
BINARY_NAME=bctl
BINARY_DIR=../../bin
WORKER_CMD?=$(BINARY_DIR)/cub-worker-run

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
	$(GOBUILD) \
	-ldflags "-X main.BuildTag=$$(git rev-parse HEAD) \
	-X main.BuildDate=$$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
	-v -o $(BINARY_DIR)/cub-worker-run-${OS}-${ARCH} ../cmd/cub-worker/*.go
else
	$(GOBUILD) \
	-ldflags "-X main.BuildTag=$$(git rev-parse HEAD) \
	-X main.BuildDate=$$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
	-v -o $(WORKER_CMD) ../cmd/cub-worker/*.go
endif

all: cub-worker-run
