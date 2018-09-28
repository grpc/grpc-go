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

package binarylog

import (
	"io"

	"github.com/golang/protobuf/proto"
	pb "google.golang.org/grpc/binarylog/grpc_binarylog_v1"
	"google.golang.org/grpc/grpclog"
)

var (
	defaultSink Sink = &noopSink{} // TODO(blog): change this default (file in /tmp).
)

// SetDefaultSink sets the sink where binary logs will be written to.
//
// Not thread safe. Only set during initialization.
func SetDefaultSink(s Sink) {
	defaultSink = s
}

// Sink writes log entry into the binary log sink.
type Sink interface {
	Write(*pb.GrpcLogEntry)
}

type noopSink struct{}

func (ns *noopSink) Write(*pb.GrpcLogEntry) {}

// NewWriterSink creates a binary log sink with the given writer.
func NewWriterSink(w io.Writer) Sink {
	return &writerSink{out: w}
}

type writerSink struct {
	out io.Writer
}

func (fs *writerSink) Write(e *pb.GrpcLogEntry) {
	b, err := proto.Marshal(e)
	if err != nil {
		grpclog.Infof("binary logging: failed to marshal proto message: %v", err)
	}
	fs.out.Write(b)
}
