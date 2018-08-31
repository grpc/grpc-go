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

import static com.google.common.base.Charsets.UTF_8;
import static io.grpc.internal.ClientStreamListener.RpcProgress.PROCESSED;
import static io.grpc.internal.ClientStreamListener.RpcProgress.REFUSED;
import static io.grpc.internal.GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE;
import static io.grpc.netty.Utils.CONTENT_TYPE_GRPC;
import static io.grpc.netty.Utils.CONTENT_TYPE_HEADER;
import static io.grpc.netty.Utils.HTTPS;
import static io.grpc.netty.Utils.HTTP_METHOD;
import static io.grpc.netty.Utils.STATUS_OK;
import static io.grpc.netty.Utils.TE_HEADER;
import static io.grpc.netty.Utils.TE_TRAILERS;
import static io.netty.handler.codec.http2.Http2CodecUtil.DEFAULT_PRIORITY_WEIGHT;
import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.notNull;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;

import com.google.common.base.Stopwatch;
import com.google.common.base.Supplier;
import com.google.common.base.Ticker;
import com.google.common.collect.ImmutableList;
import com.google.common.io.ByteStreams;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.errorprone.annotations.CanIgnoreReturnValue;
import io.grpc.Attributes;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.StatusException;
import io.grpc.internal.ClientStreamListener;
import io.grpc.internal.ClientStreamListener.RpcProgress;
import io.grpc.internal.ClientTransport;
import io.grpc.internal.ClientTransport.PingCallback;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.KeepAliveManager;
import io.grpc.internal.StatsTraceContext;
import io.grpc.internal.StreamListener;
import io.grpc.internal.TransportTracer;
import io.grpc.netty.GrpcHttp2HeadersUtils.GrpcHttp2ClientHeadersDecoder;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufUtil;
import io.netty.buffer.Unpooled;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPromise;
import io.netty.channel.EventLoop;
import io.netty.handler.codec.http2.DefaultHttp2Connection;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2Error;
import io.netty.handler.codec.http2.Http2Exception;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2LocalFlowController;
import io.netty.handler.codec.http2.Http2Settings;
import io.netty.handler.codec.http2.Http2Stream;
import io.netty.util.AsciiString;
import java.io.InputStream;
import java.util.LinkedList;
import java.util.List;
import java.util.Queue;
import java.util.concurrent.atomic.AtomicBoolean;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.TestName;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Matchers;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

/**
 * Tests for {@link NettyClientHandler}.
 */
@RunWith(JUnit4.class)
public class NettyClientHandlerTest extends NettyHandlerTestBase<NettyClientHandler> {

  private NettyClientStream.TransportState streamTransportState;
  private Http2Headers grpcHeaders;
  private long nanoTime; // backs a ticker, for testing ping round-trip time measurement
  private int maxHeaderListSize = Integer.MAX_VALUE;
  private int streamId = 3;
  private ClientTransportLifecycleManager lifecycleManager;
  private KeepAliveManager mockKeepAliveManager = null;
  private List<String> setKeepaliveManagerFor = ImmutableList.of("cancelShouldSucceed",
      "sendFrameShouldSucceed", "channelShutdownShouldCancelBufferedStreams",
      "createIncrementsIdsForActualAndBufferdStreams", "dataPingAckIsRecognized");
  private Runnable tooManyPingsRunnable = new Runnable() {
    @Override public void run() {}
  };

  @Rule
  public TestName testNameRule = new TestName();
  @Mock
  private NettyClientTransport.Listener listener;
  @Mock
  private ClientStreamListener streamListener;

  private final Queue<InputStream> streamListenerMessageQueue = new LinkedList<InputStream>();

  @Override
  protected void manualSetUp() throws Exception {
    setUp();
  }

