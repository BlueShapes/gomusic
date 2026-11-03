# syntax=docker/dockerfile:1

# ---------------------------------------------------------------- build ----
# The bot itself is pure Go: dave-go (DAVE/E2EE), pion/opus and kazzmir/opus-go
# are all cgo-free, so the binary is fully static.
# BUILDPLATFORM keeps the Go compiler native when cross-building for arm64/amd64
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags "-s -w" -o /out/gomusic .

# -------------------------------------------------------------- runtime ----
# node is here to be yt-dlp's JS runtime for YouTube's EJS challenges.
# yt-dlp's PyInstaller binaries bundle the EJS scripts themselves, so python is
# not needed at runtime.
FROM node:24-bookworm-slim

ARG TARGETARCH
# Pin this: bump it deliberately, and roll back by changing one line.
ARG YTDLP_VERSION=2026.08.19

RUN set -eux; \
    case "${TARGETARCH:-amd64}" in \
      amd64) asset=yt-dlp_linux ;; \
      arm64) asset=yt-dlp_linux_aarch64 ;; \
      *) echo "unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    apt-get update; \
    apt-get install -y --no-install-recommends ffmpeg ca-certificates curl; \
    curl -fsSL -o /usr/local/bin/yt-dlp \
      "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/${asset}"; \
    chmod +x /usr/local/bin/yt-dlp; \
    apt-get purge -y curl; \
    apt-get autoremove -y; \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app
# the binary stays outside /app so a bind mount over /app cannot hide it
COPY --from=build /out/gomusic /usr/local/bin/gomusic

# /app is usually a bind mount (config.toml + cache/); /cache is kept for setups
# that mount a named volume there instead.
RUN mkdir -p /app /cache && chown node:node /app /cache

USER node

# yt-dlp + node peak at ~430MB RSS per extraction; see compose mem_limit
ENTRYPOINT ["/usr/local/bin/gomusic"]
