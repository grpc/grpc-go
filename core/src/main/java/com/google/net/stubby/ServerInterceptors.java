package com.google.net.stubby;

import com.google.common.base.Preconditions;
import com.google.common.collect.ImmutableList;
import com.google.net.stubby.ServerMethodDefinition;
import com.google.net.stubby.ServerCallHandler;
import com.google.net.stubby.ServerInterceptor;
import com.google.net.stubby.ServerServiceDefinition;

import java.util.List;
import java.util.Iterator;

/** Utility class for {@link ServerInterceptor}s. */
public class ServerInterceptors {
  // Prevent instantiation
  private ServerInterceptors() {}

  /**
   * Create a new {@code ServerServiceDefinition} whose {@link ServerCallHandler}s will call {@code
   * interceptors} before calling the pre-existing {@code ServerCallHandler}.
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
    serviceDefBuilder.addMethod(method.getName(), method.getRequestMarshaller(),
        method.getResponseMarshaller(), callHandler);
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
    public ServerCall.Listener<ReqT> startCall(MethodDescriptor<ReqT, RespT> method,
        ServerCall<ReqT, RespT> call) {
      return ProcessInterceptorsCallHandler.create(interceptors.iterator(), callHandler)
          .startCall(method, call);
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
    public ServerCall.Listener<ReqT> startCall(MethodDescriptor<ReqT, RespT> method,
        ServerCall<ReqT, RespT> call) {
      if (interceptors != null && interceptors.hasNext()) {
        return interceptors.next().interceptCall(method, call, this);
      } else {
        interceptors = null;
        return callHandler.startCall(method, call);
      }
    }
  }
}
