# Repo Standards (docker-images)

This repository contains Docker images I build and publish for my home cluster.

## Layout

- `images/<image>/`
  - `Dockerfile`
  - `.dockerignore`
  - `README.md` (image-specific notes: upstream, config, usage)
  - `image.toml` (metadata used by CI)
- `scripts/`
  - repo automation (matrix generation, local build helpers)
- `.github/workflows/`
  - CI and publish workflows

## Image Metadata (`images/<image>/image.toml`)

Required keys:

- `image` (string): image name segment used in the registry path (defaults to folder name if omitted).

Optional keys:

- `version` (string): if set, CI publishes this tag as well (example: `"1.2.3"` or `"v1.2.3"`).
- `platforms` (array of strings): defaults to `["linux/amd64", "linux/arm64"]`.

Example:

```toml
image = "kube-tools"
version = "1.30.4"
platforms = ["linux/amd64", "linux/arm64"]
```

## Publishing Rules

- Registry: GHCR.
- Image reference format: `ghcr.io/<owner>/<repo>/<image>`.
- On push to `main`, for each changed image:
  - always push `:latest`
  - always push `:sha-<shortsha>`
  - if `version` is set in `image.toml`, also push `:<version>`

Notes:

- Version tags are not immutable by default. If you re-run CI with the same `version`, it can republish that tag.

## CI Expectations

- Use `mise` to install tool dependencies in CI (via `jdx/mise-action`).
- Builds must be multi-arch with Buildx (at least `linux/amd64` and `linux/arm64`).
- PRs should validate Dockerfiles (lint + build) but must not push to GHCR.
- CI must run repo linting via `./scripts/lint.sh` (ShellCheck for `scripts/*.sh`, Hadolint for `images/*/Dockerfile`).

## Linting

- Run locally: `./scripts/lint.sh`
- CI must run this script on PRs and on `main` publishes so linting stays consistent across sessions.

## Adding A New Image

1. Create `images/<image>/Dockerfile` and `.dockerignore`.
2. Add `images/<image>/image.toml` (set `version` if you want a stable tag).
3. Add a short `images/<image>/README.md`.
