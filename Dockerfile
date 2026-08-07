# syntax=docker/dockerfile:1.7

# Build the frontend independently so changes to the Go module do not
# invalidate the dependency install layer.
FROM oven/bun:1@sha256:0733e50325078969732ebe3b15ce4c4be5082f18c4ac1a0f0ca4839c2e4e42a7 AS frontend-builder

WORKDIR /build/web
COPY web/package.json web/bun.lock ./
RUN --mount=type=cache,target=/root/.bun/install/cache \
    bun install --frozen-lockfile
COPY web/ ./
COPY VERSION /build/VERSION
RUN DISABLE_ESLINT_PLUGIN='true' \
    VITE_REACT_APP_VERSION="$(cat /build/VERSION)" \
    bun run build

# Build a static binary on the build platform. BuildKit's cache mounts keep
# the Go module and compiler caches outside the image layers and make repeated
# release builds substantially faster.
FROM --platform=$BUILDPLATFORM golang:1.26.1-alpine@sha256:2389ebfa5b7f43eeafbd6be0c3700cc46690ef842ad962f6c5bd6be49ed82039 AS go-builder

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOWORK=off \
    GOEXPERIMENT=greenteagc

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG GOAMD64
ENV GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    GOAMD64=${GOAMD64}

WORKDIR /build

COPY go.mod go.sum ./
# relaykit is a local module referenced through replace; both module manifests
# must be present before downloading the root module graph.
COPY relaykit/go.mod relaykit/go.sum ./relaykit/
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
COPY --from=frontend-builder /build/web/dist ./web/dist
RUN --mount=type=cache,target=/root/.cache/go-build \
    version="$(cat VERSION)" && \
    go build \
      -trimpath \
      -buildvcs=false \
      -tags netgo,osusergo \
      -ldflags="-s -w -X github.com/QuantumNous/new-api/common.Version=${version}" \
      -o /out/new-api .

# Alpine keeps the runtime image small while retaining CA roots, timezone
# data, and BusyBox wget used by the compose healthcheck. The binary is fully
# static, so no compiler or libc runtime is needed here.
FROM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce AS runtime

RUN apk add --no-cache ca-certificates tzdata

COPY --from=go-builder /out/new-api /new-api
COPY LICENSE NOTICE THIRD-PARTY-LICENSES.md /licenses/

EXPOSE 3000
WORKDIR /data
STOPSIGNAL SIGTERM
ENTRYPOINT ["/new-api"]
