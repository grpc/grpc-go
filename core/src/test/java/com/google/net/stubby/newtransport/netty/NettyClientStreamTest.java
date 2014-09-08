package com.google.net.stubby.newtransport.netty;

import static io.netty.util.CharsetUtil.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.verify;

import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;
import com.google.net.stubby.newtransport.HttpUtil;
import com.google.net.stubby.newtransport.StreamState;
import com.google.net.stubby.transport.Transport;

import io.netty.buffer.Unpooled;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.Http2Headers;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;

/**
 * Tests for {@link NettyClientStream}.
 */
@RunWith(JUnit4.class)
public class NettyClientStreamTest extends NettyStreamTestBase {

  @Test
  public void closeShouldSucceed() {
    // Force stream creation.
    stream().id(STREAM_ID);
    stream().halfClose();
    assertEquals(StreamState.READ_ONLY, stream.state());
  }

  @Test
  public void cancelShouldSendCommand() {
    stream().cancel();
    verify(channel).writeAndFlush(any(CancelStreamCommand.class));
  }

  @Test
  public void writeContextShouldSendRequest() throws Exception {
    // Force stream creation.
    stream().id(STREAM_ID);
    stream.writeContext(CONTEXT_KEY, input, input.available(), accepted);
    stream.flush();
    verify(channel).writeAndFlush(new SendGrpcFrameCommand(1, contextFrame(), false));
    verify(accepted).run();
  }

  @Test
  public void writeMessageShouldSendRequest() throws Exception {
    // Force stream creation.
    stream().id(STREAM_ID);
    stream.writeMessage(input, input.available(), accepted);
    stream.flush();
    verify(channel).writeAndFlush(new SendGrpcFrameCommand(1, messageFrame(), false));
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
    Status errorStatus = new Status(Transport.Code.INTERNAL);
    stream().setStatus(errorStatus, new Metadata.Trailers());
    verify(listener).closed(eq(errorStatus), any(Metadata.Trailers.class));
    assertEquals(StreamState.CLOSED, stream.state());
  }

  @Test
  public void setStatusWithOkShouldNotOverrideError() {
    Status errorStatus = new Status(Transport.Code.INTERNAL);
    stream().setStatus(errorStatus, new Metadata.Trailers());
    stream().setStatus(Status.OK, new Metadata.Trailers());
    verify(listener).closed(any(Status.class), any(Metadata.Trailers.class));
    assertEquals(StreamState.CLOSED, stream.state());
  }

  @Test
  public void setStatusWithErrorShouldNotOverridePreviousError() {
    Status errorStatus = new Status(Transport.Code.INTERNAL);
    stream().setStatus(errorStatus, new Metadata.Trailers());
    stream().setStatus(Status.fromThrowable(new RuntimeException("fake")),
        new Metadata.Trailers());
    verify(listener).closed(any(Status.class), any(Metadata.Trailers.class));
    assertEquals(StreamState.CLOSED, stream.state());
  }

  @Override
  @Test
  public void inboundContextShouldCallListener() throws Exception {
    // Receive headers first so that it's a valid GRPC response.
    stream().id(1);
    stream().inboundHeadersRecieved(grpcResponseHeaders(), false);

    super.inboundContextShouldCallListener();
  }

  @Override
  @Test
  public void inboundMessageShouldCallListener() throws Exception {
    // Receive headers first so that it's a valid GRPC response.
    stream().id(1);
    stream().inboundHeadersRecieved(grpcResponseHeaders(), false);

    super.inboundMessageShouldCallListener();
  }

  @Test
  public void inboundStatusShouldSetStatus() throws Exception {
    stream().id(1);

    // Receive headers first so that it's a valid GRPC response.
    stream().inboundHeadersRecieved(grpcResponseHeaders(), false);

    stream.inboundDataReceived(statusFrame(new Status(Transport.Code.INTERNAL)), false);
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(listener).closed(captor.capture(), any(Metadata.Trailers.class));
    assertEquals(Transport.Code.INTERNAL, captor.getValue().getCode());
    assertEquals(StreamState.CLOSED, stream.state());
  }

  @Test
  public void nonGrpcResponseShouldSetStatus() throws Exception {
    stream.inboundDataReceived(Unpooled.copiedBuffer(MESSAGE, UTF_8), true);
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(listener).closed(captor.capture(), any(Metadata.Trailers.class));
    assertEquals(MESSAGE, captor.getValue().getDescription());
  }

  @Override
  protected NettyStream createStream() {
    NettyStream stream = new NettyClientStream(listener, channel, inboundFlow);
    assertEquals(StreamState.OPEN, stream.state());
    return stream;
  }

  private NettyClientStream stream() {
    return (NettyClientStream) stream;
  }

  private Http2Headers grpcResponseHeaders() {
    return DefaultHttp2Headers.newBuilder().status("200")
        .set(HttpUtil.CONTENT_TYPE_HEADER, HttpUtil.CONTENT_TYPE_PROTORPC).build();
  }
}
