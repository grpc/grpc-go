/*
 *
 * Copyright 2015 gRPC authors.
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
	"log"
	"os"

	"crypto/tls"
        "crypto/x509"

	"io/ioutil"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
)

const (
	address     = "localhost:50051"
	defaultName = "world"
)

func main() {
	certificate, err := tls.LoadX509KeyPair(
		  "client.cert.pem",
	    "client.key.pem",
    )

	    certPool := x509.NewCertPool()
	    bs, err := ioutil.ReadFile("ca.cert.pem")
	    if err != nil {
		      log.Fatalf("failed to read ca cert: %s", err)
	      }

	      ok := certPool.AppendCertsFromPEM(bs)
	      if !ok {
		        log.Fatal("failed to append certs")
		}

		transportCreds := credentials.NewTLS(&tls.Config{
			  ServerName:   "localhost",
			    Certificates: []tls.Certificate{certificate},
			      RootCAs:      certPool,
		      })
        // Create the client TLS credentials
//        creds, err := credentials.NewClientTLSFromFile("server.crt", "")
  //      if err != nil {
    //             log.Fatalf("could not load tls cert: %s", err)
      //  }

	// Set up a connection to the server.
	dialOption := grpc.WithTransportCredentials(transportCreds)
	conn, err := grpc.Dial(address, dialOption)
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewGreeterClient(conn)

	// Contact the server and print out its response.
	name := defaultName
	if len(os.Args) > 1 {
		name = os.Args[1]
	}
	r, err := c.SayHello(context.Background(), &pb.HelloRequest{Name: name})
	if err != nil {
		log.Fatalf("could not greet: %v", err)
	}
	log.Printf("Greeting: %s", r.Message)
}
