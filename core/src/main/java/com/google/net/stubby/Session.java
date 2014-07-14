package com.google.net.stubby;

import java.util.Map;

/**
 * Session interface to be bound to the transport layer which is used by the higher-level
 * layers to dispatch calls.
 * <p>
 *  A session is used as a factory to start a named remote {@link Request} operation. The caller
 *  provides a {@link Response} operation to receive responses. Clients will make calls on the
 *  {@link Request} to send state to the server, simultaneously the transport layer will make calls
 *  into the {@link Response} as the server provides response state.
 *  <p>
 */
public interface Session {

  /**
   * Start a request in the context of this session.
   */
  public Request startRequest(String operationName,
                              Map<String, String> headers,
                              Response.ResponseBuilder responseBuilder);
}
