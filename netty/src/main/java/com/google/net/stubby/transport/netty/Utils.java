package com.google.net.stubby.transport.netty;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.ThreadFactoryBuilder;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.SharedResourceHolder.Resource;
import com.google.net.stubby.transport.HttpUtil;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufAllocator;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.handler.codec.AsciiString;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.util.concurrent.ExecutorServiceFactory;

import java.nio.ByteBuffer;
import java.util.Map;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/**
 * Common utility methods.
 */
class Utils {

  public static final AsciiString STATUS_OK = new AsciiString("200");
  public static final AsciiString HTTP_METHOD = new AsciiString(HttpUtil.HTTP_METHOD);
  public static final AsciiString HTTPS = new AsciiString("https");
  public static final AsciiString HTTP = new AsciiString("http");
  public static final AsciiString CONTENT_TYPE_HEADER =
      new AsciiString(HttpUtil.CONTENT_TYPE.name());
  public static final AsciiString CONTENT_TYPE_GRPC =
      new AsciiString(HttpUtil.CONTENT_TYPE_GRPC);
  public static final AsciiString TE_HEADER = new AsciiString(HttpUtil.TE.name());
  public static final AsciiString TE_TRAILERS = new AsciiString(HttpUtil.TE_TRAILERS);

  public static final Resource<EventLoopGroup> DEFAULT_CHANNEL_EVENT_LOOP_GROUP =
      new DefaultEventLoopGroupResource("grpc-default-channel-ELG");

  public static final Resource<EventLoopGroup> DEFAULT_BOSS_EVENT_LOOP_GROUP =
      new DefaultEventLoopGroupResource("grpc-default-boss-ELG");

  public static final Resource<EventLoopGroup> DEFAULT_WORKER_EVENT_LOOP_GROUP =
      new DefaultEventLoopGroupResource("grpc-default-worker-ELG");

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
        .set(CONTENT_TYPE_HEADER, CONTENT_TYPE_GRPC)
        .set(TE_HEADER, TE_TRAILERS);

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
    Http2Headers http2Headers = convertMetadata(headers);
    http2Headers.set(CONTENT_TYPE_HEADER, CONTENT_TYPE_GRPC);
    http2Headers.status(STATUS_OK);
    return http2Headers;
  }

  public static Http2Headers convertTrailers(Metadata.Trailers trailers, boolean headersSent) {
    Http2Headers http2Trailers = convertMetadata(trailers);
    if (!headersSent) {
      http2Trailers.set(Utils.CONTENT_TYPE_HEADER, Utils.CONTENT_TYPE_GRPC);
      http2Trailers.status(STATUS_OK);
    }
    return http2Trailers;
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

  private static class DefaultEventLoopGroupResource implements Resource<EventLoopGroup> {
    private final String name;

    DefaultEventLoopGroupResource(String name) {
      this.name = name;
    }

    @Override
    public EventLoopGroup create() {
      return new NioEventLoopGroup(0, new ExecutorServiceFactory() {
        @Override
        public ExecutorService newExecutorService(int parallelism) {
          return Executors.newFixedThreadPool(parallelism, new ThreadFactoryBuilder()
              .setNameFormat(name + "-%d").build());
        }
      });
    }

    @Override
    public void close(EventLoopGroup instance) {
      instance.shutdownGracefully();
    }

    @Override
    public String toString() {
      return name;
    }
  }

  private Utils() {
    // Prevents instantiation
  }
}
