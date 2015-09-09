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
import static io.grpc.Status.Code.INTERNAL;
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

import com.squareup.okhttp.internal.spdy.ErrorCode;
import com.squareup.okhttp.internal.spdy.FrameReader;
import com.squareup.okhttp.internal.spdy.FrameWriter;
import com.squareup.okhttp.internal.spdy.Header;
import com.squareup.okhttp.internal.spdy.HeadersMode;
import com.squareup.okhttp.internal.spdy.OkHttpSettingsUtil;
import com.squareup.okhttp.internal.spdy.Settings;

import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.Status;
import io.grpc.StatusException;
import io.grpc.internal.AbstractStream;
import io.grpc.internal.ClientStreamListener;
import io.grpc.internal.ClientTransport;
import io.grpc.internal.GrpcUtil;
import io.grpc.okhttp.OkHttpClientTransport.ClientFrameHandler;

import okio.Buffer;

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

import java.io.BufferedReader;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.net.Socket;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;

import javax.annotation.Nullable;

/**
 * Tests for {@link OkHttpClientTransport}.
 */
@RunWith(JUnit4.class)
public class OkHttpClientTransportTest {
  private static final int TIME_OUT_MS = 500;
  private static final String NETWORK_ISSUE_MESSAGE = "network issue";
  // The gRPC header length, which includes 1 byte compression flag and 4 bytes message length.
  private static final int HEADER_LENGTH = 5;

  @Rule
  public Timeout globalTimeout = new Timeout(10 * 1000);

  @Mock
  private FrameWriter frameWriter;
  @Mock
  MethodDescriptor<?, ?> method;
  @Mock
  private ClientTransport.Listener transportListener;
  private OkHttpClientTransport clientTransport;
  private MockFrameReader frameReader;
  private ExecutorService executor;
  private long nanoTime; // backs a ticker, for testing ping round-trip time measurement
  private SettableFuture<Void> connectedFuture;
  private DelayConnectedCallback delayConnectedCallback;

