/*
 * Copyright 2015, Google Inc. All rights reserved.
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

import static io.grpc.internal.GrpcUtil.Http2Error.CANCEL;
import static io.netty.handler.codec.http2.Http2CodecUtil.DEFAULT_PRIORITY_WEIGHT;
import static io.netty.handler.codec.http2.Http2Stream.State.HALF_CLOSED_LOCAL;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Mockito.atLeastOnce;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.spy;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.Unpooled;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPromise;
import io.netty.channel.embedded.EmbeddedChannel;
import io.netty.handler.codec.http2.DefaultHttp2Connection;
import io.netty.handler.codec.http2.DefaultHttp2ConnectionDecoder;
import io.netty.handler.codec.http2.DefaultHttp2ConnectionEncoder;
import io.netty.handler.codec.http2.DefaultHttp2FrameReader;
import io.netty.handler.codec.http2.DefaultHttp2FrameWriter;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2ConnectionHandler;
import io.netty.handler.codec.http2.Http2Exception;
import io.netty.handler.codec.http2.Http2FrameListener;
import io.netty.handler.codec.http2.Http2FrameWriter;
import io.netty.handler.codec.http2.Http2Headers;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import org.mockito.verification.VerificationMode;

import java.util.ArrayList;
import java.util.List;

/**
 * Tests for {@link BufferingHttp2ConnectionEncoder}.
 */
@RunWith(JUnit4.class)
public class BufferingHttp2ConnectionEncoderTest {

  private BufferingHttp2ConnectionEncoder encoder;

  private Http2Connection connection;

  private Http2FrameWriter writer;

  @Mock
  private Http2FrameListener frameListener;

  private ChannelHandlerContext ctx;

  private EmbeddedChannel channel;

  /**
   * Init fields and do mocking.
   */
  @Before
  public void setup() throws Exception {
    MockitoAnnotations.initMocks(this);

    connection = new DefaultHttp2Connection(false);

    writer = spy(new DefaultHttp2FrameWriter());
    DefaultHttp2ConnectionEncoder defaultEncoder =
        new DefaultHttp2ConnectionEncoder(connection, writer);
    encoder = new BufferingHttp2ConnectionEncoder(defaultEncoder);
    DefaultHttp2ConnectionDecoder decoder = new DefaultHttp2ConnectionDecoder(connection, encoder,
        new DefaultHttp2FrameReader(), frameListener);

    Http2ConnectionHandler handler = new Http2ConnectionHandler(decoder, encoder);
    channel = new EmbeddedChannel(handler);
    ctx = channel.pipeline().context(handler);
  }

  @After
  public void teardown() {
    channel.close();
  }

  @Test
  public void multipleWritesToActiveStream() throws Http2Exception {
    encoder.writeSettingsAck(ctx, newPromise());
    encoderWriteHeaders(3);
    assertEquals(0, encoder.numBufferedStreams());
    encoder.writeData(ctx, 3, data(), 0, false, newPromise());
    encoder.writeData(ctx, 3, data(), 0, false, newPromise());
    encoder.writeData(ctx, 3, data(), 0, false, newPromise());
    encoderWriteHeaders(3);

    flush();

    writeVerifyWriteHeaders(times(2), 3);
    verify(writer, atLeastOnce()).writeData(eq(ctx), eq(3), any(ByteBuf.class), eq(0), eq(false),
        any(ChannelPromise.class));
  }

  @Test
  public void ensureCanCreateNextStreamWhenStreamCloses() {
    encoder.writeSettingsAck(ctx, newPromise());
    connection.local().maxActiveStreams(1);

    encoderWriteHeaders(3);
    flush();
    assertEquals(0, encoder.numBufferedStreams());

    // This one gets buffered.
    encoderWriteHeaders(5);
    flush();
    assertEquals(1, connection.numActiveStreams());
    assertEquals(1, encoder.numBufferedStreams());

    // Now prevent us from creating another stream.
    connection.local().maxActiveStreams(0);

    // Close the previous stream.
    connection.stream(3).close();

    flush();

    // Ensure that no streams are currently active and that only the HEADERS from the first
    // stream were written.
    writeVerifyWriteHeaders(times(1), 3);
    writeVerifyWriteHeaders(never(), 5);
    assertEquals(0, connection.numActiveStreams());
    assertEquals(1, encoder.numBufferedStreams());
  }

  @Test
  public void alternatingWritesToActiveAndBufferedStreams() {
    encoder.writeSettingsAck(ctx, newPromise());
    connection.local().maxActiveStreams(1);

    encoderWriteHeaders(3);
    assertEquals(0, encoder.numBufferedStreams());

    encoderWriteHeaders(5);
    assertEquals(1, connection.numActiveStreams());
    assertEquals(1, encoder.numBufferedStreams());

    encoder.writeData(ctx, 3, Unpooled.buffer(0), 0, false, newPromise());
    flush();
    writeVerifyWriteHeaders(times(1), 3);
    encoder.writeData(ctx, 5, Unpooled.buffer(0), 0, false, newPromise());
    flush();
    verify(writer, never()).writeData(eq(ctx), eq(5), any(ByteBuf.class), eq(0), eq(false),
        any(ChannelPromise.class));
  }

