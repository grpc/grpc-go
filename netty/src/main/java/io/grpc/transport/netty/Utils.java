/*
 * Copyright 2014, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc.transport.netty;

import static io.netty.util.CharsetUtil.UTF_8;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.ThreadFactoryBuilder;

import io.grpc.Metadata;
import io.grpc.SharedResourceHolder.Resource;
import io.grpc.transport.HttpUtil;
import io.grpc.transport.TransportFrameUtil;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufAllocator;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.util.ByteString;
import io.netty.util.concurrent.Future;
import io.netty.util.concurrent.GenericFutureListener;

import java.nio.ByteBuffer;
import java.util.Map;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.TimeUnit;

/**
 * Common utility methods.
 */
class Utils {

  public static final ByteString STATUS_OK = new ByteString("200".getBytes(UTF_8));
  public static final ByteString HTTP_METHOD = new ByteString(HttpUtil.HTTP_METHOD.getBytes(UTF_8));
  public static final ByteString HTTPS = new ByteString("https".getBytes(UTF_8));
  public static final ByteString HTTP = new ByteString("http".getBytes(UTF_8));
  public static final ByteString CONTENT_TYPE_HEADER = new ByteString(HttpUtil.CONTENT_TYPE.name()
      .getBytes(UTF_8));
  public static final ByteString CONTENT_TYPE_GRPC = new ByteString(
      HttpUtil.CONTENT_TYPE_GRPC.getBytes(UTF_8));
  public static final ByteString TE_HEADER = new ByteString(HttpUtil.TE.name().getBytes(UTF_8));
  public static final ByteString TE_TRAILERS = new ByteString(HttpUtil.TE_TRAILERS.getBytes(UTF_8));

  public static final Resource<EventLoopGroup> DEFAULT_BOSS_EVENT_LOOP_GROUP =
      new DefaultEventLoopGroupResource(1, "grpc-default-boss-ELG");

  public static final Resource<EventLoopGroup> DEFAULT_WORKER_EVENT_LOOP_GROUP =
      new DefaultEventLoopGroupResource(0, "grpc-default-worker-ELG");

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
      // toString() here is safe since it doesn't use the default Charset.
      headers.setAuthority(http2Headers.authority().toString());
    }
    if (http2Headers.path() != null) {
      headers.setPath(http2Headers.path().toString());
    }
    return headers;
  }

  private static byte[][] convertHeadersToArray(Http2Headers http2Headers) {
    // The Netty AsciiString class is really just a wrapper around a byte[] and supports
    // arbitrary binary data, not just ASCII.
    byte[][] headerValues = new byte[http2Headers.size() * 2][];
    int i = 0;
    for (Map.Entry<ByteString, ByteString> entry : http2Headers) {
      headerValues[i++] = entry.getKey().array();
      headerValues[i++] = entry.getValue().array();
    }
    return TransportFrameUtil.toRawSerializedHeaders(headerValues);
  }

  public static Http2Headers convertClientHeaders(Metadata.Headers headers,
      ByteString scheme,
      ByteString defaultPath,
      ByteString defaultAuthority) {
    Preconditions.checkNotNull(defaultPath, "defaultPath");
    Preconditions.checkNotNull(defaultAuthority, "defaultAuthority");
    // Add any application-provided headers first.
    Http2Headers http2Headers = convertMetadata(headers);

    // Now set GRPC-specific default headers.
    http2Headers.authority(defaultAuthority)
        .path(defaultPath)
        .method(HTTP_METHOD)
        .scheme(scheme)
        .set(CONTENT_TYPE_HEADER, CONTENT_TYPE_GRPC)
        .set(TE_HEADER, TE_TRAILERS);

    // Override the default authority and path if provided by the headers.
    if (headers.getAuthority() != null) {
      http2Headers.authority(new ByteString(headers.getAuthority().getBytes(UTF_8)));
    }
    if (headers.getPath() != null) {
      http2Headers.path(new ByteString(headers.getPath().getBytes(UTF_8)));
    }

    return http2Headers;
  }

  public static Http2Headers convertServerHeaders(Metadata.Headers headers) {
    Http2Headers http2Headers = convertMetadata(headers);
    http2Headers.set(CONTENT_TYPE_HEADER, CONTENT_TYPE_GRPC);
    http2Headers.status(STATUS_OK);
    return http2Headers;
  }

  public static Metadata.Trailers convertTrailers(Http2Headers http2Headers) {
    return new Metadata.Trailers(convertHeadersToArray(http2Headers));
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
    byte[][] serializedHeaders = TransportFrameUtil.toHttp2Headers(headers);
    for (int i = 0; i < serializedHeaders.length; i += 2) {
      http2Headers.add(new ByteString(serializedHeaders[i], false),
          new ByteString(serializedHeaders[i + 1], false));
    }
    return http2Headers;
  }

  private static class DefaultEventLoopGroupResource implements Resource<EventLoopGroup> {
    private final String name;
    private final int numEventLoops;

    DefaultEventLoopGroupResource(int numEventLoops, String name) {
      this.name = name;
      this.numEventLoops = numEventLoops;
    }

    @Override
    public EventLoopGroup create() {
      // Use the executor based constructor so we can work with both Netty4 & Netty5.
      ThreadFactory threadFactory = new ThreadFactoryBuilder().setNameFormat(name + "-%d").build();
      int parallelism = numEventLoops == 0
          ? Runtime.getRuntime().availableProcessors() * 2 : numEventLoops;
      final ExecutorService executor = Executors.newFixedThreadPool(parallelism, threadFactory);
      NioEventLoopGroup nioEventLoopGroup = new NioEventLoopGroup(parallelism, executor);
      nioEventLoopGroup.terminationFuture().addListener(
          new GenericFutureListener<Future<Object>>() {
            @Override
            public void operationComplete(Future<Object> future) throws Exception {
              executor.shutdown();
            }
          });
      return nioEventLoopGroup;
    }

    @Override
    public void close(EventLoopGroup instance) {
      instance.shutdownGracefully(0, 0, TimeUnit.SECONDS);
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
