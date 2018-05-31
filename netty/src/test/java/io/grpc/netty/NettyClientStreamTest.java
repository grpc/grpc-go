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
import static io.grpc.internal.ClientStreamListener.RpcProgress.PROCESSED;
import static io.grpc.internal.GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE;
import static io.grpc.netty.NettyTestUtil.messageFrame;
import static io.grpc.netty.Utils.CONTENT_TYPE_GRPC;
import static io.grpc.netty.Utils.CONTENT_TYPE_HEADER;
import static io.grpc.netty.Utils.STATUS_OK;
import static io.netty.util.CharsetUtil.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.isA;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.reset;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.collect.ImmutableListMultimap;
import com.google.common.io.BaseEncoding;
import io.grpc.InternalStatus;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.internal.ClientStreamListener;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.StatsTraceContext;
import io.grpc.internal.StreamListener;
import io.grpc.internal.TransportTracer;
import io.grpc.netty.WriteQueue.QueuedCommand;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.Unpooled;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelPromise;
import io.netty.channel.DefaultChannelPromise;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.util.AsciiString;
import java.io.BufferedInputStream;
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
import org.mockito.Mockito;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

/**
 * Tests for {@link NettyClientStream}.
 */
@RunWith(JUnit4.class)
public class NettyClientStreamTest extends NettyStreamTestBase<NettyClientStream> {
  @Mock
  protected ClientStreamListener listener;

  @Mock
  protected NettyClientHandler handler;

  @SuppressWarnings("unchecked")
  private MethodDescriptor.Marshaller<Void> marshaller = mock(MethodDescriptor.Marshaller.class);
  private final Queue<InputStream> listenerMessageQueue = new LinkedList<InputStream>();

  // Must be initialized before @Before, because it is used by createStream()
  private MethodDescriptor<?, ?> methodDescriptor = MethodDescriptor.<Void, Void>newBuilder()
      .setType(MethodDescriptor.MethodType.UNARY)
      .setFullMethodName("testService/test")
      .setRequestMarshaller(marshaller)
      .setResponseMarshaller(marshaller)
      .build();

  private final TransportTracer transportTracer = new TransportTracer();

  /** Set up for test. */
  @Before
  @Override
  public void setUp() {
    super.setUp();

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
      .when(listener)
      .messagesAvailable(Matchers.<StreamListener.MessageProducer>any());
  }

  @Override
  protected ClientStreamListener listener() {
    return listener;
  }

  @Override
  protected Queue<InputStream> listenerMessageQueue() {
    return listenerMessageQueue;
  }

