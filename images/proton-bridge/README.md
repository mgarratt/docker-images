# proton-bridge

Headless Proton Mail Bridge image with `s6`-supervised services.

## Services

The container runs these long-lived services under `s6`:

- `bridge` (`/usr/bin/bridge --grpc` by default; serves mail and the gRPC machine API)
- `gpg-agent` (launched and monitored via `gpgconf`)
- `smtp-relay` (a small STARTTLS-terminating SMTP relay on `${CONTAINER_SMTP_PORT}`; see [STARTTLS Relay](#starttls-relay) below)
- `imap-relay` (the same relay for IMAP on `${CONTAINER_IMAP_PORT}`)
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
- `CONTAINER_IMAP_TLS_CERT_FILE` (default `/etc/proton-bridge/tls/tls.crt`) — certificate the `imap-relay` service presents for STARTTLS on `CONTAINER_IMAP_PORT`
- `CONTAINER_IMAP_TLS_KEY_FILE` (default `/etc/proton-bridge/tls/tls.key`) — matching private key

The defaults for the TLS files match the two keys of a Kubernetes TLS-type
`Secret` (`tls.crt`/`tls.key`), so mounting such a secret at
`/etc/proton-bridge/tls/` needs no env var overrides.

Optional runtime tuning:

- `CRASH_LOOP_WINDOW_SECONDS` (default `20`)
- `CRASH_LOOP_MAX_RESTARTS` (default `5`)
- `BRIDGE_EXIT_ZERO_STOPS_CONTAINER` (default `true`)
- `BRIDGE_GPG_AGENT_WAIT_SECONDS` (default `30`)
- `GPG_AGENT_LAUNCH_MAX_FAILURES` (default `10`)
- `BRIDGE_MODE` (`grpc`, `noninteractive`, or `cli`, default `grpc`)
- `SMTP_RELAY_DEBUG` / `IMAP_RELAY_DEBUG` (`true` enables verbose per-connection logging on that relay, default `false`)

## Metrics

The `exporter` service exposes Prometheus metrics on `${CONTAINER_METRICS_PORT}`
(default `9154`, path `/metrics`), including per-account login state and
dropped-auth counters scraped from the bridge gRPC API. This requires the
default `BRIDGE_MODE=grpc`; under `noninteractive`/`cli` the gRPC API is absent
and the exporter reports `proton_bridge_up 0`.

See [`exporter/README.md`](exporter/README.md) for the full metric list and how
its gRPC stubs track the pinned Bridge version.

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

The `smtp-relay` and `imap-relay` services need a certificate and key at
`CONTAINER_{SMTP,IMAP}_TLS_CERT_FILE`/`CONTAINER_{SMTP,IMAP}_TLS_KEY_FILE` (default
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

## STARTTLS Relay

Bridge binds SMTP and IMAP on loopback and presents a certificate for `CN=127.0.0.1`,
`SAN IP:127.0.0.1` — correct for a same-machine client, but not verifiable by
any client that reaches it across a pod boundary. Rather than forward that
certificate unchanged (as a plain TCP proxy like `socat` would), the
`smtp-relay` and `imap-relay` services (one binary,
`proton-bridge-starttls-relay smtp|imap`) terminate TLS at the container
boundary. For each protocol:

- it listens on `CONTAINER_{SMTP,IMAP}_PORT` and offers **STARTTLS**, presenting
  the certificate/key at `CONTAINER_{SMTP,IMAP}_TLS_CERT_FILE`/`_KEY_FILE`;
- it then negotiates **STARTTLS onward** to Bridge at
  `${PROTON_BRIDGE_HOST}:${PROTON_BRIDGE_{SMTP,IMAP}_PORT}` — exactly what a client
  talking to Bridge directly does today, so nothing assumes Bridge accepts
  plaintext;
- once both legs are TLS it pipes bytes verbatim in both directions. It never
  parses SMTP `AUTH`/`MAIL`/`RCPT`/`DATA` or any IMAP command after
  `STARTTLS`, so credentials pass through exactly as Bridge issued them — the
  relay authenticates nothing of its own;
- the inner (Bridge-facing) hop skips certificate verification: it is loopback
  inside one pod, to a certificate whose IP SAN genuinely matches;
- a client that never issues `STARTTLS` is refused (SMTP `530`; IMAP
  advertises `LOGINDISABLED` and answers `LOGIN`/`AUTHENTICATE` with `NO`), not
  silently downgraded to plaintext.

The certificate is read once at start-up; a renewed certificate is picked up
when the container restarts (e.g. via Reloader on the mounted Secret).

It is a small (~300 line), dependency-free Go program under
[`starttls-relay/`](starttls-relay/main.go), built and run the same way as the
[`exporter`](exporter/README.md). `decke/smtprelay` was evaluated first, as the
framing named it as an unverified candidate, and rejected: it authenticates
inbound connections against its own local (bcrypt-hashed) user file and logs
into its configured remote with its own separately-configured credentials —
that is deliberate proxy behaviour, not pass-through, and it would mean SMTP
AUTH no longer reaches Bridge unmodified. A purpose-built relay avoided that
mismatch and needed no third-party dependency at all.

## Healthcheck

The container `HEALTHCHECK` script validates:

- `s6` supervision state for `bridge`, `gpg-agent`, `smtp-relay`, `imap-relay`, and `exporter`
- `gpg-agent` control socket responsiveness (`gpg-connect-agent /bye`)
- listening state for SMTP/IMAP container ports
- lightweight SMTP and IMAP banner-level handshake probes on the relay ports, plus an IMAP banner probe on Bridge's own loopback port

## Build Supply Chain

The Dockerfile requires source commit verification via build arg:

- `ENV_PROTONMAIL_BRIDGE_COMMIT` (required)

The build fails if the cloned tag does not resolve to the expected commit SHA.

## Corresponding Source

This image distributes GPL-licensed Proton Bridge binaries built from source. Corresponding source is provided by:

- Upstream Proton Bridge source: <https://github.com/ProtonMail/proton-bridge>
- This image wrapper source: <https://github.com/mgarratt/docker-images/tree/main/images/proton-bridge>

Pinned upstream source for this image definition (`images/proton-bridge/image.toml`):

- `ENV_PROTONMAIL_BRIDGE_VERSION=v3.27.2`
- `ENV_PROTONMAIL_BRIDGE_COMMIT=3e9f85aa3c676a573b64b1d90829970ded26dbfa`

For published images, the wrapper source commit is recorded in image label:

- `org.opencontainers.image.revision=<docker-images commit SHA>`
- The same SHA is published as image tag `:sha-<shortsha>`

## Licensing

- Proton Bridge itself is GPL-3.0 (`ProtonMail/proton-bridge`).
- This image configuration includes material adapted from `shenxn/protonmail-bridge-docker` (GPL-3.0), especially the initial bootstrap/GPG parameter approach.
- This image configuration also references containerization patterns from `VideoCurio/ProtonMailBridgeDocker` (GPL-3.0).
- See `images/proton-bridge/NOTICE` and `images/proton-bridge/LICENSE` for attribution and GPL text.
