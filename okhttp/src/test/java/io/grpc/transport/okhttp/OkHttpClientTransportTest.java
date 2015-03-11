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

package io.grpc.transport.okhttp;

import static com.google.common.base.Charsets.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.reset;
import static org.mockito.Mockito.timeout;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.google.common.collect.ImmutableList;

import com.squareup.okhttp.internal.spdy.ErrorCode;
import com.squareup.okhttp.internal.spdy.FrameReader;
import com.squareup.okhttp.internal.spdy.Header;
import com.squareup.okhttp.internal.spdy.HeadersMode;

import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.transport.ClientStreamListener;
import io.grpc.transport.ClientTransport;
import io.grpc.transport.okhttp.OkHttpClientTransport.ClientFrameHandler;

import okio.Buffer;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.io.BufferedReader;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;

/**
 * Tests for {@link OkHttpClientTransport}.
 */
@RunWith(JUnit4.class)
public class OkHttpClientTransportTest {
  private static final int TIME_OUT_MS = 500;
  private static final String NETWORK_ISSUE_MESSAGE = "network issue";
  // The gRPC header length, which includes 1 byte compression flag and 4 bytes message length.
  private static final int HEADER_LENGTH = 5;

  @Mock
  private AsyncFrameWriter frameWriter;
  @Mock
  MethodDescriptor<?, ?> method;
  @Mock
  private ClientTransport.Listener listener;
  private OkHttpClientTransport clientTransport;
  private MockFrameReader frameReader;
  private Map<Integer, OkHttpClientStream> streams;
  private ClientFrameHandler frameHandler;
  private ExecutorService executor;

  @Before
  public void setup() {
    MockitoAnnotations.initMocks(this);
    streams = new HashMap<Integer, OkHttpClientStream>();
    frameReader = new MockFrameReader();
    executor = Executors.newCachedThreadPool();
    clientTransport = new OkHttpClientTransport(executor, frameReader, frameWriter, 3);
    clientTransport.start(listener);
    frameHandler = clientTransport.getHandler();
    streams = clientTransport.getStreams();
    when(method.getName()).thenReturn("fakemethod");
    when(frameWriter.maxDataLength()).thenReturn(Integer.MAX_VALUE);
  }

  @After
  public void tearDown() {
    clientTransport.shutdown();
    assertTrue(frameReader.closed);
    verify(frameWriter).close();
    executor.shutdown();
  }

  /**
   * When nextFrame throws IOException, the transport should be aborted.
   */
  @Test
  public void nextFrameThrowIOException() throws Exception {
    MockStreamListener listener1 = new MockStreamListener();
    MockStreamListener listener2 = new MockStreamListener();
    clientTransport.newStream(method, new Metadata.Headers(), listener1).request(1);
    clientTransport.newStream(method, new Metadata.Headers(), listener2).request(1);
    assertEquals(2, streams.size());
    assertTrue(streams.containsKey(3));
    assertTrue(streams.containsKey(5));
    frameReader.throwIOExceptionForNextFrame();
    listener1.waitUntilStreamClosed();
    listener2.waitUntilStreamClosed();
    assertEquals(0, streams.size());
    assertEquals(Status.INTERNAL.getCode(), listener1.status.getCode());
    assertEquals(NETWORK_ISSUE_MESSAGE, listener2.status.getCause().getMessage());
    assertEquals(Status.INTERNAL.getCode(), listener1.status.getCode());
    assertEquals(NETWORK_ISSUE_MESSAGE, listener2.status.getCause().getMessage());
    verify(listener).transportShutdown();
    verify(listener, timeout(TIME_OUT_MS)).transportTerminated();
  }

