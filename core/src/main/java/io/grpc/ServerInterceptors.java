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

package io.grpc;

import com.google.common.base.Preconditions;
import java.io.BufferedInputStream;
import java.io.InputStream;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;

/**
 * Utility methods for working with {@link ServerInterceptor}s.
 */
public final class ServerInterceptors {
  // Prevent instantiation
  private ServerInterceptors() {}

  /**
   * Create a new {@code ServerServiceDefinition} whose {@link ServerCallHandler}s will call
   * {@code interceptors} before calling the pre-existing {@code ServerCallHandler}. The first
   * interceptor will have its {@link ServerInterceptor#interceptCall} called first.
   *
   * @param serviceDef   the service definition for which to intercept all its methods.
   * @param interceptors array of interceptors to apply to the service.
   * @return a wrapped version of {@code serviceDef} with the interceptors applied.
   */
  public static ServerServiceDefinition interceptForward(ServerServiceDefinition serviceDef,
                                                         ServerInterceptor... interceptors) {
    return interceptForward(serviceDef, Arrays.asList(interceptors));
  }

  public static ServerServiceDefinition interceptForward(BindableService bindableService,
      ServerInterceptor... interceptors) {
    return interceptForward(bindableService.bindService(), Arrays.asList(interceptors));
  }

  /**
   * Create a new {@code ServerServiceDefinition} whose {@link ServerCallHandler}s will call
   * {@code interceptors} before calling the pre-existing {@code ServerCallHandler}. The first
   * interceptor will have its {@link ServerInterceptor#interceptCall} called first.
   *
   * @param serviceDef   the service definition for which to intercept all its methods.
   * @param interceptors list of interceptors to apply to the service.
   * @return a wrapped version of {@code serviceDef} with the interceptors applied.
   */
  public static ServerServiceDefinition interceptForward(
      ServerServiceDefinition serviceDef,
      List<? extends ServerInterceptor> interceptors) {
    List<? extends ServerInterceptor> copy = new ArrayList<>(interceptors);
    Collections.reverse(copy);
    return intercept(serviceDef, copy);
  }

  public static ServerServiceDefinition interceptForward(
      BindableService bindableService,
      List<? extends ServerInterceptor> interceptors) {
    return interceptForward(bindableService.bindService(), interceptors);
  }

  /**
   * Create a new {@code ServerServiceDefinition} whose {@link ServerCallHandler}s will call
   * {@code interceptors} before calling the pre-existing {@code ServerCallHandler}. The last
   * interceptor will have its {@link ServerInterceptor#interceptCall} called first.
   *
   * @param serviceDef   the service definition for which to intercept all its methods.
   * @param interceptors array of interceptors to apply to the service.
   * @return a wrapped version of {@code serviceDef} with the interceptors applied.
   */
  public static ServerServiceDefinition intercept(ServerServiceDefinition serviceDef,
                                                  ServerInterceptor... interceptors) {
    return intercept(serviceDef, Arrays.asList(interceptors));
  }

  public static ServerServiceDefinition intercept(BindableService bindableService,
      ServerInterceptor... interceptors) {
    Preconditions.checkNotNull(bindableService, "bindableService");
    return intercept(bindableService.bindService(), Arrays.asList(interceptors));
  }

  /**
   * Create a new {@code ServerServiceDefinition} whose {@link ServerCallHandler}s will call
   * {@code interceptors} before calling the pre-existing {@code ServerCallHandler}. The last
   * interceptor will have its {@link ServerInterceptor#interceptCall} called first.
   *
   * @param serviceDef   the service definition for which to intercept all its methods.
   * @param interceptors list of interceptors to apply to the service.
   * @return a wrapped version of {@code serviceDef} with the interceptors applied.
   */
  public static ServerServiceDefinition intercept(ServerServiceDefinition serviceDef,
                                                  List<? extends ServerInterceptor> interceptors) {
    Preconditions.checkNotNull(serviceDef, "serviceDef");
    if (interceptors.isEmpty()) {
      return serviceDef;
    }
    ServerServiceDefinition.Builder serviceDefBuilder
        = ServerServiceDefinition.builder(serviceDef.getServiceDescriptor());
    for (ServerMethodDefinition<?, ?> method : serviceDef.getMethods()) {
      wrapAndAddMethod(serviceDefBuilder, method, interceptors);
    }
    return serviceDefBuilder.build();
  }

  public static ServerServiceDefinition intercept(BindableService bindableService,
      List<? extends ServerInterceptor> interceptors) {
    Preconditions.checkNotNull(bindableService, "bindableService");
    return intercept(bindableService.bindService(), interceptors);
  }