  @Test
  public void bufferingNewStreamFailsAfterGoAwayReceived() {
    encoder.writeSettingsAck(ctx, newPromise());
    connection.local().maxActiveStreams(0);
    connection.goAwayReceived(1, 8, null);

    ChannelFuture future = encoderWriteHeaders(3);
    assertEquals(0, encoder.numBufferedStreams());
    assertTrue(future.isDone());
    assertFalse(future.isSuccess());
  }

  @Test
  public void receivingGoAwayFailsBufferedStreams() {
    encoder.writeSettingsAck(ctx, newPromise());

    int maxStreams = 5;
    connection.local().maxActiveStreams(maxStreams);

    List<ChannelFuture> futures = new ArrayList<ChannelFuture>();
    int streamId = 3;
    for (int i = 0; i < 9; i++) {
      futures.add(encoderWriteHeaders(streamId));
      streamId += 2;
    }
    flush();
    assertEquals(4, encoder.numBufferedStreams());

    connection.goAwayReceived(11, 8, null);

    assertEquals(5, connection.numActiveStreams());
    // The 4 buffered streams must have been failed.

    int failed = 0;
    for (ChannelFuture future : futures) {
      if (future.isDone() && !future.isSuccess()) {
        failed++;
      }
    }
    assertEquals(4, failed);
    assertEquals(0, encoder.numBufferedStreams());
  }

  @Test
  public void sendingGoAwayShouldNotFailStreams() throws Exception {
    encoder.writeSettingsAck(ctx, newPromise());
    connection.local().maxActiveStreams(1);

    List<ChannelFuture> futures = new ArrayList<ChannelFuture>();
    futures.add(encoderWriteHeaders(3));
    assertEquals(0, encoder.numBufferedStreams());
    futures.add(encoderWriteHeaders(5));
    assertEquals(1, encoder.numBufferedStreams());
    futures.add(encoderWriteHeaders(7));
    assertEquals(2, encoder.numBufferedStreams());

    ByteBuf empty = Unpooled.buffer(0);
    encoder.writeGoAway(ctx, 3, CANCEL.code(), empty, newPromise());

    assertEquals(1, connection.numActiveStreams());
    assertEquals(2, encoder.numBufferedStreams());

    for (ChannelFuture future : futures) {
      assertNull(future.cause());
    }
  }

  @Test
  public void endStreamDoesNotFailBufferedStream() {
    encoder.writeSettingsAck(ctx, newPromise());
    connection.local().maxActiveStreams(0);

    encoderWriteHeaders(3);
    assertEquals(1, encoder.numBufferedStreams());

    ByteBuf empty = Unpooled.buffer(0);
    encoder.writeData(ctx, 3, empty, 0, true, newPromise());
    flush();

    assertEquals(0, connection.numActiveStreams());
    assertEquals(1, encoder.numBufferedStreams());

    // Simulate that we received a SETTINGS frame which
    // increased MAX_CONCURRENT_STREAMS to 1.
    connection.local().maxActiveStreams(1);
    encoder.writeSettingsAck(ctx, newPromise());
    flush();

    assertEquals(1, connection.numActiveStreams());
    assertEquals(0, encoder.numBufferedStreams());
    assertEquals(HALF_CLOSED_LOCAL, connection.stream(3).state());
  }

  @Test
  public void rstStreamClosesBufferedStream() {
    encoder.writeSettingsAck(ctx, newPromise());
    connection.local().maxActiveStreams(0);

    ChannelFuture future = encoderWriteHeaders(3);
    assertEquals(1, encoder.numBufferedStreams());

    assertFalse(future.isDone());
    ChannelPromise rstStreamPromise = mock(ChannelPromise.class);
    encoder.writeRstStream(ctx, 3, CANCEL.code(), rstStreamPromise);
    assertTrue(future.isSuccess());
    verify(rstStreamPromise).setSuccess();
    assertEquals(0, encoder.numBufferedStreams());
  }

