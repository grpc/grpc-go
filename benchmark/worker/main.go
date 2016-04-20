package main

import (
	"io"
	"net"
	"runtime"
	"sync"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	testpb "google.golang.org/grpc/benchmark/grpc_testing"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
)

var (
	ports = []string{":10000"}
	// ports = []string{":10010"}
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
	bs *benchmarkServer
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

			bs, err := startBenchmarkServerWithSetup(t.Setup)
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
	return &testpb.Void{}, nil
}

func main() {
	var wg sync.WaitGroup
	wg.Add(len(ports))
	for i := 0; i < len(ports); i++ {
		lis, err := net.Listen("tcp", ports[i])
		if err != nil {
			grpclog.Fatalf("failed to listen: %v", err)
		}
		grpclog.Printf("worker %d listening at port %v", i, ports[i])

		s := grpc.NewServer()
		testpb.RegisterWorkerServiceServer(s, &workerServer{})
		go func() {
			defer wg.Done()
			s.Serve(lis)
		}()
	}
	wg.Wait()
}
