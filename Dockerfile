# Build stage
FROM golang:1.22-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /gatewayd ./cmd/gatewayd
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /controld ./cmd/controld
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /telemetryd ./cmd/telemetryd

# Runtime stage
FROM alpine:3.19 AS runtime
RUN apk add --no-cache ca-certificates tzdata
RUN adduser -D -g '' gateway
USER gateway
COPY --from=builder /gatewayd /controld /telemetryd /bin/
ENTRYPOINT ["/bin/gatewayd"]