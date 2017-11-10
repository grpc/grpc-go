/*
 * Copyright 2014, gRPC Authors All rights reserved.
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

import static com.google.common.base.Charsets.UTF_8;
import static io.grpc.internal.GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE;
import static io.grpc.internal.GrpcUtil.DEFAULT_SERVER_KEEPALIVE_TIMEOUT_NANOS;
import static io.grpc.internal.GrpcUtil.DEFAULT_SERVER_KEEPALIVE_TIME_NANOS;
import static io.grpc.netty.NettyServerBuilder.MAX_CONNECTION_AGE_GRACE_NANOS_INFINITE;
import static io.grpc.netty.NettyServerBuilder.MAX_CONNECTION_AGE_NANOS_DISABLED;
import static io.grpc.netty.NettyServerBuilder.MAX_CONNECTION_IDLE_NANOS_DISABLED;
import static io.grpc.netty.Utils.CONTENT_TYPE_GRPC;
import static io.grpc.netty.Utils.CONTENT_TYPE_HEADER;
import static io.grpc.netty.Utils.HTTP_METHOD;
import static io.grpc.netty.Utils.TE_HEADER;
import static io.grpc.netty.Utils.TE_TRAILERS;
import static io.netty.buffer.Unpooled.directBuffer;
import static io.netty.handler.codec.http2.Http2CodecUtil.DEFAULT_WINDOW_SIZE;
import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.AdditionalAnswers.delegatesTo;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyString;
import static org.mockito.Matchers.eq;
import static org.mockito.Mockito.atLeastOnce;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.spy;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.io.ByteStreams;
import com.google.common.truth.Truth;
import io.grpc.Attributes;
import io.grpc.Metadata;
import io.grpc.ServerStreamTracer;
import io.grpc.Status;
import io.grpc.Status.Code;
import io.grpc.StreamTracer;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.KeepAliveManager;
import io.grpc.internal.ServerStream;
import io.grpc.internal.ServerStreamListener;
import io.grpc.internal.ServerTransportListener;
import io.grpc.internal.StatsTraceContext;
import io.grpc.internal.StreamListener;
import io.grpc.internal.TransportTracer;
import io.grpc.internal.testing.TestServerStreamTracer;
import io.grpc.netty.GrpcHttp2HeadersUtils.GrpcHttp2ServerHeadersDecoder;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufUtil;
import io.netty.buffer.Unpooled;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPromise;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.Http2CodecUtil;
import io.netty.handler.codec.http2.Http2Error;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2LocalFlowController;
import io.netty.handler.codec.http2.Http2Settings;
import io.netty.handler.codec.http2.Http2Stream;
import io.netty.util.AsciiString;
import java.io.InputStream;
import java.util.Arrays;
import java.util.LinkedList;
import java.util.List;
import java.util.Queue;
import java.util.concurrent.TimeUnit;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.Timeout;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Matchers;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

/**
 * Unit tests for {@link NettyServerHandler}.
 */
@RunWith(JUnit4.class)
public class NettyServerHandlerTest extends NettyHandlerTestBase<NettyServerHandler> {

  @Rule
  public final Timeout globalTimeout = Timeout.seconds(1);

  private static final int STREAM_ID = 3;

  @Mock
  private ServerStreamListener streamListener;

  @Mock
  private ServerStreamTracer.Factory streamTracerFactory;

  private final ServerTransportListener transportListener = spy(new ServerTransportListenerImpl());
  private final TestServerStreamTracer streamTracer = new TestServerStreamTracer();

  private NettyServerStream stream;
  private KeepAliveManager spyKeepAliveManager;

  final Queue<InputStream> streamListenerMessageQueue = new LinkedList<InputStream>();

