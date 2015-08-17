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

import java.util.Arrays;
import java.util.List;

/**
 * Utility methods for working with {@link ServerInterceptor}s.
 */
public class ServerInterceptors {
  // Prevent instantiation
  private ServerInterceptors() {}

  /**
   * Create a new {@code ServerServiceDefinition} whose {@link ServerCallHandler}s will call
   * {@code interceptors} before calling the pre-existing {@code ServerCallHandler}. The last
   * interceptor will have its {@link ServerInterceptor#interceptCall} called first.
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
   * {@code interceptors} before calling the pre-existing {@code ServerCallHandler}. The last
   * interceptor will have its {@link ServerInterceptor#interceptCall} called first.
   *
   * @param serviceDef the service definition for which to intercept all its methods.
   * @param interceptors list of interceptors to apply to the service.
   * @return a wrapped version of {@code serviceDef} with the interceptors applied.
   */
  public static ServerServiceDefinition intercept(ServerServiceDefinition serviceDef,
      List<? extends ServerInterceptor> interceptors) {
    Preconditions.checkNotNull(serviceDef);
    if (interceptors.isEmpty()) {
      return serviceDef;
    }
    ServerServiceDefinition.Builder serviceDefBuilder
        = ServerServiceDefinition.builder(serviceDef.getName());
    for (ServerMethodDefinition<?, ?> method : serviceDef.getMethods()) {
      wrapAndAddMethod(serviceDefBuilder, method, interceptors);
    }
    return serviceDefBuilder.build();
  }

  private static <ReqT, RespT> void wrapAndAddMethod(
      ServerServiceDefinition.Builder serviceDefBuilder, ServerMethodDefinition<ReqT, RespT> method,
      List<? extends ServerInterceptor> interceptors) {
    ServerCallHandler<ReqT, RespT> callHandler = method.getServerCallHandler();
    for (ServerInterceptor interceptor : interceptors) {
      callHandler = InterceptCallHandler.create(interceptor, callHandler);
    }
    serviceDefBuilder.addMethod(method.withServerCallHandler(callHandler));
  }

  private static class InterceptCallHandler<ReqT, RespT> implements ServerCallHandler<ReqT, RespT> {
    public static <ReqT, RespT> InterceptCallHandler<ReqT, RespT> create(
        ServerInterceptor interceptor, ServerCallHandler<ReqT, RespT> callHandler) {
      return new InterceptCallHandler<ReqT, RespT>(interceptor, callHandler);
    }

    private final ServerInterceptor interceptor;
    private final ServerCallHandler<ReqT, RespT> callHandler;

    private InterceptCallHandler(ServerInterceptor interceptor,
        ServerCallHandler<ReqT, RespT> callHandler) {
      this.interceptor = Preconditions.checkNotNull(interceptor, "interceptor");
      this.callHandler = callHandler;
    }

    @Override
    public ServerCall.Listener<ReqT> startCall(
        MethodDescriptor<ReqT, RespT> method,
        ServerCall<RespT> call,
        Metadata headers) {
      return interceptor.interceptCall(method, call, headers, callHandler);
    }
  }
}
