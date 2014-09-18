package com.google.net.stubby.newtransport.netty;

import static com.google.net.stubby.newtransport.netty.Utils.CONTENT_TYPE_HEADER;
import static com.google.net.stubby.newtransport.netty.Utils.CONTENT_TYPE_PROTORPC;
import static com.google.net.stubby.newtransport.netty.Utils.HTTPS;
import static com.google.net.stubby.newtransport.netty.Utils.HTTP_METHOD;
import static com.google.net.stubby.newtransport.netty.Utils.STATUS_OK;
import static java.nio.charset.StandardCharsets.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.notNull;
import static org.mockito.Mockito.calls;
import static org.mockito.Mockito.inOrder;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;
import com.google.net.stubby.newtransport.StreamState;
import com.google.net.stubby.transport.Transport;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.Unpooled;
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
import org.mockito.InOrder;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/**
 * Tests for {@link NettyClientHandler}.
 */
@RunWith(JUnit4.class)
public class NettyClientHandlerTest extends NettyHandlerTestBase {

  private NettyClientHandler handler;

  // TODO(user): mocking concrete classes is not safe. Consider making NettyClientStream an
  // interface.
  @Mock
  private NettyClientStream stream;

  private ByteBuf content;
  private Http2Headers grpcHeaders;

  @Before
  public void setup() throws Exception {
    MockitoAnnotations.initMocks(this);

    frameWriter = new DefaultHttp2FrameWriter();
    frameReader = new DefaultHttp2FrameReader();
    handler = newHandler();
    content = Unpooled.copiedBuffer("hello world", UTF_8);

    when(channel.isActive()).thenReturn(true);
    mockContext();
    mockFuture(true);

    grpcHeaders = new DefaultHttp2Headers()
        .scheme(HTTPS)
        .authority(as("www.fake.com"))
        .path(as("/fakemethod"))
        .method(HTTP_METHOD)
        .add(as("auth"), as("sometoken"))
        .add(CONTENT_TYPE_HEADER, CONTENT_TYPE_PROTORPC);

    when(stream.state()).thenReturn(StreamState.OPEN);

    // Simulate activation of the handler to force writing of the initial settings
    handler.handlerAdded(ctx);

    // Simulate receipt of initial remote settings.
    ByteBuf serializedSettings = serializeSettings(new Http2Settings());
    handler.channelRead(ctx, serializedSettings);

    // Reset the context to clear any interactions resulting from the HTTP/2
    // connection preface handshake.
    mockContext();
  }

  @Test
  public void createStreamShouldSucceed() throws Exception {
    handler.write(ctx, new CreateStreamCommand(grpcHeaders, stream),
        promise);
    verify(promise).setSuccess();
    verify(stream).id(eq(3));

    // Capture and verify the written headers frame.
    ByteBuf serializedHeaders = captureWrite(ctx);
    ChannelHandlerContext ctx = newContext();
    frameReader.readFrame(ctx, serializedHeaders, frameListener);
    ArgumentCaptor<Http2Headers> captor = ArgumentCaptor.forClass(Http2Headers.class);
    verify(frameListener).onHeadersRead(eq(ctx),
        eq(3),
        captor.capture(),
        eq(0),
        eq(Http2CodecUtil.DEFAULT_PRIORITY_WEIGHT),
        eq(false),
        eq(0),
        eq(false));
    Http2Headers headers = captor.getValue();
    assertEquals("https", headers.scheme().toString());
    assertEquals(HTTP_METHOD, headers.method());
    assertEquals("www.fake.com", headers.authority().toString());
    assertEquals(CONTENT_TYPE_PROTORPC, headers.get(CONTENT_TYPE_HEADER));
    assertEquals("/fakemethod", headers.path().toString());
    assertEquals("sometoken", headers.get(as("auth")).toString());
  }

  @Test
  public void cancelShouldSucceed() throws Exception {
    createStream();

    handler.write(ctx, new CancelStreamCommand(stream), promise);

    ByteBuf expected = rstStreamFrame(3, Http2Error.CANCEL.code());
    verify(ctx).write(eq(expected), eq(promise));
    verify(ctx).flush();
  }

  @Test
  public void cancelForUnknownStreamShouldFail() throws Exception {
    when(stream.id()).thenReturn(3);
    handler.write(ctx, new CancelStreamCommand(stream), promise);
    verify(promise).setFailure(any(Throwable.class));
  }

