OS = $(shell uname | tr A-Z a-z)

PROJ=holos-console
ORG_PATH=github.com/holos-run
REPO_PATH=$(ORG_PATH)/$(PROJ)

VERSION := $(shell cat console/version/major console/version/minor console/version/patch | xargs printf "%s.%s.%s")
BIN_NAME := holos-console
INJECTOR_BIN_NAME := holos-secret-injector

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

.PHONY: bump-major
bump-major: ## Bump major version (resets minor and patch to 0).
	@echo $$(( $(shell cat console/version/major) + 1 )) > console/version/major
	@echo 0 > console/version/minor
	@echo 0 > console/version/patch
	@echo "Version bumped to $$(cat console/version/major).$$(cat console/version/minor).$$(cat console/version/patch)"

.PHONY: bump-minor
bump-minor: ## Bump minor version (resets patch to 0).
	@echo $$(( $(shell cat console/version/minor) + 1 )) > console/version/minor
	@echo 0 > console/version/patch
	@echo "Version bumped to $$(cat console/version/major).$$(cat console/version/minor).$$(cat console/version/patch)"

.PHONY: bump-patch
bump-patch: ## Bump patch version.
	@echo $$(( $(shell cat console/version/patch) + 1 )) > console/version/patch
	@echo "Version bumped to $$(cat console/version/major).$$(cat console/version/minor).$$(cat console/version/patch)"

.PHONY: tag
tag: ## Create annotated version tag from embedded version files.
	@if git rev-parse "v$(VERSION)" >/dev/null 2>&1; then \
		echo "Error: tag v$(VERSION) already exists" >&2; exit 1; \
	fi
	@if [ "$$(git status --porcelain)" != "" ]; then \
		echo "Error: working tree is dirty, commit changes first" >&2; exit 1; \
	fi
	git tag -a "v$(VERSION)" -m "Release v$(VERSION)"
	@echo "Created tag v$(VERSION)"

.PHONY: build
build: build-console build-injector ## Build both binaries (holos-console, holos-secret-injector).

.PHONY: build-console
build-console: | console/dist ## Build the holos-console executable.
	@echo "building ${BIN_NAME} ${VERSION}"
	@echo "GOPATH=${GOPATH}"
	go build -trimpath -o bin/$(BIN_NAME) -ldflags $(LD_FLAGS) $(REPO_PATH)/cmd/holos-console

.PHONY: build-injector
# build-injector depends on console/dist because cmd/secret-injector imports
# github.com/holos-run/holos-console/console for GetVersion(), and
# console/console.go has `//go:embed all:dist`. Without the prerequisite,
# `make build-injector` and `make -j build` would fail on fresh checkouts
# before the frontend has been generated. Dockerfile.secret-injector
# (HOL-689) takes a different path: its Go build stage creates an empty
# `console/dist/` directly so the embed directive resolves without the
# Vite pipeline. The host-side `make build-injector` retains the
# prerequisite because developers usually already have `console/dist`
# from a prior `make generate`.
build-injector: | console/dist ## Build the holos-secret-injector executable.
	@echo "building ${INJECTOR_BIN_NAME} ${VERSION}"
	@echo "GOPATH=${GOPATH}"
	go build -trimpath -o bin/$(INJECTOR_BIN_NAME) -ldflags $(LD_FLAGS) $(REPO_PATH)/cmd/secret-injector

.PHONY: build-binary
build-binary: ## Build holos-console without UI prerequisites (for use in Dockerfile Go stage).
	@echo "building ${BIN_NAME} ${VERSION}"
	@echo "GOPATH=${GOPATH}"
	go build -trimpath -o bin/$(BIN_NAME) -ldflags $(LD_FLAGS) $(REPO_PATH)/cmd/holos-console

.PHONY: debug
debug: | console/dist ## Build debug executable.
	@echo "building ${BIN_NAME}-debug ${VERSION}"
	@echo "GOPATH=${GOPATH}"
	go build -o bin/$(BIN_NAME)-debug $(REPO_PATH)/cmd/holos-console

.PHONY: install
install: build ## Install both binaries to GOPATH/bin
	install bin/$(BIN_NAME) $(shell go env GOPATH)/bin/$(BIN_NAME)
	install bin/$(INJECTOR_BIN_NAME) $(shell go env GOPATH)/bin/$(INJECTOR_BIN_NAME)

.PHONY: clean
clean: ## Clean executables.
	@test ! -e bin/${BIN_NAME} || rm bin/${BIN_NAME}
	@test ! -e bin/${BIN_NAME}-debug || rm bin/${BIN_NAME}-debug
	@test ! -e bin/${INJECTOR_BIN_NAME} || rm bin/${INJECTOR_BIN_NAME}

.PHONY: fmt
fmt: ## Format code.
	go fmt ./...

.PHONY: vet
vet: ## Vet Go code.
	go vet ./...

.PHONY: check-imports
# check-imports enforces ADR 031's disjoint-tree invariant: the holos-console
# tree (internal/controller/...) and the holos-secret-injector tree
# (internal/secretinjector/...) must never import each other. A violation
# would reunify the two binaries through a shared dependency and defeat the
# whole point of the split. The grep checks only real Go import lines
# (double-quoted module paths) so doc-comment references like "internal/
# controller/... and vice versa" do not trip the check.
check-imports: ## Enforce ADR 031's disjoint-tree invariant across internal/controller and internal/secretinjector.
	@if grep -rn '"github.com/holos-run/holos-console/internal/controller' internal/secretinjector/ 2>/dev/null; then \
		echo "check-imports: internal/secretinjector imports internal/controller (ADR 031 violation)" >&2; \
		exit 1; \
	fi
	@if grep -rn '"github.com/holos-run/holos-console/internal/secretinjector' internal/controller/ 2>/dev/null; then \
		echo "check-imports: internal/controller imports internal/secretinjector (ADR 031 violation)" >&2; \
		exit 1; \
	fi

