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

.PHONY: build test lint examples e2e e2e-up e2e-test e2e-down version demo

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

## examples: build every example, so a schema change cannot leave them stale.
## Needs no cluster: charts are local and repositories are only recorded.
examples: build
	@set -e; for dir in examples/*/; do \
		[ -f "$$dir/nelmwave.yml.tpl" ] || continue; \
		echo "== $$dir"; \
		( cd "$$dir" && ENV=stg SOPS_AGE_KEY_FILE=age.key \
			$(CURDIR)/nelmwave build --log-level warn ); \
	done

## demo: re-record demo/nelmwave.cast (needs asciinema, bat, tree and kubectl).
## Runs the full build/diff/up/down loop against the e2e fixture, so bring it up
## first: `make e2e-up`. demo.sh refuses to record against a non-local cluster.
demo: build
	TERM=xterm-256color asciinema rec demo/nelmwave.cast --overwrite \
		--window-size 100x36 --idle-time-limit 2 \
		-t 'nelmwave — declarative release orchestrator on top of nelm' \
		-c 'bash demo/demo.sh'

## e2e: bring the cluster up, run the suite, tear it down (even on failure).
e2e:
	$(MAKE) e2e-up
	$(MAKE) e2e-test; status=$$?; $(MAKE) e2e-down; exit $$status

## e2e-up: start the k3s fixture and wait for its healthcheck to pass.
# The kubeconfig directory is created here, not by the bind mount: a rootful
# docker creates a missing mount source as root:root, and then nothing outside
# the container can delete the kubeconfig k3s drops into it — e2e-down fails with
# "Permission denied". Owning the directory ourselves keeps teardown possible,
# since removing a file needs write permission on its directory, not on the file.
e2e-up:
	mkdir -p $(dir $(KUBECONFIG_PATH))
	$(COMPOSE) -f $(COMPOSE_FILE) up -d --wait

## e2e-test: run the suite against the running fixture.
e2e-test:
	KUBECONFIG=$(abspath $(KUBECONFIG_PATH)) go test -tags e2e -count=1 -timeout 20m ./test/e2e/...

## e2e-down: remove the fixture and its volumes.
e2e-down:
	$(COMPOSE) -f $(COMPOSE_FILE) down -v
	rm -rf $(dir $(KUBECONFIG_PATH))
