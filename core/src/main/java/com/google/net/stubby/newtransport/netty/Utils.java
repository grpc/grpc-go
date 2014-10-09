package com.google.net.stubby.newtransport.netty;

import com.google.common.base.Preconditions;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.newtransport.HttpUtil;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufAllocator;
import io.netty.handler.codec.AsciiString;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.Http2Headers;

import java.nio.ByteBuffer;
import java.util.Map;

/**
 * Common utility methods.
 */
class Utils {

  public static final AsciiString STATUS_OK = new AsciiString("200");
  public static final AsciiString HTTP_METHOD = new AsciiString(HttpUtil.HTTP_METHOD);
  public static final AsciiString HTTPS = new AsciiString("https");
  public static final AsciiString HTTP = new AsciiString("http");
  public static final AsciiString CONTENT_TYPE_HEADER =
      new AsciiString(HttpUtil.CONTENT_TYPE_HEADER);
  public static final AsciiString CONTENT_TYPE_PROTORPC =
      new AsciiString(HttpUtil.CONTENT_TYPE_PROTORPC);
  public static final AsciiString GRPC_STATUS_HEADER = new AsciiString(HttpUtil.GRPC_STATUS_HEADER);

  /**
   * Copies the content of the given {@link ByteBuffer} to a new {@link ByteBuf} instance.
   */
  static ByteBuf toByteBuf(ByteBufAllocator alloc, ByteBuffer source) {
    ByteBuf buf = alloc.buffer(source.remaining());
    buf.writeBytes(source);
    return buf;
  }

  public static Metadata.Headers convertHeaders(Http2Headers http2Headers) {
    Metadata.Headers headers = new Metadata.Headers(convertHeadersToArray(http2Headers));
    if (http2Headers.authority() != null) {
      headers.setAuthority(http2Headers.authority().toString());
    }
    if (http2Headers.path() != null) {
      headers.setPath(http2Headers.path().toString());
    }
    return headers;
  }

  public static Metadata.Trailers convertTrailers(Http2Headers http2Headers) {
    return new Metadata.Trailers(convertHeadersToArray(http2Headers));
  }

  private static byte[][] convertHeadersToArray(Http2Headers http2Headers) {
    // The Netty AsciiString class is really just a wrapper around a byte[] and supports
    // arbitrary binary data, not just ASCII.
    byte[][] headerValues = new byte[http2Headers.size()*2][];
    int i = 0;
    for (Map.Entry<AsciiString, AsciiString> entry : http2Headers) {
      headerValues[i++] = entry.getKey().array();
      headerValues[i++] = entry.getValue().array();
    }
    return headerValues;
  }

  public static Http2Headers convertClientHeaders(Metadata.Headers headers,
      boolean ssl,
      AsciiString defaultPath,
      AsciiString defaultAuthority) {
    Preconditions.checkNotNull(defaultPath, "defaultPath");
    Preconditions.checkNotNull(defaultAuthority, "defaultAuthority");
    // Add any application-provided headers first.
    Http2Headers http2Headers = convertMetadata(headers);

    // Now set GRPC-specific default headers.
    http2Headers.authority(defaultAuthority)
        .path(defaultPath)
        .method(HTTP_METHOD)
        .scheme(ssl ? HTTPS : HTTP)
        .set(CONTENT_TYPE_HEADER, CONTENT_TYPE_PROTORPC);

    // Override the default authority and path if provided by the headers.
    if (headers.getAuthority() != null) {
      http2Headers.authority(new AsciiString(headers.getAuthority()));
    }
    if (headers.getPath() != null) {
      http2Headers.path(new AsciiString(headers.getPath()));
    }

    return http2Headers;
  }

  public static Http2Headers convertServerHeaders(Metadata.Headers headers) {
    return convertMetadata(headers);
  }

  public static Http2Headers convertTrailers(Metadata.Trailers trailers) {
    return convertMetadata(trailers);
  }

  private static Http2Headers convertMetadata(Metadata headers) {
    Preconditions.checkNotNull(headers, "headers");
    Http2Headers http2Headers = new DefaultHttp2Headers();
    byte[][] serializedHeaders = headers.serialize();
    for (int i = 0; i < serializedHeaders.length; i++) {
      http2Headers.add(new AsciiString(serializedHeaders[i], false),
          new AsciiString(serializedHeaders[++i], false));
    }
    return http2Headers;
  }

  private Utils() {
    // Prevents instantiation
  }
}
