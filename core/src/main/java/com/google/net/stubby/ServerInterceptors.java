package com.google.net.stubby;

import com.google.common.base.Preconditions;
import com.google.common.collect.ImmutableList;
import com.google.net.stubby.Server.CallHandler;
import com.google.net.stubby.Server.Interceptor;
import com.google.net.stubby.Server.MethodDefinition;
import com.google.net.stubby.Server.ServiceDefinition;

import java.util.List;
import java.util.Iterator;

/** Utility class for {@link Server.Interceptor}s. */
public class ServerInterceptors {
  // Prevent instantiation
  private ServerInterceptors() {}

  /**
   * Create a new {@code ServiceDefinition} whose {@link Server.CallHandler}s will call {@code
   * interceptors} before calling the pre-existing {@code CallHandler}.
   */
  public static ServiceDefinition intercept(ServiceDefinition serviceDef,
      List<Interceptor> interceptors) {
    Preconditions.checkNotNull(serviceDef);
    List<Interceptor> immutableInterceptors = ImmutableList.copyOf(interceptors);
    if (immutableInterceptors.isEmpty()) {
      return serviceDef;
    }
    ServiceDefinition.Builder serviceDefBuilder = ServiceDefinition.builder(serviceDef.getName());
    for (MethodDefinition<?, ?> method : serviceDef.getMethods()) {
      wrapAndAddMethod(serviceDefBuilder, method, immutableInterceptors);
    }
    return serviceDefBuilder.build();
  }

  private static <ReqT, RespT> void wrapAndAddMethod(ServiceDefinition.Builder serviceDefBuilder,
      MethodDefinition<ReqT, RespT> method, List<Interceptor> interceptors) {
    CallHandler<ReqT, RespT> callHandler
        = InterceptCallHandler.create(interceptors, method.getCallHandler());
    serviceDefBuilder.addMethod(method.getName(), method.getRequestMarshaller(),
        method.getResponseMarshaller(), callHandler);
  }

  private static class InterceptCallHandler<ReqT, RespT> implements CallHandler<ReqT, RespT> {
    public static <ReqT, RespT> InterceptCallHandler<ReqT, RespT> create(
        List<Interceptor> interceptors, CallHandler<ReqT, RespT> callHandler) {
      return new InterceptCallHandler<ReqT, RespT>(interceptors, callHandler);
    }

    private final List<Interceptor> interceptors;
    private final CallHandler<ReqT, RespT> callHandler;

    private InterceptCallHandler(List<Interceptor> interceptors,
        CallHandler<ReqT, RespT> callHandler) {
      this.interceptors = interceptors;
      this.callHandler = callHandler;
    }

    @Override
    public Server.Call.Listener<ReqT> startCall(MethodDescriptor<ReqT, RespT> method,
        Server.Call<ReqT, RespT> call) {
      return ProcessInterceptorsCallHandler.create(interceptors.iterator(), callHandler)
          .startCall(method, call);
    }
  }

  private static class ProcessInterceptorsCallHandler<ReqT, RespT>
      implements CallHandler<ReqT, RespT> {
    public static <ReqT, RespT> ProcessInterceptorsCallHandler<ReqT, RespT> create(
        Iterator<Interceptor> interceptors, CallHandler<ReqT, RespT> callHandler) {
      return new ProcessInterceptorsCallHandler<ReqT, RespT>(interceptors, callHandler);
    }

    private Iterator<Interceptor> interceptors;
    private final CallHandler<ReqT, RespT> callHandler;

    private ProcessInterceptorsCallHandler(Iterator<Interceptor> interceptors,
        CallHandler<ReqT, RespT> callHandler) {
      this.interceptors = interceptors;
      this.callHandler = callHandler;
    }

    @Override
    public Server.Call.Listener<ReqT> startCall(MethodDescriptor<ReqT, RespT> method,
        Server.Call<ReqT, RespT> call) {
      if (interceptors != null && interceptors.hasNext()) {
        return interceptors.next().interceptCall(method, call, this);
      } else {
        interceptors = null;
        return callHandler.startCall(method, call);
      }
    }
  }
}
