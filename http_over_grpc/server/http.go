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

package http_over_grpc

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/textproto"
	"time"

	pb "google.golang.org/grpc/http_over_grpc/http_over_grpc_proto"
)

var (
	defaultTimeout = time.Duration(time.Minute * 2)
)

type responseOrError struct {
	resp *pb.HTTPOverGRPCReply
	err  error
}

// HTTPClient is responsible for communicating with the HTTP Server.
type HTTPClient struct {
	http.ResponseWriter
	// The http client used to communicate with the HTTP server.
	client *http.Client
}

// NewHTTPClient returns a new HTTPClient with the appropriate TLS configuration
// for the environment.
func NewHTTPClient(tlsConfig *TLSConfig) (*HTTPClient, error) {
	transport := &http.Transport{
		Dial: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).Dial,
		IdleConnTimeout: 15 * time.Minute,
	}
	if tlsConfig.InsecureSkipVerify {
		log.Printf("****WARNING USING INSECURE SKIP VERIFY FOR HTTP CLIENT****")
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	} else {
		certPool := x509.NewCertPool()
		ca, err := ioutil.ReadFile(tlsConfig.CAFile)
		if err != nil {
			return nil, err
		}
		certPool.AppendCertsFromPEM(ca)
		transport.TLSClientConfig = &tls.Config{
			RootCAs:    certPool,
			ServerName: tlsConfig.ServerName,
		}
	}
	return &HTTPClient{client: &http.Client{Transport: transport}}, nil
}

func (s *HTTPClient) doRequest(w io.Writer, req *http.Request) error {
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}

	return resp.Write(w)
}

// Do performs the http request and returns an gRPC encapsulating the HTTP
// response.
func (s *HTTPClient) Do(ctx context.Context, req *pb.HTTPOverGRPCRequest) (*pb.HTTPOverGRPCReply, error) {
	if req == nil {
		return nil, fmt.Errorf("[%s] request message was empty", req.String())
	}
	hReq, err := http.NewRequest(req.Method, req.Url, ioutil.NopCloser(bytes.NewReader(req.Body)))
	if err != nil {
		return nil, fmt.Errorf("[%s] request invalid: %v", req.String(), err)
	}
	log.Printf("%s %q", req.Method, req.Url)
	// Copy headers to the request headers.
	toHeader(req.Headers, hReq.Header)

	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(s.doRequest(pw, hReq))
	}()

	type responseOrError struct {
		resp *pb.HTTPOverGRPCReply
		err  error
	}
	// This needs to be buffered, otherwise the goroutine will leak.
	respCh := make(chan responseOrError, 1)
	go func() {
		resp, err := readResponse(pr, hReq)
		respCh <- responseOrError{resp, err}
	}()
	timeoutFunc := func() (*pb.HTTPOverGRPCReply, error) {
		// Cancel the RPC by closing the pipe reader.
		pr.Close()
		return nil, fmt.Errorf("[%s] request timeout for %q", req.String(), req.Url)
	}

	deadline, ok := ctx.Deadline()
	var deadlinetimer *time.Timer
	if !ok {
		deadlinetimer = time.NewTimer(defaultTimeout)
	} else {
		deadlinetimer = time.NewTimer(deadline.Sub(time.Now()))
	}

	defer deadlinetimer.Stop()

	select {
	case re := <-respCh:
		if re.err != nil {
			return nil, fmt.Errorf("[%s] request failed for %q: %v", req.GetMethod(), req.Url, re.err)
		}
		return re.resp, re.err
	case <-deadlinetimer.C:
		return timeoutFunc()
	}
}

func readResponse(pr *io.PipeReader, req *http.Request) (*pb.HTTPOverGRPCReply, error) {
	tp := textproto.NewReader(bufio.NewReader(pr))
	resp, err := http.ReadResponse(tp.R, req)
	if err != nil {
		log.Printf("Response for %q returned err: %v", req.URL, err)
		pr.CloseWithError(err)
		return nil, err
	}
	log.Printf("Response status %q for %q", resp.Status, req.URL)

	b := new(bytes.Buffer)
	if err := resp.Write(b); err != nil {
		return nil, err
	}

	return &pb.HTTPOverGRPCReply{
		Status:  200,
		Body:    b.Bytes(),
		Headers: nil,
	}, nil
}

func toHeader(hs []*pb.Header, header http.Header) {
	for _, h := range hs {
		header[h.Key] = h.Values
	}
}