  /**
   * Set up for test.
   */
  @Before
  public void setUp() throws Exception {
    MockitoAnnotations.initMocks(this);

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

    lifecycleManager = new ClientTransportLifecycleManager(listener);
    // This mocks the keepalive manager only for there's in which we verify it. For other tests
    // it'll be null which will be testing if we behave correctly when it's not present.
    if (setKeepaliveManagerFor.contains(testNameRule.getMethodName())) {
      mockKeepAliveManager = mock(KeepAliveManager.class);
    }

    initChannel(new GrpcHttp2ClientHeadersDecoder(GrpcUtil.DEFAULT_MAX_HEADER_LIST_SIZE));
    streamTransportState = new TransportStateImpl(
        handler(),
        channel().eventLoop(),
        DEFAULT_MAX_MESSAGE_SIZE,
        transportTracer);
    streamTransportState.setListener(streamListener);

    grpcHeaders = new DefaultHttp2Headers()
        .scheme(HTTPS)
        .authority(as("www.fake.com"))
        .path(as("/fakemethod"))
        .method(HTTP_METHOD)
        .add(as("auth"), as("sometoken"))
        .add(CONTENT_TYPE_HEADER, CONTENT_TYPE_GRPC)
        .add(TE_HEADER, TE_TRAILERS);

    // Simulate receipt of initial remote settings.
    ByteBuf serializedSettings = serializeSettings(new Http2Settings());
    channelRead(serializedSettings);
  }

  @Test
  public void cancelBufferedStreamShouldChangeClientStreamStatus() throws Exception {
    // Force the stream to be buffered.
    receiveMaxConcurrentStreams(0);
    // Create a new stream with id 3.
    ChannelFuture createFuture = enqueue(
        new CreateStreamCommand(grpcHeaders, streamTransportState));
    assertEquals(3, streamTransportState.id());
    // Cancel the stream.
    cancelStream(Status.CANCELLED);

    assertTrue(createFuture.isSuccess());
    verify(streamListener).closed(eq(Status.CANCELLED), same(PROCESSED), any(Metadata.class));
  }

  @Test
  public void createStreamShouldSucceed() throws Exception {
    createStream();
    verifyWrite().writeHeaders(eq(ctx()), eq(3), eq(grpcHeaders), eq(0),
        eq(DEFAULT_PRIORITY_WEIGHT), eq(false), eq(0), eq(false), any(ChannelPromise.class));
  }

  @Test
  public void cancelShouldSucceed() throws Exception {
    createStream();
    cancelStream(Status.CANCELLED);

    verifyWrite().writeRstStream(eq(ctx()), eq(3), eq(Http2Error.CANCEL.code()),
        any(ChannelPromise.class));
    verify(mockKeepAliveManager, times(1)).onTransportActive(); // onStreamActive
    verify(mockKeepAliveManager, times(1)).onTransportIdle(); // onStreamClosed
    verifyNoMoreInteractions(mockKeepAliveManager);
  }

  @Test
  public void cancelDeadlineExceededShouldSucceed() throws Exception {
    createStream();
    cancelStream(Status.DEADLINE_EXCEEDED);

    verifyWrite().writeRstStream(eq(ctx()), eq(3), eq(Http2Error.CANCEL.code()),
        any(ChannelPromise.class));
  }

  @Test
  public void cancelWhileBufferedShouldSucceed() throws Exception {
    // Force the stream to be buffered.
    receiveMaxConcurrentStreams(0);

    ChannelFuture createFuture = createStream();
    assertFalse(createFuture.isDone());

    ChannelFuture cancelFuture = cancelStream(Status.CANCELLED);
    assertTrue(cancelFuture.isSuccess());
    assertTrue(createFuture.isDone());
    assertTrue(createFuture.isSuccess());
  }

  /**
   * Although nobody is listening to an exception should it occur during cancel(), we don't want an
   * exception to be thrown because it would negatively impact performance, and we don't want our
   * users working around around such performance issues.
   */
  @Test
  public void cancelTwiceShouldSucceed() throws Exception {
    createStream();

    cancelStream(Status.CANCELLED);

    verifyWrite().writeRstStream(any(ChannelHandlerContext.class), eq(3),
        eq(Http2Error.CANCEL.code()), any(ChannelPromise.class));

    ChannelFuture future = cancelStream(Status.CANCELLED);
    assertTrue(future.isSuccess());
  }

