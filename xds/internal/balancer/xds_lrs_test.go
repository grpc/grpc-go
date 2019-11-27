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
 *
 */

package balancer

import (
	"testing"
	"time"

	durationpb "github.com/golang/protobuf/ptypes/duration"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal/client/fakexds"
)

func (s) TestXdsLoadReporting(t *testing.T) {
	originalNewEDSBalancer := newEDSBalancer
	newEDSBalancer = newFakeEDSBalancer
	defer func() {
		newEDSBalancer = originalNewEDSBalancer
	}()

	builder := balancer.Get(edsName)
	cc := newTestClientConn()
	lb, ok := builder.Build(cc, balancer.BuildOptions{Target: resolver.Target{Endpoint: testEDSClusterName}}).(*edsBalancer)
	if !ok {
		t.Fatalf("unable to type assert to *edsBalancer")
	}
	defer lb.Close()

	td, cleanup := fakexds.StartServer(t)
	defer cleanup()

	const intervalNano = 1000 * 1000 * 50
	td.LRS.ReportingInterval = &durationpb.Duration{
		Seconds: 0,
		Nanos:   intervalNano,
	}
	td.LRS.ExpectedEDSClusterName = testEDSClusterName

	cfg := &XDSConfig{
		BalancerName: td.Address,
		// Set lrs server name to an empty string, instead of nil, so the xds
		// server will be used for LRS.
		LrsLoadReportingServerName: new(string),
	}
	lb.UpdateClientConnState(balancer.ClientConnState{BalancerConfig: cfg})
	td.ResponseChan <- &fakexds.Response{Resp: testEDSResp}
	var (
		i     int
		edsLB *fakeEDSBalancer
	)
	for i = 0; i < 10; i++ {
		edsLB = getLatestEdsBalancer()
		if edsLB != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if i == 10 {
		t.Fatal("edsBalancer instance has not been created and assigned to lb.xdsLB after 1s")
	}

	var dropCategories = []string{"drop_for_real", "drop_for_fun"}
	drops := map[string]uint64{
		dropCategories[0]: 31,
		dropCategories[1]: 41,
	}

	for c, d := range drops {
		for i := 0; i < int(d); i++ {
			edsLB.loadStore.CallDropped(c)
			time.Sleep(time.Nanosecond * intervalNano / 10)
		}
	}
	time.Sleep(time.Nanosecond * intervalNano * 2)

	if got := td.LRS.GetDrops(); !cmp.Equal(got, drops) {
		t.Errorf("different: %v %v %v", got, drops, cmp.Diff(got, drops))
	}
}
