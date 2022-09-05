package grpc_feeder

import (
	"net"

	feeder "github.com/sbezverk/tools/telemetry_feeder"
	"github.com/sbezverk/tools/telemetry_feeder/proto/mdtdialout"
	"google.golang.org/grpc"
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
	srv.stopCh <- struct{}{}
	<-srv.stopCh
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
		gSrv:   grpc.NewServer(grpc.MaxRecvMsgSize(MaxRcvMsgSize)),
	}
	mdtdialout.RegisterGRPCMdtDialoutServer(srv.gSrv, srv)

	go srv.gSrv.Serve(conn)

	return srv, nil
}

func (srv *grpcSrv) MdtDialout(session mdtdialout.GRPCMdtDialout_MdtDialoutServer) error {
	return srv.worker(session)
}

func (srv *grpcSrv) worker(session mdtdialout.GRPCMdtDialout_MdtDialoutServer) error {
	infoCh := make(chan *mdtdialout.MdtDialoutArgs)
	errCh := make(chan error)
	defer func() {
		close(infoCh)
		close(errCh)
	}()
	go func(iCh chan *mdtdialout.MdtDialoutArgs, eCh chan error) {
		for {
			info, err := session.Recv()
			if err != nil {
				eCh <- err
				return
			}
			// Got telemetry info, sending it to the parent for further processing
			if info != nil {
				iCh <- info
			}
		}
	}(infoCh, errCh)
	for {
		select {
		case msg := <-infoCh:
			f := &feeder.Feed{}
			f.TelemetryMsg = make([]byte, len(msg.Data))
			copy(f.TelemetryMsg, msg.Data)
			f.Err = nil
			// Sending recieved Telemetry message for processing
			srv.feed <- f
		case err := <-errCh:
			srv.feed <- &feeder.Feed{
				TelemetryMsg: nil,
				Err:          err,
			}
			return err
		case <-srv.stopCh:
			close(srv.stopCh)
			return nil
		}
	}
}
