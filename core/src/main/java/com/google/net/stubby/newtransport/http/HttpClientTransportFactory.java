package com.google.net.stubby.newtransport.http;

import com.google.net.stubby.newtransport.ClientTransportFactory;

import java.net.URI;
import java.net.URISyntaxException;

/**
 * Factory that manufactures instances of {@link HttpClientTransport}.
 */
public class HttpClientTransportFactory implements ClientTransportFactory {
  private final URI baseUri;

  public HttpClientTransportFactory(String host, int port, boolean ssl) {
    try {
      String scheme = ssl ? "https" : "http";
      baseUri = new URI(scheme, null, host, port, "/", null, null);
    } catch (URISyntaxException e) {
      throw new RuntimeException(e);
    }
  }

  @Override
  public HttpClientTransport newClientTransport() {
    return new HttpClientTransport(baseUri);
  }
}
