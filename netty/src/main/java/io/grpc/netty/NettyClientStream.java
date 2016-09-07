/*
 * Copyright 2015, Google Inc. All rights reserved.
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

import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static io.netty.buffer.Unpooled.EMPTY_BUFFER;

import io.grpc.InternalKnownTransport;
import io.grpc.InternalMethodDescriptor;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.internal.ClientStreamListener;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.Http2ClientStream;
import io.grpc.internal.WritableBuffer;
import io.netty.buffer.ByteBuf;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2Stream;
import io.netty.util.AsciiString;

import javax.annotation.Nullable;

/**
 * Client stream for a Netty transport.
 */
abstract class NettyClientStream extends Http2ClientStream implements StreamIdHolder {

  private static final InternalMethodDescriptor methodDescriptorAccessor =
      new InternalMethodDescriptor(InternalKnownTransport.NETTY);

  private final MethodDescriptor<?, ?> method;
  /** {@code null} after start. */
  private Metadata headers;
  private final Channel channel;
  private final NettyClientHandler handler;
  private final AsciiString scheme;
  private final AsciiString userAgent;
  private AsciiString authority;

  private Http2Stream http2Stream;
  private Integer id;
  private WriteQueue writeQueue;

  NettyClientStream(MethodDescriptor<?, ?> method, Metadata headers, Channel channel,
      NettyClientHandler handler, int maxMessageSize, AsciiString authority, AsciiString scheme,
      AsciiString userAgent) {
    super(new NettyWritableBufferAllocator(channel.alloc()), maxMessageSize);
    this.method = checkNotNull(method, "method");
    this.headers = checkNotNull(headers, "headers");
    this.writeQueue = handler.getWriteQueue();
    this.channel = checkNotNull(channel, "channel");
    this.handler = checkNotNull(handler, "handler");
    this.authority = checkNotNull(authority, "authority");
    this.scheme = checkNotNull(scheme, "scheme");
    this.userAgent = userAgent;
  }

  @Override
  public void setAuthority(String authority) {
    checkState(listener() == null, "must be call before start");
    this.authority = AsciiString.of(checkNotNull(authority, "authority"));
  }

  @Override
  public void start(ClientStreamListener listener) {
    super.start(listener);

    // Convert the headers into Netty HTTP/2 headers.
    AsciiString defaultPath = (AsciiString) methodDescriptorAccessor.geRawMethodName(method);
    if (defaultPath == null) {
      defaultPath = new AsciiString("/" + method.getFullMethodName());
      methodDescriptorAccessor.setRawMethodName(method, defaultPath);
    }
    headers.discardAll(GrpcUtil.USER_AGENT_KEY);
    Http2Headers http2Headers
        = Utils.convertClientHeaders(headers, scheme, defaultPath, authority, userAgent);
    headers = null;

    ChannelFutureListener failureListener = new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        if (!future.isSuccess()) {
          // Stream creation failed. Close the stream if not already closed.
          Status s = statusFromFailedFuture(future);
          transportReportStatus(s, true, new Metadata());
        }
      }
    };

    // Write the command requesting the creation of the stream.
    writeQueue.enqueue(new CreateStreamCommand(http2Headers, this),
        !method.getType().clientSendsOneMessage()).addListener(failureListener);
  }

  @Override
  public void transportReportStatus(Status newStatus, boolean stopDelivery, Metadata trailers) {
    super.transportReportStatus(newStatus, stopDelivery, trailers);
  }

  /**
   * Intended to be overriden by NettyClientTransport, which has more information about failures.
   * May only be called from event loop.
   */
  protected abstract Status statusFromFailedFuture(ChannelFuture f);

  @Override
  public void request(int numMessages) {
    if (channel.eventLoop().inEventLoop()) {
      // Processing data read in the event loop so can call into the deframer immediately
      requestMessagesFromDeframer(numMessages);
    } else {
      writeQueue.enqueue(new RequestMessagesCommand(this, numMessages), true);
    }
  }

  @Override
  public Integer id() {
    return id;
  }

  public void id(int id) {
    this.id = id;
  }

  /**
   * Sets the underlying Netty {@link Http2Stream} for this stream. This must be called in the
   * context of the transport thread.
   */
  public void setHttp2Stream(Http2Stream http2Stream) {
    checkNotNull(http2Stream, "http2Stream");
    checkState(this.http2Stream == null, "Can only set http2Stream once");
    this.http2Stream = http2Stream;

    // Now that the stream has actually been initialized, call the listener's onReady callback if
    // appropriate.
    onStreamAllocated();
  }

  /**
   * Gets the underlying Netty {@link Http2Stream} for this stream.
   */
  @Nullable
  public Http2Stream http2Stream() {
    return http2Stream;
  }

  void transportHeadersReceived(Http2Headers headers, boolean endOfStream) {
    if (endOfStream) {
      transportTrailersReceived(Utils.convertTrailers(headers));
    } else {
      transportHeadersReceived(Utils.convertHeaders(headers));
    }
  }

  void transportDataReceived(ByteBuf frame, boolean endOfStream) {
    transportDataReceived(new NettyReadableBuffer(frame.retain()), endOfStream);
  }

  @Override
  protected void sendCancel(Status reason) {
    // Send the cancel command to the handler.
    writeQueue.enqueue(new CancelClientStreamCommand(this, reason), true);
  }

  @Override
  protected void sendFrame(WritableBuffer frame, boolean endOfStream, boolean flush) {
    ByteBuf bytebuf = frame == null ? EMPTY_BUFFER : ((NettyWritableBuffer) frame).bytebuf();
    final int numBytes = bytebuf.readableBytes();
    if (numBytes > 0) {
      // Add the bytes to outbound flow control.
      onSendingBytes(numBytes);
      writeQueue.enqueue(
          new SendGrpcFrameCommand(this, bytebuf, endOfStream),
          channel.newPromise().addListener(new ChannelFutureListener() {
            @Override
            public void operationComplete(ChannelFuture future) throws Exception {
              // Remove the bytes from outbound flow control, optionally notifying
              // the client that they can send more bytes.
              onSentBytes(numBytes);
            }
          }), flush);
    } else {
      // The frame is empty and will not impact outbound flow control. Just send it.
      writeQueue.enqueue(new SendGrpcFrameCommand(this, bytebuf, endOfStream), flush);
    }
  }

  @Override
  protected void returnProcessedBytes(int processedBytes) {
    handler.returnProcessedBytes(http2Stream, processedBytes);
    writeQueue.scheduleFlush();
  }
}
