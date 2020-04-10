package security

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcutil"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	authpb "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	sdsgrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/golang/protobuf/ptypes"
)

type sdsStream sdsgrpc.SecretDiscoveryService_StreamSecretsClient

const sdsURL = "type.googleapis.com/envoy.api.v2.auth.Secret"

// sdsClient uses the SecretDiscoveryService on the SDS server to stream secrets
// associated with specific resource names. It provides a watch API for its
// users and reports the secrets streamed from the server asynchronously, via
// callbacks. The only expected user of this client will be the CredsProvider
// which will be local to this package. Hence no fields or methods are exported.
//
// It uses a separate stream for each watched resource, for a couple of reasons.
// The SDS server implementation in the NodeAgent in Istio supports only one
// resource per stream. Also in most, if not all deployments, we do not expect
// more than a handful of streams per workload.
type sdsClient struct {
	ctx    context.Context
	cancel context.CancelFunc

	// Channel to the SDS server.
	cc *grpc.ClientConn
	// Backoff strategy to use when recreating streams.
	bs func(int) time.Duration
	// Identifies this client to the SDS server. This nodeProto is read from the
	// same bootstrap file that the xdsClient uses.
	nodeProto *corepb.Node
	logger    *grpclog.PrefixLogger
}

// sdsClientOpts wraps the parameters required for creating an sdsClient.
type sdsClientOpts struct {
	// serverURI is the address of the SDS server. The only supported URI scheme
	// is a Unix Domain Socket path.
	serverURI string
	// nodeProto identifies the client in all SDS requests. The user of the
	// sdsClient is expected to read the nodeProto from the bootstrap file.
	nodeProto *corepb.Node
	// Backoff strategy to use when recreating broken streams.
	backoff func(int) time.Duration
	logger  *grpclog.PrefixLogger
}