.PHONY: lint
lint: vet check-imports ## Run linters.
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

.PHONY: test-secret-injector
# test-secret-injector runs the Go race detector across the secret-injector's
# disjoint source tree: cmd/secret-injector/... + internal/secretinjector/... +
# api/secrets/... . No test files live under these paths at the end of M0, so
# `go test -race` exits 0 with a "no test files" notice per package. M1+ adds
# real tests without changing this target's shape. See ADR 031 for the disjoint
# tree rationale.
test-secret-injector: ## Run holos-secret-injector Go tests with the race detector.
	CGO_ENABLED=1 go test -race ./cmd/secret-injector/... ./internal/secretinjector/... ./api/secrets/...

.PHONY: test-ui
test-ui: | frontend/node_modules ## Run UI tests.
	cd frontend && npm test -- --run

.PHONY: test-e2e
test-e2e: build ## Run Playwright E2E tests (orchestrates servers automatically).
	cd frontend && npm run test:e2e

.PHONY: coverage
coverage: test ## Test coverage profile.
	go tool cover -html=coverage.out

.PHONY: manifests
# manifests invokes controller-gen twice so each binary's API group lands in a
# separate config/ tree. Per ADR 031, the templates group (owned by
# holos-console) keeps config/{crd,rbac}/, and the secrets group (owned by
# holos-secret-injector) lands under config/secret-injector/{crd,rbac}/. The
# second invocation emits an empty config/secret-injector/crd/ during M0
# because no kinds exist yet — M1 adds SecretRequest et al. without any
# Makefile change.
manifests: ## Generate CRD, RBAC, and deepcopy sources from +kubebuilder markers.
	controller-gen \
		crd \
		rbac:roleName=holos-console-templates \
		object:headerFile="hack/boilerplate.go.txt" \
		paths="./api/templates/..." \
		paths="./internal/controller/..." \
		output:crd:artifacts:config=config/crd \
		output:rbac:artifacts:config=config/rbac
	controller-gen \
		crd \
		rbac:roleName=holos-secret-injector \
		object:headerFile="hack/boilerplate.go.txt" \
		paths="./api/secrets/..." \
		paths="./internal/secretinjector/controller/..." \
		output:crd:artifacts:config=config/secret-injector/crd \
		output:rbac:artifacts:config=config/secret-injector/rbac

.PHONY: generate
generate: manifests ## Generate protobuf code, CRD manifests, and build frontend.
	go generate ./...
	cd frontend && npm run build

.PHONY: certs
certs: ## Generate TLS certificates using mkcert.
	./scripts/certs

.PHONY: run
run: ## Build and run the server with generated certificates.
	./scripts/run

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
docker-build: docker-build-console docker-build-injector ## Build both holos-console and holos-secret-injector container images.

.PHONY: docker-build-console
docker-build-console: ## Build holos-console container image for current platform.
	docker build --load -f Dockerfile.console -t $(DOCKER_REPO):$(IMAGE_TAG) .
	docker tag $(DOCKER_REPO):$(IMAGE_TAG) $(DOCKER_REPO):latest

.PHONY: docker-build-injector
docker-build-injector: ## Build holos-secret-injector container image for current platform.
	docker build --load -f Dockerfile.secret-injector -t $(DOCKER_REPO)-secret-injector:$(IMAGE_TAG) .
	docker tag $(DOCKER_REPO)-secret-injector:$(IMAGE_TAG) $(DOCKER_REPO)-secret-injector:latest

.PHONY: docker-buildx
docker-buildx: docker-buildx-console docker-buildx-injector ## Build multi-platform images for both binaries (amd64, arm64).

.PHONY: docker-buildx-console
docker-buildx-console: ## Build multi-platform holos-console image (amd64, arm64).
	docker buildx build --platform $(PLATFORMS) -f Dockerfile.console -t $(DOCKER_REPO):$(IMAGE_TAG) -t $(DOCKER_REPO):latest .

.PHONY: docker-buildx-injector
docker-buildx-injector: ## Build multi-platform holos-secret-injector image (amd64, arm64).
	docker buildx build --platform $(PLATFORMS) -f Dockerfile.secret-injector -t $(DOCKER_REPO)-secret-injector:$(IMAGE_TAG) -t $(DOCKER_REPO)-secret-injector:latest .

.PHONY: docker-push
docker-push: ## Build and push multi-platform container images for both binaries.
	docker buildx build --platform $(PLATFORMS) -f Dockerfile.console -t $(DOCKER_REPO):$(IMAGE_TAG) -t $(DOCKER_REPO):latest --push .
	docker buildx build --platform $(PLATFORMS) -f Dockerfile.secret-injector -t $(DOCKER_REPO)-secret-injector:$(IMAGE_TAG) -t $(DOCKER_REPO)-secret-injector:latest --push .

.PHONY: cluster
cluster: ## Create local k3d cluster (DNS + cluster + CA).
	./scripts/local-dns
	./scripts/local-k3d
	./scripts/local-ca

.PHONY: kind-up
kind-up: ## Create cluster, install CRDs, admission policies, and RBAC.
	./scripts/kind-up

.PHONY: help
help: ## Display this help menu.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
