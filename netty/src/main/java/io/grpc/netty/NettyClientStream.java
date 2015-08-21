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

import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static io.netty.buffer.Unpooled.EMPTY_BUFFER;

import io.grpc.Status;
import io.grpc.internal.ClientStreamListener;
import io.grpc.internal.Http2ClientStream;
import io.grpc.internal.WritableBuffer;
import io.netty.buffer.ByteBuf;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2Stream;

import javax.annotation.Nullable;

/**
 * Client stream for a Netty transport.
 */
class NettyClientStream extends Http2ClientStream {
  private final Channel channel;
  private final NettyClientHandler handler;
  private Http2Stream http2Stream;
  private Integer id;
  private WriteQueue writeQueue;

  NettyClientStream(ClientStreamListener listener, Channel channel, NettyClientHandler handler,
                    int maxMessageSize) {
    super(new NettyWritableBufferAllocator(channel.alloc()), listener, maxMessageSize);
    this.writeQueue = handler.getWriteQueue();
    this.channel = checkNotNull(channel, "channel");
    this.handler = checkNotNull(handler, "handler");
  }

  @Override
  public void request(final int numMessages) {
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
