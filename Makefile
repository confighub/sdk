GOTESTSUM_V?=1.12.0
GOCI_LINT_V?=v1.61.0
PRE_COMMIT_V?=v4.0.1
OS?=$(shell go env GOOS)
ARCH?=$(shell go env GOARCH)
GOPATH ?= $(shell go env GOPATH)
GOBIN?=${GOPATH}/bin
BRIDGE_WORKER?=confighub-worker
SHA_SUM := $(shell git rev-parse HEAD)
CUB_CMD?=./bin/cub
RELEASE?= # 'true|1' Set to true to build a release version of the CLI
VERSION?=
LDFLAGS :=
ifneq ($(VERSION),)
  LDFLAGS := -ldflags "\
    -X main.Version=$(VERSION) \
    -X main.BuildTag=$$(git rev-parse HEAD) \
    -X main.BuildDate=$$(date -u +%Y-%m-%dT%H:%M:%SZ)"
endif

.DEFAULT_GOAL:=help

# HOWTO
# To have targets included when running `make help` you must add an inline comment starting with ##

.PHONY: help
help:
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: clean
clean:
	@rm -f $(CUB_CMD)
	@rm -f ./cmd/bin/*
	@rm -rf ./test/results

# Sibling modules that contain tests
CORE_MODULES = ./core 
TEST_MODULES = ./function-impl ./bridge-impl ./worker-function-impl \
	./configkit/yqkit ./configkit/hclkit ./configkit/tomlkit ./configkit/inikit \
	./configkit/k8skit ./configkit/propkit ./configkit/appyamlkit \
	./configkit/jsonkit ./configkit/envkit ./configkit/textkit
CMD_MODULES = ./cmd/cub ./cmd/cub-worker ./cmd/functionsrv ./cmd/fctl
# All sibling modules that need prep (mod download/tidy)
SIBLING_MODULES = $(TEST_MODULES) $(CMD_MODULES)

.PHONY: all-prep
all-prep:
	@for mod in $(CORE_MODULES); do echo "=== Prep $$mod ===" && (cd $$mod && go mod download && go mod tidy) ; done
	@for mod in $(SIBLING_MODULES); do echo "=== Prep $$mod ===" && (cd $$mod && go mod download && go mod tidy) ; done

.PHONY: all-local
all-local: all-prep build-modules build-cli build-funcexec build-worker ## Builds all the things locally (no docker) without tests or lints

.PHONY: all
all: all-local ## Builds all the things, without tests or lints

.PHONY: lint
lint: ## Run linters
ifdef CI
	mkdir -p ./test/results
	cd core && golangci-lint run --out-format json ./... > ../test/results/public-lint-tests.json
else
	cd core && golangci-lint run -v ./...
	gitleaks detect -v --redact
endif

.PHONY: format
format: ## Format source code based on golang-ci configuration
	cd core && golangci-lint run --fix -v ./...

# RELEASE is for non-container builds
# Use abspath so the output path survives the cd into cmd/cub
CUB_CMD_ABS=$(abspath $(CUB_CMD))

.PHONY: build-cli
build-cli: ## Build the CLI
ifdef RELEASE
	cd ./cmd/cub && go build $(LDFLAGS) -v -o $(CUB_CMD_ABS)-${OS}-${ARCH} .
else
	cd ./cmd/cub && go build $(LDFLAGS) -v -o $(CUB_CMD_ABS) .
endif

.PHONY: test
test: ## Run golang tests
ifdef CI
	@for mod in $(CORE_MODULES); do echo "=== Testing $$mod ===" && (cd $$mod && go test -v ./...) || exit 1; done
	@for mod in $(TEST_MODULES); do echo "=== Testing $$mod ===" && (cd $$mod && go test -v ./...) || exit 1; done
else
	mkdir -p ./test/results
	@for mod in $(CORE_MODULES); do echo "=== Testing $$mod ===" && (cd $$mod && gotestsum --junitfile ../test/results/public-unit-tests.xml -- -race -coverprofile=../test/results/public-cover.out -v ./...) || exit 1; done
	@for mod in $(TEST_MODULES); do echo "=== Testing $$mod ===" && (cd $$mod && go test -race -v ./...) || exit 1; done
endif

.PHONY: cover
cover: test ## Generate coverage profile and display it in a web browser
	go tool cover -html=./test/results/public-cover.out -o ./test/results/public-cover.html
ifndef CI
	open ./test/results/public-cover.html
endif

.PHONY: build-worker
build-worker: ## Build bridge worker
	$(MAKE) -C ./bridge-impl BINARY_DIR=../bin all

.PHONY: build-modules
build-modules: ## Build modules
	@for mod in $(CORE_MODULES) $(TEST_MODULES); do echo "=== Building $$mod ===" && (cd $$mod && go build ./...) ; done

.PHONY: build-funcexec
build-funcexec: ## Build standalone function execuctor and its CLI
	cd ./function-impl && $(MAKE) all

.PHONY: test-funcexec
test-funcexec: ## Test standalone function executor, its CLI, and functions
	cd ./function-impl && $(MAKE) manual-test

.PHONY: kind-up
kind-up: ## Create a kind cluster
	${GOBIN}/kind create cluster --name $${NAME:-kind}

.PHONY: kind-down
kind-down: ## Delete the kind cluster
	${GOBIN}/kind delete cluster --name $${NAME:-kind}
