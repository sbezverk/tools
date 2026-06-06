package udp_feeder

import (
	"encoding/json"
	"errors"
	"net"
	"sync/atomic"
	"time"

	feeder "github.com/sbezverk/tools/telemetry_feeder"
)

const (
	MaxRcvMsgSize     = 1024 * 1024 * 4
	feedQueueCapacity = 1024 * 25
)

type udpFeeder struct {
	conn                        *net.UDPConn
	stopCh                      chan struct{}
	feed                        chan *feeder.Feed
	startTime                   time.Time
	messagesReceivedTotal       atomic.Int64
	payloadBytesReceivedTotal   atomic.Int64
	transportBytesReceivedTotal atomic.Int64
	feedItemsEnqueuedTotal      atomic.Int64
	feedErrorItemsEnqueuedTotal atomic.Int64
	feedQueueDepthMax           atomic.Int64
	feedPublishBlockNanosTotal  atomic.Int64
	feedPublishBlockNanosMax    atomic.Int64
	receiveErrorsTotal          atomic.Int64
	receiveTimeoutErrorsTotal   atomic.Int64
	receiveClosedTotal          atomic.Int64
	receiveOtherErrorsTotal     atomic.Int64
}

func (srv *udpFeeder) GetFeed() chan *feeder.Feed {
	return srv.feed
}

func (srv *udpFeeder) Stop() {
	close(srv.stopCh)
	srv.conn.Close()
}

func (srv *udpFeeder) statsSnapshot() feeder.StatsSnapshot {
	return feeder.StatsSnapshot{
		Transport:                   "udp",
		StartTime:                   srv.startTime.UTC(),
		UptimeSeconds:               int64(time.Since(srv.startTime).Seconds()),
		MessagesReceivedTotal:       srv.messagesReceivedTotal.Load(),
		PayloadBytesReceivedTotal:   srv.payloadBytesReceivedTotal.Load(),
		TransportBytesReceivedTotal: srv.transportBytesReceivedTotal.Load(),
		FeedItemsEnqueuedTotal:      srv.feedItemsEnqueuedTotal.Load(),
		FeedErrorItemsEnqueuedTotal: srv.feedErrorItemsEnqueuedTotal.Load(),
		FeedQueueDepth:              int64(len(srv.feed)),
		FeedQueueDepthMax:           srv.feedQueueDepthMax.Load(),
		FeedQueueCapacity:           int64(cap(srv.feed)),
		FeedPublishBlockNanosTotal:  srv.feedPublishBlockNanosTotal.Load(),
		FeedPublishBlockNanosMax:    srv.feedPublishBlockNanosMax.Load(),
		ReceiveErrorsTotal:          srv.receiveErrorsTotal.Load(),
		ReceiveTimeoutErrorsTotal:   srv.receiveTimeoutErrorsTotal.Load(),
		ReceiveClosedTotal:          srv.receiveClosedTotal.Load(),
		ReceiveOtherErrorsTotal:     srv.receiveOtherErrorsTotal.Load(),
	}
}

func (srv *udpFeeder) GetStatsJson() ([]byte, error) {
	snapshot := srv.statsSnapshot()
	return json.Marshal(snapshot)
}

func classifyReceiveError(err error) string {
	if err == nil {
		return "none"
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return "timeout"
	}
	if errors.Is(err, net.ErrClosed) {
		return "closed"
	}
	if opErr, ok := err.(*net.OpError); ok && opErr.Err != nil && opErr.Err.Error() == "use of closed network connection" {
		return "closed"
	}
	return "other"
}

func updateMax(max *atomic.Int64, value int64) {
	for {
		current := max.Load()
		if value <= current || max.CompareAndSwap(current, value) {
			return
		}
	}
}

func (srv *udpFeeder) publishFeed(item *feeder.Feed) bool {
	// Ensure a closed stopCh always prevents publishing, even if the send would not block.
	select {
	case <-srv.stopCh:
		return false
	default:
	}
	queueDepthAfterSend := int64(len(srv.feed) + 1)
	if queueDepthAfterSend > int64(cap(srv.feed)) {
		queueDepthAfterSend = int64(cap(srv.feed))
	}
	started := time.Now()
	select {
	case <-srv.stopCh:
		return false
	case srv.feed <- item:
		blocked := time.Since(started).Nanoseconds()
		srv.feedItemsEnqueuedTotal.Add(1)
		if item.Err != nil {
			srv.feedErrorItemsEnqueuedTotal.Add(1)
		}
		srv.feedPublishBlockNanosTotal.Add(blocked)
		updateMax(&srv.feedPublishBlockNanosMax, blocked)
		updateMax(&srv.feedQueueDepthMax, queueDepthAfterSend)
		return true
	}
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
		conn:      conn,
		stopCh:    make(chan struct{}),
		feed:      make(chan *feeder.Feed, feedQueueCapacity),
		startTime: time.Now(),
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
				errClass := classifyReceiveError(err)
				switch errClass {
				case "timeout":
					srv.receiveErrorsTotal.Add(1)
					srv.receiveTimeoutErrorsTotal.Add(1)
				case "closed":
					srv.receiveErrorsTotal.Add(1)
					srv.receiveClosedTotal.Add(1)
				case "other":
					srv.receiveErrorsTotal.Add(1)
					srv.receiveOtherErrorsTotal.Add(1)
				}
				if errClass == "closed" {
					select {
					case <-srv.stopCh:
						return nil
					default:
					}
				}
				if !srv.publishFeed(&feeder.Feed{
					ProducerAddr: producerAddr,
					TelemetryMsg: nil,
					Err:          err,
					Transport:    feeder.TransportUDP,
					Encoding:     feeder.EncodingJSON,
					Framing:      feeder.FramingNone,
				}) {
					return nil
				}
				// Need to check the error, if local socket is closed, there is no point to continue receiving messages, just return
				if errClass == "closed" {
					return nil
				}
				continue
			} else {
				srv.messagesReceivedTotal.Add(1)
				srv.transportBytesReceivedTotal.Add(int64(n))
				feedMsg, err := feeder.MakeFeederMsgFromJson(buf[:n], n, feeder.TransportUDP)
				if err != nil {
					srv.payloadBytesReceivedTotal.Add(int64(n))
					if !srv.publishFeed(&feeder.Feed{
						ProducerAddr: producerAddr,
						Err:          err,
						Transport:    feeder.TransportUDP,
						Encoding:     feeder.EncodingJSON,
						Framing:      feeder.FramingNone,
					}) {
						return nil
					}
					continue
				}
				srv.payloadBytesReceivedTotal.Add(int64(len(feedMsg.TelemetryMsg)))
				feedMsg.ProducerAddr = producerAddr
				if !srv.publishFeed(&feedMsg) {
					return nil
				}
			}
		}
	}
}
