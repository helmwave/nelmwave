# With podman, point compose at its socket before running the e2e targets:
#   export DOCKER_HOST="unix://$(podman machine inspect \
#     --format '{{.ConnectionInfo.PodmanSocket.Path}}')"
COMPOSE ?= docker-compose
COMPOSE_FILE := test/e2e/docker-compose.yml
KUBECONFIG_PATH := test/e2e/.kube/kubeconfig.yaml

# Build metadata stamped into the binary (see internal/version). Every value is
# overridable, so a release pipeline can pass its own — e.g.
# `make build DATE=$(git log -1 --format=%cs)` for a build that reproduces
# byte-for-byte from the same commit.
#
# Each falls back to a constant when git is unavailable (source tarball, Docker
# build without .git), so `make build` never fails over metadata.
VERSION ?= $(shell git describe --tags --dirty 2>/dev/null || echo dev)
# Date only: the time of day says nothing useful about a build, and it makes two
# builds of the same commit look different.
DATE ?= $(shell date -u +%Y-%m-%d)

# Resolved in two steps on purpose: outside a git repository the dirty check
# fails too, and appending its result unconditionally would stamp "none-dirty".
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null)
ifeq ($(GIT_COMMIT),)
COMMIT ?= none
else
# A trailing -dirty marks a binary built with uncommitted changes: without it,
# the recorded commit would claim the build matches that revision. `diff HEAD`
# covers staged changes too, which plain `git diff` misses.
COMMIT ?= $(GIT_COMMIT)$(shell git diff --quiet HEAD 2>/dev/null || echo -dirty)
endif

VERSION_PKG := github.com/helmwave/nelmwave/internal/version
LDFLAGS := -X $(VERSION_PKG).Version=$(VERSION) \
           -X $(VERSION_PKG).Commit=$(COMMIT) \
           -X $(VERSION_PKG).Date=$(DATE)

.PHONY: build test lint e2e e2e-up e2e-test e2e-down version

build:
	go build -ldflags '$(LDFLAGS)' ./cmd/nelmwave

## version: print the metadata `make build` would stamp in, without building.
version:
	@echo "version=$(VERSION)"
	@echo "commit=$(COMMIT)"
	@echo "date=$(DATE)"

test:
	go test ./...

lint:
	golangci-lint run ./...

## e2e: bring the cluster up, run the suite, tear it down (even on failure).
e2e:
	$(MAKE) e2e-up
	$(MAKE) e2e-test; status=$$?; $(MAKE) e2e-down; exit $$status

## e2e-up: start the k3s fixture and wait for its healthcheck to pass.
e2e-up:
	$(COMPOSE) -f $(COMPOSE_FILE) up -d --wait

## e2e-test: run the suite against the running fixture.
e2e-test:
	KUBECONFIG=$(abspath $(KUBECONFIG_PATH)) go test -tags e2e -count=1 -timeout 20m ./test/e2e/...

## e2e-down: remove the fixture and its volumes.
e2e-down:
	$(COMPOSE) -f $(COMPOSE_FILE) down -v
	rm -rf test/e2e/.kube
