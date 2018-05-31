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

import static com.google.common.truth.Truth.assertThat;
import static io.grpc.internal.GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE;
import static io.grpc.netty.NettyTestUtil.messageFrame;
import static org.junit.Assert.assertNull;
import static org.mockito.Matchers.any;
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

import com.google.common.collect.ImmutableListMultimap;
import com.google.common.collect.ListMultimap;
import io.grpc.Attributes;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.internal.ServerStreamListener;
import io.grpc.internal.StatsTraceContext;
import io.grpc.internal.StreamListener;
import io.grpc.internal.TransportTracer;
import io.netty.buffer.EmptyByteBuf;
import io.netty.buffer.UnpooledByteBufAllocator;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.util.AsciiString;
import java.io.ByteArrayInputStream;
import java.io.InputStream;
import java.util.LinkedList;
import java.util.Queue;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Matchers;
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
  private final Queue<InputStream> listenerMessageQueue = new LinkedList<InputStream>();

  @Before
  @Override
  public void setUp() {
    super.setUp();

    // Verify onReady notification and then reset it.
    verify(listener()).onReady();
    reset(listener());

    doAnswer(
          new Answer<Void>() {
            @Override
            public Void answer(InvocationOnMock invocation) throws Throwable {
              StreamListener.MessageProducer producer =
                  (StreamListener.MessageProducer) invocation.getArguments()[0];
              InputStream message;
              while ((message = producer.next()) != null) {
                listenerMessageQueue.add(message);
              }
              return null;
            }
          })
      .when(serverListener)
      .messagesAvailable(Matchers.<StreamListener.MessageProducer>any());
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
    assertNull("no message expected", listenerMessageQueue.poll());
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
    assertNull("no message expected", listenerMessageQueue.poll());
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
    assertNull("no message expected", listenerMessageQueue.poll());

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
    assertNull("no message expected", listenerMessageQueue.poll());
  }

  @Test
  public void abortStreamAndNotSendStatus() throws Exception {
    Status status = Status.INTERNAL.withCause(new Throwable());
    stream().transportState().transportReportStatus(status);
    verify(serverListener).closed(same(status));
    verify(channel, never()).writeAndFlush(any(SendResponseHeadersCommand.class));
    verify(channel, never()).writeAndFlush(any(SendGrpcFrameCommand.class));
    assertNull("no message expected", listenerMessageQueue.poll());
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
    assertNull("no message expected", listenerMessageQueue.poll());
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
    StatsTraceContext statsTraceCtx = StatsTraceContext.NOOP;
    TransportTracer transportTracer = new TransportTracer();
    NettyServerStream.TransportState state = new NettyServerStream.TransportState(
        handler, channel.eventLoop(), http2Stream, DEFAULT_MAX_MESSAGE_SIZE, statsTraceCtx,
        transportTracer);
    NettyServerStream stream = new NettyServerStream(channel, state, Attributes.EMPTY,
        "test-authority", statsTraceCtx, transportTracer);
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

  @Override
  protected Queue<InputStream> listenerMessageQueue() {
    return listenerMessageQueue;
  }

  private NettyServerStream stream() {
    return stream;
  }
}