  private int flowControlWindow = DEFAULT_WINDOW_SIZE;
  private int maxConcurrentStreams = Integer.MAX_VALUE;
  private int maxHeaderListSize = Integer.MAX_VALUE;
  private boolean permitKeepAliveWithoutCalls = true;
  private long permitKeepAliveTimeInNanos = 0;
  private long maxConnectionIdleInNanos = MAX_CONNECTION_IDLE_NANOS_DISABLED;
  private long maxConnectionAgeInNanos = MAX_CONNECTION_AGE_NANOS_DISABLED;
  private long maxConnectionAgeGraceInNanos = MAX_CONNECTION_AGE_GRACE_NANOS_INFINITE;
  private long keepAliveTimeInNanos = DEFAULT_SERVER_KEEPALIVE_TIME_NANOS;
  private long keepAliveTimeoutInNanos = DEFAULT_SERVER_KEEPALIVE_TIMEOUT_NANOS;
  private TransportTracer transportTracer;

  private class ServerTransportListenerImpl implements ServerTransportListener {

    @Override
    public void streamCreated(ServerStream stream, String method, Metadata headers) {
      stream.setListener(streamListener);
    }

    @Override
    public Attributes transportReady(Attributes attributes) {
      return Attributes.EMPTY;
    }

    @Override
    public void transportTerminated() {
    }
  }

  @Override
  protected void manualSetUp() throws Exception {
    assertNull("manualSetUp should not run more than once", handler());

    MockitoAnnotations.initMocks(this);
    transportTracer = new TransportTracer();
    when(streamTracerFactory.newServerStreamTracer(anyString(), any(Metadata.class)))
        .thenReturn(streamTracer);

    doAnswer(
          new Answer<Void>() {
            @Override
            public Void answer(InvocationOnMock invocation) throws Throwable {
              StreamListener.MessageProducer producer =
                  (StreamListener.MessageProducer) invocation.getArguments()[0];
              InputStream message;
              while ((message = producer.next()) != null) {
                streamListenerMessageQueue.add(message);
              }
              return null;
            }
          })
      .when(streamListener)
      .messagesAvailable(Matchers.<StreamListener.MessageProducer>any());

    initChannel(new GrpcHttp2ServerHeadersDecoder(GrpcUtil.DEFAULT_MAX_HEADER_LIST_SIZE));

    // replace the keepAliveManager with spyKeepAliveManager
    spyKeepAliveManager =
        mock(KeepAliveManager.class, delegatesTo(handler().getKeepAliveManagerForTest()));
    handler().setKeepAliveManagerForTest(spyKeepAliveManager);

    // Simulate receipt of the connection preface
    handler().handleProtocolNegotiationCompleted(Attributes.EMPTY);
    channelRead(Http2CodecUtil.connectionPrefaceBuf());
    // Simulate receipt of initial remote settings.
    ByteBuf serializedSettings = serializeSettings(new Http2Settings());
    channelRead(serializedSettings);
  }

  @Test
  public void sendFrameShouldSucceed() throws Exception {
    manualSetUp();
    createStream();

    // Send a frame and verify that it was written.
    ChannelFuture future = enqueue(
        new SendGrpcFrameCommand(stream.transportState(), content(), false));
    assertTrue(future.isSuccess());
    verifyWrite().writeData(eq(ctx()), eq(STREAM_ID), eq(content()), eq(0), eq(false),
        any(ChannelPromise.class));
  }

  @Test
  public void streamTracerCreated() throws Exception {
    manualSetUp();
    createStream();

    verify(streamTracerFactory).newServerStreamTracer(eq("foo/bar"), any(Metadata.class));
    StatsTraceContext statsTraceCtx = stream.statsTraceContext();
    List<StreamTracer> tracers = statsTraceCtx.getTracersForTest();
    assertEquals(1, tracers.size());
    assertSame(streamTracer, tracers.get(0));
  }

  @Test
  public void inboundDataWithEndStreamShouldForwardToStreamListener() throws Exception {
    manualSetUp();
    inboundDataShouldForwardToStreamListener(true);
  }

  @Test
  public void inboundDataShouldForwardToStreamListener() throws Exception {
    manualSetUp();
    inboundDataShouldForwardToStreamListener(false);
  }

