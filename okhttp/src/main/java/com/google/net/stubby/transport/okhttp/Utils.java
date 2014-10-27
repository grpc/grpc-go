package com.google.net.stubby.transport.okhttp;

import com.google.net.stubby.Metadata;

import com.squareup.okhttp.internal.spdy.Header;

import java.util.List;

/**
 * Common utility methods for OkHttp transport.
 */
public class Utils {
  public static Metadata.Headers convertHeaders(List<Header> http2Headers) {
    return new Metadata.Headers(convertHeadersToArray(http2Headers));
  }

  public static Metadata.Trailers convertTrailers(List<Header> http2Headers) {
    return new Metadata.Trailers(convertHeadersToArray(http2Headers));
  }

  private static byte[][] convertHeadersToArray(List<Header> http2Headers) {
    byte[][] headerValues = new byte[http2Headers.size() * 2][];
    int i = 0;
    for (Header header : http2Headers) {
      headerValues[i++] = header.name.toByteArray();
      headerValues[i++] = header.value.toByteArray();
    }
    return headerValues;
  }

  private Utils() {
    // Prevents instantiation
  }
}
