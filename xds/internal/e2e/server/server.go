/*
 *
 * Copyright 2020 gRPC authors.
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

package main

import (
	"fmt"
	"log"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/testutils/fakeserver"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	routepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	httppb "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v2"
	anypb "github.com/golang/protobuf/ptypes/any"
)

var (
	httpConnManagerURL = "type.googleapis.com/envoy.config.filter.network.http_connection_manager.v2.HttpConnectionManager"
	ldsURL             = "type.googleapis.com/envoy.api.v2.Listener"
	rdsURL             = "type.googleapis.com/envoy.api.v2.RouteConfiguration"
	cdsURL             = "type.googleapis.com/envoy.api.v2.Cluster"
	edsURL             = "type.googleapis.com/envoy.api.v2.ClusterLoadAssignment"

	goodHTTPConnManager1 = &httppb.HttpConnectionManager{
		RouteSpecifier: &httppb.HttpConnectionManager_Rds{
			Rds: &httppb.Rds{
				RouteConfigName: "route.name",
			},
		},
	}
	marshaledConnMgr1, _ = proto.Marshal(goodHTTPConnManager1)
	goodListener1        = &xdspb.Listener{
		Name: "lds.name",
		ApiListener: &listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: httpConnManagerURL,
				Value:   marshaledConnMgr1,
			},
		},
	}
	marshaledListener1, _ = proto.Marshal(goodListener1)
	goodLDSResponse1      = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: ldsURL,
				Value:   marshaledListener1,
			},
		},
		TypeUrl: ldsURL,
	}

	goodRouteConfig1 = &xdspb.RouteConfiguration{
		Name: "route.name",
		VirtualHosts: []*routepb.VirtualHost{
			{
				Domains: []string{"lds.name"},
				Routes: []*routepb.Route{
					{
						Match: &routepb.RouteMatch{PathSpecifier: &routepb.RouteMatch_Prefix{Prefix: ""}},
						Action: &routepb.Route_Route{
							Route: &routepb.RouteAction{
								ClusterSpecifier: &routepb.RouteAction_Cluster{Cluster: "cluster.name"},
							},
						},
					},
				},
			},
		},
	}
	marshaledGoodRouteConfig1, _ = proto.Marshal(goodRouteConfig1)

	goodRDSResponse1 = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: rdsURL,
				Value:   marshaledGoodRouteConfig1,
			},
		},
		TypeUrl: rdsURL,
	}

	goodCluster2 = &xdspb.Cluster{
		Name:                 "cluster.name",
		ClusterDiscoveryType: &xdspb.Cluster_Type{Type: xdspb.Cluster_EDS},
		EdsClusterConfig: &xdspb.Cluster_EdsClusterConfig{
			EdsConfig: &corepb.ConfigSource{
				ConfigSourceSpecifier: &corepb.ConfigSource_Ads{
					Ads: &corepb.AggregatedConfigSource{},
				},
			},
			ServiceName: "service.name",
		},
		LbPolicy: xdspb.Cluster_ROUND_ROBIN,
	}
	marshaledCluster2, _ = proto.Marshal(goodCluster2)
	goodCDSResponse2     = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: cdsURL,
				Value:   marshaledCluster2,
			},
		},
		TypeUrl: cdsURL,
	}

	goodEDSResponse1 = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			func() *anypb.Any {
				clab0 := client.NewClusterLoadAssignmentBuilder("service.name", nil)
				clab0.AddLocality("locality-2", 1, 0, []string{"localhost:19527"}, nil)
				a, _ := ptypes.MarshalAny(clab0.Build())
				return a
			}(),
		},
		TypeUrl: edsURL,
	}
)

func main() {
	s, cleanup, err := fakeserver.StartServer()
	if err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
	defer cleanup()

	fmt.Printf("xds server serving on %s\n", s.Address)

	for {
		fmt.Println()
		fmt.Println(" ----- beginning ----- ")
		fmt.Println(" Press Enter to start serving xDS")
		fmt.Scanln()

		// time.Sleep(time.Second)
		fmt.Println("\nlds")
		ldsReq, err := s.XDSRequestChan.TimedReceive(10 * time.Second)
		fmt.Println(ldsReq, err)
		// time.Sleep(time.Second * 10)
		s.XDSResponseChan <- &fakeserver.Response{
			Resp: goodLDSResponse1,
		}
		s.XDSRequestChan.TimedReceive(10 * time.Second) // ack

		// time.Sleep(time.Second)
		fmt.Println("\nrds")
		rdsReq, err := s.XDSRequestChan.TimedReceive(10 * time.Second)
		fmt.Println(rdsReq, err)
		s.XDSResponseChan <- &fakeserver.Response{
			Resp: goodRDSResponse1,
		}
		s.XDSRequestChan.TimedReceive(10 * time.Second) // ack

		// time.Sleep(time.Second)
		fmt.Println("\ncds")
		cdsReq, err := s.XDSRequestChan.TimedReceive(10 * time.Second)
		fmt.Println(cdsReq, err)
		s.XDSResponseChan <- &fakeserver.Response{
			Resp: goodCDSResponse2,
		}
		s.XDSRequestChan.TimedReceive(10 * time.Second) // ack

		fmt.Println("\ncds")
		edsReq, err := s.XDSRequestChan.TimedReceive(10 * time.Second)
		fmt.Println(edsReq, err)
		s.XDSResponseChan <- &fakeserver.Response{
			Resp: goodEDSResponse1,
		}
		s.XDSRequestChan.TimedReceive(10 * time.Second) // ack

		fmt.Println(" ----- end ----- ")
		fmt.Println()
	}
	select {}
}
