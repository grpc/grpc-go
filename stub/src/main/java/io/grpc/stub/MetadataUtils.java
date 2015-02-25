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

package io.grpc.stub;

import io.grpc.Call;
import io.grpc.Channel;
import io.grpc.ClientInterceptor;
import io.grpc.ClientInterceptors.ForwardingCall;
import io.grpc.ClientInterceptors.ForwardingListener;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;

import java.util.concurrent.atomic.AtomicReference;

/**
 * Utility functions for binding and receiving headers
 */
public class MetadataUtils {

  /**
   * Attaches a set of request headers to a stub.
   *
   * @param stub to bind the headers to.
   * @param extraHeaders the headers to be passed by each call on the returned stub.
   * @return an implementation of the stub with {@code extraHeaders} bound to each call.
   */
  @SuppressWarnings({"unchecked", "rawtypes"})
  public static <T extends AbstractStub> T attachHeaders(
      T stub,
      final Metadata.Headers extraHeaders) {
    return (T) stub.configureNewStub().addInterceptor(
        newAttachHeadersInterceptor(extraHeaders)).build();
  }

  /**
   * Returns a client interceptor that attaches a set of headers to requests.
   *
   * @param extraHeaders the headers to be passed by each call that is processed by the returned
   *                     interceptor
   */
  public static ClientInterceptor newAttachHeadersInterceptor(final Metadata.Headers extraHeaders) {
    return new ClientInterceptor() {
      @Override
      public <ReqT, RespT> Call<ReqT, RespT> interceptCall(MethodDescriptor<ReqT, RespT> method,
          Channel next) {
        return new ForwardingCall<ReqT, RespT>(next.newCall(method)) {
          @Override
          public void start(Listener<RespT> responseListener, Metadata.Headers headers) {
            headers.merge(extraHeaders);
            super.start(responseListener, headers);
          }
        };
      }
    };
  }

  /**
   * Captures the last received metadata for a stub. Useful for testing
   *
   * @param stub to capture for
   * @param headersCapture to record the last received headers
   * @param trailersCapture to record the last received trailers
   * @return an implementation of the stub with {@code extraHeaders} bound to each call.
   */
  @SuppressWarnings({"unchecked", "rawtypes"})
  public static <T extends AbstractStub> T captureMetadata(
      T stub,
      AtomicReference<Metadata.Headers> headersCapture,
      AtomicReference<Metadata.Trailers> trailersCapture) {
    return (T) stub.configureNewStub().addInterceptor(
        newCaptureMetadataInterceptor(headersCapture, trailersCapture)).build();
  }

  /**
   * Captures the last received metadata on a channel. Useful for testing.
   *
   * @param headersCapture to record the last received headers
   * @param trailersCapture to record the last received trailers
   * @return an implementation of the channel with captures installed.
   */
  @SuppressWarnings("unchecked")
  public static ClientInterceptor newCaptureMetadataInterceptor(
      final AtomicReference<Metadata.Headers> headersCapture,
      final AtomicReference<Metadata.Trailers> trailersCapture) {
    return new ClientInterceptor() {
      @Override
      public <ReqT, RespT> Call<ReqT, RespT> interceptCall(MethodDescriptor<ReqT, RespT> method,
          Channel next) {
        return new ForwardingCall<ReqT, RespT>(next.newCall(method)) {
          @Override
          public void start(Listener<RespT> responseListener, Metadata.Headers headers) {
            headersCapture.set(null);
            trailersCapture.set(null);
            super.start(new ForwardingListener<RespT>(responseListener) {
              @Override
              public void onHeaders(Metadata.Headers headers) {
                headersCapture.set(headers);
                super.onHeaders(headers);
              }

              @Override
              public void onClose(Status status, Metadata.Trailers trailers) {
                trailersCapture.set(trailers);
                super.onClose(status, trailers);
              }
            }, headers);
          }
        };
      }
    };
  }
}