  private void inboundDataShouldForwardToStreamListener(boolean endStream) throws Exception {
    createStream();
    stream.request(1);

    // Create a data frame and then trigger the handler to read it.
    ByteBuf frame = grpcDataFrame(STREAM_ID, endStream, contentAsArray());
    channelRead(frame);
    verify(streamListener, atLeastOnce())
        .messagesAvailable(any(StreamListener.MessageProducer.class));
    InputStream message = streamListenerMessageQueue.poll();
    assertArrayEquals(ByteBufUtil.getBytes(content()), ByteStreams.toByteArray(message));
    message.close();
    assertNull("no additional message expected", streamListenerMessageQueue.poll());

    if (endStream) {
      verify(streamListener).halfClosed();
    }
    verify(streamListener, atLeastOnce()).onReady();
    verifyNoMoreInteractions(streamListener);
  }

  @Test
  public void clientHalfCloseShouldForwardToStreamListener() throws Exception {
    manualSetUp();
    createStream();
    stream.request(1);

    channelRead(emptyGrpcFrame(STREAM_ID, true));

    verify(streamListener, atLeastOnce())
        .messagesAvailable(any(StreamListener.MessageProducer.class));
    InputStream message = streamListenerMessageQueue.poll();
    assertArrayEquals(new byte[0], ByteStreams.toByteArray(message));
    assertNull("no additional message expected", streamListenerMessageQueue.poll());
    verify(streamListener).halfClosed();
    verify(streamListener, atLeastOnce()).onReady();
    verifyNoMoreInteractions(streamListener);
  }

  @Test
  public void clientCancelShouldForwardToStreamListener() throws Exception {
    manualSetUp();
    createStream();

    channelRead(rstStreamFrame(STREAM_ID, (int) Http2Error.CANCEL.code()));

    ArgumentCaptor<Status> statusCap = ArgumentCaptor.forClass(Status.class);
    verify(streamListener).closed(statusCap.capture());
    assertEquals(Code.CANCELLED, statusCap.getValue().getCode());
    Truth.assertThat(statusCap.getValue().getDescription()).contains("RST_STREAM");
    verify(streamListener, atLeastOnce()).onReady();
    assertNull("no messages expected", streamListenerMessageQueue.poll());
  }

  @Test
  public void streamErrorShouldNotCloseChannel() throws Exception {
    manualSetUp();
    createStream();
    stream.request(1);

    // When a DATA frame is read, throw an exception. It will be converted into an
    // Http2StreamException.
    RuntimeException e = new RuntimeException("Fake Exception");
    doThrow(e).when(streamListener).messagesAvailable(any(StreamListener.MessageProducer.class));

    // Read a DATA frame to trigger the exception.
    channelRead(emptyGrpcFrame(STREAM_ID, true));

    // Verify that the channel was NOT closed.
    assertTrue(channel().isOpen());

    // Verify the stream was closed.
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(streamListener).closed(captor.capture());
    assertEquals(e, captor.getValue().asException().getCause());
    assertEquals(Code.UNKNOWN, captor.getValue().getCode());
  }

  @Test
  public void closeShouldCloseChannel() throws Exception {
    manualSetUp();
    handler().close(ctx(), newPromise());

    verifyWrite().writeGoAway(eq(ctx()), eq(0), eq(Http2Error.NO_ERROR.code()),
        eq(Unpooled.EMPTY_BUFFER), any(ChannelPromise.class));

    // Verify that the channel was closed.
    assertFalse(channel().isOpen());
  }

  @Test
  public void exceptionCaughtShouldCloseConnection() throws Exception {
    manualSetUp();
    handler().exceptionCaught(ctx(), new RuntimeException("fake exception"));

    // TODO(nmittler): EmbeddedChannel does not currently invoke the channelInactive processing,
    // so exceptionCaught() will not close streams properly in this test.
    // Once https://github.com/netty/netty/issues/4316 is resolved, we should also verify that
    // any open streams are closed properly.
    assertFalse(channel().isOpen());
  }

  @Test
  public void channelInactiveShouldCloseStreams() throws Exception {
    manualSetUp();
    createStream();
    handler().channelInactive(ctx());
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(streamListener).closed(captor.capture());
    assertFalse(captor.getValue().isOk());
  }

  @Test
  public void shouldAdvertiseMaxConcurrentStreams() throws Exception {
    maxConcurrentStreams = 314;
    manualSetUp();

    ArgumentCaptor<Http2Settings> captor = ArgumentCaptor.forClass(Http2Settings.class);
    verifyWrite().writeSettings(
        any(ChannelHandlerContext.class), captor.capture(), any(ChannelPromise.class));

    assertEquals(maxConcurrentStreams, captor.getValue().maxConcurrentStreams().intValue());
  }

