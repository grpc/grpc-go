/*
 *
 * Copyright 2015, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

package main

import (
	"flag"
	"io"
	"log"
	"math/rand"
	"net"
	"strconv"
	"time"

	pb "github.com/wonderfly/grpc-go/examples/route_guide"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var (
	useTLS        = flag.Bool("use_tls", false, "Connection uses TLS if true, else plain TCP")
	caFile        = flag.String("tls_ca_file", "testdata/ca.pem", "The file containning the CA root cert file")
	serverHost    = flag.String("server_host", "127.0.0.1", "The server host name")
	serverPort    = flag.Int("server_port", 10000, "The server port number")
	tlsServerName = flag.String("tls_server_name", "x.test.youtube.com", "The server name use to verify the hostname returned by TLS handshake")
)

// doGetFeature gets the feature for the given point.
func doGetFeature(client pb.RouteGuideClient, point *pb.Point) {
	log.Printf("Getting feature for point (%d, %d)", point.Latitude, point.Longitude)
	reply, err := client.GetFeature(context.Background(), point)
	if err != nil {
		log.Fatalf("%v.GetFeatures(_) = _, %v: ", client, err)
		return
	}
	log.Println(reply)
}

// doListFeatures lists all the features within the given bounding Rectangle.
func doListFeatures(client pb.RouteGuideClient, rect *pb.Rectangle) {
	log.Printf("Looking for features within %v", rect)
	stream, err := client.ListFeatures(context.Background(), rect)
	if err != nil {
		log.Fatalf("%v.ListFeatures(_) = _, %v", client, err)
	}
	var rpcStatus error
	for {
		reply, err := stream.Recv()
		if err != nil {
			rpcStatus = err
			break
		}
		log.Println(reply)
	}
	if rpcStatus != io.EOF {
		log.Fatalf("%v.ListFeatures(_) = _, %v", client, err)
	}
}

// doRecordRoute sends a sequence of points to server and expects to get a RouteSummary from server.
func doRecordRoute(client pb.RouteGuideClient) {
	// Create a random number of random points
	rand.Seed(time.Now().UnixNano())
	pointCount := rand.Int31n(100) + 2 // Tranverse at least two points
	points := make([]*pb.Point, pointCount)
	for i, _ := range points {
		points[i] = randomPoint()
	}
	log.Printf("Traversing %d points.", len(points))
	stream, err := client.RecordRoute(context.Background())
	if err != nil {
		log.Fatalf("%v.RecordRoute(_) = _, %v", client, err)
	}
	for _, point := range points {
		if err := stream.Send(point); err != nil {
			log.Fatalf("%v.Send(%v) = %v", stream, point, err)
		}
	}
	reply, err := stream.CloseAndRecv()
	if err != nil {
		log.Fatalf("%v.CloseAndRecv() got error %v, want %v", stream, err, nil)
	}
	log.Printf("Route summary: %v\n", reply)
}

// doRouteChat receives a sequence of route notes, while sending notes for various locations.
func doRouteChat(client pb.RouteGuideClient) {
	notes := []*pb.RouteNote{
		&pb.RouteNote{&pb.Point{0, 1}, "First message"},
		&pb.RouteNote{&pb.Point{0, 2}, "Second message"},
		&pb.RouteNote{&pb.Point{0, 3}, "Third message"},
		&pb.RouteNote{&pb.Point{0, 1}, "Fourth message"},
		&pb.RouteNote{&pb.Point{0, 2}, "Fifth message"},
		&pb.RouteNote{&pb.Point{0, 3}, "Sixth message"},
	}
	stream, err := client.RouteChat(context.Background())
	if err != nil {
		log.Fatalf("%v.RouteChat(_) = _, %v", client, err)
	}
	c := make(chan int)
	go func() {
		for {
			in, err := stream.Recv()
			if err == io.EOF {
				// read done.
				c <- 1
				return
			}
			if err != nil {
				log.Fatalf("Failed to receive a note : %v\n", err)
			}
			log.Printf("Got message %s at point(%d, %d)\n", in.Message, in.Location.Latitude, in.Location.Longitude)
		}
	}()
	for _, note := range notes {
		if err := stream.Send(note); err != nil {
			log.Fatalf("Failed to send a note: %v\n", err)
		}
	}
	stream.CloseSend()
	<-c
}

func randomPoint() *pb.Point {
	lat := (rand.Int31n(180) - 90) * 1e7
	long := (rand.Int31n(360) - 180) * 1e7
	return &pb.Point{lat, long}
}

func main() {
	flag.Parse()
	serverAddr := net.JoinHostPort(*serverHost, strconv.Itoa(*serverPort))
	var opts []grpc.DialOption
	if *useTLS {
		var sn string
		if *tlsServerName != "" {
			sn = *tlsServerName
		}
		var creds credentials.TransportAuthenticator
		if *caFile != "" {
			var err error
			creds, err = credentials.NewClientTLSFromFile(*caFile, sn)
			if err != nil {
				log.Fatalf("Failed to create TLS credentials %v", err)
			}
		} else {
			creds = credentials.NewClientTLSFromCert(nil, sn)
		}
		opts = append(opts, grpc.WithClientTLS(creds))
	}
	conn, err := grpc.Dial(serverAddr, opts...)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	defer conn.Close()
	client := pb.NewRouteGuideClient(conn)

	// Looking for a valid feature
	doGetFeature(client, &pb.Point{409146138, -746188906})

	// Feature missing.
	doGetFeature(client, &pb.Point{0, 0})

	// Looking for features between 40, -75 and 42, -73.
	doListFeatures(client, &pb.Rectangle{&pb.Point{400000000, -750000000}, &pb.Point{420000000, -730000000}})

	// RecordRoute
	doRecordRoute(client)

	// RouteChat
	doRouteChat(client)
}
