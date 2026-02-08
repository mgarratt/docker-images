#!/usr/bin/env bash
set -euo pipefail

img="${1:-}"
if [[ -z "$img" ]]; then
  echo "usage: $0 <image-dir-name> [tag]" >&2
  echo "example: $0 kube-tools dev" >&2
  exit 2
fi

tag="${2:-dev}"
dir="images/$img"

if [[ ! -f "$dir/Dockerfile" ]]; then
  echo "missing $dir/Dockerfile" >&2
  exit 2
fi

# Local build for the host arch. Multi-arch builds require --push and a registry.
docker buildx build --load -t "local/$img:$tag" "$dir"

