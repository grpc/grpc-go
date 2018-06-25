// +build js,wasm

/*
 *
 * Copyright 2018 gRPC authors.
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

package grpc

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/transport"
)

func parseMsg(p *parser, s *transport.Stream, maxReceiveMessageSize int, pf payloadFormat, d []byte) ([]byte, error) {
	if pf == trailer || pf == compressedTrailer {
		trailers, err := readTrailers(bytes.NewReader(d), len(d))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "grpc: failed to parse returned trailers: %v", err)
		}
		// Holy layer violation :(
		// This is necessary because the gRPC-Web trailers are part of the response body. See
		// https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-WEB.md#protocol-differences-vs-grpc-over-http2
		err = s.SetTrailers(trailers)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "grpc: failed to set trailers on stream: %v", err)
		}
		// Sanity check that parser is done. This should return io.EOF
		_, _, err = p.recvMsg(maxReceiveMessageSize)
		if err == io.EOF {
			return nil, err
		}
		return nil, status.Errorf(codes.Internal, "grpc: unexpected error reading last message from stream: %v", err)
	}
	return d, nil
}

// readTrailers consumes the rest of the reader and parses the
// contents as "\r\n"-separated trailers, in accordance with
// https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-WEB.md#protocol-differences-vs-grpc-over-http2
func readTrailers(in io.Reader, bufSize int) (http.Header, error) {
	s := bufio.NewScanner(in)
	buf := make([]byte, bufSize)
	s.Buffer(buf, len(buf))
	// Uses http.Header instead of metadata.MD as .Add method
	// normalizes trailer keys.
	trailers := http.Header{}
	for s.Scan() {
		v := s.Text()
		kv := strings.SplitN(v, ": ", 2)
		if len(kv) != 2 {
			return nil, errors.New("malformed trailer: " + v)
		}
		trailers.Add(kv[0], kv[1])
	}

	return trailers, s.Err()
}
