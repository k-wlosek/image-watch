# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.25.13-alpine3.24 AS build

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
RUN CGO_ENABLED=0 \
  GOOS=$TARGETOS \
  GOARCH=$TARGETARCH \
  GOARM=${TARGETVARIANT#v} \
  go build \
  -trimpath \
  -ldflags="-s -w -X main.version=${VERSION}" \
  -o /out/image-watch \
  ./cmd/image-watch

FROM scratch

# Copy CA certificates for HTTPS registry access.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/image-watch /image-watch

# HEALTHCHECK invokes the binary directly because scratch has no shell.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD ["/image-watch", "healthcheck"]

ENTRYPOINT ["/image-watch", "daemon"]
