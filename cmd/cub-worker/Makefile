# Go parameters
GOCMD=go
OS?=$(shell go env GOOS)
ARCH?=$(shell go env GOARCH)
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
BINARY_DIR=../../../bin
WORKER_CMD?=$(BINARY_DIR)/cub-worker-run
WORKER_CMD_ABS=$(abspath $(WORKER_CMD))
# VERSION and the API version it has to agree with are declared in version.mk, two
# directories up in both layouts this Makefile is built in.
include ../../version.mk
# Always stamped: version.mk gives every build a version, a release one or a dev one.
LDFLAGS := -ldflags "\
  -X main.Version=$(VERSION) \
  -X main.BuildTag=$$(git rev-parse HEAD 2>/dev/null || echo unknown) \
  -X main.BuildDate=$$(date -u +%Y-%m-%dT%H:%M:%SZ)"

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
	$(GOBUILD) $(LDFLAGS) -v -o $(abspath $(BINARY_DIR))/cub-worker-run-${OS}-${ARCH} .
else
	$(GOBUILD) $(LDFLAGS) -v -o $(WORKER_CMD_ABS) .
endif

all: cub-worker-run
