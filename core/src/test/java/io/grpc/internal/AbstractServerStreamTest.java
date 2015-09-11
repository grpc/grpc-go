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

package io.grpc.internal;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.isA;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;

import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.internal.AbstractStream.Phase;
import io.grpc.internal.MessageFramerTest.ByteWritableBuffer;

import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.io.ByteArrayInputStream;
import java.io.InputStream;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicReference;

import javax.annotation.Nullable;

/**
 * Tests for {@link AbstractServerStream}.
 */
@RunWith(JUnit4.class)
public class AbstractServerStreamTest {
  private static int MAX_MESSAGE_SIZE = 100;

  @Rule public final ExpectedException thrown = ExpectedException.none();

  private final WritableBufferAllocator allocator = new WritableBufferAllocator() {
    @Override
    public WritableBuffer allocate(int capacityHint) {
      return new ByteWritableBuffer(capacityHint);
    }
  };

  private final AbstractServerStreamBase defaultStream =
      new AbstractServerStreamBase(allocator, MAX_MESSAGE_SIZE);

  @Test
  public void setListener_setOnlyOnce() {

    defaultStream.setListener(new ServerStreamListenerBase());
    thrown.expect(IllegalStateException.class);

    defaultStream.setListener(new ServerStreamListenerBase());
  }

  @Test
  public void setListener_readyCalled() {
    ServerStreamListener streamListener = mock(ServerStreamListener.class);
    defaultStream.setListener(streamListener);

    verify(streamListener).onReady();
  }

  @Test
  public void setListener_failsOnNull() {
    thrown.expect(NullPointerException.class);

    defaultStream.setListener(null);
  }

  @Test
  public void receiveMessage_listenerCalled() {
    final ServerStreamListener streamListener = mock(ServerStreamListener.class);
    AbstractServerStreamBase stream = new AbstractServerStreamBase(allocator, MAX_MESSAGE_SIZE) {
      @Override
      protected ServerStreamListener listener() {
        return streamListener;
      }
    };

    // Normally called by a deframe event.
    stream.receiveMessage(new ByteArrayInputStream(new byte[]{}));

    verify(streamListener).messageRead(isA(InputStream.class));
  }

  @Test
  public void receiveMessage_failsIfHalfClosed() {
    // Simulate being closed, without invoking the listener
    defaultStream.inboundPhase(Phase.STATUS);

    thrown.expect(IllegalStateException.class);

    // Normally called by a deframe event.
    defaultStream.receiveMessage(new ByteArrayInputStream(new byte[]{}));
  }

  @Test
  public void writeHeaders_failsOnNullHeaders() {
    AbstractServerStreamBase stream = new AbstractServerStreamBase(allocator, MAX_MESSAGE_SIZE);
    thrown.expect(NullPointerException.class);

    stream.writeHeaders(null);
  }

  @Test
  public void writeHeaders_failsIfAlreadySent() {
    defaultStream.writeHeaders(new Metadata());
    thrown.expect(IllegalStateException.class);

    defaultStream.writeHeaders(new Metadata());
  }

  @Test
  public void writeHeaders() {
    final AtomicReference<Metadata> capturedHeaders = new AtomicReference<Metadata>(null);
    Metadata headers = new Metadata();
    AbstractServerStreamBase stream = new AbstractServerStreamBase(allocator, MAX_MESSAGE_SIZE) {
      @Override
      protected void internalSendHeaders(Metadata captured) {
        capturedHeaders.set(captured);
      }
    };

    stream.writeHeaders(headers);

    assertEquals(headers, capturedHeaders.get());
    assertEquals(Phase.MESSAGE, stream.outboundPhase());
  }

  @Test
  public void writeMessage_writeHeadersIfNeeded() {
    final AtomicReference<Metadata> capturedHeaders = new AtomicReference<Metadata>(null);
    AbstractServerStreamBase stream = new AbstractServerStreamBase(allocator, MAX_MESSAGE_SIZE) {
      @Override
      protected void internalSendHeaders(Metadata captured) {
        capturedHeaders.set(captured);
      }
    };
    stream.writeHeaders(new Metadata());

    stream.writeMessage(new ByteArrayInputStream(new byte[]{}));

    assertNotNull(capturedHeaders.get());
  }

