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
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyBoolean;
import static org.mockito.Matchers.anyInt;
import static org.mockito.Matchers.eq;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.reset;
import static org.mockito.Mockito.timeout;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.google.common.base.Ticker;
import com.google.common.collect.ImmutableList;
import com.google.common.util.concurrent.MoreExecutors;

import com.squareup.okhttp.internal.spdy.ErrorCode;
import com.squareup.okhttp.internal.spdy.FrameReader;
import com.squareup.okhttp.internal.spdy.Header;
import com.squareup.okhttp.internal.spdy.HeadersMode;
import com.squareup.okhttp.internal.spdy.OkHttpSettingsUtil;
import com.squareup.okhttp.internal.spdy.Settings;

import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodType;
import io.grpc.Status;
import io.grpc.StatusException;
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
  private ClientTransport.Listener transportListener;
  private OkHttpClientTransport clientTransport;
  private MockFrameReader frameReader;
  private MockSocket socket;
  private Map<Integer, OkHttpClientStream> streams;
  private ClientFrameHandler frameHandler;
  private ExecutorService executor;
  private long nanoTime; // backs a ticker, for testing ping round-trip time measurement

  /** Set up for test. */
  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    streams = new HashMap<Integer, OkHttpClientStream>();
    frameReader = new MockFrameReader();
    socket = new MockSocket(frameReader);
    executor = Executors.newCachedThreadPool();
    Ticker ticker = new Ticker() {
      @Override
      public long read() {
        return nanoTime;
      }
    };
    clientTransport = new OkHttpClientTransport(
        executor, frameReader, frameWriter, 3, socket, ticker);
    clientTransport.start(transportListener);
    frameHandler = clientTransport.getHandler();
    streams = clientTransport.getStreams();
    when(method.getName()).thenReturn("fakemethod");
    when(method.getType()).thenReturn(MethodType.UNARY);
    when(frameWriter.maxDataLength()).thenReturn(Integer.MAX_VALUE);
  }

  /** Final test checks and clean up. */
  @After
  public void tearDown() {
    clientTransport.shutdown();
    assertEquals(0, streams.size());
    verify(frameWriter).close();
    frameReader.assertClosed();
    executor.shutdown();
  }

  /**
   * When nextFrame throws IOException, the transport should be aborted.
   */
  @Test
  public void nextFrameThrowIoException() throws Exception {
    MockStreamListener listener1 = new MockStreamListener();
    MockStreamListener listener2 = new MockStreamListener();
    clientTransport.newStream(method, new Metadata.Headers(), listener1).request(1);
    clientTransport.newStream(method, new Metadata.Headers(), listener2).request(1);
    assertEquals(2, streams.size());
    assertTrue(streams.containsKey(3));
    assertTrue(streams.containsKey(5));
    frameReader.throwIoExceptionForNextFrame();
    listener1.waitUntilStreamClosed();
    listener2.waitUntilStreamClosed();
    assertEquals(0, streams.size());
    assertEquals(Status.INTERNAL.getCode(), listener1.status.getCode());
    assertEquals("Protocol error\n" + NETWORK_ISSUE_MESSAGE, listener1.status.getDescription());
    assertEquals(Status.INTERNAL.getCode(), listener2.status.getCode());
    assertEquals("Protocol error\n" + NETWORK_ISSUE_MESSAGE, listener2.status.getDescription());
    verify(transportListener).transportShutdown();
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
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
  public void receivedHeadersForInvalidStreamShouldKillConnection() throws Exception {
    // Empty headers block without correct content type or status
    frameHandler.headers(false, false, 3, 0, new ArrayList<Header>(),
        HeadersMode.HTTP_20_HEADERS);
    verify(frameWriter).goAway(eq(0), eq(ErrorCode.PROTOCOL_ERROR), any(byte[].class));
    verify(transportListener).transportShutdown();
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
  }

  @Test
  public void receivedDataForInvalidStreamShouldKillConnection() throws Exception {
    frameHandler.data(false, 3, createMessageFrame(new String(new char[1000])), 1000);
    verify(frameWriter).goAway(eq(0), eq(ErrorCode.PROTOCOL_ERROR), any(byte[].class));
    verify(transportListener).transportShutdown();
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
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
    clientTransport.newStream(method, new Metadata.Headers(), listener);
    assertTrue(streams.containsKey(3));
    frameHandler.headers(true, true, 3, 0, grpcResponseTrailers(), HeadersMode.HTTP_20_HEADERS);
    listener.waitUntilStreamClosed();
    assertEquals(Status.Code.OK, listener.status.getCode());
  }

  @Test
  public void receiveReset() throws Exception {
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata.Headers(), listener);
    assertTrue(streams.containsKey(3));
    frameHandler.rstStream(3, ErrorCode.PROTOCOL_ERROR);
    listener.waitUntilStreamClosed();
    assertEquals(OkHttpClientTransport.toGrpcStatus(ErrorCode.PROTOCOL_ERROR), listener.status);
  }

  @Test
  public void cancelStream() throws Exception {
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata.Headers(), listener);
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
    clientTransport.newStream(method, new Metadata.Headers(), listener);
    OkHttpClientStream stream = streams.get(3);
    InputStream input = new ByteArrayInputStream(message.getBytes(UTF_8));
    assertEquals(12, input.available());
    stream.writeMessage(input);
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
  public void outboundFlowControl() throws Exception {
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata.Headers(), listener);

    // The first message should be sent out.
    int messageLength = Utils.DEFAULT_WINDOW_SIZE / 2 + 1;
    InputStream input = new ByteArrayInputStream(new byte[messageLength]);
    stream.writeMessage(input);
    stream.flush();
    verify(frameWriter).data(
        eq(false), eq(3), any(Buffer.class), eq(messageLength + HEADER_LENGTH));


    // The second message should be partially sent out.
    input = new ByteArrayInputStream(new byte[messageLength]);
    stream.writeMessage(input);
    stream.flush();
    int partiallySentSize =
        Utils.DEFAULT_WINDOW_SIZE - messageLength - HEADER_LENGTH;
    verify(frameWriter).data(eq(false), eq(3), any(Buffer.class), eq(partiallySentSize));

    // Get more credit, the rest data should be sent out.
    frameHandler.windowUpdate(3, Utils.DEFAULT_WINDOW_SIZE);
    frameHandler.windowUpdate(0, Utils.DEFAULT_WINDOW_SIZE);
    verify(frameWriter).data(
        eq(false), eq(3), any(Buffer.class), eq(messageLength + HEADER_LENGTH - partiallySentSize));

    stream.cancel();
    listener.waitUntilStreamClosed();
  }

  @Test
  public void outboundFlowControlWithInitialWindowSizeChange() throws Exception {
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata.Headers(), listener);
    int messageLength = 20;
    setInitialWindowSize(HEADER_LENGTH + 10);
    InputStream input = new ByteArrayInputStream(new byte[messageLength]);
    stream.writeMessage(input);
    stream.flush();
    // part of the message can be sent.
    verify(frameWriter).data(eq(false), eq(3), any(Buffer.class), eq(HEADER_LENGTH + 10));
    // Avoid connection flow control.
    frameHandler.windowUpdate(0, HEADER_LENGTH + 10);

    // Increase initial window size
    setInitialWindowSize(HEADER_LENGTH + 20);
    // The rest data should be sent.
    verify(frameWriter).data(eq(false), eq(3), any(Buffer.class), eq(10));
    frameHandler.windowUpdate(0, 10);

    // Decrease initial window size to HEADER_LENGTH, since we've already sent
    // out HEADER_LENGTH + 20 bytes data, the window size should be -20 now.
    setInitialWindowSize(HEADER_LENGTH);
    // Get 20 tokens back, still can't send any data.
    frameHandler.windowUpdate(3, 20);
    input = new ByteArrayInputStream(new byte[messageLength]);
    stream.writeMessage(input);
    stream.flush();
    // Only the previous two write operations happened.
    verify(frameWriter, times(2)).data(anyBoolean(), anyInt(), any(Buffer.class), anyInt());

    // Get enough tokens to send the pending message.
    frameHandler.windowUpdate(3, HEADER_LENGTH + 20);
    verify(frameWriter).data(eq(false), eq(3), any(Buffer.class), eq(HEADER_LENGTH + 20));

    stream.cancel();
    listener.waitUntilStreamClosed();
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
    verify(transportListener).transportShutdown();

    stream1.cancel();
    stream2.cancel();
    listener1.waitUntilStreamClosed();
    listener2.waitUntilStreamClosed();
    assertEquals(0, streams.size());
    assertEquals(Status.CANCELLED.getCode(), listener1.status.getCode());
    assertEquals(Status.CANCELLED.getCode(), listener2.status.getCode());
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
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
    verify(transportListener).transportShutdown();
    verify(transportListener, never()).transportTerminated();

    // Stream 2 should be closed.
    listener2.waitUntilStreamClosed();
    assertEquals(1, streams.size());
    assertEquals(Status.CANCELLED.getCode(), listener2.status.getCode());

    // New stream should be failed.
    assertNewStreamFail(clientTransport);

    // But stream 1 should be able to send.
    final String sentMessage = "Should I also go away?";
    OkHttpClientStream stream = streams.get(3);
    InputStream input =
        new ByteArrayInputStream(sentMessage.getBytes(UTF_8));
    assertEquals(22, input.available());
    stream.writeMessage(input);
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
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
  }

  @Test
  public void streamIdExhausted() throws Exception {
    int startId = Integer.MAX_VALUE - 2;
    AsyncFrameWriter writer =  mock(AsyncFrameWriter.class);
    MockFrameReader frameReader = new MockFrameReader();
    OkHttpClientTransport transport = new OkHttpClientTransport(
        executor, frameReader, writer, startId, new MockSocket(frameReader));
    transport.start(transportListener);
    streams = transport.getStreams();

    MockStreamListener listener1 = new MockStreamListener();
    transport.newStream(method, new Metadata.Headers(), listener1);

    assertNewStreamFail(transport);

    streams.get(startId).cancel();
    listener1.waitUntilStreamClosed();
    verify(writer).rstStream(eq(startId), eq(ErrorCode.CANCEL));
    verify(transportListener).transportShutdown();
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
  }

  @Test
  public void pendingStreamSucceed() throws Exception {
    setMaxConcurrentStreams(1);
    final MockStreamListener listener1 = new MockStreamListener();
    final MockStreamListener listener2 = new MockStreamListener();
    OkHttpClientStream stream1
        = clientTransport.newStream(method, new Metadata.Headers(), listener1);

    final CountDownLatch newStreamReturn = new CountDownLatch(1);
    // The second stream should be pending, and the newStream call get blocked.
    new Thread(new Runnable() {
      @Override
      public void run() {
        clientTransport.newStream(method, new Metadata.Headers(), listener2);
        newStreamReturn.countDown();
      }
    }).start();
    waitForStreamPending(1);
    assertEquals(1, streams.size());
    assertEquals(3, (int) stream1.id());

    // Finish the first stream
    stream1.cancel();
    assertTrue("newStream() call is still blocking",
        newStreamReturn.await(TIME_OUT_MS, TimeUnit.MILLISECONDS));
    assertEquals(1, streams.size());
    assertEquals(0, clientTransport.getPendingStreamSize());
    OkHttpClientStream stream2 = streams.get(5);
    assertNotNull(stream2);
    stream2.cancel();
  }

  @Test
  public void pendingStreamFailedByGoAway() throws Exception {
    setMaxConcurrentStreams(0);
    final MockStreamListener listener = new MockStreamListener();
    final CountDownLatch newStreamReturn = new CountDownLatch(1);
    // The second stream should be pending, and the newStream call get blocked.
    new Thread(new Runnable() {
      @Override
      public void run() {
        clientTransport.newStream(method, new Metadata.Headers(), listener);
        newStreamReturn.countDown();
      }
    }).start();
    waitForStreamPending(1);

    frameHandler.goAway(0, ErrorCode.CANCEL, null);

    assertTrue("newStream() call is still blocking",
        newStreamReturn.await(TIME_OUT_MS, TimeUnit.MILLISECONDS));
    listener.waitUntilStreamClosed();
    assertEquals(Status.CANCELLED.getCode(), listener.status.getCode());
    assertEquals(0, clientTransport.getPendingStreamSize());
  }

  @Test
  public void pendingStreamFailedByShutdown() throws Exception {
    setMaxConcurrentStreams(0);
    final MockStreamListener listener = new MockStreamListener();
    final CountDownLatch newStreamReturn = new CountDownLatch(1);
    // The second stream should be pending, and the newStream call get blocked.
    new Thread(new Runnable() {
      @Override
      public void run() {
        clientTransport.newStream(method, new Metadata.Headers(), listener);
        newStreamReturn.countDown();
      }
    }).start();
    waitForStreamPending(1);

    clientTransport.shutdown();

    assertTrue("newStream() call is still blocking",
        newStreamReturn.await(TIME_OUT_MS, TimeUnit.MILLISECONDS));
    listener.waitUntilStreamClosed();
    assertEquals(Status.UNAVAILABLE.getCode(), listener.status.getCode());
    assertEquals(0, clientTransport.getPendingStreamSize());
  }

  @Test
  public void pendingStreamFailedByIdExhausted() throws Exception {
    int startId = Integer.MAX_VALUE - 4;
    clientTransport = new OkHttpClientTransport(
        executor, frameReader, frameWriter, startId, new MockSocket(frameReader));
    clientTransport.start(transportListener);
    frameHandler = clientTransport.getHandler();
    streams = clientTransport.getStreams();
    setMaxConcurrentStreams(1);

    final MockStreamListener listener1 = new MockStreamListener();
    final MockStreamListener listener2 = new MockStreamListener();
    final MockStreamListener listener3 = new MockStreamListener();

    OkHttpClientStream stream1 =
        clientTransport.newStream(method, new Metadata.Headers(), listener1);

    final CountDownLatch newStreamReturn2 = new CountDownLatch(1);
    final CountDownLatch newStreamReturn3 = new CountDownLatch(1);
    // The second and third stream should be pending.
    new Thread(new Runnable() {
      @Override
      public void run() {
        clientTransport.newStream(method, new Metadata.Headers(), listener2);
        newStreamReturn2.countDown();
      }
    }).start();
    // Wait until stream2 is pending, to make sure stream2 is queued in front of stream3.
    waitForStreamPending(1);
    new Thread(new Runnable() {
      @Override
      public void run() {
        clientTransport.newStream(method, new Metadata.Headers(), listener3);
        newStreamReturn3.countDown();
      }
    }).start();

    waitForStreamPending(2);
    assertEquals(1, streams.size());
    assertEquals(startId, (int) stream1.id());

    // Now finish stream1, stream2 should be started and exhaust the id,
    // so stream3 should be failed.
    stream1.cancel();
    assertTrue("newStream() call for stream2 is still blocking",
        newStreamReturn2.await(TIME_OUT_MS, TimeUnit.MILLISECONDS));
    assertTrue("newStream() call for stream3 is still blocking",
        newStreamReturn3.await(TIME_OUT_MS, TimeUnit.MILLISECONDS));
    listener1.waitUntilStreamClosed();
    listener3.waitUntilStreamClosed();
    assertEquals(Status.INTERNAL.getCode(), listener3.status.getCode());
    assertEquals(0, clientTransport.getPendingStreamSize());
    assertEquals(1, streams.size());
    OkHttpClientStream stream2 = streams.get(startId + 2);
    assertNotNull(stream2);
    stream2.cancel();
  }

  @Test
  public void receivingWindowExceeded() throws Exception {
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata.Headers(), listener).request(1);

    frameHandler.headers(false, false, 3, 0, grpcResponseHeaders(), HeadersMode.HTTP_20_HEADERS);

    int messageLength = OkHttpClientTransport.DEFAULT_INITIAL_WINDOW_SIZE + 1;
    byte[] fakeMessage = new byte[messageLength];
    Buffer buffer = createMessageFrame(fakeMessage);
    int messageFrameLength = (int) buffer.size();
    frameHandler.data(false, 3, buffer, messageFrameLength);

    listener.waitUntilStreamClosed();
    assertEquals(Status.INTERNAL.getCode(), listener.status.getCode());
    assertEquals("Received data size exceeded our receiving window size",
        listener.status.getDescription());
    verify(frameWriter).rstStream(eq(3), eq(ErrorCode.FLOW_CONTROL_ERROR));
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
    when(method.getType()).thenReturn(MethodType.DUPLEX_STREAMING);
    shouldHeadersBeFlushed(true);
  }

  private void shouldHeadersBeFlushed(boolean shouldBeFlushed) throws Exception {
    OkHttpClientStream stream = clientTransport.newStream(
        method, new Metadata.Headers(), new MockStreamListener());
    verify(frameWriter).synStream(
        eq(false), eq(false), eq(3), eq(0), Matchers.anyListOf(Header.class));
    if (shouldBeFlushed) {
      verify(frameWriter).flush();
    } else {
      verify(frameWriter, times(0)).flush();
    }
    stream.cancel();
  }

  @Test
  public void receiveDataWithoutHeader() throws Exception {
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method,new Metadata.Headers(), listener).request(1);
    Buffer buffer = createMessageFrame(new byte[1]);
    frameHandler.data(false, 3, buffer, (int) buffer.size());

    // Trigger the failure by a trailer.
    frameHandler.headers(
        true, true, 3, 0, grpcResponseHeaders(), HeadersMode.HTTP_20_HEADERS);

    listener.waitUntilStreamClosed();
    assertEquals(Status.INTERNAL.getCode(), listener.status.getCode());
    assertTrue(listener.status.getDescription().startsWith("no headers received prior to data"));
    assertEquals(0, listener.messages.size());
  }

  @Test
  public void receiveDataWithoutHeaderAndTrailer() throws Exception {
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method, new Metadata.Headers(), listener).request(1);
    Buffer buffer = createMessageFrame(new byte[1]);
    frameHandler.data(false, 3, buffer, (int) buffer.size());

    // Trigger the failure by a data frame.
    buffer = createMessageFrame(new byte[1]);
    frameHandler.data(true, 3, buffer, (int) buffer.size());

    listener.waitUntilStreamClosed();
    assertEquals(Status.INTERNAL.getCode(), listener.status.getCode());
    assertTrue(listener.status.getDescription().startsWith("no headers received prior to data"));
    assertEquals(0, listener.messages.size());
  }

  @Test
  public void receiveLongEnoughDataWithoutHeaderAndTrailer() throws Exception {
    MockStreamListener listener = new MockStreamListener();
    clientTransport.newStream(method,new Metadata.Headers(), listener).request(1);
    Buffer buffer = createMessageFrame(new byte[1000]);
    frameHandler.data(false, 3, buffer, (int) buffer.size());

    // Once we receive enough detail, we cancel the stream. so we should have sent cancel.
    verify(frameWriter).rstStream(eq(3), eq(ErrorCode.CANCEL));

    listener.waitUntilStreamClosed();
    assertEquals(Status.INTERNAL.getCode(), listener.status.getCode());
    assertTrue(listener.status.getDescription().startsWith("no headers received prior to data"));
    assertEquals(0, listener.messages.size());
  }

  @Test
  public void receiveDataForUnknownStreamUpdateConnectionWindow() throws Exception {
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata.Headers(), listener);
    stream.cancel();

    Buffer buffer = createMessageFrame(
        new byte[OkHttpClientTransport.DEFAULT_INITIAL_WINDOW_SIZE / 2 + 1]);
    frameHandler.data(false, 3, buffer, (int) buffer.size());
    // Should still update the connection window even stream 3 is gone.
    verify(frameWriter).windowUpdate(0,
        HEADER_LENGTH + OkHttpClientTransport.DEFAULT_INITIAL_WINDOW_SIZE / 2 + 1);
    buffer = createMessageFrame(
        new byte[OkHttpClientTransport.DEFAULT_INITIAL_WINDOW_SIZE / 2 + 1]);

    // This should kill the connection, since we never created stream 5.
    frameHandler.data(false, 5, buffer, (int) buffer.size());
    verify(frameWriter).goAway(eq(0), eq(ErrorCode.PROTOCOL_ERROR), any(byte[].class));
    verify(transportListener).transportShutdown();
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
  }

  @Test
  public void receiveWindowUpdateForUnknownStream() throws Exception {
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(method, new Metadata.Headers(), listener);
    stream.cancel();
    // This should be ignored.
    frameHandler.windowUpdate(3, 73);
    listener.waitUntilStreamClosed();
    // This should kill the connection, since we never created stream 5.
    frameHandler.windowUpdate(5, 73);
    verify(frameWriter).goAway(eq(0), eq(ErrorCode.PROTOCOL_ERROR), any(byte[].class));
    verify(transportListener).transportShutdown();
    verify(transportListener, timeout(TIME_OUT_MS)).transportTerminated();
  }

  @Test
  public void shouldBeInitiallyReady() throws Exception {
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(
        method,new Metadata.Headers(), listener);
    assertTrue(stream.isReady());
    assertTrue(listener.isOnReadyCalled());
    stream.cancel();
    assertFalse(stream.isReady());
  }

  @Test
  public void notifyOnReady() throws Exception {
    final int messageLength = 15;
    setInitialWindowSize(0);
    MockStreamListener listener = new MockStreamListener();
    OkHttpClientStream stream = clientTransport.newStream(
        method,new Metadata.Headers(), listener);
    stream.setOnReadyThreshold(HEADER_LENGTH + 20);
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
    frameHandler.windowUpdate(0, HEADER_LENGTH + messageLength);
    frameHandler.windowUpdate(3, HEADER_LENGTH + messageLength);
    assertFalse(stream.isReady());
    assertFalse(listener.isOnReadyCalled());

    // Let the second message out.
    frameHandler.windowUpdate(0, HEADER_LENGTH + messageLength);
    frameHandler.windowUpdate(3, HEADER_LENGTH + messageLength);
    assertTrue(stream.isReady());
    assertTrue(listener.isOnReadyCalled());

    // Now the first message is still in the queue, and it's size is smaller than the threshold.
    // Increase the threshold should have no affection.
    stream.setOnReadyThreshold(messageLength * 10);
    assertFalse(listener.isOnReadyCalled());
    // Decrease the threshold should have no affection too.
    stream.setOnReadyThreshold(HEADER_LENGTH);
    assertFalse(listener.isOnReadyCalled());
    // But now increase the threshold to larger than the queued message size, onReady should be
    // triggered.
    stream.setOnReadyThreshold(HEADER_LENGTH + messageLength + 1);
    assertTrue(listener.isOnReadyCalled());

    stream.cancel();
  }

  @Test
  public void ping() throws Exception {
    PingCallbackImpl callback1 = new PingCallbackImpl();
    clientTransport.ping(callback1, MoreExecutors.directExecutor());
    // add'l ping will be added as listener to outstanding operation
    PingCallbackImpl callback2 = new PingCallbackImpl();
    clientTransport.ping(callback2, MoreExecutors.directExecutor());

    ArgumentCaptor<Integer> captor1 = ArgumentCaptor.forClass(int.class);
    ArgumentCaptor<Integer> captor2 = ArgumentCaptor.forClass(int.class);
    verify(frameWriter).ping(eq(false), captor1.capture(), captor2.capture());
    // callback not invoked until we see acknowledgement
    assertEquals(0, callback1.invocationCount);
    assertEquals(0, callback2.invocationCount);

    int payload1 = captor1.getValue();
    int payload2 = captor2.getValue();
    // getting a bad ack won't complete the future
    // to make the ack "bad", we modify the payload so it doesn't match
    frameHandler.ping(true, payload1, payload2 - 1);
    // operation not complete because ack was wrong
    assertEquals(0, callback1.invocationCount);
    assertEquals(0, callback2.invocationCount);

    nanoTime += TimeUnit.MICROSECONDS.toNanos(10101);

    // reading the proper response should complete the future
    frameHandler.ping(true, payload1, payload2);
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
    PingCallbackImpl callback = new PingCallbackImpl();
    clientTransport.ping(callback, MoreExecutors.directExecutor());
    assertEquals(0, callback.invocationCount);

    clientTransport.onIoException(new IOException());
    // ping failed on error
    assertEquals(1, callback.invocationCount);
    assertTrue(callback.failureCause instanceof StatusException);
    assertEquals(Status.Code.INTERNAL,
        ((StatusException) callback.failureCause).getStatus().getCode());

    // now that handler is in terminal state, all future pings fail immediately
    callback = new PingCallbackImpl();
    clientTransport.ping(callback, MoreExecutors.directExecutor());
    assertEquals(1, callback.invocationCount);
    assertTrue(callback.failureCause instanceof StatusException);
    assertEquals(Status.Code.INTERNAL,
        ((StatusException) callback.failureCause).getStatus().getCode());
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

  private void assertNewStreamFail(OkHttpClientTransport transport) throws Exception {
    MockStreamListener listener = new MockStreamListener();
    transport.newStream(method, new Metadata.Headers(), listener);
    listener.waitUntilStreamClosed();
    assertFalse(listener.status.isOk());
  }

  private void setMaxConcurrentStreams(int num) {
    Settings settings = new Settings();
    OkHttpSettingsUtil.set(settings, OkHttpSettingsUtil.MAX_CONCURRENT_STREAMS, num);
    frameHandler.settings(false, settings);
  }

  private void setInitialWindowSize(int size) {
    Settings settings = new Settings();
    OkHttpSettingsUtil.set(settings, OkHttpSettingsUtil.INITIAL_WINDOW_SIZE, size);
    frameHandler.settings(false, settings);
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
    Metadata.Headers headers;
    Metadata.Trailers trailers;
    CountDownLatch closed = new CountDownLatch(1);
    ArrayList<String> messages = new ArrayList<String>();
    boolean onReadyCalled;

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
}
