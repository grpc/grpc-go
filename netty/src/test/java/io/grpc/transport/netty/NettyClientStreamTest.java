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

import static io.grpc.transport.netty.NettyTestUtil.messageFrame;
import static io.grpc.transport.netty.Utils.CONTENT_TYPE_GRPC;
import static io.grpc.transport.netty.Utils.CONTENT_TYPE_HEADER;
import static io.grpc.transport.netty.Utils.STATUS_OK;
import static io.netty.util.CharsetUtil.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;

import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.transport.AbstractStream;
import io.grpc.transport.ClientStreamListener;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.Unpooled;
import io.netty.handler.codec.AsciiString;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.Http2Headers;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;

import java.io.InputStream;

/**
 * Tests for {@link NettyClientStream}.
 */
@RunWith(JUnit4.class)
public class NettyClientStreamTest extends NettyStreamTestBase {
  @Mock
  protected ClientStreamListener listener;

  @Mock
  protected NettyClientHandler handler;

  @Override
  protected ClientStreamListener listener() {
    return listener;
  }

  @Test
  public void closeShouldSucceed() {
    // Force stream creation.
    stream().id(STREAM_ID);
    stream().halfClose();
    assertTrue(stream().canReceive());
    assertFalse(stream().canSend());
  }

  @Test
  public void cancelShouldSendCommand() {
    // Set stream id to indicate it has been created
    stream().id(STREAM_ID);
    stream().cancel();
    verify(channel).writeAndFlush(any(CancelStreamCommand.class));
  }

  @Test
  public void cancelShouldStillSendCommandIfStreamNotCreatedToCancelCreation() {
    stream().cancel();
    verify(channel).writeAndFlush(any(CancelStreamCommand.class));
  }

  @Test
  public void writeMessageShouldSendRequest() throws Exception {
    // Force stream creation.
    stream().id(STREAM_ID);
    stream.writeMessage(input, input.available(), accepted);
    stream.flush();
    verify(channel).writeAndFlush(new SendGrpcFrameCommand(stream, messageFrame(MESSAGE), false));
    verify(accepted).run();
  }

  @Test
  public void setStatusWithOkShouldCloseStream() {
    stream().id(1);
    stream().transportReportStatus(Status.OK, true, new Metadata.Trailers());
    verify(listener).closed(same(Status.OK), any(Metadata.Trailers.class));
    assertTrue(stream.isClosed());
  }

  @Test
  public void setStatusWithErrorShouldCloseStream() {
    Status errorStatus = Status.INTERNAL;
    stream().transportReportStatus(errorStatus, true, new Metadata.Trailers());
    verify(listener).closed(eq(errorStatus), any(Metadata.Trailers.class));
    assertTrue(stream.isClosed());
  }

  @Test
  public void setStatusWithOkShouldNotOverrideError() {
    Status errorStatus = Status.INTERNAL;
    stream().transportReportStatus(errorStatus, true, new Metadata.Trailers());
    stream().transportReportStatus(Status.OK, true, new Metadata.Trailers());
    verify(listener).closed(any(Status.class), any(Metadata.Trailers.class));
    assertTrue(stream.isClosed());
  }

  @Test
  public void setStatusWithErrorShouldNotOverridePreviousError() {
    Status errorStatus = Status.INTERNAL;
    stream().transportReportStatus(errorStatus, true, new Metadata.Trailers());
    stream().transportReportStatus(Status.fromThrowable(new RuntimeException("fake")), true,
        new Metadata.Trailers());
    verify(listener).closed(any(Status.class), any(Metadata.Trailers.class));
    assertTrue(stream.isClosed());
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

    stream().transportHeadersReceived(grpcResponseTrailers(Status.INTERNAL), true);
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(listener).closed(captor.capture(), any(Metadata.Trailers.class));
    assertEquals(Status.INTERNAL.getCode(), captor.getValue().getCode());
    assertTrue(stream.isClosed());
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
    verify(channel, never()).writeAndFlush(any(CancelStreamCommand.class));
    stream().transportDataReceived(Unpooled.buffer(100).writeZero(100), false);
    verify(channel, never()).writeAndFlush(any(CancelStreamCommand.class));
    stream().transportDataReceived(Unpooled.buffer(1000).writeZero(1000), false);

    // Now verify that cancel is sent and an error is reported to the listener
    verify(channel).writeAndFlush(any(CancelStreamCommand.class));
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(listener).closed(captor.capture(), any(Metadata.Trailers.class));
    assertEquals(Status.INTERNAL.getCode(), captor.getValue().getCode());
    assertTrue(stream.isClosed());

  }

  @Test
  public void nonGrpcResponseShouldSetStatus() throws Exception {
    stream().transportDataReceived(Unpooled.copiedBuffer(MESSAGE, UTF_8), true);
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(listener).closed(captor.capture(), any(Metadata.Trailers.class));
    assertEquals(Status.Code.INTERNAL, captor.getValue().getCode());
  }

  @Test
  public void deframedDataAfterCancelShouldBeIgnored() throws Exception {
    stream().id(1);
    // Receive headers first so that it's a valid GRPC response.
    stream().transportHeadersReceived(grpcResponseHeaders(), false);

    // Receive 2 consecutive empty frames. Only one is delivered at a time to the listener.
    stream().transportDataReceived(simpleGrpcFrame(), false);
    stream().transportDataReceived(simpleGrpcFrame(), false);

    // Only allow the first to be delivered.
    stream().request(1);

    // Receive error trailers. The server status will not be processed until after all of the
    // data frames have been processed. Since cancellation will interrupt message delivery,
    // this status will never be processed and the listener will instead only see the
    // cancellation.
    stream().transportHeadersReceived(grpcResponseTrailers(Status.INTERNAL), true);

    // Verify that the first was delivered.
    verify(listener).messageRead(any(InputStream.class));

    // Now set the error status.
    Metadata.Trailers trailers = Utils.convertTrailers(grpcResponseTrailers(Status.CANCELLED));
    stream().transportReportStatus(Status.CANCELLED, true, trailers);

    // Now allow the delivery of the second.
    stream().request(1);

    // Verify that the listener was only notified of the first message, not the second.
    verify(listener).messageRead(any(InputStream.class));
    verify(listener).closed(eq(Status.CANCELLED), eq(trailers));
  }

  @Test
  public void dataFrameWithEosShouldDeframeAndThenFail() {
    stream().id(1);
    stream().request(1);

    // Receive headers first so that it's a valid GRPC response.
    stream().transportHeadersReceived(grpcResponseHeaders(), false);

    // Receive a DATA frame with EOS set.
    stream().transportDataReceived(simpleGrpcFrame(), true);

    // Verify that the message was delivered.
    verify(listener).messageRead(any(InputStream.class));

    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(listener).closed(captor.capture(), any(Metadata.Trailers.class));
    assertEquals(Status.Code.INTERNAL, captor.getValue().getCode());
  }

  @Override
  protected AbstractStream<Integer> createStream() {
    AbstractStream<Integer> stream = new NettyClientStream(listener, channel, handler);
    assertTrue(stream.canSend());
    assertTrue(stream.canReceive());
    return stream;
  }

  private ByteBuf simpleGrpcFrame() {
    return Unpooled.wrappedBuffer(new byte[] {0, 0, 0, 0, 2, 3, 14});
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
