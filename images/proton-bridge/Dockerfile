# syntax=docker/dockerfile:1.7

FROM --platform=$TARGETPLATFORM golang:alpine3.23 AS build

ARG ENV_PROTONMAIL_BRIDGE_VERSION
ARG ENV_PROTONMAIL_BRIDGE_COMMIT
ARG TARGETPLATFORM

# Install dependencies
# hadolint ignore=DL3018
RUN set -eux; \
    apk add --no-cache \
      bash \
      g++ \
      git \
      grep \
      libcbor-dev \
      libfido2-dev \
      libsecret-dev \
      make \
      openssl-dev \
      pkgconf \
      sed

WORKDIR /build/
RUN set -eux; \
    : "${ENV_PROTONMAIL_BRIDGE_VERSION:?build arg ENV_PROTONMAIL_BRIDGE_VERSION is required}"; \
    : "${ENV_PROTONMAIL_BRIDGE_COMMIT:?build arg ENV_PROTONMAIL_BRIDGE_COMMIT is required}"; \
    git clone --depth 1 --single-branch --branch "$ENV_PROTONMAIL_BRIDGE_VERSION" https://github.com/ProtonMail/proton-bridge.git; \
    actual_commit="$(git -C /build/proton-bridge rev-parse HEAD)"; \
    if [ "${actual_commit}" != "${ENV_PROTONMAIL_BRIDGE_COMMIT}" ]; then \
      echo "Expected commit ${ENV_PROTONMAIL_BRIDGE_COMMIT}, got ${actual_commit}" >&2; \
      exit 1; \
    fi; \
    printf '%s\n' "$ENV_PROTONMAIL_BRIDGE_VERSION" > /build/BRIDGE_VERSION
WORKDIR /build/proton-bridge/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eux; \
    make build-nogui vault-editor

# Working stage image
FROM --platform=$TARGETPLATFORM alpine:3.23

# Define arguments and env variables
ARG TARGETPLATFORM
ARG ENV_PROTONMAIL_BRIDGE_VERSION
ARG ENV_PROTONMAIL_BRIDGE_COMMIT
# Indicate (NOT define) the ports/network interface really used by Proton bridge mail.
# It should be 1025/tcp and 1143/tcp but on some k3s instances it could be 1026 and 1144 (why ?)
# Launch `netstat -ltnp` on a running container to be sure.
ARG ENV_BRIDGE_SMTP_PORT=1025
ARG ENV_BRIDGE_IMAP_PORT=1143
ARG ENV_BRIDGE_HOST=127.0.0.1
# Change ENV_CONTAINER_SMTP_PORT only if you have a docker port conflict on host network namespace.
ARG ENV_CONTAINER_SMTP_PORT=1026
ARG ENV_CONTAINER_IMAP_PORT=1144
ARG ENV_CONTAINER_METRICS_PORT=9154
ENV PROTON_BRIDGE_SMTP_PORT=$ENV_BRIDGE_SMTP_PORT \
    PROTON_BRIDGE_IMAP_PORT=$ENV_BRIDGE_IMAP_PORT \
    PROTON_BRIDGE_HOST=$ENV_BRIDGE_HOST \
    CONTAINER_SMTP_PORT=$ENV_CONTAINER_SMTP_PORT \
    CONTAINER_IMAP_PORT=$ENV_CONTAINER_IMAP_PORT \
    CONTAINER_METRICS_PORT=$ENV_CONTAINER_METRICS_PORT \
    ENV_TARGET_PLATFORM=$TARGETPLATFORM

# Install dependencies
# hadolint ignore=DL3018
RUN set -eux; \
    apk add --no-cache \
      bash \
      ca-certificates \
      gpg \
      gpg-agent \
      gnupg-keyboxd \
      libcbor \
      libfido2 \
      libsecret \
      net-tools \
      pass \
      s6 \
      socat

RUN set -eux; \
    addgroup -g 1000 -S bridge; \
    adduser -u 1000 -S -G bridge -h /home/bridge bridge; \
    mkdir -p /home/bridge /app; \
    chown -R bridge:bridge /home/bridge /app

