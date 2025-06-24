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

.DEFAULT_GOAL:=help

# HOWTO
# To have targets included when running `make help` you must add an inline comment starting with ##

.PHONY: help
help:
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: clean
clean:
	@rm -f $(CUB_CMD)
	@rm -f ./bridge-worker/bin/*
	@rm -f ./function/bin/*
	@rm -rf ./test/results

.PHONY: all-prep
all-prep:
	go mod download
	go mod tidy

.PHONY: all-local
all-local: all-prep build-cli build-funcexec build-worker ## Builds all the things locally (no docker) without tests or lints

.PHONY: all
all: all-local ## Builds all the things, without tests or lints

.PHONY: lint
lint: ## Run linters
ifdef CI
	mkdir -p ./test/results
	golangci-lint run --out-format json ./... > ./test/results/public-lint-tests.json
else
	golangci-lint run -v ./...
	gitleaks detect -v --redact
endif

.PHONY: format
format: ## Format source code based on golang-ci configuration
	golangci-lint run --fix -v ./...

# RELEASE is for non-container builds
.PHONY: build-cli
build-cli: ## Build the CLI
ifdef RELEASE
	go build \
	-ldflags "-X main.BuildTag=$$(git rev-parse HEAD) \
	-X main.BuildDate=$$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
	-v -o $(CUB_CMD)-${OS}-${ARCH} ./cmd/cub
else
	go build \
	-ldflags "-X main.BuildTag=$$(git rev-parse HEAD) \
	-X main.BuildDate=$$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
	-v -o $(CUB_CMD) ./cmd/cub
endif

.PHONY: test
test: ## Run golang tests
ifdef CI
	go test -v ./...
else
	mkdir -p ./test/results
	gotestsum --junitfile ./test/results/public-unit-tests.xml -- -race -coverprofile=./test/results/internal-cover.out -v ./...
endif

.PHONY: cover
cover: test ## Generate coverage profile and display it in a web browser
	go tool cover -html=./test/results/public-cover.out -o ./test/results/public-cover.html
ifndef CI
	open ./test/results/public-cover.html
endif

.PHONY: build-worker
build-worker: ## Build bridge worker
	$(MAKE) -C ./bridge-worker all

.PHONY: build-funcexec
build-funcexec: ## Build standalone function execuctor and its CLI
	cd ./function && $(MAKE) all

.PHONY: test-funcexec
test-funcexec: ## Test standalone function executor, its CLI, and functions
	cd ./function && $(MAKE) manual-test

.PHONY: kind-up
kind-up: ## Create a kind cluster
	${GOBIN}/kind create cluster --name $${NAME:-kind}

.PHONY: kind-down
kind-down: ## Delete the kind cluster
	${GOBIN}/kind delete cluster --name $${NAME:-kind}
