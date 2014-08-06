package com.google.net.stubby.newtransport.okhttp;

import com.google.net.stubby.newtransport.ClientTransport;
import com.google.net.stubby.newtransport.ClientTransportFactory;

import java.util.concurrent.ExecutorService;

/**
 * Factory that manufactures instances of {@link OkHttpClientTransport}.
 */
public class OkHttpClientTransportFactory implements ClientTransportFactory {
  private final String host;
  private final int port;
  private final ExecutorService executor;

  public OkHttpClientTransportFactory(String host, int port, ExecutorService executor) {
    this.host = host;
    this.port = port;
    this.executor = executor;
  }

  @Override
  public ClientTransport newClientTransport() {
    return new OkHttpClientTransport(host, port, executor);
  }

}
