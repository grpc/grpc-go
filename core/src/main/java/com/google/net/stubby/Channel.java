package com.google.net.stubby;

import javax.annotation.concurrent.ThreadSafe;

/**
 * An abstraction layer between stubs and the transport details for use with outgoing calls.
 * Channels are responsible for call initiation and tracking. Channels can be decorated to provide
 * cross-cutting behaviors across all operations in a stub.
 */
@ThreadSafe
public interface Channel {

  /**
   * Create a call to the given service method.
   */
  // TODO(user): perform start() as part of new Call creation?
  public <ReqT, RespT> Call<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method);
}
