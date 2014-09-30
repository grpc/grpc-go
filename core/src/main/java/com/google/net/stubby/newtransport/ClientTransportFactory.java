package com.google.net.stubby.newtransport;

/** Pre-configured factory for creating {@link ClientTransport} instances. */
public interface ClientTransportFactory {
  /** Create an unstarted transport for exclusive use. */
  ClientTransport newClientTransport();
}
