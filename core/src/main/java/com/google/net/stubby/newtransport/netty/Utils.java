package com.google.net.stubby.newtransport.netty;

import com.google.common.collect.ImmutableMap;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufAllocator;
import io.netty.handler.codec.http2.Http2Headers;

import java.nio.ByteBuffer;
import java.util.Map;

import javax.inject.Provider;

/**
 * Common utility methods.
 */
class Utils {

  /**
   * Copies the content of the given {@link ByteBuffer} to a new {@link ByteBuf} instance.
   */
  static ByteBuf toByteBuf(ByteBufAllocator alloc, ByteBuffer source) {
    ByteBuf buf = alloc.buffer(source.remaining());
    buf.writeBytes(source);
    return buf;
  }

  public static ImmutableMap<String, Provider<String>> convertHeaders(Http2Headers headers) {
    ImmutableMap.Builder<String, Provider<String>> grpcHeaders =
        new ImmutableMap.Builder<String, Provider<String>>();
    for (Map.Entry<String, String> header : headers) {
      if (!header.getKey().startsWith(":")) {
        final String value = header.getValue();
        // headers starting with ":" are reserved for HTTP/2 built-in headers
        grpcHeaders.put(header.getKey(), new Provider<String>() {
          @Override
          public String get() {
            return value;
          }
        });
      }
    }
    return grpcHeaders.build();
  }

  private Utils() {
    // Prevents instantiation
  }
}
