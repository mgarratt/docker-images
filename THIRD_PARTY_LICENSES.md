# Third-Party Licensing Notices

## Proton Bridge Image (`images/proton-bridge`)

The `proton-bridge` image distributes binaries built from:

- `ProtonMail/proton-bridge` (GPL-3.0)
- Adapted bootstrap/containerization ideas from `shenxn/protonmail-bridge-docker` (GPL-3.0)
- Additional referenced implementation patterns from `VideoCurio/ProtonMailBridgeDocker` (GPL-3.0)

The bundled `proton-bridge-exporter` additionally vendors the Bridge gRPC client
stubs (`images/proton-bridge/exporter/bridgepb/`, GPL-3.0, taken verbatim from
`ProtonMail/proton-bridge` with only the Go package name changed) and links Go
libraries under their own permissive licences (gRPC and Prometheus client
under Apache-2.0; `google.golang.org/protobuf` under BSD-3-Clause).

Relevant local files:

- `images/proton-bridge/LICENSE` (full GPL-3.0 license text)
- `images/proton-bridge/NOTICE` (attribution and provenance)
- `images/proton-bridge/exporter/README.md` (exporter provenance and stub regeneration)

Upstream sources:

- https://github.com/ProtonMail/proton-bridge
- https://github.com/shenxn/protonmail-bridge-docker
- https://github.com/VideoCurio/ProtonMailBridgeDocker
