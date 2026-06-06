package grpc_feeder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"

	feeder "github.com/sbezverk/tools/telemetry_feeder"
	"github.com/sbezverk/tools/telemetry_feeder/proto/mdtdialout"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	MaxRcvMsgSize     = 1024 * 1024 * 4
	feedQueueCapacity = 1024 * 10
)

type grpcSrv struct {
	conn                        net.Listener
	gSrv                        *grpc.Server
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
	mdtdialout.UnimplementedGRPCMdtDialoutServer
}

func (srv *grpcSrv) GetFeed() chan *feeder.Feed {
	return srv.feed
}

func (srv *grpcSrv) Stop() {
	srv.gSrv.Stop()
	close(srv.stopCh)
	srv.conn.Close()
}

func (srv *grpcSrv) statsSnapshot() feeder.StatsSnapshot {
	return feeder.StatsSnapshot{
		Transport:                   "grpc",
		StartTime:                   srv.startTime,
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

func (srv *grpcSrv) GetStatsJson() ([]byte, error) {
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
	if status.Code(err) == codes.DeadlineExceeded {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) || status.Code(err) == codes.Canceled {
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

func (srv *grpcSrv) publishFeed(item *feeder.Feed) bool {
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
	conn, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	srv := &grpcSrv{
		conn:      conn,
		stopCh:    make(chan struct{}),
		feed:      make(chan *feeder.Feed, feedQueueCapacity),
		startTime: time.Now().UTC(),
		gSrv: grpc.NewServer(
			grpc.MaxRecvMsgSize(MaxRcvMsgSize),
			grpc.KeepaliveParams(keepalive.ServerParameters{Time: time.Second * 30, Timeout: time.Second * 10}),
		),
	}
	mdtdialout.RegisterGRPCMdtDialoutServer(srv.gSrv, srv)

	go srv.gSrv.Serve(conn)

	return srv, nil
}

func (srv *grpcSrv) MdtDialout(session mdtdialout.GRPCMdtDialout_MdtDialoutServer) error {
	infoCh := make(chan *mdtdialout.MdtDialoutArgs)
	errCh := make(chan error)

	return srv.worker(session, infoCh, errCh)
}

func (srv *grpcSrv) worker(session mdtdialout.GRPCMdtDialout_MdtDialoutServer,
	infoCh chan *mdtdialout.MdtDialoutArgs,
	errCh chan error) error {
	var producer net.Addr
	if p, ok := peer.FromContext(session.Context()); ok {
		producer = p.Addr
	} else {
		producer = &net.IPAddr{IP: net.ParseIP("0.0.0.0")}
	}
	go func(iCh chan *mdtdialout.MdtDialoutArgs, eCh chan error) {
		for {
			info, err := session.Recv()
			if err != nil {
				errClass := classifyReceiveError(err)
				switch errClass {
				case "timeout":
					srv.receiveErrorsTotal.Add(1)
					srv.receiveTimeoutErrorsTotal.Add(1)
				case "closed":
					srv.receiveClosedTotal.Add(1)
				case "other":
					srv.receiveErrorsTotal.Add(1)
					srv.receiveOtherErrorsTotal.Add(1)
				}
				// Before sending the message, check if gRPC session has not been canceled
				if status.Code(session.Context().Err()) == codes.Canceled {
					select {
					case eCh <- fmt.Errorf("connection with peer %s has been canceled: %w", producer.String(), context.Canceled):
					case <-srv.stopCh:
						return
					}
				} else if err == io.EOF {
					select {
					case eCh <- fmt.Errorf("connection with peer %s has been closed cleanly: %w", producer.String(), io.EOF):
					case <-srv.stopCh:
						return
					}
				} else {
					select {
					case eCh <- fmt.Errorf("connection with peer %s has been terminated with the error: %w", producer.String(), err):
					case <-srv.stopCh:
						return
					}
				}
				return
			}
			// Got telemetry info, sending it to the parent for further processing
			select {
			case iCh <- info:
			case <-srv.stopCh:
				return
			}
		}
	}(infoCh, errCh)
	for {
		select {
		case msg := <-infoCh:
			if msg == nil {
				continue
			}
			f := &feeder.Feed{
				ProducerAddr: producer,
			}
			data := msg.GetData()
			f.TelemetryMsg = make([]byte, len(data))
			copy(f.TelemetryMsg, data)
			f.Err = nil
			srv.messagesReceivedTotal.Add(1)
			srv.payloadBytesReceivedTotal.Add(int64(len(data)))
			srv.transportBytesReceivedTotal.Add(int64(proto.Size(msg)))
			// Sending recieved Telemetry message for processing
			if !srv.publishFeed(f) {
				return nil
			}
		case err := <-errCh:
			if !srv.publishFeed(&feeder.Feed{
				ProducerAddr: producer,
				TelemetryMsg: nil,
				Err:          err,
			}) {
				return nil
			}
			return err
		case <-srv.stopCh:
			return nil
		}
	}
}
