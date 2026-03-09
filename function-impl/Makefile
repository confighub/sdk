# HOWTO
# To have targets included when running `make help` you must add an inline comment starting with ##

.PHONY: all
all: build-cli build-server ## Builds all targets without tests or lints

.PHONY: build-cli
build-cli: ## Build the CLI to bin/fctl
	mkdir -p ../cmd/bin
	cd ../cmd/fctl && go build -o ../bin/fctl .

.PHONY: build-server
build-server: ## Build the server to bin/functionsrv
	mkdir -p ../cmd/bin
	cd ../cmd/functionsrv && go build -o ../bin/functionsrv .

.PHONY: build-server-debug
build-server-debug: ## Build the server to bin/functionsrv
	mkdir -p ../cmd/bin
	cd ../cmd/functionsrv && go build -gcflags="all=-N -l" -o ../bin/functionsrv .

.PHONY: test
test: ## Run golang tests
	go test -race -v ./...

.PHONY: manual-test
manual-test: build-cli build-server ## Run manual end-to-end tests
	(cd ../cmd/fctl ; ./manual-test.sh)

.PHONY: all-test
all-test: test manual-test ## Run all test targets
