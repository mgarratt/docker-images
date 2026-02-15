# proton-bridge

Headless Proton Mail Bridge image with `s6`-supervised services.

## Services

The container runs these long-lived services under `s6`:

- `bridge` (`/usr/bin/bridge --noninteractive` by default)
- `gpg-agent` (launched and monitored via `gpgconf`)
- SMTP forwarder (`socat` on `${CONTAINER_SMTP_PORT}` -> `${PROTON_BRIDGE_HOST}:${PROTON_BRIDGE_SMTP_PORT}`)
- IMAP forwarder (`socat` on `${CONTAINER_IMAP_PORT}` -> `${PROTON_BRIDGE_HOST}:${PROTON_BRIDGE_IMAP_PORT}`)
- metrics endpoint (`socat` listener on `${CONTAINER_METRICS_PORT}` serving `/cgi-bin/metrics`)

Bootstrap-only initialization in `entrypoint.sh`:

- validate required environment variables
- initialize GPG key + pass store on first startup (when `${HOME}/.password-store` does not exist)

## Environment Variables

Required:

- `PROTON_BRIDGE_SMTP_PORT`
- `PROTON_BRIDGE_IMAP_PORT`
- `PROTON_BRIDGE_HOST`
- `CONTAINER_SMTP_PORT`
- `CONTAINER_IMAP_PORT`
- `CONTAINER_METRICS_PORT`

Optional runtime tuning:

- `CRASH_LOOP_WINDOW_SECONDS` (default `20`)
- `CRASH_LOOP_MAX_RESTARTS` (default `5`)
- `BRIDGE_EXIT_ZERO_STOPS_CONTAINER` (default `true`)
- `BRIDGE_GPG_AGENT_WAIT_SECONDS` (default `30`)
- `GPG_AGENT_LAUNCH_MAX_FAILURES` (default `10`)
- `BRIDGE_MODE` (`noninteractive` or `cli`, default `noninteractive`)
- `SOCAT_DEBUG` (`true` enables verbose socat logs, default `false`)

## Login

Use Bridge CLI mode for first-time account login.

This image uses an `s6` entrypoint, so `docker run ... --cli` does not pass `--cli` to Bridge.
Set `BRIDGE_MODE=cli` instead.

Example:

```bash
docker run --rm -it \
  -e BRIDGE_MODE=cli \
  -e PROTON_BRIDGE_SMTP_PORT=1025 \
  -e PROTON_BRIDGE_IMAP_PORT=1143 \
  -e PROTON_BRIDGE_HOST=127.0.0.1 \
  -e CONTAINER_SMTP_PORT=1026 \
  -e CONTAINER_IMAP_PORT=1144 \
  -v proton-bridge-home:/home/bridge \
  ghcr.io/mgarratt/docker-images/proton-bridge:latest
```

Then in the Bridge CLI prompt:

1. Run `login`
2. Complete Proton login (including MFA, if enabled)
3. Run `info` to see account status and the generated Bridge mailbox credentials

Use the generated Bridge credentials in your mail client, not your Proton account password.

After login is complete, restart the container with `BRIDGE_MODE=noninteractive` for normal long-running use.

## Behavior Notes

- `bridge` clean exit (`code=0`) stops container by default. Set `BRIDGE_EXIT_ZERO_STOPS_CONTAINER=false` to allow restart instead.
- `bridge` runs in non-interactive mode by default to avoid EOF-driven exits. Set `BRIDGE_MODE=cli` only for interactive debugging.
- `gpg-agent` service handles Alpine stale socket/lock cleanup before launch and during service finish.
- Bridge may log DBus keychain warnings in headless environments without `dbus-launch`; this is expected when not using a desktop keyring.
- The bootstrap GPG key is generated with `%no-protection` and no passphrase. Protect mounted `/home/bridge` volumes and backups accordingly.

## Healthcheck

The container `HEALTHCHECK` script validates:

- `s6` supervision state for `bridge`, `gpg-agent`, `socat-smtp`, and `socat-imap`
- `gpg-agent` control socket responsiveness (`gpg-connect-agent /bye`)
- listening state for SMTP/IMAP container ports
- lightweight SMTP and IMAP banner-level handshake probes on local forwarded ports
- metrics endpoint HTTP readiness (`/metrics`)

## Metrics

The image now exposes Prometheus-formatted metrics from inside the container.

- listen port: `${CONTAINER_METRICS_PORT}` (default `9154`)
- scrape path: `/metrics` (`/cgi-bin/metrics` also supported)

Current metrics include:

- `proton_bridge_service_up{service=...}`
- `proton_bridge_service_restart_count{service=...}`
- `proton_bridge_service_start_time_seconds{service=...}`
- `proton_bridge_gpg_agent_up`
- `proton_bridge_port_listening{listener=...,port=...}`
- `proton_bridge_smtp_banner_probe_up`
- `proton_bridge_imap_banner_probe_up`
- `proton_bridge_pass_entry_count`

Limitations:

- Proton Bridge does not currently expose a stable machine API for per-account sync status in this image mode.
- You can scrape transport/process readiness today; account-level sync state likely requires either upstream Bridge support or a dedicated log/CLI parser sidecar.

## Build Supply Chain

The Dockerfile requires source commit verification via build arg:

- `ENV_PROTONMAIL_BRIDGE_COMMIT` (required)

The build fails if the cloned tag does not resolve to the expected commit SHA.

## Corresponding Source

This image distributes GPL-licensed Proton Bridge binaries built from source. Corresponding source is provided by:

- Upstream Proton Bridge source: <https://github.com/ProtonMail/proton-bridge>
- This image wrapper source: <https://github.com/mgarratt/docker-images/tree/main/images/proton-bridge>

Pinned upstream source for this image definition (`images/proton-bridge/image.toml`):

- `ENV_PROTONMAIL_BRIDGE_VERSION=v3.22.0`
- `ENV_PROTONMAIL_BRIDGE_COMMIT=87bba395d0f93301ac318d9b5cffde3312bbe13e`

For published images, the wrapper source commit is recorded in image label:

- `org.opencontainers.image.revision=<docker-images commit SHA>`
- The same SHA is published as image tag `:sha-<shortsha>`

## Licensing

- Proton Bridge itself is GPL-3.0 (`ProtonMail/proton-bridge`).
- This image configuration includes material adapted from `shenxn/protonmail-bridge-docker` (GPL-3.0), especially the initial bootstrap/GPG parameter approach.
- This image configuration also references containerization patterns from `VideoCurio/ProtonMailBridgeDocker` (GPL-3.0).
- See `images/proton-bridge/NOTICE` and `images/proton-bridge/LICENSE` for attribution and GPL text.