  @Test
  public void shouldAdvertiseMaxHeaderListSize() throws Exception {
    maxHeaderListSize = 123;
    manualSetUp();

    ArgumentCaptor<Http2Settings> captor = ArgumentCaptor.forClass(Http2Settings.class);
    verifyWrite().writeSettings(
        any(ChannelHandlerContext.class), captor.capture(), any(ChannelPromise.class));

    assertEquals(maxHeaderListSize, captor.getValue().maxHeaderListSize().intValue());
  }

  @Test
  public void connectionWindowShouldBeOverridden() throws Exception {
    flowControlWindow = 1048576; // 1MiB
    manualSetUp();

    Http2Stream connectionStream = connection().connectionStream();
    Http2LocalFlowController localFlowController = connection().local().flowController();
    int actualInitialWindowSize = localFlowController.initialWindowSize(connectionStream);
    int actualWindowSize = localFlowController.windowSize(connectionStream);
    assertEquals(flowControlWindow, actualWindowSize);
    assertEquals(flowControlWindow, actualInitialWindowSize);
  }

  @Test
  public void cancelShouldSendRstStream() throws Exception {
    manualSetUp();
    createStream();
    enqueue(new CancelServerStreamCommand(stream.transportState(), Status.DEADLINE_EXCEEDED));
    verifyWrite().writeRstStream(eq(ctx()), eq(stream.transportState().id()),
        eq(Http2Error.CANCEL.code()), any(ChannelPromise.class));
  }

  @Test
  public void headersWithInvalidContentTypeShouldFail() throws Exception {
    manualSetUp();
    Http2Headers headers = new DefaultHttp2Headers()
            .method(HTTP_METHOD)
            .set(CONTENT_TYPE_HEADER, new AsciiString("application/bad", UTF_8))
            .set(TE_HEADER, TE_TRAILERS)
            .path(new AsciiString("/foo/bar"));
    ByteBuf headersFrame = headersFrame(STREAM_ID, headers);
    channelRead(headersFrame);
    verifyWrite().writeRstStream(eq(ctx()), eq(STREAM_ID), eq(Http2Error.REFUSED_STREAM.code()),
        any(ChannelPromise.class));
  }

  @Test
  public void headersSupportExtensionContentType() throws Exception {
    manualSetUp();
    Http2Headers headers = new DefaultHttp2Headers()
        .method(HTTP_METHOD)
        .set(CONTENT_TYPE_HEADER, new AsciiString("application/grpc+json", UTF_8))
        .set(TE_HEADER, TE_TRAILERS)
        .path(new AsciiString("/foo/bar"));
    ByteBuf headersFrame = headersFrame(STREAM_ID, headers);
    channelRead(headersFrame);

    ArgumentCaptor<NettyServerStream> streamCaptor =
        ArgumentCaptor.forClass(NettyServerStream.class);
    ArgumentCaptor<String> methodCaptor = ArgumentCaptor.forClass(String.class);
    verify(transportListener).streamCreated(streamCaptor.capture(), methodCaptor.capture(),
        any(Metadata.class));
    stream = streamCaptor.getValue();
  }

  @Test
  public void keepAliveManagerOnDataReceived_headersRead() throws Exception {
    manualSetUp();
    ByteBuf headersFrame = headersFrame(STREAM_ID, new DefaultHttp2Headers());
    channelRead(headersFrame);

    verify(spyKeepAliveManager).onDataReceived();
    verify(spyKeepAliveManager, never()).onTransportTermination();
  }

  @Test
  public void keepAliveManagerOnDataReceived_dataRead() throws Exception {
    manualSetUp();
    createStream();
    verify(spyKeepAliveManager).onDataReceived(); // received headers

    channelRead(grpcDataFrame(STREAM_ID, false, contentAsArray()));

    verify(spyKeepAliveManager, times(2)).onDataReceived();

    channelRead(grpcDataFrame(STREAM_ID, false, contentAsArray()));

    verify(spyKeepAliveManager, times(3)).onDataReceived();
    verify(spyKeepAliveManager, never()).onTransportTermination();
  }

