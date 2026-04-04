package udp_feeder

import (
	"net"

	feeder "github.com/sbezverk/tools/telemetry_feeder"
)

const (
	MaxRcvMsgSize = 1024 * 1024 * 4
)

type udpFeeder struct {
	conn   *net.UDPConn
	stopCh chan struct{}
	feed   chan *feeder.Feed
}

func (srv *udpFeeder) GetFeed() chan *feeder.Feed {
	return srv.feed
}

func (srv *udpFeeder) Stop() {
	close(srv.stopCh)
	srv.conn.Close()
}

func New(addr string) (feeder.Feeder, error) {
	// Need to open UDP socket to listen for incoming telemetry messages
	srvAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", srvAddr)
	if err != nil {
		return nil, err
	}
	srv := &udpFeeder{
		conn:   conn,
		stopCh: make(chan struct{}),
		feed:   make(chan *feeder.Feed, 100),
	}
	go srv.worker()

	return srv, nil
}

func (srv *udpFeeder) worker() error {
	buf := make([]byte, MaxRcvMsgSize)
	for {
		select {
		case <-srv.stopCh:
			return nil
		default:
			n, producerAddr, err := srv.conn.ReadFrom(buf)
			if err != nil {
				select {
				case <-srv.stopCh:
					return nil
				case srv.feed <- &feeder.Feed{
					ProducerAddr: producerAddr,
					TelemetryMsg: nil,
					Err:          err,
				}:
				}
				// Need to check the error, if local socket is closed, there is no point to continue receiving messages, just return
				if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "use of closed network connection" {
					return nil
				}
				continue
			} else {
				select {
				case <-srv.stopCh:
					return nil
				case srv.feed <- &feeder.Feed{
					ProducerAddr: producerAddr,
					TelemetryMsg: buf[:n],
					Err:          nil,
				}:
				}
			}
		}
	}
}
