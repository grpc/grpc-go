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

import static com.google.common.base.Preconditions.checkNotNull;

import io.grpc.Metadata;
import io.grpc.transport.AbstractServerStream;
import io.grpc.transport.WritableBuffer;
import io.netty.buffer.ByteBuf;
import io.netty.channel.Channel;
import io.netty.handler.codec.http2.Http2Headers;

/**
 * Server stream for a Netty HTTP2 transport
 */
class NettyServerStream extends AbstractServerStream<Integer> {

  private final Channel channel;
  private final NettyServerHandler handler;

  NettyServerStream(Channel channel, int id, NettyServerHandler handler) {
    super(new NettyWritableBufferAllocator(channel.alloc()), id);
    this.channel = checkNotNull(channel, "channel");
    this.handler = checkNotNull(handler, "handler");
  }

  void inboundDataReceived(ByteBuf frame, boolean endOfStream) {
    super.inboundDataReceived(new NettyReadableBuffer(frame.retain()), endOfStream);
  }

  @Override
  public void request(final int numMessages) {
    channel.eventLoop().execute(new Runnable() {
      @Override
      public void run() {
        requestMessagesFromDeframer(numMessages);
      }
    });
  }

  @Override
  protected void inboundDeliveryPaused() {
    // Do nothing.
  }

  @Override
  protected void internalSendHeaders(Metadata.Headers headers) {
    channel.writeAndFlush(new SendResponseHeadersCommand(id(),
        Utils.convertServerHeaders(headers), false));
  }

  @Override
  protected void sendFrame(WritableBuffer frame, boolean endOfStream) {
    ByteBuf bytebuf = ((NettyWritableBuffer) frame).bytebuf();
    channel.writeAndFlush(new SendGrpcFrameCommand(this, bytebuf, endOfStream));
  }

  @Override
  protected void sendTrailers(Metadata.Trailers trailers, boolean headersSent) {
    Http2Headers http2Trailers = Utils.convertTrailers(trailers, headersSent);
    channel.writeAndFlush(new SendResponseHeadersCommand(id(), http2Trailers, true));
  }

  @Override
  protected void returnProcessedBytes(int processedBytes) {
    handler.returnProcessedBytes(id(), processedBytes);
  }
}
