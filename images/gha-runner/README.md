# gha-runner

Custom GitHub Actions runner image for my **home cluster's ARC scale set** — the
upstream `actions/actions-runner` plus the tools `ubuntu-latest` ships that the
minimal image doesn't, so self-hosted lint/test jobs work:

- `make` + `build-essential` — Makefile targets and Python C-extension builds
- `libatomic1` — pnpm's prebuilt binary needs `libatomic.so.1`
- the `docker compose` CLI plugin — compose-based smoke tests
- `yq` (mikefarah) — YAML queries in workflow steps

`arm64`-only (the runners are aarch64). Built and published by the standard
matrix (`publish.yml`) like every other image, to
`ghcr.io/mgarratt/docker-images/gha-runner`. This repo is public, so the package
is public too and the ARC scale set pulls it without a secret; the
home cluster's ARC `ResourceSet` (`gha-system/arc-runner-sets`) points every scale
set's runner `image:` here. It is consumed as `:latest`: ARC disables runner
self-update, so Renovate automerges `RUNNER_VERSION` bumps and new runner pods
pick them up.
