/*
 *
 * Copyright 2016, Google Inc.
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

package grpc

import (
	"reflect"
	"testing"

	"golang.org/x/net/context"
)

func TestMultiUnaryInterceptor(t *testing.T) {
	var values []int
	interceptor := newMultiUnaryServerInterceptor(
		func(ctx context.Context, req interface{}, info *UnaryServerInfo, handler UnaryHandler) (interface{}, error) {
			req = req.(int) + 1
			values = append(values, req.(int))
			return handler(ctx, req)
		},
		func(ctx context.Context, req interface{}, info *UnaryServerInfo, handler UnaryHandler) (interface{}, error) {
			req = req.(int) + 2
			values = append(values, req.(int))
			return handler(ctx, req)
		},
		func(ctx context.Context, req interface{}, info *UnaryServerInfo, handler UnaryHandler) (interface{}, error) {
			req = req.(int) + 3
			values = append(values, req.(int))
			return handler(ctx, req)
		},
		func(ctx context.Context, req interface{}, info *UnaryServerInfo, handler UnaryHandler) (interface{}, error) {
			req = req.(int) + 4
			values = append(values, req.(int))
			return handler(ctx, req)
		},
	)
	resp, err := interceptor(
		context.Background(),
		0,
		nil,
		func(ctx context.Context, req interface{}) (interface{}, error) {
			req = req.(int) + 4
			values = append(values, req.(int))
			return req, nil
		},
	)
	if err != nil {
		t.Errorf(err.Error())
	}
	if resp != 14 {
		t.Errorf("expected 14, got %v", resp)
	}
	expected := []int{1, 3, 6, 10, 14}
	if !reflect.DeepEqual(expected, values) {
		t.Errorf("expected %v, got %v", expected, values)
	}
}

func TestMultiStreamInterceptor(t *testing.T) {
	var values []int
	interceptor := newMultiStreamServerInterceptor(
		func(srv interface{}, ss ServerStream, info *StreamServerInfo, handler StreamHandler) error {
			srv = srv.(int) + 1
			values = append(values, srv.(int))
			return handler(srv, ss)
		},
		func(srv interface{}, ss ServerStream, info *StreamServerInfo, handler StreamHandler) error {
			srv = srv.(int) + 2
			values = append(values, srv.(int))
			return handler(srv, ss)
		},
		func(srv interface{}, ss ServerStream, info *StreamServerInfo, handler StreamHandler) error {
			srv = srv.(int) + 3
			values = append(values, srv.(int))
			return handler(srv, ss)
		},
		func(srv interface{}, ss ServerStream, info *StreamServerInfo, handler StreamHandler) error {
			srv = srv.(int) + 4
			values = append(values, srv.(int))
			return handler(srv, ss)
		},
	)
	err := interceptor(
		0,
		nil,
		nil,
		func(srv interface{}, stream ServerStream) error {
			values = append(values, srv.(int)+4)
			return nil
		},
	)
	if err != nil {
		t.Errorf(err.Error())
	}
	expected := []int{1, 3, 6, 10, 14}
	if !reflect.DeepEqual(expected, values) {
		t.Errorf("expected %v, got %v", expected, values)
	}
}
