package tlscreds

import (
	"context"
	"crypto/tls"
	"fmt"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/testdata"
)

func TestFaillingProvider(t *testing.T) {
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
	dialOpts := []grpc.DialOption{
		grpc.WithCredentialsBundle(tlsBundle),
		grpc.WithAuthority("x.test.example.com"),
	}

	// Check that if the provider returns an errors, we fail the handshake.
	// It's not easy to trigger this condition, so we rely on closing the
	// provider.
	creds, ok := tlsBundle.TransportCredentials().(*reloadingCreds)
	if !ok {
		t.Fatalf("Expected reloadingCreds, got %T", tlsBundle.TransportCredentials())
	}

	// Force the provider to be initialized. The test is flaky otherwise,
	// since close may be a noop.
	_, _ = creds.provider.KeyMaterial(context.Background())

	creds.provider.Close()

	conn, err := grpc.Dial(s.Address, dialOpts...)
	if err != nil {
		t.Fatalf("Error dialing: %v", err)
	}
	client := testgrpc.NewTestServiceClient(conn)
	_, err = client.EmptyCall(context.Background(), &testpb.Empty{})
	if wantErr := "provider instance is closed"; err.Error() != wantErr {
		t.Errorf("Expected error to end with %v, got %v", wantErr, err)
	}
	conn.Close()
}
