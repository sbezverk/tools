# XR GetProtoFile Helper

This directory contains a small standalone client for Cisco IOS XR's
`GetProtoFile` gRPC RPC. It is useful when XR is configured for native
telemetry and you need the authoritative `.proto` schema for a YANG path.

The client was added after validating that XR can return model-specific proto
content on demand for RIB paths such as:

`Cisco-IOS-XR-ip-rib-ipv4-oper:rib`

## What It Does

The binary:

- connects to XR over gRPC
- sends `username` / `password` metadata expected by EMS
- calls `GetProtoFile`
- writes the returned proto text to a file or stdout

## Build

Build for Linux AMD64:

```bash
bazel build --config=linux_amd64 //xr_getproto:xr_getproto --color=no --curses=no
```

The resulting binary will be at:

```bash
bazel-bin/xr_getproto/xr_getproto_/xr_getproto
```

Build for the local macOS host:

```bash
bazel build --config=darwin_arm64 //xr_getproto:xr_getproto --color=no --curses=no
```

## Usage

Typical no-TLS IOS XR setup:

```bash
./xr_getproto \
  -server_addr 2.2.2.2:57400 \
  -username cisco \
  -password cisco123 \
  -yang_path 'Cisco-IOS-XR-ip-rib-ipv4-oper:rib' \
  -out /tmp/xr-rib.proto
```

Target a narrower subtree if XR supports it:

```bash
./xr_getproto \
  -server_addr 2.2.2.2:57400 \
  -username cisco \
  -password cisco123 \
  -yang_path 'Cisco-IOS-XR-ip-rib-ipv4-oper:rib/vrfs/vrf/afs/af/safs/saf/ip-rib-route-table-names/ip-rib-route-table-name/ribtph-entries/ribtph-entry' \
  -out /tmp/xr-rib-event.proto
```

TLS example:

```bash
./xr_getproto \
  -server_addr 2.2.2.2:57400 \
  -tls \
  -ca_file /path/to/ca.pem \
  -server_name ems.cisco.com \
  -username cisco \
  -password cisco123 \
  -yang_path 'Cisco-IOS-XR-ip-rib-ipv4-oper:rib' \
  -out /tmp/xr-rib.proto
```

## Important Notes

- If the lab host uses HTTP proxy environment variables, gRPC may try to use
  the proxy and fail before reaching XR.
- If you see `Via: ... Cisco-WSA` or an HTTP `504`, unset proxy variables for
  the command:

```bash
env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u all_proxy -u ALL_PROXY \
./xr_getproto \
  -server_addr 2.2.2.2:57400 \
  -username cisco \
  -password cisco123 \
  -yang_path 'Cisco-IOS-XR-ip-rib-ipv4-oper:rib' \
  -out /tmp/xr-rib.proto
```

- `-timeout 0` means no client-side deadline. This is often the safest choice
  for large generated proto bundles.
- Some paths may return zero bytes. A broader model root like `...:rib` can
  still return valid proto content even when a narrower subtree does not.

## Related XR Proto Workflow

The large generated RIB bundle can be placed under:

`telemetry_feeder/proto/xr-rib`

That area contains helper tooling to:

- split the concatenated XR output into individual `.proto` fragments
- generate `pb.go` files from the fragments

For normal importer use, prefer the curated package:

`telemetry_feeder/proto/ios-xr-rib`

That package is a cleaned-up public schema derived from the XR-generated RIB
route-event fragment.
