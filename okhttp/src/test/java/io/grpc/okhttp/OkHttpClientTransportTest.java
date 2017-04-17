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

package io.grpc.okhttp;

import static com.google.common.base.Charsets.UTF_8;
import static com.google.common.truth.Truth.assertThat;
import static io.grpc.internal.GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE;
import static io.grpc.okhttp.Headers.CONTENT_TYPE_HEADER;
import static io.grpc.okhttp.Headers.METHOD_HEADER;
import static io.grpc.okhttp.Headers.SCHEME_HEADER;
import static io.grpc.okhttp.Headers.TE_HEADER;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyBoolean;
import static org.mockito.Matchers.anyInt;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.isA;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.reset;
import static org.mockito.Mockito.timeout;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.verifyZeroInteractions;
import static org.mockito.Mockito.when;

import com.google.common.base.Ticker;
import com.google.common.collect.ImmutableList;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.SettableFuture;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.Status;
import io.grpc.Status.Code;
import io.grpc.StatusException;
import io.grpc.internal.AbstractStream2;
import io.grpc.internal.ClientStreamListener;
import io.grpc.internal.ClientTransport;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.ManagedClientTransport;
import io.grpc.okhttp.OkHttpClientTransport.ClientFrameHandler;
import io.grpc.okhttp.internal.ConnectionSpec;
import io.grpc.okhttp.internal.framed.ErrorCode;
import io.grpc.okhttp.internal.framed.FrameReader;
import io.grpc.okhttp.internal.framed.FrameWriter;
import io.grpc.okhttp.internal.framed.Header;
import io.grpc.okhttp.internal.framed.HeadersMode;
import io.grpc.okhttp.internal.framed.Settings;
import io.grpc.testing.TestMethodDescriptors;
import java.io.BufferedReader;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import javax.annotation.Nullable;
import okio.Buffer;
import okio.ByteString;
import org.junit.After;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.Timeout;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Matchers;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/**
 * Tests for {@link OkHttpClientTransport}.
 */
@RunWith(JUnit4.class)
public class OkHttpClientTransportTest {
  private static final int TIME_OUT_MS = 2000;
  private static final String NETWORK_ISSUE_MESSAGE = "network issue";
  private static final String ERROR_MESSAGE = "simulated error";
  // The gRPC header length, which includes 1 byte compression flag and 4 bytes message length.
  private static final int HEADER_LENGTH = 5;

  @Rule
  public Timeout globalTimeout = new Timeout(10 * 1000);

  @Mock
  private FrameWriter frameWriter;

  private MethodDescriptor<Void, Void> method = TestMethodDescriptors.voidMethod();

  @Mock
  private ManagedClientTransport.Listener transportListener;
  private OkHttpClientTransport clientTransport;
  private MockFrameReader frameReader;
  private ExecutorService executor;
  private long nanoTime; // backs a ticker, for testing ping round-trip time measurement
  private SettableFuture<Void> connectedFuture;
  private DelayConnectedCallback delayConnectedCallback;
  private Runnable tooManyPingsRunnable = new Runnable() {
    @Override public void run() {
      throw new AssertionError();
    }
  };

