package com.google.net.stubby;

import com.google.net.stubby.newtransport.ClientTransport;
import com.google.net.stubby.newtransport.ClientTransportFactory;

/**
 * Shim between Session and Channel. Will be removed when Session is removed.
 *
 * <p>This factory always returns the same instance, which does not adhere to the API.
 */
public class SessionClientTransportFactory implements ClientTransportFactory {
  private final SessionClientTransport transport;

  public SessionClientTransportFactory(Session session) {
    transport = new SessionClientTransport(session);
  }

  @Override
  public ClientTransport newClientTransport() {
    return transport;
  }
}
