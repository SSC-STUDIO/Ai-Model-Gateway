# Build stage
FROM golang:1.25.9-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir -p /src/bin
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /src/bin/aigw ./cmd/aigw
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /src/bin/gatewayd ./cmd/gatewayd
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /src/bin/controld ./cmd/controld
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /src/bin/telemetryd ./cmd/telemetryd
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /src/bin/gateway-cli ./cmd/gateway-cli
RUN /src/bin/aigw bundle build -root /src -out /src/aigw-manifest.json
RUN /src/bin/aigw bundle verify -root /src -manifest /src/aigw-manifest.json
RUN mkdir -p /runtime-web/admin && if [ -d /src/web/admin/dist ]; then cp -R /src/web/admin/dist /runtime-web/admin/dist; fi

# Runtime stage
FROM alpine:3.19 AS runtime
RUN apk add --no-cache ca-certificates tzdata
RUN adduser -D -g '' gateway
WORKDIR /opt/ai-model-gateway
COPY --from=builder /src/configs /opt/ai-model-gateway/configs
COPY --from=builder /src/bin /opt/ai-model-gateway/bin
COPY --from=builder /src/aigw-manifest.json /opt/ai-model-gateway/aigw-manifest.json
COPY --from=builder /runtime-web /opt/ai-model-gateway/web
RUN if [ ! -f /opt/ai-model-gateway/configs/config.yaml ]; then cp /opt/ai-model-gateway/configs/config.example.yaml /opt/ai-model-gateway/configs/config.yaml; fi
RUN mkdir -p /opt/ai-model-gateway/.gateway-runtime && chown -R gateway:gateway /opt/ai-model-gateway
USER gateway
ENTRYPOINT ["/opt/ai-model-gateway/bin/aigw"]
CMD ["supervise", "-runtime-root", "/opt/ai-model-gateway/.gateway-runtime", "-config-dir", "/opt/ai-model-gateway/configs/docker", "-bin-dir", "/opt/ai-model-gateway/bin", "-manifest", "/opt/ai-model-gateway/aigw-manifest.json", "-strict-manifest=true"]
