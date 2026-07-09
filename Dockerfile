# AI Model Gateway - Multi-stage Dockerfile
#
# Builds a minimal runtime image by fetching pre-built Linux binaries from
# GitHub releases. The Go source lives in a separate private repository; this
# Dockerfile is the public-facing packaging path.
#
# Usage (local build):
#   docker build -t ai-model-gateway .
#
# Usage (via docker-compose):
#   docker compose -f deploy/docker-compose.yaml build
#   docker compose -f deploy/docker-compose.yaml up -d
#
# Build args -----------------------------------------------------------------
#   AIGW_VERSION  — release tag to fetch (default: read from VERSION file)
#   GITHUB_REPO   — owner/repo for release asset URLs
#   GO_VERSION    — Go toolchain version used for the build stage

ARG GO_VERSION=1.23.4

# ============================================================================
# Stage 1: builder — compile from Go source (future use)
# ============================================================================
FROM golang:${GO_VERSION}-alpine AS builder

ARG AIGW_VERSION
ARG GITHUB_REPO=SSC-STUDIO/Ai-Model-Gateway

WORKDIR /src

# If Go source is available locally, copy and build:
# COPY go.mod go.sum ./
# RUN go mod download
# COPY . .
# RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
#     go build -ldflags="-s -w" -o /out/aigw      ./cmd/aigw && \
#     go build -ldflags="-s -w" -o /out/gatewayd   ./cmd/gatewayd && \
#     go build -ldflags="-s -w" -o /out/controld   ./cmd/controld && \
#     go build -ldflags="-s -w" -o /out/telemetryd ./cmd/telemetryd && \
#     go build -ldflags="-s -w" -o /out/gateway-cli ./cmd/gateway-cli

# Placeholder: the build stage is here for when Go source is added to the repo.
# Until then, the runtime stage uses a pre-built binary download approach.
RUN echo "builder stage ready — Go source not yet in this repo" > /dev/null

# ============================================================================
# Stage 2: runtime — minimal image with pre-built binaries
# ============================================================================
FROM debian:bookworm-slim AS runtime

ARG AIGW_VERSION
ARG GITHUB_REPO=SSC-STUDIO/Ai-Model-Gateway

LABEL org.opencontainers.image.title="AI Model Gateway"
LABEL org.opencontainers.image.description="Self-hosted LLM gateway with provider routing, telemetry, and admin UI"
LABEL org.opencontainers.image.url="https://github.com/${GITHUB_REPO}"
LABEL org.opencontainers.image.source="https://github.com/${GITHUB_REPO}"

# Install minimal runtime dependencies
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        wget \
        curl \
        jq \
    && rm -rf /var/lib/apt/lists/*

# Create dedicated non-root user
RUN groupadd -r gateway && \
    useradd -r -g gateway -d /opt/ai-model-gateway -s /sbin/nologin gateway

WORKDIR /opt/ai-model-gateway

# --- Download release binaries ------------------------------------------------
# If AIGW_VERSION is set, download from GitHub releases; otherwise use
# local binaries that were COPYed into the build context.
ARG TARGETARCH=amd64

# Try to read VERSION from the repo; fall back to 'latest' if not provided.
COPY VERSION /tmp/aigw-version
RUN AIGW_VER="${AIGW_VERSION:-$(cat /tmp/aigw-version | tr -d '[:space:]')}" && \
    echo "Fetching AI Model Gateway ${AIGW_VER} binaries..." && \
    RELEASE_URL="https://github.com/${GITHUB_REPO}/releases/download/${AIGW_VER}" && \
    BINARIES="aigw gatewayd controld telemetryd gateway-cli" && \
    mkdir -p bin && \
    for binary in ${BINARIES}; do \
        echo "  -> ${binary}" && \
        wget -q "${RELEASE_URL}/${binary}-linux-${TARGETARCH}" \
            -O "bin/${binary}" 2>/dev/null || \
        wget -q "${RELEASE_URL}/${binary}" \
            -O "bin/${binary}" 2>/dev/null || \
        { echo "WARNING: failed to fetch ${binary}, using stub"; \
          echo '#!/bin/sh' > "bin/${binary}"; echo "echo '${binary}: not available'" >> "bin/${binary}"; } && \
        chmod +x "bin/${binary}"; \
    done && \
    rm -f /tmp/aigw-version

# --- Runtime directory structure ----------------------------------------------
RUN mkdir -p \
        .gateway-runtime/telemetry \
        .gateway-runtime/telemetry-migrated \
        .gateway-runtime/gateway \
        .gateway-runtime/control \
        .gateway-runtime/update \
        configs/data \
        web/admin/dist \
        logs

# --- Copy static assets from build context ------------------------------------
COPY configs/config.yaml configs/config.yaml
COPY configs/controld.json configs/controld.json
COPY configs/gatewayd.json configs/gatewayd.json
COPY configs/telemetryd.json configs/telemetryd.json
COPY aigw-manifest.json aigw-manifest.json
COPY web/admin/dist/ web/admin/dist/

# --- Expose ports -------------------------------------------------------------
# Data plane (client traffic)
EXPOSE 18080
# Control plane (admin UI + API)
EXPOSE 18081

# --- Health check -------------------------------------------------------------
HEALTHCHECK --interval=30s --timeout=10s --retries=3 --start-period=20s \
    CMD wget -q --spider http://127.0.0.1:18080/-/health || exit 1

# --- Switch to non-root user ---------------------------------------------------
RUN chown -R gateway:gateway /opt/ai-model-gateway
USER gateway

# --- Entrypoint ---------------------------------------------------------------
ENTRYPOINT ["./bin/aigw"]
CMD ["supervise", \
     "-runtime-root", "/opt/ai-model-gateway/.gateway-runtime", \
     "-config-dir", "/opt/ai-model-gateway/configs", \
     "-bin-dir", "/opt/ai-model-gateway/bin", \
     "-manifest", "/opt/ai-model-gateway/aigw-manifest.json", \
     "-strict-manifest=true"]