  @Test
  public void sendFrameShouldSucceed() throws Exception {
    createStream();

    // Send a frame and verify that it was written.
    handler.write(ctx, new SendGrpcFrameCommand(stream.id(), content, true), promise);
    verify(promise, never()).setFailure(any(Throwable.class));
    verify(ctx).write(any(ByteBuf.class), eq(promise));
    verify(ctx).flush();
  }

  @Test
  public void sendForUnknownStreamShouldFail() throws Exception {
    when(stream.id()).thenReturn(3);
    handler.write(ctx, new SendGrpcFrameCommand(stream.id(), content, true), promise);
    verify(promise).setFailure(any(Throwable.class));
  }

  @Test
  public void inboundHeadersShouldForwardToStream() throws Exception {
    createStream();

    // Read a headers frame first.
    Http2Headers headers = new DefaultHttp2Headers().status(STATUS_OK)
        .set(CONTENT_TYPE_HEADER, CONTENT_TYPE_PROTORPC);
    ByteBuf headersFrame = headersFrame(3, headers);
    handler.channelRead(this.ctx, headersFrame);
    verify(stream).inboundHeadersRecieved(headers, false);
  }

  @Test
  public void inboundDataShouldForwardToStream() throws Exception {
    createStream();

    // Create a data frame and then trigger the handler to read it.
    // Need to retain to simulate what is done by the stream.
    ByteBuf frame = dataFrame(3, false).retain();
    handler.channelRead(this.ctx, frame);
    verify(stream).inboundDataReceived(eq(content), eq(false));
  }

  @Test
  public void createShouldQueueStream() throws Exception {
    // Disallow stream creation to force the stream to get added to the pending queue.
    setMaxConcurrentStreams(0);
    handler.write(ctx, new CreateStreamCommand(grpcHeaders, stream),
        promise);

    // Make sure the write never occurred.
    verify(frameListener, never()).onHeadersRead(eq(ctx),
        eq(3),
        any(Http2Headers.class),
        eq(0),
        eq(Http2CodecUtil.DEFAULT_PRIORITY_WEIGHT),
        eq(false),
        eq(0),
        eq(false));
  }

  @Test
  public void receivedGoAwayShouldFailQueuedStreams() throws Exception {
    // Force a stream to get added to the pending queue.
    setMaxConcurrentStreams(0);
    handler.write(ctx, new CreateStreamCommand(grpcHeaders, stream),
        promise);

    handler.channelRead(ctx, goAwayFrame(0));
    verify(promise).setFailure(any(Throwable.class));
  }

  @Test
  public void receivedGoAwayShouldFailUnknownStreams() throws Exception {
    // Force a stream to get added to the pending queue.
    handler.write(ctx, new CreateStreamCommand(grpcHeaders, stream),
        promise);

    // Read a GOAWAY that indicates our stream was never processed by the server.
    handler.channelRead(ctx, goAwayFrame(0));
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    InOrder inOrder = inOrder(stream);
    inOrder.verify(stream, calls(1)).setStatus(captor.capture(), notNull(Metadata.Trailers.class));
    assertEquals(Transport.Code.UNAVAILABLE, captor.getValue().getCode());
  }

  private void setMaxConcurrentStreams(int max) throws Exception {
    ByteBuf serializedSettings = serializeSettings(new Http2Settings().maxConcurrentStreams(max));
    handler.channelRead(ctx, serializedSettings);
    // Reset the context to clear this write.
    mockContext();
  }

  private ByteBuf dataFrame(int streamId, boolean endStream) {
    // Need to retain the content since the frameWriter releases it.
    content.retain();
    ChannelHandlerContext ctx = newContext();
    frameWriter.writeData(ctx, streamId, content, 0, endStream, newPromise());
    return captureWrite(ctx);
  }

  private void createStream() throws Exception {
    // Create the stream.
    handler.write(ctx, new CreateStreamCommand(grpcHeaders, stream),
        promise);
    when(stream.id()).thenReturn(3);
    // Reset the context mock to clear recording of sent headers frame.
    mockContext();
  }

  private static NettyClientHandler newHandler() {
    Http2Connection connection = new DefaultHttp2Connection(false);
    Http2FrameReader frameReader = new DefaultHttp2FrameReader();
    Http2FrameWriter frameWriter = new DefaultHttp2FrameWriter();
    DefaultHttp2InboundFlowController inboundFlow =
        new DefaultHttp2InboundFlowController(connection, frameWriter);
    Http2OutboundFlowController outboundFlow =
        new DefaultHttp2OutboundFlowController(connection, frameWriter);
    return new NettyClientHandler(connection,
        frameReader,
        frameWriter,
        inboundFlow,
        outboundFlow);
  }

  private AsciiString as(String string) {
    return new AsciiString(string);
  }
}