  /**
   * Create a new {@code ServerServiceDefinition} whose {@link MethodDescriptor} serializes to
   * and from InputStream for all methods.  The InputStream is guaranteed return true for
   * markSupported().  The {@code ServerCallHandler} created will automatically
   * convert back to the original types for request and response before calling the existing
   * {@code ServerCallHandler}.  Calling this method combined with the intercept methods will
   * allow the developer to choose whether to intercept messages of InputStream, or the modeled
   * types of their application.
   *
   * @param serviceDef the service definition to convert messages to InputStream
   * @return a wrapped version of {@code serviceDef} with the InputStream conversion applied.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1712")
  public static ServerServiceDefinition useInputStreamMessages(
      final ServerServiceDefinition serviceDef) {
    final MethodDescriptor.Marshaller<InputStream> marshaller =
        new MethodDescriptor.Marshaller<InputStream>() {
      @Override
      public InputStream stream(final InputStream value) {
        return value;
      }

      @Override
      public InputStream parse(final InputStream stream) {
        if (stream.markSupported()) {
          return stream;
        } else {
          return new BufferedInputStream(stream);
        }
      }
    };

    return useMarshalledMessages(serviceDef, marshaller);
  }

  /**
   * Create a new {@code ServerServiceDefinition} whose {@link MethodDescriptor} serializes to
   * and from T for all methods.  The {@code ServerCallHandler} created will automatically
   * convert back to the original types for request and response before calling the existing
   * {@code ServerCallHandler}.  Calling this method combined with the intercept methods will
   * allow the developer to choose whether to intercept messages of T, or the modeled types
   * of their application.  This can also be chained to allow for interceptors to handle messages
   * as multiple different T types within the chain if the added cost of serialization is not
   * a concern.
   *
   * @param serviceDef the service definition to convert messages to T
   * @return a wrapped version of {@code serviceDef} with the T conversion applied.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1712")
  public static <T> ServerServiceDefinition useMarshalledMessages(
      final ServerServiceDefinition serviceDef,
      final MethodDescriptor.Marshaller<T> marshaller) {
    List<ServerMethodDefinition<?, ?>> wrappedMethods =
        new ArrayList<>();
    List<MethodDescriptor<?, ?>> wrappedDescriptors =
        new ArrayList<>();
    // Wrap the descriptors
    for (final ServerMethodDefinition<?, ?> definition : serviceDef.getMethods()) {
      final MethodDescriptor<?, ?> originalMethodDescriptor = definition.getMethodDescriptor();
      final MethodDescriptor<T, T> wrappedMethodDescriptor =
          originalMethodDescriptor.toBuilder(marshaller, marshaller).build();
      wrappedDescriptors.add(wrappedMethodDescriptor);
      wrappedMethods.add(wrapMethod(definition, wrappedMethodDescriptor));
    }
    // Build the new service descriptor
    final ServerServiceDefinition.Builder serviceBuilder = ServerServiceDefinition
        .builder(new ServiceDescriptor(serviceDef.getServiceDescriptor().getName(),
            wrappedDescriptors));
    // Create the new service definiton.
    for (ServerMethodDefinition<?, ?> definition : wrappedMethods) {
      serviceBuilder.addMethod(definition);
    }
    return serviceBuilder.build();
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

  static final class InterceptCallHandler<ReqT, RespT> implements ServerCallHandler<ReqT, RespT> {
    public static <ReqT, RespT> InterceptCallHandler<ReqT, RespT> create(
        ServerInterceptor interceptor, ServerCallHandler<ReqT, RespT> callHandler) {
      return new InterceptCallHandler<>(interceptor, callHandler);
    }

    private final ServerInterceptor interceptor;
    private final ServerCallHandler<ReqT, RespT> callHandler;

    private InterceptCallHandler(
        ServerInterceptor interceptor, ServerCallHandler<ReqT, RespT> callHandler) {
      this.interceptor = Preconditions.checkNotNull(interceptor, "interceptor");
      this.callHandler = callHandler;
    }

    @Override
    public ServerCall.Listener<ReqT> startCall(
        ServerCall<ReqT, RespT> call,
        Metadata headers) {
      return interceptor.interceptCall(call, headers, callHandler);
    }
  }

  static <OReqT, ORespT, WReqT, WRespT> ServerMethodDefinition<WReqT, WRespT> wrapMethod(
      final ServerMethodDefinition<OReqT, ORespT> definition,
      final MethodDescriptor<WReqT, WRespT> wrappedMethod) {
    final ServerCallHandler<WReqT, WRespT> wrappedHandler = wrapHandler(
        definition.getServerCallHandler(),
        definition.getMethodDescriptor(),
        wrappedMethod);
    return ServerMethodDefinition.create(wrappedMethod, wrappedHandler);
  }

  private static <OReqT, ORespT, WReqT, WRespT> ServerCallHandler<WReqT, WRespT> wrapHandler(
      final ServerCallHandler<OReqT, ORespT> originalHandler,
      final MethodDescriptor<OReqT, ORespT> originalMethod,
      final MethodDescriptor<WReqT, WRespT> wrappedMethod) {
    return new ServerCallHandler<WReqT, WRespT>() {
      @Override
      public ServerCall.Listener<WReqT> startCall(
          final ServerCall<WReqT, WRespT> call,
          final Metadata headers) {
        final ServerCall<OReqT, ORespT> unwrappedCall =
            new PartialForwardingServerCall<OReqT, ORespT>() {
          @Override
          protected ServerCall<WReqT, WRespT> delegate() {
            return call;
          }

          @Override
          public void sendMessage(ORespT message) {
            final InputStream is = originalMethod.streamResponse(message);
            final WRespT wrappedMessage = wrappedMethod.parseResponse(is);
            delegate().sendMessage(wrappedMessage);
          }

          @Override
          public MethodDescriptor<OReqT, ORespT> getMethodDescriptor() {
            return originalMethod;
          }
        };

        final ServerCall.Listener<OReqT> originalListener = originalHandler
            .startCall(unwrappedCall, headers);

        return new PartialForwardingServerCallListener<WReqT>() {
          @Override
          protected ServerCall.Listener<OReqT> delegate() {
            return originalListener;
          }

          @Override
          public void onMessage(WReqT message) {
            final InputStream is = wrappedMethod.streamRequest(message);
            final OReqT originalMessage = originalMethod.parseRequest(is);
            delegate().onMessage(originalMessage);
          }
        };
      }
    };
  }
}
