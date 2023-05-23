package authz

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/authz/audit"
	"google.golang.org/grpc/credentials"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/testdata"

	_ "google.golang.org/grpc/authz/audit/stdout"
)

type testServer struct {
	testgrpc.UnimplementedTestServiceServer
}

func (s *testServer) UnaryCall(ctx context.Context, req *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
	return &testpb.SimpleResponse{}, nil
}

type statAuditLogger struct {
	AuthzDescisionStat map[bool]int      //Map to hold the counts of authorization decisions
	EventContent       map[string]string //Map to hold event fields in key:value fashion
	SpiffeIds          []string          //Slice to hold collected SPIFFE IDs
}

func (s *statAuditLogger) Log(event *audit.Event) {
	if event.Authorized {
		s.AuthzDescisionStat[true]++
	} else {
		s.AuthzDescisionStat[false]++
	}
	s.SpiffeIds = append(s.SpiffeIds, event.Principal)
	s.EventContent["rpc_method"] = event.FullMethodName
	s.EventContent["principal"] = event.Principal
	s.EventContent["policy_name"] = event.PolicyName
	s.EventContent["matched_rule"] = event.MatchedRule
	s.EventContent["authorized"] = strconv.FormatBool(event.Authorized)
}

type loggerBuilder struct {
	AuthzDescisionStat map[bool]int
	EventContent       map[string]string
	SpiffeIds          []string
}

func (loggerBuilder) Name() string {
	return "stat_logger"
}
func (lb *loggerBuilder) Build(audit.LoggerConfig) audit.Logger {
	return &statAuditLogger{
		AuthzDescisionStat: lb.AuthzDescisionStat,
		EventContent:       lb.EventContent,
	}
}

func (*loggerBuilder) ParseLoggerConfig(config json.RawMessage) (audit.LoggerConfig, error) {
	return nil, nil
}

const spiffeId = "spiffe://foo.bar.com/client/workload/1"

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
						},
						{
							"name": "stdout_logger",
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
				AuthzDescisionStat: make(map[bool]int),
				EventContent:       make(map[string]string),
			}
			audit.RegisterLoggerBuilder(lb)
			i, _ := NewStatic(test.authzPolicy)
			cert, err := tls.LoadX509KeyPair(testdata.Path("x509/server1_cert.pem"), testdata.Path("x509/server1_key.pem"))
			if err != nil {
				t.Fatalf("tls.LoadX509KeyPair(x509/server1_cert.pem, x509/server1_key.pem) failed: %v", err)
			}
			ca, err := os.ReadFile(testdata.Path("x509/client_ca_cert.pem"))
			if err != nil {
				t.Fatalf("os.ReadFile(x509/client_ca_cert.pem) failed: %v", err)
			}
			certPool := x509.NewCertPool()
			if !certPool.AppendCertsFromPEM(ca) {
				t.Fatal("failed to append certificates")
			}
			creds := credentials.NewTLS(&tls.Config{
				ClientAuth:   tls.RequireAndVerifyClientCert,
				Certificates: []tls.Certificate{cert},
				ClientCAs:    certPool,
			})
			s := grpc.NewServer(
				grpc.Creds(creds),
				grpc.ChainUnaryInterceptor(i.UnaryInterceptor),
				grpc.ChainStreamInterceptor(i.StreamInterceptor))
			defer s.Stop()
			testgrpc.RegisterTestServiceServer(s, &testServer{})
			lis, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				t.Fatalf("error listening: %v", err)
			}
			go s.Serve(lis)

			// Setup gRPC test client with certificates containing a SPIFFE Id
			cert, err = tls.LoadX509KeyPair(testdata.Path("x509/client_with_spiffe_cert.pem"), testdata.Path("x509/client_with_spiffe_key.pem"))
			if err != nil {
				t.Fatalf("tls.LoadX509KeyPair(x509/client1_cert.pem, x509/client1_key.pem) failed: %v", err)
			}
			ca, err = os.ReadFile(testdata.Path("x509/server_ca_cert.pem"))
			if err != nil {
				t.Fatalf("os.ReadFile(x509/server_ca_cert.pem) failed: %v", err)
			}
			roots := x509.NewCertPool()
			if !roots.AppendCertsFromPEM(ca) {
				t.Fatal("failed to append certificates")
			}
			creds = credentials.NewTLS(&tls.Config{
				Certificates: []tls.Certificate{cert},
				RootCAs:      roots,
				ServerName:   "x.test.example.com",
			})
			clientConn, err := grpc.Dial(lis.Addr().String(), grpc.WithTransportCredentials(creds))
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
			if lb.AuthzDescisionStat[true] != test.wantAllows {
				t.Errorf("Allow case failed, want %v got %v", test.wantAllows, lb.AuthzDescisionStat[true])
			}
			if lb.AuthzDescisionStat[false] != test.wantDenies {
				t.Errorf("Deny case failed, want %v got %v", test.wantDenies, lb.AuthzDescisionStat[false])
			}
			//Compare recorded SPIFFE Ids with the value from cert
			for _, id := range lb.SpiffeIds {
				if id != spiffeId {
					t.Errorf("Unexpected SPIFFE Id, want %v got %v", spiffeId, id)
				}
			}
			//Special case - compare event fields with expected values from authz policy
			if name == `Allow All Deny Streaming - Audit All` {
				if diff := cmp.Diff(lb.EventContent, generateEventAsMap()); diff != "" {
					t.Fatalf("Unexpected message\ndiff (-got +want):\n%s", diff)
				}
			}
		})
	}
}

// generateEvent produces an map contaning audit.Event fields.
// It's used to compare captured audit.Event with the matched rule during
// `Allow All Deny Streaming - Audit All` scenario (authz_deny_all rule)
func generateEventAsMap() map[string]string {
	return map[string]string{
		"rpc_method":   "/grpc.testing.TestService/StreamingInputCall",
		"principal":    "spiffe://foo.bar.com/client/workload/1",
		"policy_name":  "authz",
		"matched_rule": "authz_deny_all",
		"authorized":   "false",
	}
}