  @Test
  public void cancelTwiceDifferentReasons() throws Exception {
    createStream();

    cancelStream(Status.DEADLINE_EXCEEDED);

    verifyWrite().writeRstStream(eq(ctx()), eq(3), eq(Http2Error.CANCEL.code()),
        any(ChannelPromise.class));

    ChannelFuture future = cancelStream(Status.CANCELLED);
    assertTrue(future.isSuccess());
  }

  @Test
  public void sendFrameShouldSucceed() throws Exception {
    createStream();

    // Send a frame and verify that it was written.
    ChannelFuture future
        = enqueue(new SendGrpcFrameCommand(streamTransportState, content(), true));

    assertTrue(future.isSuccess());
    verifyWrite().writeData(eq(ctx()), eq(3), eq(content()), eq(0), eq(true),
        any(ChannelPromise.class));
    verify(mockKeepAliveManager, times(1)).onTransportActive(); // onStreamActive
    verifyNoMoreInteractions(mockKeepAliveManager);
  }

  @Test
  public void sendForUnknownStreamShouldFail() throws Exception {
    ChannelFuture future
        = enqueue(new SendGrpcFrameCommand(streamTransportState, content(), true));
    assertTrue(future.isDone());
    assertFalse(future.isSuccess());
  }

  @Test
  public void inboundShouldForwardToStream() throws Exception {
    createStream();

    // Read a headers frame first.
    Http2Headers headers = new DefaultHttp2Headers().status(STATUS_OK)
        .set(CONTENT_TYPE_HEADER, CONTENT_TYPE_GRPC)
        .set(as("magic"), as("value"));
    ByteBuf headersFrame = headersFrame(3, headers);
    channelRead(headersFrame);
    ArgumentCaptor<Metadata> captor = ArgumentCaptor.forClass(Metadata.class);
    verify(streamListener).headersRead(captor.capture());
    assertEquals("value",
        captor.getValue().get(Metadata.Key.of("magic", Metadata.ASCII_STRING_MARSHALLER)));

    streamTransportState.requestMessagesFromDeframer(1);

    // Create a data frame and then trigger the handler to read it.
    ByteBuf frame = grpcDataFrame(3, false, contentAsArray());
    channelRead(frame);
    InputStream message = streamListenerMessageQueue.poll();
    assertArrayEquals(ByteBufUtil.getBytes(content()), ByteStreams.toByteArray(message));
    message.close();
    assertNull("no additional message expected", streamListenerMessageQueue.poll());
  }

  @Test
  public void receivedGoAwayShouldCancelBufferedStream() throws Exception {
    // Force the stream to be buffered.
    receiveMaxConcurrentStreams(0);
    ChannelFuture future = enqueue(new CreateStreamCommand(grpcHeaders, streamTransportState));
    channelRead(goAwayFrame(0));
    assertTrue(future.isDone());
    assertFalse(future.isSuccess());
    Status status = Status.fromThrowable(future.cause());
    assertEquals(Status.Code.UNAVAILABLE, status.getCode());
    assertEquals("HTTP/2 error code: NO_ERROR\nReceived Goaway", status.getDescription());
  }

  @Test
  public void receivedGoAwayShouldRefuseLaterStreamId() throws Exception {
    ChannelFuture future = enqueue(new CreateStreamCommand(grpcHeaders, streamTransportState));
    channelRead(goAwayFrame(streamId - 1));
    verify(streamListener).closed(any(Status.class), eq(REFUSED), any(Metadata.class));
    assertTrue(future.isDone());
  }

  @Test
  public void receivedGoAwayShouldNotAffectEarlyStreamId() throws Exception {
    ChannelFuture future = enqueue(new CreateStreamCommand(grpcHeaders, streamTransportState));
    channelRead(goAwayFrame(streamId));
    verify(streamListener, never())
        .closed(any(Status.class), any(Metadata.class));
    verify(streamListener, never())
        .closed(any(Status.class), any(RpcProgress.class), any(Metadata.class));
    assertTrue(future.isDone());
  }

