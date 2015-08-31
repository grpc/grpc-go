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
import static io.grpc.netty.Utils.CONTENT_TYPE_GRPC;
import static io.grpc.netty.Utils.CONTENT_TYPE_HEADER;
import static io.grpc.netty.Utils.HTTPS;
import static io.grpc.netty.Utils.HTTP_METHOD;
import static io.grpc.netty.Utils.STATUS_OK;
import static io.grpc.netty.Utils.TE_HEADER;
import static io.grpc.netty.Utils.TE_TRAILERS;
import static io.netty.handler.codec.http2.Http2CodecUtil.DEFAULT_PRIORITY_WEIGHT;
import static io.netty.handler.codec.http2.Http2CodecUtil.DEFAULT_WINDOW_SIZE;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.notNull;
import static org.mockito.Mockito.calls;
import static org.mockito.Mockito.inOrder;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.google.common.base.Ticker;
import com.google.common.util.concurrent.MoreExecutors;

import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.StatusException;
import io.grpc.internal.ClientTransport;
import io.grpc.internal.ClientTransport.PingCallback;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.Unpooled;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPromise;
import io.netty.handler.codec.http2.DefaultHttp2Connection;
import io.netty.handler.codec.http2.DefaultHttp2ConnectionEncoder;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2Error;
import io.netty.handler.codec.http2.Http2Exception;
import io.netty.handler.codec.http2.Http2FlowController;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2Settings;
import io.netty.handler.codec.http2.Http2Stream;
import io.netty.util.AsciiString;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.InOrder;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.util.concurrent.TimeUnit;

/**
 * Tests for {@link NettyClientHandler}.
 */
@RunWith(JUnit4.class)
public class NettyClientHandlerTest extends NettyHandlerTestBase<NettyClientHandler> {

  // TODO(zhangkun83): mocking concrete classes is not safe. Consider making NettyClientStream an
  // interface.
  @Mock
  private NettyClientStream stream;
  private Http2Headers grpcHeaders;
  private long nanoTime; // backs a ticker, for testing ping round-trip time measurement
  private int flowControlWindow = DEFAULT_WINDOW_SIZE;
  private int streamId = 3;

  /**
   * Set up for test.
   */
  @Before
  public void setUp() throws Exception {
    MockitoAnnotations.initMocks(this);

    initChannel();

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
    ChannelFuture createFuture = enqueue(new CreateStreamCommand(grpcHeaders, stream));
    verify(stream).id(eq(3));
    when(stream.id()).thenReturn(3);
    // Cancel the stream.
    cancelStream(Status.CANCELLED);

    assertTrue(createFuture.isSuccess());
    verify(stream).transportReportStatus(eq(Status.CANCELLED), eq(true),
        any(Metadata.class));
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
    connection().local().maxActiveStreams(0);

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
    ChannelFuture future = enqueue(new SendGrpcFrameCommand(stream, content(), true));

    assertTrue(future.isSuccess());
    verifyWrite().writeData(eq(ctx()), eq(3), eq(content()), eq(0), eq(true),
        any(ChannelPromise.class));
  }

  @Test
  public void sendForUnknownStreamShouldFail() throws Exception {
    when(stream.id()).thenReturn(3);
    ChannelFuture future = enqueue(new SendGrpcFrameCommand(stream, content(), true));
    assertTrue(future.isDone());
    assertFalse(future.isSuccess());
  }

  @Test
  public void inboundHeadersShouldForwardToStream() throws Exception {
    createStream();

    // Read a headers frame first.
    Http2Headers headers = new DefaultHttp2Headers().status(STATUS_OK)
        .set(CONTENT_TYPE_HEADER, CONTENT_TYPE_GRPC);
    ByteBuf headersFrame = headersFrame(3, headers);
    channelRead(headersFrame);
    verify(stream).transportHeadersReceived(headers, false);
  }

