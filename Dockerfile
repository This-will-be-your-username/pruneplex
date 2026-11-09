# syntax=docker/dockerfile:1

# PrunePlex is a single static Go binary with its web assets embedded, so the
# runtime image is `scratch`: no OS, no shell, nothing but the binary and the CA
# certificates needed to reach plex.tv, OMDb, Trakt and any HTTPS Radarr/Sonarr.
# That keeps the image to a few MB and the idle footprint to the Go runtime's.

# ---- build ------------------------------------------------------------------
# Runs on the build machine's own architecture and cross-compiles, so building
# for another CPU (e.g. arm64 on an x86 host) needs no slow emulation.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
ARG TARGETOS
ARG TARGETARCH
# -s -w strips the symbol table and debug info; -trimpath drops build paths.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/pruneplex ./cmd/server
# A config directory the unprivileged user can write to (scratch has no mkdir).
RUN mkdir /out/config && chown 65532:65532 /out/config

# ---- runtime ----------------------------------------------------------------
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=65532:65532 /out/config /config
COPY --from=build /out/pruneplex /pruneplex

# The app derives plex.json, sonarr.json, lidarr.json, ratings.json and the
# caches from the directory of this path. There is no $HOME in the image, so it
# must be set explicitly.
ENV PRUNEPLEX_CONFIG=/config/radarr.json \
    PRUNEPLEX_PORT=4848

USER 65532:65532
VOLUME /config
EXPOSE 4848

# The image has no shell or curl; the binary probes itself.
HEALTHCHECK --interval=60s --timeout=3s --start-period=10s --start-interval=2s --retries=3 \
    CMD ["/pruneplex", "healthcheck"]

ENTRYPOINT ["/pruneplex"]