  @Test
  public void receivedResetWithRefuseCode() throws Exception {
    ChannelFuture future = enqueue(new CreateStreamCommand(grpcHeaders, streamTransportState));
    channelRead(rstStreamFrame(streamId, (int) Http2Error.REFUSED_STREAM.code() ));
    verify(streamListener).closed(any(Status.class), eq(REFUSED), any(Metadata.class));
    assertTrue(future.isDone());
  }

  @Test
  public void receivedResetWithCanceCode() throws Exception {
    ChannelFuture future = enqueue(new CreateStreamCommand(grpcHeaders, streamTransportState));
    channelRead(rstStreamFrame(streamId, (int) Http2Error.CANCEL.code()));
    verify(streamListener).closed(any(Status.class), eq(PROCESSED), any(Metadata.class));
    assertTrue(future.isDone());
  }

  @Test
  public void receivedGoAwayShouldFailUnknownStreams() throws Exception {
    enqueue(new CreateStreamCommand(grpcHeaders, streamTransportState));

    // Read a GOAWAY that indicates our stream was never processed by the server.
    channelRead(goAwayFrame(0, 8 /* Cancel */, Unpooled.copiedBuffer("this is a test", UTF_8)));
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(streamListener).closed(captor.capture(), same(REFUSED), notNull(Metadata.class));
    assertEquals(Status.CANCELLED.getCode(), captor.getValue().getCode());
    assertEquals("HTTP/2 error code: CANCEL\nReceived Goaway\nthis is a test",
        captor.getValue().getDescription());
  }

  @Test
  public void receivedGoAwayShouldFailUnknownBufferedStreams() throws Exception {
    receiveMaxConcurrentStreams(0);

    ChannelFuture future = enqueue(new CreateStreamCommand(grpcHeaders, streamTransportState));

    // Read a GOAWAY that indicates our stream was never processed by the server.
    channelRead(goAwayFrame(0, 8 /* Cancel */, Unpooled.copiedBuffer("this is a test", UTF_8)));
    assertTrue(future.isDone());
    assertFalse(future.isSuccess());
    Status status = Status.fromThrowable(future.cause());
    assertEquals(Status.CANCELLED.getCode(), status.getCode());
    assertEquals("HTTP/2 error code: CANCEL\nReceived Goaway\nthis is a test",
        status.getDescription());
  }

  @Test
  public void receivedGoAwayShouldFailNewStreams() throws Exception {
    // Read a GOAWAY that indicates our stream was never processed by the server.
    channelRead(goAwayFrame(0, 8 /* Cancel */, Unpooled.copiedBuffer("this is a test", UTF_8)));

    // Now try to create a stream.
    ChannelFuture future = enqueue(new CreateStreamCommand(grpcHeaders, streamTransportState));
    assertTrue(future.isDone());
    assertFalse(future.isSuccess());
    Status status = Status.fromThrowable(future.cause());
    assertEquals(Status.CANCELLED.getCode(), status.getCode());
    assertEquals("HTTP/2 error code: CANCEL\nReceived Goaway\nthis is a test",
        status.getDescription());
  }

  // This test is not as useful as it looks, because the HTTP/2 Netty code catches and doesn't
  // propagate exceptions during the onGoAwayReceived callback.
  @Test
  public void receivedGoAway_notUtf8() throws Exception {
    // 0xFF is never permitted in UTF-8. 0xF0 should have 3 continuations following, and 0x0a isn't
    // a continuation.
    channelRead(goAwayFrame(0, 11 /* ENHANCE_YOUR_CALM */,
          Unpooled.copiedBuffer(new byte[] {(byte) 0xFF, (byte) 0xF0, (byte) 0x0a})));
  }

  @Test
  public void receivedGoAway_enhanceYourCalmWithoutTooManyPings() throws Exception {
    final AtomicBoolean b = new AtomicBoolean();
    tooManyPingsRunnable = new Runnable() {
      @Override
      public void run() {
        b.set(true);
      }
    };
    setUp();

    channelRead(goAwayFrame(0, 11 /* ENHANCE_YOUR_CALM */,
          Unpooled.copiedBuffer("not_many_pings", UTF_8)));
    assertFalse(b.get());
  }

