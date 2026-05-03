# syntax=docker/dockerfile:1.7
#
# Two-stage build that produces a tiny, scratch-grade runtime image.
#   1. builder — full Go toolchain, downloads modules, compiles a static
#      binary. Heavy (~1GB) but only lives during build.
#   2. runtime — distroless/static, ~2MB. No shell, no package manager, no
#      glibc — just ca-certs + tzdata + our binary running as a non-root user.
#
# CGO_ENABLED=0 keeps the binary fully static so distroless/static (which
# lacks libc) is enough. If you ever add a CGO dependency, switch the runtime
# base to gcr.io/distroless/cc-debian12:nonroot.

# ---------- builder ----------
FROM --platform=$BUILDPLATFORM golang:1.26.1-alpine AS builder

# Build args carry version metadata into the binary via -ldflags. Override at
# build time:  docker build --build-arg VERSION=v1.2.3 .
ARG VERSION=dev
ARG COMMIT=unknown
ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

# Download modules in a separate layer so a code-only change doesn't bust
# the dependency cache. The two COPY lines + go mod download are the
# expensive bit; keep them above the rest of the COPY.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

# -trimpath strips local paths from the binary (reproducibility + privacy).
# -s -w drop the symbol/DWARF tables (smaller binary, no debugger output).
# -X stamps version info even though the app currently reads ServiceVersion
# from config — useful for `--version` style flags later.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build \
        -trimpath \
        -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
        -o /out/url-shortener \
        ./cmd/api

# ---------- runtime ----------
FROM gcr.io/distroless/static-debian12:nonroot

# Distroless `static` already includes:
#   - /etc/ssl/certs/ca-certificates.crt
#   - /etc/passwd with a `nonroot` user (UID 65532)
#   - /usr/share/zoneinfo
# So the only thing we need to bring in is the binary.
COPY --from=builder /out/url-shortener /usr/local/bin/url-shortener

# 8080: API. 6060: pprof + /metrics. Operators can choose to publish only
# 8080 in production and reach 6060 via a port-forward / sidecar.
EXPOSE 8080 6060

USER nonroot:nonroot

# Distroless has no shell, so this is the exec form (no /bin/sh involved).
ENTRYPOINT ["/usr/local/bin/url-shortener"]
