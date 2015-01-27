/*
 * Copyright 2014, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
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
 */

package io.grpc.testing;

import io.grpc.Metadata;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptor;
import io.grpc.ServerInterceptors;
import io.grpc.Status;

import java.util.Arrays;
import java.util.HashSet;
import java.util.Set;

/**
 * Common utility functions useful for writing tests.
 */
public class TestUtils {

  /**
   * Echo the request headers from a client into response headers and trailers. Useful for
   * testing end-to-end metadata propagation.
   */
  public static ServerInterceptor echoRequestHeadersInterceptor(final Metadata.Key<?>... keys) {
    final Set<Metadata.Key<?>> keySet = new HashSet<Metadata.Key<?>>(Arrays.asList(keys));
    return new ServerInterceptor() {
      @Override
      public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(String method,
           ServerCall<RespT> call,
           final Metadata.Headers requestHeaders,
           ServerCallHandler<ReqT, RespT> next) {
        ServerCall.Listener<ReqT> listener = next.startCall(method,
            new ServerInterceptors.ForwardingServerCall<RespT>(call) {
              boolean sentHeaders;

              @Override
              public void sendHeaders(Metadata.Headers responseHeaders) {
                responseHeaders.merge(requestHeaders, keySet);
                super.sendHeaders(responseHeaders);
                sentHeaders = true;
              }

              @Override
              public void sendPayload(RespT payload) {
                if (!sentHeaders) {
                  sendHeaders(new Metadata.Headers());
                }
                super.sendPayload(payload);
              }

              @Override
              public void close(Status status, Metadata.Trailers trailers) {
                trailers.merge(requestHeaders, keySet);
                super.close(status, trailers);
              }
            }, requestHeaders);
        return listener;
      }
    };
  }

  private TestUtils() {}
}
