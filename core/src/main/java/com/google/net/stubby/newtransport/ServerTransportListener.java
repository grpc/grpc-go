package com.google.net.stubby.newtransport;

import com.google.net.stubby.MethodDescriptor;

/**
 * A observer of a server-side transport for stream creation events.
 */
public interface ServerTransportListener {

  /**
   * Called when a new stream was created by the remote client.
   *
   * @param stream the newly created stream.
   * @param method the full method name being called on the server.
   * @return a listener for events on the new stream.
   */
  StreamListener streamCreated(ServerStream stream, String method);
}