  @Test
  public void receivedGoAway_enhanceYourCalmWithTooManyPings() throws Exception {
    final AtomicBoolean b = new AtomicBoolean();
    tooManyPingsRunnable = new Runnable() {
      @Override
      public void run() {
        b.set(true);
      }
    };
    setUp();

    channelRead(goAwayFrame(0, 11 /* ENHANCE_YOUR_CALM */,
          Unpooled.copiedBuffer("too_many_pings", UTF_8)));
    assertTrue(b.get());
  }

  @Test
  public void cancelStreamShouldCreateAndThenFailBufferedStream() throws Exception {
    receiveMaxConcurrentStreams(0);
    enqueue(new CreateStreamCommand(grpcHeaders, streamTransportState));
    assertEquals(3, streamTransportState.id());
    cancelStream(Status.CANCELLED);
    verify(streamListener).closed(eq(Status.CANCELLED), same(PROCESSED), any(Metadata.class));
  }

  @Test
  public void channelShutdownShouldCancelBufferedStreams() throws Exception {
    // Force a stream to get added to the pending queue.
    receiveMaxConcurrentStreams(0);
    ChannelFuture future = enqueue(new CreateStreamCommand(grpcHeaders, streamTransportState));

    handler().channelInactive(ctx());
    assertTrue(future.isDone());
    assertFalse(future.isSuccess());
    verify(mockKeepAliveManager, times(1)).onTransportTermination(); // channelInactive
    verifyNoMoreInteractions(mockKeepAliveManager);
  }

  @Test
  public void channelShutdownShouldFailInFlightStreams() throws Exception {
    createStream();

    handler().channelInactive(ctx());
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(streamListener).closed(captor.capture(), same(PROCESSED), notNull(Metadata.class));
    assertEquals(Status.UNAVAILABLE.getCode(), captor.getValue().getCode());
  }

  @Test
  public void connectionWindowShouldBeOverridden() throws Exception {
    flowControlWindow = 1048576; // 1MiB
    setUp();

    Http2Stream connectionStream = connection().connectionStream();
    Http2LocalFlowController localFlowController = connection().local().flowController();
    int actualInitialWindowSize = localFlowController.initialWindowSize(connectionStream);
    int actualWindowSize = localFlowController.windowSize(connectionStream);
    assertEquals(flowControlWindow, actualWindowSize);
    assertEquals(flowControlWindow, actualInitialWindowSize);
    assertEquals(1048576, actualWindowSize);
  }

  @Test
  public void createIncrementsIdsForActualAndBufferdStreams() throws Exception {
    receiveMaxConcurrentStreams(2);
    enqueue(new CreateStreamCommand(grpcHeaders, streamTransportState));
    assertEquals(3, streamTransportState.id());

    streamTransportState = new TransportStateImpl(
        handler(),
        channel().eventLoop(),
        DEFAULT_MAX_MESSAGE_SIZE,
        transportTracer);
    streamTransportState.setListener(streamListener);
    enqueue(new CreateStreamCommand(grpcHeaders, streamTransportState));
    assertEquals(5, streamTransportState.id());

    streamTransportState = new TransportStateImpl(
        handler(),
        channel().eventLoop(),
        DEFAULT_MAX_MESSAGE_SIZE,
        transportTracer);
    streamTransportState.setListener(streamListener);
    enqueue(new CreateStreamCommand(grpcHeaders, streamTransportState));
    assertEquals(7, streamTransportState.id());

    verify(mockKeepAliveManager, times(1)).onTransportActive(); // onStreamActive
    verifyNoMoreInteractions(mockKeepAliveManager);
  }

  @Test
  public void exhaustedStreamsShouldFail() throws Exception {
    streamId = Integer.MAX_VALUE;
    setUp();

    assertNull(lifecycleManager.getShutdownStatus());
    // Create the MAX_INT stream.
    ChannelFuture future = createStream();
    assertTrue(future.isSuccess());

    // This should fail - out of stream IDs.
    future = createStream();
    assertTrue(future.isDone());
    assertFalse(future.isSuccess());
    Status status = lifecycleManager.getShutdownStatus();
    assertNotNull(status);
    assertTrue("status does not reference 'exhausted': " + status,
        status.getDescription().contains("exhausted"));
  }