  /** Set up for test. */
  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    executor = Executors.newCachedThreadPool();
    when(method.getFullMethodName()).thenReturn("fakemethod");
    when(method.getType()).thenReturn(MethodType.UNARY);
    when(frameWriter.maxDataLength()).thenReturn(Integer.MAX_VALUE);
    frameReader = new MockFrameReader();
  }

  private void initTransport() throws Exception {
    startTransport(3, null, true, DEFAULT_MAX_MESSAGE_SIZE);
  }

  private void initTransport(int startId) throws Exception {
    startTransport(startId, null, true, DEFAULT_MAX_MESSAGE_SIZE);
  }

  private void initTransportAndDelayConnected() throws Exception {
    delayConnectedCallback = new DelayConnectedCallback();
    startTransport(3, delayConnectedCallback, false, DEFAULT_MAX_MESSAGE_SIZE);
  }

  private void startTransport(int startId, @Nullable Runnable connectingCallback,
      boolean waitingForConnected, int maxMessageSize) throws Exception {
    connectedFuture = SettableFuture.create();
    Ticker ticker = new Ticker() {
      @Override
      public long read() {
        return nanoTime;
      }
    };
    clientTransport = new OkHttpClientTransport(
        executor, frameReader, frameWriter, startId,
        new MockSocket(frameReader), ticker, connectingCallback, connectedFuture,
        maxMessageSize);
    clientTransport.start(transportListener);
    if (waitingForConnected) {
      connectedFuture.get(TIME_OUT_MS, TimeUnit.MILLISECONDS);
    }
  }

  /** Final test checks and clean up. */
  @After
  public void tearDown() throws Exception {
    clientTransport.shutdown();
    assertEquals(0, activeStreamCount());
    verify(frameWriter, timeout(TIME_OUT_MS)).close();
    frameReader.assertClosed();
    executor.shutdown();
  }

  @Test
  public void maxMessageSizeShouldBeEnforced() throws Exception {
    // Allow the response payloads of up to 1 byte.
    startTransport(3, null, true, 1);

    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata(), listener).request(1);
    assertContainStream(3);
    frameHandler().headers(false, false, 3, 0, grpcResponseHeaders(), HeadersMode.HTTP_20_HEADERS);
    assertNotNull(listener.headers);

    // Receive the message.
    final String message = "Hello Client";
    Buffer buffer = createMessageFrame(message);
    frameHandler().data(false, 3, buffer, (int) buffer.size());

    listener.waitUntilStreamClosed();
    assertEquals(INTERNAL, listener.status.getCode());
  }

  /**
   * When nextFrame throws IOException, the transport should be aborted.
   */
  @Test
  public void nextFrameThrowIoException() throws Exception {
    initTransport();
    MockStreamListener listener1 = new MockStreamListener();
    MockStreamListener listener2 = new MockStreamListener();
    clientTransport.newStream(method, new Metadata(), listener1).request(1);
    clientTransport.newStream(method, new Metadata(), listener2).request(1);
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
  }

  @Test
  public void readMessages() throws Exception {
    initTransport();
    final int numMessages = 10;
    final String message = "Hello Client";
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata(), listener).request(numMessages);
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
  }

  @Test
  public void receivedDataForInvalidStreamShouldKillConnection() throws Exception {
    initTransport();
    frameHandler().data(false, 3, createMessageFrame(new String(new char[1000])), 1000);
    verify(frameWriter, timeout(TIME_OUT_MS))
        .goAway(eq(0), eq(ErrorCode.PROTOCOL_ERROR), any(byte[].class));
    verify(transportListener).transportShutdown(isA(Status.class));
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
  }

  @Test
  public void invalidInboundHeadersCancelStream() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata(), listener).request(1);
    assertContainStream(3);
    // Empty headers block without correct content type or status
    frameHandler().headers(false, false, 3, 0, new ArrayList<Header>(),
        HeadersMode.HTTP_20_HEADERS);
    // Now wait to receive 1000 bytes of data so we can have a better error message before
    // cancelling the streaam.
    frameHandler().data(false, 3, createMessageFrame(new String(new char[1000])), 1000);
    verify(frameWriter, timeout(TIME_OUT_MS)).rstStream(eq(3), eq(ErrorCode.CANCEL));
    assertNull(listener.headers);
    assertEquals(Status.INTERNAL.getCode(), listener.status.getCode());
    assertNotNull(listener.trailers);
  }

  @Test
  public void readStatus() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata(), listener);
    assertContainStream(3);
    frameHandler().headers(true, true, 3, 0, grpcResponseTrailers(), HeadersMode.HTTP_20_HEADERS);
    listener.waitUntilStreamClosed();
    assertEquals(Status.Code.OK, listener.status.getCode());
  }

  @Test
  public void receiveReset() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata(), listener);
    assertContainStream(3);
    frameHandler().rstStream(3, ErrorCode.PROTOCOL_ERROR);
    listener.waitUntilStreamClosed();
    assertEquals(OkHttpClientTransport.toGrpcStatus(ErrorCode.PROTOCOL_ERROR), listener.status);
  }

  @Test
  public void cancelStream() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata(), listener);
    getStream(3).cancel(Status.CANCELLED);
    verify(frameWriter, timeout(TIME_OUT_MS)).rstStream(eq(3), eq(ErrorCode.CANCEL));
    listener.waitUntilStreamClosed();
    assertEquals(OkHttpClientTransport.toGrpcStatus(ErrorCode.CANCEL).getCode(),
        listener.status.getCode());
  }

  @Test
  public void headersShouldAddDefaultUserAgent() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata(), listener);
    Header userAgentHeader = new Header(GrpcUtil.USER_AGENT_KEY.name(),
            GrpcUtil.getGrpcUserAgent("okhttp", null));
    List<Header> expectedHeaders = Arrays.asList(SCHEME_HEADER, METHOD_HEADER,
            new Header(Header.TARGET_AUTHORITY, "notarealauthority:80"),
            new Header(Header.TARGET_PATH, "/fakemethod"),
            userAgentHeader, CONTENT_TYPE_HEADER, TE_HEADER);
    verify(frameWriter, timeout(TIME_OUT_MS))
        .synStream(eq(false), eq(false), eq(3), eq(0), eq(expectedHeaders));
    getStream(3).cancel(Status.CANCELLED);
  }

  @Test
  public void headersShouldOverrideDefaultUserAgent() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    String userAgent = "fakeUserAgent";
    Metadata metadata = new Metadata();
    metadata.put(GrpcUtil.USER_AGENT_KEY, userAgent);
    clientTransport.newStream(method, metadata, listener);
    List<Header> expectedHeaders = Arrays.asList(SCHEME_HEADER, METHOD_HEADER,
        new Header(Header.TARGET_AUTHORITY, "notarealauthority:80"),
        new Header(Header.TARGET_PATH, "/fakemethod"),
        new Header(GrpcUtil.USER_AGENT_KEY.name(),
            GrpcUtil.getGrpcUserAgent("okhttp", userAgent)),
        CONTENT_TYPE_HEADER, TE_HEADER);
    verify(frameWriter, timeout(TIME_OUT_MS))
        .synStream(eq(false), eq(false), eq(3), eq(0), eq(expectedHeaders));
    getStream(3).cancel(Status.CANCELLED);
  }

  @Test
  public void cancelStreamForDeadlineExceeded() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata(), listener);
    getStream(3).cancel(Status.DEADLINE_EXCEEDED);
    verify(frameWriter, timeout(TIME_OUT_MS)).rstStream(eq(3), eq(ErrorCode.CANCEL));
    listener.waitUntilStreamClosed();
  }

  @Test
  public void writeMessage() throws Exception {
    initTransport();
    final String message = "Hello Server";
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream =
        clientTransport.newStream(method, new Metadata(), listener);
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
  }

  @Test
  public void windowUpdate() throws Exception {
    initTransport();
    MockStreamListener listener1 = new MockStreamListener();
    MockStreamListener listener2 = new MockStreamListener();
    clientTransport.newStream(method,new Metadata(), listener1).request(2);
    clientTransport.newStream(method,new Metadata(), listener2).request(2);
    assertEquals(2, activeStreamCount());
    OkHttpClientStream stream1 = getStream(3);
    OkHttpClientStream stream2 = getStream(5);

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
  }

  @Test
  public void windowUpdateWithInboundFlowControl() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata(), listener).request(1);

    int messageLength = Utils.DEFAULT_WINDOW_SIZE / 2 + 1;
    byte[] fakeMessage = new byte[messageLength];

    frameHandler().headers(false, false, 3, 0, grpcResponseHeaders(), HeadersMode.HTTP_20_HEADERS);
    Buffer buffer = createMessageFrame(fakeMessage);
    long messageFrameLength = buffer.size();
    frameHandler().data(false, 3, buffer, (int) messageFrameLength);
    verify(frameWriter, timeout(TIME_OUT_MS)).windowUpdate(eq(0), eq(messageFrameLength));
    // We return the bytes for the stream window as we read the message.
    verify(frameWriter, timeout(TIME_OUT_MS)).windowUpdate(eq(3), eq(messageFrameLength));

    getStream(3).cancel(Status.CANCELLED);
    verify(frameWriter, timeout(TIME_OUT_MS)).rstStream(eq(3), eq(ErrorCode.CANCEL));
    listener.waitUntilStreamClosed();
    assertEquals(OkHttpClientTransport.toGrpcStatus(ErrorCode.CANCEL).getCode(),
        listener.status.getCode());
  }

  @Test
  public void outboundFlowControl() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata(), listener);

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
  }

  @Test
  public void outboundFlowControlWithInitialWindowSizeChange() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata(), listener);
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
  }

  @Test
  public void stopNormally() throws Exception {
    initTransport();
    MockStreamListener listener1 = new MockStreamListener();
    MockStreamListener listener2 = new MockStreamListener();
    OkHttpClientStream stream1
        = clientTransport.newStream(method, new Metadata(), listener1);
    OkHttpClientStream stream2
        = clientTransport.newStream(method, new Metadata(), listener2);
    assertEquals(2, activeStreamCount());
    clientTransport.shutdown();
    verify(frameWriter, timeout(TIME_OUT_MS)).goAway(eq(0), eq(ErrorCode.NO_ERROR), (byte[]) any());

    assertEquals(2, activeStreamCount());
    verify(transportListener).transportShutdown(isA(Status.class));

    stream1.cancel(Status.CANCELLED);
    stream2.cancel(Status.CANCELLED);
    listener1.waitUntilStreamClosed();
    listener2.waitUntilStreamClosed();
    assertEquals(0, activeStreamCount());
    assertEquals(Status.CANCELLED.getCode(), listener1.status.getCode());
    assertEquals(Status.CANCELLED.getCode(), listener2.status.getCode());
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
  }

  @Test
  public void receiveGoAway() throws Exception {
    initTransport();
    // start 2 streams.
    MockStreamListener listener1 = new MockStreamListener();
    MockStreamListener listener2 = new MockStreamListener();
    clientTransport.newStream(method,new Metadata(), listener1).request(1);
    clientTransport.newStream(method,new Metadata(), listener2).request(1);
    assertEquals(2, activeStreamCount());

    // Receive goAway, max good id is 3.
    frameHandler().goAway(3, ErrorCode.CANCEL, null);

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
  }

  @Test
  public void streamIdExhausted() throws Exception {
    int startId = Integer.MAX_VALUE - 2;
    initTransport(startId);

    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata(), listener).request(1);

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
  }

  @Test
  public void pendingStreamSucceed() throws Exception {
    initTransport();
    setMaxConcurrentStreams(1);
    final MockStreamListener listener1 = new MockStreamListener();
    final MockStreamListener listener2 = new MockStreamListener();
    OkHttpClientStream stream1
        = clientTransport.newStream(method, new Metadata(), listener1);

    // The second stream should be pending.
    OkHttpClientStream stream2 =
        clientTransport.newStream(method, new Metadata(), listener2);
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
    stream2.sendCancel(Status.CANCELLED);
  }

  @Test
  public void pendingStreamCancelled() throws Exception {
    initTransport();
    setMaxConcurrentStreams(0);
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream
        = clientTransport.newStream(method, new Metadata(), listener);
    waitForStreamPending(1);
    stream.sendCancel(Status.CANCELLED);
    // The second cancel should be an no-op.
    stream.sendCancel(Status.UNKNOWN);
    listener.waitUntilStreamClosed();
    assertEquals(0, clientTransport.getPendingStreamSize());
    assertEquals(Status.CANCELLED.getCode(), listener.status.getCode());
  }

  @Test
  public void pendingStreamFailedByGoAway() throws Exception {
    initTransport();
    setMaxConcurrentStreams(1);
    final MockStreamListener listener1 = new MockStreamListener();
    final MockStreamListener listener2 = new MockStreamListener();
    clientTransport.newStream(method, new Metadata(), listener1);
    // The second stream should be pending.
    clientTransport.newStream(method, new Metadata(), listener2);

    waitForStreamPending(1);
    assertEquals(1, activeStreamCount());

    // Receives GO_AWAY.
    frameHandler().goAway(99, ErrorCode.CANCEL, null);

    listener2.waitUntilStreamClosed();
    assertEquals(Status.CANCELLED.getCode(), listener2.status.getCode());
    assertEquals(0, clientTransport.getPendingStreamSize());

    // active stream should not be affected.
    assertEquals(1, activeStreamCount());
    getStream(3).sendCancel(Status.CANCELLED);
  }

  @Test
  public void pendingStreamFailedByShutdown() throws Exception {
    initTransport();
    setMaxConcurrentStreams(0);
    final MockStreamListener listener = new MockStreamListener();
    // The second stream should be pending.
    clientTransport.newStream(method, new Metadata(), listener);
    waitForStreamPending(1);

    clientTransport.shutdown();

    listener.waitUntilStreamClosed();
    assertEquals(Status.UNAVAILABLE.getCode(), listener.status.getCode());
    assertEquals(0, clientTransport.getPendingStreamSize());
  }

  @Test
  public void pendingStreamFailedByIdExhausted() throws Exception {
    int startId = Integer.MAX_VALUE - 4;
    initTransport(startId);
    setMaxConcurrentStreams(1);

    final MockStreamListener listener1 = new MockStreamListener();
    final MockStreamListener listener2 = new MockStreamListener();
    final MockStreamListener listener3 = new MockStreamListener();

    OkHttpClientStream stream1 =
        clientTransport.newStream(method, new Metadata(), listener1);

    // The second and third stream should be pending.
    clientTransport.newStream(method, new Metadata(), listener2);
    clientTransport.newStream(method, new Metadata(), listener3);

    waitForStreamPending(2);
    assertEquals(1, activeStreamCount());

    // Now finish stream1, stream2 should be started and exhaust the id,
    // so stream3 should be failed.
    stream1.cancel(Status.CANCELLED);
    listener1.waitUntilStreamClosed();
    listener3.waitUntilStreamClosed();
    assertEquals(Status.INTERNAL.getCode(), listener3.status.getCode());
    assertEquals(0, clientTransport.getPendingStreamSize());
    assertEquals(1, activeStreamCount());
    OkHttpClientStream stream2 = getStream(startId + 2);
    stream2.cancel(Status.CANCELLED);
  }

  @Test
  public void receivingWindowExceeded() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata(), listener).request(1);

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
  }

  @Test
  public void unaryHeadersShouldNotBeFlushed() throws Exception {
    // By default the method is a Unary call
    shouldHeadersBeFlushed(false);
  }

  @Test
  public void serverStreamingHeadersShouldNotBeFlushed() throws Exception {
    when(method.getType()).thenReturn(MethodType.SERVER_STREAMING);
    shouldHeadersBeFlushed(false);
  }

  @Test
  public void clientStreamingHeadersShouldBeFlushed() throws Exception {
    when(method.getType()).thenReturn(MethodType.CLIENT_STREAMING);
    shouldHeadersBeFlushed(true);
  }

  @Test
  public void duplexStreamingHeadersShouldNotBeFlushed() throws Exception {
    when(method.getType()).thenReturn(MethodType.BIDI_STREAMING);
    shouldHeadersBeFlushed(true);
  }

  private void shouldHeadersBeFlushed(boolean shouldBeFlushed) throws Exception {
    initTransport();
    OkHttpClientStream stream = clientTransport.newStream(
        method, new Metadata(), new MockStreamListener());
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
    clientTransport.newStream(method,new Metadata(), listener).request(1);
    Buffer buffer = createMessageFrame(new byte[1]);
    frameHandler().data(false, 3, buffer, (int) buffer.size());

    // Trigger the failure by a trailer.
    frameHandler().headers(
        true, true, 3, 0, grpcResponseHeaders(), HeadersMode.HTTP_20_HEADERS);

    listener.waitUntilStreamClosed();
    assertEquals(Status.INTERNAL.getCode(), listener.status.getCode());
    assertTrue(listener.status.getDescription().startsWith("no headers received prior to data"));
    assertEquals(0, listener.messages.size());
  }

  @Test
  public void receiveDataWithoutHeaderAndTrailer() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata(), listener).request(1);
    Buffer buffer = createMessageFrame(new byte[1]);
    frameHandler().data(false, 3, buffer, (int) buffer.size());

    // Trigger the failure by a data frame.
    buffer = createMessageFrame(new byte[1]);
    frameHandler().data(true, 3, buffer, (int) buffer.size());

    listener.waitUntilStreamClosed();
    assertEquals(Status.INTERNAL.getCode(), listener.status.getCode());
    assertTrue(listener.status.getDescription().startsWith("no headers received prior to data"));
    assertEquals(0, listener.messages.size());
  }

  @Test
  public void receiveLongEnoughDataWithoutHeaderAndTrailer() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata(), listener).request(1);
    Buffer buffer = createMessageFrame(new byte[1000]);
    frameHandler().data(false, 3, buffer, (int) buffer.size());

    // Once we receive enough detail, we cancel the stream. so we should have sent cancel.
    verify(frameWriter, timeout(TIME_OUT_MS)).rstStream(eq(3), eq(ErrorCode.CANCEL));

    listener.waitUntilStreamClosed();
    assertEquals(Status.INTERNAL.getCode(), listener.status.getCode());
    assertTrue(listener.status.getDescription().startsWith("no headers received prior to data"));
    assertEquals(0, listener.messages.size());
  }

  @Test
  public void receiveDataForUnknownStreamUpdateConnectionWindow() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata(), listener);
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
  }

  @Test
  public void receiveWindowUpdateForUnknownStream() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata(), listener);
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
  }

  @Test
  public void shouldBeInitiallyReady() throws Exception {
    initTransport();
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(
        method, new Metadata(), listener);
    assertTrue(stream.isReady());
    assertTrue(listener.isOnReadyCalled());
    stream.cancel(Status.CANCELLED);
    assertFalse(stream.isReady());
  }

  @Test
  public void notifyOnReady() throws Exception {
    initTransport();
    // exactly one byte below the threshold
    int messageLength = AbstractStream.DEFAULT_ONREADY_THRESHOLD - HEADER_LENGTH - 1;
    setInitialWindowSize(0);
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(
        method, new Metadata(), listener);
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
  }

  @Test
  public void transportReady() throws Exception {
    initTransport();
    verifyZeroInteractions(transportListener);
    frameHandler().settings(false, new Settings());
    verify(transportListener).transportReady();
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

    nanoTime += TimeUnit.MICROSECONDS.toNanos(10101);

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
  }

  @Test
  public void writeBeforeConnected() throws Exception {
    initTransportAndDelayConnected();
    final String message = "Hello Server";
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata(), listener);
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
  }

  @Test
  public void cancelBeforeConnected() throws Exception {
    initTransportAndDelayConnected();
    final String message = "Hello Server";
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata(), listener);
    InputStream input = new ByteArrayInputStream(message.getBytes(UTF_8));
    stream.writeMessage(input);
    stream.flush();
    stream.cancel(Status.CANCELLED);
    verifyNoMoreInteractions(frameWriter);

    allowTransportConnected();
    verifyNoMoreInteractions(frameWriter);
  }

  @Test
  public void shutdownDuringConnecting() throws Exception {
    initTransportAndDelayConnected();
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata(), listener);

    clientTransport.shutdown();
    allowTransportConnected();

    // The new stream should be failed, as well as the pending stream.
    assertNewStreamFail();
    listener.waitUntilStreamClosed();
    assertEquals(Status.UNAVAILABLE.getCode(), listener.status.getCode());
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
    clientTransport.newStream(method, new Metadata(), listener);
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
    return ImmutableList.<Header>builder()
        .add(new Header(":status", "200"))
        .add(CONTENT_TYPE_HEADER)
        .build();
  }

  private List<Header> grpcResponseTrailers() {
    return ImmutableList.<Header>builder()
        .add(new Header(Status.CODE_KEY.name(), "0"))
        // Adding Content-Type for testing responses with only a single HEADERS frame.
        .add(CONTENT_TYPE_HEADER)
        .build();
  }

  private static class MockFrameReader implements FrameReader {
    CountDownLatch closed = new CountDownLatch(1);
    boolean throwExceptionForNextFrame;
    boolean returnFalseInNextFrame;

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
        fail("Interrupted while waiting for frame reader to be closed.");
      }
    }

    @Override
    public synchronized boolean nextFrame(Handler handler) throws IOException {
      if (throwExceptionForNextFrame) {
        throw new IOException(NETWORK_ISSUE_MESSAGE);
      }
      if (returnFalseInNextFrame) {
        return false;
      }
      try {
        wait();
      } catch (InterruptedException e) {
        throw new IOException(e);
      }
      if (throwExceptionForNextFrame) {
        throw new IOException(NETWORK_ISSUE_MESSAGE);
      }
      return !returnFalseInNextFrame;
    }

    synchronized void throwIoExceptionForNextFrame() {
      throwExceptionForNextFrame = true;
      notifyAll();
    }

    synchronized void nextFrameAtEndOfStream() {
      returnFalseInNextFrame = true;
      notifyAll();
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

  private static class MockSocket extends Socket {
    MockFrameReader frameReader;

    MockSocket(MockFrameReader frameReader) {
      this.frameReader = frameReader;
    }

    @Override
    public void close() {
      frameReader.nextFrameAtEndOfStream();
    }
  }

  static class PingCallbackImpl implements ClientTransport.PingCallback {
    int invocationCount;
    long roundTripTime;
    Throwable failureCause;

    @Override
    public void pingAcknowledged(long roundTripTimeMicros) {
      invocationCount++;
      this.roundTripTime = roundTripTimeMicros;
    }

    @Override
    public void pingFailed(Throwable cause) {
      invocationCount++;
      this.failureCause = cause;
    }
  }

  private void allowTransportConnected() {
    delayConnectedCallback.allowConnected();
  }

  private class DelayConnectedCallback implements Runnable {
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
