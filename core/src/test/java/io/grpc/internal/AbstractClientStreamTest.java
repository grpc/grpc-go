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

import static io.grpc.internal.GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;

import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.ClientStreamTracer;
import io.grpc.Codec;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.Status.Code;
import io.grpc.StreamTracer;
import io.grpc.internal.AbstractClientStream.TransportState;
import io.grpc.internal.MessageFramerTest.ByteWritableBuffer;
import io.grpc.internal.testing.TestClientStreamTracer;
import java.io.ByteArrayInputStream;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/**
 * Test for {@link AbstractClientStream}.  This class tries to test functionality in
 * AbstractClientStream, but not in any super classes.
 */
@RunWith(JUnit4.class)
public class AbstractClientStreamTest {

  @Rule public final ExpectedException thrown = ExpectedException.none();

  private final StatsTraceContext statsTraceCtx = StatsTraceContext.NOOP;
  @Mock private ClientStreamListener mockListener;

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
      AbstractClientStream stream = new BaseAbstractClientStream(allocator, statsTraceCtx);
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
    AbstractClientStream stream = new BaseAbstractClientStream(allocator, statsTraceCtx);
    stream.start(listener);
    thrown.expect(NullPointerException.class);

    stream.cancel(null);
  }

  @Test
  public void cancel_notifiesOnlyOnce() {
    final BaseTransportState state = new BaseTransportState(statsTraceCtx);
    AbstractClientStream stream = new BaseAbstractClientStream(allocator, state, new BaseSink() {
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
    AbstractClientStream stream = new BaseAbstractClientStream(allocator, statsTraceCtx);

    thrown.expect(NullPointerException.class);

    stream.start(null);
  }

  @Test
  public void cantCallStartTwice() {
    AbstractClientStream stream = new BaseAbstractClientStream(allocator, statsTraceCtx);
    stream.start(mockListener);
    thrown.expect(IllegalStateException.class);

    stream.start(mockListener);
  }

  @Test
  public void inboundDataReceived_failsOnNullFrame() {
    ClientStreamListener listener = new NoopClientStreamListener();
    AbstractClientStream stream = new BaseAbstractClientStream(allocator, statsTraceCtx);
    stream.start(listener);

    TransportState state = stream.transportState();

    thrown.expect(NullPointerException.class);
    state.inboundDataReceived(null);
  }

  @Test
  public void inboundHeadersReceived_notifiesListener() {
    AbstractClientStream stream = new BaseAbstractClientStream(allocator, statsTraceCtx);
    stream.start(mockListener);
    Metadata headers = new Metadata();

    stream.transportState().inboundHeadersReceived(headers);
    verify(mockListener).headersRead(headers);
  }

  @Test
  public void inboundHeadersReceived_failsIfStatusReported() {
    AbstractClientStream stream = new BaseAbstractClientStream(allocator, statsTraceCtx);
    stream.start(mockListener);
    stream.transportState().transportReportStatus(Status.CANCELLED, false, new Metadata());

    TransportState state = stream.transportState();

    thrown.expect(IllegalStateException.class);
    state.inboundHeadersReceived(new Metadata());
  }

  @Test
  public void inboundHeadersReceived_acceptsGzipEncoding() {
    AbstractClientStream stream = new BaseAbstractClientStream(allocator, statsTraceCtx);
    stream.start(mockListener);
    Metadata headers = new Metadata();
    headers.put(GrpcUtil.MESSAGE_ENCODING_KEY, new Codec.Gzip().getMessageEncoding());

    stream.transportState().inboundHeadersReceived(headers);
    verify(mockListener).headersRead(headers);
  }

  @Test
  public void inboundHeadersReceived_acceptsIdentityEncoding() {
    AbstractClientStream stream = new BaseAbstractClientStream(allocator, statsTraceCtx);
    stream.start(mockListener);
    Metadata headers = new Metadata();
    headers.put(GrpcUtil.MESSAGE_ENCODING_KEY, Codec.Identity.NONE.getMessageEncoding());

    stream.transportState().inboundHeadersReceived(headers);
    verify(mockListener).headersRead(headers);
  }

  @Test
  public void rstStreamClosesStream() {
    AbstractClientStream stream = new BaseAbstractClientStream(allocator, statsTraceCtx);
    stream.start(mockListener);
    // The application will call request when waiting for a message, which will in turn call this
    // on the transport thread.
    stream.transportState().requestMessagesFromDeframer(1);
    // Send first byte of 2 byte message
    stream.transportState().deframe(ReadableBuffers.wrap(new byte[] {0, 0, 0, 0, 2, 1}));
    Status status = Status.INTERNAL;
    // Simulate getting a reset
    stream.transportState().transportReportStatus(status, false /*stop delivery*/, new Metadata());

    verify(mockListener).closed(any(Status.class), any(Metadata.class));
  }
  
  @Test
  public void getRequest() {
    AbstractClientStream.Sink sink = mock(AbstractClientStream.Sink.class);
    final TestClientStreamTracer tracer = new TestClientStreamTracer();
    ClientStreamTracer.Factory tracerFactory =
        new ClientStreamTracer.Factory() {
          @Override
          public ClientStreamTracer newClientStreamTracer(
              CallOptions callOptions, Metadata headers) {
            return tracer;
          }
        };
    StatsTraceContext statsTraceCtx = new StatsTraceContext(new StreamTracer[] {tracer});
    AbstractClientStream stream = new BaseAbstractClientStream(allocator,
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
    assertEquals(1, tracer.getOutboundMessageCount());
    assertEquals(1, tracer.getOutboundWireSize());
    assertEquals(1, tracer.getOutboundUncompressedSize());
  }

  /**
   * No-op base class for testing.
   */
  private static class BaseAbstractClientStream extends AbstractClientStream {
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

  private static class BaseSink implements AbstractClientStream.Sink {
    @Override
    public void writeHeaders(Metadata headers, byte[] payload) {}

    @Override
    public void request(int numMessages) {}

    @Override
    public void writeFrame(WritableBuffer frame, boolean endOfStream, boolean flush) {}

    @Override
    public void cancel(Status reason) {}
  }

  private static class BaseTransportState extends AbstractClientStream.TransportState {
    public BaseTransportState(StatsTraceContext statsTraceCtx) {
      super(DEFAULT_MAX_MESSAGE_SIZE, statsTraceCtx);
    }

    @Override
    protected void deframeFailed(Throwable cause) {}

    @Override
    public void bytesRead(int processedBytes) {}
  }
}