# Copy executables made during previous stage
WORKDIR /usr/bin/
COPY --from=build /build/proton-bridge/bridge /build/proton-bridge/proton-bridge /build/proton-bridge/vault-editor /usr/bin/

# Install needed scripts and files
WORKDIR /app/
COPY --chmod=0755 entrypoint.sh /app/entrypoint.sh
COPY --chmod=0755 healthcheck.sh /app/healthcheck.sh
COPY --chmod=0755 metrics/metrics.cgi /app/metrics/metrics.cgi
COPY --chmod=0755 metrics/serve-http.sh /app/metrics/serve-http.sh
COPY GPGparams.txt /app/GPGparams.txt
COPY LICENSE /app/licenses/LICENSE
COPY NOTICE /app/licenses/NOTICE
COPY --chmod=0755 services/.s6-finish-policy.sh /app/services/.s6-finish-policy.sh
COPY --chmod=0755 services/bridge/run /app/services/bridge/run
COPY --chmod=0755 services/bridge/finish /app/services/bridge/finish
COPY --chmod=0755 services/gpg-agent/run /app/services/gpg-agent/run
COPY --chmod=0755 services/gpg-agent/finish /app/services/gpg-agent/finish
COPY --chmod=0755 services/socat-smtp/run /app/services/socat-smtp/run
COPY --chmod=0755 services/socat-smtp/finish /app/services/socat-smtp/finish
COPY --chmod=0755 services/socat-imap/run /app/services/socat-imap/run
COPY --chmod=0755 services/socat-imap/finish /app/services/socat-imap/finish
COPY --chmod=0755 services/metrics/run /app/services/metrics/run
COPY --chmod=0755 services/metrics/finish /app/services/metrics/finish
COPY --from=build /build/BRIDGE_VERSION /app/VERSION
RUN chown -R bridge:bridge /app

# Dynamic image metadata is declared late to avoid cache misses on earlier layers.
ARG ENV_IMAGE_SOURCE=https://github.com/mgarratt/docker-images
ARG ENV_IMAGE_REVISION=unknown
RUN set -eu; \
    printf '%s\n' \
      "upstream_source=https://github.com/ProtonMail/proton-bridge" \
      "upstream_version=${ENV_PROTONMAIL_BRIDGE_VERSION}" \
      "upstream_revision=${ENV_PROTONMAIL_BRIDGE_COMMIT}" \
      "image_wrapper_source=${ENV_IMAGE_SOURCE}" \
      "image_wrapper_revision=${ENV_IMAGE_REVISION}" \
      > /app/CORRESPONDING_SOURCE; \
    chown bridge:bridge /app/CORRESPONDING_SOURCE

LABEL org.opencontainers.image.source="$ENV_IMAGE_SOURCE" \
      org.opencontainers.image.revision="$ENV_IMAGE_REVISION" \
      org.opencontainers.image.version="$ENV_PROTONMAIL_BRIDGE_VERSION" \
      org.opencontainers.image.upstream="https://github.com/ProtonMail/proton-bridge" \
      org.opencontainers.image.upstream.revision="$ENV_PROTONMAIL_BRIDGE_COMMIT" \
      org.opencontainers.image.title="proton-bridge" \
      org.opencontainers.image.description="Headless Proton Mail Bridge with CLI tooling"

# SMTP and IMAP ports are not exposed by default, so you could adjust them if necessary with ENV
# variables CONTAINER_SMTP_PORT and CONTAINER_IMAP_PORT.
# See README.md and/or compose.yaml file.
# EXPOSE ${ENV_CONTAINER_SMTP_PORT}/tcp
# EXPOSE ${ENV_CONTAINER_IMAP_PORT}/tcp

# Volume to save pass and bridge configurations/data
VOLUME /home/bridge

HEALTHCHECK --interval=30s --timeout=5s --start-period=45s --retries=3 \
  CMD ["/app/healthcheck.sh"]

USER bridge:bridge
ENTRYPOINT ["/app/entrypoint.sh"]
