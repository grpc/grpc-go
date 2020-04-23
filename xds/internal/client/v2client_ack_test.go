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

package client

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/golang/protobuf/proto"
	anypb "github.com/golang/protobuf/ptypes/any"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeserver"
)

func startXDSV2Client(t *testing.T, cc *grpc.ClientConn) (v2c *v2Client, cbLDS, cbRDS, cbCDS, cbEDS *testutils.Channel, cleanup func()) {
	cbLDS = testutils.NewChannel()
	cbRDS = testutils.NewChannel()
	cbCDS = testutils.NewChannel()
	cbEDS = testutils.NewChannel()
	v2c = newV2Client(&testUpdateReceiver{
		f: func(typeURL string, d map[string]interface{}) {
			t.Logf("Received %s callback with {%+v}", typeURL, d)
			switch typeURL {
			case ldsURL:
				if _, ok := d[goodLDSTarget1]; ok {
					cbLDS.Send(struct{}{})
				}
			case rdsURL:
				if _, ok := d[goodRouteName1]; ok {
					cbRDS.Send(struct{}{})
				}
			case cdsURL:
				if _, ok := d[goodClusterName1]; ok {
					cbCDS.Send(struct{}{})
				}
			case edsURL:
				if _, ok := d[goodEDSName]; ok {
					cbEDS.Send(struct{}{})
				}
			}
		},
	}, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	t.Log("Started xds v2Client...")
	return v2c, cbLDS, cbRDS, cbCDS, cbEDS, v2c.close
}

// compareXDSRequest reads requests from channel, compare it with want.
func compareXDSRequest(ch *testutils.Channel, want *xdspb.DiscoveryRequest, version, nonce string) error {
	val, err := ch.Receive()
	if err != nil {
		return err
	}
	req := val.(*fakeserver.Request)
	if req.Err != nil {
		return fmt.Errorf("unexpected error from request: %v", req.Err)
	}
	wantClone := proto.Clone(want).(*xdspb.DiscoveryRequest)
	wantClone.VersionInfo = version
	wantClone.ResponseNonce = nonce
	if !cmp.Equal(req.Req, wantClone, cmp.Comparer(proto.Equal)) {
		return fmt.Errorf("received request different from want, diff: %s", cmp.Diff(req.Req, wantClone))
	}
	return nil
}

func sendXDSRespWithVersion(ch chan<- *fakeserver.Response, respWithoutVersion *xdspb.DiscoveryResponse, version int) (nonce string) {
	respToSend := proto.Clone(respWithoutVersion).(*xdspb.DiscoveryResponse)
	respToSend.VersionInfo = strconv.Itoa(version)
	nonce = strconv.Itoa(int(time.Now().UnixNano()))
	respToSend.Nonce = nonce
	ch <- &fakeserver.Response{Resp: respToSend}
	return
}

// startXDS calls watch to send the first request. It then sends a good response
// and checks for ack.
func startXDS(t *testing.T, xdsname string, v2c *v2Client, reqChan *testutils.Channel, req *xdspb.DiscoveryRequest, preVersion string, preNonce string) {
	var (
		nameToWatch, typeURLToWatch string
	)
	switch xdsname {
	case "LDS":
		typeURLToWatch = ldsURL
		nameToWatch = goodLDSTarget1
	case "RDS":
		typeURLToWatch = rdsURL
		nameToWatch = goodRouteName1
	case "CDS":
		typeURLToWatch = cdsURL
		nameToWatch = goodClusterName1
	case "EDS":
		typeURLToWatch = edsURL
		nameToWatch = goodEDSName
	}
	v2c.addWatch(typeURLToWatch, nameToWatch)

	if err := compareXDSRequest(reqChan, req, preVersion, preNonce); err != nil {
		t.Fatalf("Failed to receive %s request: %v", xdsname, err)
	}
	t.Logf("FakeServer received %s request...", xdsname)
}

// sendGoodResp sends the good response, with the given version, and a random
// nonce.
//
// It also waits and checks that the ack request contains the given version, and
// the generated nonce.
func sendGoodResp(t *testing.T, xdsname string, fakeServer *fakeserver.Server, version int, goodResp *xdspb.DiscoveryResponse, wantReq *xdspb.DiscoveryRequest, callbackCh *testutils.Channel) (string, error) {
	nonce := sendXDSRespWithVersion(fakeServer.XDSResponseChan, goodResp, version)
	t.Logf("Good %s response pushed to fakeServer...", xdsname)

	if err := compareXDSRequest(fakeServer.XDSRequestChan, wantReq, strconv.Itoa(version), nonce); err != nil {
		return "", fmt.Errorf("failed to receive %s request: %v", xdsname, err)
	}
	t.Logf("Good %s response acked", xdsname)

	if _, err := callbackCh.Receive(); err != nil {
		return "", fmt.Errorf("timeout when expecting %s update", xdsname)
	}
	t.Logf("Good %s response callback executed", xdsname)
	return nonce, nil
}

// sendBadResp sends a bad response with the given version. This response will
// be nacked, so we expect a request with the previous version (version-1).
//
// But the nonce in request should be the new nonce.
func sendBadResp(t *testing.T, xdsname string, fakeServer *fakeserver.Server, version int, wantReq *xdspb.DiscoveryRequest) error {
	var typeURL string
	switch xdsname {
	case "LDS":
		typeURL = ldsURL
	case "RDS":
		typeURL = rdsURL
	case "CDS":
		typeURL = cdsURL
	case "EDS":
		typeURL = edsURL
	}
	nonce := sendXDSRespWithVersion(fakeServer.XDSResponseChan, &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{{}},
		TypeUrl:   typeURL,
	}, version)
	t.Logf("Bad %s response pushed to fakeServer...", xdsname)
	if err := compareXDSRequest(fakeServer.XDSRequestChan, wantReq, strconv.Itoa(version-1), nonce); err != nil {
		return fmt.Errorf("failed to receive %s request: %v", xdsname, err)
	}
	t.Logf("Bad %s response nacked", xdsname)
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

	// Start the watch, send a good response, and check for ack.
	startXDS(t, "LDS", v2c, fakeServer.XDSRequestChan, goodLDSRequest, "", "")
	if _, err := sendGoodResp(t, "LDS", fakeServer, versionLDS, goodLDSResponse1, goodLDSRequest, cbLDS); err != nil {
		t.Fatal(err)
	}
	versionLDS++
	startXDS(t, "RDS", v2c, fakeServer.XDSRequestChan, goodRDSRequest, "", "")
	if _, err := sendGoodResp(t, "RDS", fakeServer, versionRDS, goodRDSResponse1, goodRDSRequest, cbRDS); err != nil {
		t.Fatal(err)
	}
	versionRDS++
	startXDS(t, "CDS", v2c, fakeServer.XDSRequestChan, goodCDSRequest, "", "")
	if _, err := sendGoodResp(t, "CDS", fakeServer, versionCDS, goodCDSResponse1, goodCDSRequest, cbCDS); err != nil {
		t.Fatal(err)
	}
	versionCDS++
	startXDS(t, "EDS", v2c, fakeServer.XDSRequestChan, goodEDSRequest, "", "")
	if _, err := sendGoodResp(t, "EDS", fakeServer, versionEDS, goodEDSResponse1, goodEDSRequest, cbEDS); err != nil {
		t.Fatal(err)
	}
	versionEDS++

	// Send a bad response, and check for nack.
	if err := sendBadResp(t, "LDS", fakeServer, versionLDS, goodLDSRequest); err != nil {
		t.Fatal(err)
	}
	versionLDS++
	if err := sendBadResp(t, "RDS", fakeServer, versionRDS, goodRDSRequest); err != nil {
		t.Fatal(err)
	}
	versionRDS++
	if err := sendBadResp(t, "CDS", fakeServer, versionCDS, goodCDSRequest); err != nil {
		t.Fatal(err)
	}
	versionCDS++
	if err := sendBadResp(t, "EDS", fakeServer, versionEDS, goodEDSRequest); err != nil {
		t.Fatal(err)
	}
	versionEDS++

	// send another good response, and check for ack, with the new version.
	if _, err := sendGoodResp(t, "LDS", fakeServer, versionLDS, goodLDSResponse1, goodLDSRequest, cbLDS); err != nil {
		t.Fatal(err)
	}
	versionLDS++
	if _, err := sendGoodResp(t, "RDS", fakeServer, versionRDS, goodRDSResponse1, goodRDSRequest, cbRDS); err != nil {
		t.Fatal(err)
	}
	versionRDS++
	if _, err := sendGoodResp(t, "CDS", fakeServer, versionCDS, goodCDSResponse1, goodCDSRequest, cbCDS); err != nil {
		t.Fatal(err)
	}
	versionCDS++
	if _, err := sendGoodResp(t, "EDS", fakeServer, versionEDS, goodEDSResponse1, goodEDSRequest, cbEDS); err != nil {
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

	// Start the watch, send a good response, and check for ack.
	startXDS(t, "LDS", v2c, fakeServer.XDSRequestChan, goodLDSRequest, "", "")

	nonce := sendXDSRespWithVersion(fakeServer.XDSResponseChan, &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{{}},
		TypeUrl:   ldsURL,
	}, versionLDS)
	t.Logf("Bad response pushed to fakeServer...")

	// The expected version string is an empty string, because this is the first
	// response, and it's nacked (so there's no previous ack version).
	if err := compareXDSRequest(fakeServer.XDSRequestChan, goodLDSRequest, "", nonce); err != nil {
		t.Errorf("Failed to receive request: %v", err)
	}
	t.Logf("Bad response nacked")
	versionLDS++

	sendGoodResp(t, "LDS", fakeServer, versionLDS, goodLDSResponse1, goodLDSRequest, cbLDS)
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

	// Start the watch, send a good response, and check for ack.
	startXDS(t, "LDS", v2c, fakeServer.XDSRequestChan, goodLDSRequest, "", "")
	nonce, err := sendGoodResp(t, "LDS", fakeServer, versionLDS, goodLDSResponse1, goodLDSRequest, cbLDS)
	if err != nil {
		t.Fatal(err)
	}
	// Start a new watch. The version in the new request should be the version
	// from the previous response, thus versionLDS before ++.
	startXDS(t, "LDS", v2c, fakeServer.XDSRequestChan, goodLDSRequest, strconv.Itoa(versionLDS), nonce)
	versionLDS++

	// This is an invalid response after the new watch.
	nonce = sendXDSRespWithVersion(fakeServer.XDSResponseChan, &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{{}},
		TypeUrl:   ldsURL,
	}, versionLDS)
	t.Logf("Bad response pushed to fakeServer...")

	// The expected version string is the previous acked version.
	if err := compareXDSRequest(fakeServer.XDSRequestChan, goodLDSRequest, strconv.Itoa(versionLDS-1), nonce); err != nil {
		t.Errorf("Failed to receive request: %v", err)
	}
	t.Logf("Bad response nacked")
	versionLDS++

	if _, err := sendGoodResp(t, "LDS", fakeServer, versionLDS, goodLDSResponse1, goodLDSRequest, cbLDS); err != nil {
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

	// Start a CDS watch.
	v2c.addWatch(cdsURL, goodClusterName1)
	if err := compareXDSRequest(fakeServer.XDSRequestChan, goodCDSRequest, "", ""); err != nil {
		t.Fatal(err)
	}
	t.Logf("FakeServer received %s request...", "CDS")

	// Send a good CDS response, this function waits for the ACK with the right
	// version.
	nonce, err := sendGoodResp(t, "CDS", fakeServer, versionCDS, goodCDSResponse1, goodCDSRequest, cbCDS)
	if err != nil {
		t.Fatal(err)
	}
	// Cancel the CDS watch, and start a new one. The new watch should have the
	// version from the response above.
	v2c.removeWatch(cdsURL, goodClusterName1)
	// Wait for a request with no resource names, because the only watch was
	// removed.
	emptyReq := &xdspb.DiscoveryRequest{Node: goodNodeProto, TypeUrl: cdsURL}
	if err := compareXDSRequest(fakeServer.XDSRequestChan, emptyReq, strconv.Itoa(versionCDS), nonce); err != nil {
		t.Fatalf("Failed to receive %s request: %v", "CDS", err)
	}
	v2c.addWatch(cdsURL, goodClusterName1)
	// Wait for a request with correct resource names and version.
	if err := compareXDSRequest(fakeServer.XDSRequestChan, goodCDSRequest, strconv.Itoa(versionCDS), nonce); err != nil {
		t.Fatalf("Failed to receive %s request: %v", "CDS", err)
	}
	versionCDS++

	// Send a bad response with the next version.
	if err := sendBadResp(t, "CDS", fakeServer, versionCDS, goodCDSRequest); err != nil {
		t.Fatal(err)
	}
	versionCDS++

	// send another good response, and check for ack, with the new version.
	if _, err := sendGoodResp(t, "CDS", fakeServer, versionCDS, goodCDSResponse1, goodCDSRequest, cbCDS); err != nil {
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

	// Start a CDS watch.
	v2c.addWatch(cdsURL, goodClusterName1)
	if err := compareXDSRequest(fakeServer.XDSRequestChan, goodCDSRequest, "", ""); err != nil {
		t.Fatalf("Failed to receive %s request: %v", "CDS", err)
	}
	t.Logf("FakeServer received %s request...", "CDS")

	// send a good response, and check for ack, with the new version.
	nonce, err := sendGoodResp(t, "CDS", fakeServer, versionCDS, goodCDSResponse1, goodCDSRequest, cbCDS)
	if err != nil {
		t.Fatal(err)
	}
	// Cancel the watch before the next response is sent. This mimics the case
	// watch is canceled while response is on wire.
	v2c.removeWatch(cdsURL, goodClusterName1)
	// Wait for a request with no resource names, because the only watch was
	// removed.
	emptyReq := &xdspb.DiscoveryRequest{Node: goodNodeProto, TypeUrl: cdsURL}
	if err := compareXDSRequest(fakeServer.XDSRequestChan, emptyReq, strconv.Itoa(versionCDS), nonce); err != nil {
		t.Fatalf("Failed to receive %s request: %v", "CDS", err)
	}
	versionCDS++

	if req, err := fakeServer.XDSRequestChan.Receive(); err != testutils.ErrRecvTimeout {
		t.Fatalf("Got unexpected xds request after watch is canceled: %v", req)
	}

	// Send a good response.
	nonce = sendXDSRespWithVersion(fakeServer.XDSResponseChan, goodCDSResponse1, versionCDS)
	t.Logf("Good %s response pushed to fakeServer...", "CDS")

	// Expect no ACK because watch was canceled.
	if req, err := fakeServer.XDSRequestChan.Receive(); err != testutils.ErrRecvTimeout {
		t.Fatalf("Got unexpected xds request after watch is canceled: %v", req)
	}
	// Still expected an callback update, because response was good.
	if _, err := cbCDS.Receive(); err != nil {
		t.Fatalf("Timeout when expecting %s update", "CDS")
	}

	// Start a new watch. The new watch should have the nonce from the response
	// above, and version from the first good response.
	v2c.addWatch(cdsURL, goodClusterName1)
	if err := compareXDSRequest(fakeServer.XDSRequestChan, goodCDSRequest, strconv.Itoa(versionCDS-1), nonce); err != nil {
		t.Fatalf("Failed to receive %s request: %v", "CDS", err)
	}

	// Send a bad response with the next version.
	if err := sendBadResp(t, "CDS", fakeServer, versionCDS, goodCDSRequest); err != nil {
		t.Fatal(err)
	}
	versionCDS++

	// send another good response, and check for ack, with the new version.
	if _, err := sendGoodResp(t, "CDS", fakeServer, versionCDS, goodCDSResponse1, goodCDSRequest, cbCDS); err != nil {
		t.Fatal(err)
	}
	versionCDS++
}
