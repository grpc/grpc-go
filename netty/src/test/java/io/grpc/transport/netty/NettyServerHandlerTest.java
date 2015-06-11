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

import static com.google.common.base.Charsets.UTF_8;
import static io.grpc.transport.netty.Utils.CONTENT_TYPE_GRPC;
import static io.grpc.transport.netty.Utils.CONTENT_TYPE_HEADER;
import static io.grpc.transport.netty.Utils.HTTP_METHOD;
import static io.grpc.transport.netty.Utils.TE_HEADER;
import static io.grpc.transport.netty.Utils.TE_TRAILERS;
import static io.netty.handler.codec.http2.Http2CodecUtil.DEFAULT_WINDOW_SIZE;
import static io.netty.handler.codec.http2.Http2CodecUtil.toByteBuf;
import static io.netty.handler.codec.http2.Http2Exception.connectionError;
import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.mockito.Matchers.any;
import static org.mockito.Mockito.atLeastOnce;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.io.ByteStreams;

import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.Status.Code;
import io.grpc.transport.MessageFramer;
import io.grpc.transport.ServerStream;
import io.grpc.transport.ServerStreamListener;
import io.grpc.transport.ServerTransportListener;
import io.grpc.transport.WritableBuffer;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufAllocator;
import io.netty.buffer.Unpooled;
import io.netty.buffer.UnpooledByteBufAllocator;
import io.netty.channel.Channel;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPromise;
import io.netty.channel.DefaultChannelPromise;
import io.netty.handler.codec.http2.DefaultHttp2Connection;
import io.netty.handler.codec.http2.DefaultHttp2FrameReader;
import io.netty.handler.codec.http2.DefaultHttp2FrameWriter;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.Http2CodecUtil;
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2Error;
import io.netty.handler.codec.http2.Http2FlowController;
import io.netty.handler.codec.http2.Http2FrameReader;
import io.netty.handler.codec.http2.Http2FrameWriter;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2Settings;
import io.netty.handler.codec.http2.Http2Stream;
import io.netty.util.AsciiString;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

import java.io.ByteArrayInputStream;
import java.io.InputStream;
import java.util.List;

/** Unit tests for {@link NettyServerHandler}. */
@RunWith(JUnit4.class)
public class NettyServerHandlerTest extends NettyHandlerTestBase {

  private static final int STREAM_ID = 3;
  private static final byte[] CONTENT = "hello world".getBytes(UTF_8);

  @Mock
  private ServerTransportListener transportListener;

  @Mock
  private ServerStreamListener streamListener;

  private NettyServerStream stream;

  private NettyServerHandler handler;
  private WriteQueue writeQueue;

  /** Set up for test. */
  @Before
  public void setUp() throws Exception {
    MockitoAnnotations.initMocks(this);

    when(transportListener.streamCreated(any(ServerStream.class),
        any(String.class),
        any(Metadata.Headers.class)))
        .thenReturn(streamListener);
    handler = newHandler(transportListener);
    frameWriter = new DefaultHttp2FrameWriter();
    frameReader = new DefaultHttp2FrameReader();

    when(channel.isActive()).thenReturn(true);
    mockContext();
    mockFuture(true);
    // Delegate writes on the channel to the handler
    doAnswer(new Answer() {
      @Override
      public Object answer(InvocationOnMock invocation) throws Throwable {
        handler.write(ctx, invocation.getArguments()[0], promise);
        return future;
      }
    }).when(channel).write(any());
    doAnswer(new Answer() {
      @Override
      public Object answer(InvocationOnMock invocation) throws Throwable {
        handler.write(ctx, invocation.getArguments()[0],
            (ChannelPromise) invocation.getArguments()[1]);
        return future;
      }
    }).when(channel).write(any(), any(ChannelPromise.class));

    when(channel.alloc()).thenReturn(UnpooledByteBufAllocator.DEFAULT);

    // Simulate activation of the handler to force writing of the initial settings
    handler.handlerAdded(ctx);
    writeQueue = handler.getWriteQueue();

    // Simulate receipt of the connection preface
    handler.channelRead(ctx, Http2CodecUtil.connectionPrefaceBuf());
    // Simulate receipt of initial remote settings.
    ByteBuf serializedSettings = serializeSettings(new Http2Settings());
    handler.channelRead(ctx, serializedSettings);

    // Reset the context to clear any interactions resulting from the HTTP/2
    // connection preface handshake.
    mockContext();
    mockFuture(promise, true);
  }

  @Test
  public void sendFrameShouldSucceed() throws Exception {
    createStream();
    ByteBuf content = Unpooled.copiedBuffer(CONTENT);

    // Send a frame and verify that it was written.
    writeQueue.enqueue(new SendGrpcFrameCommand(stream, content, false), true);
    verify(promise, never()).setFailure(any(Throwable.class));
    ByteBuf bufWritten = captureWrite(ctx);
    verify(channel, times(1)).flush();
    int startIndex = bufWritten.readerIndex() + Http2CodecUtil.FRAME_HEADER_LENGTH;
    int length = bufWritten.writerIndex() - startIndex;
    ByteBuf writtenContent = bufWritten.slice(startIndex, length);
    assertEquals(content, writtenContent);
  }

