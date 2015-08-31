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

/*
Package glogger defines glog-based logging for grpc.
*/
package glogger

import (
	"bytes"
	"fmt"

	"github.com/golang/glog"
	"google.golang.org/grpc/grpclog"
)

func init() {
	grpclog.SetLogger(&glogger{})
}

type glogger struct {
	kvs [][2]string
}

func (g *glogger) Err(err error) grpclog.Logger {
	return g.With("err", err)
}

func (g *glogger) With(keyvals ...interface{}) grpclog.Logger {
	if len(keyvals) == 0 {
		return g
	}
	if len(keyvals)%2 != 0 {
		keyvals = append(keyvals, "<MISSING VALUE>")
	}
	kvs := make([][2]string, len(g.kvs), len(g.kvs)+len(keyvals)/2)
	copy(kvs, g.kvs)
	for i := 0; i < len(keyvals); i += 2 {
		k := keyvals[i].(string)
		switch v := keyvals[i+1].(type) {
		case string:
			kvs = append(kvs, [2]string{k, v})
		case []byte:
			kvs = append(kvs, [2]string{k, fmt.Sprintf("%x", v)})
		case fmt.Stringer:
			kvs = append(kvs, [2]string{k, v.String()})
		case fmt.GoStringer:
			kvs = append(kvs, [2]string{k, v.GoString()})
		case interface {
			Error() string
		}:
			kvs = append(kvs, [2]string{k, v.Error()})
		default:
			kvs = append(kvs, [2]string{k, fmt.Sprintf("%#v", v)})
		}
	}
	return &glogger{kvs: kvs}
}

func (g *glogger) Fatal(msg string) {
	glog.Fatal(g.print(msg))
}

func (g *glogger) Print(msg string) {
	glog.Info(g.print(msg))
}

func (g *glogger) print(msg string) string {
	buf := new(bytes.Buffer)
	fmt.Fprint(buf, msg)
	for _, kv := range g.kvs {
		fmt.Fprintf(buf, " %q=%q", kv[0], kv[1])
	}
	return buf.String()
}
