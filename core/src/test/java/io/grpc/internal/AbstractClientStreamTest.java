/*
 * Copyright 2015 The gRPC Authors
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

import static com.google.common.truth.Truth.assertThat;
import static io.grpc.internal.ClientStreamListener.RpcProgress.PROCESSED;
import static io.grpc.internal.GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.AdditionalAnswers.delegatesTo;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;

import io.grpc.Attributes;
import io.grpc.Codec;
import io.grpc.Deadline;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.Status.Code;
import io.grpc.StreamTracer;
import io.grpc.internal.AbstractClientStream.TransportState;
import io.grpc.internal.ClientStreamListener.RpcProgress;
import io.grpc.internal.MessageFramerTest.ByteWritableBuffer;
import io.grpc.internal.testing.TestClientStreamTracer;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.concurrent.TimeUnit;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Matchers;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

/**
 * Test for {@link AbstractClientStream}.  This class tries to test functionality in
 * AbstractClientStream, but not in any super classes.
 */
@RunWith(JUnit4.class)
public class AbstractClientStreamTest {

  @Rule public final ExpectedException thrown = ExpectedException.none();

  private final StatsTraceContext statsTraceCtx = StatsTraceContext.NOOP;
  private final TransportTracer transportTracer = new TransportTracer();
  @Mock private ClientStreamListener mockListener;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);

    doAnswer(new Answer<Void>() {
      @Override
      public Void answer(InvocationOnMock invocation) throws Throwable {
        StreamListener.MessageProducer producer =
            (StreamListener.MessageProducer) invocation.getArguments()[0];
        while (producer.next() != null) {}
        return null;
      }
    }).when(mockListener).messagesAvailable(Matchers.<StreamListener.MessageProducer>any());
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
      AbstractClientStream stream =
          new BaseAbstractClientStream(allocator, statsTraceCtx, transportTracer);
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
    AbstractClientStream stream =
        new BaseAbstractClientStream(allocator, statsTraceCtx, transportTracer);
    stream.start(listener);
    thrown.expect(NullPointerException.class);

    stream.cancel(null);
  }

  @Test
  public void cancel_notifiesOnlyOnce() {
    final BaseTransportState state = new BaseTransportState(statsTraceCtx, transportTracer);
    AbstractClientStream stream = new BaseAbstractClientStream(allocator, state, new BaseSink() {
      @Override
      public void cancel(Status errorStatus) {
        // Cancel should eventually result in a transportReportStatus on the transport thread
        state.transportReportStatus(errorStatus, true/*stop delivery*/, new Metadata());
      }
    }, statsTraceCtx, transportTracer);
    stream.start(mockListener);

    stream.cancel(Status.DEADLINE_EXCEEDED);
    stream.cancel(Status.DEADLINE_EXCEEDED);

    verify(mockListener).closed(any(Status.class), same(PROCESSED), any(Metadata.class));
  }

  @Test
  public void startFailsOnNullListener() {
    AbstractClientStream stream =
        new BaseAbstractClientStream(allocator, statsTraceCtx, transportTracer);

    thrown.expect(NullPointerException.class);

    stream.start(null);
  }

  @Test
  public void cantCallStartTwice() {
    AbstractClientStream stream =
        new BaseAbstractClientStream(allocator, statsTraceCtx, transportTracer);
    stream.start(mockListener);
    thrown.expect(IllegalStateException.class);

    stream.start(mockListener);
  }

  @Test
  public void inboundDataReceived_failsOnNullFrame() {
    ClientStreamListener listener = new NoopClientStreamListener();
    AbstractClientStream stream =
        new BaseAbstractClientStream(allocator, statsTraceCtx, transportTracer);
    stream.start(listener);

    TransportState state = stream.transportState();

    thrown.expect(NullPointerException.class);
    state.inboundDataReceived(null);
  }

  @Test
  public void inboundHeadersReceived_notifiesListener() {
    AbstractClientStream stream =
        new BaseAbstractClientStream(allocator, statsTraceCtx, transportTracer);
    stream.start(mockListener);
    Metadata headers = new Metadata();

    stream.transportState().inboundHeadersReceived(headers);
    verify(mockListener).headersRead(headers);
  }

  @Test
  public void inboundHeadersReceived_failsIfStatusReported() {
    AbstractClientStream stream =
        new BaseAbstractClientStream(allocator, statsTraceCtx, transportTracer);
    stream.start(mockListener);
    stream.transportState().transportReportStatus(Status.CANCELLED, false, new Metadata());

    TransportState state = stream.transportState();

    thrown.expect(IllegalStateException.class);
    state.inboundHeadersReceived(new Metadata());
  }

  @Test
  public void inboundHeadersReceived_acceptsGzipContentEncoding() {
    AbstractClientStream stream =
        new BaseAbstractClientStream(allocator, statsTraceCtx, transportTracer);
    stream.start(mockListener);
    Metadata headers = new Metadata();
    headers.put(GrpcUtil.CONTENT_ENCODING_KEY, "gzip");

    stream.setFullStreamDecompression(true);
    stream.transportState().inboundHeadersReceived(headers);

    verify(mockListener).headersRead(headers);
  }

  @Test
  // https://tools.ietf.org/html/rfc7231#section-3.1.2.1
  public void inboundHeadersReceived_contentEncodingIsCaseInsensitive() {
    AbstractClientStream stream =
        new BaseAbstractClientStream(allocator, statsTraceCtx, transportTracer);
    stream.start(mockListener);
    Metadata headers = new Metadata();
    headers.put(GrpcUtil.CONTENT_ENCODING_KEY, "gZIp");

    stream.setFullStreamDecompression(true);
    stream.transportState().inboundHeadersReceived(headers);

    verify(mockListener).headersRead(headers);
  }

  @Test
  public void inboundHeadersReceived_failsOnUnrecognizedContentEncoding() {
    AbstractClientStream stream =
        new BaseAbstractClientStream(allocator, statsTraceCtx, transportTracer);
    stream.start(mockListener);
    Metadata headers = new Metadata();
    headers.put(GrpcUtil.CONTENT_ENCODING_KEY, "not-a-real-compression-method");

    stream.setFullStreamDecompression(true);
    stream.transportState().inboundHeadersReceived(headers);

    verifyNoMoreInteractions(mockListener);
    Throwable t = ((BaseTransportState) stream.transportState()).getDeframeFailedCause();
    assertEquals(Status.INTERNAL.getCode(), Status.fromThrowable(t).getCode());
    assertTrue(
        "unexpected deframe failed description",
        Status.fromThrowable(t)
            .getDescription()
            .startsWith("Can't find full stream decompressor for"));
  }

  @Test
  public void inboundHeadersReceived_disallowsContentAndMessageEncoding() {
    AbstractClientStream stream =
        new BaseAbstractClientStream(allocator, statsTraceCtx, transportTracer);
    stream.start(mockListener);
    Metadata headers = new Metadata();
    headers.put(GrpcUtil.CONTENT_ENCODING_KEY, "gzip");
    headers.put(GrpcUtil.MESSAGE_ENCODING_KEY, new Codec.Gzip().getMessageEncoding());

    stream.setFullStreamDecompression(true);
    stream.transportState().inboundHeadersReceived(headers);

    verifyNoMoreInteractions(mockListener);
    Throwable t = ((BaseTransportState) stream.transportState()).getDeframeFailedCause();
    assertEquals(Status.INTERNAL.getCode(), Status.fromThrowable(t).getCode());
    assertTrue(
        "unexpected deframe failed description",
        Status.fromThrowable(t)
            .getDescription()
            .equals("Full stream and gRPC message encoding cannot both be set"));
  }

  @Test
  public void inboundHeadersReceived_acceptsGzipMessageEncoding() {
    AbstractClientStream stream =
        new BaseAbstractClientStream(allocator, statsTraceCtx, transportTracer);
    stream.start(mockListener);
    Metadata headers = new Metadata();
    headers.put(GrpcUtil.MESSAGE_ENCODING_KEY, new Codec.Gzip().getMessageEncoding());

    stream.transportState().inboundHeadersReceived(headers);
    verify(mockListener).headersRead(headers);
  }

  @Test
  public void inboundHeadersReceived_acceptsIdentityMessageEncoding() {
    AbstractClientStream stream =
        new BaseAbstractClientStream(allocator, statsTraceCtx, transportTracer);
    stream.start(mockListener);
    Metadata headers = new Metadata();
    headers.put(GrpcUtil.MESSAGE_ENCODING_KEY, Codec.Identity.NONE.getMessageEncoding());

    stream.transportState().inboundHeadersReceived(headers);
    verify(mockListener).headersRead(headers);
  }

  @Test
  public void inboundHeadersReceived_failsOnUnrecognizedMessageEncoding() {
    AbstractClientStream stream =
        new BaseAbstractClientStream(allocator, statsTraceCtx, transportTracer);
    stream.start(mockListener);
    Metadata headers = new Metadata();
    headers.put(GrpcUtil.MESSAGE_ENCODING_KEY, "not-a-real-compression-method");

    stream.transportState().inboundHeadersReceived(headers);

    verifyNoMoreInteractions(mockListener);
    Throwable t = ((BaseTransportState) stream.transportState()).getDeframeFailedCause();
    assertEquals(Status.INTERNAL.getCode(), Status.fromThrowable(t).getCode());
    assertTrue(
        "unexpected deframe failed description",
        Status.fromThrowable(t).getDescription().startsWith("Can't find decompressor for"));
  }

  @Test
  public void rstStreamClosesStream() {
    AbstractClientStream stream =
        new BaseAbstractClientStream(allocator, statsTraceCtx, transportTracer);
    stream.start(mockListener);
    // The application will call request when waiting for a message, which will in turn call this
    // on the transport thread.
    stream.transportState().requestMessagesFromDeframer(1);
    // Send first byte of 2 byte message
    stream.transportState().deframe(ReadableBuffers.wrap(new byte[] {0, 0, 0, 0, 2, 1}));
    Status status = Status.INTERNAL.withDescription("rst___stream");
    // Simulate getting a reset
    stream.transportState().transportReportStatus(status, false /*stop delivery*/, new Metadata());

    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(mockListener)
        .closed(statusCaptor.capture(), any(RpcProgress.class), any(Metadata.class));
    assertSame(Status.Code.INTERNAL, statusCaptor.getValue().getCode());
    assertEquals("rst___stream", statusCaptor.getValue().getDescription());
  }

  @Test
  public void trailerOkWithTruncatedMessage() {
    AbstractClientStream stream =
        new BaseAbstractClientStream(allocator, statsTraceCtx, transportTracer);
    stream.start(mockListener);

    stream.transportState().requestMessagesFromDeframer(1);
    stream.transportState().deframe(ReadableBuffers.wrap(new byte[] {0, 0, 0, 0, 2, 1}));
    stream.transportState().inboundTrailersReceived(new Metadata(), Status.OK);

    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(mockListener)
        .closed(statusCaptor.capture(), any(RpcProgress.class), any(Metadata.class));
    assertSame(Status.Code.INTERNAL, statusCaptor.getValue().getCode());
    assertEquals("Encountered end-of-stream mid-frame", statusCaptor.getValue().getDescription());
  }

  @Test
  public void trailerNotOkWithTruncatedMessage() {
    AbstractClientStream stream =
        new BaseAbstractClientStream(allocator, statsTraceCtx, transportTracer);
    stream.start(mockListener);

    stream.transportState().requestMessagesFromDeframer(1);
    stream.transportState().deframe(ReadableBuffers.wrap(new byte[] {0, 0, 0, 0, 2, 1}));
    stream.transportState().inboundTrailersReceived(
        new Metadata(), Status.DATA_LOSS.withDescription("data___loss"));

    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(mockListener)
        .closed(statusCaptor.capture(), any(RpcProgress.class), any(Metadata.class));
    assertSame(Status.Code.DATA_LOSS, statusCaptor.getValue().getCode());
    assertEquals("data___loss", statusCaptor.getValue().getDescription());
  }
  
  @Test
  public void getRequest() {
    AbstractClientStream.Sink sink = mock(AbstractClientStream.Sink.class);
    final TestClientStreamTracer tracer = new TestClientStreamTracer();
    StatsTraceContext statsTraceCtx = new StatsTraceContext(new StreamTracer[]{tracer});
    AbstractClientStream stream = new BaseAbstractClientStream(
        allocator,
        new BaseTransportState(statsTraceCtx, transportTracer),
        sink,
        statsTraceCtx,
        transportTracer,
        true);
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
        .writeFrame(
            any(WritableBuffer.class), any(Boolean.class), any(Boolean.class), any(Integer.class));
    assertThat(tracer.nextOutboundEvent()).isEqualTo("outboundMessage(0)");
    assertThat(tracer.nextOutboundEvent()).matches("outboundMessageSent\\(0, [0-9]+, [0-9]+\\)");
    assertNull(tracer.nextOutboundEvent());
    assertNull(tracer.nextInboundEvent());
    assertEquals(1, tracer.getOutboundWireSize());
    assertEquals(1, tracer.getOutboundUncompressedSize());
  }

  @Test
  public void writeMessage_closesStream() throws IOException {
    AbstractClientStream.Sink sink = mock(AbstractClientStream.Sink.class);
    final TestClientStreamTracer tracer = new TestClientStreamTracer();
    StatsTraceContext statsTraceCtx = new StatsTraceContext(new StreamTracer[] {tracer});
    AbstractClientStream stream = new BaseAbstractClientStream(
        allocator,
        new BaseTransportState(statsTraceCtx, transportTracer),
        sink,
        statsTraceCtx,
        transportTracer,
        true);
    stream.start(mockListener);
    InputStream input = mock(InputStream.class, delegatesTo(new ByteArrayInputStream(new byte[1])));
    stream.writeMessage(input);
    verify(input).close();
  }

  @Test
  public void deadlineTimeoutPopulatedToHeaders() {
    AbstractClientStream.Sink sink = mock(AbstractClientStream.Sink.class);
    ClientStream stream = new BaseAbstractClientStream(
        allocator, new BaseTransportState(statsTraceCtx, transportTracer), sink, statsTraceCtx,
        transportTracer);

    stream.setDeadline(Deadline.after(1, TimeUnit.SECONDS));
    stream.start(mockListener);

    ArgumentCaptor<Metadata> headersCaptor = ArgumentCaptor.forClass(Metadata.class);
    verify(sink).writeHeaders(headersCaptor.capture(), any(byte[].class));

    Metadata headers = headersCaptor.getValue();
    assertTrue(headers.containsKey(GrpcUtil.TIMEOUT_KEY));
    assertThat(headers.get(GrpcUtil.TIMEOUT_KEY).longValue())
        .isLessThan(TimeUnit.SECONDS.toNanos(1));
    assertThat(headers.get(GrpcUtil.TIMEOUT_KEY).longValue())
        .isGreaterThan(TimeUnit.MILLISECONDS.toNanos(600));
  }

  /**
   * No-op base class for testing.
   */
  private static class BaseAbstractClientStream extends AbstractClientStream {
    private final TransportState state;
    private final Sink sink;

    public BaseAbstractClientStream(
        WritableBufferAllocator allocator,
        StatsTraceContext statsTraceCtx,
        TransportTracer transportTracer) {
      this(
          allocator,
          new BaseTransportState(statsTraceCtx, transportTracer),
          new BaseSink(),
          statsTraceCtx,
          transportTracer);
    }

    public BaseAbstractClientStream(
        WritableBufferAllocator allocator,
        TransportState state,
        Sink sink,
        StatsTraceContext statsTraceCtx,
        TransportTracer transportTracer) {
      this(allocator, state, sink, statsTraceCtx, transportTracer, false);
    }

    public BaseAbstractClientStream(
        WritableBufferAllocator allocator,
        TransportState state,
        Sink sink,
        StatsTraceContext statsTraceCtx,
        TransportTracer transportTracer,
        boolean useGet) {
      super(allocator, statsTraceCtx, transportTracer, new Metadata(), useGet);
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
    public void writeFrame(
        WritableBuffer frame, boolean endOfStream, boolean flush, int numMessages) {}

    @Override
    public void cancel(Status reason) {}
  }

  private static class BaseTransportState extends AbstractClientStream.TransportState {
    private Throwable deframeFailedCause;

    private Throwable getDeframeFailedCause() {
      return deframeFailedCause;
    }

    public BaseTransportState(StatsTraceContext statsTraceCtx, TransportTracer transportTracer) {
      super(DEFAULT_MAX_MESSAGE_SIZE, statsTraceCtx, transportTracer);
    }

    @Override
    public void deframeFailed(Throwable cause) {
      assertNull("deframeFailed already called", deframeFailedCause);
      deframeFailedCause = cause;
    }

    @Override
    public void bytesRead(int processedBytes) {}

    @Override
    public void runOnTransportThread(Runnable r) {
      r.run();
    }
  }
}
