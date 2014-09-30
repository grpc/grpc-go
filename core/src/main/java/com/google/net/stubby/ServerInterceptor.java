package com.google.net.stubby;

import javax.annotation.concurrent.ThreadSafe;

/**
 * Interface for intercepting incoming RPCs before the handler receives them.
 */
@ThreadSafe
public interface ServerInterceptor {
  /**
   * Intercept a new call. General semantics of {@link ServerCallHandler#startCall} apply. {@code
   * next} may only be called once. Returned listener must not be {@code null}.
   *
   * <p>If the implementation throws an exception, {@code call} will be closed with an error.
   * Implementations must not throw an exception if they started processing that may use {@code
   * call} on another thread.
   *
   * @param method full method name of the call
   * @param call object for responding
   * @param next next processor in the interceptor chain
   * @return listener for processing incoming messages for {@code call}
   */
  <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(String method, ServerCall<RespT> call,
      Metadata.Headers headers, ServerCallHandler<ReqT, RespT> next);
}
