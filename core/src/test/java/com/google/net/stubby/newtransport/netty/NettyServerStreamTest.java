package com.google.net.stubby.newtransport.netty;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.fail;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.verifyZeroInteractions;

import com.google.net.stubby.Status;
import com.google.net.stubby.newtransport.StreamState;
import com.google.net.stubby.transport.Transport;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link NettyServerStream}. */
@RunWith(JUnit4.class)
public class NettyServerStreamTest extends NettyStreamTestBase {

  @Test
  public void writeContextShouldSendResponse() throws Exception {
    stream.writeContext(CONTEXT_KEY, input, input.available(), accepted);
    stream.flush();
    verify(channel).write(new SendResponseHeadersCommand(STREAM_ID));
    verify(channel).writeAndFlush(new SendGrpcFrameCommand(STREAM_ID, contextFrame(), false));
    verify(accepted).run();
  }

  @Test
  public void writeMessageShouldSendResponse() throws Exception {
    stream.writeMessage(input, input.available(), accepted);
    stream.flush();
    verify(channel).write(new SendResponseHeadersCommand(STREAM_ID));
    verify(channel).writeAndFlush(new SendGrpcFrameCommand(STREAM_ID, messageFrame(), false));
    verify(accepted).run();
  }

  @Test
  public void closeBeforeClientHalfCloseShouldFail() {
    try {
      stream().close(Status.OK);
      fail("Should throw exception");
    } catch (IllegalStateException expected) {
    }
    assertEquals(StreamState.OPEN, stream.state());
    verifyZeroInteractions(listener);
  }

  @Test
  public void closeWithErrorBeforeClientHalfCloseShouldSucceed() throws Exception {
    stream().close(Status.CANCELLED);
    assertEquals(StreamState.CLOSED, stream.state());
    verify(channel).writeAndFlush(
        new SendGrpcFrameCommand(STREAM_ID, statusFrame(Status.CANCELLED), true));
    verifyZeroInteractions(listener);
  }

  @Test
  public void closeAfterClientHalfCloseShouldSucceed() throws Exception {
    // Client half-closes. Listener gets closed()
    stream().remoteEndClosed();
    assertEquals(StreamState.WRITE_ONLY, stream.state());
    verify(listener).closed(Status.OK);
    // Server closes. Status sent.
    stream().close(Status.OK);
    assertEquals(StreamState.CLOSED, stream.state());
    verify(channel).writeAndFlush(
        new SendGrpcFrameCommand(STREAM_ID, statusFrame(Status.OK), true));
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void clientHalfCloseForTheSecondTimeShouldFail() throws Exception {
    // Client half-closes. Listener gets closed()
    stream().remoteEndClosed();
    assertEquals(StreamState.WRITE_ONLY, stream.state());
    verify(listener).closed(Status.OK);
    // Client half-closes again. Stream will be aborted with an error.
    stream().remoteEndClosed();
    assertEquals(StreamState.CLOSED, stream.state());
    verify(channel).writeAndFlush(new SendGrpcFrameCommand(STREAM_ID, statusFrame(
        new Status(Transport.Code.FAILED_PRECONDITION, "Client-end of the stream already closed")),
        true));
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void abortStreamAndSendStatus() throws Exception {
    Status status = new Status(Transport.Code.INTERNAL, new Throwable());
    stream().abortStream(status, true);
    assertEquals(StreamState.CLOSED, stream.state());
    verify(listener).closed(status);
    verify(channel).writeAndFlush(new SendGrpcFrameCommand(STREAM_ID, statusFrame(status), true));
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void abortStreamAndNotSendStatus() throws Exception {
    Status status = new Status(Transport.Code.INTERNAL, new Throwable());
    stream().abortStream(status, false);
    assertEquals(StreamState.CLOSED, stream.state());
    verify(listener).closed(status);
    verify(channel, never()).writeAndFlush(
        new SendGrpcFrameCommand(STREAM_ID, statusFrame(status), true));
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void abortStreamAfterClientHalfCloseShouldNotCallListenerTwice() {
    Status status = new Status(Transport.Code.INTERNAL, new Throwable());
    // Client half-closes. Listener gets closed()
    stream().remoteEndClosed();
    assertEquals(StreamState.WRITE_ONLY, stream.state());
    verify(listener).closed(Status.OK);
    // Abort
    stream().abortStream(status, true);
    assertEquals(StreamState.CLOSED, stream.state());
    verifyNoMoreInteractions(listener);
  }

  @Override
  protected NettyStream createStream() {
    NettyServerStream stream = new NettyServerStream(channel, STREAM_ID, inboundFlow);
    stream.setListener(listener);
    assertEquals(StreamState.OPEN, stream.state());
    verifyZeroInteractions(listener);
    return stream;
  }

  private NettyServerStream stream() {
    return (NettyServerStream) stream;
  }
}