  @Test
  public void ping() throws Exception {
    PingCallbackImpl callback1 = new PingCallbackImpl();
    assertEquals(0, transportTracer.getStats().keepAlivesSent);
    sendPing(callback1);
    assertEquals(1, transportTracer.getStats().keepAlivesSent);
    // add'l ping will be added as listener to outstanding operation
    PingCallbackImpl callback2 = new PingCallbackImpl();
    sendPing(callback2);
    assertEquals(1, transportTracer.getStats().keepAlivesSent);

    ArgumentCaptor<Long> captor = ArgumentCaptor.forClass(long.class);
    verifyWrite().writePing(eq(ctx()), eq(false), captor.capture(),
        any(ChannelPromise.class));

    // getting a bad ack won't cause the callback to be invoked
    long pingPayload = captor.getValue();
    // to compute bad payload, read the good payload and subtract one
    long badPingPayload = pingPayload - 1;

    channelRead(pingFrame(true, badPingPayload));
    // operation not complete because ack was wrong
    assertEquals(0, callback1.invocationCount);
    assertEquals(0, callback2.invocationCount);

    nanoTime += 10101;

    // reading the proper response should complete the future
    channelRead(pingFrame(true, pingPayload));
    assertEquals(1, callback1.invocationCount);
    assertEquals(10101, callback1.roundTripTime);
    assertNull(callback1.failureCause);
    // callback2 piggy-backed on same operation
    assertEquals(1, callback2.invocationCount);
    assertEquals(10101, callback2.roundTripTime);
    assertNull(callback2.failureCause);

    // now that previous ping is done, next request starts a new operation
    callback1 = new PingCallbackImpl();
    assertEquals(1, transportTracer.getStats().keepAlivesSent);
    sendPing(callback1);
    assertEquals(2, transportTracer.getStats().keepAlivesSent);
    assertEquals(0, callback1.invocationCount);
  }

  @Test
  public void ping_failsWhenChannelCloses() throws Exception {
    PingCallbackImpl callback = new PingCallbackImpl();
    sendPing(callback);
    assertEquals(0, callback.invocationCount);

    handler().channelInactive(ctx());
    // ping failed on channel going inactive
    assertEquals(1, callback.invocationCount);
    assertTrue(callback.failureCause instanceof StatusException);
    assertEquals(Status.Code.UNAVAILABLE,
        ((StatusException) callback.failureCause).getStatus().getCode());
    // A failed ping is still counted
    assertEquals(1, transportTracer.getStats().keepAlivesSent);
  }

  @Test
  public void oustandingUserPingShouldNotInteractWithDataPing() throws Exception {
    createStream();
    handler().setAutoTuneFlowControl(true);

    PingCallbackImpl callback = new PingCallbackImpl();
    assertEquals(0, transportTracer.getStats().keepAlivesSent);
    sendPing(callback);
    assertEquals(1, transportTracer.getStats().keepAlivesSent);
    ArgumentCaptor<Long> captor = ArgumentCaptor.forClass(long.class);
    verifyWrite().writePing(eq(ctx()), eq(false), captor.capture(), any(ChannelPromise.class));
    long payload = captor.getValue();
    channelRead(grpcDataFrame(3, false, contentAsArray()));
    long pingData = handler().flowControlPing().payload();
    channelRead(pingFrame(true, pingData));

    assertEquals(1, handler().flowControlPing().getPingReturn());
    assertEquals(0, callback.invocationCount);

    channelRead(pingFrame(true, payload));

    assertEquals(1, handler().flowControlPing().getPingReturn());
    assertEquals(1, callback.invocationCount);
    assertEquals(1, transportTracer.getStats().keepAlivesSent);
  }

