# Images

Each subdirectory in `images/` is one Docker image.

- Create a new image by copying `images/_template/` to `images/<image>/` and editing.
- CI only builds directories that contain both `Dockerfile` and `image.toml`.

## Available

- `proton-bridge`: Proton Bridge built from source (multi-arch) with a k8s-friendly entrypoint.
