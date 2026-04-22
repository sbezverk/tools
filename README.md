# tools

A collection of reusable Go packages for network telemetry, peer monitoring, generic data structures, and Cisco IOS XR / NX-OS tooling.

**Module:** `github.com/sbezverk/tools`  
**Go version:** 1.25+

---

## Table of Contents

- [Building](#building)
- [Root package — `tools`](#root-package--tools)
  - [Hex utilities](#hex-utilities)
  - [Address validation](#address-validation)
  - [Signal handler](#signal-handler)
- [Package `peer`](#package-peer)
  - [Peer](#peer-1)
  - [Monitor](#monitor)
- [Package `sort`](#package-sort)
- [Package `store`](#package-store)
- [Package `telemetry_feeder`](#package-telemetry_feeder)
  - [gRPC feeder](#grpc-feeder)
  - [UDP feeder](#udp-feeder)
  - [Offline feeder](#offline-feeder)
  - [Proto schemas](#proto-schemas)
- [Tool `xr_getproto`](#tool-xr_getproto)

---

## Building

Both standard Go tooling and Bazel are supported.

### Go

```sh
go build ./...
go test ./...
```

### Bazel

```sh
bazel build //...
bazel test //...
```

Cross-platform targets:

```sh
bazel build --config=linux_amd64  //...
bazel build --config=darwin_amd64 //...
bazel build --config=darwin_arm64 //...
```

---

## Root package — `tools`

```go
import "github.com/sbezverk/tools"
```

### Hex utilities

#### `MessageHex(b []byte) string`

Formats a byte slice as a human-readable hex string suitable for logging.

```go
s := tools.MessageHex([]byte{0xDE, 0xAD, 0xBE, 0xEF})
// s == "[ 0xDE, 0xAD, 0xBE, 0xEF ]"
```

#### `ConvertToHex(b byte) string`

Returns the two-character uppercase hex representation of a single byte.

```go
s := tools.ConvertToHex(0x0F)
// s == "0F"
```

### Address validation

#### `HostAddrValidator(addr string) error`

Validates that `addr` is a well-formed `host:port` string — splits with
`net.SplitHostPort`, performs a DNS lookup on the host, and checks that the
port is in the range `1–65535`.

```go
if err := tools.HostAddrValidator("router1.example.com:57400"); err != nil {
    log.Fatalf("invalid address: %v", err)
}
```

#### `URLAddrValidation(addr string) error`

Parses a full URL and then calls `HostAddrValidator` on the host portion.

```go
if err := tools.URLAddrValidation("grpc://10.0.0.1:57400"); err != nil {
    log.Fatalf("invalid URL: %v", err)
}
```

### Signal handler

```go
import "github.com/sbezverk/tools"
```

#### `SetupSignalHandler() <-chan struct{}`

Returns a channel that is closed when the process receives its first `SIGINT`
(Ctrl-C). A second `SIGINT` calls `os.Exit(1)` immediately. Only one call per
process is allowed; a second call panics.

```go
stopCh := tools.SetupSignalHandler()

// block until Ctrl-C
<-stopCh
fmt.Println("shutting down")
```

---

## Package `peer`

```go
import "github.com/sbezverk/tools/peer"
```

Implements a UDP keepalive peer-monitoring system. Local and remote endpoints
exchange small binary heartbeat messages. A `Monitor` aggregates multiple peers
and emits events whenever any peer transitions between UP and DOWN.

### Peer

A `Peer` manages a single local ↔ remote UDP keepalive session.

```go
type Peer interface {
    Start()
    Stop()
    GetSessionStateChangeCh() chan *SessionState
    GetRemoteAddr() string
    Alive() bool
}
```

**Creating a peer:**

```go
stateCh := make(chan *peer.SessionState, 10)

p, err := peer.NewPeer(
    "192.168.1.1",  // local address (without port)
    9000,           // UDP port used on both ends
    "192.168.1.2",  // remote address
    stateCh,
)
if err != nil {
    log.Fatal(err)
}

p.Start()
defer p.Stop()

for s := range stateCh {
    fmt.Printf("peer %s is now %s\n", s.RemotePeer, s.PeerState)
}
```

**Keepalive message** (`KeepaliveMessageLen = 9` bytes):

| Field         | Type     | Description                                    |
|---------------|----------|------------------------------------------------|
| `Priority`    | uint8    | Peer priority                                  |
| `Interval`    | uint32   | Keepalive interval in milliseconds (default 1000) |
| `DeadInterval`| uint32   | Dead interval in milliseconds (default 3000)   |

The peer is declared DOWN after `DeadInterval / Interval` consecutive missed
keepalive intervals.

### Monitor

`Monitor` wraps multiple peers behind a single event channel. All peer state
changes are multiplexed onto one `chan *MonitorMessage`.

```go
type Monitor interface {
    GetMonitorCh() chan *MonitorMessage
    Stop()
}
```

```go
mon, err := peer.SetupMonitorForRemotePeer(
    "192.168.1.1",     // local address
    9000,              // port
    "192.168.1.2",     // one or more remote peers
    "192.168.1.3",
)
if err != nil {
    log.Fatal(err)
}
defer mon.Stop()

for msg := range mon.GetMonitorCh() {
    fmt.Printf("remote %s → %s\n", msg.RemotePeer, msg.PeerState)
}
```

**Constraints:**
- At least one remote peer must be provided.
- Port must be < 65535.
- All addresses must belong to the same IP family (no mixing of IPv4 and IPv6).

**`PeerState` values:**

| Constant   | Value | Meaning            |
|------------|-------|--------------------|
| `PeerUp`   | 1     | Peer is reachable  |
| `PeerDown` | 2     | Peer is unreachable|

---

## Package `sort`

```go
import "github.com/sbezverk/tools/sort"
```

Generic merge sort for slices of any type that satisfies
`golang.org/x/exp/constraints.Ordered` (integers, floats, strings, …).

#### `SortMergeComparableSlice[T constraints.Ordered](s []T) []T`

Sorts `s` in-place using merge sort and returns the same slice. Safe to call
on nil or empty slices.

```go
import "github.com/sbezverk/tools/sort"

nums := []int{5, 2, 8, 1, 9, 3}
sorted := sort.SortMergeComparableSlice(nums)
// sorted == [1, 2, 3, 5, 8, 9]

words := []string{"banana", "apple", "cherry"}
sort.SortMergeComparableSlice(words)
// words == ["apple", "banana", "cherry"]
```

---

## Package `store`

```go
import "github.com/sbezverk/tools/store"
```

An actor-model, goroutine-safe, in-memory key-value store. All mutations and
reads are serialised through a single manager goroutine — no mutexes required
in calling code.

### Interface

Any value that implements `Storable` can be stored:

```go
type Storable interface {
    Key() string
}
```

The store is accessed through the `Manager` interface:

```go
type Manager interface {
    Add(Storable) error    // ErrAlreadyExist if key is taken
    Remove(string) error   // ErrNotFound if key is absent
    List() []Storable      // snapshot of all values
    Get(string) Storable   // nil if not found
    Stop()                 // shut down the manager goroutine
}
```

### Usage

```go
type Route struct {
    Prefix string
    NextHop string
}

func (r *Route) Key() string { return r.Prefix }

// ---

s := store.NewStore()
defer s.Stop()

r := &Route{Prefix: "10.0.0.0/8", NextHop: "192.168.0.1"}

if err := s.Add(r); err != nil {
    log.Printf("add failed: %v", err) // store.ErrAlreadyExist
}

entry := s.Get("10.0.0.0/8")
if entry != nil {
    fmt.Println(entry.(*Route).NextHop)
}

all := s.List()
fmt.Printf("%d routes stored\n", len(all))

if err := s.Remove("10.0.0.0/8"); err != nil {
    log.Printf("remove failed: %v", err) // store.ErrNotFound
}
```

---

## Package `telemetry_feeder`

```go
import "github.com/sbezverk/tools/telemetry_feeder"
```

Defines the common interface consumed by all three transport implementations
(gRPC, UDP, offline). Callers depend only on this interface and are transport-agnostic.

```go
// A single telemetry event delivered to the caller.
type Feed struct {
    ProducerAddr net.Addr // source address of the sender
    TelemetryMsg []byte   // raw serialised protobuf payload
    Err          error    // non-nil on transport error
}

type Feeder interface {
    GetFeed() chan *Feed
    Stop()
}
```

**Sentinel errors:**

| Error                     | Meaning                                     |
|---------------------------|---------------------------------------------|
| `ErrUnmarshalTelemetryMsg`| Failed to deserialise the telemetry message |
| `ErrReceiveTelemetryMsg`  | Transport receive error                     |

### gRPC feeder

```go
import "github.com/sbezverk/tools/telemetry_feeder/grpc_feeder"
```

Starts a gRPC server that accepts Cisco MDT dial-out connections
(`gRPCMdtDialout` service). Each inbound `MdtDialoutArgs.data` payload is
emitted as a `*Feed`.

```go
f, err := grpc_feeder.New("0.0.0.0:57500")
if err != nil {
    log.Fatal(err)
}
defer f.Stop()

for feed := range f.GetFeed() {
    if feed.Err != nil {
        log.Printf("grpc error: %v", feed.Err)
        continue
    }
    // decode feed.TelemetryMsg as *telemetry.Telemetry
    msg := &telemetry.Telemetry{}
    if err := proto.Unmarshal(feed.TelemetryMsg, msg); err != nil {
        log.Printf("unmarshal: %v", err)
        continue
    }
    fmt.Printf("path=%s ts=%d\n", msg.EncodingPath, msg.MsgTimestamp)
}
```

**Server configuration defaults:**

| Setting               | Value  |
|-----------------------|--------|
| Max receive msg size  | 4 MB   |
| Keepalive time        | 30 s   |
| Keepalive timeout     | 10 s   |

The router side (IOS XR / NX-OS) must be configured with `destination-group`
pointing at the listener address and `encoding self-describing-gpb` or
`encoding gpb`.

### UDP feeder

```go
import "github.com/sbezverk/tools/telemetry_feeder/udp_feeder"
```

Listens on a UDP socket and emits each received datagram as a `*Feed`. The
feed channel is internally buffered at 100 messages.

```go
f, err := udp_feeder.New("0.0.0.0:57500")
if err != nil {
    log.Fatal(err)
}
defer f.Stop()

for feed := range f.GetFeed() {
    if feed.Err != nil {
        log.Printf("udp error: %v", feed.Err)
        continue
    }
    fmt.Printf("received %d bytes from %s\n",
        len(feed.TelemetryMsg), feed.ProducerAddr)
}
```

Maximum datagram size: **4 MB**.

### Offline feeder

```go
import "github.com/sbezverk/tools/telemetry_feeder/offline_feeder"
```

Replays telemetry from a binary capture file at a rate of one message per
second. The file format is a simple length-prefixed stream:

```
[ 4-byte big-endian uint32 length ][ <length> bytes payload ]
[ 4-byte big-endian uint32 length ][ <length> bytes payload ]
...
```

```go
f, err := offline_feeder.New("/path/to/capture.bin")
if err != nil {
    log.Fatal(err)
}
defer f.Stop()

for feed := range f.GetFeed() {
    if feed.Err != nil {
        break // EOF or read error
    }
    msg := &telemetry.Telemetry{}
    proto.Unmarshal(feed.TelemetryMsg, msg)
    // process msg ...
}
```

The channel is closed when EOF is reached or `Stop()` is called.

### Proto schemas

All protobuf-generated Go packages live under `telemetry_feeder/proto/`.

| Import path | Description |
|---|---|
| `.../proto/telemetry` | Cisco MDT envelope (`Telemetry`, `TelemetryField`, `TelemetryGPBTable`) |
| `.../proto/mdtdialout` | `gRPCMdtDialout` service — used internally by `grpc_feeder` |
| `.../proto/adjacency` | NX-OS adjacency add/delete/update events |
| `.../proto/mac_all` | NX-OS MAC table events |
| `.../proto/urib` | NX-OS Unicast RIB (L3 route / next-hop) events |
| `.../proto/ios-xr-rib/...` | IOS XR RIB YANG-generated schemas (44 message types) |

**Decoding a self-describing GPB-KV telemetry message:**

```go
import (
    "github.com/sbezverk/tools/telemetry_feeder/proto/telemetry"
    "google.golang.org/protobuf/proto"
)

msg := &telemetry.Telemetry{}
if err := proto.Unmarshal(feed.TelemetryMsg, msg); err != nil {
    return err
}

fmt.Printf("node:         %s\n", msg.GetNodeIdStr())
fmt.Printf("subscription: %s\n", msg.GetSubscriptionIdStr())
fmt.Printf("path:         %s\n", msg.EncodingPath)
fmt.Printf("collection:   %d\n", msg.CollectionId)

// GPB-KV rows
for _, field := range msg.DataGpbkv {
    fmt.Printf("  field: %s\n", field.Name)
    for _, child := range field.Fields {
        fmt.Printf("    %s = %v\n", child.Name, child.ValueByType)
    }
}
```

**Decoding NX-OS adjacency events:**

```go
import (
    adj "github.com/sbezverk/tools/telemetry_feeder/proto/adjacency"
)

a := &adj.NxAdjacencyProto{}
if err := proto.Unmarshal(row.Content, a); err != nil {
    return err
}
fmt.Printf("adj %s/%s event=%s\n",
    a.IpAddress, a.MacAddress, a.EventType)
```

**Decoding NX-OS URIB (L3 route) events:**

```go
import (
    urib "github.com/sbezverk/tools/telemetry_feeder/proto/urib"
)

route := &urib.NxL3RouteProto{}
if err := proto.Unmarshal(row.Content, route); err != nil {
    return err
}
fmt.Printf("route %s/%d vrf=%s event=%s\n",
    route.Address, route.MaskLen, route.VrfName, route.EventType)
for _, nh := range route.NextHop {
    fmt.Printf("  nexthop %s via %s\n", nh.Address, nh.OutInterface)
}
```

---

## Tool `xr_getproto`

A command-line utility that connects to a live Cisco IOS XR router and
retrieves `.proto` schema files for arbitrary YANG paths via the
`GetProtoFile` gRPC RPC.

### Build

```sh
# Go
go build -o xr_getproto ./xr_getproto

# Bazel
bazel build //xr_getproto:xr_getproto
```

### Usage

```
xr_getproto [flags]

Flags:
  -server_addr  string   Router address (host:port) [required]
  -yang_path    string   YANG path to fetch proto for [required]
  -out          string   Output file path (default: stdout)
  -req_id       int      Request ID (default: 1)
  -tls                   Enable TLS
  -ca_file      string   CA certificate file (for TLS)
  -server_name  string   Server name override (for TLS)
  -username     string   gRPC metadata username
  -password     string   gRPC metadata password
  -timeout      int      Request timeout in seconds (default: 30)
```

### Examples

Fetch the BGP RIB proto and print to stdout:

```sh
xr_getproto \
  -server_addr 10.0.0.1:57400 \
  -yang_path   "Cisco-IOS-XR-ip-rib-oper:rib/vrfs/vrf/afs/af/safs/saf/ip-rib-route-table-names/ip-rib-route-table-name/routes/route" \
  -username    admin \
  -password    secret
```

Save to a file with TLS:

```sh
xr_getproto \
  -server_addr  router.example.com:57400 \
  -yang_path    "Cisco-IOS-XR-ip-rib-oper:rib" \
  -tls \
  -ca_file      /etc/tls/ca.pem \
  -server_name  router.example.com \
  -username     admin \
  -password     secret \
  -out          xr_rib.proto
```

> **Note:** If an HTTP proxy is configured in the environment (`HTTP_PROXY`,
> `HTTPS_PROXY`) it may intercept the gRPC connection. Either unset those
> variables or add the router to `NO_PROXY` before running the tool.

### Authentication

Credentials are sent as per-RPC gRPC metadata (not TLS client certificates).
This means they work over plaintext gRPC connections as well as TLS-encrypted
ones. Use `-tls` when operating over untrusted networks.

---

## License

See [LICENSE](LICENSE).
