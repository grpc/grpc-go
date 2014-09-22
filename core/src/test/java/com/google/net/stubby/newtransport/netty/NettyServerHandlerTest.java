package com.google.net.stubby.newtransport.netty;

import static com.google.net.stubby.newtransport.netty.Utils.CONTENT_TYPE_HEADER;
import static com.google.net.stubby.newtransport.netty.Utils.CONTENT_TYPE_PROTORPC;
import static com.google.net.stubby.newtransport.netty.Utils.HTTP_METHOD;
import static java.nio.charset.StandardCharsets.UTF_8;
import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyInt;
import static org.mockito.Matchers.eq;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.io.ByteStreams;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;
import com.google.net.stubby.newtransport.Framer;
import com.google.net.stubby.newtransport.MessageFramer;
import com.google.net.stubby.newtransport.ServerStream;
import com.google.net.stubby.newtransport.ServerStreamListener;
import com.google.net.stubby.newtransport.ServerTransportListener;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.Unpooled;
import io.netty.buffer.UnpooledByteBufAllocator;
import io.netty.channel.ChannelHandlerContext;
import io.netty.handler.codec.AsciiString;
import io.netty.handler.codec.http2.DefaultHttp2Connection;
import io.netty.handler.codec.http2.DefaultHttp2FrameReader;
import io.netty.handler.codec.http2.DefaultHttp2FrameWriter;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.DefaultHttp2InboundFlowController;
import io.netty.handler.codec.http2.DefaultHttp2OutboundFlowController;
import io.netty.handler.codec.http2.Http2CodecUtil;
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2Error;
import io.netty.handler.codec.http2.Http2FrameReader;
import io.netty.handler.codec.http2.Http2FrameWriter;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2OutboundFlowController;
import io.netty.handler.codec.http2.Http2Settings;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.io.ByteArrayInputStream;
import java.io.InputStream;
import java.nio.ByteBuffer;

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

  @Before
  public void setup() throws Exception {
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

    when(channel.alloc()).thenReturn(UnpooledByteBufAllocator.DEFAULT);

    // Simulate activation of the handler to force writing of the initial settings
    handler.handlerAdded(ctx);

    // Simulate receipt of the connection preface
    handler.channelRead(ctx, Http2CodecUtil.connectionPrefaceBuf());
    // Simulate receipt of initial remote settings.
    ByteBuf serializedSettings = serializeSettings(new Http2Settings());
    handler.channelRead(ctx, serializedSettings);

    // Reset the context to clear any interactions resulting from the HTTP/2
    // connection preface handshake.
    mockContext();
  }

  @Test
  public void sendFrameShouldSucceed() throws Exception {
    createStream();
    ByteBuf content = Unpooled.copiedBuffer(CONTENT);

    // Send a frame and verify that it was written.
    handler.write(ctx, new SendGrpcFrameCommand(stream.id(), content, false), promise);
    verify(promise, never()).setFailure(any(Throwable.class));
    verify(ctx).write(any(ByteBuf.class), eq(promise));
    assertEquals(0, content.refCnt());
  }

  @Test
  public void inboundDataShouldForwardToStreamListener() throws Exception {
    inboundDataShouldForwardToStreamListener(false);
  }

  @Test
  public void inboundDataWithEndStreamShouldForwardToStreamListener() throws Exception {
    inboundDataShouldForwardToStreamListener(true);
  }

  private void inboundDataShouldForwardToStreamListener(boolean endStream) throws Exception {
    createStream();

    // Create a data frame and then trigger the handler to read it.
    ByteBuf frame = dataFrame(STREAM_ID, endStream);
    handler.channelRead(ctx, frame);
    ArgumentCaptor<InputStream> captor = ArgumentCaptor.forClass(InputStream.class);
    verify(streamListener).messageRead(captor.capture(), eq(CONTENT.length));
    assertArrayEquals(CONTENT, ByteStreams.toByteArray(captor.getValue()));

    if (endStream) {
      verify(streamListener).halfClosed();
    }
    verifyNoMoreInteractions(streamListener);
  }

  @Test
  public void clientHalfCloseShouldForwardToStreamListener() throws Exception {
    createStream();

    handler.channelRead(ctx, emptyDataFrame(STREAM_ID, true));
    verify(streamListener, never()).messageRead(any(InputStream.class), anyInt());
    verify(streamListener).halfClosed();
    verifyNoMoreInteractions(streamListener);
  }

  @Test
  public void clientCancelShouldForwardToStreamListener() throws Exception {
    createStream();

    handler.channelRead(ctx, rstStreamFrame(STREAM_ID, Http2Error.CANCEL.code()));
    verify(streamListener, never()).messageRead(any(InputStream.class), anyInt());
    verify(streamListener).closed(Status.CANCELLED);
    verifyNoMoreInteractions(streamListener);
  }

  private void createStream() throws Exception {
    Http2Headers headers = new DefaultHttp2Headers()
        .method(HTTP_METHOD)
        .set(CONTENT_TYPE_HEADER, CONTENT_TYPE_PROTORPC)
        .path(new AsciiString("/foo.bar"));
    ByteBuf headersFrame = headersFrame(STREAM_ID, headers);
    handler.channelRead(ctx, headersFrame);
    ArgumentCaptor<NettyServerStream> streamCaptor =
        ArgumentCaptor.forClass(NettyServerStream.class);
    @SuppressWarnings("rawtypes")
    ArgumentCaptor<String> methodCaptor = ArgumentCaptor.forClass(String.class);
    verify(transportListener).streamCreated(streamCaptor.capture(), methodCaptor.capture(),
        any(Metadata.Headers.class));
    stream = streamCaptor.getValue();
  }

  private ByteBuf dataFrame(int streamId, boolean endStream) {
    final ByteBuf compressionFrame = Unpooled.buffer(CONTENT.length);
    MessageFramer framer = new MessageFramer(new Framer.Sink<ByteBuffer>() {
      @Override
      public void deliverFrame(ByteBuffer frame, boolean endOfStream) {
        compressionFrame.writeBytes(frame);
      }
    }, 1000);
    framer.writePayload(new ByteArrayInputStream(CONTENT), CONTENT.length);
    framer.flush();
    if (endStream) {
      framer.close();
    }
    ChannelHandlerContext ctx = newContext();
    frameWriter.writeData(ctx, streamId, compressionFrame, 0, endStream, newPromise());
    return captureWrite(ctx);
  }

  private ByteBuf emptyDataFrame(int streamId, boolean endStream) {
    ChannelHandlerContext ctx = newContext();
    frameWriter.writeData(ctx, streamId, Unpooled.EMPTY_BUFFER, 0, endStream, newPromise());
    return captureWrite(ctx);
  }

  private static NettyServerHandler newHandler(ServerTransportListener transportListener) {
    Http2Connection connection = new DefaultHttp2Connection(true);
    Http2FrameReader frameReader = new DefaultHttp2FrameReader();
    Http2FrameWriter frameWriter = new DefaultHttp2FrameWriter();
    DefaultHttp2InboundFlowController inboundFlow =
        new DefaultHttp2InboundFlowController(connection, frameWriter);
    Http2OutboundFlowController outboundFlow =
        new DefaultHttp2OutboundFlowController(connection, frameWriter);
    return new NettyServerHandler(transportListener,
        connection,
        frameReader,
        frameWriter,
        inboundFlow,
        outboundFlow);
  }
}
