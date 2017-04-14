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
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.spy;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;

import io.grpc.Attributes;
import io.grpc.ClientStreamTracer;
import io.grpc.Codec;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.Status.Code;
import io.grpc.StreamTracer;
import io.grpc.internal.AbstractClientStream2.TransportState;
import io.grpc.internal.MessageFramerTest.ByteWritableBuffer;
import java.io.ByteArrayInputStream;
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

/**
 * Test for {@link AbstractClientStream2}.  This class tries to test functionality in
 * AbstractClientStream2, but not in any super classes.
 */
@RunWith(JUnit4.class)
public class AbstractClientStream2Test {

  @Rule public final ExpectedException thrown = ExpectedException.none();

  private final StatsTraceContext statsTraceCtx = StatsTraceContext.NOOP;
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
  public void cancel_doNotAcceptOk() {
    for (Code code : Code.values()) {
      ClientStreamListener listener = new NoopClientStreamListener();
      AbstractClientStream2 stream = new BaseAbstractClientStream(allocator, statsTraceCtx);
      stream.start(listener);
      if (code != Code.OK) {
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
    ClientStreamListener listener = new NoopClientStreamListener();
    AbstractClientStream2 stream = new BaseAbstractClientStream(allocator, statsTraceCtx);
    stream.start(listener);
    thrown.expect(NullPointerException.class);

    stream.cancel(null);
  }

  @Test
  public void cancel_notifiesOnlyOnce() {
    final BaseTransportState state = new BaseTransportState(statsTraceCtx);
    AbstractClientStream2 stream = new BaseAbstractClientStream(allocator, state, new BaseSink() {
      @Override
      public void cancel(Status errorStatus) {
        // Cancel should eventually result in a transportReportStatus on the transport thread
        state.transportReportStatus(errorStatus, true/*stop delivery*/, new Metadata());
      }
      }, statsTraceCtx);
    stream.start(mockListener);

    stream.cancel(Status.DEADLINE_EXCEEDED);
    stream.cancel(Status.DEADLINE_EXCEEDED);

    verify(mockListener).closed(any(Status.class), any(Metadata.class));
  }

  @Test
  public void startFailsOnNullListener() {
    AbstractClientStream2 stream = new BaseAbstractClientStream(allocator, statsTraceCtx);

    thrown.expect(NullPointerException.class);

    stream.start(null);
  }

  @Test
  public void cantCallStartTwice() {
    AbstractClientStream2 stream = new BaseAbstractClientStream(allocator, statsTraceCtx);
    stream.start(mockListener);
    thrown.expect(IllegalStateException.class);

    stream.start(mockListener);
  }

  @Test
  public void inboundDataReceived_failsOnNullFrame() {
    ClientStreamListener listener = new NoopClientStreamListener();
    AbstractClientStream2 stream = new BaseAbstractClientStream(allocator, statsTraceCtx);
    stream.start(listener);

    TransportState state = stream.transportState();

    thrown.expect(NullPointerException.class);
    state.inboundDataReceived(null);
  }

  @Test
  public void inboundHeadersReceived_notifiesListener() {
    AbstractClientStream2 stream = new BaseAbstractClientStream(allocator, statsTraceCtx);
    stream.start(mockListener);
    Metadata headers = new Metadata();

    stream.transportState().inboundHeadersReceived(headers);
    verify(mockListener).headersRead(headers);
  }

  @Test
  public void inboundHeadersReceived_failsIfStatusReported() {
    AbstractClientStream2 stream = new BaseAbstractClientStream(allocator, statsTraceCtx);
    stream.start(mockListener);
    stream.transportState().transportReportStatus(Status.CANCELLED, false, new Metadata());

    TransportState state = stream.transportState();

    thrown.expect(IllegalStateException.class);
    state.inboundHeadersReceived(new Metadata());
  }

  @Test
  public void inboundHeadersReceived_acceptsGzipEncoding() {
    AbstractClientStream2 stream = new BaseAbstractClientStream(allocator, statsTraceCtx);
    stream.start(mockListener);
    Metadata headers = new Metadata();
    headers.put(GrpcUtil.MESSAGE_ENCODING_KEY, new Codec.Gzip().getMessageEncoding());

    stream.transportState().inboundHeadersReceived(headers);
    verify(mockListener).headersRead(headers);
  }

  @Test
  public void inboundHeadersReceived_acceptsIdentityEncoding() {
    AbstractClientStream2 stream = new BaseAbstractClientStream(allocator, statsTraceCtx);
    stream.start(mockListener);
    Metadata headers = new Metadata();
    headers.put(GrpcUtil.MESSAGE_ENCODING_KEY, Codec.Identity.NONE.getMessageEncoding());

    stream.transportState().inboundHeadersReceived(headers);
    verify(mockListener).headersRead(headers);
  }

  @Test
  public void rstStreamClosesStream() {
    AbstractClientStream2 stream = new BaseAbstractClientStream(allocator, statsTraceCtx);
    stream.start(mockListener);
    // The application will call request when waiting for a message, which will in turn call this
    // on the transport thread.
    stream.transportState().requestMessagesFromDeframer(1);
    // Send first byte of 2 byte message
    stream.transportState().deframe(ReadableBuffers.wrap(new byte[] {0, 0, 0, 0, 2, 1}), false);
    Status status = Status.INTERNAL;
    // Simulate getting a reset
    stream.transportState().transportReportStatus(status, false /*stop delivery*/, new Metadata());

    verify(mockListener).closed(any(Status.class), any(Metadata.class));
  }
  
  @Test
  public void getRequest() {
    AbstractClientStream2.Sink sink = mock(AbstractClientStream2.Sink.class);
    final ClientStreamTracer tracer = spy(new ClientStreamTracer() {});
    ClientStreamTracer.Factory tracerFactory =
        new ClientStreamTracer.Factory() {
          @Override
          public ClientStreamTracer newClientStreamTracer(Metadata headers) {
            return tracer;
          }
        };
    StatsTraceContext statsTraceCtx = new StatsTraceContext(new StreamTracer[] {tracer});
    AbstractClientStream2 stream = new BaseAbstractClientStream(allocator,
        new BaseTransportState(statsTraceCtx), sink, statsTraceCtx, true);
    stream.start(mockListener);
    stream.writeMessage(new ByteArrayInputStream(new byte[1]));
    // writeHeaders will be delayed since we're sending a GET request.
    verify(sink, never()).writeHeaders(any(Metadata.class), any(byte[].class));
    // halfClose will trigger writeHeaders.
    stream.halfClose();
    ArgumentCaptor<byte[]> payloadCaptor = ArgumentCaptor.forClass(byte[].class);
    verify(sink).writeHeaders(any(Metadata.class), payloadCaptor.capture());
    assertTrue(payloadCaptor.getValue() != null);
    // GET requests don't have BODY.
    verify(sink, never())
        .writeFrame(any(WritableBuffer.class), any(Boolean.class), any(Boolean.class));
    verify(tracer).outboundMessage();
    verify(tracer).outboundWireSize(1);
    verify(tracer).outboundUncompressedSize(1);
    verifyNoMoreInteractions(tracer);
  }

  /**
   * No-op base class for testing.
   */
  private static class BaseAbstractClientStream extends AbstractClientStream2 {
    private final TransportState state;
    private final Sink sink;

    public BaseAbstractClientStream(WritableBufferAllocator allocator,
        StatsTraceContext statsTraceCtx) {
      this(allocator, new BaseTransportState(statsTraceCtx), new BaseSink(), statsTraceCtx);
    }

    public BaseAbstractClientStream(WritableBufferAllocator allocator, TransportState state,
        Sink sink, StatsTraceContext statsTraceCtx) {
      this(allocator, state, sink, statsTraceCtx, false);
    }

    public BaseAbstractClientStream(WritableBufferAllocator allocator, TransportState state,
        Sink sink, StatsTraceContext statsTraceCtx, boolean useGet) {
      super(allocator, statsTraceCtx, new Metadata(), useGet);
      this.state = state;
      this.sink = sink;
    }

    @Override
    protected Sink abstractClientStreamSink() {
      return sink;
    }

    @Override
    public TransportState transportState() {
      return state;
    }

    @Override
    public void setAuthority(String authority) {}

    @Override
    public void setMaxInboundMessageSize(int maxSize) {}

    @Override
    public void setMaxOutboundMessageSize(int maxSize) {}

    @Override
    public Attributes getAttributes() {
      return Attributes.EMPTY;
    }
  }

  private static class BaseSink implements AbstractClientStream2.Sink {
    @Override
    public void writeHeaders(Metadata headers, byte[] payload) {}

    @Override
    public void request(int numMessages) {}

    @Override
    public void writeFrame(WritableBuffer frame, boolean endOfStream, boolean flush) {}

    @Override
    public void cancel(Status reason) {}
  }

  private static class BaseTransportState extends AbstractClientStream2.TransportState {
    public BaseTransportState(StatsTraceContext statsTraceCtx) {
      super(DEFAULT_MAX_MESSAGE_SIZE, statsTraceCtx);
    }

    @Override
    protected void deframeFailed(Throwable cause) {}

    @Override
    public void bytesRead(int processedBytes) {}
  }
}
