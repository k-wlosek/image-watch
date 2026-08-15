# syntax=docker/dockerfile:1
FROM golang:1.25.13-alpine3.24 AS build

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 \
    GOOS=linux \
    go build \
      -trimpath \
      -ldflags="-s -w -X main.version=$(cat VERSION 2>/dev/null || echo dev)" \
      -o /out/image-watch \
      ./cmd/image-watch

FROM scratch

# Copy CA certificates for HTTPS registry access.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/image-watch /image-watch

# Run as a non-root UID/GID.
USER 65532:65532

# HEALTHCHECK invokes the binary directly because scratch has no shell.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD ["/image-watch", "healthcheck"]

ENTRYPOINT ["/image-watch", "daemon"]