  @Override
  public void dataPingAckIsRecognized() throws Exception {
    super.dataPingAckIsRecognized();
    verify(mockKeepAliveManager, times(1)).onTransportActive(); // onStreamActive
    // onHeadersRead, onDataRead, onPingAckRead
    verify(mockKeepAliveManager, times(3)).onDataReceived();
    verifyNoMoreInteractions(mockKeepAliveManager);
  }

  @Test
  public void exceptionCaughtShouldCloseConnection() throws Exception {
    handler().exceptionCaught(ctx(), new RuntimeException("fake exception"));

    // TODO(nmittler): EmbeddedChannel does not currently invoke the channelInactive processing,
    // so exceptionCaught() will not close streams properly in this test.
    // Once https://github.com/netty/netty/issues/4316 is resolved, we should also verify that
    // any open streams are closed properly.
    assertFalse(channel().isOpen());
  }

  @Override
  protected void makeStream() throws Exception {
    createStream();
    // The tests in NettyServerHandlerTest expect the header to already be read, since they work on
    // both client- and server-side.
    Http2Headers headers = new DefaultHttp2Headers().status(STATUS_OK)
        .set(CONTENT_TYPE_HEADER, CONTENT_TYPE_GRPC);
    ByteBuf headersFrame = headersFrame(3, headers);
    channelRead(headersFrame);
  }

  @CanIgnoreReturnValue
  private ChannelFuture sendPing(PingCallback callback) {
    return enqueue(new SendPingCommand(callback, MoreExecutors.directExecutor()));
  }

  private void receiveMaxConcurrentStreams(int max) throws Exception {
    ByteBuf serializedSettings = serializeSettings(new Http2Settings().maxConcurrentStreams(max));
    channelRead(serializedSettings);
  }

  @CanIgnoreReturnValue
  private ChannelFuture createStream() throws Exception {
    ChannelFuture future = enqueue(new CreateStreamCommand(grpcHeaders, streamTransportState));
    return future;
  }

  @CanIgnoreReturnValue
  private ChannelFuture cancelStream(Status status) throws Exception {
    return enqueue(new CancelClientStreamCommand(streamTransportState, status));
  }

  @Override
  protected NettyClientHandler newHandler() throws Http2Exception {
    Http2Connection connection = new DefaultHttp2Connection(false);

    // Create and close a stream previous to the nextStreamId.
    Http2Stream stream = connection.local().createStream(streamId - 2, true);
    stream.close();

    final Ticker ticker = new Ticker() {
      @Override
      public long read() {
        return nanoTime;
      }
    };
    Supplier<Stopwatch> stopwatchSupplier = new Supplier<Stopwatch>() {
      @Override
      public Stopwatch get() {
        return Stopwatch.createUnstarted(ticker);
      }
    };
    return NettyClientHandler.newHandler(
        connection,
        frameReader(),
        frameWriter(),
        lifecycleManager,
        mockKeepAliveManager,
        flowControlWindow,
        maxHeaderListSize,
        stopwatchSupplier,
        tooManyPingsRunnable,
        transportTracer,
        Attributes.EMPTY,
        "someauthority");
  }

  @Override
  protected WriteQueue initWriteQueue() {
    handler().startWriteQueue(channel());
    return handler().getWriteQueue();
  }

  private AsciiString as(String string) {
    return new AsciiString(string);
  }

  private static class PingCallbackImpl implements ClientTransport.PingCallback {
    int invocationCount;
    long roundTripTime;
    Throwable failureCause;

    @Override
    public void onSuccess(long roundTripTimeNanos) {
      invocationCount++;
      this.roundTripTime = roundTripTimeNanos;
    }

    @Override
    public void onFailure(Throwable cause) {
      invocationCount++;
      this.failureCause = cause;
    }
  }

  private static class TransportStateImpl extends NettyClientStream.TransportState {
    public TransportStateImpl(
        NettyClientHandler handler,
        EventLoop eventLoop,
        int maxMessageSize,
        TransportTracer transportTracer) {
      super(handler, eventLoop, maxMessageSize, StatsTraceContext.NOOP, transportTracer);
    }

    @Override
    protected Status statusFromFailedFuture(ChannelFuture f) {
      return Utils.statusFromThrowable(f.cause());
    }
  }
}