  @Test
  public void closeShouldSucceed() {
    // Force stream creation.
    stream().transportState().setId(STREAM_ID);
    stream().halfClose();
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void cancelShouldSendCommand() {
    // Set stream id to indicate it has been created
    stream().transportState().setId(STREAM_ID);
    stream().cancel(Status.CANCELLED);
    ArgumentCaptor<CancelClientStreamCommand> commandCaptor =
        ArgumentCaptor.forClass(CancelClientStreamCommand.class);
    verify(writeQueue).enqueue(commandCaptor.capture(), eq(true));
    assertEquals(commandCaptor.getValue().reason(), Status.CANCELLED);
  }

  @Test
  public void deadlineExceededCancelShouldSendCommand() {
    // Set stream id to indicate it has been created
    stream().transportState().setId(STREAM_ID);
    stream().cancel(Status.DEADLINE_EXCEEDED);
    ArgumentCaptor<CancelClientStreamCommand> commandCaptor =
        ArgumentCaptor.forClass(CancelClientStreamCommand.class);
    verify(writeQueue).enqueue(commandCaptor.capture(), eq(true));
    assertEquals(commandCaptor.getValue().reason(), Status.DEADLINE_EXCEEDED);
  }

  @Test
  public void cancelShouldStillSendCommandIfStreamNotCreatedToCancelCreation() {
    stream().cancel(Status.CANCELLED);
    verify(writeQueue).enqueue(isA(CancelClientStreamCommand.class), eq(true));
  }

  @Test
  public void writeMessageShouldSendRequest() throws Exception {
    // Force stream creation.
    stream().transportState().setId(STREAM_ID);
    byte[] msg = smallMessage();
    stream.writeMessage(new ByteArrayInputStream(msg));
    stream.flush();
    verify(writeQueue).enqueue(
        eq(new SendGrpcFrameCommand(stream.transportState(), messageFrame(MESSAGE), false)),
        eq(true));
  }

  @Test
  public void writeMessageShouldSendRequestUnknownLength() throws Exception {
    // Force stream creation.
    stream().transportState().setId(STREAM_ID);
    byte[] msg = smallMessage();
    stream.writeMessage(new BufferedInputStream(new ByteArrayInputStream(msg)));
    stream.flush();
    // Two writes occur, one for the GRPC frame header and the second with the payload
    // The framer reports the message count when the payload is completely written
    verify(writeQueue).enqueue(
            eq(new SendGrpcFrameCommand(
                stream.transportState(), messageFrame(MESSAGE).slice(0, 5), false)),
            eq(false));
    verify(writeQueue).enqueue(
        eq(new SendGrpcFrameCommand(
            stream.transportState(), messageFrame(MESSAGE).slice(5, 11), false)),
        eq(true));
  }

  @Test
  public void setStatusWithOkShouldCloseStream() {
    stream().transportState().setId(STREAM_ID);
    stream().transportState().transportReportStatus(Status.OK, true, new Metadata());
    verify(listener).closed(same(Status.OK), same(PROCESSED), any(Metadata.class));
  }

  @Test
  public void setStatusWithErrorShouldCloseStream() {
    Status errorStatus = Status.INTERNAL;
    stream().transportState().transportReportStatus(errorStatus, true, new Metadata());
    verify(listener).closed(eq(errorStatus), same(PROCESSED), any(Metadata.class));
  }

  @Test
  public void setStatusWithOkShouldNotOverrideError() {
    Status errorStatus = Status.INTERNAL;
    stream().transportState().transportReportStatus(errorStatus, true, new Metadata());
    stream().transportState().transportReportStatus(Status.OK, true, new Metadata());
    verify(listener).closed(any(Status.class), same(PROCESSED), any(Metadata.class));
  }

  @Test
  public void setStatusWithErrorShouldNotOverridePreviousError() {
    Status errorStatus = Status.INTERNAL;
    stream().transportState().transportReportStatus(errorStatus, true, new Metadata());
    stream().transportState().transportReportStatus(
        Status.fromThrowable(new RuntimeException("fake")), true, new Metadata());
    verify(listener).closed(any(Status.class), same(PROCESSED), any(Metadata.class));
  }

  @Override
  @Test
  public void inboundMessageShouldCallListener() throws Exception {
    // Receive headers first so that it's a valid GRPC response.
    stream().transportState().setId(STREAM_ID);
    stream().transportState().transportHeadersReceived(grpcResponseHeaders(), false);
    super.inboundMessageShouldCallListener();
  }

  @Test
  public void inboundHeadersShouldCallListenerHeadersRead() throws Exception {
    stream().transportState().setId(STREAM_ID);
    Http2Headers headers = grpcResponseHeaders();
    stream().transportState().transportHeadersReceived(headers, false);
    verify(listener).headersRead(any(Metadata.class));
  }

  @Test
  public void inboundTrailersClosesCall() throws Exception {
    stream().transportState().setId(STREAM_ID);
    stream().transportState().transportHeadersReceived(grpcResponseHeaders(), false);
    super.inboundMessageShouldCallListener();
    stream().transportState().transportHeadersReceived(grpcResponseTrailers(Status.OK), true);
  }

  @Test
  public void inboundTrailersBeforeHalfCloseSendsRstStream() {
    stream().transportState().setId(STREAM_ID);
    stream().transportState().transportHeadersReceived(grpcResponseHeaders(), false);
    stream().transportState().transportHeadersReceived(grpcResponseTrailers(Status.OK), true);

    // Verify a cancel stream with reason=null is sent to the handler.
    ArgumentCaptor<CancelClientStreamCommand> captor = ArgumentCaptor
        .forClass(CancelClientStreamCommand.class);
    verify(writeQueue).enqueue(captor.capture(), eq(true));
    assertNull(captor.getValue().reason());
  }

  @Test
  public void inboundTrailersAfterHalfCloseDoesNotSendRstStream() {
    stream().transportState().setId(STREAM_ID);
    stream().transportState().transportHeadersReceived(grpcResponseHeaders(), false);
    stream.halfClose();
    stream().transportState().transportHeadersReceived(grpcResponseTrailers(Status.OK), true);
    verify(writeQueue, never()).enqueue(isA(CancelClientStreamCommand.class), eq(true));
  }

  @Test
  public void inboundStatusShouldSetStatus() throws Exception {
    stream().transportState().setId(STREAM_ID);

    // Receive headers first so that it's a valid GRPC response.
    stream().transportState().transportHeadersReceived(grpcResponseHeaders(), false);

    stream().transportState().transportHeadersReceived(grpcResponseTrailers(Status.INTERNAL), true);
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(listener).closed(captor.capture(), same(PROCESSED), any(Metadata.class));
    assertEquals(Status.INTERNAL.getCode(), captor.getValue().getCode());
  }

  @Test
  public void invalidInboundHeadersCancelStream() throws Exception {
    stream().transportState().setId(STREAM_ID);
    Http2Headers headers = grpcResponseHeaders();
    headers.set("random", "4");
    headers.remove(CONTENT_TYPE_HEADER);
    // Remove once b/16290036 is fixed.
    headers.status(new AsciiString("500"));
    stream().transportState().transportHeadersReceived(headers, false);
    verify(listener, never()).closed(any(Status.class), any(Metadata.class));

    // We are now waiting for 100 bytes of error context on the stream, cancel has not yet been
    // sent
    verify(channel, never()).writeAndFlush(any(CancelClientStreamCommand.class));
    stream().transportState().transportDataReceived(Unpooled.buffer(100).writeZero(100), false);
    verify(channel, never()).writeAndFlush(any(CancelClientStreamCommand.class));
    stream().transportState().transportDataReceived(Unpooled.buffer(1000).writeZero(1000), false);

    // Now verify that cancel is sent and an error is reported to the listener
    verify(writeQueue).enqueue(isA(CancelClientStreamCommand.class), eq(true));
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    ArgumentCaptor<Metadata> metadataCaptor = ArgumentCaptor.forClass(Metadata.class);
    verify(listener).closed(captor.capture(), same(PROCESSED), metadataCaptor.capture());
    assertEquals(Status.UNKNOWN.getCode(), captor.getValue().getCode());
    assertEquals("4", metadataCaptor.getValue()
        .get(Metadata.Key.of("random", Metadata.ASCII_STRING_MARSHALLER)));

  }

  @Test
  public void invalidInboundContentTypeShouldCancelStream() {
    // Set stream id to indicate it has been created
    stream().transportState().setId(STREAM_ID);
    Http2Headers headers = new DefaultHttp2Headers().status(STATUS_OK).set(CONTENT_TYPE_HEADER,
            new AsciiString("application/bad", UTF_8));
    stream().transportState().transportHeadersReceived(headers, false);
    Http2Headers trailers = new DefaultHttp2Headers()
        .set(new AsciiString("grpc-status", UTF_8), new AsciiString("0", UTF_8));
    stream().transportState().transportHeadersReceived(trailers, true);
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    ArgumentCaptor<Metadata> metadataCaptor = ArgumentCaptor.forClass(Metadata.class);
    verify(listener).closed(captor.capture(), same(PROCESSED), metadataCaptor.capture());
    Status status = captor.getValue();
    assertEquals(Status.Code.UNKNOWN, status.getCode());
    assertTrue(status.getDescription().contains("content-type"));
    assertEquals("application/bad", metadataCaptor.getValue()
        .get(Metadata.Key.of("Content-Type", Metadata.ASCII_STRING_MARSHALLER)));
  }

  @Test
  public void nonGrpcResponseShouldSetStatus() throws Exception {
    stream().transportState().transportDataReceived(Unpooled.copiedBuffer(MESSAGE, UTF_8), true);
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(listener).closed(captor.capture(), same(PROCESSED), any(Metadata.class));
    assertEquals(Status.Code.INTERNAL, captor.getValue().getCode());
  }

  @Test
  public void deframedDataAfterCancelShouldBeIgnored() throws Exception {
    stream().transportState().setId(STREAM_ID);
    // Receive headers first so that it's a valid GRPC response.
    stream().transportState().transportHeadersReceived(grpcResponseHeaders(), false);

    // Receive 2 consecutive empty frames. Only one is delivered at a time to the listener.
    stream().transportState().transportDataReceived(simpleGrpcFrame(), false);
    stream().transportState().transportDataReceived(simpleGrpcFrame(), false);

    // Only allow the first to be delivered.
    stream().request(1);

    // Receive error trailers. The server status will not be processed until after all of the
    // data frames have been processed. Since cancellation will interrupt message delivery,
    // this status will never be processed and the listener will instead only see the
    // cancellation.
    stream().transportState().transportHeadersReceived(grpcResponseTrailers(Status.INTERNAL), true);

    // Verify that the first was delivered.
    assertNotNull("message expected", listenerMessageQueue.poll());
    assertNull("no additional message expected", listenerMessageQueue.poll());

    // Now set the error status.
    Metadata trailers = Utils.convertTrailers(grpcResponseTrailers(Status.CANCELLED));
    stream().transportState().transportReportStatus(Status.CANCELLED, true, trailers);

    // Now allow the delivery of the second.
    stream().request(1);

    // Verify that the listener was only notified of the first message, not the second.
    assertNull("no additional message expected", listenerMessageQueue.poll());
    verify(listener).closed(eq(Status.CANCELLED), same(PROCESSED), eq(trailers));
  }

  @Test
  public void dataFrameWithEosShouldDeframeAndThenFail() {
    stream().transportState().setId(STREAM_ID);
    stream().request(1);

    // Receive headers first so that it's a valid GRPC response.
    stream().transportState().transportHeadersReceived(grpcResponseHeaders(), false);

    // Receive a DATA frame with EOS set.
    stream().transportState().transportDataReceived(simpleGrpcFrame(), true);

    // Verify that the message was delivered.
    assertNotNull("message expected", listenerMessageQueue.poll());
    assertNull("no additional message expected", listenerMessageQueue.poll());

    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(listener).closed(captor.capture(), same(PROCESSED), any(Metadata.class));
    assertEquals(Status.Code.INTERNAL, captor.getValue().getCode());
  }

  @Test
  public void setHttp2StreamShouldNotifyReady() {
    listener = mock(ClientStreamListener.class);

    stream = new NettyClientStream(new TransportStateImpl(handler, DEFAULT_MAX_MESSAGE_SIZE),
        methodDescriptor,
        new Metadata(),
        channel,
        AsciiString.of("localhost"),
        AsciiString.of("http"),
        AsciiString.of("agent"),
        StatsTraceContext.NOOP,
        transportTracer);
    stream.start(listener);
    stream().transportState().setId(STREAM_ID);
    verify(listener, never()).onReady();
    assertFalse(stream.isReady());
    stream().transportState().setHttp2Stream(http2Stream);
    verify(listener).onReady();
    assertTrue(stream.isReady());
  }

  @Test
  public void removeUserAgentFromApplicationHeaders() {
    Metadata metadata = new Metadata();
    metadata.put(GrpcUtil.USER_AGENT_KEY, "bad agent");
    listener = mock(ClientStreamListener.class);
    Mockito.reset(writeQueue);
    ChannelPromise completedPromise = new DefaultChannelPromise(channel)
        .setSuccess();
    when(writeQueue.enqueue(any(QueuedCommand.class), any(boolean.class)))
        .thenReturn(completedPromise);

    stream = new NettyClientStream(
        new TransportStateImpl(handler, DEFAULT_MAX_MESSAGE_SIZE),
        methodDescriptor,
        new Metadata(),
        channel,
        AsciiString.of("localhost"),
        AsciiString.of("http"),
        AsciiString.of("good agent"),
        StatsTraceContext.NOOP,
        transportTracer);
    stream.start(listener);

    ArgumentCaptor<CreateStreamCommand> cmdCap = ArgumentCaptor.forClass(CreateStreamCommand.class);
    verify(writeQueue).enqueue(cmdCap.capture(), eq(false));
    assertThat(ImmutableListMultimap.copyOf(cmdCap.getValue().headers()))
        .containsEntry(Utils.USER_AGENT, AsciiString.of("good agent"));
  }

  @Test
  public void getRequestSentThroughHeader() {
    // Creating a GET method
    MethodDescriptor<?, ?> descriptor = MethodDescriptor.<Void, Void>newBuilder()
        .setType(MethodDescriptor.MethodType.UNARY)
        .setFullMethodName("testService/test")
        .setRequestMarshaller(marshaller)
        .setResponseMarshaller(marshaller)
        .setIdempotent(true)
        .setSafe(true)
        .build();
    NettyClientStream stream = new NettyClientStream(
        new TransportStateImpl(handler, DEFAULT_MAX_MESSAGE_SIZE),
        descriptor,
        new Metadata(),
        channel,
        AsciiString.of("localhost"),
        AsciiString.of("http"),
        AsciiString.of("agent"),
        StatsTraceContext.NOOP,
        transportTracer);
    stream.start(listener);
    stream.transportState().setId(STREAM_ID);
    stream.transportState().setHttp2Stream(http2Stream);

    byte[] msg = smallMessage();
    stream.writeMessage(new ByteArrayInputStream(msg));
    stream.flush();
    stream.halfClose();
    ArgumentCaptor<CreateStreamCommand> cmdCap = ArgumentCaptor.forClass(CreateStreamCommand.class);
    verify(writeQueue).enqueue(cmdCap.capture(), eq(true));
    ImmutableListMultimap<CharSequence, CharSequence> headers =
        ImmutableListMultimap.copyOf(cmdCap.getValue().headers());
    assertThat(headers).containsEntry(AsciiString.of(":method"), Utils.HTTP_GET_METHOD);
    assertThat(headers)
        .containsEntry(
            AsciiString.of(":path"),
            AsciiString.of("/testService/test?" + BaseEncoding.base64().encode(msg)));
  }

  @Override
  protected NettyClientStream createStream() {
    when(handler.getWriteQueue()).thenReturn(writeQueue);
    NettyClientStream stream = new NettyClientStream(
        new TransportStateImpl(handler, DEFAULT_MAX_MESSAGE_SIZE),
        methodDescriptor,
        new Metadata(),
        channel,
        AsciiString.of("localhost"),
        AsciiString.of("http"),
        AsciiString.of("agent"),
        StatsTraceContext.NOOP,
        transportTracer);
    stream.start(listener);
    stream.transportState().setId(STREAM_ID);
    stream.transportState().setHttp2Stream(http2Stream);
    reset(listener);
    return stream;
  }

  @Override
  protected void sendHeadersIfServer() {}

  @Override
  protected void closeStream() {
    stream().cancel(Status.CANCELLED);
  }

  private ByteBuf simpleGrpcFrame() {
    return Unpooled.wrappedBuffer(new byte[] {0, 0, 0, 0, 2, 3, 14});
  }

  private NettyClientStream stream() {
    return stream;
  }

  private Http2Headers grpcResponseHeaders() {
    return new DefaultHttp2Headers()
        .status(STATUS_OK)
        .set(CONTENT_TYPE_HEADER, CONTENT_TYPE_GRPC);
  }

  private Http2Headers grpcResponseTrailers(Status status) {
    Metadata trailers = new Metadata();
    trailers.put(InternalStatus.CODE_KEY, status);
    return Utils.convertTrailers(trailers, true);
  }

  private class TransportStateImpl extends NettyClientStream.TransportState {
    public TransportStateImpl(NettyClientHandler handler, int maxMessageSize) {
      super(handler, channel.eventLoop(), maxMessageSize, StatsTraceContext.NOOP, transportTracer);
    }

    @Override
    protected Status statusFromFailedFuture(ChannelFuture f) {
      return Utils.statusFromThrowable(f.cause());
    }
  }
}
