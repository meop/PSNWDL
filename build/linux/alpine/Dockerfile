# syntax=docker/dockerfile:1

# APK packages target Alpine Linux, which uses musl rather than glibc. Build the
# Wails binary inside Alpine so the package never contains the Ubuntu binary.
FROM golang:1.25-alpine3.22 AS builder

ARG PNPM_VERSION=11.18.0
ARG NFPM_VERSION=v2.47.0
ARG TARGETARCH
ARG WAILS_VERSION=v3.0.0-alpha2.121

RUN apk add --no-cache \
      build-base \
      git \
      gtk4.0-dev \
      nodejs \
      npm \
      pkgconf \
      webkit2gtk-6.0-dev \
    && npm install --global "pnpm@${PNPM_VERSION}" \
    && go install "github.com/wailsapp/wails/v3/cmd/wails3@${WAILS_VERSION}" \
    && go install "github.com/goreleaser/nfpm/v2/cmd/nfpm@${NFPM_VERSION}"

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN pnpm --dir frontend install --frozen-lockfile \
    && wails3 task linux:create:apk "ARCH=${TARGETARCH}" \
    && mkdir -p /out \
    && cp bin/*.apk /out/

FROM scratch AS package
COPY --from=builder /out/ /
