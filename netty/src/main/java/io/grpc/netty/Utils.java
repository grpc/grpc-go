/*
 * Copyright 2014 The gRPC Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package io.grpc.netty;

import static io.grpc.internal.GrpcUtil.CONTENT_TYPE_KEY;
import static io.grpc.internal.TransportFrameUtil.toHttp2Headers;
import static io.grpc.internal.TransportFrameUtil.toRawSerializedHeaders;
import static io.netty.channel.ChannelOption.SO_LINGER;
import static io.netty.channel.ChannelOption.SO_TIMEOUT;
import static io.netty.util.CharsetUtil.UTF_8;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import io.grpc.InternalChannelz;
import io.grpc.InternalMetadata;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.SharedResourceHolder.Resource;
import io.grpc.netty.GrpcHttp2HeadersUtils.GrpcHttp2InboundHeaders;
import io.grpc.netty.NettySocketSupport.NativeSocketOptions;
import io.netty.channel.Channel;
import io.netty.channel.ChannelConfig;
import io.netty.channel.ChannelOption;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.handler.codec.http2.Http2Exception;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.util.AsciiString;
import io.netty.util.concurrent.DefaultThreadFactory;
import java.io.IOException;
import java.nio.channels.ClosedChannelException;
import java.util.Map;
import java.util.Map.Entry;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.TimeUnit;

/**
 * Common utility methods.
 */
@VisibleForTesting
class Utils {

  public static final AsciiString STATUS_OK = AsciiString.of("200");
  public static final AsciiString HTTP_METHOD = AsciiString.of(GrpcUtil.HTTP_METHOD);
  public static final AsciiString HTTP_GET_METHOD = AsciiString.of("GET");
  public static final AsciiString HTTPS = AsciiString.of("https");
  public static final AsciiString HTTP = AsciiString.of("http");
  public static final AsciiString CONTENT_TYPE_HEADER = AsciiString.of(CONTENT_TYPE_KEY.name());
  public static final AsciiString CONTENT_TYPE_GRPC = AsciiString.of(GrpcUtil.CONTENT_TYPE_GRPC);
  public static final AsciiString TE_HEADER = AsciiString.of(GrpcUtil.TE_HEADER.name());
  public static final AsciiString TE_TRAILERS = AsciiString.of(GrpcUtil.TE_TRAILERS);
  public static final AsciiString USER_AGENT = AsciiString.of(GrpcUtil.USER_AGENT_KEY.name());

  public static final Resource<EventLoopGroup> DEFAULT_BOSS_EVENT_LOOP_GROUP =
      new DefaultEventLoopGroupResource(1, "grpc-default-boss-ELG");

  public static final Resource<EventLoopGroup> DEFAULT_WORKER_EVENT_LOOP_GROUP =
      new DefaultEventLoopGroupResource(0, "grpc-default-worker-ELG");

  @VisibleForTesting
  static boolean validateHeaders = false;

  public static Metadata convertHeaders(Http2Headers http2Headers) {
    if (http2Headers instanceof GrpcHttp2InboundHeaders) {
      GrpcHttp2InboundHeaders h = (GrpcHttp2InboundHeaders) http2Headers;
      return InternalMetadata.newMetadata(h.numHeaders(), h.namesAndValues());
    }
    return InternalMetadata.newMetadata(convertHeadersToArray(http2Headers));
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
    return toRawSerializedHeaders(headerValues);
  }

  private static byte[] bytes(CharSequence seq) {
    if (seq instanceof AsciiString) {
      // Fast path - sometimes copy.
      AsciiString str = (AsciiString) seq;
      return str.isEntireArrayUsed() ? str.array() : str.toByteArray();
    }
    // Slow path - copy.
    return seq.toString().getBytes(UTF_8);
  }

  public static Http2Headers convertClientHeaders(Metadata headers,
      AsciiString scheme,
      AsciiString defaultPath,
      AsciiString authority,
      AsciiString method,
      AsciiString userAgent) {
    Preconditions.checkNotNull(defaultPath, "defaultPath");
    Preconditions.checkNotNull(authority, "authority");
    Preconditions.checkNotNull(method, "method");

    // Discard any application supplied duplicates of the reserved headers
    headers.discardAll(CONTENT_TYPE_KEY);
    headers.discardAll(GrpcUtil.TE_HEADER);
    headers.discardAll(GrpcUtil.USER_AGENT_KEY);

    return GrpcHttp2OutboundHeaders.clientRequestHeaders(
        toHttp2Headers(headers),
        authority,
        defaultPath,
        method,
        scheme,
        userAgent);
  }

