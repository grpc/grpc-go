package security

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	authpb "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/golang/protobuf/proto"
	anypb "github.com/golang/protobuf/ptypes/any"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeserver"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	secretName = "secret"
	version1   = "version1"
	nonce1     = "nonce1"
	version2   = "version2"
	nonce2     = "nonce2"
)

var (
	goodSecret = &authpb.Secret{
		Name: secretName,
		Type: &authpb.Secret_TlsCertificate{
			TlsCertificate: &authpb.TlsCertificate{
				CertificateChain: &corepb.DataSource{},
			},
		},
	}
	marshaledGoodSecret, _ = proto.Marshal(goodSecret)
	sdsGoodResponse        = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{TypeUrl: sdsURL, Value: marshaledGoodSecret},
		},
		TypeUrl:     sdsURL,
		VersionInfo: version1,
		Nonce:       nonce1,
	}
	secretConfig             = &authpb.SdsSecretConfig{}
	marshaledSecretConfig, _ = proto.Marshal(secretConfig)
	sdsBadTypeResponse       = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: "type.googleapis.com/envoy.api.v2.auth.SdsSecretConfig",
				Value:   marshaledSecretConfig,
			},
		},
		TypeUrl:     sdsURL,
		VersionInfo: version2,
		Nonce:       nonce2,
	}
	wrongNameSecret = &authpb.Secret{
		Name: secretName + "wrong",
		Type: &authpb.Secret_TlsCertificate{
			TlsCertificate: &authpb.TlsCertificate{
				CertificateChain: &corepb.DataSource{},
			},
		},
	}
	marshaledWrongNameSecret, _ = proto.Marshal(wrongNameSecret)
	wantRequest                 = &xdspb.DiscoveryRequest{
		ResourceNames: []string{secretName},
		TypeUrl:       sdsURL,
		Node:          &corepb.Node{},
	}
)

// compareRequest reads requests from channel, compares it with want.
func compareRequest(ch *testutils.Channel, want *xdspb.DiscoveryRequest, version, nonce string) error {
	val, err := ch.Receive()
	if err != nil {
		return err
	}
	req := val.(*fakeserver.Request)
	if req.Err != nil {
		return fmt.Errorf("request type is %T, want %T", req, &fakeserver.Request{})
	}
	wantClone := proto.Clone(want).(*xdspb.DiscoveryRequest)
	wantClone.VersionInfo = version
	wantClone.ResponseNonce = nonce
	if diff := cmp.Diff(req.Req, wantClone, cmp.Comparer(proto.Equal)); diff != "" {
		return fmt.Errorf("request diff (-got +want): %s", diff)
	}
	return nil
}

// TestHandleSDSResponseSuccess tests the handleSDSResponse function with inputs
// which are expected to succeed.
func TestHandleSDSResponseSuccess(t *testing.T) {
	updateCh := testutils.NewChannel()
	cb := func(gotSecret *authpb.Secret, err error) {
		if diff := cmp.Diff(gotSecret, goodSecret, cmp.Comparer(proto.Equal)); diff != "" {
			updateCh.Send(fmt.Errorf("secret diff (-got +want): %s", diff))
		}
		updateCh.Send(nil)
	}

	if err := handleSDSResponse(sdsGoodResponse, secretName, cb); err != nil {
		t.Fatal(err)
	}
	if _, err := updateCh.Receive(); err != nil {
		t.Fatal(err)
	}
}

// TestHandleSDSResponseFailure tests the handleSDSResponse function with inputs
// which are expected to fail.
func TestHandleSDSResponseFailure(t *testing.T) {
	tests := []struct {
		desc          string
		resp          *xdspb.DiscoveryResponse
		secretName    string
		wantErrPrefix string
	}{
		{
			desc:          "no resources in response",
			resp:          &xdspb.DiscoveryResponse{},
			secretName:    secretName,
			wantErrPrefix: "sds: expected response with exactly one resouce",
		},
		{
			desc: "too many resources in response",
			resp: &xdspb.DiscoveryResponse{
				Resources: []*anypb.Any{
					{TypeUrl: sdsURL, Value: marshaledGoodSecret},
					{TypeUrl: sdsURL, Value: marshaledGoodSecret},
				},
				TypeUrl: sdsURL,
			},
			secretName:    secretName,
			wantErrPrefix: "sds: expected response with exactly one resouce",
		},
		{
			desc: "marshaling error",
			resp: &xdspb.DiscoveryResponse{
				Resources: []*anypb.Any{
					{TypeUrl: sdsURL, Value: []byte{1, 2, 3, 4}},
				},
				TypeUrl: sdsURL,
			},
			secretName:    secretName,
			wantErrPrefix: "sds: response unmarshal failed",
		},
		{
			desc:          "bad type",
			resp:          sdsBadTypeResponse,
			secretName:    secretName,
			wantErrPrefix: "sds: resource type",
		},
		{
			desc: "wrong resource name in response",
			resp: &xdspb.DiscoveryResponse{
				Resources: []*anypb.Any{
					{TypeUrl: sdsURL, Value: marshaledWrongNameSecret},
				},
				TypeUrl: sdsURL,
			},
			secretName:    secretName,
			wantErrPrefix: "sds: expected resource name",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			updateCh := testutils.NewChannel()
			cb := func(_ *authpb.Secret, _ error) {
				t.Fatalf("watch callback invoked for bad response")
			}
			if err := handleSDSResponse(test.resp, test.secretName, cb); !strings.HasPrefix(err.Error(), test.wantErrPrefix) {
				t.Errorf("handleSDSResponse = %v, wantErrPrefix: %s", err, test.wantErrPrefix)
			}
			if _, err := updateCh.Receive(); err != testutils.ErrRecvTimeout {
				t.Error(err)
			}
		})
	}
}