  /** Set up for test. */
  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    executor = Executors.newCachedThreadPool();
    when(frameWriter.maxDataLength()).thenReturn(Integer.MAX_VALUE);
    frameReader = new MockFrameReader();
  }

  @After
  public void tearDown() {
    executor.shutdownNow();
  }

  private void initTransport() throws Exception {
    startTransport(3, null, true, DEFAULT_MAX_MESSAGE_SIZE, null);
  }

  private void initTransport(int startId) throws Exception {
    startTransport(startId, null, true, DEFAULT_MAX_MESSAGE_SIZE, null);
  }

  private void initTransportAndDelayConnected() throws Exception {
    delayConnectedCallback = new DelayConnectedCallback();
    startTransport(3, delayConnectedCallback, false, DEFAULT_MAX_MESSAGE_SIZE, null);
  }

  private void startTransport(int startId, @Nullable Runnable connectingCallback,
      boolean waitingForConnected, int maxMessageSize, String userAgent) throws Exception {
    connectedFuture = SettableFuture.create();
    Ticker ticker = new Ticker() {
      @Override
      public long read() {
        return nanoTime;
      }
    };
    clientTransport = new OkHttpClientTransport(userAgent, executor, frameReader,
        frameWriter, startId, new MockSocket(frameReader), ticker, connectingCallback,
        connectedFuture, maxMessageSize, tooManyPingsRunnable);
    clientTransport.start(transportListener);
    if (waitingForConnected) {
      connectedFuture.get(TIME_OUT_MS, TimeUnit.MILLISECONDS);
    }
  }

  @Test
  public void testToString() throws Exception {
    InetSocketAddress address = InetSocketAddress.createUnresolved("hostname", 31415);
    clientTransport = new OkHttpClientTransport(
        address, "hostname", null /* agent */, executor, null,
        Utils.convertSpec(OkHttpChannelBuilder.DEFAULT_CONNECTION_SPEC), DEFAULT_MAX_MESSAGE_SIZE,
        null, null, null, tooManyPingsRunnable);
    String s = clientTransport.toString();
    assertTrue("Unexpected: " + s, s.contains("OkHttpClientTransport"));
    assertTrue("Unexpected: " + s, s.contains(address.toString()));
  }

  @Test
  public void maxMessageSizeShouldBeEnforced() throws Exception {
    // Allow the response payloads of up to 1 byte.
    startTransport(3, null, true, 1, null);

    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    stream.request(1);
    assertContainStream(3);
    frameHandler().headers(false, false, 3, 0, grpcResponseHeaders(), HeadersMode.HTTP_20_HEADERS);
    assertNotNull(listener.headers);

    // Receive the message.
    final String message = "Hello Client";
    Buffer buffer = createMessageFrame(message);
    frameHandler().data(false, 3, buffer, (int) buffer.size());

    listener.waitUntilStreamClosed();
    assertEquals(Code.RESOURCE_EXHAUSTED, listener.status.getCode());
    shutdownAndVerify();
  }

  /**
   * When nextFrame throws IOException, the transport should be aborted.
   */
  @Test
  public void nextFrameThrowIoException() throws Exception {
    initTransport();
    MockStreamListener listener1 = new MockStreamListener();
    MockStreamListener listener2 = new MockStreamListener();
    OkHttpClientStream stream1 = clientTransport.newStream(method, new Metadata());
    stream1.start(listener1);
    stream1.request(1);
    OkHttpClientStream stream2 = clientTransport.newStream(method, new Metadata());
    stream2.start(listener2);
    stream2.request(1);
    assertEquals(2, activeStreamCount());
    assertContainStream(3);
    assertContainStream(5);
    frameReader.throwIoExceptionForNextFrame();
    listener1.waitUntilStreamClosed();
    listener2.waitUntilStreamClosed();

    assertEquals(0, activeStreamCount());
    assertEquals(Status.UNAVAILABLE.getCode(), listener1.status.getCode());
    assertEquals(NETWORK_ISSUE_MESSAGE, listener1.status.getCause().getMessage());
    assertEquals(Status.UNAVAILABLE.getCode(), listener2.status.getCode());
    assertEquals(NETWORK_ISSUE_MESSAGE, listener2.status.getCause().getMessage());
    verify(transportListener, timeout(TIME_OUT_MS)).transportShutdown(isA(Status.class));
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
    shutdownAndVerify();
  }

  /**
   * Test that even if an Error is thrown from the reading loop of the transport,
   * it can still clean up and call transportShutdown() and transportTerminated() as expected
   * by the channel.
   */
  @Test
  public void nextFrameThrowsError() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    stream.request(1);
    assertEquals(1, activeStreamCount());
    assertContainStream(3);
    frameReader.throwErrorForNextFrame();
    listener.waitUntilStreamClosed();

    assertEquals(0, activeStreamCount());
    assertEquals(Status.UNAVAILABLE.getCode(), listener.status.getCode());
    assertEquals(ERROR_MESSAGE, listener.status.getCause().getMessage());
    verify(transportListener, timeout(TIME_OUT_MS)).transportShutdown(isA(Status.class));
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
    shutdownAndVerify();
  }

  @Test
  public void nextFrameReturnFalse() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    stream.request(1);
    frameReader.nextFrameAtEndOfStream();
    listener.waitUntilStreamClosed();
    assertEquals(Status.UNAVAILABLE.getCode(), listener.status.getCode());
    verify(transportListener, timeout(TIME_OUT_MS)).transportShutdown(isA(Status.class));
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
    shutdownAndVerify();
  }

  @Test
  public void readMessages() throws Exception {
    initTransport();
    final int numMessages = 10;
    final String message = "Hello Client";
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    stream.request(numMessages);
    assertContainStream(3);
    frameHandler().headers(false, false, 3, 0, grpcResponseHeaders(), HeadersMode.HTTP_20_HEADERS);
    assertNotNull(listener.headers);
    for (int i = 0; i < numMessages; i++) {
      Buffer buffer = createMessageFrame(message + i);
      frameHandler().data(false, 3, buffer, (int) buffer.size());
    }
    frameHandler().headers(true, true, 3, 0, grpcResponseTrailers(), HeadersMode.HTTP_20_HEADERS);
    listener.waitUntilStreamClosed();
    assertEquals(Status.OK, listener.status);
    assertNotNull(listener.trailers);
    assertEquals(numMessages, listener.messages.size());
    for (int i = 0; i < numMessages; i++) {
      assertEquals(message + i, listener.messages.get(i));
    }
    shutdownAndVerify();
  }

  @Test
  public void receivedHeadersForInvalidStreamShouldKillConnection() throws Exception {
    initTransport();
    // Empty headers block without correct content type or status
    frameHandler().headers(false, false, 3, 0, new ArrayList<Header>(),
        HeadersMode.HTTP_20_HEADERS);
    verify(frameWriter, timeout(TIME_OUT_MS))
        .goAway(eq(0), eq(ErrorCode.PROTOCOL_ERROR), any(byte[].class));
    verify(transportListener).transportShutdown(isA(Status.class));
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
    shutdownAndVerify();
  }

  @Test
  public void receivedDataForInvalidStreamShouldKillConnection() throws Exception {
    initTransport();
    frameHandler().data(false, 3, createMessageFrame(new String(new char[1000])), 1000);
    verify(frameWriter, timeout(TIME_OUT_MS))
        .goAway(eq(0), eq(ErrorCode.PROTOCOL_ERROR), any(byte[].class));
    verify(transportListener).transportShutdown(isA(Status.class));
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
    shutdownAndVerify();
  }

  @Test
  public void invalidInboundHeadersCancelStream() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    stream.request(1);
    assertContainStream(3);
    // Headers block without correct content type or status
    frameHandler().headers(false, false, 3, 0, Arrays.asList(new Header("random", "4")),
        HeadersMode.HTTP_20_HEADERS);
    // Now wait to receive 1000 bytes of data so we can have a better error message before
    // cancelling the streaam.
    frameHandler().data(false, 3, createMessageFrame(new String(new char[1000])), 1000);
    verify(frameWriter, timeout(TIME_OUT_MS)).rstStream(eq(3), eq(ErrorCode.CANCEL));
    assertNull(listener.headers);
    assertEquals(Status.INTERNAL.getCode(), listener.status.getCode());
    assertNotNull(listener.trailers);
    assertEquals("4", listener.trailers
        .get(Metadata.Key.of("random", Metadata.ASCII_STRING_MARSHALLER)));
    shutdownAndVerify();
  }

  @Test
  public void invalidInboundTrailersPropagateToMetadata() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    stream.request(1);
    assertContainStream(3);
    // Headers block with EOS without correct content type or status
    frameHandler().headers(true, true, 3, 0, Arrays.asList(new Header("random", "4")),
        HeadersMode.HTTP_20_HEADERS);
    assertNull(listener.headers);
    assertEquals(Status.INTERNAL.getCode(), listener.status.getCode());
    assertNotNull(listener.trailers);
    assertEquals("4", listener.trailers
        .get(Metadata.Key.of("random", Metadata.ASCII_STRING_MARSHALLER)));
    shutdownAndVerify();
  }

  @Test
  public void readStatus() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    assertContainStream(3);
    frameHandler().headers(true, true, 3, 0, grpcResponseTrailers(), HeadersMode.HTTP_20_HEADERS);
    listener.waitUntilStreamClosed();
    assertEquals(Status.Code.OK, listener.status.getCode());
    shutdownAndVerify();
  }

  @Test
  public void receiveReset() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    assertContainStream(3);
    frameHandler().rstStream(3, ErrorCode.PROTOCOL_ERROR);
    listener.waitUntilStreamClosed();

    assertThat(listener.status.getDescription()).contains("Rst Stream");
    assertThat(listener.status.getCode()).isEqualTo(Code.INTERNAL);
    shutdownAndVerify();
  }

  @Test
  public void cancelStream() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    getStream(3).cancel(Status.CANCELLED);
    verify(frameWriter, timeout(TIME_OUT_MS)).rstStream(eq(3), eq(ErrorCode.CANCEL));
    listener.waitUntilStreamClosed();
    assertEquals(OkHttpClientTransport.toGrpcStatus(ErrorCode.CANCEL).getCode(),
        listener.status.getCode());
    shutdownAndVerify();
  }

  @Test
  public void addDefaultUserAgent() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    Header userAgentHeader = new Header(GrpcUtil.USER_AGENT_KEY.name(),
            GrpcUtil.getGrpcUserAgent("okhttp", null));
    List<Header> expectedHeaders = Arrays.asList(SCHEME_HEADER, METHOD_HEADER,
            new Header(Header.TARGET_AUTHORITY, "notarealauthority:80"),
            new Header(Header.TARGET_PATH, "/" + method.getFullMethodName()),
            userAgentHeader, CONTENT_TYPE_HEADER, TE_HEADER);
    verify(frameWriter, timeout(TIME_OUT_MS))
        .synStream(eq(false), eq(false), eq(3), eq(0), eq(expectedHeaders));
    getStream(3).cancel(Status.CANCELLED);
    shutdownAndVerify();
  }

  @Test
  public void overrideDefaultUserAgent() throws Exception {
    startTransport(3, null, true, DEFAULT_MAX_MESSAGE_SIZE, "fakeUserAgent");
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    List<Header> expectedHeaders = Arrays.asList(SCHEME_HEADER, METHOD_HEADER,
        new Header(Header.TARGET_AUTHORITY, "notarealauthority:80"),
        new Header(Header.TARGET_PATH, "/" + method.getFullMethodName()),
        new Header(GrpcUtil.USER_AGENT_KEY.name(),
            GrpcUtil.getGrpcUserAgent("okhttp", "fakeUserAgent")),
        CONTENT_TYPE_HEADER, TE_HEADER);
    verify(frameWriter, timeout(TIME_OUT_MS))
        .synStream(eq(false), eq(false), eq(3), eq(0), eq(expectedHeaders));
    getStream(3).cancel(Status.CANCELLED);
    shutdownAndVerify();
  }

  @Test
  public void cancelStreamForDeadlineExceeded() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    getStream(3).cancel(Status.DEADLINE_EXCEEDED);
    verify(frameWriter, timeout(TIME_OUT_MS)).rstStream(eq(3), eq(ErrorCode.CANCEL));
    listener.waitUntilStreamClosed();
    shutdownAndVerify();
  }

  @Test
  public void writeMessage() throws Exception {
    initTransport();
    final String message = "Hello Server";
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    InputStream input = new ByteArrayInputStream(message.getBytes(UTF_8));
    assertEquals(12, input.available());
    stream.writeMessage(input);
    stream.flush();
    ArgumentCaptor<Buffer> captor = ArgumentCaptor.forClass(Buffer.class);
    verify(frameWriter, timeout(TIME_OUT_MS))
        .data(eq(false), eq(3), captor.capture(), eq(12 + HEADER_LENGTH));
    Buffer sentFrame = captor.getValue();
    assertEquals(createMessageFrame(message), sentFrame);
    stream.cancel(Status.CANCELLED);
    shutdownAndVerify();
  }

  @Test
  public void windowUpdate() throws Exception {
    initTransport();
    MockStreamListener listener1 = new MockStreamListener();
    MockStreamListener listener2 = new MockStreamListener();
    OkHttpClientStream stream1 = clientTransport.newStream(method, new Metadata());
    stream1.start(listener1);
    stream1.request(2);

    OkHttpClientStream stream2 = clientTransport.newStream(method, new Metadata());
    stream2.start(listener2);
    stream2.request(2);
    assertEquals(2, activeStreamCount());
    stream1 = getStream(3);
    stream2 = getStream(5);

    frameHandler().headers(false, false, 3, 0, grpcResponseHeaders(), HeadersMode.HTTP_20_HEADERS);
    frameHandler().headers(false, false, 5, 0, grpcResponseHeaders(), HeadersMode.HTTP_20_HEADERS);

    int messageLength = Utils.DEFAULT_WINDOW_SIZE / 4;
    byte[] fakeMessage = new byte[messageLength];

    // Stream 1 receives a message
    Buffer buffer = createMessageFrame(fakeMessage);
    int messageFrameLength = (int) buffer.size();
    frameHandler().data(false, 3, buffer, messageFrameLength);

    // Stream 2 receives a message
    buffer = createMessageFrame(fakeMessage);
    frameHandler().data(false, 5, buffer, messageFrameLength);

    verify(frameWriter, timeout(TIME_OUT_MS))
        .windowUpdate(eq(0), eq((long) 2 * messageFrameLength));
    reset(frameWriter);

    // Stream 1 receives another message
    buffer = createMessageFrame(fakeMessage);
    frameHandler().data(false, 3, buffer, messageFrameLength);

    verify(frameWriter, timeout(TIME_OUT_MS))
        .windowUpdate(eq(3), eq((long) 2 * messageFrameLength));

    // Stream 2 receives another message
    buffer = createMessageFrame(fakeMessage);
    frameHandler().data(false, 5, buffer, messageFrameLength);

    verify(frameWriter, timeout(TIME_OUT_MS))
        .windowUpdate(eq(5), eq((long) 2 * messageFrameLength));
    verify(frameWriter, timeout(TIME_OUT_MS))
        .windowUpdate(eq(0), eq((long) 2 * messageFrameLength));

    stream1.cancel(Status.CANCELLED);
    verify(frameWriter, timeout(TIME_OUT_MS)).rstStream(eq(3), eq(ErrorCode.CANCEL));
    listener1.waitUntilStreamClosed();
    assertEquals(OkHttpClientTransport.toGrpcStatus(ErrorCode.CANCEL).getCode(),
        listener1.status.getCode());

    stream2.cancel(Status.CANCELLED);
    verify(frameWriter, timeout(TIME_OUT_MS)).rstStream(eq(5), eq(ErrorCode.CANCEL));
    listener2.waitUntilStreamClosed();
    assertEquals(OkHttpClientTransport.toGrpcStatus(ErrorCode.CANCEL).getCode(),
        listener2.status.getCode());
    shutdownAndVerify();
  }

  @Test
  public void windowUpdateWithInboundFlowControl() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    int messageLength = Utils.DEFAULT_WINDOW_SIZE / 2 + 1;
    byte[] fakeMessage = new byte[messageLength];

    frameHandler().headers(false, false, 3, 0, grpcResponseHeaders(), HeadersMode.HTTP_20_HEADERS);
    Buffer buffer = createMessageFrame(fakeMessage);
    long messageFrameLength = buffer.size();
    frameHandler().data(false, 3, buffer, (int) messageFrameLength);
    ArgumentCaptor<Integer> idCaptor = ArgumentCaptor.forClass(Integer.class);
    verify(frameWriter, timeout(TIME_OUT_MS)).windowUpdate(
        idCaptor.capture(), eq(messageFrameLength));
    // Should only send window update for the connection.
    assertEquals(1, idCaptor.getAllValues().size());
    assertEquals(0, (int)idCaptor.getValue());

    stream.request(1);
    // We return the bytes for the stream window as we read the message.
    verify(frameWriter, timeout(TIME_OUT_MS)).windowUpdate(eq(3), eq(messageFrameLength));

    getStream(3).cancel(Status.CANCELLED);
    verify(frameWriter, timeout(TIME_OUT_MS)).rstStream(eq(3), eq(ErrorCode.CANCEL));
    listener.waitUntilStreamClosed();
    assertEquals(OkHttpClientTransport.toGrpcStatus(ErrorCode.CANCEL).getCode(),
        listener.status.getCode());
    shutdownAndVerify();
  }

  @Test
  public void outboundFlowControl() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    // The first message should be sent out.
    int messageLength = Utils.DEFAULT_WINDOW_SIZE / 2 + 1;
    InputStream input = new ByteArrayInputStream(new byte[messageLength]);
    stream.writeMessage(input);
    stream.flush();
    verify(frameWriter, timeout(TIME_OUT_MS)).data(
        eq(false), eq(3), any(Buffer.class), eq(messageLength + HEADER_LENGTH));


    // The second message should be partially sent out.
    input = new ByteArrayInputStream(new byte[messageLength]);
    stream.writeMessage(input);
    stream.flush();
    int partiallySentSize =
        Utils.DEFAULT_WINDOW_SIZE - messageLength - HEADER_LENGTH;
    verify(frameWriter, timeout(TIME_OUT_MS))
        .data(eq(false), eq(3), any(Buffer.class), eq(partiallySentSize));

    // Get more credit, the rest data should be sent out.
    frameHandler().windowUpdate(3, Utils.DEFAULT_WINDOW_SIZE);
    frameHandler().windowUpdate(0, Utils.DEFAULT_WINDOW_SIZE);
    verify(frameWriter, timeout(TIME_OUT_MS)).data(
        eq(false), eq(3), any(Buffer.class),
        eq(messageLength + HEADER_LENGTH - partiallySentSize));

    stream.cancel(Status.CANCELLED);
    listener.waitUntilStreamClosed();
    shutdownAndVerify();
  }

  @Test
  public void outboundFlowControlWithInitialWindowSizeChange() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    int messageLength = 20;
    setInitialWindowSize(HEADER_LENGTH + 10);
    InputStream input = new ByteArrayInputStream(new byte[messageLength]);
    stream.writeMessage(input);
    stream.flush();
    // part of the message can be sent.
    verify(frameWriter, timeout(TIME_OUT_MS))
        .data(eq(false), eq(3), any(Buffer.class), eq(HEADER_LENGTH + 10));
    // Avoid connection flow control.
    frameHandler().windowUpdate(0, HEADER_LENGTH + 10);

    // Increase initial window size
    setInitialWindowSize(HEADER_LENGTH + 20);
    // The rest data should be sent.
    verify(frameWriter, timeout(TIME_OUT_MS)).data(eq(false), eq(3), any(Buffer.class), eq(10));
    frameHandler().windowUpdate(0, 10);

    // Decrease initial window size to HEADER_LENGTH, since we've already sent
    // out HEADER_LENGTH + 20 bytes data, the window size should be -20 now.
    setInitialWindowSize(HEADER_LENGTH);
    // Get 20 tokens back, still can't send any data.
    frameHandler().windowUpdate(3, 20);
    input = new ByteArrayInputStream(new byte[messageLength]);
    stream.writeMessage(input);
    stream.flush();
    // Only the previous two write operations happened.
    verify(frameWriter, timeout(TIME_OUT_MS).times(2))
        .data(anyBoolean(), anyInt(), any(Buffer.class), anyInt());

    // Get enough tokens to send the pending message.
    frameHandler().windowUpdate(3, HEADER_LENGTH + 20);
    verify(frameWriter, timeout(TIME_OUT_MS))
        .data(eq(false), eq(3), any(Buffer.class), eq(HEADER_LENGTH + 20));

    stream.cancel(Status.CANCELLED);
    listener.waitUntilStreamClosed();
    shutdownAndVerify();
  }

  @Test
  public void stopNormally() throws Exception {
    initTransport();
    MockStreamListener listener1 = new MockStreamListener();
    MockStreamListener listener2 = new MockStreamListener();
    OkHttpClientStream stream1 = clientTransport.newStream(method, new Metadata());
    stream1.start(listener1);
    OkHttpClientStream stream2 = clientTransport.newStream(method, new Metadata());
    stream2.start(listener2);
    assertEquals(2, activeStreamCount());
    clientTransport.shutdown();

    assertEquals(2, activeStreamCount());
    verify(transportListener).transportShutdown(isA(Status.class));

    stream1.cancel(Status.CANCELLED);
    stream2.cancel(Status.CANCELLED);
    listener1.waitUntilStreamClosed();
    listener2.waitUntilStreamClosed();
    assertEquals(0, activeStreamCount());
    assertEquals(Status.CANCELLED.getCode(), listener1.status.getCode());
    assertEquals(Status.CANCELLED.getCode(), listener2.status.getCode());
    verify(frameWriter, timeout(TIME_OUT_MS)).goAway(eq(0), eq(ErrorCode.NO_ERROR), (byte[]) any());
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
    shutdownAndVerify();
  }

  @Test
  public void receiveGoAway() throws Exception {
    initTransport();
    // start 2 streams.
    MockStreamListener listener1 = new MockStreamListener();
    MockStreamListener listener2 = new MockStreamListener();
    OkHttpClientStream stream1 = clientTransport.newStream(method, new Metadata());
    stream1.start(listener1);
    stream1.request(1);
    OkHttpClientStream stream2 = clientTransport.newStream(method, new Metadata());
    stream2.start(listener2);
    stream2.request(1);
    assertEquals(2, activeStreamCount());

    // Receive goAway, max good id is 3.
    frameHandler().goAway(3, ErrorCode.CANCEL, ByteString.EMPTY);

    // Transport should be in STOPPING state.
    verify(transportListener).transportShutdown(isA(Status.class));
    verify(transportListener, never()).transportTerminated();

    // Stream 2 should be closed.
    listener2.waitUntilStreamClosed();
    assertEquals(1, activeStreamCount());
    assertEquals(Status.CANCELLED.getCode(), listener2.status.getCode());

    // New stream should be failed.
    assertNewStreamFail();

    // But stream 1 should be able to send.
    final String sentMessage = "Should I also go away?";
    OkHttpClientStream stream = getStream(3);
    InputStream input = new ByteArrayInputStream(sentMessage.getBytes(UTF_8));
    assertEquals(22, input.available());
    stream.writeMessage(input);
    stream.flush();
    ArgumentCaptor<Buffer> captor =
        ArgumentCaptor.forClass(Buffer.class);
    verify(frameWriter, timeout(TIME_OUT_MS))
        .data(eq(false), eq(3), captor.capture(), eq(22 + HEADER_LENGTH));
    Buffer sentFrame = captor.getValue();
    assertEquals(createMessageFrame(sentMessage), sentFrame);

    // And read.
    frameHandler().headers(false, false, 3, 0, grpcResponseHeaders(), HeadersMode.HTTP_20_HEADERS);
    final String receivedMessage = "No, you are fine.";
    Buffer buffer = createMessageFrame(receivedMessage);
    frameHandler().data(false, 3, buffer, (int) buffer.size());
    frameHandler().headers(true, true, 3, 0, grpcResponseTrailers(), HeadersMode.HTTP_20_HEADERS);
    listener1.waitUntilStreamClosed();
    assertEquals(1, listener1.messages.size());
    assertEquals(receivedMessage, listener1.messages.get(0));

    // The transport should be stopped after all active streams finished.
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
    shutdownAndVerify();
  }

  @Test
  public void streamIdExhausted() throws Exception {
    int startId = Integer.MAX_VALUE - 2;
    initTransport(startId);

    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    stream.request(1);

    // New stream should be failed.
    assertNewStreamFail();

    // The alive stream should be functional, receives a message.
    frameHandler().headers(
        false, false, startId, 0, grpcResponseHeaders(), HeadersMode.HTTP_20_HEADERS);
    assertNotNull(listener.headers);
    String message = "hello";
    Buffer buffer = createMessageFrame(message);
    frameHandler().data(false, startId, buffer, (int) buffer.size());

    getStream(startId).cancel(Status.CANCELLED);
    // Receives the second message after be cancelled.
    buffer = createMessageFrame(message);
    frameHandler().data(false, startId, buffer, (int) buffer.size());

    listener.waitUntilStreamClosed();
    // Should only have the first message delivered.
    assertEquals(message, listener.messages.get(0));
    verify(frameWriter, timeout(TIME_OUT_MS)).rstStream(eq(startId), eq(ErrorCode.CANCEL));
    verify(transportListener).transportShutdown(isA(Status.class));
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
    shutdownAndVerify();
  }

  @Test
  public void pendingStreamSucceed() throws Exception {
    initTransport();
    setMaxConcurrentStreams(1);
    final MockStreamListener listener1 = new MockStreamListener();
    final MockStreamListener listener2 = new MockStreamListener();
    OkHttpClientStream stream1 = clientTransport.newStream(method, new Metadata());
    stream1.start(listener1);
    // The second stream should be pending.
    OkHttpClientStream stream2 = clientTransport.newStream(method, new Metadata());
    stream2.start(listener2);
    String sentMessage = "hello";
    InputStream input = new ByteArrayInputStream(sentMessage.getBytes(UTF_8));
    assertEquals(5, input.available());
    stream2.writeMessage(input);
    stream2.flush();
    stream2.halfClose();

    waitForStreamPending(1);
    assertEquals(1, activeStreamCount());

    // Finish the first stream
    stream1.cancel(Status.CANCELLED);
    listener1.waitUntilStreamClosed();

    // The second stream should be active now, and the pending data should be sent out.
    assertEquals(1, activeStreamCount());
    assertEquals(0, clientTransport.getPendingStreamSize());
    ArgumentCaptor<Buffer> captor = ArgumentCaptor.forClass(Buffer.class);
    verify(frameWriter, timeout(TIME_OUT_MS))
        .data(eq(false), eq(5), captor.capture(), eq(5 + HEADER_LENGTH));
    Buffer sentFrame = captor.getValue();
    assertEquals(createMessageFrame(sentMessage), sentFrame);
    verify(frameWriter, timeout(TIME_OUT_MS)).data(eq(true), eq(5), any(Buffer.class), eq(0));
    stream2.cancel(Status.CANCELLED);
    shutdownAndVerify();
  }

  @Test
  public void pendingStreamCancelled() throws Exception {
    initTransport();
    setMaxConcurrentStreams(0);
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    waitForStreamPending(1);
    stream.cancel(Status.CANCELLED);
    // The second cancel should be an no-op.
    stream.cancel(Status.UNKNOWN);
    listener.waitUntilStreamClosed();
    assertEquals(0, clientTransport.getPendingStreamSize());
    assertEquals(Status.CANCELLED.getCode(), listener.status.getCode());
    shutdownAndVerify();
  }

  @Test
  public void pendingStreamFailedByGoAway() throws Exception {
    initTransport();
    setMaxConcurrentStreams(1);
    final MockStreamListener listener1 = new MockStreamListener();
    final MockStreamListener listener2 = new MockStreamListener();
    OkHttpClientStream stream1 = clientTransport.newStream(method, new Metadata());
    stream1.start(listener1);
    // The second stream should be pending.
    OkHttpClientStream stream2 = clientTransport.newStream(method, new Metadata());
    stream2.start(listener2);

    waitForStreamPending(1);
    assertEquals(1, activeStreamCount());

    // Receives GO_AWAY.
    frameHandler().goAway(99, ErrorCode.CANCEL, ByteString.EMPTY);

    listener2.waitUntilStreamClosed();
    assertEquals(Status.CANCELLED.getCode(), listener2.status.getCode());
    assertEquals(0, clientTransport.getPendingStreamSize());

    // active stream should not be affected.
    assertEquals(1, activeStreamCount());
    getStream(3).cancel(Status.CANCELLED);
    shutdownAndVerify();
  }

  @Test
  public void pendingStreamSucceedAfterShutdown() throws Exception {
    initTransport();
    setMaxConcurrentStreams(0);
    final MockStreamListener listener = new MockStreamListener();
    // The second stream should be pending.
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    waitForStreamPending(1);

    clientTransport.shutdown();
    setMaxConcurrentStreams(1);
    verify(frameWriter, timeout(TIME_OUT_MS))
        .synStream(anyBoolean(), anyBoolean(), eq(3), anyInt(), anyListHeader());
    assertEquals(1, activeStreamCount());
    stream.cancel(Status.CANCELLED);
    shutdownAndVerify();
  }

  @Test
  public void pendingStreamFailedByIdExhausted() throws Exception {
    int startId = Integer.MAX_VALUE - 4;
    initTransport(startId);
    setMaxConcurrentStreams(1);

    final MockStreamListener listener1 = new MockStreamListener();
    final MockStreamListener listener2 = new MockStreamListener();
    final MockStreamListener listener3 = new MockStreamListener();

    OkHttpClientStream stream1 = clientTransport.newStream(method, new Metadata());
    stream1.start(listener1);

    // The second and third stream should be pending.
    OkHttpClientStream stream2 = clientTransport.newStream(method, new Metadata());
    stream2.start(listener2);
    OkHttpClientStream stream3 = clientTransport.newStream(method, new Metadata());
    stream3.start(listener3);

    waitForStreamPending(2);
    assertEquals(1, activeStreamCount());

    // Now finish stream1, stream2 should be started and exhaust the id,
    // so stream3 should be failed.
    stream1.cancel(Status.CANCELLED);
    listener1.waitUntilStreamClosed();
    listener3.waitUntilStreamClosed();
    assertEquals(Status.UNAVAILABLE.getCode(), listener3.status.getCode());
    assertEquals(0, clientTransport.getPendingStreamSize());
    assertEquals(1, activeStreamCount());
    stream2 = getStream(startId + 2);
    stream2.cancel(Status.CANCELLED);
    shutdownAndVerify();
  }

  @Test
  public void receivingWindowExceeded() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    stream.request(1);

    frameHandler().headers(false, false, 3, 0, grpcResponseHeaders(), HeadersMode.HTTP_20_HEADERS);

    int messageLength = Utils.DEFAULT_WINDOW_SIZE + 1;
    byte[] fakeMessage = new byte[messageLength];
    Buffer buffer = createMessageFrame(fakeMessage);
    int messageFrameLength = (int) buffer.size();
    frameHandler().data(false, 3, buffer, messageFrameLength);

    listener.waitUntilStreamClosed();
    assertEquals(Status.INTERNAL.getCode(), listener.status.getCode());
    assertEquals("Received data size exceeded our receiving window size",
        listener.status.getDescription());
    verify(frameWriter, timeout(TIME_OUT_MS)).rstStream(eq(3), eq(ErrorCode.FLOW_CONTROL_ERROR));
    shutdownAndVerify();
  }

  @Test
  public void unaryHeadersShouldNotBeFlushed() throws Exception {
    // By default the method is a Unary call
    shouldHeadersBeFlushed(false);
    shutdownAndVerify();
  }

  @Test
  public void serverStreamingHeadersShouldNotBeFlushed() throws Exception {
    method = method.toBuilder().setType(MethodType.SERVER_STREAMING).build();
    shouldHeadersBeFlushed(false);
    shutdownAndVerify();
  }

  @Test
  public void clientStreamingHeadersShouldBeFlushed() throws Exception {
    method = method.toBuilder().setType(MethodType.CLIENT_STREAMING).build();
    shouldHeadersBeFlushed(true);
    shutdownAndVerify();
  }

  @Test
  public void duplexStreamingHeadersShouldNotBeFlushed() throws Exception {
    method = method.toBuilder().setType(MethodType.BIDI_STREAMING).build();
    shouldHeadersBeFlushed(true);
    shutdownAndVerify();
  }

  private void shouldHeadersBeFlushed(boolean shouldBeFlushed) throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    verify(frameWriter, timeout(TIME_OUT_MS)).synStream(
        eq(false), eq(false), eq(3), eq(0), Matchers.anyListOf(Header.class));
    if (shouldBeFlushed) {
      verify(frameWriter, timeout(TIME_OUT_MS)).flush();
    } else {
      verify(frameWriter, timeout(TIME_OUT_MS).times(0)).flush();
    }
    stream.cancel(Status.CANCELLED);
  }

  @Test
  public void receiveDataWithoutHeader() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    stream.request(1);
    Buffer buffer = createMessageFrame(new byte[1]);
    frameHandler().data(false, 3, buffer, (int) buffer.size());

    // Trigger the failure by a trailer.
    frameHandler().headers(
        true, true, 3, 0, grpcResponseHeaders(), HeadersMode.HTTP_20_HEADERS);

    listener.waitUntilStreamClosed();
    assertEquals(Status.INTERNAL.getCode(), listener.status.getCode());
    assertTrue(listener.status.getDescription().startsWith("headers not received before payload"));
    assertEquals(0, listener.messages.size());
    shutdownAndVerify();
  }

  @Test
  public void receiveDataWithoutHeaderAndTrailer() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    stream.request(1);
    Buffer buffer = createMessageFrame(new byte[1]);
    frameHandler().data(false, 3, buffer, (int) buffer.size());

    // Trigger the failure by a data frame.
    buffer = createMessageFrame(new byte[1]);
    frameHandler().data(true, 3, buffer, (int) buffer.size());

    listener.waitUntilStreamClosed();
    assertEquals(Status.INTERNAL.getCode(), listener.status.getCode());
    assertTrue(listener.status.getDescription().startsWith("headers not received before payload"));
    assertEquals(0, listener.messages.size());
    shutdownAndVerify();
  }

  @Test
  public void receiveLongEnoughDataWithoutHeaderAndTrailer() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    stream.request(1);
    Buffer buffer = createMessageFrame(new byte[1000]);
    frameHandler().data(false, 3, buffer, (int) buffer.size());

    // Once we receive enough detail, we cancel the stream. so we should have sent cancel.
    verify(frameWriter, timeout(TIME_OUT_MS)).rstStream(eq(3), eq(ErrorCode.CANCEL));

    listener.waitUntilStreamClosed();
    assertEquals(Status.INTERNAL.getCode(), listener.status.getCode());
    assertTrue(listener.status.getDescription().startsWith("headers not received before payload"));
    assertEquals(0, listener.messages.size());
    shutdownAndVerify();
  }

  @Test
  public void receiveDataForUnknownStreamUpdateConnectionWindow() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    stream.cancel(Status.CANCELLED);

    Buffer buffer = createMessageFrame(
        new byte[Utils.DEFAULT_WINDOW_SIZE / 2 + 1]);
    frameHandler().data(false, 3, buffer, (int) buffer.size());
    // Should still update the connection window even stream 3 is gone.
    verify(frameWriter, timeout(TIME_OUT_MS)).windowUpdate(0,
        HEADER_LENGTH + Utils.DEFAULT_WINDOW_SIZE / 2 + 1);
    buffer = createMessageFrame(
        new byte[Utils.DEFAULT_WINDOW_SIZE / 2 + 1]);

    // This should kill the connection, since we never created stream 5.
    frameHandler().data(false, 5, buffer, (int) buffer.size());
    verify(frameWriter, timeout(TIME_OUT_MS))
        .goAway(eq(0), eq(ErrorCode.PROTOCOL_ERROR), any(byte[].class));
    verify(transportListener).transportShutdown(isA(Status.class));
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
    shutdownAndVerify();
  }

  @Test
  public void receiveWindowUpdateForUnknownStream() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    stream.cancel(Status.CANCELLED);
    // This should be ignored.
    frameHandler().windowUpdate(3, 73);
    listener.waitUntilStreamClosed();
    // This should kill the connection, since we never created stream 5.
    frameHandler().windowUpdate(5, 73);
    verify(frameWriter, timeout(TIME_OUT_MS))
        .goAway(eq(0), eq(ErrorCode.PROTOCOL_ERROR), any(byte[].class));
    verify(transportListener).transportShutdown(isA(Status.class));
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
    shutdownAndVerify();
  }

  @Test
  public void shouldBeInitiallyReady() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    assertTrue(stream.isReady());
    assertTrue(listener.isOnReadyCalled());
    stream.cancel(Status.CANCELLED);
    assertFalse(stream.isReady());
    shutdownAndVerify();
  }

  @Test
  public void notifyOnReady() throws Exception {
    initTransport();
    // exactly one byte below the threshold
    int messageLength =
        AbstractStream2.TransportState.DEFAULT_ONREADY_THRESHOLD - HEADER_LENGTH - 1;
    setInitialWindowSize(0);
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    assertTrue(stream.isReady());
    // Be notified at the beginning.
    assertTrue(listener.isOnReadyCalled());

    // Write a message that will not exceed the notification threshold and queue it.
    InputStream input = new ByteArrayInputStream(new byte[messageLength]);
    stream.writeMessage(input);
    stream.flush();
    assertTrue(stream.isReady());

    // Write another two messages, still be queued.
    input = new ByteArrayInputStream(new byte[messageLength]);
    stream.writeMessage(input);
    stream.flush();
    assertFalse(stream.isReady());
    input = new ByteArrayInputStream(new byte[messageLength]);
    stream.writeMessage(input);
    stream.flush();
    assertFalse(stream.isReady());

    // Let the first message out.
    frameHandler().windowUpdate(0, HEADER_LENGTH + messageLength);
    frameHandler().windowUpdate(3, HEADER_LENGTH + messageLength);
    assertFalse(stream.isReady());
    assertFalse(listener.isOnReadyCalled());

    // Let the second message out.
    frameHandler().windowUpdate(0, HEADER_LENGTH + messageLength);
    frameHandler().windowUpdate(3, HEADER_LENGTH + messageLength);
    assertTrue(stream.isReady());
    assertTrue(listener.isOnReadyCalled());

    stream.cancel(Status.CANCELLED);
    shutdownAndVerify();
  }

  @Test
  public void transportReady() throws Exception {
    initTransport();
    verifyZeroInteractions(transportListener);
    frameHandler().settings(false, new Settings());
    verify(transportListener).transportReady();
    shutdownAndVerify();
  }

  @Test
  public void ping() throws Exception {
    initTransport();
    PingCallbackImpl callback1 = new PingCallbackImpl();
    clientTransport.ping(callback1, MoreExecutors.directExecutor());
    // add'l ping will be added as listener to outstanding operation
    PingCallbackImpl callback2 = new PingCallbackImpl();
    clientTransport.ping(callback2, MoreExecutors.directExecutor());

    ArgumentCaptor<Integer> captor1 = ArgumentCaptor.forClass(int.class);
    ArgumentCaptor<Integer> captor2 = ArgumentCaptor.forClass(int.class);
    verify(frameWriter, timeout(TIME_OUT_MS)).ping(eq(false), captor1.capture(), captor2.capture());
    // callback not invoked until we see acknowledgement
    assertEquals(0, callback1.invocationCount);
    assertEquals(0, callback2.invocationCount);

    int payload1 = captor1.getValue();
    int payload2 = captor2.getValue();
    // getting a bad ack won't complete the future
    // to make the ack "bad", we modify the payload so it doesn't match
    frameHandler().ping(true, payload1, payload2 - 1);
    // operation not complete because ack was wrong
    assertEquals(0, callback1.invocationCount);
    assertEquals(0, callback2.invocationCount);

    nanoTime += 10101;

    // reading the proper response should complete the future
    frameHandler().ping(true, payload1, payload2);
    assertEquals(1, callback1.invocationCount);
    assertEquals(10101, callback1.roundTripTime);
    assertNull(callback1.failureCause);
    // callback2 piggy-backed on same operation
    assertEquals(1, callback2.invocationCount);
    assertEquals(10101, callback2.roundTripTime);
    assertNull(callback2.failureCause);

    // now that previous ping is done, next request returns a different future
    callback1 = new PingCallbackImpl();
    clientTransport.ping(callback1, MoreExecutors.directExecutor());
    assertEquals(0, callback1.invocationCount);
    shutdownAndVerify();
  }

  @Test
  public void ping_failsWhenTransportShutdown() throws Exception {
    initTransport();
    PingCallbackImpl callback = new PingCallbackImpl();
    clientTransport.ping(callback, MoreExecutors.directExecutor());
    assertEquals(0, callback.invocationCount);

    clientTransport.shutdown();
    // ping failed on channel shutdown
    assertEquals(1, callback.invocationCount);
    assertTrue(callback.failureCause instanceof StatusException);
    assertEquals(Status.Code.UNAVAILABLE,
        ((StatusException) callback.failureCause).getStatus().getCode());

    // now that handler is in terminal state, all future pings fail immediately
    callback = new PingCallbackImpl();
    clientTransport.ping(callback, MoreExecutors.directExecutor());
    assertEquals(1, callback.invocationCount);
    assertTrue(callback.failureCause instanceof StatusException);
    assertEquals(Status.Code.UNAVAILABLE,
        ((StatusException) callback.failureCause).getStatus().getCode());
    shutdownAndVerify();
  }

  @Test
  public void ping_failsIfTransportFails() throws Exception {
    initTransport();
    PingCallbackImpl callback = new PingCallbackImpl();
    clientTransport.ping(callback, MoreExecutors.directExecutor());
    assertEquals(0, callback.invocationCount);

    clientTransport.onException(new IOException());
    // ping failed on error
    assertEquals(1, callback.invocationCount);
    assertTrue(callback.failureCause instanceof StatusException);
    assertEquals(Status.Code.UNAVAILABLE,
        ((StatusException) callback.failureCause).getStatus().getCode());

    // now that handler is in terminal state, all future pings fail immediately
    callback = new PingCallbackImpl();
    clientTransport.ping(callback, MoreExecutors.directExecutor());
    assertEquals(1, callback.invocationCount);
    assertTrue(callback.failureCause instanceof StatusException);
    assertEquals(Status.Code.UNAVAILABLE,
        ((StatusException) callback.failureCause).getStatus().getCode());
    shutdownAndVerify();
  }

  @Test
  public void writeBeforeConnected() throws Exception {
    initTransportAndDelayConnected();
    final String message = "Hello Server";
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    InputStream input = new ByteArrayInputStream(message.getBytes(UTF_8));
    stream.writeMessage(input);
    stream.flush();
    // The message should be queued.
    verifyNoMoreInteractions(frameWriter);

    allowTransportConnected();

    // The queued message should be sent out.
    ArgumentCaptor<Buffer> captor = ArgumentCaptor.forClass(Buffer.class);
    verify(frameWriter, timeout(TIME_OUT_MS))
        .data(eq(false), eq(3), captor.capture(), eq(12 + HEADER_LENGTH));
    Buffer sentFrame = captor.getValue();
    assertEquals(createMessageFrame(message), sentFrame);
    stream.cancel(Status.CANCELLED);
    shutdownAndVerify();
  }

  @Test
  public void cancelBeforeConnected() throws Exception {
    initTransportAndDelayConnected();
    final String message = "Hello Server";
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    InputStream input = new ByteArrayInputStream(message.getBytes(UTF_8));
    stream.writeMessage(input);
    stream.flush();
    stream.cancel(Status.CANCELLED);
    verifyNoMoreInteractions(frameWriter);

    allowTransportConnected();
    verifyNoMoreInteractions(frameWriter);
    shutdownAndVerify();
  }

  @Test
  public void shutdownDuringConnecting() throws Exception {
    initTransportAndDelayConnected();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    clientTransport.shutdown();
    allowTransportConnected();

    // The new stream should be failed, but not the pending stream.
    assertNewStreamFail();
    verify(frameWriter, timeout(TIME_OUT_MS))
        .synStream(anyBoolean(), anyBoolean(), eq(3), anyInt(), anyListHeader());
    assertEquals(1, activeStreamCount());
    stream.cancel(Status.CANCELLED);
    listener.waitUntilStreamClosed();
    assertEquals(Status.CANCELLED.getCode(), listener.status.getCode());
    shutdownAndVerify();
  }

  @Test
  public void invalidAuthorityPropagates() {
    clientTransport = new OkHttpClientTransport(
        new InetSocketAddress("host", 1234),
        "invalid_authority",
        "userAgent",
        executor,
        null,
        ConnectionSpec.CLEARTEXT,
        DEFAULT_MAX_MESSAGE_SIZE,
        null,
        null,
        null,
        tooManyPingsRunnable);

    String host = clientTransport.getOverridenHost();
    int port = clientTransport.getOverridenPort();

    assertEquals("invalid_authority", host);
    assertEquals(1234, port);
  }

  @Test
  public void unreachableServer() throws Exception {
    clientTransport = new OkHttpClientTransport(
        new InetSocketAddress("localhost", 0),
        "authority",
        "userAgent",
        executor,
        null,
        ConnectionSpec.CLEARTEXT,
        DEFAULT_MAX_MESSAGE_SIZE,
        null,
        null,
        null,
        tooManyPingsRunnable);

    ManagedClientTransport.Listener listener = mock(ManagedClientTransport.Listener.class);
    clientTransport.start(listener);
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(listener, timeout(TIME_OUT_MS)).transportShutdown(captor.capture());
    Status status = captor.getValue();
    assertEquals(Status.UNAVAILABLE.getCode(), status.getCode());
    assertTrue(status.getCause().toString(), status.getCause() instanceof IOException);

    MockStreamListener streamListener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata()).start(streamListener);
    streamListener.waitUntilStreamClosed();
    assertEquals(Status.UNAVAILABLE.getCode(), streamListener.status.getCode());
  }

  @Test
  public void proxy_200() throws Exception {
    ServerSocket serverSocket = new ServerSocket(0);
    clientTransport = new OkHttpClientTransport(
        InetSocketAddress.createUnresolved("theservice", 80),
        "authority",
        "userAgent",
        executor,
        null,
        ConnectionSpec.CLEARTEXT,
        DEFAULT_MAX_MESSAGE_SIZE,
        (InetSocketAddress) serverSocket.getLocalSocketAddress(),
        null,
        null,
        tooManyPingsRunnable);
    clientTransport.start(transportListener);

    Socket sock = serverSocket.accept();
    serverSocket.close();

    BufferedReader reader = new BufferedReader(new InputStreamReader(sock.getInputStream(), UTF_8));
    assertEquals("CONNECT theservice:80 HTTP/1.1", reader.readLine());
    assertEquals("Host: theservice:80", reader.readLine());
    while (!"".equals(reader.readLine())) {}

    sock.getOutputStream().write("HTTP/1.1 200 OK\r\nServer: test\r\n\r\n".getBytes(UTF_8));
    sock.getOutputStream().flush();

    assertEquals("PRI * HTTP/2.0", reader.readLine());
    assertEquals("", reader.readLine());
    assertEquals("SM", reader.readLine());
    assertEquals("", reader.readLine());

    // Empty SETTINGS
    sock.getOutputStream().write(new byte[] {0, 0, 0, 0, 0x4, 0});
    // GOAWAY
    sock.getOutputStream().write(new byte[] {
        0, 0, 0, 8, 0x7, 0,
        0, 0, 0, 0, // last stream id
        0, 0, 0, 0, // error code
    });
    sock.getOutputStream().flush();

    verify(transportListener, timeout(TIME_OUT_MS)).transportShutdown(isA(Status.class));
    while (sock.getInputStream().read() != -1) {}
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
    sock.close();
  }

  @Test
  public void proxy_500() throws Exception {
    ServerSocket serverSocket = new ServerSocket(0);
    clientTransport = new OkHttpClientTransport(
        InetSocketAddress.createUnresolved("theservice", 80),
        "authority",
        "userAgent",
        executor,
        null,
        ConnectionSpec.CLEARTEXT,
        DEFAULT_MAX_MESSAGE_SIZE,
        (InetSocketAddress) serverSocket.getLocalSocketAddress(),
        null,
        null,
        tooManyPingsRunnable);
    clientTransport.start(transportListener);

    Socket sock = serverSocket.accept();
    serverSocket.close();

    BufferedReader reader = new BufferedReader(new InputStreamReader(sock.getInputStream(), UTF_8));
    assertEquals("CONNECT theservice:80 HTTP/1.1", reader.readLine());
    assertEquals("Host: theservice:80", reader.readLine());
    while (!"".equals(reader.readLine())) {}

    final String errorText = "text describing error";
    sock.getOutputStream().write("HTTP/1.1 500 OH NO\r\n\r\n".getBytes(UTF_8));
    sock.getOutputStream().write(errorText.getBytes(UTF_8));
    sock.getOutputStream().flush();
    sock.shutdownOutput();

    assertEquals(-1, sock.getInputStream().read());

    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(transportListener, timeout(TIME_OUT_MS)).transportShutdown(captor.capture());
    Status error = captor.getValue();
    assertTrue("Status didn't contain error code: " + captor.getValue(),
        error.getDescription().contains("500"));
    assertTrue("Status didn't contain error description: " + captor.getValue(),
        error.getDescription().contains("OH NO"));
    assertTrue("Status didn't contain error text: " + captor.getValue(),
        error.getDescription().contains(errorText));
    assertEquals("Not UNAVAILABLE: " + captor.getValue(),
        Status.UNAVAILABLE.getCode(), error.getCode());
    sock.close();
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
  }

  @Test
  public void proxy_immediateServerClose() throws Exception {
    ServerSocket serverSocket = new ServerSocket(0);
    clientTransport = new OkHttpClientTransport(
        InetSocketAddress.createUnresolved("theservice", 80),
        "authority",
        "userAgent",
        executor,
        null,
        ConnectionSpec.CLEARTEXT,
        DEFAULT_MAX_MESSAGE_SIZE,
        (InetSocketAddress) serverSocket.getLocalSocketAddress(),
        null,
        null,
        tooManyPingsRunnable);
    clientTransport.start(transportListener);

    Socket sock = serverSocket.accept();
    serverSocket.close();
    sock.close();

    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(transportListener, timeout(TIME_OUT_MS)).transportShutdown(captor.capture());
    Status error = captor.getValue();
    assertTrue("Status didn't contain proxy: " + captor.getValue(),
        error.getDescription().contains("proxy"));
    assertEquals("Not UNAVAILABLE: " + captor.getValue(),
        Status.UNAVAILABLE.getCode(), error.getCode());
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
  }

  @Test
  public void goAway_notUtf8() throws Exception {
    initTransport();
    // 0xFF is never permitted in UTF-8. 0xF0 should have 3 continuations following, and 0x0a isn't
    // a continuation.
    frameHandler().goAway(
        0, ErrorCode.ENHANCE_YOUR_CALM, ByteString.of((byte) 0xFF, (byte) 0xF0, (byte) 0x0a));

    shutdownAndVerify();
  }

  @Test
  public void goAway_notTooManyPings() throws Exception {
    final AtomicBoolean run = new AtomicBoolean();
    tooManyPingsRunnable = new Runnable() {
      @Override
      public void run() {
        run.set(true);
      }
    };
    initTransport();
    frameHandler().goAway(0, ErrorCode.ENHANCE_YOUR_CALM, ByteString.encodeUtf8("not_many_pings"));
    assertFalse(run.get());

    shutdownAndVerify();
  }

  @Test
  public void goAway_tooManyPings() throws Exception {
    final AtomicBoolean run = new AtomicBoolean();
    tooManyPingsRunnable = new Runnable() {
      @Override
      public void run() {
        run.set(true);
      }
    };
    initTransport();
    frameHandler().goAway(0, ErrorCode.ENHANCE_YOUR_CALM, ByteString.encodeUtf8("too_many_pings"));
    assertTrue(run.get());

    shutdownAndVerify();
  }

  private int activeStreamCount() {
    return clientTransport.getActiveStreams().length;
  }

  private OkHttpClientStream getStream(int streamId) {
    return clientTransport.getStream(streamId);
  }

  void assertContainStream(int streamId) {
    assertNotNull(clientTransport.getStream(streamId));
  }

  private ClientFrameHandler frameHandler() throws Exception {
    return clientTransport.getHandler();
  }

  private void waitForStreamPending(int expected) throws Exception {
    int duration = TIME_OUT_MS / 10;
    for (int i = 0; i < 10; i++) {
      if (clientTransport.getPendingStreamSize() == expected) {
        return;
      }
      Thread.sleep(duration);
    }
    assertEquals(expected, clientTransport.getPendingStreamSize());
  }

  private void assertNewStreamFail() throws Exception {
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata());
    stream.start(listener);
    listener.waitUntilStreamClosed();
    assertFalse(listener.status.isOk());
  }

  private void setMaxConcurrentStreams(int num) throws Exception {
    Settings settings = new Settings();
    OkHttpSettingsUtil.set(settings, OkHttpSettingsUtil.MAX_CONCURRENT_STREAMS, num);
    frameHandler().settings(false, settings);
  }

  private void setInitialWindowSize(int size) throws Exception {
    Settings settings = new Settings();
    OkHttpSettingsUtil.set(settings, OkHttpSettingsUtil.INITIAL_WINDOW_SIZE, size);
    frameHandler().settings(false, settings);
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
    return ImmutableList.of(
        new Header(":status", "200"),
        CONTENT_TYPE_HEADER);
  }

  private List<Header> grpcResponseTrailers() {
    return ImmutableList.of(
        new Header(Status.CODE_KEY.name(), "0"),
        // Adding Content-Type and :status for testing responses with only a single HEADERS frame.
        new Header(":status", "200"),
        CONTENT_TYPE_HEADER);
  }

  private static List<Header> anyListHeader() {
    return any();
  }

  private static class MockFrameReader implements FrameReader {
    final CountDownLatch closed = new CountDownLatch(1);

    enum Result {
      THROW_EXCEPTION,
      RETURN_FALSE,
      THROW_ERROR
    }

    final LinkedBlockingQueue<Result> nextResults = new LinkedBlockingQueue<Result>();

    @Override
    public void close() throws IOException {
      closed.countDown();
    }

    void assertClosed() {
      try {
        if (!closed.await(TIME_OUT_MS, TimeUnit.MILLISECONDS)) {
          fail("Failed waiting frame reader to be closed.");
        }
      } catch (InterruptedException e) {
        Thread.currentThread().interrupt();
        fail("Interrupted while waiting for frame reader to be closed.");
      }
    }

    // The wait is safe; nextFrame is called in a loop and can have spurious wakeups
    @SuppressWarnings("WaitNotInLoop")
    @Override
    public boolean nextFrame(Handler handler) throws IOException {
      Result result;
      try {
        result = nextResults.take();
      } catch (InterruptedException e) {
        Thread.currentThread().interrupt();
        throw new IOException(e);
      }
      switch (result) {
        case THROW_EXCEPTION:
          throw new IOException(NETWORK_ISSUE_MESSAGE);
        case RETURN_FALSE:
          return false;
        case THROW_ERROR:
          throw new Error(ERROR_MESSAGE);
        default:
          throw new UnsupportedOperationException("unimplemented: " + result);
      }
    }

    void throwIoExceptionForNextFrame() {
      nextResults.add(Result.THROW_EXCEPTION);
    }

    void throwErrorForNextFrame() {
      nextResults.add(Result.THROW_ERROR);
    }

    void nextFrameAtEndOfStream() {
      nextResults.add(Result.RETURN_FALSE);
    }

    @Override
    public void readConnectionPreface() throws IOException {
      // not used.
    }
  }

  private static class MockStreamListener implements ClientStreamListener {
    Status status;
    Metadata headers;
    Metadata trailers;
    CountDownLatch closed = new CountDownLatch(1);
    ArrayList<String> messages = new ArrayList<String>();
    boolean onReadyCalled;

    MockStreamListener() {
    }

    @Override
    public void headersRead(Metadata headers) {
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
    public void closed(Status status, Metadata trailers) {
      this.status = status;
      this.trailers = trailers;
      closed.countDown();
    }

    @Override
    public void onReady() {
      onReadyCalled = true;
    }

    boolean isOnReadyCalled() {
      boolean value = onReadyCalled;
      onReadyCalled = false;
      return value;
    }

    void waitUntilStreamClosed() throws InterruptedException {
      if (!closed.await(TIME_OUT_MS, TimeUnit.MILLISECONDS)) {
        fail("Failed waiting stream to be closed.");
      }
    }

    @SuppressWarnings("Finally") // We don't care about suppressed exceptions in the test
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
          // Ignore
        }
      }
    }
  }

  private static class MockSocket extends Socket {
    MockFrameReader frameReader;

    MockSocket(MockFrameReader frameReader) {
      this.frameReader = frameReader;
    }

    @Override
    public synchronized void close() {
      frameReader.nextFrameAtEndOfStream();
    }
  }

  static class PingCallbackImpl implements ClientTransport.PingCallback {
    int invocationCount;
    long roundTripTime;
    Throwable failureCause;

    @Override
    public void onSuccess(long roundTripTimeNanos) {
      invocationCount++;
      this.roundTripTime = roundTripTimeNanos;
    }

    @Override
    public void onFailure(Throwable cause) {
      invocationCount++;
      this.failureCause = cause;
    }
  }

  private void allowTransportConnected() {
    delayConnectedCallback.allowConnected();
  }

  private void shutdownAndVerify() {
    clientTransport.shutdown();
    assertEquals(0, activeStreamCount());
    try {
      verify(frameWriter, timeout(TIME_OUT_MS)).close();
    } catch (IOException e) {
      throw new RuntimeException(e);

    }
    frameReader.assertClosed();
  }

  private static class DelayConnectedCallback implements Runnable {
    SettableFuture<Void> delayed = SettableFuture.create();

    @Override
    public void run() {
      Futures.getUnchecked(delayed);
    }

    void allowConnected() {
      delayed.set(null);
    }
  }
}
