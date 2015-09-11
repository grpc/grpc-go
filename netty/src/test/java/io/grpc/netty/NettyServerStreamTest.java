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

import static io.grpc.internal.GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE;
import static io.grpc.netty.NettyTestUtil.messageFrame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyBoolean;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.atLeastOnce;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.reset;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.verifyZeroInteractions;
import static org.mockito.Mockito.when;

import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.internal.ServerStreamListener;

import io.netty.buffer.EmptyByteBuf;
import io.netty.buffer.UnpooledByteBufAllocator;
import io.netty.channel.ChannelPromise;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.util.AsciiString;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

import java.io.ByteArrayInputStream;

/** Unit tests for {@link NettyServerStream}. */
@RunWith(JUnit4.class)
public class NettyServerStreamTest extends NettyStreamTestBase<NettyServerStream> {
  @Mock
  protected ServerStreamListener serverListener;

  @Mock
  private NettyServerHandler handler;

  private Metadata trailers = new Metadata();

  @Before
  @Override
  public void setUp() {
    super.setUp();

    // Verify onReady notification and then reset it.
    verify(listener()).onReady();
    reset(listener());
  }

  @Test
  public void writeMessageShouldSendResponse() throws Exception {
    stream.writeHeaders(new Metadata());
    Http2Headers headers = new DefaultHttp2Headers()
        .status(Utils.STATUS_OK)
        .set(Utils.CONTENT_TYPE_HEADER, Utils.CONTENT_TYPE_GRPC);
    verify(writeQueue).enqueue(new SendResponseHeadersCommand(STREAM_ID, headers, false), true);
    byte[] msg = smallMessage();
    stream.writeMessage(new ByteArrayInputStream(msg));
    stream.flush();
    verify(writeQueue).enqueue(eq(new SendGrpcFrameCommand(stream, messageFrame(MESSAGE), false)),
        any(ChannelPromise.class),
        eq(true));
  }

  @Test
  public void writeHeadersShouldSendHeaders() throws Exception {
    Metadata headers = new Metadata();
    stream().writeHeaders(headers);
    verify(writeQueue).enqueue(new SendResponseHeadersCommand(STREAM_ID,
        Utils.convertServerHeaders(headers), false), true);
  }

  @Test
  public void duplicateWriteHeadersShouldFail() throws Exception {
    Metadata headers = new Metadata();
    stream().writeHeaders(headers);
    verify(writeQueue).enqueue(new SendResponseHeadersCommand(STREAM_ID,
        Utils.convertServerHeaders(headers), false), true);
    try {
      stream().writeHeaders(headers);
      fail("Can only write response headers once");
    } catch (IllegalStateException ise) {
      // Success
    }
  }

  @Test
  public void closeBeforeClientHalfCloseShouldSucceed() throws Exception {
    stream().close(Status.OK, new Metadata());
    verify(writeQueue).enqueue(
        new SendResponseHeadersCommand(STREAM_ID, new DefaultHttp2Headers()
            .status(new AsciiString("200"))
            .set(new AsciiString("content-type"), new AsciiString("application/grpc"))
            .set(new AsciiString("grpc-status"), new AsciiString("0")), true),
        true);
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
    verify(writeQueue).enqueue(
        new SendResponseHeadersCommand(STREAM_ID, new DefaultHttp2Headers()
            .status(new AsciiString("200"))
            .set(new AsciiString("content-type"), new AsciiString("application/grpc"))
            .set(new AsciiString("grpc-status"), new AsciiString("1")), true),
        true);
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
    verify(writeQueue).enqueue(
        new SendResponseHeadersCommand(STREAM_ID, new DefaultHttp2Headers()
            .status(new AsciiString("200"))
            .set(new AsciiString("content-type"), new AsciiString("application/grpc"))
            .set(new AsciiString("grpc-status"), new AsciiString("0")), true),
        true);
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
    verify(writeQueue).enqueue(
        new SendResponseHeadersCommand(STREAM_ID, new DefaultHttp2Headers()
            .status(new AsciiString("200"))
            .set(new AsciiString("content-type"), new AsciiString("application/grpc"))
            .set(new AsciiString("grpc-status"), new AsciiString("" + status.getCode().value())),
            true),
        true);
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

  @Test
  public void emptyFramerShouldSendNoPayload() throws Exception {
    stream().close(Status.OK, new Metadata());
    verify(writeQueue).enqueue(
        new SendResponseHeadersCommand(STREAM_ID, new DefaultHttp2Headers()
            .status(new AsciiString("200"))
            .set(new AsciiString("content-type"), new AsciiString("application/grpc"))
            .set(new AsciiString("grpc-status"), new AsciiString("0")), true),
        true);
  }

  @Test
  public void cancelStreamShouldSucceed() {
    stream().cancel(Status.DEADLINE_EXCEEDED);
    verify(writeQueue).enqueue(
        new CancelServerStreamCommand(stream(), Status.DEADLINE_EXCEEDED),
        true);
  }

  @Override
  protected NettyServerStream createStream() {
    when(handler.getWriteQueue()).thenReturn(writeQueue);
    doAnswer(new Answer<Object>() {
      @Override
      public Object answer(InvocationOnMock invocation) throws Throwable {
        if (future.isDone()) {
          ((ChannelPromise) invocation.getArguments()[1]).setSuccess();
        }
        return null;
      }
    }).when(writeQueue).enqueue(any(), any(ChannelPromise.class), anyBoolean());
    when(writeQueue.enqueue(any(), anyBoolean())).thenReturn(future);
    NettyServerStream stream = new NettyServerStream(channel, http2Stream, handler,
            DEFAULT_MAX_MESSAGE_SIZE);
    stream.setListener(serverListener);
    assertTrue(stream.canReceive());
    assertTrue(stream.canSend());
    verify(serverListener, atLeastOnce()).onReady();
    verifyNoMoreInteractions(serverListener);
    return stream;
  }

  @Override
  protected void sendHeadersIfServer() {
    stream.writeHeaders(new Metadata());
  }

  @Override
  protected void closeStream() {
    stream().close(Status.ABORTED, new Metadata());
  }

  @Override
  protected ServerStreamListener listener() {
    return serverListener;
  }

  private NettyServerStream stream() {
    return (NettyServerStream) stream;
  }
}
