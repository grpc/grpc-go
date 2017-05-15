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
import static io.grpc.testing.TestUtils.sleepAtLeast;
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
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.spy;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.io.ByteStreams;
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
import java.util.List;
import java.util.concurrent.TimeUnit;
import org.junit.Ignore;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/**
 * Unit tests for {@link NettyServerHandler}.
 */
@RunWith(JUnit4.class)
public class NettyServerHandlerTest extends NettyHandlerTestBase<NettyServerHandler> {

  private static final int STREAM_ID = 3;

  @Mock
  private ServerStreamListener streamListener;

  @Mock
  private ServerStreamTracer.Factory streamTracerFactory;

  private final ServerTransportListener transportListener = spy(new ServerTransportListenerImpl());
  private final ServerStreamTracer streamTracer = spy(new ServerStreamTracer() {});

  private NettyServerStream stream;
  private KeepAliveManager spyKeepAliveManager;

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
    when(streamTracerFactory.newServerStreamTracer(anyString(), any(Metadata.class)))
        .thenReturn(streamTracer);

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
    ArgumentCaptor<InputStream> captor = ArgumentCaptor.forClass(InputStream.class);
    verify(streamListener).messageRead(captor.capture());
    assertArrayEquals(ByteBufUtil.getBytes(content()), ByteStreams.toByteArray(captor.getValue()));
    captor.getValue().close();

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
    ArgumentCaptor<InputStream> captor = ArgumentCaptor.forClass(InputStream.class);
    verify(streamListener).messageRead(captor.capture());
    assertArrayEquals(new byte[0], ByteStreams.toByteArray(captor.getValue()));
    verify(streamListener).halfClosed();
    verify(streamListener, atLeastOnce()).onReady();
    verifyNoMoreInteractions(streamListener);
  }

  @Test
  public void clientCancelShouldForwardToStreamListener() throws Exception {
    manualSetUp();
    createStream();

    channelRead(rstStreamFrame(STREAM_ID, (int) Http2Error.CANCEL.code()));
    verify(streamListener, never()).messageRead(any(InputStream.class));
    verify(streamListener).closed(Status.CANCELLED);
    verify(streamListener, atLeastOnce()).onReady();
    verifyNoMoreInteractions(streamListener);
  }

  @Test
  public void streamErrorShouldNotCloseChannel() throws Exception {
    manualSetUp();
    createStream();
    stream.request(1);

    // When a DATA frame is read, throw an exception. It will be converted into an
    // Http2StreamException.
    RuntimeException e = new RuntimeException("Fake Exception");
    doThrow(e).when(streamListener).messageRead(any(InputStream.class));

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
  @Ignore("Re-enable once https://github.com/grpc/grpc-java/issues/1175 is fixed")
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

    sleepAtLeast(10L);
    channel().runPendingTasks();

    verifyWrite().writePing(eq(ctx()), eq(false), eq(pingBuf), any(ChannelPromise.class));

    spyKeepAliveManager.onDataReceived();
    sleepAtLeast(10L);
    channel().runPendingTasks();

    verifyWrite(times(2))
        .writePing(eq(ctx()), eq(false), eq(pingBuf), any(ChannelPromise.class));
    assertTrue(channel().isOpen());
  }

  @Test
  public void keepAliveManager_pingTimeout() throws Exception {
    keepAliveTimeInNanos = TimeUnit.MILLISECONDS.toNanos(10L);
    keepAliveTimeoutInNanos = TimeUnit.MILLISECONDS.toNanos(10L);
    manualSetUp();

    sleepAtLeast(10L);
    channel().runPendingTasks();

    assertTrue(channel().isOpen());

    sleepAtLeast(10L);
    channel().runPendingTasks();

    assertTrue(!channel().isOpen());
  }

  @Test
  public void keepAliveEnforcer_enforcesPings() throws Exception {
    permitKeepAliveWithoutCalls = false;
    permitKeepAliveTimeInNanos = TimeUnit.HOURS.toNanos(1);
    manualSetUp();

    ByteBuf payload = handler().ctx().alloc().buffer(8);
    payload.writeLong(1);
    for (int i = 0; i < 10; i++) {
      channelRead(pingFrame(false /* isAck */, payload.slice()));
    }
    payload.release();
    verifyWrite().writeGoAway(eq(ctx()), eq(0), eq(Http2Error.ENHANCE_YOUR_CALM.code()),
        any(ByteBuf.class), any(ChannelPromise.class));
  }

  @Test(timeout = 1000)
  public void keepAliveEnforcer_sendingDataResetsCounters() throws Exception {
    permitKeepAliveWithoutCalls = false;
    permitKeepAliveTimeInNanos = TimeUnit.HOURS.toNanos(1);
    manualSetUp();

    createStream();
    Http2Headers headers = Utils.convertServerHeaders(new Metadata());
    ChannelFuture future = enqueue(
        new SendResponseHeadersCommand(stream.transportState(), headers, false));
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
    for (int i = 0; i < 10; i++) {
      channelRead(pingFrame(false /* isAck */, payload.slice()));
    }
    payload.release();
    verifyWrite().writeGoAway(eq(ctx()), eq(0),
        eq(Http2Error.ENHANCE_YOUR_CALM.code()), any(ByteBuf.class), any(ChannelPromise.class));
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
    for (int i = 0; i < 10; i++) {
      channelRead(pingFrame(false /* isAck */, payload.slice()));
    }
    payload.release();
    verifyWrite().writeGoAway(eq(ctx()), eq(STREAM_ID),
        eq(Http2Error.ENHANCE_YOUR_CALM.code()), any(ByteBuf.class), any(ChannelPromise.class));
  }

  @Test
  public void noGoAwaySentBeforeMaxConnectionIdleReached() throws Exception {
    maxConnectionIdleInNanos = TimeUnit.MINUTES.toNanos(30L);
    manualSetUp();

    sleepAtLeast(10L);
    channel().runPendingTasks();

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

    sleepAtLeast(10L);
    channel().runPendingTasks();

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

    sleepAtLeast(10L);
    channel().runPendingTasks();

    // GO_AWAY not sent when active
    verifyWrite(never()).writeGoAway(
        any(ChannelHandlerContext.class), any(Integer.class), any(Long.class), any(ByteBuf.class),
        any(ChannelPromise.class));
    assertTrue(channel().isOpen());

    channelRead(rstStreamFrame(STREAM_ID, (int) Http2Error.CANCEL.code()));

    sleepAtLeast(10L);
    channel().runPendingTasks();

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

    sleepAtLeast(10L);
    channel().runPendingTasks();

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

    sleepAtLeast(10L);
    channel().runPendingTasks();

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

    sleepAtLeast(10L);
    channel().runPendingTasks();

    verifyWrite().writeGoAway(
        eq(ctx()), eq(Integer.MAX_VALUE), eq(Http2Error.NO_ERROR.code()), any(ByteBuf.class),
        any(ChannelPromise.class));

    sleepAtLeast(10L);
    channel().runPendingTasks();

    // channel not closed yet
    assertTrue(channel().isOpen());
  }

  @Test
  public void maxConnectionAgeGrace_channelClosedAfterGracePeriod() throws Exception {
    maxConnectionAgeInNanos = TimeUnit.MILLISECONDS.toNanos(10L);
    maxConnectionAgeGraceInNanos = TimeUnit.MILLISECONDS.toNanos(10L);
    manualSetUp();
    createStream();

    // runPendingTasks so that GO_AWAY is sent and the forceful shutdown is scheduled
    sleepAtLeast(10L);
    channel().runPendingTasks();

    verifyWrite().writeGoAway(
        eq(ctx()), eq(Integer.MAX_VALUE), eq(Http2Error.NO_ERROR.code()), any(ByteBuf.class),
        any(ChannelPromise.class));
    assertTrue(channel().isOpen());

    // need runPendingTasks again so that the forceful shutdown can be executed
    sleepAtLeast(10L);
    channel().runPendingTasks();

    // channel closed
    assertTrue(!channel().isOpen());
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
        Arrays.asList(streamTracerFactory), maxConcurrentStreams, flowControlWindow,
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
