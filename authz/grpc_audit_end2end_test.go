package authz

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/authz/audit"
	"google.golang.org/grpc/credentials/insecure"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

type testServer struct {
	testgrpc.UnimplementedTestServiceServer
}

func (s *testServer) UnaryCall(ctx context.Context, req *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
	return &testpb.SimpleResponse{}, nil
}

type statAuditLogger struct {
	Stat         map[bool]int
	stdoutLogger audit.Logger
}

func (s *statAuditLogger) Log(event *audit.Event) {
	s.stdoutLogger.Log(event)
	if event.Authorized {
		s.Stat[true]++
	} else {
		s.Stat[false]++
	}
}

type loggerBuilder struct {
	Stat map[bool]int
}

func (loggerBuilder) Name() string {
	return "stat_logger"
}
func (lb *loggerBuilder) Build(audit.LoggerConfig) audit.Logger {
	return &statAuditLogger{
		Stat:         lb.Stat,
		stdoutLogger: audit.GetLoggerBuilder("stdout_logger").Build(nil),
	}
}

func (*loggerBuilder) ParseLoggerConfig(config json.RawMessage) (audit.LoggerConfig, error) {
	return nil, nil
}

func TestAuditLogger(t *testing.T) {
	tests := map[string]struct {
		authzPolicy string
		wantAllows  int
		wantDenies  int
	}{
		"No audit": {
			authzPolicy: `{
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
				"audit_logging_options": {
					"audit_condition": "NONE",
					"audit_loggers": [
						{
							"name": "stat_logger",
							"config": {},
							"is_optional": false
						}
					]
				}
			}`,
			wantAllows: 0,
			wantDenies: 0,
		},
		"Allow All Deny Streaming - Audit All": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules":
				[
					{
						"name": "allow_all",
						"request": {
							"paths":
							[
								"*"
							]
						}
					}
				],
				"deny_rules":
				[
					{
						"name": "deny_all",
						"request": {
							"paths":
							[
								"/grpc.testing.TestService/StreamingInputCall"
							]
						}
					}
				],
				"audit_logging_options": {
					"audit_condition": "ON_DENY_AND_ALLOW",
					"audit_loggers": [
						{
							"name": "stat_logger",
							"config": {},
							"is_optional": false
						}
					]
				}
			}`,
			wantAllows: 2,
			wantDenies: 1,
		},
		"Allow Unary - Audit Allow": {
			authzPolicy: `{
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
				"audit_logging_options": {
					"audit_condition": "ON_ALLOW",
					"audit_loggers": [
						{
							"name": "stat_logger",
							"config": {},
							"is_optional": false
						}
					]
				}
			}`,
			wantAllows: 2,
			wantDenies: 0,
		},
		"Allow Typo - Audit Deny": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules":
				[
					{
						"name": "allow_UnaryCall",
						"request":
						{
							"paths":
							[
								"/grpc.testing.TestService/UnaryCall_Z"
							]
						}
					}
				],
				"audit_logging_options": {
					"audit_condition": "ON_DENY",
					"audit_loggers": [
						{
							"name": "stat_logger",
							"config": {},
							"is_optional": false
						}
					]
				}
			}`,
			wantAllows: 0,
			wantDenies: 3,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// Setup test statAuditLogger, gRPC test server with authzPolicy, unary and stream interceptors
			lb := &loggerBuilder{
				Stat: make(map[bool]int),
			}
			audit.RegisterLoggerBuilder(lb)
			i, _ := NewStatic(test.authzPolicy)
			s := grpc.NewServer(
				grpc.ChainUnaryInterceptor(i.UnaryInterceptor),
				grpc.ChainStreamInterceptor(i.StreamInterceptor))
			defer s.Stop()
			testgrpc.RegisterTestServiceServer(s, &testServer{})
			lis, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				t.Fatalf("error listening: %v", err)
			}
			go s.Serve(lis)
			clientConn, err := grpc.Dial(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatalf("grpc.Dial(%v) failed: %v", lis.Addr().String(), err)
			}
			defer clientConn.Close()
			client := testgrpc.NewTestServiceClient(clientConn)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			//Make 2 unary calls and 1 streaming call
			client.UnaryCall(ctx, &testpb.SimpleRequest{})
			client.UnaryCall(ctx, &testpb.SimpleRequest{})
			stream, err := client.StreamingInputCall(ctx)
			if err != nil {
				t.Fatalf("failed StreamingInputCall err: %v", err)
			}
			req := &testpb.StreamingInputCallRequest{
				Payload: &testpb.Payload{
					Body: []byte("hi"),
				},
			}
			if err := stream.Send(req); err != nil && err != io.EOF {
				t.Fatalf("failed stream.Send err: %v", err)
			}
			stream.CloseAndRecv()

			//Compare expected number of allows/denies with content of internal map of statAuditLogger
			if lb.Stat[true] != test.wantAllows {
				t.Errorf("Allow case failed, want %v got %v", test.wantAllows, lb.Stat[true])
			}
			if lb.Stat[false] != test.wantDenies {
				t.Errorf("Deny case failed, want %v got %v", test.wantDenies, lb.Stat[false])
			}
		})
	}
}
