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

package io.grpc.netty;

import static io.grpc.internal.GrpcUtil.CONTENT_TYPE_KEY;
import static io.grpc.internal.GrpcUtil.USER_AGENT_KEY;
import static io.netty.util.CharsetUtil.UTF_8;

import com.google.common.base.Preconditions;

import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.SharedResourceHolder.Resource;
import io.grpc.internal.TransportFrameUtil;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.Http2Exception;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.util.AsciiString;
import io.netty.util.AttributeKey;
import io.netty.util.concurrent.DefaultThreadFactory;
import io.netty.util.concurrent.FastThreadLocalThread;

import java.io.IOException;
import java.nio.channels.ClosedChannelException;
import java.util.Map;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.TimeUnit;

import javax.net.ssl.SSLSession;

/**
 * Common utility methods.
 */
class Utils {

  public static final AsciiString STATUS_OK = AsciiString.of("200");
  public static final AsciiString HTTP_METHOD = AsciiString.of(GrpcUtil.HTTP_METHOD);
  public static final AsciiString HTTPS = AsciiString.of("https");
  public static final AsciiString HTTP = AsciiString.of("http");
  public static final AsciiString CONTENT_TYPE_HEADER = AsciiString.of(CONTENT_TYPE_KEY.name());
  public static final AsciiString CONTENT_TYPE_GRPC = AsciiString.of(GrpcUtil.CONTENT_TYPE_GRPC);
  public static final AsciiString TE_HEADER = AsciiString.of("te");
  public static final AsciiString TE_TRAILERS = AsciiString.of(GrpcUtil.TE_TRAILERS);
  public static final AsciiString USER_AGENT = AsciiString.of(USER_AGENT_KEY.name());

  public static final Resource<EventLoopGroup> DEFAULT_BOSS_EVENT_LOOP_GROUP =
      new DefaultEventLoopGroupResource(1, "grpc-default-boss-ELG");

  public static final Resource<EventLoopGroup> DEFAULT_WORKER_EVENT_LOOP_GROUP =
      new DefaultEventLoopGroupResource(0, "grpc-default-worker-ELG");

  public static final AttributeKey<SSLSession> SSL_SESSION_ATTR_KEY =
      AttributeKey.valueOf(SSLSession.class, "ssl-session");

  public static Metadata convertHeaders(Http2Headers http2Headers) {
    return new Metadata(convertHeadersToArray(http2Headers));
  }

  private static byte[][] convertHeadersToArray(Http2Headers http2Headers) {
    // The Netty AsciiString class is really just a wrapper around a byte[] and supports
    // arbitrary binary data, not just ASCII.
    byte[][] headerValues = new byte[http2Headers.size() * 2][];
    int i = 0;
    for (Map.Entry<CharSequence, CharSequence> entry : http2Headers) {
      headerValues[i++] = bytes(entry.getKey());
      headerValues[i++] = bytes(entry.getValue());
    }
    return TransportFrameUtil.toRawSerializedHeaders(headerValues);
  }

  private static byte[] bytes(CharSequence seq) {
    if (seq instanceof AsciiString) {
      // Fast path - no copy.
      return ((AsciiString) seq).array();
    }
    // Slow path - copy.
    return seq.toString().getBytes(UTF_8);
  }

  public static Http2Headers convertClientHeaders(Metadata headers,
      AsciiString scheme,
      AsciiString defaultPath,
      AsciiString authority) {
    Preconditions.checkNotNull(defaultPath, "defaultPath");
    Preconditions.checkNotNull(authority, "authority");
    // Add any application-provided headers first.
    Http2Headers http2Headers = convertMetadata(headers);

    // Now set GRPC-specific default headers.
    http2Headers.authority(authority)
        .path(defaultPath)
        .method(HTTP_METHOD)
        .scheme(scheme)
        .set(CONTENT_TYPE_HEADER, CONTENT_TYPE_GRPC)
        .set(TE_HEADER, TE_TRAILERS);

    // Set the User-Agent header.
    String userAgent = GrpcUtil.getGrpcUserAgent("netty", headers.get(USER_AGENT_KEY));
    http2Headers.set(USER_AGENT, new AsciiString(userAgent.getBytes(UTF_8)));

    return http2Headers;
  }

  public static Http2Headers convertServerHeaders(Metadata headers) {
    Http2Headers http2Headers = convertMetadata(headers);
    http2Headers.set(CONTENT_TYPE_HEADER, CONTENT_TYPE_GRPC);
    http2Headers.status(STATUS_OK);
    return http2Headers;
  }

  public static Metadata convertTrailers(Http2Headers http2Headers) {
    return new Metadata(convertHeadersToArray(http2Headers));
  }

  public static Http2Headers convertTrailers(Metadata trailers, boolean headersSent) {
    Http2Headers http2Trailers = convertMetadata(trailers);
    if (!headersSent) {
      http2Trailers.set(Utils.CONTENT_TYPE_HEADER, Utils.CONTENT_TYPE_GRPC);
      http2Trailers.status(STATUS_OK);
    }
    return http2Trailers;
  }

  private static Http2Headers convertMetadata(Metadata headers) {
    Preconditions.checkNotNull(headers, "headers");
    boolean validate = true;
    Http2Headers http2Headers = new DefaultHttp2Headers(validate, headers.headerCount());
    byte[][] serializedHeaders = TransportFrameUtil.toHttp2Headers(headers);
    for (int i = 0; i < serializedHeaders.length; i += 2) {
      AsciiString name = new AsciiString(serializedHeaders[i], false);
      AsciiString value = new AsciiString(serializedHeaders[i + 1], false);
      http2Headers.add(name, value);
    }

    return http2Headers;
  }

  public static Status statusFromThrowable(Throwable t) {
    Status s = Status.fromThrowable(t);
    if (s.getCode() != Status.Code.UNKNOWN) {
      return s;
    }
    // TODO(ejona): reenable once startup races are resolved; ClosedChannelException is being seen
    // still. Some tests are asserting UNAVAILABLE and were "working" previously but are now
    // detecting that our behavior is flaky. See #1330
    if (false && t instanceof ClosedChannelException) {
      // ClosedChannelException is used any time the Netty channel is closed. Proper error
      // processing requires remembering the error that occurred before this one and using it
      // instead.
      return Status.UNKNOWN.withCause(t);
    }
    if (t instanceof IOException) {
      return Status.UNAVAILABLE.withCause(t);
    }
    if (t instanceof Http2Exception) {
      return Status.INTERNAL.withCause(t);
    }
    return s;
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
      // Use Netty's DefaultThreadFactory in order to get the benefit of FastThreadLocal.
      boolean useDaemonThreads = true;
      ThreadFactory threadFactory = new ThreadGroupSavingDefaultThreadFactory(
          name, useDaemonThreads);
      int parallelism = numEventLoops == 0
          ? Runtime.getRuntime().availableProcessors() * 2 : numEventLoops;
      return new NioEventLoopGroup(parallelism, threadFactory);
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

  // A workaround for https://github.com/netty/netty/issues/5084
  // TODO(zhangkun83): remove it once the issue has been resolved in netty.
  private static class ThreadGroupSavingDefaultThreadFactory extends DefaultThreadFactory {
    final ThreadGroup threadGroup;

    ThreadGroupSavingDefaultThreadFactory(String name, boolean daemon) {
      super(name, daemon);
      threadGroup = Thread.currentThread().getThreadGroup();
    }

    @Override
    protected Thread newThread(Runnable r, String name) {
      return new FastThreadLocalThread(threadGroup, r, name);
    }
  }

  private Utils() {
    // Prevents instantiation
  }
}