  public static Http2Headers convertServerHeaders(Metadata headers) {
    // Discard any application supplied duplicates of the reserved headers
    headers.discardAll(CONTENT_TYPE_KEY);
    headers.discardAll(GrpcUtil.TE_HEADER);
    headers.discardAll(GrpcUtil.USER_AGENT_KEY);

    return GrpcHttp2OutboundHeaders.serverResponseHeaders(toHttp2Headers(headers));
  }

  public static Metadata convertTrailers(Http2Headers http2Headers) {
    if (http2Headers instanceof GrpcHttp2InboundHeaders) {
      GrpcHttp2InboundHeaders h = (GrpcHttp2InboundHeaders) http2Headers;
      return InternalMetadata.newMetadata(h.numHeaders(), h.namesAndValues());
    }
    return InternalMetadata.newMetadata(convertHeadersToArray(http2Headers));
  }

  public static Http2Headers convertTrailers(Metadata trailers, boolean headersSent) {
    if (!headersSent) {
      return convertServerHeaders(trailers);
    }
    return GrpcHttp2OutboundHeaders.serverResponseTrailers(toHttp2Headers(trailers));
  }

  public static Status statusFromThrowable(Throwable t) {
    Status s = Status.fromThrowable(t);
    if (s.getCode() != Status.Code.UNKNOWN) {
      return s;
    }
    if (t instanceof ClosedChannelException) {
      // ClosedChannelException is used any time the Netty channel is closed. Proper error
      // processing requires remembering the error that occurred before this one and using it
      // instead.
      //
      // Netty uses an exception that has no stack trace, while we would never hope to show this to
      // users, if it happens having the extra information may provide a small hint of where to
      // look.
      ClosedChannelException extraT = new ClosedChannelException();
      extraT.initCause(t);
      return Status.UNKNOWN.withDescription("channel closed").withCause(extraT);
    }
    if (t instanceof IOException) {
      return Status.UNAVAILABLE.withDescription("io exception").withCause(t);
    }
    if (t instanceof Http2Exception) {
      return Status.INTERNAL.withDescription("http2 exception").withCause(t);
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
      ThreadFactory threadFactory = new DefaultThreadFactory(name, useDaemonThreads);
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

  static InternalChannelz.SocketOptions getSocketOptions(Channel channel) {
    ChannelConfig config = channel.config();
    InternalChannelz.SocketOptions.Builder b = new InternalChannelz.SocketOptions.Builder();

    // The API allows returning null but not sure if it can happen in practice.
    // Let's be paranoid and do null checking just in case.
    Integer lingerSeconds = config.getOption(SO_LINGER);
    if (lingerSeconds != null) {
      b.setSocketOptionLingerSeconds(lingerSeconds);
    }

    Integer timeoutMillis = config.getOption(SO_TIMEOUT);
    if (timeoutMillis != null) {
      // in java, SO_TIMEOUT only applies to receiving
      b.setSocketOptionTimeoutMillis(timeoutMillis);
    }

    for (Entry<ChannelOption<?>, Object> opt : config.getOptions().entrySet()) {
      ChannelOption<?> key = opt.getKey();
      // Constants are pooled, so there should only be one instance of each constant
      if (key.equals(SO_LINGER) || key.equals(SO_TIMEOUT)) {
        continue;
      }
      Object value = opt.getValue();
      // zpencer: Can a netty option be null?
      b.addOption(key.name(), String.valueOf(value));
    }

    NativeSocketOptions nativeOptions
        = NettySocketSupport.getNativeSocketOptions(channel);
    if (nativeOptions != null) {
      b.setTcpInfo(nativeOptions.tcpInfo); // may be null
      for (Entry<String, String> entry : nativeOptions.otherInfo.entrySet()) {
        b.addOption(entry.getKey(), entry.getValue());
      }
    }
    return b.build();
  }

  private Utils() {
    // Prevents instantiation
  }
}
