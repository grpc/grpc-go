package com.google.net.stubby.transport;

import com.google.common.util.concurrent.Service;

/**
 * A listener to a server for transport creation events.
 */
public interface ServerListener {

  /**
   * Called upon the establishment of a new client connection.
   *
   * @param transport the new transport to be observed.
   * @return a listener for stream creation events on the transport.
   */
  ServerTransportListener transportCreated(Service transport);
}