  @Test
  public void keepAliveManagerOnDataReceived_rstStreamRead() throws Exception {
    manualSetUp();
    createStream();
    verify(spyKeepAliveManager).onDataReceived(); // received headers

    channelRead(rstStreamFrame(STREAM_ID, (int) Http2Error.CANCEL.code()));

    verify(spyKeepAliveManager, times(2)).onDataReceived();
    verify(spyKeepAliveManager, never()).onTransportTermination();
  }

  @Test
  public void keepAliveManagerOnDataReceived_pingRead() throws Exception {
    manualSetUp();
    ByteBuf payload = handler().ctx().alloc().buffer(8);
    payload.writeLong(1234L);
    channelRead(pingFrame(false /* isAck */, payload));

    verify(spyKeepAliveManager).onDataReceived();
    verify(spyKeepAliveManager, never()).onTransportTermination();
  }

  @Test
  public void keepAliveManagerOnDataReceived_pingActRead() throws Exception {
    manualSetUp();
    ByteBuf payload = handler().ctx().alloc().buffer(8);
    payload.writeLong(1234L);
    channelRead(pingFrame(true /* isAck */, payload));

    verify(spyKeepAliveManager).onDataReceived();
    verify(spyKeepAliveManager, never()).onTransportTermination();
  }

  @Test
  public void keepAliveManagerOnTransportTermination() throws Exception {
    manualSetUp();
    handler().channelInactive(handler().ctx());

    verify(spyKeepAliveManager).onTransportTermination();
  }

  @Test
  public void keepAliveManager_pingSent() throws Exception {
    ByteBuf pingBuf = directBuffer(8).writeLong(0xDEADL);
    keepAliveTimeInNanos = TimeUnit.MILLISECONDS.toNanos(10L);
    keepAliveTimeoutInNanos = TimeUnit.MINUTES.toNanos(30L);
    manualSetUp();

    assertEquals(0, transportTracer.getStats().keepAlivesSent);
    fakeClock().forwardNanos(keepAliveTimeInNanos);
    assertEquals(1, transportTracer.getStats().keepAlivesSent);

    verifyWrite().writePing(eq(ctx()), eq(false), eq(pingBuf), any(ChannelPromise.class));

    spyKeepAliveManager.onDataReceived();
    fakeClock().forwardTime(10L, TimeUnit.MILLISECONDS);

    verifyWrite(times(2))
        .writePing(eq(ctx()), eq(false), eq(pingBuf), any(ChannelPromise.class));
    assertTrue(channel().isOpen());
  }

  @Test
  public void keepAliveManager_pingTimeout() throws Exception {
    keepAliveTimeInNanos = TimeUnit.NANOSECONDS.toNanos(123L);
    keepAliveTimeoutInNanos = TimeUnit.NANOSECONDS.toNanos(456L);
    manualSetUp();

    fakeClock().forwardNanos(keepAliveTimeInNanos);

    assertTrue(channel().isOpen());

    fakeClock().forwardNanos(keepAliveTimeoutInNanos);

    assertTrue(!channel().isOpen());
  }

  @Test
  public void keepAliveEnforcer_enforcesPings() throws Exception {
    permitKeepAliveWithoutCalls = false;
    permitKeepAliveTimeInNanos = TimeUnit.HOURS.toNanos(1);
    manualSetUp();

    ByteBuf payload = handler().ctx().alloc().buffer(8);
    payload.writeLong(1);
    for (int i = 0; i < KeepAliveEnforcer.MAX_PING_STRIKES + 1; i++) {
      channelRead(pingFrame(false /* isAck */, payload.slice()));
    }
    payload.release();
    verifyWrite().writeGoAway(eq(ctx()), eq(0), eq(Http2Error.ENHANCE_YOUR_CALM.code()),
        any(ByteBuf.class), any(ChannelPromise.class));
    assertFalse(channel().isActive());
  }

