# With podman, point compose at its socket before running the e2e targets:
#   export DOCKER_HOST="unix://$(podman machine inspect \
#     --format '{{.ConnectionInfo.PodmanSocket.Path}}')"
COMPOSE ?= docker-compose
COMPOSE_FILE := test/e2e/docker-compose.yml
KUBECONFIG_PATH := test/e2e/.kube/kubeconfig.yaml

.PHONY: build test lint e2e e2e-up e2e-test e2e-down

build:
	go build ./cmd/nelmwave

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
