OS = $(shell uname | tr A-Z a-z)

PROJ=holos-console
ORG_PATH=github.com/holos-run
REPO_PATH=$(ORG_PATH)/$(PROJ)

VERSION := $(shell cat console/version/major console/version/minor console/version/patch | xargs printf "%s.%s.%s")
BIN_NAME := holos-console

GIT_COMMIT=$(shell git rev-parse HEAD)
GIT_SUFFIX=$(shell test -n "`git status --porcelain`" && echo "-dirty" || echo "")
GIT_DETAIL=$(shell git describe --tags HEAD 2>/dev/null || echo "dev")
GIT_TREE_STATE=$(shell test -n "`git status --porcelain`" && echo "dirty" || echo "clean")
BUILD_DATE=$(shell date -Iseconds)

LD_FLAGS="-w -X ${ORG_PATH}/${PROJ}/console.GitDescribe=${GIT_DETAIL}${GIT_SUFFIX} -X ${ORG_PATH}/${PROJ}/console.GitCommit=${GIT_COMMIT} -X ${ORG_PATH}/${PROJ}/console.GitTreeState=${GIT_TREE_STATE} -X ${ORG_PATH}/${PROJ}/console.BuildDate=${BUILD_DATE}"

default: build

.PHONY: show-version
show-version: ## Show current version.
	@echo $(VERSION)

.PHONY: tag
tag: ## Create version tag.
	git tag v$(VERSION)

.PHONY: build
build: ## Build executable.
	@echo "building ${BIN_NAME} ${VERSION}"
	@echo "GOPATH=${GOPATH}"
	go build -trimpath -o bin/$(BIN_NAME) -ldflags $(LD_FLAGS) $(REPO_PATH)/cmd

.PHONY: debug
debug: ## Build debug executable.
	@echo "building ${BIN_NAME}-debug ${VERSION}"
	@echo "GOPATH=${GOPATH}"
	go build -o bin/$(BIN_NAME)-debug $(REPO_PATH)/cmd

.PHONY: install
install: build ## Install to GOPATH/bin
	install bin/$(BIN_NAME) $(shell go env GOPATH)/bin/$(BIN_NAME)

.PHONY: clean
clean: ## Clean executables.
	@test ! -e bin/${BIN_NAME} || rm bin/${BIN_NAME}
	@test ! -e bin/${BIN_NAME}-debug || rm bin/${BIN_NAME}-debug

.PHONY: fmt
fmt: ## Format code.
	go fmt ./...

.PHONY: vet
vet: ## Vet Go code.
	go vet ./...

.PHONY: lint
lint: vet ## Run linters.
	golangci-lint run

.PHONY: tidy
tidy: ## Tidy go module.
	go mod tidy

.PHONY: test
test: ## Run tests.
	go test -race -coverprofile=coverage.out ./...

.PHONY: coverage
coverage: test ## Test coverage profile.
	go tool cover -html=coverage.out

.PHONY: generate
generate: ## Generate code.
	go generate ./...

.PHONY: certs
certs: ## Generate TLS certificates using mkcert.
	./scripts/certs

.PHONY: run
run: build ## Run the server with generated certificates.
	./bin/$(BIN_NAME) --cert certs/tls.crt --key certs/tls.key

.PHONY: rpc-version
rpc-version: ## Get server version via gRPC.
	./scripts/rpc-version

.PHONY: help
help: ## Display this help menu.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