  @Test
  public void keepAliveEnforcer_sendingDataResetsCounters() throws Exception {
    permitKeepAliveWithoutCalls = false;
    permitKeepAliveTimeInNanos = TimeUnit.HOURS.toNanos(1);
    manualSetUp();

    createStream();
    Http2Headers headers = Utils.convertServerHeaders(new Metadata());
    ChannelFuture future = enqueue(
        SendResponseHeadersCommand.createHeaders(stream.transportState(), headers));
    future.get();
    ByteBuf payload = handler().ctx().alloc().buffer(8);
    payload.writeLong(1);
    for (int i = 0; i < 10; i++) {
      future = enqueue(
          new SendGrpcFrameCommand(stream.transportState(), content().retainedSlice(), false));
      future.get();
      channel().releaseOutbound();
      channelRead(pingFrame(false /* isAck */, payload.slice()));
    }
    payload.release();
    verifyWrite(never()).writeGoAway(eq(ctx()), eq(STREAM_ID),
        eq(Http2Error.ENHANCE_YOUR_CALM.code()), any(ByteBuf.class), any(ChannelPromise.class));
  }

  @Test
  public void keepAliveEnforcer_initialIdle() throws Exception {
    permitKeepAliveWithoutCalls = false;
    permitKeepAliveTimeInNanos = 0;
    manualSetUp();

    ByteBuf payload = handler().ctx().alloc().buffer(8);
    payload.writeLong(1);
    for (int i = 0; i < KeepAliveEnforcer.MAX_PING_STRIKES + 1; i++) {
      channelRead(pingFrame(false /* isAck */, payload.slice()));
    }
    payload.release();
    verifyWrite().writeGoAway(eq(ctx()), eq(0),
        eq(Http2Error.ENHANCE_YOUR_CALM.code()), any(ByteBuf.class), any(ChannelPromise.class));
    assertFalse(channel().isActive());
  }

  @Test
  public void keepAliveEnforcer_noticesActive() throws Exception {
    permitKeepAliveWithoutCalls = false;
    permitKeepAliveTimeInNanos = 0;
    manualSetUp();

    createStream();
    ByteBuf payload = handler().ctx().alloc().buffer(8);
    payload.writeLong(1);
    for (int i = 0; i < 10; i++) {
      channelRead(pingFrame(false /* isAck */, payload.slice()));
    }
    payload.release();
    verifyWrite(never()).writeGoAway(eq(ctx()), eq(STREAM_ID),
        eq(Http2Error.ENHANCE_YOUR_CALM.code()), any(ByteBuf.class), any(ChannelPromise.class));
  }

  @Test
  public void keepAliveEnforcer_noticesInactive() throws Exception {
    permitKeepAliveWithoutCalls = false;
    permitKeepAliveTimeInNanos = 0;
    manualSetUp();

    createStream();
    channelRead(rstStreamFrame(STREAM_ID, (int) Http2Error.CANCEL.code()));
    ByteBuf payload = handler().ctx().alloc().buffer(8);
    payload.writeLong(1);
    for (int i = 0; i < KeepAliveEnforcer.MAX_PING_STRIKES + 1; i++) {
      channelRead(pingFrame(false /* isAck */, payload.slice()));
    }
    payload.release();
    verifyWrite().writeGoAway(eq(ctx()), eq(STREAM_ID),
        eq(Http2Error.ENHANCE_YOUR_CALM.code()), any(ByteBuf.class), any(ChannelPromise.class));
    assertFalse(channel().isActive());
  }

  @Test
  public void noGoAwaySentBeforeMaxConnectionIdleReached() throws Exception {
    maxConnectionIdleInNanos = TimeUnit.MINUTES.toNanos(30L);
    manualSetUp();

    fakeClock().forwardTime(20, TimeUnit.MINUTES);

    // GO_AWAY not sent yet
    verifyWrite(never()).writeGoAway(
        any(ChannelHandlerContext.class), any(Integer.class), any(Long.class), any(ByteBuf.class),
        any(ChannelPromise.class));
    assertTrue(channel().isOpen());
  }

