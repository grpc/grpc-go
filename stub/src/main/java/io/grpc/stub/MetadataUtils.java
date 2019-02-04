/*
 * Copyright 2014 The gRPC Authors
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
 */

package io.grpc.stub;

import static com.google.common.base.Preconditions.checkNotNull;

import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.ExperimentalApi;
import io.grpc.ForwardingClientCall.SimpleForwardingClientCall;
import io.grpc.ForwardingClientCallListener.SimpleForwardingClientCallListener;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import java.util.concurrent.atomic.AtomicReference;

/**
 * Utility functions for binding and receiving headers.
 */
public final class MetadataUtils {
  // Prevent instantiation
  private MetadataUtils() {}

  /**
   * Attaches a set of request headers to a stub.
   *
   * @param stub to bind the headers to.
   * @param extraHeaders the headers to be passed by each call on the returned stub.
   * @return an implementation of the stub with {@code extraHeaders} bound to each call.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1789")
  public static <T extends AbstractStub<T>> T attachHeaders(T stub, Metadata extraHeaders) {
    return stub.withInterceptors(newAttachHeadersInterceptor(extraHeaders));
  }

  /**
   * Returns a client interceptor that attaches a set of headers to requests.
   *
   * @param extraHeaders the headers to be passed by each call that is processed by the returned
   *                     interceptor
   */
  public static ClientInterceptor newAttachHeadersInterceptor(Metadata extraHeaders) {
    return new HeaderAttachingClientInterceptor(extraHeaders);
  }

  private static final class HeaderAttachingClientInterceptor implements ClientInterceptor {

    private final Metadata extraHeaders;

    // Non private to avoid synthetic class
    HeaderAttachingClientInterceptor(Metadata extraHeaders) {
      this.extraHeaders = checkNotNull(extraHeaders, "extraHeaders");
    }

    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
        MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
      return new HeaderAttachingClientCall<>(next.newCall(method, callOptions));
    }

    private final class HeaderAttachingClientCall<ReqT, RespT>
        extends SimpleForwardingClientCall<ReqT, RespT> {

      // Non private to avoid synthetic class
      HeaderAttachingClientCall(ClientCall<ReqT, RespT> call) {
        super(call);
      }

      @Override
      public void start(Listener<RespT> responseListener, Metadata headers) {
        headers.merge(extraHeaders);
        super.start(responseListener, headers);
      }
    }
  }

  /**
   * Captures the last received metadata for a stub. Useful for testing
   *
   * @param stub to capture for
   * @param headersCapture to record the last received headers
   * @param trailersCapture to record the last received trailers
   * @return an implementation of the stub that allows to access the last received call's
   *         headers and trailers via {@code headersCapture} and {@code trailersCapture}.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1789")
  public static <T extends AbstractStub<T>> T captureMetadata(
      T stub,
      AtomicReference<Metadata> headersCapture,
      AtomicReference<Metadata> trailersCapture) {
    return stub.withInterceptors(
        newCaptureMetadataInterceptor(headersCapture, trailersCapture));
  }

  /**
   * Captures the last received metadata on a channel. Useful for testing.
   *
   * @param headersCapture to record the last received headers
   * @param trailersCapture to record the last received trailers
   * @return an implementation of the channel with captures installed.
   */
  public static ClientInterceptor newCaptureMetadataInterceptor(
      AtomicReference<Metadata> headersCapture, AtomicReference<Metadata> trailersCapture) {
    return new MetadataCapturingClientInterceptor(headersCapture, trailersCapture);
  }

  private static final class MetadataCapturingClientInterceptor implements ClientInterceptor {

    final AtomicReference<Metadata> headersCapture;
    final AtomicReference<Metadata> trailersCapture;

    // Non private to avoid synthetic class
    MetadataCapturingClientInterceptor(
        AtomicReference<Metadata> headersCapture, AtomicReference<Metadata> trailersCapture) {
      this.headersCapture = checkNotNull(headersCapture, "headersCapture");
      this.trailersCapture = checkNotNull(trailersCapture, "trailersCapture");
    }

    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
        MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
      return new MetadataCapturingClientCall<>(next.newCall(method, callOptions));
    }

    private final class MetadataCapturingClientCall<ReqT, RespT>
        extends SimpleForwardingClientCall<ReqT, RespT> {

      // Non private to avoid synthetic class
      MetadataCapturingClientCall(ClientCall<ReqT, RespT> call) {
        super(call);
      }

      @Override
      public void start(ClientCall.Listener<RespT> responseListener, Metadata headers) {
        headersCapture.set(null);
        trailersCapture.set(null);
        super.start(new MetadataCapturingClientCallListener(responseListener), headers);
      }

      private final class MetadataCapturingClientCallListener
          extends SimpleForwardingClientCallListener<RespT> {

        MetadataCapturingClientCallListener(ClientCall.Listener<RespT> responseListener) {
          super(responseListener);
        }

        @Override
        public void onHeaders(Metadata headers) {
          headersCapture.set(headers);
          super.onHeaders(headers);
        }

        @Override
        public void onClose(Status status, Metadata trailers) {
          trailersCapture.set(trailers);
          super.onClose(status, trailers);
        }
      }
    }
  }
}
