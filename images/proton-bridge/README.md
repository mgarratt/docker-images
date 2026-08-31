# proton-bridge

Headless Proton Mail Bridge image with `s6`-supervised services.

## Services

The container runs these long-lived services under `s6`:

- `bridge` (`/usr/bin/bridge --grpc` by default; serves mail and the gRPC machine API)
- `gpg-agent` (launched and monitored via `gpgconf`)
- `smtp-relay` (a small STARTTLS-terminating SMTP relay on `${CONTAINER_SMTP_PORT}`; see [SMTP Relay](#smtp-relay) below)
- IMAP forwarder (`socat` on `${CONTAINER_IMAP_PORT}` -> `${PROTON_BRIDGE_HOST}:${PROTON_BRIDGE_IMAP_PORT}`, a plain TCP forward)
- `exporter` (Prometheus metrics on `${CONTAINER_METRICS_PORT}`, scraping the bridge gRPC API)

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
- `CONTAINER_SMTP_TLS_CERT_FILE` (default `/etc/proton-bridge/tls/tls.crt`) — certificate the `smtp-relay` service presents for STARTTLS on `CONTAINER_SMTP_PORT`
- `CONTAINER_SMTP_TLS_KEY_FILE` (default `/etc/proton-bridge/tls/tls.key`) — matching private key

The defaults for the last two match the two keys of a Kubernetes TLS-type
`Secret` (`tls.crt`/`tls.key`), so mounting such a secret at
`/etc/proton-bridge/tls/` needs no env var overrides.

Optional runtime tuning:

- `CRASH_LOOP_WINDOW_SECONDS` (default `20`)
- `CRASH_LOOP_MAX_RESTARTS` (default `5`)
- `BRIDGE_EXIT_ZERO_STOPS_CONTAINER` (default `true`)
- `BRIDGE_GPG_AGENT_WAIT_SECONDS` (default `30`)
- `GPG_AGENT_LAUNCH_MAX_FAILURES` (default `10`)
- `BRIDGE_MODE` (`grpc`, `noninteractive`, or `cli`, default `grpc`)
- `SOCAT_DEBUG` (`true` enables verbose socat logs on the IMAP forwarder, default `false`)
- `SMTP_RELAY_DEBUG` (`true` enables verbose per-connection logging on the SMTP relay, default `false`)

## Metrics

The `exporter` service exposes Prometheus metrics on `${CONTAINER_METRICS_PORT}`
(default `9154`, path `/metrics`), including per-account login state and
dropped-auth counters scraped from the bridge gRPC API. This requires the
default `BRIDGE_MODE=grpc`; under `noninteractive`/`cli` the gRPC API is absent
and the exporter reports `proton_bridge_up 0`.

See [`exporter/README.md`](exporter/README.md) for the full metric list and the
stub-regeneration step required on each upstream version bump.

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
  -e CONTAINER_METRICS_PORT=9154 \
  -v proton-bridge-home:/home/bridge \
  -v /path/to/tls:/etc/proton-bridge/tls:ro \
  ghcr.io/mgarratt/docker-images/proton-bridge:latest
```

The `smtp-relay` service needs a certificate and key at
`CONTAINER_SMTP_TLS_CERT_FILE`/`CONTAINER_SMTP_TLS_KEY_FILE` (default
`/etc/proton-bridge/tls/tls.crt`/`tls.key`) to start; the mount above supplies
them.

Then in the Bridge CLI prompt:

1. Run `login`
2. Complete Proton login (including MFA, if enabled)
3. Run `info` to see account status and the generated Bridge mailbox credentials

Use the generated Bridge credentials in your mail client, not your Proton account password.

After login is complete, restart the container in the default `grpc` mode for normal long-running use (or `BRIDGE_MODE=noninteractive` if you don't need the metrics exporter).

## Behavior Notes

- `bridge` clean exit (`code=0`) stops container by default. Set `BRIDGE_EXIT_ZERO_STOPS_CONTAINER=false` to allow restart instead.
- `bridge` runs in `grpc` mode by default (serves mail plus the gRPC API the metrics exporter scrapes). Use `BRIDGE_MODE=noninteractive` for a leaner run without metrics, or `BRIDGE_MODE=cli` only for interactive debugging.
- `gpg-agent` service handles Alpine stale socket/lock cleanup before launch and during service finish.
- Bridge may log DBus keychain warnings in headless environments without `dbus-launch`; this is expected when not using a desktop keyring.
- The bootstrap GPG key is generated with `%no-protection` and no passphrase. Protect mounted `/home/bridge` volumes and backups accordingly.

## SMTP Relay

Bridge binds SMTP on loopback and presents a certificate for `CN=127.0.0.1`,
`SAN IP:127.0.0.1` — correct for a same-machine client, but not verifiable by
any client that reaches it across a pod boundary. Rather than forward that
certificate unchanged (as a plain TCP proxy like `socat` would), the
`smtp-relay` service terminates TLS at the container boundary:

- it listens on `CONTAINER_SMTP_PORT` and offers **STARTTLS**, presenting the
  certificate/key at `CONTAINER_SMTP_TLS_CERT_FILE`/`CONTAINER_SMTP_TLS_KEY_FILE`;
- it then negotiates **STARTTLS onward** to Bridge at
  `${PROTON_BRIDGE_HOST}:${PROTON_BRIDGE_SMTP_PORT}` — exactly what a client
  talking to Bridge directly does today, so nothing assumes Bridge accepts
  plaintext;
- once both legs are TLS it pipes bytes verbatim in both directions. It never
  parses `AUTH`, `MAIL`, `RCPT` or `DATA`, so SMTP AUTH passes through exactly
  as Bridge issued it — the relay authenticates nothing of its own;
- the inner (Bridge-facing) hop skips certificate verification: it is loopback
  inside one pod, to a certificate whose IP SAN genuinely matches;
- a client that never issues `STARTTLS` is refused (`530`), not silently
  downgraded to plaintext.

It is a small (~200 line), dependency-free Go program under
[`smtp-relay/`](smtp-relay/main.go), built and run the same way as the
[`exporter`](exporter/README.md). `decke/smtprelay` was evaluated first, as the
framing named it as an unverified candidate, and rejected: it authenticates
inbound connections against its own local (bcrypt-hashed) user file and logs
into its configured remote with its own separately-configured credentials —
that is deliberate proxy behaviour, not pass-through, and it would mean SMTP
AUTH no longer reaches Bridge unmodified. A purpose-built relay avoided that
mismatch and needed no third-party dependency at all.

## Healthcheck

The container `HEALTHCHECK` script validates:

- `s6` supervision state for `bridge`, `gpg-agent`, `smtp-relay`, and `socat-imap`
- `gpg-agent` control socket responsiveness (`gpg-connect-agent /bye`)
- listening state for SMTP/IMAP container ports
- lightweight SMTP and IMAP banner-level handshake probes on local forwarded ports

## Build Supply Chain

The Dockerfile requires source commit verification via build arg:

- `ENV_PROTONMAIL_BRIDGE_COMMIT` (required)

The build fails if the cloned tag does not resolve to the expected commit SHA.

## Corresponding Source

This image distributes GPL-licensed Proton Bridge binaries built from source. Corresponding source is provided by:

- Upstream Proton Bridge source: <https://github.com/ProtonMail/proton-bridge>
- This image wrapper source: <https://github.com/mgarratt/docker-images/tree/main/images/proton-bridge>

Pinned upstream source for this image definition (`images/proton-bridge/image.toml`):

- `ENV_PROTONMAIL_BRIDGE_VERSION=v3.25.0`
- `ENV_PROTONMAIL_BRIDGE_COMMIT=f1f599e97167265cb0d10ad3d169269c324d9cc7`

For published images, the wrapper source commit is recorded in image label:

- `org.opencontainers.image.revision=<docker-images commit SHA>`
- The same SHA is published as image tag `:sha-<shortsha>`

## Licensing

- Proton Bridge itself is GPL-3.0 (`ProtonMail/proton-bridge`).
- This image configuration includes material adapted from `shenxn/protonmail-bridge-docker` (GPL-3.0), especially the initial bootstrap/GPG parameter approach.
- This image configuration also references containerization patterns from `VideoCurio/ProtonMailBridgeDocker` (GPL-3.0).
- See `images/proton-bridge/NOTICE` and `images/proton-bridge/LICENSE` for attribution and GPL text.
