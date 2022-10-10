package grpc_feeder

import (
	"net"
	"time"

	feeder "github.com/sbezverk/tools/telemetry_feeder"
	"github.com/sbezverk/tools/telemetry_feeder/proto/mdtdialout"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const (
	MaxRcvMsgSize = 1024 * 1024
)

type grpcSrv struct {
	conn   net.Listener
	gSrv   *grpc.Server
	stopCh chan struct{}
	feed   chan *feeder.Feed
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

func New(addr string) (feeder.Feeder, error) {
	conn, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	srv := &grpcSrv{
		conn:   conn,
		stopCh: make(chan struct{}),
		feed:   make(chan *feeder.Feed),
		gSrv: grpc.NewServer(
			grpc.MaxRecvMsgSize(MaxRcvMsgSize),
			grpc.KeepaliveParams(keepalive.ServerParameters{Time: time.Second * 30, Timeout: time.Second * 10}),
			grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: time.Second * 10, PermitWithoutStream: true}),
		),
	}
	mdtdialout.RegisterGRPCMdtDialoutServer(srv.gSrv, srv)

	go srv.gSrv.Serve(conn)

	return srv, nil
}

func (srv *grpcSrv) MdtDialout(session mdtdialout.GRPCMdtDialout_MdtDialoutServer) error {
	infoCh := make(chan *mdtdialout.MdtDialoutArgs)
	errCh := make(chan error)
	defer func() {
		close(infoCh)
		close(errCh)
	}()

	return srv.worker(session, infoCh, errCh)
}

func (srv *grpcSrv) worker(session mdtdialout.GRPCMdtDialout_MdtDialoutServer,
	infoCh chan *mdtdialout.MdtDialoutArgs,
	errCh chan error) error {
	var producer net.Addr
	if p, ok := peer.FromContext(session.Context()); ok {
		producer = p.Addr
	}
	go func(iCh chan *mdtdialout.MdtDialoutArgs, eCh chan error) {
		for {
			info, err := session.Recv()
			if err != nil {
				eCh <- err
				return
			}
			// Before sending the message, check if gRPC session has not been canceled
			if status.Code(session.Context().Err()) == codes.Canceled {
				return
			}
			// Got telemetry info, sending it to the parent for further processing
			iCh <- info
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
			f.TelemetryMsg = make([]byte, len(msg.Data))
			copy(f.TelemetryMsg, msg.Data)
			f.Err = nil
			// Sending recieved Telemetry message for processing
			srv.feed <- f
		case err := <-errCh:
			srv.feed <- &feeder.Feed{
				ProducerAddr: producer,
				TelemetryMsg: nil,
				Err:          err,
			}
			return err
		case <-srv.stopCh:
			return nil
		}
	}
}
