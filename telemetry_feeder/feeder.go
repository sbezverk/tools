package telemetry_feeder

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

var (
	ErrUnmarshalTelemetryMsg = errors.New("failed to unmarshal telemetry message")
	ErrReceiveTelemetryMsg   = errors.New("failed to receive telemetry message")
)

type Transport string
type PayloadEncoding string
type Framing string

const (
	TransportGRPC Transport = "grpc"
	TransportUDP  Transport = "udp"
	TransportTCP  Transport = "tcp"

	EncodingGPB  PayloadEncoding = "gpb"
	EncodingJSON PayloadEncoding = "json"

	FramingNone         Framing = "none"
	FramingCiscoXRST    Framing = "cisco-xr-st"
	FramingCiscoNXOSUDP Framing = "cisco-nxos-udp"
)

type Feed struct {
	ProducerAddr net.Addr
	TelemetryMsg []byte
	Err          error
	Transport    Transport
	Encoding     PayloadEncoding
	Framing      Framing
}

func MakeFeederMsgFromJson(b []byte, n int, transport Transport) (Feed, error) {
	m := Feed{}
	if n < 0 {
		return m, fmt.Errorf("invalid message length: %d", n)
	}
	if n == 0 {
		return m, fmt.Errorf("empty message")
	}
	if n > len(b) {
		return m, fmt.Errorf("invalid message length: %d exceeds buffer size %d", n, len(b))
	}

	msg := b[:n]
	if msg[0] == '{' {
		// Direct JSON, just build the Feed Message with the payload
		m.Transport = transport
		m.Encoding = EncodingJSON
		m.Framing = FramingNone
		m.TelemetryMsg = make([]byte, n)
		copy(m.TelemetryMsg, msg)
		return m, nil
	}
	// Three possibilities:
	// 1. The message is framed with a header that indicates the length of the JSON payload, e.g. Cisco XR ST framing
	// 2. The message is framed with a header that indicates the length of the JSON payload, e.g. Cisco NXOS UDP framing
	// 3. The message is not JSON at all and is malformed

	if n < 6 {
		return m, fmt.Errorf("message too short to contain framing header: length %d", n)
	}
	if msg[0] == 0x01 && msg[1] == 0x02 {
		if n <= 6 {
			return m, fmt.Errorf("Cisco NX-OS UDP framed message has no JSON payload: length %d", n)
		}
		payloadLength := int(binary.BigEndian.Uint16(msg[2:4]))
		expectedLength := payloadLength + 6
		if expectedLength != n {
			return m, fmt.Errorf("Cisco NX-OS UDP framing length mismatch: payload length %d plus header 6 does not match message length %d", payloadLength, n)
		}
		m.Transport = transport
		m.Encoding = EncodingJSON
		m.Framing = FramingCiscoNXOSUDP
		m.TelemetryMsg = make([]byte, payloadLength)
		copy(m.TelemetryMsg, msg[6:])
		if m.TelemetryMsg[0] != '{' {
			return Feed{}, fmt.Errorf("Cisco NX-OS UDP framing payload does not start with JSON object")
		}
		return m, nil
	}

	if n <= 12 {
		return m, fmt.Errorf("message too short to contain Cisco XR ST framing header: length %d", n)
	}
	payloadLength := binary.BigEndian.Uint32(msg[8:12])
	expectedLength := uint64(payloadLength) + 12
	if expectedLength != uint64(n) {
		return m, fmt.Errorf("Cisco XR ST framing length mismatch: payload length %d plus header 12 does not match message length %d", payloadLength, n)
	}
	m.Transport = transport
	m.Encoding = EncodingJSON
	m.Framing = FramingCiscoXRST
	m.TelemetryMsg = make([]byte, int(payloadLength))
	copy(m.TelemetryMsg, msg[12:])
	if m.TelemetryMsg[0] != '{' {
		return Feed{}, fmt.Errorf("Cisco XR ST framing payload does not start with JSON object")
	}
	return m, nil
}

type StatsSnapshot struct {
	Transport                   string    `json:"transport"`
	StartTime                   time.Time `json:"start_time"`
	UptimeSeconds               int64     `json:"uptime_seconds"`
	MessagesReceivedTotal       int64     `json:"messages_received_total"`
	PayloadBytesReceivedTotal   int64     `json:"payload_bytes_received_total"`
	TransportBytesReceivedTotal int64     `json:"transport_bytes_received_total"`
	FeedItemsEnqueuedTotal      int64     `json:"feed_items_enqueued_total"`
	FeedErrorItemsEnqueuedTotal int64     `json:"feed_error_items_enqueued_total"`
	FeedQueueDepth              int64     `json:"feed_queue_depth"`
	FeedQueueDepthMax           int64     `json:"feed_queue_depth_max"`
	FeedQueueCapacity           int64     `json:"feed_queue_capacity"`
	FeedPublishBlockNanosTotal  int64     `json:"feed_publish_block_nanos_total"`
	FeedPublishBlockNanosMax    int64     `json:"feed_publish_block_nanos_max"`
	ReceiveErrorsTotal          int64     `json:"receive_errors_total"`
	ReceiveTimeoutErrorsTotal   int64     `json:"receive_timeout_errors_total"`
	ReceiveClosedTotal          int64     `json:"receive_closed_total"`
	ReceiveOtherErrorsTotal     int64     `json:"receive_other_errors_total"`
}

type Feeder interface {
	GetStatsJson() ([]byte, error)
	GetFeed() chan *Feed
	Stop()
}