  @Test
  public void writeMessage_dontWriteDuplicateHeaders() {
    final AtomicReference<Metadata> capturedHeaders = new AtomicReference<Metadata>(null);
    Metadata headers = new Metadata();
    AbstractServerStreamBase stream = new AbstractServerStreamBase(allocator, MAX_MESSAGE_SIZE) {
      @Override
      protected void internalSendHeaders(Metadata captured) {
        capturedHeaders.set(captured);
      }
    };
    stream.writeHeaders(headers);

    stream.writeMessage(new ByteArrayInputStream(new byte[]{}));

    // Make sure it wasn't called twice, by checking that the exact headers sent are the ones
    // returned.
    assertSame(headers, capturedHeaders.get());
  }

  @Test
  public void writeMessage_ignoreIfFramerClosed() {
    final AtomicBoolean sendCalled = new AtomicBoolean();
    AbstractServerStreamBase stream = new AbstractServerStreamBase(allocator, MAX_MESSAGE_SIZE) {
      @Override
      protected void sendFrame(WritableBuffer frame, boolean endOfStream, boolean flush) {
        sendCalled.set(true);
      }
    };
    stream.writeHeaders(new Metadata());
    stream.closeFramer();

    stream.writeMessage(new ByteArrayInputStream(new byte[]{}));

    assertFalse(sendCalled.get());
  }

  @Test
  public void writeMessage() {
    final AtomicBoolean sendCalled = new AtomicBoolean();
    AbstractServerStreamBase stream = new AbstractServerStreamBase(allocator, MAX_MESSAGE_SIZE) {
      @Override
      protected void sendFrame(WritableBuffer frame, boolean endOfStream, boolean flush) {
        sendCalled.set(true);
      }
    };
    stream.writeHeaders(new Metadata());

    stream.writeMessage(new ByteArrayInputStream(new byte[]{}));
    // Force the message to be flushed
    stream.closeFramer();

    assertTrue(sendCalled.get());
    assertEquals(Phase.MESSAGE, stream.outboundPhase());
  }

  @Test
  public void close_failsOnNullStatus() {
    thrown.expect(NullPointerException.class);

    defaultStream.close(null, new Metadata());
  }

  @Test
  public void close_failsOnNullMetadata() {
    thrown.expect(NullPointerException.class);

    defaultStream.close(Status.INTERNAL, null);
  }

  @Test
  public void close_sendsTrailers() {
    final AtomicReference<Metadata> capturedTrailers = new AtomicReference<Metadata>(null);
    AbstractServerStreamBase stream = new AbstractServerStreamBase(allocator, MAX_MESSAGE_SIZE) {
      @Override
      protected void sendTrailers(Metadata trailers, boolean headersSent) {
        capturedTrailers.set(trailers);
      }
    };
    Metadata trailers = new Metadata();

    stream.close(Status.INTERNAL, trailers);

    assertSame(trailers, capturedTrailers.get());
  }

  @Test
  public void close_sendTrailersClearsReservedFields() {
    final AtomicReference<Metadata> capturedTrailers = new AtomicReference<Metadata>(null);
    AbstractServerStreamBase stream = new AbstractServerStreamBase(allocator, MAX_MESSAGE_SIZE) {
      @Override
      protected void sendTrailers(Metadata trailers, boolean headersSent) {
        capturedTrailers.set(trailers);
      }
    };
    // stream actually mutates trailers, so we can't check that the fields here are the same as
    // the captured ones.
    Metadata trailers = new Metadata();
    trailers.put(Status.CODE_KEY, Status.OK);
    trailers.put(Status.MESSAGE_KEY, "Everything's super.");

    stream.close(Status.INTERNAL.withDescription("bad"), trailers);

    assertEquals(Status.Code.INTERNAL, capturedTrailers.get().get(Status.CODE_KEY).getCode());
    assertEquals("bad", capturedTrailers.get().get(Status.MESSAGE_KEY));
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

  private static class AbstractServerStreamBase extends AbstractServerStream<Void> {
    protected AbstractServerStreamBase(
        WritableBufferAllocator bufferAllocator, int maxMessageSize) {
      super(bufferAllocator, maxMessageSize);
    }

    @Override
    public void cancel(Status status) {}

    @Override
    public void request(int numMessages) {}

    @Override
    protected void internalSendHeaders(Metadata headers) {}

    @Override
    protected void sendFrame(WritableBuffer frame, boolean endOfStream, boolean flush) {}

    @Override
    protected void sendTrailers(Metadata trailers, boolean headersSent) {}

    @Override
    @Nullable
    public Void id() {
      return null;
    }

    @Override
    protected void inboundDeliveryPaused() {}

    @Override
    protected void returnProcessedBytes(int processedBytes) {}
  }
}

