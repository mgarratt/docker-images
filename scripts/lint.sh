#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

echo "[lint] shellcheck"
if command -v shellcheck >/dev/null 2>&1; then
  shellcheck scripts/*.sh
elif command -v mise >/dev/null 2>&1; then
  mise exec -- shellcheck scripts/*.sh
else
  echo "[lint] shellcheck (missing; install via mise)" >&2
  exit 127
fi

dockerfiles=()
while IFS= read -r -d '' f; do
  dockerfiles+=("$f")
done < <(find images -mindepth 2 -maxdepth 2 -type f -name Dockerfile -print0 2>/dev/null || true)

if [[ ${#dockerfiles[@]} -gt 0 ]]; then
  echo "[lint] hadolint"
  for f in "${dockerfiles[@]}"; do
    if command -v hadolint >/dev/null 2>&1; then
      hadolint "$f"
    elif command -v mise >/dev/null 2>&1; then
      mise exec -- hadolint "$f"
    else
      echo "[lint] hadolint (missing; install via mise)" >&2
      exit 127
    fi
  done
else
  echo "[lint] hadolint (skipped; no Dockerfiles found)"
fi
