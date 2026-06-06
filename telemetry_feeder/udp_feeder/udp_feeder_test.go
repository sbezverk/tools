package udp_feeder

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	feeder "github.com/sbezverk/tools/telemetry_feeder"
)

func TestNewReceivesDatagram(t *testing.T) {
	f, addr := newTestFeeder(t)
	defer f.Stop()

	conn := newUDPClient(t)
	defer conn.Close()

	payload := []byte(`{"encoding_path":"rib","data":[{"vrfName":"default"}]}`)
	if _, err := conn.WriteToUDP(payload, addr); err != nil {
		t.Fatalf("failed to send test datagram: %v", err)
	}

	select {
	case got := <-f.GetFeed():
		if got == nil {
			t.Fatal("expected feed item, got nil")
		}
		if got.Err != nil {
			t.Fatalf("expected nil error, got %v", got.Err)
		}
		if !bytes.Equal(got.TelemetryMsg, payload) {
			t.Fatalf("payload mismatch, want %q got %q", payload, got.TelemetryMsg)
		}
		if got.ProducerAddr == nil {
			t.Fatal("expected producer address to be populated")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for datagram")
	}
}

func TestQueuedDatagramsKeepIndependentPayloads(t *testing.T) {
	f, addr := newTestFeeder(t)
	defer f.Stop()

	conn := newUDPClient(t)
	defer conn.Close()

	firstPayload := []byte(`{"payload":"first-00000000"}`)
	secondPayload := []byte(`{"payload":"second-0000000"}`)
	if len(firstPayload) != len(secondPayload) {
		t.Fatalf("test payload lengths must match, got %d and %d", len(firstPayload), len(secondPayload))
	}

	if _, err := conn.WriteToUDP(firstPayload, addr); err != nil {
		t.Fatalf("failed to send first datagram: %v", err)
	}
	if _, err := conn.WriteToUDP(secondPayload, addr); err != nil {
		t.Fatalf("failed to send second datagram: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for f.statsSnapshot().MessagesReceivedTotal < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := f.statsSnapshot().MessagesReceivedTotal; got != 2 {
		t.Fatalf("timed out waiting for two datagrams, got %d", got)
	}

	var firstFeed, secondFeed *feeder.Feed
	select {
	case firstFeed = <-f.GetFeed():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first feed")
	}
	select {
	case secondFeed = <-f.GetFeed():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second feed")
	}
	if !((bytes.Equal(firstFeed.TelemetryMsg, firstPayload) && bytes.Equal(secondFeed.TelemetryMsg, secondPayload)) ||
		(bytes.Equal(firstFeed.TelemetryMsg, secondPayload) && bytes.Equal(secondFeed.TelemetryMsg, firstPayload))) {
		t.Fatalf("payloads mismatch, got %q and %q", firstFeed.TelemetryMsg, secondFeed.TelemetryMsg)
	}
}

func TestMalformedDatagramPublishesErrorFeed(t *testing.T) {
	f, addr := newTestFeeder(t)
	defer f.Stop()

	conn := newUDPClient(t)
	defer conn.Close()

	payload := []byte("not-json")
	if _, err := conn.WriteToUDP(payload, addr); err != nil {
		t.Fatalf("failed to send test datagram: %v", err)
	}

	var got *feeder.Feed
	select {
	case got = <-f.GetFeed():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for error feed")
	}

	if got == nil {
		t.Fatal("expected feed item, got nil")
	}
	if got.Err == nil {
		t.Fatal("expected error feed item, got nil error")
	}
	if got.TelemetryMsg != nil {
		t.Fatalf("expected nil telemetry payload for error feed, got %q", got.TelemetryMsg)
	}
	if got.ProducerAddr == nil {
		t.Fatal("expected producer address to be populated")
	}
	if got.Transport != feeder.TransportUDP {
		t.Fatalf("transport mismatch, want %q got %q", feeder.TransportUDP, got.Transport)
	}
	if got.Encoding != feeder.EncodingJSON {
		t.Fatalf("encoding mismatch, want %q got %q", feeder.EncodingJSON, got.Encoding)
	}

	stats := f.statsSnapshot()
	if stats.MessagesReceivedTotal != 1 {
		t.Fatalf("messages_received_total mismatch, want 1 got %d", stats.MessagesReceivedTotal)
	}
	if stats.FeedItemsEnqueuedTotal != 1 {
		t.Fatalf("feed_items_enqueued_total mismatch, want 1 got %d", stats.FeedItemsEnqueuedTotal)
	}
	if stats.FeedErrorItemsEnqueuedTotal != 1 {
		t.Fatalf("feed_error_items_enqueued_total mismatch, want 1 got %d", stats.FeedErrorItemsEnqueuedTotal)
	}
}

func TestStatsAfterDatagram(t *testing.T) {
	f, addr := newTestFeeder(t)
	defer f.Stop()

	conn := newUDPClient(t)
	defer conn.Close()

	payload := []byte(`{"encoding_path":"rib","data":[{"vrfName":"default"}]}`)
	if _, err := conn.WriteToUDP(payload, addr); err != nil {
		t.Fatalf("failed to send test datagram: %v", err)
	}

	select {
	case <-f.GetFeed():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for datagram")
	}

	statsBytes, err := f.GetStatsJson()
	if err != nil {
		t.Fatalf("failed to marshal stats: %v", err)
	}

	var stats feeder.StatsSnapshot
	if err := json.Unmarshal(statsBytes, &stats); err != nil {
		t.Fatalf("failed to unmarshal stats: %v", err)
	}

	if stats.Transport != "udp" {
		t.Fatalf("transport mismatch, want udp got %q", stats.Transport)
	}
	if stats.MessagesReceivedTotal != 1 {
		t.Fatalf("messages_received_total mismatch, want 1 got %d", stats.MessagesReceivedTotal)
	}
	if stats.PayloadBytesReceivedTotal != int64(len(payload)) {
		t.Fatalf("payload_bytes_received_total mismatch, want %d got %d", len(payload), stats.PayloadBytesReceivedTotal)
	}
	if stats.TransportBytesReceivedTotal != int64(len(payload)) {
		t.Fatalf("transport_bytes_received_total mismatch, want %d got %d", len(payload), stats.TransportBytesReceivedTotal)
	}
	if stats.FeedItemsEnqueuedTotal != 1 {
		t.Fatalf("feed_items_enqueued_total mismatch, want 1 got %d", stats.FeedItemsEnqueuedTotal)
	}
	if stats.FeedQueueCapacity != feedQueueCapacity {
		t.Fatalf("feed_queue_capacity mismatch, want %d got %d", feedQueueCapacity, stats.FeedQueueCapacity)
	}
}

func TestStopUnblocksWorker(t *testing.T) {
	f, _ := newTestFeeder(t)

	done := make(chan struct{})
	go func() {
		f.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return in time")
	}
}

func TestReceiveAfterStopDoesNotPublish(t *testing.T) {
	f, addr := newTestFeeder(t)
	f.Stop()

	conn := newUDPClient(t)
	defer conn.Close()

	if _, err := conn.WriteToUDP([]byte("ignored"), addr); err != nil {
		// Once the listener is closed, either the send succeeds and is dropped or fails locally.
		// Both are acceptable for this test.
	}

	select {
	case got := <-f.GetFeed():
		t.Fatalf("expected no feed items after stop, got %+v", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func newTestFeeder(t *testing.T) (*udpFeeder, *net.UDPAddr) {
	t.Helper()

	fdr, err := New("127.0.0.1:0")
	if err != nil {
		if isSocketPermissionError(err) {
			t.Skipf("udp sockets are not available in this environment: %v", err)
		}
		t.Fatalf("failed to create feeder: %v", err)
	}

	f, ok := fdr.(*udpFeeder)
	if !ok {
		t.Fatalf("unexpected feeder type %T", fdr)
	}

	addr, ok := f.conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("unexpected local address type %T", f.conn.LocalAddr())
	}

	return f, addr
}

func newUDPClient(t *testing.T) *net.UDPConn {
	t.Helper()

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		if isSocketPermissionError(err) {
			t.Skipf("udp sockets are not available in this environment: %v", err)
		}
		t.Fatalf("failed to create UDP client: %v", err)
	}

	return conn
}

func isSocketPermissionError(err error) bool {
	return os.IsPermission(err) || strings.Contains(err.Error(), "operation not permitted")
}
