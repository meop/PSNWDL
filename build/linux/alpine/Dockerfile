# syntax=docker/dockerfile:1

# APK packages target Alpine Linux, which uses musl rather than glibc. Build the
# Wails binary inside Alpine so the package never contains the Ubuntu binary.
#
# GO_VERSION comes from go.mod's toolchain line (CI passes it). ALPINE_VERSION
# is the oldest Alpine release the package supports.
ARG GO_VERSION
ARG ALPINE_VERSION=3.23
FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder

ARG PNPM_VERSION
ARG NFPM_VERSION
ARG TARGETARCH
ARG WAILS_VERSION

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