  @Test
  public void inboundDataShouldForwardToStream() throws Exception {
    createStream();

    // Create a data frame and then trigger the handler to read it.
    // Need to retain to simulate what is done by the stream.
    ByteBuf frame = dataFrame(3, false).retain();
    channelRead(frame);
    verify(stream).transportDataReceived(eq(content()), eq(false));
  }

  @Test
  public void receivedGoAwayShouldCancelBufferedStream() throws Exception {
    // Force the stream to be buffered.
    receiveMaxConcurrentStreams(0);
    ChannelFuture future = enqueue(new CreateStreamCommand(grpcHeaders, stream));
    channelRead(goAwayFrame(0));
    assertTrue(future.isDone());
    assertFalse(future.isSuccess());
    verify(stream).transportReportStatus(any(Status.class), eq(false),
        notNull(Metadata.class));
  }

  @Test
  public void receivedGoAwayShouldFailUnknownStreams() throws Exception {
    enqueue(new CreateStreamCommand(grpcHeaders, stream));

    // Read a GOAWAY that indicates our stream was never processed by the server.
    channelRead(goAwayFrame(0, 8 /* Cancel */, Unpooled.copiedBuffer("this is a test", UTF_8)));
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(stream).transportReportStatus(captor.capture(), eq(false),
        notNull(Metadata.class));
    assertEquals(Status.CANCELLED.getCode(), captor.getValue().getCode());
    assertEquals("HTTP/2 error code: CANCEL\nthis is a test",
        captor.getValue().getDescription());
  }

  @Test
  public void receivedGoAwayShouldFailUnknownBufferedStreams() throws Exception {
    receiveMaxConcurrentStreams(0);

    enqueue(new CreateStreamCommand(grpcHeaders, stream));

    // Read a GOAWAY that indicates our stream was never processed by the server.
    channelRead(goAwayFrame(0, 8 /* Cancel */, Unpooled.copiedBuffer("this is a test", UTF_8)));
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(stream).transportReportStatus(captor.capture(), eq(false),
        notNull(Metadata.class));
    assertEquals(Status.CANCELLED.getCode(), captor.getValue().getCode());
    assertEquals("HTTP/2 error code: CANCEL\nthis is a test",
        captor.getValue().getDescription());
  }

  @Test
  public void cancelStreamShouldCreateAndThenFailBufferedStream() throws Exception {
    receiveMaxConcurrentStreams(0);
    enqueue(new CreateStreamCommand(grpcHeaders, stream));
    verify(stream).id(3);
    when(stream.id()).thenReturn(3);
    cancelStream(Status.CANCELLED);
    verify(stream).transportReportStatus(eq(Status.CANCELLED), eq(true),
        any(Metadata.class));
  }

  @Test
  public void channelShutdownShouldCancelBufferedStreams() throws Exception {
    // Force a stream to get added to the pending queue.
    receiveMaxConcurrentStreams(0);
    ChannelFuture future = enqueue(new CreateStreamCommand(grpcHeaders, stream));

    handler().channelInactive(ctx());
    assertTrue(future.isDone());
    assertFalse(future.isSuccess());
  }

  @Test
  public void channelShutdownShouldFailInFlightStreams() throws Exception {
    createStream();

    handler().channelInactive(ctx());
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    InOrder inOrder = inOrder(stream);
    inOrder.verify(stream, calls(1)).transportReportStatus(captor.capture(), eq(false),
        notNull(Metadata.class));
    assertEquals(Status.UNAVAILABLE.getCode(), captor.getValue().getCode());
  }

  @Test
  public void connectionWindowShouldBeOverridden() throws Exception {
    flowControlWindow = 1048576; // 1MiB
    setUp();

    Http2Stream connectionStream = connection().connectionStream();
    Http2FlowController localFlowController = connection().local().flowController();
    int actualInitialWindowSize = localFlowController.initialWindowSize(connectionStream);
    int actualWindowSize = localFlowController.windowSize(connectionStream);
    assertEquals(flowControlWindow, actualWindowSize);
    assertEquals(flowControlWindow, actualInitialWindowSize);
  }

