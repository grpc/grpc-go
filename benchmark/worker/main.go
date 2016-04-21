package main

import (
	"flag"
	"io"
	"net"
	"runtime"
	"strconv"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	testpb "google.golang.org/grpc/benchmark/grpc_testing"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
)

var (
	driverPort = flag.Int("driver_port", 10000, "port for communication with driver")
	serverPort = flag.Int("server_port", 0, "port for operation as a server")
)

type byteBufCodec struct {
}

func (byteBufCodec) Marshal(v interface{}) ([]byte, error) {
	return v.([]byte), nil
}

func (byteBufCodec) Unmarshal(data []byte, v interface{}) error {
	v = data
	return nil
}

func (byteBufCodec) String() string {
	return "byteBufCodec"
}

type workerServer struct {
	bs         *benchmarkServer
	stop       chan<- bool
	serverPort int
}

func (s *workerServer) RunServer(stream testpb.WorkerService_RunServerServer) error {
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			grpclog.Printf("closing benchmark server")
			if s.bs != nil {
				s.bs.close()
				s.bs = nil
			}
			return nil
		}
		if err != nil {
			return err
		}

		switch t := in.Argtype.(type) {
		case *testpb.ServerArgs_Setup:
			grpclog.Printf("server setup received:")

			bs, err := startBenchmarkServerWithSetup(t.Setup, s.serverPort)
			if err != nil {
				return err
			}
			s.bs = bs
		case *testpb.ServerArgs_Mark:
			grpclog.Printf("server mark received:")
			grpclog.Printf(" - %v", t)
			if s.bs == nil {
				return grpc.Errorf(codes.InvalidArgument, "server does not exist when mark received")
			}
			if t.Mark.Reset_ {
				s.bs.reset()
			}
		}

		out := &testpb.ServerStatus{
			Stats: s.bs.getStats(),
			Port:  int32(s.bs.port),
			Cores: 1,
		}
		if err := stream.Send(out); err != nil {
			return err
		}
	}

	return nil
}

func (s *workerServer) RunClient(stream testpb.WorkerService_RunClientServer) error {
	return nil
}

func (s *workerServer) CoreCount(ctx context.Context, in *testpb.CoreRequest) (*testpb.CoreResponse, error) {
	grpclog.Printf("core count: %v", runtime.NumCPU())
	return &testpb.CoreResponse{int32(runtime.NumCPU())}, nil
}

func (s *workerServer) QuitWorker(ctx context.Context, in *testpb.Void) (*testpb.Void, error) {
	grpclog.Printf("quiting worker")
	if s.bs != nil {
		s.bs.close()
	}
	s.stop <- true
	return &testpb.Void{}, nil
}

func main() {
	flag.Parse()
	lis, err := net.Listen("tcp", ":"+strconv.Itoa(*driverPort))
	if err != nil {
		grpclog.Fatalf("failed to listen: %v", err)
	}
	grpclog.Printf("worker listening at port %v", *driverPort)

	s := grpc.NewServer()
	stop := make(chan bool)
	testpb.RegisterWorkerServiceServer(s, &workerServer{
		stop:       stop,
		serverPort: *serverPort,
	})
	go s.Serve(lis)
	<-stop
	s.Stop()
}
