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
Package grpclog defines logging for grpc.
*/
package grpclog // import "google.golang.org/grpc/grpclog"

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

type logfmt struct {
	kvs [][2]string
	out io.Writer
	mu  sync.Mutex
}

func (l *logfmt) Print(msg string) {
	l.print("info", msg)
}

func (l *logfmt) Fatal(msg string) {
	defer os.Exit(1)
	l.print("fatal", msg)
}

func (l *logfmt) print(lvl, msg string) {
	now := time.Now().Format(time.RFC3339Nano)
	msg = strings.TrimSuffix(msg, "\n")
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.out, `"time"=%q "lvl"=%q "msg"=%q`, now, lvl, msg)
	for _, kv := range l.kvs {
		fmt.Fprintf(l.out, " %q=%q", kv[0], kv[1])
	}
	l.out.Write([]byte{'\n'})
}

func (l *logfmt) Err(err error) Logger {
	return l.With("err", err)
}

func (l *logfmt) With(keyvals ...interface{}) Logger {
	if len(keyvals) == 0 {
		return l
	}
	if len(keyvals)%2 != 0 {
		keyvals = append(keyvals, "<MISSING VALUE>")
	}
	kvs := make([][2]string, len(l.kvs), len(l.kvs)+len(keyvals)/2)
	copy(kvs, l.kvs)
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
	return &logfmt{kvs: kvs, out: l.out, mu: l.mu}
}

// Use a logfmt logger by default.
var logger Logger = &logfmt{out: os.Stderr}

// Logger is a minimal interface for a structured logger.
type Logger interface {
	Err(err error) Logger
	With(keyvals ...interface{}) Logger
	Fatal(msg string)
	Print(msg string)
}

// SetLogger sets the logger that is used in grpc.
func SetLogger(l Logger) {
	logger = l
}

// Err is the equivalent of With("err", err).
func Err(err error) Logger {
	return logger.Err(err)
}

// With appends a set of key-values to a logger. The key-values will
// be logged everytime the subsequent logger emits a message.
func With(keyvals ...interface{}) Logger {
	return logger.With(keyvals...)
}

// Fatal is equivalent to Print() followed by a call to os.Exit() with a non-zero exit code.
func Fatal(msg string) {
	logger.Fatal(msg)
}

// Print prints to the logger.
func Print(msg string) {
	logger.Print(msg)
}
