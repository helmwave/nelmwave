# Adapted from the helmwave Dockerfile. Same layout — a builder, a shared base,
# and one thin stage per distribution channel — with three differences that the
# original shape hides:
#
#   * ARG is re-declared inside every stage that uses it. An ARG above the first
#     FROM is only visible in FROM lines; inside a stage it expands to the empty
#     string, so `WORKDIR ${PROJECT}` and `COPY ${PROJECT} /bin/` are silently
#     wrong there.
#   * ENTRYPOINT spells the path out. Exec-form ENTRYPOINT does not expand
#     variables at all: `ENTRYPOINT ["/bin/${PROJECT}"]` makes a container that
#     dies with `executable file "/bin/${PROJECT}" not found`.
#   * The in-Docker build stamps internal/version through -ldflags, so a binary
#     built here reports a version like `make build` does instead of `dev`.
#
# Targets:
#   goreleaser          alpine + the binary goreleaser already built  (:X.Y.Z, :latest)
#   debug-goreleaser    the above + bash, jq, kubectl                 (:X.Y.Z-debug)
#   scratch-goreleaser  scratch + ca-certs + the binary               (:X.Y.Z-scratch)
#   release             alpine, compiled in Docker (no goreleaser)
#   scratch-release     scratch, compiled in Docker
#
# Build one by hand:
#   docker build --target release -t nelmwave:dev .

ARG GOLANG_VERSION=1.26
ARG ALPINE_VERSION=3.22
ARG KUBECTL_IMAGE=bitnami/kubectl:latest

### kubectl for the debug flavour. A stage has to be declared before a COPY can
### reference it by name, hence the position.
FROM ${KUBECTL_IMAGE} AS kubectl

### A /tmp for the scratch images, which have no directories at all: nelmwave
### writes a temporary OCI config.json when it resolves a chart from a registry.
### 1777 so the unprivileged user can write there. Kept out of `builder` so the
### goreleaser targets never pull the toolchain in just to get a directory.
FROM alpine:${ALPINE_VERSION} AS rootfs

RUN mkdir -p /rootfs/tmp && chmod 1777 /rootfs/tmp

### Builder — only used by the `release` and `scratch-release` targets.
FROM golang:${GOLANG_VERSION}-alpine${ALPINE_VERSION} AS builder

ARG PROJECT=nelmwave
# Build metadata, mirroring the Makefile. Passed in with
# `docker build --build-arg VERSION=$(git describe --tags)`; the defaults match
# internal/version's own fallbacks, so an unstamped build is still honest.
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown

ENV GO111MODULE=on
ENV CGO_ENABLED=0

WORKDIR /src

# Dependencies first: this layer survives every change that does not touch go.mod.
COPY go.mod go.sum ./
RUN go mod download

# nelmwave keeps its packages under internal/, not pkg/ — nothing here is meant
# to be imported by another module.
COPY ./cmd ./cmd
COPY internal internal

# -trimpath keeps absolute build paths out of the binary; the -X flags are the
# same three the Makefile stamps.
RUN go build -trimpath \
      -ldflags "-s -w \
        -X github.com/helmwave/nelmwave/internal/version.Version=${VERSION} \
        -X github.com/helmwave/nelmwave/internal/version.Commit=${COMMIT} \
        -X github.com/helmwave/nelmwave/internal/version.Date=${DATE}" \
      -o "/out/${PROJECT}" "./cmd/${PROJECT}"

### Shared base for every alpine-flavoured image.
FROM alpine:${ALPINE_VERSION} AS base-release

# Helm repositories, OCI registries and the Kubernetes API are all TLS.
RUN apk --update --no-cache add ca-certificates && update-ca-certificates

# nelmwave resolves values and stores relative to the working directory:
#   docker run --rm -v "$PWD:/workspace" ghcr.io/helmwave/nelmwave:latest build
WORKDIR /workspace

# 65534 is nobody: nothing nelmwave does needs root, and HOME must be writable
# because helm caches repository indexes under it.
ENV HOME=/tmp
USER 65534:65534

ENTRYPOINT ["/bin/nelmwave"]
CMD ["--help"]

### Base for the debug flavour. Stays root: the point of this image is poking
### around inside it.
FROM base-release AS base-debug-release

USER root:root
RUN apk --update --no-cache add jq bash

COPY --chown=root:root --chmod=0755 --from=kubectl /opt/bitnami/kubectl/bin/kubectl /bin/kubectl

### Published as :X.Y.Z, :X.Y and :latest — goreleaser hands over the binary it
### already built, so nothing is compiled here.
###
### dockers_v2 builds from a temporary context holding only this Dockerfile and
### the artifacts, laid out as <os>/<arch>/<binary> — hence $TARGETPLATFORM,
### which buildx sets to e.g. `linux/arm64`. A plain `COPY nelmwave /bin/` fails
### with `copier: stat: "/nelmwave": no such file or directory`, and these three
### stages therefore only build under goreleaser. Use `--target release` by hand.
FROM base-release AS goreleaser

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/nelmwave /bin/nelmwave

### Published as :X.Y.Z-debug.
FROM base-debug-release AS debug-goreleaser

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/nelmwave /bin/nelmwave

### `docker build --target release` — no goreleaser involved.
FROM base-release AS release

COPY --from=builder /out/nelmwave /bin/

### Published as :X.Y.Z-scratch. No shell, no package manager, no libc.
FROM scratch AS scratch-release

COPY --from=base-release /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=rootfs /rootfs/tmp /tmp
COPY --from=builder /out/nelmwave /bin/

ENV HOME=/tmp
WORKDIR /workspace
USER 65534:65534

ENTRYPOINT ["/bin/nelmwave"]
CMD ["--help"]

### The scratch image built from the goreleaser binary.
FROM scratch AS scratch-goreleaser

COPY --from=base-release /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=rootfs /rootfs/tmp /tmp

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/nelmwave /bin/nelmwave

ENV HOME=/tmp
WORKDIR /workspace
USER 65534:65534

ENTRYPOINT ["/bin/nelmwave"]
CMD ["--help"]
