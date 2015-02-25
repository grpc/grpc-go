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

import static io.grpc.transport.netty.NettyTestUtil.messageFrame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.verifyZeroInteractions;

import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.transport.AbstractStream;
import io.grpc.transport.ServerStreamListener;
import io.netty.buffer.EmptyByteBuf;
import io.netty.buffer.UnpooledByteBufAllocator;
import io.netty.handler.codec.AsciiString;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.Http2Headers;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;

/** Unit tests for {@link NettyServerStream}. */
@RunWith(JUnit4.class)
public class NettyServerStreamTest extends NettyStreamTestBase {
  @Mock
  protected ServerStreamListener serverListener;
  @Mock
  private NettyServerHandler handler;
  private Metadata.Trailers trailers = new Metadata.Trailers();

  @Test
  public void writeMessageShouldSendResponse() throws Exception {
    stream.writeMessage(input, input.available(), accepted);
    stream.flush();
    Http2Headers headers = new DefaultHttp2Headers()
        .status(Utils.STATUS_OK)
        .set(Utils.CONTENT_TYPE_HEADER, Utils.CONTENT_TYPE_GRPC);
    verify(channel).writeAndFlush(new SendResponseHeadersCommand(STREAM_ID, headers, false));
    verify(channel).writeAndFlush(new SendGrpcFrameCommand(stream, messageFrame(MESSAGE), false));
    verify(accepted).run();
  }

  @Test
  public void writeHeadersShouldSendHeaders() throws Exception {
    Metadata.Headers headers = new Metadata.Headers();
    stream().writeHeaders(headers);
    verify(channel).writeAndFlush(new SendResponseHeadersCommand(STREAM_ID,
        Utils.convertServerHeaders(headers), false));
  }

  @Test
  public void duplicateWriteHeadersShouldFail() throws Exception {
    Metadata.Headers headers = new Metadata.Headers();
    stream().writeHeaders(headers);
    verify(channel).writeAndFlush(new SendResponseHeadersCommand(STREAM_ID,
        Utils.convertServerHeaders(headers), false));
    try {
      stream().writeHeaders(headers);
      fail("Can only write response headers once");
    } catch (IllegalStateException ise) {
      // Success
    }
  }

  @Test
  public void closeBeforeClientHalfCloseShouldSucceed() throws Exception {
    stream().close(Status.OK, new Metadata.Trailers());
    verify(channel).writeAndFlush(
        new SendResponseHeadersCommand(STREAM_ID, new DefaultHttp2Headers()
          .status(new AsciiString("200"))
          .set(new AsciiString("content-type"), new AsciiString("application/grpc"))
          .set(new AsciiString("grpc-status"), new AsciiString("0")), true));
    verifyZeroInteractions(serverListener);
    // Sending complete. Listener gets closed()
    stream().complete();
    verify(serverListener).closed(Status.OK);
    assertTrue(stream().isClosed());
    verifyZeroInteractions(serverListener);
  }

  @Test
  public void closeWithErrorBeforeClientHalfCloseShouldSucceed() throws Exception {
    // Error is sent on wire and ends the stream
    stream().close(Status.CANCELLED, trailers);
    verify(channel).writeAndFlush(
        new SendResponseHeadersCommand(STREAM_ID, new DefaultHttp2Headers()
          .status(new AsciiString("200"))
          .set(new AsciiString("content-type"), new AsciiString("application/grpc"))
          .set(new AsciiString("grpc-status"), new AsciiString("1")), true));
    verifyZeroInteractions(serverListener);
    // Sending complete. Listener gets closed()
    stream().complete();
    verify(serverListener).closed(Status.OK);
    assertTrue(stream().isClosed());
    verifyZeroInteractions(serverListener);
  }

  @Test
  public void closeAfterClientHalfCloseShouldSucceed() throws Exception {
    // Client half-closes. Listener gets halfClosed()
    stream().inboundDataReceived(new EmptyByteBuf(UnpooledByteBufAllocator.DEFAULT), true);
    assertTrue(stream().canSend());
    verify(serverListener).halfClosed();
    // Server closes. Status sent
    stream().close(Status.OK, trailers);
    assertTrue(stream().isClosed());
    verifyNoMoreInteractions(serverListener);
    verify(channel).writeAndFlush(
        new SendResponseHeadersCommand(STREAM_ID, new DefaultHttp2Headers()
          .status(new AsciiString("200"))
          .set(new AsciiString("content-type"), new AsciiString("application/grpc"))
          .set(new AsciiString("grpc-status"), new AsciiString("0")), true));
    // Sending and receiving complete. Listener gets closed()
    stream().complete();
    verify(serverListener).closed(Status.OK);
    verifyNoMoreInteractions(serverListener);
  }

  @Test
  public void abortStreamAndSendStatus() throws Exception {
    Status status = Status.INTERNAL.withCause(new Throwable());
    stream().abortStream(status, true);
    assertTrue(stream().isClosed());
    verify(serverListener).closed(same(status));
    verify(channel).writeAndFlush(
        new SendResponseHeadersCommand(STREAM_ID, new DefaultHttp2Headers()
            .status(new AsciiString("200"))
            .set(new AsciiString("content-type"), new AsciiString("application/grpc"))
            .set(new AsciiString("grpc-status"), new AsciiString("" + status.getCode().value())),
          true));
    verifyNoMoreInteractions(serverListener);
  }

  @Test
  public void abortStreamAndNotSendStatus() throws Exception {
    Status status = Status.INTERNAL.withCause(new Throwable());
    stream().abortStream(status, false);
    assertTrue(stream().isClosed());
    verify(serverListener).closed(same(status));
    verify(channel, never()).writeAndFlush(any(SendResponseHeadersCommand.class));
    verify(channel, never()).writeAndFlush(any(SendGrpcFrameCommand.class));
    verifyNoMoreInteractions(serverListener);
  }

  @Test
  public void abortStreamAfterClientHalfCloseShouldCallClose() {
    Status status = Status.INTERNAL.withCause(new Throwable());
    // Client half-closes. Listener gets halfClosed()
    stream().inboundDataReceived(new EmptyByteBuf(UnpooledByteBufAllocator.DEFAULT), true);
    assertTrue(stream().canSend());
    verify(serverListener).halfClosed();
    // Abort from the transport layer
    stream().abortStream(status, true);
    verify(serverListener).closed(same(status));
    verifyNoMoreInteractions(serverListener);
    assertTrue(stream().isClosed());
  }

  @Override
  protected AbstractStream<Integer> createStream() {
    NettyServerStream stream = new NettyServerStream(channel, STREAM_ID, handler);
    stream.setListener(serverListener);
    assertTrue(stream.canReceive());
    assertTrue(stream.canSend());
    verifyZeroInteractions(serverListener);
    return stream;
  }

  @Override
  protected ServerStreamListener listener() {
    return serverListener;
  }

  private NettyServerStream stream() {
    return (NettyServerStream) stream;
  }
}
