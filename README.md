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
See `THIRD_PARTY_LICENSES.md` for third-party license notices.

## Local Build

Build the image for your host architecture and load it into your local Docker daemon:

```bash
./scripts/build-one.py <image>
```

## Adding An Image

1. Copy `images/_template/` to `images/<image>/`.
2. Add `images/<image>/Dockerfile`.
3. Edit `images/<image>/image.toml`:
   - Set `version` when you want a stable tag like `:<version>` in addition to `:latest`.
