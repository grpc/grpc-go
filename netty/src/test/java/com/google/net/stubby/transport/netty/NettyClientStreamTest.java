package com.google.net.stubby.transport.netty;

import static com.google.net.stubby.transport.netty.NettyTestUtil.messageFrame;
import static com.google.net.stubby.transport.netty.NettyTestUtil.statusFrame;
import static com.google.net.stubby.transport.netty.Utils.CONTENT_TYPE_GRPC;
import static com.google.net.stubby.transport.netty.Utils.CONTENT_TYPE_HEADER;
import static com.google.net.stubby.transport.netty.Utils.STATUS_OK;
import static io.netty.util.CharsetUtil.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;

import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.AbstractStream;
import com.google.net.stubby.transport.ClientStreamListener;
import com.google.net.stubby.transport.StreamState;

import io.netty.buffer.Unpooled;
import io.netty.handler.codec.AsciiString;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.Http2Headers;

import org.junit.After;
import org.junit.Assume;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.Mockito;

/**
 * Tests for {@link NettyClientStream}.
 */
@RunWith(JUnit4.class)
public class NettyClientStreamTest extends NettyStreamTestBase {
  @Mock
  protected ClientStreamListener listener;

  @Override
  protected ClientStreamListener listener() {
    return listener;
  }

  @Override
  @Before
  public void setup() {
    AbstractStream.GRPC_V2_PROTOCOL = false;
    super.setup();
  }

  @After
  public void tearDown() {
    AbstractStream.GRPC_V2_PROTOCOL = false;
  }

  @Test
  public void closeShouldSucceed() {
    // Force stream creation.
    stream().id(STREAM_ID);
    stream().halfClose();
    assertEquals(StreamState.READ_ONLY, stream.state());
  }

  @Test
  public void cancelShouldSendCommand() {
    // Set stream id to indicate it has been created
    stream().id(STREAM_ID);
    stream().cancel();
    verify(channel).writeAndFlush(any(CancelStreamCommand.class));
  }

  @Test
  public void cancelShouldNotSendCommandIfStreamNotCreated() {
    stream().cancel();
    verify(channel, never()).writeAndFlush(any(CancelStreamCommand.class));
  }

  @Test
  public void writeMessageShouldSendRequest() throws Exception {
    // Force stream creation.
    stream().id(STREAM_ID);
    stream.writeMessage(input, input.available(), accepted);
    stream.flush();
    verify(channel).writeAndFlush(new SendGrpcFrameCommand(1, messageFrame(MESSAGE), false));
    verify(accepted).run();
  }

  @Test
  public void setStatusWithOkShouldCloseStream() {
    stream().id(1);
    stream().setStatus(Status.OK, new Metadata.Trailers());
    verify(listener).closed(same(Status.OK), any(Metadata.Trailers.class));
    assertEquals(StreamState.CLOSED, stream.state());
  }

  @Test
  public void setStatusWithErrorShouldCloseStream() {
    Status errorStatus = Status.INTERNAL;
    stream().setStatus(errorStatus, new Metadata.Trailers());
    verify(listener).closed(eq(errorStatus), any(Metadata.Trailers.class));
    assertEquals(StreamState.CLOSED, stream.state());
  }

  @Test
  public void setStatusWithOkShouldNotOverrideError() {
    Status errorStatus = Status.INTERNAL;
    stream().setStatus(errorStatus, new Metadata.Trailers());
    stream().setStatus(Status.OK, new Metadata.Trailers());
    verify(listener).closed(any(Status.class), any(Metadata.Trailers.class));
    assertEquals(StreamState.CLOSED, stream.state());
  }

  @Test
  public void setStatusWithErrorShouldNotOverridePreviousError() {
    Status errorStatus = Status.INTERNAL;
    stream().setStatus(errorStatus, new Metadata.Trailers());
    stream().setStatus(Status.fromThrowable(new RuntimeException("fake")),
        new Metadata.Trailers());
    verify(listener).closed(any(Status.class), any(Metadata.Trailers.class));
    assertEquals(StreamState.CLOSED, stream.state());
  }

