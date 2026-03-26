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
TEST_LDFLAGS=
ifeq ($(OS),darwin)
TEST_LDFLAGS=-ldflags=-linkmode=internal
endif

default: build

# Ensure frontend/node_modules exists. Runs npm install on fresh clones.
frontend/node_modules:
	cd frontend && npm install

# Ensure console/dist exists for go:embed. Order-only prerequisite (|) means
# Make only checks existence, not timestamps. Runs generate on fresh clones.
console/dist: | frontend/node_modules
	$(MAKE) generate

.PHONY: show-version
show-version: ## Show current version.
	@echo $(VERSION)

.PHONY: tag
tag: ## Create version tag.
	git tag v$(VERSION)

.PHONY: build
build: | console/dist ## Build executable.
	@echo "building ${BIN_NAME} ${VERSION}"
	@echo "GOPATH=${GOPATH}"
	go build -trimpath -o bin/$(BIN_NAME) -ldflags $(LD_FLAGS) $(REPO_PATH)/cmd

.PHONY: build-binary
build-binary: ## Build executable without UI prerequisites (for use in Dockerfile Go stage).
	@echo "building ${BIN_NAME} ${VERSION}"
	@echo "GOPATH=${GOPATH}"
	go build -trimpath -o bin/$(BIN_NAME) -ldflags $(LD_FLAGS) $(REPO_PATH)/cmd

.PHONY: debug
debug: | console/dist ## Build debug executable.
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

.PHONY: tools
tools: frontend/node_modules ## Install tool dependencies.
	go install $$(go list -e -f '{{range .Imports}}{{.}} {{end}}' tools.go)

.PHONY: agent-tools
agent-tools: ## Install agent-browser for AI agent browser automation.
	npm install -g agent-browser
	agent-browser install

.PHONY: test
test: test-go test-ui ## Run tests.

.PHONY: test-go
test-go: | console/dist ## Run Go tests.
	CGO_ENABLED=1 go test -race -coverprofile=coverage.out $(TEST_LDFLAGS) ./...

.PHONY: test-ui
test-ui: | frontend/node_modules ## Run UI tests.
	cd frontend && npm test -- --run

.PHONY: test-e2e
test-e2e: build ## Run Playwright E2E tests (orchestrates servers automatically).
	cd frontend && npm run test:e2e

.PHONY: coverage
coverage: test ## Test coverage profile.
	go tool cover -html=coverage.out

.PHONY: generate
generate: ## Generate protobuf code and build frontend.
	go generate ./...
	cd frontend && npm run build

.PHONY: certs
certs: ## Generate TLS certificates using mkcert.
	./scripts/certs

.PHONY: run
run: build ## Run the server with generated certificates.
	./bin/$(BIN_NAME) --enable-insecure-dex --cert certs/tls.crt --key certs/tls.key --ca-cert "$$(mkcert -CAROOT)/rootCA.pem"

.PHONY: dev
dev: ## Start the Vite dev server for frontend development.
	./scripts/dev

.PHONY: rpc-version
rpc-version: ## Get server version via gRPC.
	./scripts/rpc-version

.PHONY: dispatch
dispatch: ## Create worktree and spawn Claude Code agent for a GitHub issue.
	./scripts/dispatch $(ISSUE)

# Container image configuration
DOCKER_REPO ?= ghcr.io/holos-run/holos-console
GIT_SHA := $(shell git rev-parse --short HEAD)
IMAGE_TAG ?= $(VERSION)-$(GIT_SHA)
PLATFORMS ?= linux/amd64,linux/arm64

.PHONY: docker-build
docker-build: ## Build container image for current platform.
	docker build --load -t $(DOCKER_REPO):$(IMAGE_TAG) .
	docker tag $(DOCKER_REPO):$(IMAGE_TAG) $(DOCKER_REPO):latest

.PHONY: docker-buildx
docker-buildx: ## Build multi-platform container images (amd64, arm64).
	docker buildx build --platform $(PLATFORMS) -t $(DOCKER_REPO):$(IMAGE_TAG) -t $(DOCKER_REPO):latest .

.PHONY: docker-push
docker-push: ## Build and push multi-platform container images.
	docker buildx build --platform $(PLATFORMS) -t $(DOCKER_REPO):$(IMAGE_TAG) -t $(DOCKER_REPO):latest --push .

.PHONY: cluster
cluster: ## Create local k3d cluster (DNS + cluster + CA).
	./scripts/local-dns
	./scripts/local-k3d
	./scripts/local-ca

.PHONY: help
help: ## Display this help menu.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
