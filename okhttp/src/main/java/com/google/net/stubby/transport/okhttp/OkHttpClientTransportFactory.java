package com.google.net.stubby.transport.okhttp;

import com.google.common.base.Preconditions;
import com.google.net.stubby.transport.ClientTransport;
import com.google.net.stubby.transport.ClientTransportFactory;

import java.net.InetSocketAddress;
import java.util.concurrent.ExecutorService;

import javax.net.ssl.SSLSocketFactory;

/**
 * Factory that manufactures instances of {@link OkHttpClientTransport}.
 */
class OkHttpClientTransportFactory implements ClientTransportFactory {
  private final InetSocketAddress address;
  private final ExecutorService executor;
  private final String authorityHost;
  private final SSLSocketFactory sslSocketFactory;

  public OkHttpClientTransportFactory(InetSocketAddress address, String authorityHost,
                                      ExecutorService executor, SSLSocketFactory factory) {
    this.address = Preconditions.checkNotNull(address, "address");
    this.executor = Preconditions.checkNotNull(executor, "executor");
    this.authorityHost = Preconditions.checkNotNull(authorityHost, "authorityHost");
    this.sslSocketFactory = factory;
  }

  @Override
  public ClientTransport newClientTransport() {
    return new OkHttpClientTransport(address, authorityHost, executor, sslSocketFactory);
  }

}
