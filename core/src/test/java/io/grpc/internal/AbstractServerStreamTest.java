/*
 * Copyright 2015, gRPC Authors All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package io.grpc.internal;

import static org.junit.Assert.assertEquals;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.isA;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.reset;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;

import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.internal.AbstractServerStream.TransportState;
import io.grpc.internal.MessageFramerTest.ByteWritableBuffer;
import java.io.ByteArrayInputStream;
import java.io.InputStream;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;

/**
 * Tests for {@link AbstractServerStream}.
 */
@RunWith(JUnit4.class)
public class AbstractServerStreamTest {
  private static final int MAX_MESSAGE_SIZE = 100;

  @Rule public final ExpectedException thrown = ExpectedException.none();

  private final WritableBufferAllocator allocator = new WritableBufferAllocator() {
    @Override
    public WritableBuffer allocate(int capacityHint) {
      return new ByteWritableBuffer(capacityHint);
    }
  };

  private AbstractServerStream.Sink sink = mock(AbstractServerStream.Sink.class);
  private AbstractServerStreamBase stream = new AbstractServerStreamBase(
      allocator, sink, new AbstractServerStreamBase.TransportState(MAX_MESSAGE_SIZE));
  private final ArgumentCaptor<Metadata> metadataCaptor = ArgumentCaptor.forClass(Metadata.class);

  /**
   * Test for issue https://github.com/grpc/grpc-java/issues/1795
   */
  @Test
  public void frameShouldBeIgnoredAfterDeframerClosed() {
    ServerStreamListener streamListener = mock(ServerStreamListener.class);
    ReadableBuffer buffer = mock(ReadableBuffer.class);

    stream.transportState().setListener(streamListener);
    // Close the deframer
    stream.transportState().complete();
    // Frame received after deframer closed, should be ignored and not trigger an exception
    stream.transportState().inboundDataReceived(buffer, true);

    verify(buffer).close();
    verify(streamListener, times(0)).messageRead(any(InputStream.class));
  }

  /**
   * Test for issue https://github.com/grpc/grpc-java/issues/615
   */
  @Test
  public void completeWithoutClose() {
    stream.transportState().setListener(new ServerStreamListenerBase());
    // Test that it doesn't throw an exception
    stream.transportState().complete();
  }

  @Test
  public void setListener_setOnlyOnce() {
    TransportState state = stream.transportState();
    state.setListener(new ServerStreamListenerBase());
    thrown.expect(IllegalStateException.class);

    state.setListener(new ServerStreamListenerBase());
  }

  @Test
  public void listenerReady_onlyOnce() {
    stream.transportState().setListener(new ServerStreamListenerBase());
    stream.transportState().onStreamAllocated();

    TransportState state = stream.transportState();

    thrown.expect(IllegalStateException.class);
    state.onStreamAllocated();
  }


  @Test
  public void listenerReady_readyCalled() {
    ServerStreamListener streamListener = mock(ServerStreamListener.class);
    stream.transportState().setListener(streamListener);
    stream.transportState().onStreamAllocated();

    verify(streamListener).onReady();
  }

  @Test
  public void setListener_failsOnNull() {
    TransportState state = stream.transportState();

    thrown.expect(NullPointerException.class);
    state.setListener(null);
  }

  @Test
  public void messageRead_listenerCalled() {
    final ServerStreamListener streamListener = mock(ServerStreamListener.class);
    stream.transportState().setListener(streamListener);

    // Normally called by a deframe event.
    stream.transportState().messageRead(new ByteArrayInputStream(new byte[]{}));

    verify(streamListener).messageRead(isA(InputStream.class));
  }

  @Test
  public void writeHeaders_failsOnNullHeaders() {
    thrown.expect(NullPointerException.class);

    stream.writeHeaders(null);
  }

  @Test
  public void writeHeaders() {
    Metadata headers = new Metadata();
    stream.writeHeaders(headers);
    verify(sink).writeHeaders(same(headers));
  }

  @Test
  public void writeMessage_dontWriteDuplicateHeaders() {
    stream.writeHeaders(new Metadata());
    stream.writeMessage(new ByteArrayInputStream(new byte[]{}));

    // Make sure it wasn't called twice
    verify(sink).writeHeaders(any(Metadata.class));
  }

  @Test
  public void writeMessage_ignoreIfFramerClosed() {
    stream.writeHeaders(new Metadata());
    stream.endOfMessages();
    reset(sink);

    stream.writeMessage(new ByteArrayInputStream(new byte[]{}));

    verify(sink, never()).writeFrame(any(WritableBuffer.class), any(Boolean.class));
  }

  @Test
  public void writeMessage() {
    stream.writeHeaders(new Metadata());

    stream.writeMessage(new ByteArrayInputStream(new byte[]{}));
    stream.flush();

    verify(sink).writeFrame(any(WritableBuffer.class), eq(true));
  }

  @Test
  public void close_failsOnNullStatus() {
    thrown.expect(NullPointerException.class);

    stream.close(null, new Metadata());
  }

  @Test
  public void close_failsOnNullMetadata() {
    thrown.expect(NullPointerException.class);

    stream.close(Status.INTERNAL, null);
  }

  @Test
  public void close_sendsTrailers() {
    Metadata trailers = new Metadata();
    stream.close(Status.INTERNAL, trailers);
    verify(sink).writeTrailers(any(Metadata.class), eq(false));
  }

  @Test
  public void close_sendTrailersClearsReservedFields() {
    // stream actually mutates trailers, so we can't check that the fields here are the same as
    // the captured ones.
    Metadata trailers = new Metadata();
    trailers.put(Status.CODE_KEY, Status.OK);
    trailers.put(Status.MESSAGE_KEY, "Everything's super.");

    stream.close(Status.INTERNAL.withDescription("bad"), trailers);

    verify(sink).writeTrailers(metadataCaptor.capture(), eq(false));
    assertEquals(Status.Code.INTERNAL, metadataCaptor.getValue().get(Status.CODE_KEY).getCode());
    assertEquals("bad", metadataCaptor.getValue().get(Status.MESSAGE_KEY));
  }

  private static class ServerStreamListenerBase implements ServerStreamListener {
    @Override
    public void messageRead(InputStream message) {}

    @Override
    public void onReady() {}

    @Override
    public void halfClosed() {}

    @Override
    public void closed(Status status) {}
  }

  private static class AbstractServerStreamBase extends AbstractServerStream {
    private final Sink sink;
    private final AbstractServerStream.TransportState state;

    protected AbstractServerStreamBase(WritableBufferAllocator bufferAllocator, Sink sink,
        AbstractServerStream.TransportState state) {
      super(bufferAllocator, StatsTraceContext.NOOP);
      this.sink = sink;
      this.state = state;
    }

    @Override
    protected Sink abstractServerStreamSink() {
      return sink;
    }

    @Override
    protected AbstractServerStream.TransportState transportState() {
      return state;
    }

    static class TransportState extends AbstractServerStream.TransportState {
      protected TransportState(int maxMessageSize) {
        super(maxMessageSize, StatsTraceContext.NOOP);
      }

      @Override
      protected void deframeFailed(Throwable cause) {}

      @Override
      public void bytesRead(int processedBytes) {}
    }
  }
}