  @Test
  public void inboundDataWithEndStreamShouldForwardToStreamListener() throws Exception {
    inboundDataShouldForwardToStreamListener(true);
  }

  @Test
  public void inboundDataShouldForwardToStreamListener() throws Exception {
    inboundDataShouldForwardToStreamListener(false);
  }

  private void inboundDataShouldForwardToStreamListener(boolean endStream) throws Exception {
    createStream();
    stream.request(1);

    // Create a data frame and then trigger the handler to read it.
    ByteBuf frame = dataFrame(STREAM_ID, endStream);
    handler.channelRead(ctx, frame);
    ArgumentCaptor<InputStream> captor = ArgumentCaptor.forClass(InputStream.class);
    verify(streamListener).messageRead(captor.capture());
    assertArrayEquals(CONTENT, ByteStreams.toByteArray(captor.getValue()));

    if (endStream) {
      verify(streamListener).halfClosed();
    }
    verify(streamListener, atLeastOnce()).onReady();
    verifyNoMoreInteractions(streamListener);
  }

  @Test
  public void clientHalfCloseShouldForwardToStreamListener() throws Exception {
    createStream();
    stream.request(1);

    handler.channelRead(ctx, emptyGrpcFrame(STREAM_ID, true));
    ArgumentCaptor<InputStream> captor = ArgumentCaptor.forClass(InputStream.class);
    verify(streamListener).messageRead(captor.capture());
    assertArrayEquals(new byte[0], ByteStreams.toByteArray(captor.getValue()));
    verify(streamListener).halfClosed();
    verify(streamListener, atLeastOnce()).onReady();
    verifyNoMoreInteractions(streamListener);
  }

  @Test
  public void clientCancelShouldForwardToStreamListener() throws Exception {
    createStream();

    handler.channelRead(ctx, rstStreamFrame(STREAM_ID, (int) Http2Error.CANCEL.code()));
    verify(streamListener, never()).messageRead(any(InputStream.class));
    verify(streamListener).closed(Status.CANCELLED);
    verify(streamListener, atLeastOnce()).onReady();
    verifyNoMoreInteractions(streamListener);
  }

  @Test
  public void streamErrorShouldNotCloseChannel() throws Exception {
    createStream();
    stream.request(1);

    // When a DATA frame is read, throw an exception. It will be converted into an
    // Http2StreamException.
    RuntimeException e = new RuntimeException("Fake Exception");
    doThrow(e).when(streamListener).messageRead(any(InputStream.class));

    // Read a DATA frame to trigger the exception.
    handler.channelRead(ctx, emptyGrpcFrame(STREAM_ID, true));

    // Verify that the context was NOT closed.
    verify(ctx, never()).close();

    // Verify the stream was closed.
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(streamListener).closed(captor.capture());
    assertEquals(e, captor.getValue().asException().getCause());
    assertEquals(Code.UNKNOWN, captor.getValue().getCode());
  }

  @Test
  public void connectionErrorShouldCloseChannel() throws Exception {
    createStream();

    // Read a bad frame to trigger the exception.
    handler.channelRead(ctx, badFrame());

    // Verify the expected GO_AWAY frame was written.
    Exception e = connectionError(Http2Error.PROTOCOL_ERROR,
        "Frame length 0 incorrect size for ping.");
    ByteBuf expected =
        goAwayFrame(STREAM_ID, (int) Http2Error.FRAME_SIZE_ERROR.code(), toByteBuf(ctx, e));
    ByteBuf actual = captureWrite(ctx);
    assertEquals(expected, actual);

    // Verify that the context was closed.
    verify(ctx).close();
  }

  @Test
  public void closeShouldCloseChannel() throws Exception {
    handler.close(ctx, promise);

    // Verify the expected GO_AWAY frame was written.
    ByteBuf expected = goAwayFrame(0, (int) Http2Error.NO_ERROR.code(), Unpooled.EMPTY_BUFFER);
    ByteBuf actual = captureWrite(ctx);
    assertEquals(expected, actual);

    // Verify that the context was closed.
    verify(ctx).close(promise);
  }

