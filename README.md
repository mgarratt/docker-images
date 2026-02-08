# docker-images

Docker images I build and publish for my home cluster.

## Publishing

- Registry: GHCR
- Image path: `ghcr.io/<owner>/<repo>/<image>`
- Multi-arch: `linux/amd64`, `linux/arm64`

On pushes to `main`, CI publishes for each changed image:

- `:latest`
- `:sha-<shortsha>`
- `:<version>` if `version` is set in `images/<image>/image.toml`

## Repo Layout

- `images/<image>/` contains an image definition
- `scripts/` contains local/CI helpers

See `AGENTS.md` for the full standards.

