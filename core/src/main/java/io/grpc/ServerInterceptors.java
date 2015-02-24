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

package io.grpc;

import com.google.common.base.Preconditions;
import com.google.common.collect.ImmutableList;

import java.util.Arrays;
import java.util.Iterator;
import java.util.List;

/**
 * Utility methods for working with {@link ServerInterceptor}s.
 */
public class ServerInterceptors {
  // Prevent instantiation
  private ServerInterceptors() {}

  /**
   * Create a new {@code ServerServiceDefinition} whose {@link ServerCallHandler}s will call
   * {@code interceptors} before calling the pre-existing {@code ServerCallHandler}.
   *
   * @param serviceDef the service definition for which to intercept all its methods.
   * @param interceptors array of interceptors to apply to the service.
   * @return a wrapped version of {@code serviceDef} with the interceptors applied.
   */
  public static ServerServiceDefinition intercept(ServerServiceDefinition serviceDef,
                                                  ServerInterceptor... interceptors) {
    return intercept(serviceDef, Arrays.asList(interceptors));
  }

  /**
   * Create a new {@code ServerServiceDefinition} whose {@link ServerCallHandler}s will call
   * {@code interceptors} before calling the pre-existing {@code ServerCallHandler}.
   *
   * @param serviceDef the service definition for which to intercept all its methods.
   * @param interceptors list of interceptors to apply to the service.
   * @return a wrapped version of {@code serviceDef} with the interceptors applied.
   */
  public static ServerServiceDefinition intercept(ServerServiceDefinition serviceDef,
      List<ServerInterceptor> interceptors) {
    Preconditions.checkNotNull(serviceDef);
    List<ServerInterceptor> immutableInterceptors = ImmutableList.copyOf(interceptors);
    if (immutableInterceptors.isEmpty()) {
      return serviceDef;
    }
    ServerServiceDefinition.Builder serviceDefBuilder
        = ServerServiceDefinition.builder(serviceDef.getName());
    for (ServerMethodDefinition<?, ?> method : serviceDef.getMethods()) {
      wrapAndAddMethod(serviceDefBuilder, method, immutableInterceptors);
    }
    return serviceDefBuilder.build();
  }

  private static <ReqT, RespT> void wrapAndAddMethod(
      ServerServiceDefinition.Builder serviceDefBuilder, ServerMethodDefinition<ReqT, RespT> method,
      List<ServerInterceptor> interceptors) {
    ServerCallHandler<ReqT, RespT> callHandler
        = InterceptCallHandler.create(interceptors, method.getServerCallHandler());
    serviceDefBuilder.addMethod(method.withServerCallHandler(callHandler));
  }

  private static class InterceptCallHandler<ReqT, RespT> implements ServerCallHandler<ReqT, RespT> {
    public static <ReqT, RespT> InterceptCallHandler<ReqT, RespT> create(
        List<ServerInterceptor> interceptors, ServerCallHandler<ReqT, RespT> callHandler) {
      return new InterceptCallHandler<ReqT, RespT>(interceptors, callHandler);
    }

    private final List<ServerInterceptor> interceptors;
    private final ServerCallHandler<ReqT, RespT> callHandler;

    private InterceptCallHandler(List<ServerInterceptor> interceptors,
        ServerCallHandler<ReqT, RespT> callHandler) {
      this.interceptors = interceptors;
      this.callHandler = callHandler;
    }

    @Override
    public ServerCall.Listener<ReqT> startCall(String method, ServerCall<RespT> call,
        Metadata.Headers headers) {
      return ProcessInterceptorsCallHandler.create(interceptors.iterator(), callHandler)
          .startCall(method, call, headers);
    }
  }

  private static class ProcessInterceptorsCallHandler<ReqT, RespT>
      implements ServerCallHandler<ReqT, RespT> {
    public static <ReqT, RespT> ProcessInterceptorsCallHandler<ReqT, RespT> create(
        Iterator<ServerInterceptor> interceptors, ServerCallHandler<ReqT, RespT> callHandler) {
      return new ProcessInterceptorsCallHandler<ReqT, RespT>(interceptors, callHandler);
    }

    private Iterator<ServerInterceptor> interceptors;
    private final ServerCallHandler<ReqT, RespT> callHandler;

    private ProcessInterceptorsCallHandler(Iterator<ServerInterceptor> interceptors,
        ServerCallHandler<ReqT, RespT> callHandler) {
      this.interceptors = interceptors;
      this.callHandler = callHandler;
    }

    @Override
    public ServerCall.Listener<ReqT> startCall(String method, ServerCall<RespT> call,
        Metadata.Headers headers) {
      if (interceptors != null && interceptors.hasNext()) {
        return interceptors.next().interceptCall(method, call, headers, this);
      } else {
        Preconditions.checkState(interceptors != null,
            "The call handler has already been called. "
            + "Some interceptor must have called on \"next\" twice.");
        interceptors = null;
        return callHandler.startCall(method, call, headers);
      }
    }
  }

  /**
   * Utility base class for decorating {@link ServerCall} instances.
   */
  public static class ForwardingServerCall<RespT> extends ServerCall<RespT> {

    private final ServerCall<RespT> delegate;

    public ForwardingServerCall(ServerCall<RespT> delegate) {
      this.delegate = delegate;
    }

    @Override
    public void request(int numMessages) {
      delegate.request(numMessages);
    }

    @Override
    public void sendHeaders(Metadata.Headers headers) {
      delegate.sendHeaders(headers);
    }

    @Override
    public void sendPayload(RespT payload) {
      delegate.sendPayload(payload);
    }

    @Override
    public void close(Status status, Metadata.Trailers trailers) {
      delegate.close(status, trailers);
    }

    @Override
    public boolean isCancelled() {
      return delegate.isCancelled();
    }
  }

  /**
   * Utility base class for decorating {@link ServerCall.Listener} instances.
   */
  public static class ForwardingListener<RespT> extends ServerCall.Listener<RespT> {

    private final ServerCall.Listener<RespT> delegate;

    public ForwardingListener(ServerCall.Listener<RespT> delegate) {
      this.delegate = delegate;
    }

    @Override
    public void onPayload(RespT payload) {
      delegate.onPayload(payload);
    }

    @Override
    public void onHalfClose() {
      delegate.onHalfClose();
    }

    @Override
    public void onCancel() {
      delegate.onCancel();
    }

    @Override
    public void onComplete() {
      delegate.onComplete();
    }
  }
}
