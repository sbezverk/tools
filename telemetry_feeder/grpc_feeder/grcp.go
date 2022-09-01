package grpc_feeder

import (
	"fmt"
	"net"

	"github.com/golang/glog"
	feeder "github.com/sbezverk/tools/telemetry_feeder"
	"github.com/sbezverk/tools/telemetry_feeder/proto/mdtdialout"
	"github.com/sbezverk/tools/telemetry_feeder/proto/telemetry"
	"google.golang.org/grpc"
	grpcpeer "google.golang.org/grpc/peer"
	"google.golang.org/protobuf/proto"
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

func (g *grpcSrv) GetFeed() chan *feeder.Feed {
	return g.feed
}

func (g *grpcSrv) Stop() {
	g.gSrv.Stop()
	g.conn.Close()
	close(g.stopCh)
}

func New(addr string) (feeder.Feeder, error) {

	// TODO (sbezverk) Add address validation

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
	p, ok := grpcpeer.FromContext(session.Context())
	if ok {
		glog.V(5).Infof("Incomming MdtDialout from: %s", p.Addr)
	} else {
		glog.V(5).Infof("Incomming MdtDialout...")
	}

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
			m := &telemetry.Telemetry{}
			err := proto.Unmarshal(msg.Data, m)
			if err == nil {
				f.TelemetryMsg = m
			} else {
				err = fmt.Errorf("%+v %w", feeder.ErrUnmarshalTelemetryMsg, err)
			}
			f.Err = err
			// Sending recieved Telemetry message for processing
			srv.feed <- f
		case err := <-errCh:
			srv.feed <- &feeder.Feed{
				TelemetryMsg: nil,
				Err:          err,
			}
			return err
		case <-session.Context().Done():
			err := session.Context().Err()
			srv.feed <- &feeder.Feed{
				Err: err,
			}
			return err
		case <-srv.stopCh:
			return nil
		}
	}
}
