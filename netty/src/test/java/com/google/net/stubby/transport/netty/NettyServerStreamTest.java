package com.google.net.stubby.transport.netty;

import static com.google.net.stubby.transport.netty.NettyTestUtil.messageFrame;
import static com.google.net.stubby.transport.netty.NettyTestUtil.statusFrame;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.verifyZeroInteractions;

import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.ServerStreamListener;
import com.google.net.stubby.transport.StreamState;

import io.netty.buffer.EmptyByteBuf;
import io.netty.buffer.UnpooledByteBufAllocator;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.Http2Headers;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;

/** Unit tests for {@link NettyServerStream}. */
@RunWith(JUnit4.class)
public class NettyServerStreamTest extends NettyStreamTestBase {
  @Mock
  protected ServerStreamListener serverListener;
  private Metadata.Trailers trailers = new Metadata.Trailers();

  @Test
  public void writeMessageShouldSendResponse() throws Exception {
    stream.writeMessage(input, input.available(), accepted);
    stream.flush();
    Http2Headers headers = new DefaultHttp2Headers()
        .status(Utils.STATUS_OK)
        .set(Utils.CONTENT_TYPE_HEADER, Utils.CONTENT_TYPE_GRPC);
    verify(channel).writeAndFlush(new SendResponseHeadersCommand(STREAM_ID, headers, false));
    verify(channel).writeAndFlush(new SendGrpcFrameCommand(STREAM_ID, messageFrame(MESSAGE), false));
    verify(accepted).run();
  }

  @Test
  public void writeHeadersShouldSendHeaders() throws Exception {
    Metadata.Headers headers = new Metadata.Headers();
    stream().writeHeaders(headers);
    verify(channel).writeAndFlush(new SendResponseHeadersCommand(STREAM_ID,
        Utils.convertServerHeaders(headers), false));
  }

  @Test
  public void duplicateWriteHeadersShouldFail() throws Exception {
    Metadata.Headers headers = new Metadata.Headers();
    stream().writeHeaders(headers);
    verify(channel).writeAndFlush(new SendResponseHeadersCommand(STREAM_ID,
        Utils.convertServerHeaders(headers), false));
    try {
      stream().writeHeaders(headers);
      fail("Can only write response headers once");
    } catch (IllegalStateException ise) {
      // Success
    }
  }

  @Test
  public void closeBeforeClientHalfCloseShouldSucceed() throws Exception {
    stream().close(Status.OK, new Metadata.Trailers());
    verify(channel).writeAndFlush(
        new SendGrpcFrameCommand(STREAM_ID, statusFrame(Status.OK), true));
    verifyZeroInteractions(serverListener);
    // Sending complete. Listener gets closed()
    stream().complete();
    verify(serverListener).closed(Status.OK);
    assertEquals(StreamState.CLOSED, stream.state());
    verifyZeroInteractions(serverListener);
  }

  @Test
  public void closeWithErrorBeforeClientHalfCloseShouldSucceed() throws Exception {
    // Error is sent on wire and ends the stream
    stream().close(Status.CANCELLED, trailers);
    verify(channel).writeAndFlush(
        new SendGrpcFrameCommand(STREAM_ID, statusFrame(Status.CANCELLED), true));
    verifyZeroInteractions(serverListener);
    // Sending complete. Listener gets closed()
    stream().complete();
    verify(serverListener).closed(Status.OK);
    assertEquals(StreamState.CLOSED, stream.state());
    verifyZeroInteractions(serverListener);
  }

  @Test
  public void closeAfterClientHalfCloseShouldSucceed() throws Exception {
    // Client half-closes. Listener gets halfClosed()
    stream().inboundDataReceived(new EmptyByteBuf(UnpooledByteBufAllocator.DEFAULT), true);
    assertEquals(StreamState.WRITE_ONLY, stream.state());
    verify(serverListener).halfClosed();
    // Server closes. Status sent
    stream().close(Status.OK, trailers);
    verifyNoMoreInteractions(serverListener);
    assertEquals(StreamState.CLOSED, stream.state());
    verify(channel).writeAndFlush(
        new SendGrpcFrameCommand(STREAM_ID, statusFrame(Status.OK), true));
    // Sending and receiving complete. Listener gets closed()
    stream().complete();
    verify(serverListener).closed(Status.OK);
    verifyNoMoreInteractions(serverListener);
  }

  @Test
  public void abortStreamAndSendStatus() throws Exception {
    Status status = Status.INTERNAL.withCause(new Throwable());
    stream().abortStream(status, true);
    assertEquals(StreamState.CLOSED, stream.state());
    verify(serverListener).closed(same(status));
    verify(channel).writeAndFlush(new SendGrpcFrameCommand(STREAM_ID, statusFrame(status), true));
    verifyNoMoreInteractions(serverListener);
  }

  @Test
  public void abortStreamAndNotSendStatus() throws Exception {
    Status status = Status.INTERNAL.withCause(new Throwable());
    stream().abortStream(status, false);
    assertEquals(StreamState.CLOSED, stream.state());
    verify(serverListener).closed(same(status));
    verify(channel, never()).writeAndFlush(
        new SendGrpcFrameCommand(STREAM_ID, statusFrame(status), true));
    verifyNoMoreInteractions(serverListener);
  }

  @Test
  public void abortStreamAfterClientHalfCloseShouldCallClose() {
    Status status = Status.INTERNAL.withCause(new Throwable());
    // Client half-closes. Listener gets halfClosed()
    stream().inboundDataReceived(new EmptyByteBuf(UnpooledByteBufAllocator.DEFAULT), true);
    assertEquals(StreamState.WRITE_ONLY, stream.state());
    verify(serverListener).halfClosed();
    // Abort
    stream().abortStream(status, true);
    verify(serverListener).closed(same(status));
    assertEquals(StreamState.CLOSED, stream.state());
    verifyNoMoreInteractions(serverListener);
  }

  @Override
  protected NettyStream createStream() {
    NettyServerStream stream = new NettyServerStream(channel, STREAM_ID, inboundFlow);
    stream.setListener(serverListener);
    assertEquals(StreamState.OPEN, stream.state());
    verifyZeroInteractions(serverListener);
    return stream;
  }

  @Override
  protected ServerStreamListener listener() {
    return serverListener;
  }

  private NettyServerStream stream() {
    return (NettyServerStream) stream;
  }
}
