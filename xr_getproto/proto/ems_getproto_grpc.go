package emsgetproto

import (
	"context"

	grpc "google.golang.org/grpc"
)

const _ = grpc.SupportPackageIsVersion7

const GRPCConfigOper_GetProtoFile_FullMethodName = "/IOSXRExtensibleManagabilityService.gRPCConfigOper/GetProtoFile"

type GRPCConfigOperClient interface {
	GetProtoFile(ctx context.Context, in *GetProtoFileArgs, opts ...grpc.CallOption) (GRPCConfigOper_GetProtoFileClient, error)
}

type gRPCConfigOperClient struct {
	cc grpc.ClientConnInterface
}

func NewGRPCConfigOperClient(cc grpc.ClientConnInterface) GRPCConfigOperClient {
	return &gRPCConfigOperClient{cc}
}

func (c *gRPCConfigOperClient) GetProtoFile(ctx context.Context, in *GetProtoFileArgs, opts ...grpc.CallOption) (GRPCConfigOper_GetProtoFileClient, error) {
	stream, err := c.cc.NewStream(ctx, &GRPCConfigOper_ServiceDesc.Streams[0], GRPCConfigOper_GetProtoFile_FullMethodName, opts...)
	if err != nil {
		return nil, err
	}
	x := &gRPCConfigOperGetProtoFileClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type GRPCConfigOper_GetProtoFileClient interface {
	Recv() (*GetProtoFileReply, error)
	grpc.ClientStream
}

type gRPCConfigOperGetProtoFileClient struct {
	grpc.ClientStream
}

func (x *gRPCConfigOperGetProtoFileClient) Recv() (*GetProtoFileReply, error) {
	m := new(GetProtoFileReply)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

var GRPCConfigOper_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "IOSXRExtensibleManagabilityService.gRPCConfigOper",
	HandlerType: (*interface{})(nil),
	Methods:     []grpc.MethodDesc{},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "GetProtoFile",
			ServerStreams: true,
		},
	},
	Metadata: "ems_getproto.proto",
}
