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

import static com.google.common.truth.Truth.assertThat;
import static io.grpc.internal.GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE;
import static io.grpc.netty.NettyTestUtil.messageFrame;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyBoolean;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.isA;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.atLeastOnce;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.reset;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.verifyZeroInteractions;
import static org.mockito.Mockito.when;

import com.google.common.collect.ImmutableListMultimap;
import com.google.common.collect.ListMultimap;
import io.grpc.Attributes;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.internal.ServerStreamListener;
import io.grpc.internal.StatsTraceContext;
import io.grpc.netty.WriteQueue.QueuedCommand;
import io.netty.buffer.EmptyByteBuf;
import io.netty.buffer.UnpooledByteBufAllocator;
import io.netty.channel.ChannelPromise;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.util.AsciiString;
import java.io.ByteArrayInputStream;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

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
    ListMultimap<CharSequence, CharSequence> expectedHeaders =
        ImmutableListMultimap.copyOf(new DefaultHttp2Headers()
            .status(Utils.STATUS_OK)
            .set(Utils.CONTENT_TYPE_HEADER, Utils.CONTENT_TYPE_GRPC));

    stream.writeHeaders(new Metadata());

    ArgumentCaptor<SendResponseHeadersCommand> sendHeadersCap =
        ArgumentCaptor.forClass(SendResponseHeadersCommand.class);
    verify(writeQueue).enqueue(sendHeadersCap.capture(), eq(true));
    SendResponseHeadersCommand sendHeaders = sendHeadersCap.getValue();
    assertThat(sendHeaders.stream()).isSameAs(stream.transportState());
    assertThat(ImmutableListMultimap.copyOf(sendHeaders.headers()))
        .containsExactlyEntriesIn(expectedHeaders);
    assertThat(sendHeaders.endOfStream()).isFalse();

    byte[] msg = smallMessage();
    stream.writeMessage(new ByteArrayInputStream(msg));
    stream.flush();

    verify(writeQueue).enqueue(
        eq(new SendGrpcFrameCommand(stream.transportState(), messageFrame(MESSAGE), false)),
        isA(ChannelPromise.class),
        eq(true));
  }

  @Test
  public void writeHeadersShouldSendHeaders() throws Exception {
    Metadata headers = new Metadata();
    ListMultimap<CharSequence, CharSequence> expectedHeaders =
        ImmutableListMultimap.copyOf(Utils.convertServerHeaders(headers));

    stream().writeHeaders(headers);

    ArgumentCaptor<SendResponseHeadersCommand> sendHeadersCap =
        ArgumentCaptor.forClass(SendResponseHeadersCommand.class);
    verify(writeQueue).enqueue(sendHeadersCap.capture(), eq(true));
    SendResponseHeadersCommand sendHeaders = sendHeadersCap.getValue();
    assertThat(sendHeaders.stream()).isSameAs(stream.transportState());
    assertThat(ImmutableListMultimap.copyOf(sendHeaders.headers()))
        .containsExactlyEntriesIn(expectedHeaders);
    assertThat(sendHeaders.endOfStream()).isFalse();
  }

  @Test
  public void closeBeforeClientHalfCloseShouldSucceed() throws Exception {
    ListMultimap<CharSequence, CharSequence> expectedHeaders =
        ImmutableListMultimap.copyOf(new DefaultHttp2Headers()
            .status(new AsciiString("200"))
            .set(new AsciiString("content-type"), new AsciiString("application/grpc"))
            .set(new AsciiString("grpc-status"), new AsciiString("0")));

    stream().close(Status.OK, new Metadata());

    ArgumentCaptor<SendResponseHeadersCommand> sendHeadersCap =
        ArgumentCaptor.forClass(SendResponseHeadersCommand.class);
    verify(writeQueue).enqueue(sendHeadersCap.capture(), eq(true));
    SendResponseHeadersCommand sendHeaders = sendHeadersCap.getValue();
    assertThat(sendHeaders.stream()).isSameAs(stream.transportState());
    assertThat(ImmutableListMultimap.copyOf(sendHeaders.headers()))
        .containsExactlyEntriesIn(expectedHeaders);
    assertThat(sendHeaders.endOfStream()).isTrue();
    verifyZeroInteractions(serverListener);

    // Sending complete. Listener gets closed()
    stream().transportState().complete();

    verify(serverListener).closed(Status.OK);
    verifyZeroInteractions(serverListener);
  }

  @Test
  public void closeWithErrorBeforeClientHalfCloseShouldSucceed() throws Exception {
    ListMultimap<CharSequence, CharSequence> expectedHeaders =
        ImmutableListMultimap.copyOf(new DefaultHttp2Headers()
            .status(new AsciiString("200"))
            .set(new AsciiString("content-type"), new AsciiString("application/grpc"))
            .set(new AsciiString("grpc-status"), new AsciiString("1")));

    // Error is sent on wire and ends the stream
    stream().close(Status.CANCELLED, trailers);

    ArgumentCaptor<SendResponseHeadersCommand> sendHeadersCap =
        ArgumentCaptor.forClass(SendResponseHeadersCommand.class);
    verify(writeQueue).enqueue(sendHeadersCap.capture(), eq(true));
    SendResponseHeadersCommand sendHeaders = sendHeadersCap.getValue();
    assertThat(sendHeaders.stream()).isSameAs(stream.transportState());
    assertThat(ImmutableListMultimap.copyOf(sendHeaders.headers()))
        .containsExactlyEntriesIn(expectedHeaders);
    assertThat(sendHeaders.endOfStream()).isTrue();
    verifyZeroInteractions(serverListener);

    // Sending complete. Listener gets closed()
    stream().transportState().complete();
    verify(serverListener).closed(Status.OK);
    verifyZeroInteractions(serverListener);
  }

  @Test
  public void closeAfterClientHalfCloseShouldSucceed() throws Exception {
    ListMultimap<CharSequence, CharSequence> expectedHeaders =
        ImmutableListMultimap.copyOf(new DefaultHttp2Headers()
            .status(new AsciiString("200"))
            .set(new AsciiString("content-type"), new AsciiString("application/grpc"))
            .set(new AsciiString("grpc-status"), new AsciiString("0")));

    // Client half-closes. Listener gets halfClosed()
    stream().transportState()
        .inboundDataReceived(new EmptyByteBuf(UnpooledByteBufAllocator.DEFAULT), true);

    verify(serverListener).halfClosed();

    // Server closes. Status sent
    stream().close(Status.OK, trailers);
    verifyNoMoreInteractions(serverListener);

    ArgumentCaptor<SendResponseHeadersCommand> cmdCap =
        ArgumentCaptor.forClass(SendResponseHeadersCommand.class);
    verify(writeQueue).enqueue(cmdCap.capture(), eq(true));
    SendResponseHeadersCommand cmd = cmdCap.getValue();
    assertThat(cmd.stream()).isSameAs(stream.transportState());
    assertThat(ImmutableListMultimap.copyOf(cmd.headers()))
        .containsExactlyEntriesIn(expectedHeaders);
    assertThat(cmd.endOfStream()).isTrue();

    // Sending and receiving complete. Listener gets closed()
    stream().transportState().complete();
    verify(serverListener).closed(Status.OK);
    verifyNoMoreInteractions(serverListener);
  }

  @Test
  public void abortStreamAndNotSendStatus() throws Exception {
    Status status = Status.INTERNAL.withCause(new Throwable());
    stream().transportState().transportReportStatus(status);
    verify(serverListener).closed(same(status));
    verify(channel, never()).writeAndFlush(any(SendResponseHeadersCommand.class));
    verify(channel, never()).writeAndFlush(any(SendGrpcFrameCommand.class));
    verifyNoMoreInteractions(serverListener);
  }

  @Test
  public void abortStreamAfterClientHalfCloseShouldCallClose() {
    Status status = Status.INTERNAL.withCause(new Throwable());
    // Client half-closes. Listener gets halfClosed()
    stream().transportState().inboundDataReceived(
        new EmptyByteBuf(UnpooledByteBufAllocator.DEFAULT), true);
    verify(serverListener).halfClosed();
    // Abort from the transport layer
    stream().transportState().transportReportStatus(status);
    verify(serverListener).closed(same(status));
    verifyNoMoreInteractions(serverListener);
  }

  @Test
  public void emptyFramerShouldSendNoPayload() {
    ListMultimap<CharSequence, CharSequence> expectedHeaders =
        ImmutableListMultimap.copyOf(new DefaultHttp2Headers()
            .status(new AsciiString("200"))
            .set(new AsciiString("content-type"), new AsciiString("application/grpc"))
            .set(new AsciiString("grpc-status"), new AsciiString("0")));
    ArgumentCaptor<SendResponseHeadersCommand> cmdCap =
        ArgumentCaptor.forClass(SendResponseHeadersCommand.class);

    stream().close(Status.OK, new Metadata());

    verify(writeQueue).enqueue(cmdCap.capture(), eq(true));
    SendResponseHeadersCommand cmd = cmdCap.getValue();
    assertThat(cmd.stream()).isSameAs(stream.transportState());
    assertThat(ImmutableListMultimap.copyOf(cmd.headers()))
        .containsExactlyEntriesIn(expectedHeaders);
    assertThat(cmd.endOfStream()).isTrue();
  }

  @Test
  public void cancelStreamShouldSucceed() {
    stream().cancel(Status.DEADLINE_EXCEEDED);
    verify(writeQueue).enqueue(
        new CancelServerStreamCommand(stream().transportState(), Status.DEADLINE_EXCEEDED),
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
    }).when(writeQueue).enqueue(any(QueuedCommand.class), any(ChannelPromise.class), anyBoolean());
    when(writeQueue.enqueue(any(QueuedCommand.class), anyBoolean())).thenReturn(future);
    StatsTraceContext statsTraceCtx = StatsTraceContext.NOOP;
    NettyServerStream.TransportState state = new NettyServerStream.TransportState(
        handler, http2Stream, DEFAULT_MAX_MESSAGE_SIZE, statsTraceCtx);
    NettyServerStream stream = new NettyServerStream(channel, state, Attributes.EMPTY,
        "test-authority", statsTraceCtx);
    stream.transportState().setListener(serverListener);
    state.onStreamAllocated();
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
    return stream;
  }
}
