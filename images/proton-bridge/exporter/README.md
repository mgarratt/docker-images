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

`bridgepb/bridge.pb.go` and `bridgepb/bridge_grpc.pb.go` are **vendored verbatim**
from `ProtonMail/proton-bridge` (`internal/frontend/grpc/`, GPL-3.0), with only
the Go `package` declaration renamed to `bridgepb`. `bridgepb/bridge.proto` is
kept alongside for provenance.

The gRPC API is Bridge's internal GUI contract, so it can change between
releases. **On each upstream version bump, re-vendor these files** from the
matching tag:

```sh
tag=v3.25.0
for f in bridge.pb.go bridge_grpc.pb.go bridge.proto; do
  curl -fsSL "https://raw.githubusercontent.com/ProtonMail/proton-bridge/${tag}/internal/frontend/grpc/${f}" \
    -o "bridgepb/${f}"
done
sed -i 's/^package grpc$/package bridgepb/' bridgepb/bridge.pb.go bridgepb/bridge_grpc.pb.go
```

Then `go build ./...` and adjust `main.go` if the schema changed.
