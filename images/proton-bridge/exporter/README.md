# proton-bridge-exporter

A small Prometheus exporter that scrapes the headless Proton Bridge **gRPC
frontend** (`bridge --grpc`) and exposes process- and account-level metrics,
so dropped account authentication can be alerted on.

## How it connects

Bridge writes a `grpcServerConfig.json` to its settings directory
(`~/.config/protonmail/bridge-v3/`) containing a generated TLS certificate, an
auth token, and a unix socket path. The exporter reads that file, dials the
socket, pins the (self-signed, CN `127.0.0.1`) certificate, and presents the
token in a `server-token` per-RPC header — exactly as the desktop GUI does.

It then polls `GetUserList` / `Version` on an interval and subscribes to
`RunEventStream` for live disconnect / bad-event / IMAP-login-failure events.

## Metrics

| Metric | Type | Notes |
| --- | --- | --- |
| `proton_bridge_up` | gauge | 1 if the gRPC service is reachable |
| `proton_bridge_info{version}` | gauge | bridge version, value 1 |
| `proton_bridge_accounts_total` | gauge | number of known accounts |
| `proton_bridge_account_connected{account}` | gauge | 1 = logged in / connected |
| `proton_bridge_account_state{account,state}` | gauge | 1 for the active state (`connected`/`locked`/`signed_out`) |
| `proton_bridge_account_used_bytes{account}` | gauge | mail storage used |
| `proton_bridge_account_total_bytes{account}` | gauge | mail storage quota |
| `proton_bridge_internet_connected` | gauge | bridge's internet status |
| `proton_bridge_user_disconnected_total{account}` | counter | dropped-auth events |
| `proton_bridge_user_bad_event_total{account}` | counter | sync error events |
| `proton_bridge_imap_login_failed_total{account}` | counter | failed IMAP client logins |
| `proton_bridge_exporter_scrape_errors_total` | counter | exporter-side gRPC errors |

Account/event series only appear once at least one account is logged in or the
relevant event has fired.

Configuration via env: `CONTAINER_METRICS_PORT` (listen port, default `9154`)
and `BRIDGE_GRPC_CONFIG` (override the config path).

## gRPC stubs (`bridgepb/`) and upstream coupling

`bridgepb/` is not committed. The Docker build copies `bridge.pb.go` and
`bridge_grpc.pb.go` from `internal/frontend/grpc/` of the Bridge checkout pinned
in `../image.toml` (GPL-3.0), renaming only the Go `package` to `bridgepb`, so
the stubs always match the Bridge version being built.

The gRPC API is Bridge's internal GUI contract and can change between releases;
if it does, the exporter fails to compile and the image build fails. For a local
`go build ./...`, fetch the stubs first:

```sh
tag="$(sed -n 's/.*ENV_PROTONMAIL_BRIDGE_VERSION = "\([^"]*\)".*/\1/p' ../image.toml)"
mkdir -p bridgepb
for f in bridge.pb.go bridge_grpc.pb.go; do
  curl -fsSL "https://raw.githubusercontent.com/ProtonMail/proton-bridge/${tag}/internal/frontend/grpc/${f}" \
    -o "bridgepb/${f}"
done
sed -i 's/^package grpc$/package bridgepb/' bridgepb/*.pb.go
```
