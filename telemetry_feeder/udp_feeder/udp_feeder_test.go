package udp_feeder

import (
	"bytes"
	"net"
	"os"
	"strings"
	"testing"
	"time"
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