  @Test
  public void maxConnectionIdle_goAwaySent() throws Exception {
    maxConnectionIdleInNanos = TimeUnit.MILLISECONDS.toNanos(10L);
    manualSetUp();
    assertTrue(channel().isOpen());

    fakeClock().forwardNanos(maxConnectionIdleInNanos);

    // GO_AWAY sent
    verifyWrite().writeGoAway(
        eq(ctx()), eq(Integer.MAX_VALUE), eq(Http2Error.NO_ERROR.code()), any(ByteBuf.class),
        any(ChannelPromise.class));

    // channel closed
    assertTrue(!channel().isOpen());
  }

  @Test
  public void maxConnectionIdle_activeThenRst() throws Exception {
    maxConnectionIdleInNanos = TimeUnit.MILLISECONDS.toNanos(10L);
    manualSetUp();
    createStream();

    fakeClock().forwardNanos(maxConnectionIdleInNanos);

    // GO_AWAY not sent when active
    verifyWrite(never()).writeGoAway(
        any(ChannelHandlerContext.class), any(Integer.class), any(Long.class), any(ByteBuf.class),
        any(ChannelPromise.class));
    assertTrue(channel().isOpen());

    channelRead(rstStreamFrame(STREAM_ID, (int) Http2Error.CANCEL.code()));

    fakeClock().forwardNanos(maxConnectionIdleInNanos);

    // GO_AWAY sent
    verifyWrite().writeGoAway(
        eq(ctx()), eq(Integer.MAX_VALUE), eq(Http2Error.NO_ERROR.code()), any(ByteBuf.class),
        any(ChannelPromise.class));

    // channel closed
    assertTrue(!channel().isOpen());
  }

  @Test
  public void noGoAwaySentBeforeMaxConnectionAgeReached() throws Exception {
    maxConnectionAgeInNanos = TimeUnit.MINUTES.toNanos(30L);
    manualSetUp();

    fakeClock().forwardTime(20, TimeUnit.MINUTES);

    // GO_AWAY not sent yet
    verifyWrite(never()).writeGoAway(
        any(ChannelHandlerContext.class), any(Integer.class), any(Long.class), any(ByteBuf.class),
        any(ChannelPromise.class));
    assertTrue(channel().isOpen());
  }

  @Test
  public void maxConnectionAge_goAwaySent() throws Exception {
    maxConnectionAgeInNanos = TimeUnit.MILLISECONDS.toNanos(10L);
    manualSetUp();
    assertTrue(channel().isOpen());

    fakeClock().forwardNanos(maxConnectionAgeInNanos);

    // GO_AWAY sent
    verifyWrite().writeGoAway(
        eq(ctx()), eq(Integer.MAX_VALUE), eq(Http2Error.NO_ERROR.code()), any(ByteBuf.class),
        any(ChannelPromise.class));

    // channel closed
    assertTrue(!channel().isOpen());
  }

  @Test
  public void maxConnectionAgeGrace_channelStillOpenDuringGracePeriod() throws Exception {
    maxConnectionAgeInNanos = TimeUnit.MILLISECONDS.toNanos(10L);
    maxConnectionAgeGraceInNanos = TimeUnit.MINUTES.toNanos(30L);
    manualSetUp();
    createStream();

    fakeClock().forwardNanos(maxConnectionAgeInNanos);

    verifyWrite().writeGoAway(
        eq(ctx()), eq(Integer.MAX_VALUE), eq(Http2Error.NO_ERROR.code()), any(ByteBuf.class),
        any(ChannelPromise.class));

    fakeClock().forwardTime(20, TimeUnit.MINUTES);

    // channel not closed yet
    assertTrue(channel().isOpen());
  }

  @Test
  public void maxConnectionAgeGrace_channelClosedAfterGracePeriod() throws Exception {
    maxConnectionAgeInNanos = TimeUnit.MILLISECONDS.toNanos(10L);
    maxConnectionAgeGraceInNanos = TimeUnit.MINUTES.toNanos(30L);
    manualSetUp();
    createStream();

    fakeClock().forwardNanos(maxConnectionAgeInNanos);

    verifyWrite().writeGoAway(
        eq(ctx()), eq(Integer.MAX_VALUE), eq(Http2Error.NO_ERROR.code()), any(ByteBuf.class),
        any(ChannelPromise.class));
    assertTrue(channel().isOpen());

    fakeClock().forwardNanos(maxConnectionAgeGraceInNanos);

    // channel closed
    assertTrue(!channel().isOpen());
  }