  @Test
  public void bufferUntilActiveStreamsAreReset() {
    encoder.writeSettingsAck(ctx, newPromise());
    connection.local().maxActiveStreams(1);

    encoderWriteHeaders(3);
    assertEquals(0, encoder.numBufferedStreams());
    encoderWriteHeaders(5);
    assertEquals(1, encoder.numBufferedStreams());
    encoderWriteHeaders(7);
    assertEquals(2, encoder.numBufferedStreams());

    flush();

    writeVerifyWriteHeaders(times(1), 3);
    writeVerifyWriteHeaders(never(), 5);
    writeVerifyWriteHeaders(never(), 7);

    encoder.writeRstStream(ctx, 3, CANCEL.code(), newPromise());
    flush();
    assertEquals(1, connection.numActiveStreams());
    assertEquals(1, encoder.numBufferedStreams());

    encoder.writeRstStream(ctx, 5, CANCEL.code(), newPromise());
    flush();
    assertEquals(1, connection.numActiveStreams());
    assertEquals(0, encoder.numBufferedStreams());

    encoder.writeRstStream(ctx, 7, CANCEL.code(), newPromise());
    flush();
    assertEquals(0, connection.numActiveStreams());
    assertEquals(0, encoder.numBufferedStreams());
  }

  @Test
  public void bufferUntilMaxStreamsIncreased() {
    encoder.writeSettingsAck(ctx, newPromise());
    connection.local().maxActiveStreams(2);

    encoderWriteHeaders(3);
    encoderWriteHeaders(5);
    encoderWriteHeaders(7);
    encoderWriteHeaders(9);
    assertEquals(2, encoder.numBufferedStreams());
    flush();

    writeVerifyWriteHeaders(times(1), 3);
    writeVerifyWriteHeaders(times(1), 5);
    writeVerifyWriteHeaders(never(), 7);
    writeVerifyWriteHeaders(never(), 9);

    // Simulate that we received a SETTINGS frame which
    // increased MAX_CONCURRENT_STREAMS to 5.
    connection.local().maxActiveStreams(5);
    encoder.writeSettingsAck(ctx, newPromise());
    flush();

    assertEquals(0, encoder.numBufferedStreams());
    writeVerifyWriteHeaders(times(1), 7);
    writeVerifyWriteHeaders(times(1), 9);

    encoderWriteHeaders(11);
    flush();

    writeVerifyWriteHeaders(times(1), 11);

    assertEquals(5, connection.local().numActiveStreams());
  }

  @Test
  public void bufferUntilSettingsReceived() {
    int initialLimit = BufferingHttp2ConnectionEncoder.SMALLEST_MAX_CONCURRENT_STREAMS;
    int numStreams = initialLimit * 2;
    for (int ix = 0, nextStreamId = 3; ix < numStreams; ++ix, nextStreamId += 2) {
      encoderWriteHeaders(nextStreamId);
      flush();
      if (ix < initialLimit) {
        writeVerifyWriteHeaders(times(1), nextStreamId);
      } else {
        writeVerifyWriteHeaders(never(), nextStreamId);
      }
    }
    assertEquals(numStreams / 2, encoder.numBufferedStreams());

    // Simulate that we received a SETTINGS frame.
    encoder.writeSettingsAck(ctx, newPromise());

    assertEquals(0, encoder.numBufferedStreams());
    assertEquals(numStreams, connection.local().numActiveStreams());
  }

  @Test
  public void closedBufferedStreamReleasesByteBuf() {
    encoder.writeSettingsAck(ctx, newPromise());
    connection.local().maxActiveStreams(0);
    ByteBuf data = mock(ByteBuf.class);

    ChannelFuture headersFuture = encoderWriteHeaders(3);
    assertEquals(1, encoder.numBufferedStreams());
    ChannelFuture dataFuture = encoder.writeData(ctx, 3, data, 0, false, newPromise());

    ChannelPromise rstPromise = mock(ChannelPromise.class);
    encoder.writeRstStream(ctx, 3, CANCEL.code(), rstPromise);

    assertEquals(0, encoder.numBufferedStreams());
    verify(rstPromise).setSuccess();
    assertTrue(headersFuture.isSuccess());
    assertTrue(dataFuture.isSuccess());
    verify(data).release();
  }

  private ChannelFuture encoderWriteHeaders(int streamId) {
    return encoder.writeHeaders(ctx, streamId, new DefaultHttp2Headers(), 0,
        DEFAULT_PRIORITY_WEIGHT, false, 0, false, newPromise());
  }

  private void flush() {
    channel.flush();
  }

  private void writeVerifyWriteHeaders(VerificationMode mode, int streamId) {
    verify(writer, mode).writeHeaders(eq(ctx), eq(streamId), any(Http2Headers.class), eq(0),
                                      eq(DEFAULT_PRIORITY_WEIGHT), eq(false), eq(0),
                                      eq(false), any(ChannelPromise.class));
  }

  private static ByteBuf data() {
    ByteBuf buf = Unpooled.buffer(10);
    for (int i = 0; i < buf.writableBytes(); i++) {
      buf.writeByte(i);
    }
    return buf;
  }

  private ChannelPromise newPromise() {
    return channel.newPromise();
  }
}
