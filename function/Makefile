# HOWTO
# To have targets included when running `make help` you must add an inline comment starting with ##

.PHONY: all
all: build-cli build-server ## Builds all targets without tests or lints

.PHONY: build-cli
build-cli: ## Build the CLI to bin/fctl
	mkdir -p bin
	go build -o ./bin/fctl ../cmd/fctl

.PHONY: build-server
build-server: ## Build the server to bin/functionsrv
	mkdir -p bin
	go build -o ./bin/functionsrv ../cmd/functionsrv 

.PHONY: build-server-debug
build-server-debug: ## Build the server to bin/functionsrv
	mkdir -p bin
	go build -gcflags="all=-N -l" -o ./bin/functionsrv ../cmd/functionsrv 

.PHONY: test
test: ## Run golang tests
	mkdir -p test/results
	gotestsum --junitfile test/results/unit-tests.xml -- -race -coverprofile=test/results/cover.out -v ./...

.PHONY: manual-test
manual-test: build-cli build-server ## Run manual end-to-end tests
	(cd ../cmd/fctl ; ./manual-test.sh)

.PHONY: all-test
all-test: test manual-test ## Run all test targets