  @Test
  public void shouldAdvertiseMaxConcurrentStreams() throws Exception {
    final int maxConcurrentStreams = 314;
    channel = mock(Channel.class);
    Http2FrameWriter frameWriter = mock(Http2FrameWriter.class);
    Http2Connection connection = new DefaultHttp2Connection(true);
    handler =
        new NettyServerHandler(transportListener, connection, new DefaultHttp2FrameReader(),
            frameWriter, maxConcurrentStreams, DEFAULT_WINDOW_SIZE, DEFAULT_WINDOW_SIZE);

    when(channel.isActive()).thenReturn(true);
    mockContext();
    mockFuture(true);

    when(frameWriter.writeSettings(
        any(ChannelHandlerContext.class), any(Http2Settings.class), any(ChannelPromise.class)))
          .thenReturn(new DefaultChannelPromise(channel).setSuccess());
    when(channel.alloc()).thenReturn(UnpooledByteBufAllocator.DEFAULT);

    // Simulate activation of the handler to force writing of the initial settings
    handler.handlerAdded(ctx);
    ArgumentCaptor<Http2Settings> captor = ArgumentCaptor.forClass(Http2Settings.class);
    verify(frameWriter, times(2)).writeSettings(
        any(ChannelHandlerContext.class), captor.capture(), any(ChannelPromise.class));

    List<Http2Settings> settings = captor.getAllValues();
    assertEquals(maxConcurrentStreams, settings.get(1).maxConcurrentStreams().intValue());
  }

  @Test
  public void connectionWindowShouldBeOverridden() throws Exception {
    int connectionWindow = 1048576; // 1MiB
    handler = newHandler(transportListener, connectionWindow, DEFAULT_WINDOW_SIZE);
    handler.handlerAdded(ctx);
    Http2Stream connectionStream = handler.connection().connectionStream();
    Http2FlowController localFlowController = handler.connection().local().flowController();
    int actualInitialWindowSize = localFlowController.initialWindowSize(connectionStream);
    int actualWindowSize = localFlowController.windowSize(connectionStream);
    assertEquals(connectionWindow, actualWindowSize);
    assertEquals(connectionWindow, actualInitialWindowSize);
  }

  private void createStream() throws Exception {
    Http2Headers headers = new DefaultHttp2Headers()
        .method(HTTP_METHOD)
        .set(CONTENT_TYPE_HEADER, CONTENT_TYPE_GRPC)
        .set(TE_HEADER, TE_TRAILERS)
        .path(new AsciiString("/foo.bar"));
    ByteBuf headersFrame = headersFrame(STREAM_ID, headers);
    handler.channelRead(ctx, headersFrame);

    ArgumentCaptor<NettyServerStream> streamCaptor =
        ArgumentCaptor.forClass(NettyServerStream.class);
    ArgumentCaptor<String> methodCaptor = ArgumentCaptor.forClass(String.class);
    verify(transportListener).streamCreated(streamCaptor.capture(), methodCaptor.capture(),
        any(Metadata.Headers.class));
    stream = streamCaptor.getValue();
  }

  private ByteBuf dataFrame(int streamId, boolean endStream) {
    final ByteBuf compressionFrame = Unpooled.buffer(CONTENT.length);
    MessageFramer framer = new MessageFramer(new MessageFramer.Sink() {
      @Override
      public void deliverFrame(WritableBuffer frame, boolean endOfStream, boolean flush) {
        if (frame != null) {
          ByteBuf bytebuf = ((NettyWritableBuffer) frame).bytebuf();
          compressionFrame.writeBytes(bytebuf);
        }
      }
    }, new NettyWritableBufferAllocator(ByteBufAllocator.DEFAULT));
    framer.writePayload(new ByteArrayInputStream(CONTENT));
    framer.flush();
    if (endStream) {
      framer.close();
    }
    ChannelHandlerContext ctx = newContext();
    frameWriter.writeData(ctx, streamId, compressionFrame, 0, endStream, newPromise());
    return captureWrite(ctx);
  }

  private ByteBuf emptyGrpcFrame(int streamId, boolean endStream) throws Exception {
    ChannelHandlerContext ctx = newContext();
    ByteBuf buf = NettyTestUtil.messageFrame("");
    frameWriter.writeData(ctx, streamId, buf, 0, endStream, newPromise());
    return captureWrite(ctx);
  }

  private ByteBuf badFrame() throws Exception {
    ChannelHandlerContext ctx = newContext();
    // Write an empty PING frame - this is invalid.
    frameWriter.writePing(ctx, false, Unpooled.EMPTY_BUFFER, newPromise());
    return captureWrite(ctx);
  }

  private static NettyServerHandler newHandler(ServerTransportListener transportListener,
                                               int connectionWindowSize,
                                               int streamWindowSize) {
    Http2Connection connection = new DefaultHttp2Connection(true);
    Http2FrameReader frameReader = new DefaultHttp2FrameReader();
    Http2FrameWriter frameWriter = new DefaultHttp2FrameWriter();
    return new NettyServerHandler(transportListener, connection, frameReader, frameWriter,
        Integer.MAX_VALUE, connectionWindowSize, streamWindowSize);
  }

  private static NettyServerHandler newHandler(ServerTransportListener transportListener) {
    return newHandler(transportListener, DEFAULT_WINDOW_SIZE, DEFAULT_WINDOW_SIZE);
  }
}
