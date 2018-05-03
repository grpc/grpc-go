/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.cronet;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.isA;
import static org.mockito.Mockito.atLeast;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.google.common.io.BaseEncoding;
import io.grpc.CallOptions;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.cronet.CronetChannelBuilder.StreamBuilderFactory;
import io.grpc.internal.ClientStreamListener;
import io.grpc.internal.ClientStreamListener.RpcProgress;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.StatsTraceContext;
import io.grpc.internal.StreamListener.MessageProducer;
import io.grpc.internal.TransportTracer;
import io.grpc.internal.WritableBuffer;
import io.grpc.testing.TestMethodDescriptors;
import java.io.ByteArrayInputStream;
import java.nio.ByteBuffer;
import java.nio.charset.Charset;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.Executor;
import org.chromium.net.BidirectionalStream;
import org.chromium.net.CronetException;
import org.chromium.net.ExperimentalBidirectionalStream;
import org.chromium.net.UrlResponseInfo;
import org.chromium.net.impl.UrlResponseInfoImpl;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import org.robolectric.RobolectricTestRunner;

@RunWith(RobolectricTestRunner.class)
public final class CronetClientStreamTest {

  @Mock private CronetClientTransport transport;
  private Metadata metadata = new Metadata();
  @Mock private StreamBuilderFactory factory;
  @Mock private ExperimentalBidirectionalStream cronetStream;
  @Mock private Executor executor;
  @Mock private ClientStreamListener clientListener;
  @Mock private ExperimentalBidirectionalStream.Builder builder;
  private final Object lock = new Object();
  private final TransportTracer transportTracer = TransportTracer.getDefaultFactory().create();
  CronetClientStream clientStream;

  private MethodDescriptor.Marshaller<Void> marshaller = TestMethodDescriptors.voidMarshaller();

  private MethodDescriptor<?, ?> method = TestMethodDescriptors.voidMethod();

  private static class SetStreamFactoryRunnable implements Runnable {
    private final StreamBuilderFactory factory;
    private CronetClientStream stream;

    SetStreamFactoryRunnable(StreamBuilderFactory factory) {
      this.factory = factory;
    }

    void setStream(CronetClientStream stream) {
      this.stream = stream;
    }