// newSDSClient creates a new sdsClient with the provided opts. The caller is
// expected to provide transport credentials and call credentials, if any, as
// grpc.DialOption in dopts.
func newSDSClient(opts *sdsClientOpts, dopts ...grpc.DialOption) (*sdsClient, error) {
	if parsedTarget := grpcutil.ParseTarget(opts.serverURI); parsedTarget.Scheme != "unix" {
		return nil, fmt.Errorf("sds: newSDSClient(%v): unsupported scheme: %v", opts, parsedTarget.Scheme)
	}

	// NOTE: We currently only support communicating with the server over
	// UDS, which is considered as secure as a TLS channel. But gRPC-Go does
	// not support sending call creds over channels which do not contain
	// transport creds. We will need to support a new transport credential
	// type for local channels like UDS and TCP loopback (which are
	// considered secure), to make this possible.
	cc, err := grpc.Dial(opts.serverURI, dopts...)
	if err != nil {
		// A non-blocking dial failure indicates something serious.
		return nil, fmt.Errorf("sds: newSDSClient(%s) = %v", opts.serverURI, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	bs := opts.backoff
	if bs == nil {
		bs = backoff.DefaultExponential.Backoff
	}
	return &sdsClient{
		ctx:       ctx,
		cancel:    cancel,
		cc:        cc,
		bs:        bs,
		nodeProto: opts.nodeProto,
		logger:    opts.logger,
	}, nil
}

// Callback provided by the user to receive secrets streamed by the server.
type watchCallback func(update *authpb.Secret, err error)

// watch provides a way for users to register a watch on secrets associated with
// the provided resource name and receive results streamed from the SDS server through
// the provided callback. Each watch triggers the creation of a new gRPC stream
// to the SDS server.
// The returned cancel function must be invoked by users (once they are no
// longer interested in this watch) to cleanup resources allocated as part of
// this watch.
func (c *sdsClient) watch(name string, cb watchCallback) func() {
	c.logger.Infof("watch registered for name: %s", name)
	ctx, cancel := context.WithCancel(context.Background())
	go c.runStream(ctx, name, cb)
	return func() { cancel() }
}

// runStream is a long running goroutine which handles all communication with
// the SDS server, over a gRPC stream, as part of a watch registered for a
// resource name.
func (c *sdsClient) runStream(ctx context.Context, name string, cb watchCallback) {
	retries := 0
	for {
		// Exit if top-level context or stream specific context is done.
		select {
		case <-c.ctx.Done():
			return
		case <-ctx.Done():
			return
		default:
		}

		if retries != 0 {
			t := time.NewTimer(c.bs(retries))
			select {
			case <-t.C:
			case <-c.ctx.Done():
				t.Stop()
				return
			case <-ctx.Done():
				t.Stop()
				return
			}
		}

		retries++
		cli := sdsgrpc.NewSecretDiscoveryServiceClient(c.cc)
		stream, err := cli.StreamSecrets(ctx, grpc.WaitForReady(true))
		if err != nil {
			c.logger.Warningf("SDS stream creation failed: %v", err)
			continue
		}
		c.logger.Infof("SDS stream created")

		// The first request on a stream is sent with empty version and nonce.
		if !c.sendRequest(stream, name, "", "") {
			continue
		}

		// We use one stream per resource. So, we will no longer need to send
		// requests with different resource names on this stream. All we need to
		// do here is to receive responses from the server, ACK/NACK them, and
		// invoke the registered callback appropriately. If Send/Recv fails (the
		// stream is aborted), we break out of this for-loop and recreate the
		// stream and do the same thing over and over.
		var version, nonce string
		for {
			resp, err := stream.Recv()
			if err != nil {
				c.logger.Warningf("stream.Recv() : %v", err)
				break
			}
			c.logger.Debugf("SDS response received: %v", resp)
			if resp.GetTypeUrl() != sdsURL {
				c.logger.Warningf("Resource type %v unknown in response from server", resp.GetTypeUrl())
				continue
			}

			// Once we receive a good response on a stream, we do not have to
			// backoff for the next retry.
			retries = 0

			// Update the nonce1 irrespective of whether we will ACK/NACK.
			nonce = resp.GetNonce()
			if err := handleSDSResponse(resp, name, cb); err != nil {
				c.logger.Warningf("Sending NACK for response version: %s, nonce: %s, reason: %v", resp.GetVersionInfo(), resp.GetNonce(), err)
			} else {
				// Update the version only when ACK'ing.
				c.logger.Infof("Sending ACK for response version: %s, nonce: %s", resp.GetVersionInfo(), resp.GetNonce())
				version = resp.GetVersionInfo()
			}
			if !c.sendRequest(stream, name, version, nonce) {
				continue
			}
		}
	}
}

// Makes a DiscoveryRequest message to be sent on the wire with the provided
// name, version and nonce, and sends it on the provided stream. Returns a
// boolean indicating whether the Send succeeded.
func (c *sdsClient) sendRequest(stream sdsStream, name, version, nonce string) bool {
	req := &xdspb.DiscoveryRequest{
		Node:          c.nodeProto,
		ResourceNames: []string{name},
		TypeUrl:       sdsURL,
		VersionInfo:   version,
		ResponseNonce: nonce,
	}
	if err := stream.Send(req); err != nil {
		c.logger.Warningf("stream.Send(%v) = %v", req, err)
		return false
	}
	c.logger.Debugf("SDS request sent: %v", req)
	return true
}

// handleSDSResponse processes a single response from the SDS server. It expects
// certain conditions to be met on the received response:
// 1. should contain exactly one resource
// 2. received resource should be of type `envoy.api.v2.auth.Secret`
// 3. resource name should match the one we are watching for
//
// If all the above conditions are met, the registered callback is invoked with
// the received Secret, and a nil error is returned. A non-nil error indicates
// that the response needs to be NACK'ed.
func handleSDSResponse(resp *xdspb.DiscoveryResponse, name string, cb watchCallback) error {
	if l := len(resp.GetResources()); l != 1 {
		return fmt.Errorf("sds: expected response with exactly one resouce, found %d", l)
	}

	var resource ptypes.DynamicAny
	r := resp.GetResources()[0]
	if err := ptypes.UnmarshalAny(r, &resource); err != nil {
		return fmt.Errorf("sds: response unmarshal failed: %v", err)
	}
	secret, ok := resource.Message.(*authpb.Secret)
	if !ok {
		return fmt.Errorf("sds: resource type: %T", resource.Message)
	}
	if secret.GetName() != name {
		return fmt.Errorf("sds: expected resource name %s, found %s", name, secret.GetName())
	}

	cb(secret, nil)
	return nil
}

// close cancels the top level context which causes the goroutines managing the
// streams to exit. It also closes the clientConn to the SDS server.
func (c *sdsClient) close() {
	c.cc.Close()
	c.cancel()
}

// TODO: Testing
// 2. Connect from here witl TLS creds
// stream breaks
// no reponse