  @Test
  public void transportTracer_windowSizeDefault() throws Exception {
    manualSetUp();
    TransportTracer.Stats stats = transportTracer.getStats();
    assertEquals(Http2CodecUtil.DEFAULT_WINDOW_SIZE, stats.remoteFlowControlWindow);
    assertEquals(flowControlWindow, stats.localFlowControlWindow);
  }

  @Test
  public void transportTracer_windowUpdate_remote() throws Exception {
    flowControlWindow = 1048576; // 1MiB
    manualSetUp();
    TransportTracer.Stats before = transportTracer.getStats();
    assertEquals(Http2CodecUtil.DEFAULT_WINDOW_SIZE, before.remoteFlowControlWindow);
    assertEquals(flowControlWindow, before.localFlowControlWindow);

    ByteBuf serializedSettings = windowUpdate(0, 1000);
    channelRead(serializedSettings);
    TransportTracer.Stats after = transportTracer.getStats();
    assertEquals(Http2CodecUtil.DEFAULT_WINDOW_SIZE + 1000,
        after.remoteFlowControlWindow);
    assertEquals(flowControlWindow, after.localFlowControlWindow);
  }

  @Test
  public void transportTracer_windowUpdate_local() throws Exception {
    manualSetUp();
    TransportTracer.Stats before = transportTracer.getStats();
    assertEquals(Http2CodecUtil.DEFAULT_WINDOW_SIZE, before.remoteFlowControlWindow);
    assertEquals(flowControlWindow, before.localFlowControlWindow);

    // If the window size is below a certain threshold, netty will wait to apply the update.
    // Use a large increment to be sure that it exceeds the threshold.
    connection().local().flowController().incrementWindowSize(
        connection().connectionStream(), 8 * Http2CodecUtil.DEFAULT_WINDOW_SIZE);

    TransportTracer.Stats after = transportTracer.getStats();
    assertEquals(Http2CodecUtil.DEFAULT_WINDOW_SIZE, after.remoteFlowControlWindow);
    assertEquals(flowControlWindow + 8 * Http2CodecUtil.DEFAULT_WINDOW_SIZE,
        after.localFlowControlWindow);
  }

  private void createStream() throws Exception {
    Http2Headers headers = new DefaultHttp2Headers()
        .method(HTTP_METHOD)
        .set(CONTENT_TYPE_HEADER, CONTENT_TYPE_GRPC)
        .set(TE_HEADER, TE_TRAILERS)
        .path(new AsciiString("/foo/bar"));
    ByteBuf headersFrame = headersFrame(STREAM_ID, headers);
    channelRead(headersFrame);

    ArgumentCaptor<NettyServerStream> streamCaptor =
        ArgumentCaptor.forClass(NettyServerStream.class);
    ArgumentCaptor<String> methodCaptor = ArgumentCaptor.forClass(String.class);
    verify(transportListener).streamCreated(streamCaptor.capture(), methodCaptor.capture(),
        any(Metadata.class));
    stream = streamCaptor.getValue();
  }

  private ByteBuf emptyGrpcFrame(int streamId, boolean endStream) throws Exception {
    ByteBuf buf = NettyTestUtil.messageFrame("");
    try {
      return dataFrame(streamId, endStream, buf);
    } finally {
      buf.release();
    }
  }

  @Override
  protected NettyServerHandler newHandler() {
    return NettyServerHandler.newHandler(
        frameReader(), frameWriter(), transportListener,
        Arrays.asList(streamTracerFactory), transportTracer,
        maxConcurrentStreams, flowControlWindow,
        maxHeaderListSize, DEFAULT_MAX_MESSAGE_SIZE,
        keepAliveTimeInNanos, keepAliveTimeoutInNanos,
        maxConnectionIdleInNanos,
        maxConnectionAgeInNanos, maxConnectionAgeGraceInNanos,
        permitKeepAliveWithoutCalls, permitKeepAliveTimeInNanos);
  }

  @Override
  protected WriteQueue initWriteQueue() {
    return handler().getWriteQueue();
  }

  @Override
  protected void makeStream() throws Exception {
    createStream();
  }
}
