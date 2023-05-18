package authz

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/testdata"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

type testServer struct {
	testgrpc.UnimplementedTestServiceServer
}

func (s *testServer) UnaryCall(ctx context.Context, req *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
	return &testpb.SimpleResponse{}, nil
}

func TestStdoutLogger(t *testing.T) {
	authzPolicy := `{
				"name": "authz",
				"allow_rules":
				[
					{
						"name": "allow_UnaryCall",
						"request":
						{
							"paths":
							[
								"/grpc.testing.TestService/UnaryCall"
							]
						}
					}
				],
				"deny_rules": [
					{
						"name": "deny_policy_1",
						"source": {
							"principals":[
							"spiffe://foo.abc"
							]
						}
					}
				],
				"audit_logging_options": {
					"audit_condition": "ON_ALLOW",
					"audit_loggers": [
						{
							"name": "stdout_logger",
							"config": {},
							"is_optional": false
						}
					]
				}
			}`

	// Start a gRPC server with gRPC authz unary server interceptor.
	i, _ := NewStatic(authzPolicy)
	creds, err := credentials.NewServerTLSFromFile(testdata.Path("x509/server1_cert.pem"), testdata.Path("x509/server1_key.pem"))
	if err != nil {
		t.Fatalf("failed to generate credentials: %v", err)
	}
	s := grpc.NewServer(
		grpc.Creds(creds),
		grpc.ChainUnaryInterceptor(i.UnaryInterceptor))
	defer s.Stop()
	testgrpc.RegisterTestServiceServer(s, &testServer{})

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("error listening: %v", err)
	}
	go s.Serve(lis)

	// Establish a connection to the server.
	creds, err = credentials.NewClientTLSFromFile(testdata.Path("x509/server_ca_cert.pem"), "x.test.example.com")
	if err != nil {
		t.Fatalf("failed to load credentials: %v", err)
	}
	clientConn, err := grpc.Dial(lis.Addr().String(), grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatalf("grpc.Dial(%v) failed: %v", lis.Addr().String(), err)
	}
	defer clientConn.Close()
	client := testgrpc.NewTestServiceClient(clientConn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Verifying authorization decision.
	if _, err := client.UnaryCall(ctx, &testpb.SimpleRequest{}); err != nil {
		t.Fatalf("client.UnaryCall(_, _) = %v; want nil", err)
	}

}
