/*
 *
 * Copyright 2019 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package v2

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/golang/protobuf/proto"
	anypb "github.com/golang/protobuf/ptypes/any"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeserver"
	"google.golang.org/grpc/xds/internal/version"
	"google.golang.org/grpc/xds/internal/xdsclient"
)

const (
	defaultTestTimeout      = 5 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond
)

func startXDSV2Client(t *testing.T, cc *grpc.ClientConn) (v2c *client, cbLDS, cbRDS, cbCDS, cbEDS *testutils.Channel, cleanup func()) {
	cbLDS = testutils.NewChannel()
	cbRDS = testutils.NewChannel()
	cbCDS = testutils.NewChannel()
	cbEDS = testutils.NewChannel()
	v2c, err := newV2Client(&testUpdateReceiver{
		f: func(rType xdsclient.ResourceType, d map[string]interface{}, md xdsclient.UpdateMetadata) {
			t.Logf("Received %v callback with {%+v}", rType, d)
			switch rType {
			case xdsclient.ListenerResource:
				if _, ok := d[goodLDSTarget1]; ok {
					cbLDS.Send(struct{}{})
				}
			case xdsclient.RouteConfigResource:
				if _, ok := d[goodRouteName1]; ok {
					cbRDS.Send(struct{}{})
				}
			case xdsclient.ClusterResource:
				if _, ok := d[goodClusterName1]; ok {
					cbCDS.Send(struct{}{})
				}
			case xdsclient.EndpointsResource:
				if _, ok := d[goodEDSName]; ok {
					cbEDS.Send(struct{}{})
				}
			}
		},
	}, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Started xds client...")
	return v2c, cbLDS, cbRDS, cbCDS, cbEDS, v2c.Close
}

// compareXDSRequest reads requests from channel, compare it with want.
func compareXDSRequest(ctx context.Context, ch *testutils.Channel, want *xdspb.DiscoveryRequest, ver, nonce string, wantErr bool) error {
	val, err := ch.Receive(ctx)
	if err != nil {
		return err
	}
	req := val.(*fakeserver.Request)
	if req.Err != nil {
		return fmt.Errorf("unexpected error from request: %v", req.Err)
	}

	xdsReq := req.Req.(*xdspb.DiscoveryRequest)
	if (xdsReq.ErrorDetail != nil) != wantErr {
		return fmt.Errorf("received request with error details: %v, wantErr: %v", xdsReq.ErrorDetail, wantErr)
	}
	// All NACK request.ErrorDetails have hardcoded status code InvalidArguments.
	if xdsReq.ErrorDetail != nil && xdsReq.ErrorDetail.Code != int32(codes.InvalidArgument) {
		return fmt.Errorf("received request with error details: %v, want status with code: %v", xdsReq.ErrorDetail, codes.InvalidArgument)
	}

	xdsReq.ErrorDetail = nil // Clear the error details field before comparing.
	wantClone := proto.Clone(want).(*xdspb.DiscoveryRequest)
	wantClone.VersionInfo = ver
	wantClone.ResponseNonce = nonce
	if !cmp.Equal(xdsReq, wantClone, cmp.Comparer(proto.Equal)) {
		return fmt.Errorf("received request different from want, diff: %s", cmp.Diff(req.Req, wantClone, cmp.Comparer(proto.Equal)))
	}
	return nil
}

func sendXDSRespWithVersion(ch chan<- *fakeserver.Response, respWithoutVersion *xdspb.DiscoveryResponse, ver int) (nonce string) {
	respToSend := proto.Clone(respWithoutVersion).(*xdspb.DiscoveryResponse)
	respToSend.VersionInfo = strconv.Itoa(ver)
	nonce = strconv.Itoa(int(time.Now().UnixNano()))
	respToSend.Nonce = nonce
	ch <- &fakeserver.Response{Resp: respToSend}
	return
}

// startXDS calls watch to send the first request. It then sends a good response
// and checks for ack.
func startXDS(ctx context.Context, t *testing.T, rType xdsclient.ResourceType, v2c *client, reqChan *testutils.Channel, req *xdspb.DiscoveryRequest, preVersion string, preNonce string) {
	nameToWatch := ""
	switch rType {
	case xdsclient.ListenerResource:
		nameToWatch = goodLDSTarget1
	case xdsclient.RouteConfigResource:
		nameToWatch = goodRouteName1
	case xdsclient.ClusterResource:
		nameToWatch = goodClusterName1
	case xdsclient.EndpointsResource:
		nameToWatch = goodEDSName
	}
	v2c.AddWatch(rType, nameToWatch)

	if err := compareXDSRequest(ctx, reqChan, req, preVersion, preNonce, false); err != nil {
		t.Fatalf("Failed to receive %v request: %v", rType, err)
	}
	t.Logf("FakeServer received %v request...", rType)
}

// sendGoodResp sends the good response, with the given version, and a random
// nonce.
//
// It also waits and checks that the ack request contains the given version, and
// the generated nonce.
func sendGoodResp(ctx context.Context, t *testing.T, rType xdsclient.ResourceType, fakeServer *fakeserver.Server, ver int, goodResp *xdspb.DiscoveryResponse, wantReq *xdspb.DiscoveryRequest, callbackCh *testutils.Channel) (string, error) {
	nonce := sendXDSRespWithVersion(fakeServer.XDSResponseChan, goodResp, ver)
	t.Logf("Good %v response pushed to fakeServer...", rType)

	if err := compareXDSRequest(ctx, fakeServer.XDSRequestChan, wantReq, strconv.Itoa(ver), nonce, false); err != nil {
		return "", fmt.Errorf("failed to receive %v request: %v", rType, err)
	}
	t.Logf("Good %v response acked", rType)

	if _, err := callbackCh.Receive(ctx); err != nil {
		return "", fmt.Errorf("timeout when expecting %v update", rType)
	}
	t.Logf("Good %v response callback executed", rType)
	return nonce, nil
}

// sendBadResp sends a bad response with the given version. This response will
// be nacked, so we expect a request with the previous version (version-1).
//
// But the nonce in request should be the new nonce.
func sendBadResp(ctx context.Context, t *testing.T, rType xdsclient.ResourceType, fakeServer *fakeserver.Server, ver int, wantReq *xdspb.DiscoveryRequest) error {
	var typeURL string
	switch rType {
	case xdsclient.ListenerResource:
		typeURL = version.V2ListenerURL
	case xdsclient.RouteConfigResource:
		typeURL = version.V2RouteConfigURL
	case xdsclient.ClusterResource:
		typeURL = version.V2ClusterURL
	case xdsclient.EndpointsResource:
		typeURL = version.V2EndpointsURL
	}
	nonce := sendXDSRespWithVersion(fakeServer.XDSResponseChan, &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{{}},
		TypeUrl:   typeURL,
	}, ver)
	t.Logf("Bad %v response pushed to fakeServer...", rType)
	if err := compareXDSRequest(ctx, fakeServer.XDSRequestChan, wantReq, strconv.Itoa(ver-1), nonce, true); err != nil {
		return fmt.Errorf("failed to receive %v request: %v", rType, err)
	}
	t.Logf("Bad %v response nacked", rType)
	return nil
}

// TestV2ClientAck verifies that valid responses are acked, and invalid ones
// are nacked.
//
// This test also verifies the version for different types are independent.
func (s) TestV2ClientAck(t *testing.T) {
	var (
		versionLDS = 1000
		versionRDS = 2000
		versionCDS = 3000
		versionEDS = 4000
	)

	fakeServer, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	v2c, cbLDS, cbRDS, cbCDS, cbEDS, v2cCleanup := startXDSV2Client(t, cc)
	defer v2cCleanup()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start the watch, send a good response, and check for ack.
	startXDS(ctx, t, xdsclient.ListenerResource, v2c, fakeServer.XDSRequestChan, goodLDSRequest, "", "")
	if _, err := sendGoodResp(ctx, t, xdsclient.ListenerResource, fakeServer, versionLDS, goodLDSResponse1, goodLDSRequest, cbLDS); err != nil {
		t.Fatal(err)
	}
	versionLDS++
	startXDS(ctx, t, xdsclient.RouteConfigResource, v2c, fakeServer.XDSRequestChan, goodRDSRequest, "", "")
	if _, err := sendGoodResp(ctx, t, xdsclient.RouteConfigResource, fakeServer, versionRDS, goodRDSResponse1, goodRDSRequest, cbRDS); err != nil {
		t.Fatal(err)
	}
	versionRDS++
	startXDS(ctx, t, xdsclient.ClusterResource, v2c, fakeServer.XDSRequestChan, goodCDSRequest, "", "")
	if _, err := sendGoodResp(ctx, t, xdsclient.ClusterResource, fakeServer, versionCDS, goodCDSResponse1, goodCDSRequest, cbCDS); err != nil {
		t.Fatal(err)
	}
	versionCDS++
	startXDS(ctx, t, xdsclient.EndpointsResource, v2c, fakeServer.XDSRequestChan, goodEDSRequest, "", "")
	if _, err := sendGoodResp(ctx, t, xdsclient.EndpointsResource, fakeServer, versionEDS, goodEDSResponse1, goodEDSRequest, cbEDS); err != nil {
		t.Fatal(err)
	}
	versionEDS++

	// Send a bad response, and check for nack.
	if err := sendBadResp(ctx, t, xdsclient.ListenerResource, fakeServer, versionLDS, goodLDSRequest); err != nil {
		t.Fatal(err)
	}
	versionLDS++
	if err := sendBadResp(ctx, t, xdsclient.RouteConfigResource, fakeServer, versionRDS, goodRDSRequest); err != nil {
		t.Fatal(err)
	}
	versionRDS++
	if err := sendBadResp(ctx, t, xdsclient.ClusterResource, fakeServer, versionCDS, goodCDSRequest); err != nil {
		t.Fatal(err)
	}
	versionCDS++
	if err := sendBadResp(ctx, t, xdsclient.EndpointsResource, fakeServer, versionEDS, goodEDSRequest); err != nil {
		t.Fatal(err)
	}
	versionEDS++

	// send another good response, and check for ack, with the new version.
	if _, err := sendGoodResp(ctx, t, xdsclient.ListenerResource, fakeServer, versionLDS, goodLDSResponse1, goodLDSRequest, cbLDS); err != nil {
		t.Fatal(err)
	}
	versionLDS++
	if _, err := sendGoodResp(ctx, t, xdsclient.RouteConfigResource, fakeServer, versionRDS, goodRDSResponse1, goodRDSRequest, cbRDS); err != nil {
		t.Fatal(err)
	}
	versionRDS++
	if _, err := sendGoodResp(ctx, t, xdsclient.ClusterResource, fakeServer, versionCDS, goodCDSResponse1, goodCDSRequest, cbCDS); err != nil {
		t.Fatal(err)
	}
	versionCDS++
	if _, err := sendGoodResp(ctx, t, xdsclient.EndpointsResource, fakeServer, versionEDS, goodEDSResponse1, goodEDSRequest, cbEDS); err != nil {
		t.Fatal(err)
	}
	versionEDS++
}

// Test when the first response is invalid, and is nacked, the nack requests
// should have an empty version string.
func (s) TestV2ClientAckFirstIsNack(t *testing.T) {
	var versionLDS = 1000

	fakeServer, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	v2c, cbLDS, _, _, _, v2cCleanup := startXDSV2Client(t, cc)
	defer v2cCleanup()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start the watch, send a good response, and check for ack.
	startXDS(ctx, t, xdsclient.ListenerResource, v2c, fakeServer.XDSRequestChan, goodLDSRequest, "", "")

	nonce := sendXDSRespWithVersion(fakeServer.XDSResponseChan, &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{{}},
		TypeUrl:   version.V2ListenerURL,
	}, versionLDS)
	t.Logf("Bad response pushed to fakeServer...")

	// The expected version string is an empty string, because this is the first
	// response, and it's nacked (so there's no previous ack version).
	if err := compareXDSRequest(ctx, fakeServer.XDSRequestChan, goodLDSRequest, "", nonce, true); err != nil {
		t.Errorf("Failed to receive request: %v", err)
	}
	t.Logf("Bad response nacked")
	versionLDS++

	sendGoodResp(ctx, t, xdsclient.ListenerResource, fakeServer, versionLDS, goodLDSResponse1, goodLDSRequest, cbLDS)
	versionLDS++
}

// Test when a nack is sent after a new watch, we nack with the previous acked
// version (instead of resetting to empty string).
func (s) TestV2ClientAckNackAfterNewWatch(t *testing.T) {
	var versionLDS = 1000

	fakeServer, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	v2c, cbLDS, _, _, _, v2cCleanup := startXDSV2Client(t, cc)
	defer v2cCleanup()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start the watch, send a good response, and check for ack.
	startXDS(ctx, t, xdsclient.ListenerResource, v2c, fakeServer.XDSRequestChan, goodLDSRequest, "", "")
	nonce, err := sendGoodResp(ctx, t, xdsclient.ListenerResource, fakeServer, versionLDS, goodLDSResponse1, goodLDSRequest, cbLDS)
	if err != nil {
		t.Fatal(err)
	}
	// Start a new watch. The version in the new request should be the version
	// from the previous response, thus versionLDS before ++.
	startXDS(ctx, t, xdsclient.ListenerResource, v2c, fakeServer.XDSRequestChan, goodLDSRequest, strconv.Itoa(versionLDS), nonce)
	versionLDS++

	// This is an invalid response after the new watch.
	nonce = sendXDSRespWithVersion(fakeServer.XDSResponseChan, &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{{}},
		TypeUrl:   version.V2ListenerURL,
	}, versionLDS)
	t.Logf("Bad response pushed to fakeServer...")

	// The expected version string is the previous acked version.
	if err := compareXDSRequest(ctx, fakeServer.XDSRequestChan, goodLDSRequest, strconv.Itoa(versionLDS-1), nonce, true); err != nil {
		t.Errorf("Failed to receive request: %v", err)
	}
	t.Logf("Bad response nacked")
	versionLDS++

	if _, err := sendGoodResp(ctx, t, xdsclient.ListenerResource, fakeServer, versionLDS, goodLDSResponse1, goodLDSRequest, cbLDS); err != nil {
		t.Fatal(err)
	}
	versionLDS++
}

// TestV2ClientAckNewWatchAfterCancel verifies the new request for a new watch
// after the previous watch is canceled, has the right version.
func (s) TestV2ClientAckNewWatchAfterCancel(t *testing.T) {
	var versionCDS = 3000

	fakeServer, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	v2c, _, _, cbCDS, _, v2cCleanup := startXDSV2Client(t, cc)
	defer v2cCleanup()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start a CDS watch.
	v2c.AddWatch(xdsclient.ClusterResource, goodClusterName1)
	if err := compareXDSRequest(ctx, fakeServer.XDSRequestChan, goodCDSRequest, "", "", false); err != nil {
		t.Fatal(err)
	}
	t.Logf("FakeServer received %v request...", xdsclient.ClusterResource)

	// Send a good CDS response, this function waits for the ACK with the right
	// version.
	nonce, err := sendGoodResp(ctx, t, xdsclient.ClusterResource, fakeServer, versionCDS, goodCDSResponse1, goodCDSRequest, cbCDS)
	if err != nil {
		t.Fatal(err)
	}
	// Cancel the CDS watch, and start a new one. The new watch should have the
	// version from the response above.
	v2c.RemoveWatch(xdsclient.ClusterResource, goodClusterName1)
	// Wait for a request with no resource names, because the only watch was
	// removed.
	emptyReq := &xdspb.DiscoveryRequest{Node: goodNodeProto, TypeUrl: version.V2ClusterURL}
	if err := compareXDSRequest(ctx, fakeServer.XDSRequestChan, emptyReq, strconv.Itoa(versionCDS), nonce, false); err != nil {
		t.Fatalf("Failed to receive %v request: %v", xdsclient.ClusterResource, err)
	}
	v2c.AddWatch(xdsclient.ClusterResource, goodClusterName1)
	// Wait for a request with correct resource names and version.
	if err := compareXDSRequest(ctx, fakeServer.XDSRequestChan, goodCDSRequest, strconv.Itoa(versionCDS), nonce, false); err != nil {
		t.Fatalf("Failed to receive %v request: %v", xdsclient.ClusterResource, err)
	}
	versionCDS++

	// Send a bad response with the next version.
	if err := sendBadResp(ctx, t, xdsclient.ClusterResource, fakeServer, versionCDS, goodCDSRequest); err != nil {
		t.Fatal(err)
	}
	versionCDS++

	// send another good response, and check for ack, with the new version.
	if _, err := sendGoodResp(ctx, t, xdsclient.ClusterResource, fakeServer, versionCDS, goodCDSResponse1, goodCDSRequest, cbCDS); err != nil {
		t.Fatal(err)
	}
	versionCDS++
}

// TestV2ClientAckCancelResponseRace verifies if the response and ACK request
// race with cancel (which means the ACK request will not be sent on wire,
// because there's no active watch), the nonce will still be updated, and the
// new request with the new watch will have the correct nonce.
func (s) TestV2ClientAckCancelResponseRace(t *testing.T) {
	var versionCDS = 3000

	fakeServer, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	v2c, _, _, cbCDS, _, v2cCleanup := startXDSV2Client(t, cc)
	defer v2cCleanup()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start a CDS watch.
	v2c.AddWatch(xdsclient.ClusterResource, goodClusterName1)
	if err := compareXDSRequest(ctx, fakeServer.XDSRequestChan, goodCDSRequest, "", "", false); err != nil {
		t.Fatalf("Failed to receive %v request: %v", xdsclient.ClusterResource, err)
	}
	t.Logf("FakeServer received %v request...", xdsclient.ClusterResource)

	// send a good response, and check for ack, with the new version.
	nonce, err := sendGoodResp(ctx, t, xdsclient.ClusterResource, fakeServer, versionCDS, goodCDSResponse1, goodCDSRequest, cbCDS)
	if err != nil {
		t.Fatal(err)
	}
	// Cancel the watch before the next response is sent. This mimics the case
	// watch is canceled while response is on wire.
	v2c.RemoveWatch(xdsclient.ClusterResource, goodClusterName1)
	// Wait for a request with no resource names, because the only watch was
	// removed.
	emptyReq := &xdspb.DiscoveryRequest{Node: goodNodeProto, TypeUrl: version.V2ClusterURL}
	if err := compareXDSRequest(ctx, fakeServer.XDSRequestChan, emptyReq, strconv.Itoa(versionCDS), nonce, false); err != nil {
		t.Fatalf("Failed to receive %v request: %v", xdsclient.ClusterResource, err)
	}
	versionCDS++

	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if req, err := fakeServer.XDSRequestChan.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("Got unexpected xds request after watch is canceled: %v", req)
	}

	// Send a good response.
	nonce = sendXDSRespWithVersion(fakeServer.XDSResponseChan, goodCDSResponse1, versionCDS)
	t.Logf("Good %v response pushed to fakeServer...", xdsclient.ClusterResource)

	// Expect no ACK because watch was canceled.
	sCtx, sCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if req, err := fakeServer.XDSRequestChan.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("Got unexpected xds request after watch is canceled: %v", req)
	}

	// Still expected an callback update, because response was good.
	if _, err := cbCDS.Receive(ctx); err != nil {
		t.Fatalf("Timeout when expecting %v update", xdsclient.ClusterResource)
	}

	// Start a new watch. The new watch should have the nonce from the response
	// above, and version from the first good response.
	v2c.AddWatch(xdsclient.ClusterResource, goodClusterName1)
	if err := compareXDSRequest(ctx, fakeServer.XDSRequestChan, goodCDSRequest, strconv.Itoa(versionCDS-1), nonce, false); err != nil {
		t.Fatalf("Failed to receive %v request: %v", xdsclient.ClusterResource, err)
	}

	// Send a bad response with the next version.
	if err := sendBadResp(ctx, t, xdsclient.ClusterResource, fakeServer, versionCDS, goodCDSRequest); err != nil {
		t.Fatal(err)
	}
	versionCDS++

	// send another good response, and check for ack, with the new version.
	if _, err := sendGoodResp(ctx, t, xdsclient.ClusterResource, fakeServer, versionCDS, goodCDSResponse1, goodCDSRequest, cbCDS); err != nil {
		t.Fatal(err)
	}
	versionCDS++
}
