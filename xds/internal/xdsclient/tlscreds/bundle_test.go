package tlscreds

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/testdata"
)

type failingProvider struct{}

func (f failingProvider) KeyMaterial(ctx context.Context) (*certprovider.KeyMaterial, error) {
	return nil, errors.New("test error")
}

func (f failingProvider) Close() {}

func TestFailingProvider(t *testing.T) {
	s := stubserver.StartTestService(t, nil, grpc.Creds(e2e.CreateServerTLSCredentials(t, tls.RequireAndVerifyClientCert)))
	defer s.Stop()

	cfg := fmt.Sprintf(`{
               "ca_certificate_file": "%s",
               "certificate_file": "%s",
               "private_key_file": "%s"
       }`,
		testdata.Path("x509/server_ca_cert.pem"),
		testdata.Path("x509/client1_cert.pem"),
		testdata.Path("x509/client1_key.pem"))
	tlsBundle, err := NewBundle([]byte(cfg))
	if err != nil {
		t.Fatalf("Failed to create TLS bundle: %v", err)
	}

	dialOpts := []grpc.DialOption{
		grpc.WithCredentialsBundle(tlsBundle),
		grpc.WithAuthority("x.test.example.com"),
	}

	// Force a provider that returns an error, and make sure the client fails
	// the handshake.
	creds, ok := tlsBundle.TransportCredentials().(*reloadingCreds)
	if !ok {
		t.Fatalf("Got %T, expected reloadingCreds", tlsBundle.TransportCredentials())
	}
	creds.provider = &failingProvider{}

	conn, err := grpc.Dial(s.Address, dialOpts...)
	if err != nil {
		t.Fatalf("Error dialing: %v", err)
	}
	defer conn.Close()

	client := testgrpc.NewTestServiceClient(conn)
	_, err = client.EmptyCall(context.Background(), &testpb.Empty{})
	if wantErr := "test error"; err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Errorf("EmptyCall() got err: %s, want err to contain: %s", err, wantErr)
	}
}
