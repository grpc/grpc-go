package com.google.net.stubby.stub;

import javax.annotation.concurrent.ThreadSafe;

/**
 * An abstraction layer between stubs and the transport details. Channels are responsible for
 * call initiation and tracking. Channels can be decorated to provide cross-cutting behaviors
 * across all operations in a stub.
 */
@ThreadSafe
public interface Channel {

  /**
   * Prepare a call to the given service method.
   */
  public <ReqT, RespT> Call<ReqT, RespT> prepare(MethodDescriptor<ReqT, RespT> method);
}