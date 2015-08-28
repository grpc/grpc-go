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

import static io.grpc.internal.GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.isA;
import static org.mockito.Mockito.verify;

import io.grpc.Codec;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.Status.Code;
import io.grpc.internal.AbstractStream.Phase;
import io.grpc.internal.MessageFramerTest.ByteWritableBuffer;

import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.io.InputStream;

/**
 * Test for {@link AbstractClientStream}.  This class tries to test functionality in
 * AbstractClientStream, but not in any super classes.
 */
@RunWith(JUnit4.class)
public class AbstractClientStreamTest {

  @Rule public final ExpectedException thrown = ExpectedException.none();

  @Mock private ClientStreamListener mockListener;
  @Captor private ArgumentCaptor<Status> statusCaptor;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
  }

  private final WritableBufferAllocator allocator = new WritableBufferAllocator() {
    @Override
    public WritableBuffer allocate(int capacityHint) {
      return new ByteWritableBuffer(capacityHint);
    }
  };

  @Test
  public void cancel_onlyExpectedCodesAccepted() {
    for (Code code : Code.values()) {
      ClientStreamListener listener = new BaseClientStreamListener();
      AbstractClientStream<Integer> stream =
          new BaseAbstractClientStream<Integer>(allocator, listener);
      if (code == Code.DEADLINE_EXCEEDED || code == Code.CANCELLED || code == Code.INTERNAL
          || code == Code.UNKNOWN) {
        stream.cancel(Status.fromCodeValue(code.value()));
      } else {
        try {
          stream.cancel(Status.fromCodeValue(code.value()));
          fail();
        } catch (IllegalArgumentException e) {
          // ignore
        }
      }
    }
  }

  @Test
  public void cancel_failsOnNull() {
    ClientStreamListener listener = new BaseClientStreamListener();
    AbstractClientStream<Integer> stream =
        new BaseAbstractClientStream<Integer>(allocator, listener);
    thrown.expect(NullPointerException.class);

    stream.cancel(null);
  }

  @Test
  public void cancel_notifiesOnlyOnce() {
    AbstractClientStream<Integer> stream =
        new BaseAbstractClientStream<Integer>(allocator, mockListener) {
      @Override
      protected void sendCancel(Status errorStatus) {
        transportReportStatus(errorStatus, true/*stop delivery*/, new Metadata());
      }
    };

    stream.cancel(Status.DEADLINE_EXCEEDED);

    verify(mockListener).closed(isA(Status.class), isA(Metadata.class));
  }

  @Test
  public void newStreamFailsOnNullListener() {
    thrown.expect(NullPointerException.class);

    new BaseAbstractClientStream<Integer>(allocator, null);
  }

  @Test
  public void deframeFailed_notifiesListener() {
    AbstractClientStream<Integer> stream =
        new BaseAbstractClientStream<Integer>(allocator, mockListener) {
      @Override
      protected void sendCancel(Status errorStatus) {
        transportReportStatus(errorStatus, true/*stop delivery*/, new Metadata());
      }
    };

    stream.deframeFailed(new RuntimeException("something bad"));

    verify(mockListener).closed(statusCaptor.capture(), isA(Metadata.class));
    assertEquals(Code.INTERNAL, statusCaptor.getValue().getCode());
  }

  @Test
  public void inboundDataReceived_failsOnNullFrame() {
    ClientStreamListener listener = new BaseClientStreamListener();
    AbstractClientStream<Integer> stream =
        new BaseAbstractClientStream<Integer>(allocator, listener);
    thrown.expect(NullPointerException.class);

    stream.inboundDataReceived(null);
  }

  @Test
  public void inboundDataReceived_failsOnNoHeaders() {
    AbstractClientStream<Integer> stream =
        new BaseAbstractClientStream<Integer>(allocator, mockListener);
    stream.inboundPhase(Phase.HEADERS);

    stream.inboundDataReceived(ReadableBuffers.empty());

    verify(mockListener).closed(statusCaptor.capture(), isA(Metadata.class));
    assertEquals(Code.INTERNAL, statusCaptor.getValue().getCode());
  }

  @Test
  public void inboundHeadersReceived_notifiesListener() {
    AbstractClientStream<Integer> stream =
        new BaseAbstractClientStream<Integer>(allocator, mockListener);
    Metadata headers = new Metadata();

    stream.inboundHeadersReceived(headers);
    verify(mockListener).headersRead(headers);
  }

  @Test
  public void inboundHeadersReceived_failsOnPhaseStatus() {
    AbstractClientStream<Integer> stream =
        new BaseAbstractClientStream<Integer>(allocator, mockListener);
    Metadata headers = new Metadata();
    stream.inboundPhase(Phase.STATUS);

    thrown.expect(IllegalStateException.class);

    stream.inboundHeadersReceived(headers);
  }

  @Test
  public void inboundHeadersReceived_succeedsOnPhaseMessage() {
    AbstractClientStream<Integer> stream =
        new BaseAbstractClientStream<Integer>(allocator, mockListener);
    Metadata headers = new Metadata();
    stream.inboundPhase(Phase.MESSAGE);

    stream.inboundHeadersReceived(headers);

    verify(mockListener).headersRead(headers);
  }

  @Test
  public void inboundHeadersReceived_acceptsGzipEncoding() {
    AbstractClientStream<Integer> stream =
        new BaseAbstractClientStream<Integer>(allocator, mockListener);
    Metadata headers = new Metadata();
    headers.put(GrpcUtil.MESSAGE_ENCODING_KEY, new Codec.Gzip().getMessageEncoding());

    stream.inboundHeadersReceived(headers);
    verify(mockListener).headersRead(headers);
  }

  @Test
  public void inboundHeadersReceived_acceptsIdentityEncoding() {
    AbstractClientStream<Integer> stream =
        new BaseAbstractClientStream<Integer>(allocator, mockListener);
    Metadata headers = new Metadata();
    headers.put(GrpcUtil.MESSAGE_ENCODING_KEY, Codec.Identity.NONE.getMessageEncoding());

    stream.inboundHeadersReceived(headers);
    verify(mockListener).headersRead(headers);
  }

  @Test
  public void inboundHeadersReceived_notifiesListenerOnBadEncoding() {
    AbstractClientStream<Integer> stream =
        new BaseAbstractClientStream<Integer>(allocator, mockListener);
    Metadata headers = new Metadata();
    headers.put(GrpcUtil.MESSAGE_ENCODING_KEY, "bad");

    stream.inboundHeadersReceived(headers);

    verify(mockListener).closed(statusCaptor.capture(), isA(Metadata.class));
    assertEquals(Code.INTERNAL, statusCaptor.getValue().getCode());
  }

  @Test
  public void rstStreamClosesStream() {
    AbstractClientStream<Integer> stream =
        new BaseAbstractClientStream<Integer>(allocator, mockListener);
    // The application will call request when waiting for a message, which will in turn call this
    // on the transport thread.
    stream.requestMessagesFromDeframer(1);
    // Send first byte of 2 byte message
    stream.deframe(ReadableBuffers.wrap(new byte[] {0, 0, 0, 0, 2, 1}), false);
    Status status = Status.INTERNAL;
    // Simulate getting a reset
    stream.transportReportStatus(status, false /*stop delivery*/, new Metadata());

    assertTrue(stream.isClosed());
  }

  /**
   * No-op base class for testing.
   */
  private static class BaseAbstractClientStream<T> extends AbstractClientStream<T> {
    protected BaseAbstractClientStream(
        WritableBufferAllocator allocator, ClientStreamListener listener) {
      super(allocator, listener, DEFAULT_MAX_MESSAGE_SIZE);
    }

    @Override
    public void request(int numMessages) {}

    @Override
    protected void sendFrame(WritableBuffer frame, boolean endOfStream, boolean flush) {}

    @Override
    protected void sendCancel(Status reason) {}

    @Override
    public T id() {
      return null;
    }

    @Override
    protected void returnProcessedBytes(int processedBytes) {}
  }

  /**
   * No-op base class for testing.
   */
  private static class BaseClientStreamListener implements ClientStreamListener {
    @Override
    public void messageRead(InputStream message) {}

    @Override
    public void onReady() {}

    @Override
    public void headersRead(Metadata headers) {}

    @Override
    public void closed(Status status, Metadata trailers) {}
  }
}
