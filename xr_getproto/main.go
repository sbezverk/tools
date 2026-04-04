package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"

	emsgetproto "github.com/sbezverk/tools/xr_getproto/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const defaultServerName = "ems.cisco.com"

type passCredential struct {
	username string
	password string
}

func (p passCredential) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	return map[string]string{
		"username": p.username,
		"password": p.password,
	}, nil
}

func (passCredential) RequireTransportSecurity() bool {
	return false
}

func main() {
	serverAddr := flag.String("server_addr", "127.0.0.1:57400", "server address in host:port form")
	yangPath := flag.String("yang_path", "", "YANG path to request from XR")
	outFile := flag.String("out", "", "output file for the returned proto")
	reqID := flag.Int64("req_id", 1, "request ID")
	useTLS := flag.Bool("tls", false, "use TLS for the gRPC connection")
	caFile := flag.String("ca_file", "", "CA file for TLS")
	serverName := flag.String("server_name", defaultServerName, "TLS server name override")
	username := flag.String("username", "root", "username metadata for the RPC")
	password := flag.String("password", "lab123", "password metadata for the RPC")
	timeout := flag.Duration("timeout", 0, "RPC timeout, 0 disables the deadline")
	flag.Parse()

	if *yangPath == "" {
		log.Fatal("-yang_path is required")
	}

	var dialOpts []grpc.DialOption
	if *useTLS {
		if *caFile == "" {
			log.Fatal("-ca_file is required when -tls is set")
		}
		creds, err := credentials.NewClientTLSFromFile(*caFile, *serverName)
		if err != nil {
			log.Fatalf("failed to load TLS credentials: %v", err)
		}
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(creds))
	} else {
		dialOpts = append(dialOpts, grpc.WithInsecure())
	}
	dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(passCredential{
		username: *username,
		password: *password,
	}))
	dialOpts = append(dialOpts, grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(math.MaxInt32)))

	conn, err := grpc.Dial(*serverAddr, dialOpts...)
	if err != nil {
		log.Fatalf("failed to dial %s: %v", *serverAddr, err)
	}
	defer conn.Close()

	client := emsgetproto.NewGRPCConfigOperClient(conn)
	ctx := context.Background()
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}

	stream, err := client.GetProtoFile(ctx, &emsgetproto.GetProtoFileArgs{
		ReqId:    *reqID,
		YangPath: *yangPath,
	})
	if err != nil {
		log.Fatalf("GetProtoFile RPC failed: %v", err)
	}

	var out *os.File
	if *outFile != "" {
		if err := os.MkdirAll(filepath.Dir(*outFile), 0o755); err != nil {
			log.Fatalf("failed to create output directory: %v", err)
		}
		out, err = os.Create(*outFile)
		if err != nil {
			log.Fatalf("failed to create output file: %v", err)
		}
		defer out.Close()
	}

	total := 0
	for {
		reply, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("stream receive failed: %v", err)
		}
		if reply.GetErrors() != "" {
			log.Fatalf("device returned error: %s", reply.GetErrors())
		}
		chunk := reply.GetProtoContent()
		if chunk == "" {
			continue
		}
		total += len(chunk)
		if out != nil {
			if _, err := out.WriteString(chunk); err != nil {
				log.Fatalf("failed to write output: %v", err)
			}
			continue
		}
		fmt.Print(chunk)
	}

	if out != nil {
		fmt.Fprintf(os.Stderr, "wrote %d bytes to %s\n", total, *outFile)
	} else {
		fmt.Fprintf(os.Stderr, "received %d bytes\n", total)
	}
}