  @Test
  public void createIncrementsIdsForActualAndBufferdStreams() throws Exception {
    receiveMaxConcurrentStreams(2);
    CreateStreamCommand command = new CreateStreamCommand(grpcHeaders, stream);
    enqueue(command);
    verify(stream).id(eq(3));
    enqueue(command);
    verify(stream).id(eq(5));
    enqueue(command);
    verify(stream).id(eq(7));
  }

  @Test
  public void exhaustedStreamsShouldFail() throws Exception {
    streamId = Integer.MAX_VALUE;
    setUp();

    // Create the MAX_INT stream.
    ChannelFuture future = createStream();
    assertTrue(future.isSuccess());

    // This should fail - out of stream IDs.
    future = createStream();
    assertTrue(future.isDone());
    assertFalse(future.isSuccess());
  }

  @Test
  public void ping() throws Exception {
    PingCallbackImpl callback1 = new PingCallbackImpl();
    sendPing(callback1);
    // add'l ping will be added as listener to outstanding operation
    PingCallbackImpl callback2 = new PingCallbackImpl();
    sendPing(callback2);

    ArgumentCaptor<ByteBuf> captor = ArgumentCaptor.forClass(ByteBuf.class);
    verifyWrite().writePing(eq(ctx()), eq(false), captor.capture(),
        any(ChannelPromise.class));

    // getting a bad ack won't cause the callback to be invoked
    ByteBuf pingPayload = captor.getValue();
    // to compute bad payload, read the good payload and subtract one
    ByteBuf badPingPayload = Unpooled.copyLong(pingPayload.slice().readLong() - 1);

    channelRead(pingFrame(true, badPingPayload));
    // operation not complete because ack was wrong
    assertEquals(0, callback1.invocationCount);
    assertEquals(0, callback2.invocationCount);

    nanoTime += TimeUnit.MICROSECONDS.toNanos(10101);

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
    sendPing(callback1);
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
  }

  private ChannelFuture sendPing(PingCallback callback) {
    return enqueue(new SendPingCommand(callback, MoreExecutors.directExecutor()));
  }

  private void receiveMaxConcurrentStreams(int max) throws Exception {
    ByteBuf serializedSettings = serializeSettings(new Http2Settings().maxConcurrentStreams(max));
    channelRead(serializedSettings);
  }

  private ByteBuf dataFrame(int streamId, boolean endStream) {
    return dataFrame(streamId, endStream, content());
  }

  private ChannelFuture createStream() throws Exception {
    ChannelFuture future = enqueue(new CreateStreamCommand(grpcHeaders, stream));
    when(stream.id()).thenReturn(3);
    return future;
  }

  private ChannelFuture cancelStream(Status status) throws Exception {
    return enqueue(new CancelClientStreamCommand(stream, status));
  }

  @Override
  protected NettyClientHandler newHandler() throws Http2Exception {
    Http2Connection connection = new DefaultHttp2Connection(false);

    // Create and close a stream previous to the nextStreamId.
    Http2Stream stream = connection.local().createStream(streamId - 2, true);
    stream.close();

    BufferingHttp2ConnectionEncoder encoder = new BufferingHttp2ConnectionEncoder(
        new DefaultHttp2ConnectionEncoder(connection, frameWriter()));
    Ticker ticker = new Ticker() {
      @Override
      public long read() {
        return nanoTime;
      }
    };
    return new NettyClientHandler(encoder, connection, frameReader(), flowControlWindow, ticker);
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
    public void pingAcknowledged(long roundTripTimeMicros) {
      invocationCount++;
      this.roundTripTime = roundTripTimeMicros;
    }

    @Override
    public void pingFailed(Throwable cause) {
      invocationCount++;
      this.failureCause = cause;
    }
  }
}