// TestSDSClientAckNack test the scenario where the sdsClient first receives one
// good update and then a bad update. It verifies that the client sends ACK/NACK
// appropriately.
func (s) TestSDSClientAckNack(t *testing.T) {
	fs, cleanup, err := fakeserver.StartServer()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	t.Logf("Started fake SDS server at %s...", fs.Path)

	opts := &sdsClientOpts{
		serverURI: fs.Path,
		nodeProto: &corepb.Node{},
	}
	sdsC, err := newSDSClient(opts, grpc.WithInsecure())
	if err != nil {
		t.Fatal(err)
	}
	defer sdsC.close()
	t.Log("Created SDS client...")

	cbErrCh := make(chan error, 1)
	cancel := sdsC.watch(secretName, func(gotSecret *authpb.Secret, err error) {
		t.Logf("sdsClient watch callback invoked with secret {%v}, error {%v}", gotSecret, err)
		if diff := cmp.Diff(gotSecret, goodSecret, cmp.Comparer(proto.Equal)); diff != "" {
			cbErrCh <- fmt.Errorf("watch callback gotSecret diff: %s", diff)
			return
		}
		cbErrCh <- nil
	})
	defer cancel()
	t.Logf("Registered a watch for resource: %s", secretName)

	// First request from the client has no version or nonce.
	if err := compareRequest(fs.SDSRequestChan, wantRequest, "", ""); err != nil {
		t.Fatal(err)
	}

	// Send a good response from the fake server.
	fs.SDSResponseChan <- &fakeserver.Response{Resp: sdsGoodResponse}

	// Wait for the ACK from the client, which has version and nonce from the
	// response sent above.
	if err := compareRequest(fs.SDSRequestChan, wantRequest, sdsGoodResponse.GetVersionInfo(), sdsGoodResponse.GetNonce()); err != nil {
		t.Fatal(err)
	}

	// Send a bad response from the fake server.
	fs.SDSResponseChan <- &fakeserver.Response{Resp: sdsBadTypeResponse}

	// Wait for the NACK from the client, which has version from the last good
	// response, but nonce from the bad response sent above.
	if err := compareRequest(fs.SDSRequestChan, wantRequest, sdsGoodResponse.GetVersionInfo(), sdsBadTypeResponse.GetNonce()); err != nil {
		t.Fatal(err)
	}
}

// TestSDSClientBackoffAfterRecvError tests the scenario where the sdsClient
// receives an RPC error. It verifies the client backoff before recreating the
// stream.
func (s) TestSDSClientBackoffAfterRecvError(t *testing.T) {
	fs, cleanup, err := fakeserver.StartServer()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	t.Logf("Started fake SDS server at %s...", fs.Path)

	// Override the sdsClient backoff function with this, so that we can verify
	// that a backoff actually was triggered.
	boCh := testutils.NewChannel()
	clientBackoff := func(v int) time.Duration {
		boCh.Send(v)
		return 0
	}
	opts := &sdsClientOpts{
		serverURI: fs.Path,
		nodeProto: &corepb.Node{},
		backoff:   clientBackoff,
	}

	sdsC, err := newSDSClient(opts, grpc.WithInsecure())
	if err != nil {
		t.Fatal(err)
	}
	defer sdsC.close()
	t.Log("Created SDS client...")

	cbErrCh := make(chan error, 1)
	cancel := sdsC.watch(secretName, func(gotSecret *authpb.Secret, err error) {
		t.Logf("sdsClient watch callback invoked with secret {%v}, error {%v}", gotSecret, err)
		if diff := cmp.Diff(gotSecret, goodSecret, cmp.Comparer(proto.Equal)); diff != "" {
			cbErrCh <- fmt.Errorf("watch callback gotSecret diff: %s", diff)
			return
		}
		cbErrCh <- nil
	})
	defer cancel()
	t.Logf("Registered a watch for resource: %s", secretName)

	// First request from the client has no version or nonce.
	if err := compareRequest(fs.SDSRequestChan, wantRequest, "", ""); err != nil {
		t.Fatal(err)
	}

	// Send an RPC error the fake server which should trigger recreation of the
	// stream.
	fs.SDSResponseChan <- &fakeserver.Response{Err: errors.New("RPC error")}
	t.Log("FakeServer sending RPC error...")

	// Since this is the first retry, we expect the backoff function to called
	// with a value of 1.
	v, err := boCh.Receive()
	if err != nil {
		t.Fatal("Timeout waiting for client to backoff")
	}
	if val, _ := v.(int); val != 1 {
		t.Fatalf("Backoff retry value = %d, want 1", val)
	}
}