  @Override
  @Test
  public void inboundMessageShouldCallListener() throws Exception {
    // Receive headers first so that it's a valid GRPC response.
    stream().id(1);
    stream().transportHeadersReceived(grpcResponseHeaders(), false);
    super.inboundMessageShouldCallListener();
  }

  @Test
  public void inboundHeadersShouldCallListenerHeadersRead() throws Exception {
    stream().id(1);
    Http2Headers headers = grpcResponseHeaders();
    stream().transportHeadersReceived(headers, false);
    verify(listener).headersRead(any(Metadata.Headers.class));
  }

  @Test
  public void inboundTrailersClosesCall() throws Exception {
    Assume.assumeTrue(AbstractStream.GRPC_V2_PROTOCOL);
    stream().id(1);
    stream().transportHeadersReceived(grpcResponseHeaders(), false);
    super.inboundMessageShouldCallListener();
    stream().transportHeadersReceived(grpcResponseTrailers(Status.OK), true);
  }

  @Test
  public void inboundStatusShouldSetStatus() throws Exception {
    stream().id(1);

    // Receive headers first so that it's a valid GRPC response.
    stream().transportHeadersReceived(grpcResponseHeaders(), false);

    if (AbstractStream.GRPC_V2_PROTOCOL) {
      stream().transportHeadersReceived(grpcResponseTrailers(Status.INTERNAL), true);
    } else {
      stream().transportDataReceived(statusFrame(Status.INTERNAL), false);
    }
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(listener).closed(captor.capture(), any(Metadata.Trailers.class));
    assertEquals(Status.INTERNAL.getCode(), captor.getValue().getCode());
    assertEquals(StreamState.CLOSED, stream.state());
  }

  @Test
  public void invalidInboundHeadersCancelStream() throws Exception {
    stream().id(1);
    Http2Headers headers = grpcResponseHeaders();
    headers.remove(CONTENT_TYPE_HEADER);
    // Remove once b/16290036 is fixed.
    headers.status(AsciiString.of("500"));
    stream().transportHeadersReceived(headers, false);
    verify(listener, never()).closed(any(Status.class), any(Metadata.Trailers.class));

    // We are now waiting for 100 bytes of error context on the stream, cancel has not yet been sent
    Mockito.verify(channel, never()).writeAndFlush(any(CancelStreamCommand.class));
    stream().transportDataReceived(Unpooled.buffer(100).writeZero(100), false);
    Mockito.verify(channel, never()).writeAndFlush(any(CancelStreamCommand.class));
    stream().transportDataReceived(Unpooled.buffer(1000).writeZero(1000), false);

    // Now verify that cancel is sent and an error is reported to the listener
    verify(channel).writeAndFlush(any(CancelStreamCommand.class));
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(listener).closed(captor.capture(), any(Metadata.Trailers.class));
    assertEquals(Status.INTERNAL.getCode(), captor.getValue().getCode());
    assertEquals(StreamState.CLOSED, stream.state());

  }

  @Test
  public void nonGrpcResponseShouldSetStatus() throws Exception {
    stream().transportDataReceived(Unpooled.copiedBuffer(MESSAGE, UTF_8), true);
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(listener).closed(captor.capture(), any(Metadata.Trailers.class));
    assertEquals(Status.Code.INTERNAL, captor.getValue().getCode());
  }

  @Override
  protected AbstractStream<Integer> createStream() {
    AbstractStream<Integer> stream = new NettyClientStream(listener, channel, inboundFlow);
    assertEquals(StreamState.OPEN, stream.state());
    return stream;
  }

  private NettyClientStream stream() {
    return (NettyClientStream) stream;
  }

  private Http2Headers grpcResponseHeaders() {
    return new DefaultHttp2Headers()
        .status(STATUS_OK)
        .set(CONTENT_TYPE_HEADER, CONTENT_TYPE_GRPC);
  }

  private Http2Headers grpcResponseTrailers(Status status) {
    Metadata.Trailers trailers = new Metadata.Trailers();
    trailers.put(Status.CODE_KEY, status);
    return Utils.convertTrailers(trailers, true);
  }
}