    @Override
    public void run() {
      assertTrue(stream != null);
      stream.transportState().start(factory);
    }
  }

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);

    SetStreamFactoryRunnable callback = new SetStreamFactoryRunnable(factory);
    clientStream =
        new CronetClientStream(
            "https://www.google.com:443",
            "cronet",
            executor,
            metadata,
            transport,
            callback,
            lock,
            100,
            false /* alwaysUsePut */,
            method,
            StatsTraceContext.NOOP,
            CallOptions.DEFAULT,
            transportTracer);
    callback.setStream(clientStream);
    when(factory.newBidirectionalStreamBuilder(
            any(String.class), any(BidirectionalStream.Callback.class), any(Executor.class)))
        .thenReturn(builder);
    when(builder.build()).thenReturn(cronetStream);
    clientStream.start(clientListener);
  }

  @Test
  public void startStream() {
    verify(factory)
        .newBidirectionalStreamBuilder(
            eq("https://www.google.com:443"),
            isA(BidirectionalStream.Callback.class),
            eq(executor));
    verify(builder).build();
    // At least content type and trailer headers are set.
    verify(builder, atLeast(2)).addHeader(isA(String.class), isA(String.class));
    // addRequestAnnotation should only be called when we explicitly add the CRONET_ANNOTATION_KEY
    // to CallOptions.
    verify(builder, times(0)).addRequestAnnotation(isA(Object.class));
    verify(builder, times(0)).setHttpMethod(any(String.class));
    verify(cronetStream).start();
  }

  @Test
  public void write() {
    ArgumentCaptor<BidirectionalStream.Callback> callbackCaptor =
        ArgumentCaptor.forClass(BidirectionalStream.Callback.class);
    verify(factory)
        .newBidirectionalStreamBuilder(
            isA(String.class), callbackCaptor.capture(), isA(Executor.class));
    BidirectionalStream.Callback callback = callbackCaptor.getValue();

    // Create 5 frames to send.
    CronetWritableBufferAllocator allocator = new CronetWritableBufferAllocator();
    String[] requests = new String[5];
    WritableBuffer[] buffers = new WritableBuffer[5];
    for (int i = 0; i < 5; ++i) {
      requests[i] = new String("request" + String.valueOf(i));
      buffers[i] = allocator.allocate(requests[i].length());
      buffers[i].write(requests[i].getBytes(Charset.forName("UTF-8")), 0, requests[i].length());
      // The 3rd and 5th writeFrame calls have flush=true.
      clientStream.abstractClientStreamSink().writeFrame(buffers[i], false, i == 2 || i == 4, 1);
    }
    // BidirectionalStream.write is not called because stream is not ready yet.
    verify(cronetStream, times(0)).write(isA(ByteBuffer.class), isA(Boolean.class));

    // Stream is ready.
    callback.onStreamReady(cronetStream);
    // 5 writes are called.
    verify(cronetStream, times(5)).write(isA(ByteBuffer.class), eq(false));
    ByteBuffer fakeBuffer = ByteBuffer.allocateDirect(8);
    fakeBuffer.position(8);
    verify(cronetStream, times(2)).flush();

    // 5 onWriteCompleted callbacks for previous writes.
    callback.onWriteCompleted(cronetStream, null, fakeBuffer, false);
    callback.onWriteCompleted(cronetStream, null, fakeBuffer, false);
    callback.onWriteCompleted(cronetStream, null, fakeBuffer, false);
    callback.onWriteCompleted(cronetStream, null, fakeBuffer, false);
    callback.onWriteCompleted(cronetStream, null, fakeBuffer, false);

    // All pending data has been sent. onWriteCompleted callback will not trigger any additional
    // write call.
    verify(cronetStream, times(5)).write(isA(ByteBuffer.class), eq(false));

    // Send end of stream. write will be immediately called since stream is ready.
    clientStream.abstractClientStreamSink().writeFrame(null, true, true, 1);
    verify(cronetStream, times(1)).write(isA(ByteBuffer.class), eq(true));
    verify(cronetStream, times(3)).flush();
  }

  private static List<Map.Entry<String, String>> responseHeader(String status) {
    Map<String, String> headers = new HashMap<String, String>();
    headers.put(":status", status);
    headers.put("content-type", "application/grpc");
    headers.put("test-key", "test-value");
    List<Map.Entry<String, String>> headerList = new ArrayList<Map.Entry<String, String>>(3);
    for (Map.Entry<String, String> entry : headers.entrySet()) {
      headerList.add(entry);
    }
    return headerList;
  }

  private static List<Map.Entry<String, String>> trailers(int status) {
    Map<String, String> trailers = new HashMap<String, String>();
    trailers.put("grpc-status", String.valueOf(status));
    trailers.put("content-type", "application/grpc");
    trailers.put("test-trailer-key", "test-trailer-value");
    List<Map.Entry<String, String>> trailerList = new ArrayList<Map.Entry<String, String>>(3);
    for (Map.Entry<String, String> entry : trailers.entrySet()) {
      trailerList.add(entry);
    }
    return trailerList;
  }

  private static ByteBuffer createMessageFrame(byte[] bytes) {
    ByteBuffer buffer = ByteBuffer.allocate(1 + 4 + bytes.length);
    buffer.put((byte) 0 /* UNCOMPRESSED */);
    buffer.putInt(bytes.length);
    buffer.put(bytes);
    return buffer;
  }

  @Test
  public void read() {
    ArgumentCaptor<BidirectionalStream.Callback> callbackCaptor =
        ArgumentCaptor.forClass(BidirectionalStream.Callback.class);
    verify(factory)
        .newBidirectionalStreamBuilder(
            isA(String.class), callbackCaptor.capture(), isA(Executor.class));
    BidirectionalStream.Callback callback = callbackCaptor.getValue();

    // Read is not called until we receive the response header.
    verify(cronetStream, times(0)).read(isA(ByteBuffer.class));
    UrlResponseInfo info =
        new UrlResponseInfoImpl(
            new ArrayList<String>(), 200, "", responseHeader("200"), false, "", "");
    callback.onResponseHeadersReceived(cronetStream, info);
    verify(cronetStream, times(1)).read(isA(ByteBuffer.class));
    ArgumentCaptor<Metadata> metadataCaptor = ArgumentCaptor.forClass(Metadata.class);
    verify(clientListener).headersRead(metadataCaptor.capture());
    // Verify recevied headers.
    Metadata metadata = metadataCaptor.getValue();
    assertEquals(
        "application/grpc",
        metadata.get(Metadata.Key.of("content-type", Metadata.ASCII_STRING_MARSHALLER)));
    assertEquals(
        "test-value", metadata.get(Metadata.Key.of("test-key", Metadata.ASCII_STRING_MARSHALLER)));

    callback.onReadCompleted(
        cronetStream,
        info,
        (ByteBuffer) createMessageFrame(new String("response1").getBytes(Charset.forName("UTF-8"))),
        false);
    // Haven't request any message, so no callback is called here.
    verify(clientListener, times(0)).messagesAvailable(isA(MessageProducer.class));
    verify(cronetStream, times(1)).read(isA(ByteBuffer.class));
    // Request one message
    clientStream.request(1);
    verify(clientListener, times(1)).messagesAvailable(isA(MessageProducer.class));
    verify(cronetStream, times(2)).read(isA(ByteBuffer.class));

    // BidirectionalStream.read will not be called again after receiving endOfStream(empty buffer).
    clientStream.request(1);
    callback.onReadCompleted(cronetStream, info, ByteBuffer.allocate(0), true);
    verify(clientListener, times(1)).messagesAvailable(isA(MessageProducer.class));
    verify(cronetStream, times(2)).read(isA(ByteBuffer.class));
  }

  @Test
  public void streamSucceeded() {
    ArgumentCaptor<BidirectionalStream.Callback> callbackCaptor =
        ArgumentCaptor.forClass(BidirectionalStream.Callback.class);
    verify(factory)
        .newBidirectionalStreamBuilder(
            isA(String.class), callbackCaptor.capture(), isA(Executor.class));
    BidirectionalStream.Callback callback = callbackCaptor.getValue();

    callback.onStreamReady(cronetStream);
    verify(cronetStream, times(0)).write(isA(ByteBuffer.class), isA(Boolean.class));
    // Send the first data frame.
    CronetWritableBufferAllocator allocator = new CronetWritableBufferAllocator();
    String request = new String("request");
    WritableBuffer writableBuffer = allocator.allocate(request.length());
    writableBuffer.write(request.getBytes(Charset.forName("UTF-8")), 0, request.length());
    clientStream.abstractClientStreamSink().writeFrame(writableBuffer, false, true, 1);
    ArgumentCaptor<ByteBuffer> bufferCaptor = ArgumentCaptor.forClass(ByteBuffer.class);
    verify(cronetStream, times(1)).write(bufferCaptor.capture(), isA(Boolean.class));
    ByteBuffer buffer = bufferCaptor.getValue();
    buffer.position(request.length());
    verify(cronetStream, times(1)).flush();

    // Receive response header
    clientStream.request(2);
    UrlResponseInfo info =
        new UrlResponseInfoImpl(
            new ArrayList<String>(), 200, "", responseHeader("200"), false, "", "");
    callback.onResponseHeadersReceived(cronetStream, info);
    verify(cronetStream, times(1)).read(isA(ByteBuffer.class));
    // Receive one message
    callback.onReadCompleted(
        cronetStream,
        info,
        (ByteBuffer) createMessageFrame(new String("response").getBytes(Charset.forName("UTF-8"))),
        false);
    verify(clientListener, times(1)).messagesAvailable(isA(MessageProducer.class));
    verify(cronetStream, times(2)).read(isA(ByteBuffer.class));

    // Send endOfStream
    callback.onWriteCompleted(cronetStream, null, buffer, false);
    clientStream.abstractClientStreamSink().writeFrame(null, true, true, 1);
    verify(cronetStream, times(2)).write(isA(ByteBuffer.class), isA(Boolean.class));
    verify(cronetStream, times(2)).flush();

    // Receive trailer
    ((CronetClientStream.BidirectionalStreamCallback) callback).processTrailers(trailers(0));
    callback.onSucceeded(cronetStream, info);

    // Verify trailer
    ArgumentCaptor<Metadata> trailerCaptor = ArgumentCaptor.forClass(Metadata.class);
    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(clientListener)
        .closed(statusCaptor.capture(), isA(RpcProgress.class), trailerCaptor.capture());
    // Verify recevied headers.
    Metadata trailers = trailerCaptor.getValue();
    Status status = statusCaptor.getValue();
    assertEquals(
        "test-trailer-value",
        trailers.get(Metadata.Key.of("test-trailer-key", Metadata.ASCII_STRING_MARSHALLER)));
    assertEquals(
        "application/grpc",
        trailers.get(Metadata.Key.of("content-type", Metadata.ASCII_STRING_MARSHALLER)));
    assertTrue(status.isOk());
  }

  @Test
  public void streamSucceededWithGrpcError() {
    ArgumentCaptor<BidirectionalStream.Callback> callbackCaptor =
        ArgumentCaptor.forClass(BidirectionalStream.Callback.class);
    verify(factory)
        .newBidirectionalStreamBuilder(
            isA(String.class), callbackCaptor.capture(), isA(Executor.class));
    BidirectionalStream.Callback callback = callbackCaptor.getValue();

    callback.onStreamReady(cronetStream);
    verify(cronetStream, times(0)).write(isA(ByteBuffer.class), isA(Boolean.class));
    clientStream.abstractClientStreamSink().writeFrame(null, true, true, 1);
    verify(cronetStream, times(1)).write(isA(ByteBuffer.class), isA(Boolean.class));
    verify(cronetStream, times(1)).flush();

    // Receive response header
    clientStream.request(2);
    UrlResponseInfo info =
        new UrlResponseInfoImpl(
            new ArrayList<String>(), 200, "", responseHeader("200"), false, "", "");
    callback.onResponseHeadersReceived(cronetStream, info);
    verify(cronetStream, times(1)).read(isA(ByteBuffer.class));

    // Receive trailer
    callback.onReadCompleted(cronetStream, null, ByteBuffer.allocate(0), true);
    ((CronetClientStream.BidirectionalStreamCallback) callback)
        .processTrailers(trailers(Status.PERMISSION_DENIED.getCode().value()));
    callback.onSucceeded(cronetStream, info);

    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(clientListener)
        .closed(statusCaptor.capture(), isA(RpcProgress.class), isA(Metadata.class));
    // Verify error status.
    Status status = statusCaptor.getValue();
    assertFalse(status.isOk());
    assertEquals(Status.PERMISSION_DENIED.getCode(), status.getCode());
  }

  @Test
  public void streamFailed() {
    ArgumentCaptor<BidirectionalStream.Callback> callbackCaptor =
        ArgumentCaptor.forClass(BidirectionalStream.Callback.class);
    verify(factory)
        .newBidirectionalStreamBuilder(
            isA(String.class), callbackCaptor.capture(), isA(Executor.class));
    BidirectionalStream.Callback callback = callbackCaptor.getValue();

    // Nothing happens and stream fails

    CronetException exception = mock(CronetException.class);
    callback.onFailed(cronetStream, null, exception);
    verify(transport).finishStream(eq(clientStream), isA(Status.class));
    // finishStream calls transportReportStatus.
    clientStream.transportState().transportReportStatus(Status.UNAVAILABLE, false, new Metadata());

    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(clientListener)
        .closed(statusCaptor.capture(), isA(RpcProgress.class), isA(Metadata.class));
    Status status = statusCaptor.getValue();
    assertEquals(Status.UNAVAILABLE.getCode(), status.getCode());
  }

  @Test
  public void streamFailedAfterResponseHeaderReceived() {
    ArgumentCaptor<BidirectionalStream.Callback> callbackCaptor =
        ArgumentCaptor.forClass(BidirectionalStream.Callback.class);
    verify(factory)
        .newBidirectionalStreamBuilder(
            isA(String.class), callbackCaptor.capture(), isA(Executor.class));
    BidirectionalStream.Callback callback = callbackCaptor.getValue();

    // Receive response header
    UrlResponseInfo info =
        new UrlResponseInfoImpl(
            new ArrayList<String>(), 200, "", responseHeader("200"), false, "", "");
    callback.onResponseHeadersReceived(cronetStream, info);

    CronetException exception = mock(CronetException.class);
    callback.onFailed(cronetStream, info, exception);
    verify(transport).finishStream(eq(clientStream), isA(Status.class));
    // finishStream calls transportReportStatus.
    clientStream.transportState().transportReportStatus(Status.UNAVAILABLE, false, new Metadata());

    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(clientListener)
        .closed(statusCaptor.capture(), isA(RpcProgress.class), isA(Metadata.class));
    Status status = statusCaptor.getValue();
    assertEquals(Status.UNAVAILABLE.getCode(), status.getCode());
  }

  @Test
  public void streamFailedAfterTrailerReceived() {
    ArgumentCaptor<BidirectionalStream.Callback> callbackCaptor =
        ArgumentCaptor.forClass(BidirectionalStream.Callback.class);
    verify(factory)
        .newBidirectionalStreamBuilder(
            isA(String.class), callbackCaptor.capture(), isA(Executor.class));
    BidirectionalStream.Callback callback = callbackCaptor.getValue();

    // Receive response header
    UrlResponseInfo info =
        new UrlResponseInfoImpl(
            new ArrayList<String>(), 200, "", responseHeader("200"), false, "", "");
    callback.onResponseHeadersReceived(cronetStream, info);

    // Report trailer but not endOfStream.
    ((CronetClientStream.BidirectionalStreamCallback) callback).processTrailers(trailers(0));

    CronetException exception = mock(CronetException.class);
    callback.onFailed(cronetStream, info, exception);
    verify(transport).finishStream(eq(clientStream), isA(Status.class));
    // finishStream calls transportReportStatus.
    clientStream.transportState().transportReportStatus(Status.UNAVAILABLE, false, new Metadata());

    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(clientListener)
        .closed(statusCaptor.capture(), isA(RpcProgress.class), isA(Metadata.class));
    Status status = statusCaptor.getValue();
    // Stream has already finished so OK status should be reported.
    assertEquals(Status.UNAVAILABLE.getCode(), status.getCode());
  }

  @Test
  public void streamFailedAfterTrailerAndEndOfStreamReceived() {
    ArgumentCaptor<BidirectionalStream.Callback> callbackCaptor =
        ArgumentCaptor.forClass(BidirectionalStream.Callback.class);
    verify(factory)
        .newBidirectionalStreamBuilder(
            isA(String.class), callbackCaptor.capture(), isA(Executor.class));
    BidirectionalStream.Callback callback = callbackCaptor.getValue();

    // Receive response header
    UrlResponseInfo info =
        new UrlResponseInfoImpl(
            new ArrayList<String>(), 200, "", responseHeader("200"), false, "", "");
    callback.onResponseHeadersReceived(cronetStream, info);

    // Report trailer and endOfStream
    callback.onReadCompleted(cronetStream, null, ByteBuffer.allocate(0), true);
    ((CronetClientStream.BidirectionalStreamCallback) callback).processTrailers(trailers(0));

    CronetException exception = mock(CronetException.class);
    callback.onFailed(cronetStream, info, exception);
    verify(transport).finishStream(eq(clientStream), isA(Status.class));
    // finishStream calls transportReportStatus.
    clientStream.transportState().transportReportStatus(Status.UNAVAILABLE, false, new Metadata());

    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(clientListener)
        .closed(statusCaptor.capture(), isA(RpcProgress.class), isA(Metadata.class));
    Status status = statusCaptor.getValue();
    // Stream has already finished so OK status should be reported.
    assertEquals(Status.OK.getCode(), status.getCode());
  }

  @Test
  public void cancelStream() {
    ArgumentCaptor<BidirectionalStream.Callback> callbackCaptor =
        ArgumentCaptor.forClass(BidirectionalStream.Callback.class);
    verify(factory)
        .newBidirectionalStreamBuilder(
            isA(String.class), callbackCaptor.capture(), isA(Executor.class));
    BidirectionalStream.Callback callback = callbackCaptor.getValue();

    // Cancel the stream
    clientStream.cancel(Status.DEADLINE_EXCEEDED);
    verify(transport, times(0)).finishStream(eq(clientStream), isA(Status.class));

    callback.onCanceled(cronetStream, null);
    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(transport, times(1)).finishStream(eq(clientStream), statusCaptor.capture());
    Status status = statusCaptor.getValue();
    assertEquals(Status.DEADLINE_EXCEEDED.getCode(), status.getCode());
  }

  @Test
  public void reportTrailersWhenTrailersReceivedBeforeReadClosed() {
    ArgumentCaptor<BidirectionalStream.Callback> callbackCaptor =
        ArgumentCaptor.forClass(BidirectionalStream.Callback.class);
    verify(factory)
        .newBidirectionalStreamBuilder(
            isA(String.class), callbackCaptor.capture(), isA(Executor.class));
    BidirectionalStream.Callback callback = callbackCaptor.getValue();

    callback.onStreamReady(cronetStream);
    UrlResponseInfo info =
        new UrlResponseInfoImpl(
            new ArrayList<String>(), 200, "", responseHeader("200"), false, "", "");
    callback.onResponseHeadersReceived(cronetStream, info);
    // Receive trailer first
    ((CronetClientStream.BidirectionalStreamCallback) callback)
        .processTrailers(trailers(Status.UNAUTHENTICATED.getCode().value()));
    verify(clientListener, times(0))
        .closed(isA(Status.class), isA(RpcProgress.class), isA(Metadata.class));

    // Receive cronet's endOfStream
    callback.onReadCompleted(cronetStream, null, ByteBuffer.allocate(0), true);
    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(clientListener, times(1))
        .closed(statusCaptor.capture(), isA(RpcProgress.class), isA(Metadata.class));
    Status status = statusCaptor.getValue();
    assertEquals(Status.UNAUTHENTICATED.getCode(), status.getCode());
  }

  @Test
  public void reportTrailersWhenTrailersReceivedAfterReadClosed() {
    ArgumentCaptor<BidirectionalStream.Callback> callbackCaptor =
        ArgumentCaptor.forClass(BidirectionalStream.Callback.class);
    verify(factory)
        .newBidirectionalStreamBuilder(
            isA(String.class), callbackCaptor.capture(), isA(Executor.class));
    BidirectionalStream.Callback callback = callbackCaptor.getValue();

    callback.onStreamReady(cronetStream);
    UrlResponseInfo info =
        new UrlResponseInfoImpl(
            new ArrayList<String>(), 200, "", responseHeader("200"), false, "", "");
    callback.onResponseHeadersReceived(cronetStream, info);
    // Receive cronet's endOfStream
    callback.onReadCompleted(cronetStream, null, ByteBuffer.allocate(0), true);
    verify(clientListener, times(0))
        .closed(isA(Status.class), isA(RpcProgress.class), isA(Metadata.class));

    // Receive trailer
    ((CronetClientStream.BidirectionalStreamCallback) callback)
        .processTrailers(trailers(Status.UNAUTHENTICATED.getCode().value()));
    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(clientListener, times(1))
        .closed(statusCaptor.capture(), isA(RpcProgress.class), isA(Metadata.class));
    Status status = statusCaptor.getValue();
    assertEquals(Status.UNAUTHENTICATED.getCode(), status.getCode());
  }

  @Test
  public void addCronetRequestAnnotation_deprecated() {
    Object annotation = new Object();
    SetStreamFactoryRunnable callback = new SetStreamFactoryRunnable(factory);
    CronetClientStream stream =
        new CronetClientStream(
            "https://www.google.com:443",
            "cronet",
            executor,
            metadata,
            transport,
            callback,
            lock,
            100,
            false /* alwaysUsePut */,
            method,
            StatsTraceContext.NOOP,
            CallOptions.DEFAULT.withOption(CronetCallOptions.CRONET_ANNOTATION_KEY, annotation),
            transportTracer);
    callback.setStream(stream);
    when(factory.newBidirectionalStreamBuilder(
            any(String.class), any(BidirectionalStream.Callback.class), any(Executor.class)))
        .thenReturn(builder);
    stream.start(clientListener);

    // addRequestAnnotation should be called since we add the option CRONET_ANNOTATION_KEY above.
    verify(builder).addRequestAnnotation(annotation);
  }

  @Test
  public void withAnnotation() {
    Object annotation1 = new Object();
    Object annotation2 = new Object();
    CallOptions callOptions = CronetCallOptions.withAnnotation(CallOptions.DEFAULT, annotation1);
    callOptions = CronetCallOptions.withAnnotation(callOptions, annotation2);

    SetStreamFactoryRunnable callback = new SetStreamFactoryRunnable(factory);
    CronetClientStream stream =
        new CronetClientStream(
            "https://www.google.com:443",
            "cronet",
            executor,
            metadata,
            transport,
            callback,
            lock,
            100,
            false /* alwaysUsePut */,
            method,
            StatsTraceContext.NOOP,
            callOptions,
            transportTracer);
    callback.setStream(stream);
    when(factory.newBidirectionalStreamBuilder(
            any(String.class), any(BidirectionalStream.Callback.class), any(Executor.class)))
        .thenReturn(builder);
    stream.start(clientListener);

    verify(builder).addRequestAnnotation(annotation1);
    verify(builder).addRequestAnnotation(annotation2);
  }

  @Test
  public void getUnaryRequest() {
    StreamBuilderFactory getFactory = mock(StreamBuilderFactory.class);
    MethodDescriptor<?, ?> getMethod =
        MethodDescriptor.<Void, Void>newBuilder()
            .setType(MethodDescriptor.MethodType.UNARY)
            .setFullMethodName("/service/method")
            .setIdempotent(true)
            .setSafe(true)
            .setRequestMarshaller(marshaller)
            .setResponseMarshaller(marshaller)
            .build();
    SetStreamFactoryRunnable callback = new SetStreamFactoryRunnable(getFactory);
    CronetClientStream stream =
        new CronetClientStream(
            "https://www.google.com/service/method",
            "cronet",
            executor,
            metadata,
            transport,
            callback,
            lock,
            100,
            false /* alwaysUsePut */,
            getMethod,
            StatsTraceContext.NOOP,
            CallOptions.DEFAULT,
            transportTracer);
    callback.setStream(stream);
    ExperimentalBidirectionalStream.Builder getBuilder =
        mock(ExperimentalBidirectionalStream.Builder.class);
    when(getFactory.newBidirectionalStreamBuilder(
            any(String.class), any(BidirectionalStream.Callback.class), any(Executor.class)))
        .thenReturn(getBuilder);
    when(getBuilder.build()).thenReturn(cronetStream);
    stream.start(clientListener);

    // We will not create BidirectionalStream until we have the full request.
    verify(getFactory, times(0))
        .newBidirectionalStreamBuilder(
            isA(String.class), isA(BidirectionalStream.Callback.class), isA(Executor.class));

    byte[] msg = "request".getBytes(Charset.forName("UTF-8"));
    stream.writeMessage(new ByteArrayInputStream(msg));
    // We still haven't built the stream or sent anything.
    verify(cronetStream, times(0)).write(isA(ByteBuffer.class), isA(Boolean.class));
    verify(getFactory, times(0))
        .newBidirectionalStreamBuilder(
            isA(String.class), isA(BidirectionalStream.Callback.class), isA(Executor.class));

    // halfClose will trigger sending.
    stream.halfClose();

    // Stream should be built with request payload in the header.
    ArgumentCaptor<String> urlCaptor = ArgumentCaptor.forClass(String.class);
    verify(getFactory)
        .newBidirectionalStreamBuilder(
            urlCaptor.capture(), isA(BidirectionalStream.Callback.class), isA(Executor.class));
    verify(getBuilder).setHttpMethod("GET");
    assertEquals(
        "https://www.google.com/service/method?" + BaseEncoding.base64().encode(msg),
        urlCaptor.getValue());
  }

  @Test
  public void idempotentMethod_usesHttpPut() {
    SetStreamFactoryRunnable callback = new SetStreamFactoryRunnable(factory);
    MethodDescriptor<?, ?> idempotentMethod = method.toBuilder().setIdempotent(true).build();
    CronetClientStream stream =
        new CronetClientStream(
            "https://www.google.com:443",
            "cronet",
            executor,
            metadata,
            transport,
            callback,
            lock,
            100,
            false /* alwaysUsePut */,
            idempotentMethod,
            StatsTraceContext.NOOP,
            CallOptions.DEFAULT,
            transportTracer);
    callback.setStream(stream);
    ExperimentalBidirectionalStream.Builder builder =
        mock(ExperimentalBidirectionalStream.Builder.class);
    when(factory.newBidirectionalStreamBuilder(
            any(String.class), any(BidirectionalStream.Callback.class), any(Executor.class)))
        .thenReturn(builder);
    when(builder.build()).thenReturn(cronetStream);
    stream.start(clientListener);

    verify(builder).setHttpMethod("PUT");
  }

  @Test
  public void alwaysUsePutOption_usesHttpPut() {
    SetStreamFactoryRunnable callback = new SetStreamFactoryRunnable(factory);
    CronetClientStream stream =
        new CronetClientStream(
            "https://www.google.com:443",
            "cronet",
            executor,
            metadata,
            transport,
            callback,
            lock,
            100,
            true /* alwaysUsePut */,
            method,
            StatsTraceContext.NOOP,
            CallOptions.DEFAULT,
            transportTracer);
    callback.setStream(stream);
    ExperimentalBidirectionalStream.Builder builder =
        mock(ExperimentalBidirectionalStream.Builder.class);
    when(factory.newBidirectionalStreamBuilder(
            any(String.class), any(BidirectionalStream.Callback.class), any(Executor.class)))
        .thenReturn(builder);
    when(builder.build()).thenReturn(cronetStream);
    stream.start(clientListener);

    verify(builder).setHttpMethod("PUT");
  }

  @Test
  public void reservedHeadersStripped() {
    String userAgent = "cronet";
    Metadata headers = new Metadata();
    Metadata.Key<String> userKey = Metadata.Key.of("user-key", Metadata.ASCII_STRING_MARSHALLER);
    headers.put(GrpcUtil.CONTENT_TYPE_KEY, "to-be-removed");
    headers.put(GrpcUtil.USER_AGENT_KEY, "to-be-removed");
    headers.put(GrpcUtil.TE_HEADER, "to-be-removed");
    headers.put(userKey, "user-value");

    SetStreamFactoryRunnable callback = new SetStreamFactoryRunnable(factory);
    CronetClientStream stream =
        new CronetClientStream(
            "https://www.google.com:443",
            userAgent,
            executor,
            headers,
            transport,
            callback,
            lock,
            100,
            false /* alwaysUsePut */,
            method,
            StatsTraceContext.NOOP,
            CallOptions.DEFAULT,
            transportTracer);
    callback.setStream(stream);
    ExperimentalBidirectionalStream.Builder builder =
        mock(ExperimentalBidirectionalStream.Builder.class);
    when(factory.newBidirectionalStreamBuilder(
            any(String.class), any(BidirectionalStream.Callback.class), any(Executor.class)))
        .thenReturn(builder);
    when(builder.build()).thenReturn(cronetStream);
    stream.start(clientListener);

    verify(builder, times(4)).addHeader(any(String.class), any(String.class));
    verify(builder).addHeader(GrpcUtil.USER_AGENT_KEY.name(), userAgent);
    verify(builder).addHeader(GrpcUtil.CONTENT_TYPE_KEY.name(), GrpcUtil.CONTENT_TYPE_GRPC);
    verify(builder).addHeader("te", GrpcUtil.TE_TRAILERS);
    verify(builder).addHeader(userKey.name(), "user-value");
  }
}
