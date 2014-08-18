package com.google.net.stubby;

import javax.annotation.concurrent.ThreadSafe;

/**
 * Interface to begin processing incoming RPCs. Advanced applications and generated code implement
 * this interface to implement service methods.
 */
@ThreadSafe
public interface ServerCallHandler<ReqT, RespT> {
  /**
   * Produce a non-{@code null} listener for the incoming call. Implementations are free to call
   * methods on {@code call} before this method has returned.
   *
   * <p>If the implementation throws an exception, {@code call} will be closed with an error.
   * Implementations must not throw an exception if they started processing that may use {@code
   * call} on another thread.
   *
   * @param method metadata concerning the call
   * @param call object for responding
   * @return listener for processing incoming messages for {@code call}
   */
  ServerCall.Listener<ReqT> startCall(MethodDescriptor<ReqT, RespT> method,
      ServerCall<ReqT, RespT> call);
}
