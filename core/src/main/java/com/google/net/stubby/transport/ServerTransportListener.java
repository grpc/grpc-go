package com.google.net.stubby.transport;

import com.google.net.stubby.Metadata;

/**
 * A observer of a server-side transport for stream creation events.
 */
public interface ServerTransportListener {

  /**
   * Called when a new stream was created by the remote client.
   *
   * @param stream the newly created stream.
   * @param method the full method name being called on the server.
   * @param headers containing metadata for the call.
   * @return a listener for events on the new stream.
   */
  ServerStreamListener streamCreated(ServerStream stream, String method,
      Metadata.Headers headers);
}
