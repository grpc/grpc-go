package com.google.net.stubby;

import javax.annotation.concurrent.ThreadSafe;

/**
 * Interface for intercepting outgoing RPCs before it reaches the channel.
 */
@ThreadSafe
public interface ClientInterceptor {
  /**
   * Intercept a new call. General semantics of {@link Channel#newCall} apply. {@code next} may only
   * be called once. Returned {@link Call} must not be null.
   *
   * <p>If the implementation throws an exception, the RPC will not be started.
   *
   * @param method the method to be called
   * @param next next processor in the interceptor chain
   * @return the call object for the new RPC
   */
  <ReqT, RespT> Call<ReqT, RespT> interceptCall(MethodDescriptor<ReqT, RespT> method, Channel next);
}