  @Test
  public void readMessages() throws Exception {
    final int numMessages = 10;
    final String message = "Hello Client";
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata.Headers(), listener).request(numMessages);
    assertTrue(streams.containsKey(3));
    frameHandler.headers(false, false, 3, 0, grpcResponseHeaders(), HeadersMode.HTTP_20_HEADERS);
    assertNotNull(listener.headers);
    for (int i = 0; i < numMessages; i++) {
      Buffer buffer = createMessageFrame(message + i);
      frameHandler.data(false, 3, buffer, (int) buffer.size());
    }
    frameHandler.headers(true, true, 3, 0, grpcResponseTrailers(), HeadersMode.HTTP_20_HEADERS);
    listener.waitUntilStreamClosed();
    assertEquals(Status.OK, listener.status);
    assertNotNull(listener.trailers);
    assertEquals(numMessages, listener.messages.size());
    for (int i = 0; i < numMessages; i++) {
      assertEquals(message + i, listener.messages.get(i));
    }
  }

  @Test
  public void receivedHeadersForInvalidStreamShouldResetStream() throws Exception {
    // Empty headers block without correct content type or status
    frameHandler.headers(false, false, 3, 0, new ArrayList<Header>(),
        HeadersMode.HTTP_20_HEADERS);
    verify(frameWriter).rstStream(eq(3), eq(ErrorCode.INVALID_STREAM));
  }

  @Test
  public void receivedDataForInvalidStreamShouldResetStream() throws Exception {
    frameHandler.data(false, 3, createMessageFrame(new String(new char[1000])), 1000);
    verify(frameWriter).rstStream(eq(3), eq(ErrorCode.INVALID_STREAM));
  }

  @Test
  public void invalidInboundHeadersCancelStream() throws Exception {
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata.Headers(), listener).request(1);
    assertTrue(streams.containsKey(3));
    // Empty headers block without correct content type or status
    frameHandler.headers(false, false, 3, 0, new ArrayList<Header>(),
        HeadersMode.HTTP_20_HEADERS);
    // Now wait to receive 1000 bytes of data so we can have a better error message before
    // cancelling the streaam.
    frameHandler.data(false, 3, createMessageFrame(new String(new char[1000])), 1000);
    verify(frameWriter).rstStream(eq(3), eq(ErrorCode.CANCEL));
    assertNull(listener.headers);
    assertEquals(Status.INTERNAL.getCode(), listener.status.getCode());
    assertNotNull(listener.trailers);
  }

  @Test
  public void readStatus() throws Exception {
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method,new Metadata.Headers(), listener);
    assertTrue(streams.containsKey(3));
    frameHandler.headers(true, true, 3, 0, grpcResponseTrailers(), HeadersMode.HTTP_20_HEADERS);
    listener.waitUntilStreamClosed();
    assertEquals(Status.Code.OK, listener.status.getCode());
  }

  @Test
  public void receiveReset() throws Exception {
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method,new Metadata.Headers(), listener);
    assertTrue(streams.containsKey(3));
    frameHandler.rstStream(3, ErrorCode.PROTOCOL_ERROR);
    listener.waitUntilStreamClosed();
    assertEquals(OkHttpClientTransport.toGrpcStatus(ErrorCode.PROTOCOL_ERROR), listener.status);
  }

  @Test
  public void cancelStream() throws Exception {
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method,new Metadata.Headers(), listener);
    OkHttpClientStream stream = streams.get(3);
    assertNotNull(stream);
    stream.cancel();
    verify(frameWriter).rstStream(eq(3), eq(ErrorCode.CANCEL));
    listener.waitUntilStreamClosed();
    assertEquals(OkHttpClientTransport.toGrpcStatus(ErrorCode.CANCEL).getCode(),
        listener.status.getCode());
  }

  @Test
  public void writeMessage() throws Exception {
    final String message = "Hello Server";
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method,new Metadata.Headers(), listener);
    OkHttpClientStream stream = streams.get(3);
    InputStream input = new ByteArrayInputStream(message.getBytes(UTF_8));
    assertEquals(12, input.available());
    stream.writeMessage(input, input.available(), null);
    stream.flush();
    ArgumentCaptor<Buffer> captor = ArgumentCaptor.forClass(Buffer.class);
    verify(frameWriter).data(eq(false), eq(3), captor.capture(), eq(12 + HEADER_LENGTH));
    Buffer sentFrame = captor.getValue();
    assertEquals(createMessageFrame(message), sentFrame);
    stream.cancel();
  }

  @Test
  public void windowUpdate() throws Exception {
    MockStreamListener listener1 = new MockStreamListener();
    MockStreamListener listener2 = new MockStreamListener();
    clientTransport.newStream(method,new Metadata.Headers(), listener1).request(2);
    clientTransport.newStream(method,new Metadata.Headers(), listener2).request(2);
    assertEquals(2, streams.size());
    OkHttpClientStream stream1 = streams.get(3);
    OkHttpClientStream stream2 = streams.get(5);

    frameHandler.headers(false, false, 3, 0, grpcResponseHeaders(), HeadersMode.HTTP_20_HEADERS);
    frameHandler.headers(false, false, 5, 0, grpcResponseHeaders(), HeadersMode.HTTP_20_HEADERS);

    int messageLength = OkHttpClientTransport.DEFAULT_INITIAL_WINDOW_SIZE / 4;
    byte[] fakeMessage = new byte[messageLength];

    // Stream 1 receives a message
    Buffer buffer = createMessageFrame(fakeMessage);
    int messageFrameLength = (int) buffer.size();
    frameHandler.data(false, 3, buffer, messageFrameLength);

    // Stream 2 receives a message
    buffer = createMessageFrame(fakeMessage);
    frameHandler.data(false, 5, buffer, messageFrameLength);

    verify(frameWriter).windowUpdate(eq(0), eq((long) 2 * messageFrameLength));
    reset(frameWriter);

    // Stream 1 receives another message
    buffer = createMessageFrame(fakeMessage);
    frameHandler.data(false, 3, buffer, messageFrameLength);

    verify(frameWriter).windowUpdate(eq(3), eq((long) 2 * messageFrameLength));

    // Stream 2 receives another message
    buffer = createMessageFrame(fakeMessage);
    frameHandler.data(false, 5, buffer, messageFrameLength);

    verify(frameWriter).windowUpdate(eq(5), eq((long) 2 * messageFrameLength));
    verify(frameWriter).windowUpdate(eq(0), eq((long) 2 * messageFrameLength));

    stream1.cancel();
    verify(frameWriter).rstStream(eq(3), eq(ErrorCode.CANCEL));
    listener1.waitUntilStreamClosed();
    assertEquals(OkHttpClientTransport.toGrpcStatus(ErrorCode.CANCEL).getCode(),
        listener1.status.getCode());

    stream2.cancel();
    verify(frameWriter).rstStream(eq(5), eq(ErrorCode.CANCEL));
    listener2.waitUntilStreamClosed();
    assertEquals(OkHttpClientTransport.toGrpcStatus(ErrorCode.CANCEL).getCode(),
        listener2.status.getCode());
  }

  @Test
  public void windowUpdateWithInboundFlowControl() throws Exception {
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata.Headers(), listener).request(1);
    OkHttpClientStream stream = streams.get(3);

    int messageLength = OkHttpClientTransport.DEFAULT_INITIAL_WINDOW_SIZE / 2 + 1;
    byte[] fakeMessage = new byte[messageLength];

    frameHandler.headers(false, false, 3, 0, grpcResponseHeaders(), HeadersMode.HTTP_20_HEADERS);
    Buffer buffer = createMessageFrame(fakeMessage);
    long messageFrameLength = buffer.size();
    frameHandler.data(false, 3, buffer, (int) messageFrameLength);
    verify(frameWriter).windowUpdate(eq(0), eq(messageFrameLength));
    // We return the bytes for the stream window as we read the message.
    verify(frameWriter).windowUpdate(eq(3), eq(messageFrameLength));

    stream.cancel();
    verify(frameWriter).rstStream(eq(3), eq(ErrorCode.CANCEL));
    listener.waitUntilStreamClosed();
    assertEquals(OkHttpClientTransport.toGrpcStatus(ErrorCode.CANCEL).getCode(),
        listener.status.getCode());
  }

  @Test
  public void stopNormally() throws Exception {
    MockStreamListener listener1 = new MockStreamListener();
    MockStreamListener listener2 = new MockStreamListener();
    OkHttpClientStream stream1
        = clientTransport.newStream(method, new Metadata.Headers(), listener1);
    OkHttpClientStream stream2
        = clientTransport.newStream(method, new Metadata.Headers(), listener2);
    assertEquals(2, streams.size());
    clientTransport.shutdown();
    verify(frameWriter).goAway(eq(0), eq(ErrorCode.NO_ERROR), (byte[]) any());
    assertEquals(2, streams.size());
    verify(listener).transportShutdown();

    stream1.cancel();
    stream2.cancel();
    listener1.waitUntilStreamClosed();
    listener2.waitUntilStreamClosed();
    assertEquals(0, streams.size());
    assertEquals(Status.CANCELLED.getCode(), listener1.status.getCode());
    assertEquals(Status.CANCELLED.getCode(), listener2.status.getCode());
    verify(listener).transportTerminated();
  }

  @Test
  public void receiveGoAway() throws Exception {
    // start 2 streams.
    MockStreamListener listener1 = new MockStreamListener();
    MockStreamListener listener2 = new MockStreamListener();
    clientTransport.newStream(method,new Metadata.Headers(), listener1).request(1);
    clientTransport.newStream(method,new Metadata.Headers(), listener2).request(1);
    assertEquals(2, streams.size());

    // Receive goAway, max good id is 3.
    frameHandler.goAway(3, ErrorCode.CANCEL, null);

    // Transport should be in STOPPING state.
    verify(listener).transportShutdown();
    verify(listener, never()).transportTerminated();

    // Stream 2 should be closed.
    listener2.waitUntilStreamClosed();
    assertEquals(1, streams.size());
    assertEquals(Status.UNAVAILABLE.getCode(), listener2.status.getCode());

    // New stream should be failed.
    MockStreamListener listener3 = new MockStreamListener();
    try {
      clientTransport.newStream(method,new Metadata.Headers(), listener3);
      fail("new stream should no be accepted by a go-away transport.");
    } catch (IllegalStateException ex) {
      // expected.
    }

    // But stream 1 should be able to send.
    final String sentMessage = "Should I also go away?";
    OkHttpClientStream stream = streams.get(3);
    InputStream input =
        new ByteArrayInputStream(sentMessage.getBytes(UTF_8));
    assertEquals(22, input.available());
    stream.writeMessage(input, input.available(), null);
    stream.flush();
    ArgumentCaptor<Buffer> captor =
        ArgumentCaptor.forClass(Buffer.class);
    verify(frameWriter).data(eq(false), eq(3), captor.capture(), eq(22 + HEADER_LENGTH));
    Buffer sentFrame = captor.getValue();
    assertEquals(createMessageFrame(sentMessage), sentFrame);

    // And read.
    frameHandler.headers(false, false, 3, 0, grpcResponseHeaders(), HeadersMode.HTTP_20_HEADERS);
    final String receivedMessage = "No, you are fine.";
    Buffer buffer = createMessageFrame(receivedMessage);
    frameHandler.data(false, 3, buffer, (int) buffer.size());
    frameHandler.headers(true, true, 3, 0, grpcResponseTrailers(), HeadersMode.HTTP_20_HEADERS);
    listener1.waitUntilStreamClosed();
    assertEquals(1, listener1.messages.size());
    assertEquals(receivedMessage, listener1.messages.get(0));

    // The transport should be stopped after all active streams finished.
    verify(listener).transportTerminated();
  }

  @Test
  public void streamIdExhausted() throws Exception {
    int startId = Integer.MAX_VALUE - 2;
    AsyncFrameWriter writer =  mock(AsyncFrameWriter.class);
    OkHttpClientTransport transport =
        new OkHttpClientTransport(executor, frameReader, writer, startId);
    transport.start(listener);
    streams = transport.getStreams();

    MockStreamListener listener1 = new MockStreamListener();
    transport.newStream(method,new Metadata.Headers(), listener1);

    try {
      transport.newStream(method, new Metadata.Headers(), new MockStreamListener());
      fail("new stream should not be accepted by a go-away transport.");
    } catch (IllegalStateException ex) {
      // expected.
    }

    streams.get(startId).cancel();
    listener1.waitUntilStreamClosed();
    verify(writer).rstStream(eq(startId), eq(ErrorCode.CANCEL));
    verify(listener).transportShutdown();
    verify(listener).transportTerminated();
  }

  private static Buffer createMessageFrame(String message) {
    return createMessageFrame(message.getBytes(UTF_8));
  }

  private static Buffer createMessageFrame(byte[] message) {
    Buffer buffer = new Buffer();
    buffer.writeByte(0 /* UNCOMPRESSED */);
    buffer.writeInt(message.length);
    buffer.write(message);
    return buffer;
  }

  private List<Header> grpcResponseHeaders() {
    return ImmutableList.<Header>builder()
        .add(new Header(":status", "200"))
        .add(Headers.CONTENT_TYPE_HEADER)
        .build();
  }

  private List<Header> grpcResponseTrailers() {
    return ImmutableList.<Header>builder()
        .add(new Header(Status.CODE_KEY.name(), "0"))
        .build();
  }

  private static class MockFrameReader implements FrameReader {
    boolean closed;
    boolean throwExceptionForNextFrame;

    @Override
    public void close() throws IOException {
      closed = true;
    }

    @Override
    public boolean nextFrame(Handler handler) throws IOException {
      if (throwExceptionForNextFrame) {
        throw new IOException(NETWORK_ISSUE_MESSAGE);
      }
      synchronized (this) {
        try {
          wait();
        } catch (InterruptedException e) {
          throw new IOException(e);
        }
      }
      if (throwExceptionForNextFrame) {
        throw new IOException(NETWORK_ISSUE_MESSAGE);
      }
      return true;
    }

    synchronized void throwIOExceptionForNextFrame() {
      throwExceptionForNextFrame = true;
      notifyAll();
    }

    @Override
    public void readConnectionPreface() throws IOException {
      // not used.
    }
  }

  private static class MockStreamListener implements ClientStreamListener {
    Status status;
    Metadata.Headers headers;
    Metadata.Trailers trailers;
    CountDownLatch closed = new CountDownLatch(1);
    ArrayList<String> messages = new ArrayList<String>();

    MockStreamListener() {
    }

    @Override
    public void headersRead(Metadata.Headers headers) {
      this.headers = headers;
    }

    @Override
    public void messageRead(InputStream message) {
      String msg = getContent(message);
      if (msg != null) {
        messages.add(msg);
      }
    }

    @Override
    public void closed(Status status, Metadata.Trailers trailers) {
      this.status = status;
      this.trailers = trailers;
      closed.countDown();
    }

    void waitUntilStreamClosed() throws InterruptedException {
      if (!closed.await(TIME_OUT_MS, TimeUnit.MILLISECONDS)) {
        fail("Failed waiting stream to be closed.");
      }
    }

    static String getContent(InputStream message) {
      BufferedReader br = new BufferedReader(new InputStreamReader(message, UTF_8));
      try {
        // Only one line message is used in this test.
        return br.readLine();
      } catch (IOException e) {
        return null;
      } finally {
        try {
          message.close();
        } catch (IOException e) {
          throw new RuntimeException(e);
        }
      }
    }
  }
}
