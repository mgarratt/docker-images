# syntax=docker/dockerfile:1
# Custom GitHub Actions runner image for my home cluster's ARC scale set.
#
# The upstream actions/actions-runner image is intentionally minimal and lacks
# tools that ubuntu-latest ships and our workflows assume:
#   make + build-essential -> Makefile targets and Python C-extension builds
#   libatomic1             -> pnpm's prebuilt binary needs libatomic.so.1
#   docker compose plugin  -> the `docker compose` subcommand (compose smoke tests)
#
# arm64-only: the cluster nodes (and therefore the runners) are aarch64.
ARG RUNNER_VERSION=2.335.1
FROM ghcr.io/actions/actions-runner:${RUNNER_VERSION}

ARG COMPOSE_VERSION=v2.32.4

USER root

# hadolint ignore=DL3008
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      build-essential \
      ca-certificates \
      curl \
      git \
      jq \
      libatomic1 \
      make \
 && rm -rf /var/lib/apt/lists/*

# Docker Compose v2 as a CLI plugin (not packaged in the runner image). aarch64
# binary into a standard cli-plugins search path so `docker compose` resolves.
RUN mkdir -p /usr/local/lib/docker/cli-plugins \
 && curl -fsSL "https://github.com/docker/compose/releases/download/${COMPOSE_VERSION}/docker-compose-linux-aarch64" \
      -o /usr/local/lib/docker/cli-plugins/docker-compose \
 && chmod +x /usr/local/lib/docker/cli-plugins/docker-compose

USER runner
