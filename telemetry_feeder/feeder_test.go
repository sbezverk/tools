package telemetry_feeder

import (
	"encoding/binary"
	"testing"
)

func TestMakeFeederMsgFromJsonDirectJSON(t *testing.T) {
	payload := []byte(`{"encoding_path":"rib"}`)

	got, err := MakeFeederMsgFromJson(payload, len(payload), TransportUDP)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Transport != TransportUDP {
		t.Fatalf("transport mismatch, want %q got %q", TransportUDP, got.Transport)
	}
	if got.Encoding != EncodingJSON {
		t.Fatalf("encoding mismatch, want %q got %q", EncodingJSON, got.Encoding)
	}
	if got.Framing != FramingNone {
		t.Fatalf("framing mismatch, want %q got %q", FramingNone, got.Framing)
	}
	if string(got.TelemetryMsg) != string(payload) {
		t.Fatalf("payload mismatch, want %q got %q", payload, got.TelemetryMsg)
	}

	payload[0] = 'X'
	if got.TelemetryMsg[0] != '{' {
		t.Fatalf("payload was not copied, got %q", got.TelemetryMsg)
	}
}

func TestMakeFeederMsgFromJsonNXOSUDPFraming(t *testing.T) {
	payload := []byte(`{"encoding_path":"rib"}`)
	msg := make([]byte, 6+len(payload))
	msg[0] = 0x01
	msg[1] = 0x02
	binary.BigEndian.PutUint16(msg[2:4], uint16(len(payload)))
	copy(msg[6:], payload)

	got, err := MakeFeederMsgFromJson(msg, len(msg), TransportUDP)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Framing != FramingCiscoNXOSUDP {
		t.Fatalf("framing mismatch, want %q got %q", FramingCiscoNXOSUDP, got.Framing)
	}
	if string(got.TelemetryMsg) != string(payload) {
		t.Fatalf("payload mismatch, want %q got %q", payload, got.TelemetryMsg)
	}
}

func TestMakeFeederMsgFromJsonXRSTFraming(t *testing.T) {
	payload := []byte(`{"encoding_path":"rib"}`)
	msg := make([]byte, 12+len(payload))
	binary.BigEndian.PutUint32(msg[8:12], uint32(len(payload)))
	copy(msg[12:], payload)

	got, err := MakeFeederMsgFromJson(msg, len(msg), TransportUDP)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Framing != FramingCiscoXRST {
		t.Fatalf("framing mismatch, want %q got %q", FramingCiscoXRST, got.Framing)
	}
	if string(got.TelemetryMsg) != string(payload) {
		t.Fatalf("payload mismatch, want %q got %q", payload, got.TelemetryMsg)
	}
}

func TestMakeFeederMsgFromJsonRejectsLengthMismatch(t *testing.T) {
	payload := []byte(`{"encoding_path":"rib"}`)
	msg := make([]byte, 6+len(payload))
	msg[0] = 0x01
	msg[1] = 0x02
	binary.BigEndian.PutUint16(msg[2:4], uint16(len(payload)+1))
	copy(msg[6:], payload)

	if _, err := MakeFeederMsgFromJson(msg, len(msg), TransportUDP); err == nil {
		t.Fatal("expected length mismatch error, got nil")
	}
}

func TestMakeFeederMsgFromJsonUsesReceivedLengthOnly(t *testing.T) {
	buf := make([]byte, 32)
	copy(buf[6:], []byte(`{"stale":true}`))

	if _, err := MakeFeederMsgFromJson(buf, 1, TransportUDP); err == nil {
		t.Fatal("expected short message error, got nil")
	}
}
