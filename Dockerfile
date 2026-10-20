# syntax=docker/dockerfile:1.7
# ---------- 1. build the web UI ----------
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---------- 2. build the Go binary ----------
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/pocket-notes ./cmd/pocket-notes

# ---------- 3. runtime ----------
# yt-dlp (latest from PyPI, with its JS-challenge helper), deno as the JavaScript
# runtime YouTube now requires (node where Alpine has no deno build; the app
# passes --js-runtimes node then), ffmpeg for audio extraction. `pip --target` into
# DATA_DIR/ytdlp (Settings → Feeds & downloads → Update) keeps yt-dlp current
# without rebuilding the image.
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata curl python3 py3-pip ffmpeg \
 && (apk add --no-cache deno || apk add --no-cache nodejs) \
 && pip install --no-cache-dir --break-system-packages 'yt-dlp[default]' \
 && addgroup -g 1000 app && adduser -u 1000 -G app -D -h /data app
COPY --from=build /out/pocket-notes /usr/local/bin/pocket-notes
ENV DATA_DIR=/data PORT=8080 HOST=0.0.0.0
VOLUME ["/data"]
EXPOSE 8080
USER app
WORKDIR /data
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 CMD curl -fsS http://localhost:8080/healthz || exit 1
LABEL org.opencontainers.image.title="Pocket Notes" \
      org.opencontainers.image.description="Self-hosted podcast server: subscriptions, player, downloads, YouTube channels as podcasts, public feeds." \
      org.opencontainers.image.source="https://github.com/abjelosevic88/pocket-homelab-notes" \
      org.opencontainers.image.licenses="MIT"
ENTRYPOINT ["pocket-notes"]
