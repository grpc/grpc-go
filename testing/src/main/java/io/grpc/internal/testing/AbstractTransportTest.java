/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc.internal.testing;

import static com.google.common.base.Charsets.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotEquals;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.junit.Assume.assumeTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.inOrder;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.reset;
import static org.mockito.Mockito.timeout;
import static org.mockito.Mockito.verify;

import com.google.common.collect.Lists;
import com.google.common.io.ByteStreams;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.SettableFuture;

import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.internal.ClientStream;
import io.grpc.internal.ClientStreamListener;
import io.grpc.internal.ClientTransport;
import io.grpc.internal.ManagedClientTransport;
import io.grpc.internal.Server;
import io.grpc.internal.ServerListener;
import io.grpc.internal.ServerStream;
import io.grpc.internal.ServerStreamListener;
import io.grpc.internal.ServerTransport;
import io.grpc.internal.ServerTransportListener;

import org.junit.After;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.InOrder;
import org.mockito.Matchers;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.Future;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

/** Standard unit tests for {@link ClientTransport}s and {@link ServerTransport}s. */
@RunWith(JUnit4.class)
public abstract class AbstractTransportTest {
  private static int TIMEOUT_MS = 1000;

  /**
   * Returns a new server that when started will be able to be connected to from the client. Each
   * returned instance should be new and yet be accessible by new client transports. This
   * effectively means that each instance should listen on the same port, or similar.
   */
  protected abstract Server newServer();

  /**
   * Returns a new transport that when started will be able to connect to the server.
   */
  protected abstract ManagedClientTransport newClientTransport();

  /**
   * When non-null, will be shut down during tearDown(). However, it _must_ have been started with
   * {@code serverListener}, otherwise tearDown() can't wait for shutdown which can put following
   * tests in an indeterminate state.
   */
  private Server server;
  private ServerTransport serverTransport;
  private ManagedClientTransport client;
  private MethodDescriptor<String, String> methodDescriptor = MethodDescriptor.create(
      MethodDescriptor.MethodType.UNKNOWN, "service/method", StringMarshaller.INSTANCE,
      StringMarshaller.INSTANCE);
  private Metadata.Key<String> asciiKey = Metadata.Key.of(
      "ascii-key", Metadata.ASCII_STRING_MARSHALLER);
  private Metadata.Key<String> binaryKey = Metadata.Key.of(
      "key-bin", StringBinaryMarshaller.INSTANCE);

  private ManagedClientTransport.Listener mockClientTransportListener
      = mock(ManagedClientTransport.Listener.class);
  private ClientStreamListener mockClientStreamListener = mock(ClientStreamListener.class);
  private MockServerListener serverListener = new MockServerListener();
  private ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
  private ArgumentCaptor<Throwable> throwableCaptor = ArgumentCaptor.forClass(Throwable.class);
  private ArgumentCaptor<Metadata> metadataCaptor = ArgumentCaptor.forClass(Metadata.class);
  private ArgumentCaptor<InputStream> inputStreamCaptor
      = ArgumentCaptor.forClass(InputStream.class);
  @Rule
  public ExpectedException thrown = ExpectedException.none();

  @Before
  public void setUp() {
    server = newServer();
    client = newClientTransport();
  }

  @After
  public void tearDown() throws InterruptedException {
    if (client != null) {
      // TODO(ejona): swap to shutdownNow
      client.shutdown();
    }
    if (serverTransport != null) {
      // TODO(ejona): swap to shutdownNow
      serverTransport.shutdown();
    }
    if (server != null) {
      server.shutdown();
      assertTrue(serverListener.waitForShutdown(TIMEOUT_MS, TimeUnit.MILLISECONDS));
    }
  }

  // TODO(ejona):
  //   multiple streams on same transport
  //   multiple client transports to same server
  //   halfClose to trigger flush (client and server)
  //   flow control pushes back (client and server)
  //   flow control provides precisely number of messages requested (client and server)
  //   onReady called when buffer drained (on server and client)
  //   test no start reentrancy (esp. during failure) (transport and call)
  //   multiple requests/responses (verifying contents received)
  //   server transport shutdown triggers client shutdown (via GOAWAY)
  //   queued message InputStreams are closed on stream cancel
  //     (and maybe exceptions handled)

  @Test
  public void serverNotListening() {
    server = null;
    InOrder inOrder = inOrder(mockClientTransportListener);
    client.start(mockClientTransportListener);
    verify(mockClientTransportListener, timeout(TIMEOUT_MS)).transportTerminated();
    inOrder.verify(mockClientTransportListener).transportShutdown(statusCaptor.capture());
    assertCodeEquals(Status.UNAVAILABLE, statusCaptor.getValue());
    inOrder.verify(mockClientTransportListener).transportTerminated();
    verify(mockClientTransportListener, never()).transportReady();
  }

  @Test
  public void clientStartStop() throws Exception {
    server.start(serverListener);
    InOrder inOrder = inOrder(mockClientTransportListener);
    client.start(mockClientTransportListener);
    client.shutdown();
    verify(mockClientTransportListener, timeout(TIMEOUT_MS)).transportTerminated();
    inOrder.verify(mockClientTransportListener).transportShutdown(any(Status.class));
    inOrder.verify(mockClientTransportListener).transportTerminated();
  }

  @Test
  public void clientStartAndStopOnceConnected() throws Exception {
    server.start(serverListener);
    InOrder inOrder = inOrder(mockClientTransportListener);
    client.start(mockClientTransportListener);
    verify(mockClientTransportListener, timeout(TIMEOUT_MS)).transportReady();
    MockServerTransportListener serverTransportListener
        = serverListener.takeListenerOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    client.shutdown();
    verify(mockClientTransportListener, timeout(TIMEOUT_MS)).transportTerminated();
    inOrder.verify(mockClientTransportListener).transportShutdown(any(Status.class));
    inOrder.verify(mockClientTransportListener).transportTerminated();
    assertTrue(serverTransportListener.waitForTermination(TIMEOUT_MS, TimeUnit.MILLISECONDS));
    server.shutdown();
    assertTrue(serverListener.waitForShutdown(TIMEOUT_MS, TimeUnit.MILLISECONDS));
    server = null;
  }

  @Test
  public void serverAlreadyListening() throws Exception {
    client = null;
    server.start(serverListener);
    Server server2 = newServer();
    thrown.expect(IOException.class);
    server2.start(new MockServerListener());
  }

  @Test
  public void openStreamPreventsTermination() throws Exception {
    server.start(serverListener);
    client.start(mockClientTransportListener);
    MockServerTransportListener serverTransportListener
        = serverListener.takeListenerOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    serverTransport = serverTransportListener.transport;

    ClientStream clientStream = client.newStream(methodDescriptor, new Metadata());
    clientStream.start(mockClientStreamListener);
    StreamCreation serverStreamCreation
        = serverTransportListener.takeStreamOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    ServerStream serverStream = serverStreamCreation.stream;
    ServerStreamListener mockServerStreamListener = serverStreamCreation.listener;

    client.shutdown();
    client = null;
    server.shutdown();
    server = null;
    serverTransport.shutdown();
    serverTransport = null;

    verify(mockClientTransportListener, timeout(TIMEOUT_MS)).transportShutdown(any(Status.class));
    assertTrue(serverListener.waitForShutdown(TIMEOUT_MS, TimeUnit.MILLISECONDS));

    // A new server should be able to start listening, since the current server has given up
    // resources. There may be cases this is impossible in the future, but for now it is a useful
    // property.
    serverListener = new MockServerListener();
    server = newServer();
    server.start(serverListener);

    // Try to "flush" out any listener notifications on client and server. This also ensures that
    // the stream still functions.
    serverStream.writeHeaders(new Metadata());
    clientStream.halfClose();
    verify(mockClientStreamListener, timeout(TIMEOUT_MS)).headersRead(any(Metadata.class));
    verify(mockServerStreamListener, timeout(TIMEOUT_MS)).halfClosed();

    verify(mockClientTransportListener, never()).transportTerminated();
    assertFalse(serverTransportListener.isTerminated());

    clientStream.cancel(Status.CANCELLED);

    verify(mockClientTransportListener, timeout(TIMEOUT_MS)).transportTerminated();
    assertTrue(serverTransportListener.waitForTermination(TIMEOUT_MS, TimeUnit.MILLISECONDS));
  }

  @Test
  public void ping() throws Exception {
    server.start(serverListener);
    client.start(mockClientTransportListener);
    ClientTransport.PingCallback mockPingCallback = mock(ClientTransport.PingCallback.class);
    try {
      client.ping(mockPingCallback, MoreExecutors.directExecutor());
    } catch (UnsupportedOperationException ex) {
      // Transport doesn't support ping, so this neither passes nor fails.
      assumeTrue(false);
    }
    verify(mockPingCallback, timeout(TIMEOUT_MS)).onSuccess(Matchers.anyInt());
  }

  @Test
  public void ping_duringShutdown() throws Exception {
    server.start(serverListener);
    client.start(mockClientTransportListener);
    // Stream prevents termination
    ClientStream stream = client.newStream(methodDescriptor, new Metadata());
    stream.start(mockClientStreamListener);
    client.shutdown();
    ClientTransport.PingCallback mockPingCallback = mock(ClientTransport.PingCallback.class);
    try {
      client.ping(mockPingCallback, MoreExecutors.directExecutor());
    } catch (UnsupportedOperationException ex) {
      // Transport doesn't support ping, so this neither passes nor fails.
      assumeTrue(false);
    }
    verify(mockPingCallback, timeout(TIMEOUT_MS)).onSuccess(Matchers.anyInt());
    stream.cancel(Status.CANCELLED);
  }

  @Test
  public void ping_afterTermination() throws Exception {
    server.start(serverListener);
    client.start(mockClientTransportListener);
    client.shutdown();
    ClientTransport.PingCallback mockPingCallback = mock(ClientTransport.PingCallback.class);
    try {
      client.ping(mockPingCallback, MoreExecutors.directExecutor());
    } catch (UnsupportedOperationException ex) {
      // Transport doesn't support ping, so this neither passes nor fails.
      assumeTrue(false);
    }
    verify(mockPingCallback, timeout(TIMEOUT_MS)).onFailure(throwableCaptor.capture());
    Status status = Status.fromThrowable(throwableCaptor.getValue());
    assertCodeEquals(Status.UNAVAILABLE, status);
  }

  @Test
  public void newStream_duringShutdown() throws Exception {
    server.start(serverListener);
    client.start(mockClientTransportListener);
    // Stream prevents termination
    ClientStream stream = client.newStream(methodDescriptor, new Metadata());
    stream.start(mockClientStreamListener);
    client.shutdown();
    ClientStream stream2 = client.newStream(methodDescriptor, new Metadata());
    ClientStreamListener mockClientStreamListener2 = mock(ClientStreamListener.class);
    stream2.start(mockClientStreamListener2);
    verify(mockClientStreamListener2, timeout(TIMEOUT_MS))
        .closed(statusCaptor.capture(), any(Metadata.class));
    assertCodeEquals(Status.UNAVAILABLE, statusCaptor.getValue());

    // Make sure earlier stream works.
    MockServerTransportListener serverTransportListener
        = serverListener.takeListenerOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    serverTransport = serverTransportListener.transport;
    StreamCreation serverStreamCreation
        = serverTransportListener.takeStreamOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    serverStreamCreation.stream.close(Status.OK, new Metadata());
    verify(mockClientStreamListener, timeout(TIMEOUT_MS))
        .closed(statusCaptor.capture(), any(Metadata.class));
    assertCodeEquals(Status.OK, statusCaptor.getValue());
  }

  @Test
  public void basicStream() throws Exception {
    server.start(serverListener);
    client.start(mockClientTransportListener);
    MockServerTransportListener serverTransportListener
        = serverListener.takeListenerOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    serverTransport = serverTransportListener.transport;

    Metadata clientHeaders = new Metadata();
    clientHeaders.put(asciiKey, "client");
    clientHeaders.put(asciiKey, "dupvalue");
    clientHeaders.put(asciiKey, "dupvalue");
    clientHeaders.put(binaryKey, "äbinaryclient");
    ClientStream clientStream = client.newStream(methodDescriptor, clientHeaders);
    clientStream.start(mockClientStreamListener);
    StreamCreation serverStreamCreation
        = serverTransportListener.takeStreamOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    assertEquals(methodDescriptor.getFullMethodName(), serverStreamCreation.method);
    assertEquals(Lists.newArrayList(clientHeaders.getAll(asciiKey)),
        Lists.newArrayList(serverStreamCreation.headers.getAll(asciiKey)));
    assertEquals(Lists.newArrayList(clientHeaders.getAll(binaryKey)),
        Lists.newArrayList(serverStreamCreation.headers.getAll(binaryKey)));
    ServerStream serverStream = serverStreamCreation.stream;
    ServerStreamListener mockServerStreamListener = serverStreamCreation.listener;

    serverStream.request(1);
    verify(mockClientStreamListener, timeout(TIMEOUT_MS)).onReady();
    assertTrue(clientStream.isReady());
    clientStream.writeMessage(methodDescriptor.streamRequest("Hello!"));
    clientStream.flush();
    verify(mockServerStreamListener, timeout(TIMEOUT_MS)).messageRead(inputStreamCaptor.capture());
    assertEquals("Hello!", methodDescriptor.parseRequest(inputStreamCaptor.getValue()));
    inputStreamCaptor.getValue().close();

    clientStream.halfClose();
    verify(mockServerStreamListener, timeout(TIMEOUT_MS)).halfClosed();

    Metadata serverHeaders = new Metadata();
    serverHeaders.put(asciiKey, "server");
    serverHeaders.put(asciiKey, "dupvalue");
    serverHeaders.put(asciiKey, "dupvalue");
    serverHeaders.put(binaryKey, "äbinaryserver");
    serverStream.writeHeaders(serverHeaders);
    verify(mockClientStreamListener, timeout(TIMEOUT_MS)).headersRead(metadataCaptor.capture());
    assertEquals(Lists.newArrayList(serverHeaders.getAll(asciiKey)),
        Lists.newArrayList(metadataCaptor.getValue().getAll(asciiKey)));
    assertEquals(Lists.newArrayList(serverHeaders.getAll(binaryKey)),
        Lists.newArrayList(metadataCaptor.getValue().getAll(binaryKey)));

    clientStream.request(1);
    verify(mockServerStreamListener, timeout(TIMEOUT_MS)).onReady();
    assertTrue(serverStream.isReady());
    serverStream.writeMessage(methodDescriptor.streamResponse("Hi. Who are you?"));
    serverStream.flush();
    verify(mockClientStreamListener, timeout(TIMEOUT_MS)).messageRead(inputStreamCaptor.capture());
    assertEquals("Hi. Who are you?", methodDescriptor.parseResponse(inputStreamCaptor.getValue()));
    inputStreamCaptor.getValue().close();

    Status status = Status.OK.withDescription("That was normal");
    Metadata trailers = new Metadata();
    trailers.put(asciiKey, "trailers");
    trailers.put(asciiKey, "dupvalue");
    trailers.put(asciiKey, "dupvalue");
    trailers.put(binaryKey, "äbinarytrailers");
    serverStream.close(status, trailers);
    verify(mockServerStreamListener, timeout(TIMEOUT_MS)).closed(statusCaptor.capture());
    assertCodeEquals(Status.OK, statusCaptor.getValue());
    verify(mockClientStreamListener, timeout(TIMEOUT_MS))
        .closed(statusCaptor.capture(), metadataCaptor.capture());
    assertEquals(status.getCode(), statusCaptor.getValue().getCode());
    assertEquals(status.getDescription(), statusCaptor.getValue().getDescription());
    assertEquals(Lists.newArrayList(trailers.getAll(asciiKey)),
        Lists.newArrayList(metadataCaptor.getValue().getAll(asciiKey)));
    assertEquals(Lists.newArrayList(trailers.getAll(binaryKey)),
        Lists.newArrayList(metadataCaptor.getValue().getAll(binaryKey)));
  }

  @Test
  public void zeroMessageStream() throws Exception {
    server.start(serverListener);
    client.start(mockClientTransportListener);
    MockServerTransportListener serverTransportListener
        = serverListener.takeListenerOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    serverTransport = serverTransportListener.transport;

    ClientStream clientStream = client.newStream(methodDescriptor, new Metadata());
    clientStream.start(mockClientStreamListener);
    StreamCreation serverStreamCreation
        = serverTransportListener.takeStreamOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    ServerStream serverStream = serverStreamCreation.stream;
    ServerStreamListener mockServerStreamListener = serverStreamCreation.listener;

    clientStream.halfClose();
    verify(mockServerStreamListener, timeout(TIMEOUT_MS)).halfClosed();

    serverStream.writeHeaders(new Metadata());
    verify(mockClientStreamListener, timeout(TIMEOUT_MS)).headersRead(any(Metadata.class));

    Status status = Status.OK.withDescription("Nice talking to you");
    serverStream.close(status, new Metadata());
    verify(mockServerStreamListener, timeout(TIMEOUT_MS)).closed(statusCaptor.capture());
    assertCodeEquals(Status.OK, statusCaptor.getValue());
    verify(mockClientStreamListener, timeout(TIMEOUT_MS))
        .closed(statusCaptor.capture(), any(Metadata.class));
    assertEquals(status.getCode(), statusCaptor.getValue().getCode());
    assertEquals(status.getDescription(), statusCaptor.getValue().getDescription());
  }

  @Test
  public void earlyServerClose_withServerHeaders() throws Exception {
    server.start(serverListener);
    client.start(mockClientTransportListener);
    MockServerTransportListener serverTransportListener
        = serverListener.takeListenerOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    serverTransport = serverTransportListener.transport;

    ClientStream clientStream = client.newStream(methodDescriptor, new Metadata());
    clientStream.start(mockClientStreamListener);
    StreamCreation serverStreamCreation
        = serverTransportListener.takeStreamOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    ServerStream serverStream = serverStreamCreation.stream;
    ServerStreamListener mockServerStreamListener = serverStreamCreation.listener;

    serverStream.writeHeaders(new Metadata());
    verify(mockClientStreamListener, timeout(TIMEOUT_MS)).headersRead(any(Metadata.class));

    Status status = Status.OK.withDescription("Hello. Goodbye.");
    serverStream.close(status, new Metadata());
    verify(mockServerStreamListener, timeout(TIMEOUT_MS)).closed(statusCaptor.capture());
    assertCodeEquals(Status.OK, statusCaptor.getValue());
    verify(mockClientStreamListener, timeout(TIMEOUT_MS))
        .closed(statusCaptor.capture(), any(Metadata.class));
    assertEquals(status.getCode(), statusCaptor.getValue().getCode());
  }

  @Test
  public void earlyServerClose_noServerHeaders() throws Exception {
    server.start(serverListener);
    client.start(mockClientTransportListener);
    MockServerTransportListener serverTransportListener
        = serverListener.takeListenerOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    serverTransport = serverTransportListener.transport;

    ClientStream clientStream = client.newStream(methodDescriptor, new Metadata());
    clientStream.start(mockClientStreamListener);
    StreamCreation serverStreamCreation
        = serverTransportListener.takeStreamOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    ServerStream serverStream = serverStreamCreation.stream;
    ServerStreamListener mockServerStreamListener = serverStreamCreation.listener;

    Status status = Status.OK.withDescription("Hellogoodbye");
    Metadata trailers = new Metadata();
    trailers.put(asciiKey, "trailers");
    trailers.put(asciiKey, "dupvalue");
    trailers.put(asciiKey, "dupvalue");
    trailers.put(binaryKey, "äbinarytrailers");
    serverStream.close(status, trailers);
    verify(mockServerStreamListener, timeout(TIMEOUT_MS)).closed(statusCaptor.capture());
    assertCodeEquals(Status.OK, statusCaptor.getValue());
    verify(mockClientStreamListener, timeout(TIMEOUT_MS))
        .closed(statusCaptor.capture(), metadataCaptor.capture());
    assertEquals(status.getCode(), statusCaptor.getValue().getCode());
    assertEquals(Lists.newArrayList(trailers.getAll(asciiKey)),
        Lists.newArrayList(metadataCaptor.getValue().getAll(asciiKey)));
    assertEquals(Lists.newArrayList(trailers.getAll(binaryKey)),
        Lists.newArrayList(metadataCaptor.getValue().getAll(binaryKey)));
  }

  @Test
  public void earlyServerClose_serverFailure() throws Exception {
    server.start(serverListener);
    client.start(mockClientTransportListener);
    MockServerTransportListener serverTransportListener
        = serverListener.takeListenerOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    serverTransport = serverTransportListener.transport;

    ClientStream clientStream = client.newStream(methodDescriptor, new Metadata());
    clientStream.start(mockClientStreamListener);
    StreamCreation serverStreamCreation
        = serverTransportListener.takeStreamOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    ServerStream serverStream = serverStreamCreation.stream;
    ServerStreamListener mockServerStreamListener = serverStreamCreation.listener;

    Status status = Status.INVALID_ARGUMENT.withDescription("I'm not listening");
    serverStream.close(status, new Metadata());
    verify(mockServerStreamListener, timeout(TIMEOUT_MS)).closed(statusCaptor.capture());
    assertCodeEquals(Status.OK, statusCaptor.getValue());
    verify(mockClientStreamListener, timeout(TIMEOUT_MS))
        .closed(statusCaptor.capture(), any(Metadata.class));
    assertEquals(status.getCode(), statusCaptor.getValue().getCode());
    assertEquals(status.getDescription(), statusCaptor.getValue().getDescription());
  }

  @Test
  public void clientCancel() throws Exception {
    server.start(serverListener);
    client.start(mockClientTransportListener);
    MockServerTransportListener serverTransportListener
        = serverListener.takeListenerOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    serverTransport = serverTransportListener.transport;

    ClientStream clientStream = client.newStream(methodDescriptor, new Metadata());
    clientStream.start(mockClientStreamListener);
    StreamCreation serverStreamCreation
        = serverTransportListener.takeStreamOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    ServerStream serverStream = serverStreamCreation.stream;
    ServerStreamListener mockServerStreamListener = serverStreamCreation.listener;

    Status status = Status.CANCELLED.withDescription("Nevermind");
    clientStream.cancel(status);
    verify(mockClientStreamListener, timeout(TIMEOUT_MS))
        .closed(Matchers.same(status), any(Metadata.class));
    verify(mockServerStreamListener, timeout(TIMEOUT_MS)).closed(statusCaptor.capture());
    assertNotEquals(Status.Code.OK, statusCaptor.getValue().getCode());

    reset(mockServerStreamListener);
    reset(mockClientStreamListener);
    clientStream.cancel(status);
    verify(mockServerStreamListener, never()).closed(any(Status.class));
    verify(mockClientStreamListener, never()).closed(any(Status.class), any(Metadata.class));
  }

  @Test
  public void serverCancel() throws Exception {
    server.start(serverListener);
    client.start(mockClientTransportListener);
    MockServerTransportListener serverTransportListener
        = serverListener.takeListenerOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    serverTransport = serverTransportListener.transport;

    ClientStream clientStream = client.newStream(methodDescriptor, new Metadata());
    clientStream.start(mockClientStreamListener);
    StreamCreation serverStreamCreation
        = serverTransportListener.takeStreamOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    ServerStream serverStream = serverStreamCreation.stream;
    ServerStreamListener mockServerStreamListener = serverStreamCreation.listener;

    Status status = Status.DEADLINE_EXCEEDED.withDescription("It was bound to happen");
    serverStream.cancel(status);
    verify(mockServerStreamListener, timeout(TIMEOUT_MS)).closed(Matchers.same(status));
    verify(mockClientStreamListener, timeout(TIMEOUT_MS))
        .closed(statusCaptor.capture(), any(Metadata.class));
    // Presently we can't sent much back to the client in this case. Verify that is the current
    // behavior for consistency between transports.
    assertCodeEquals(Status.CANCELLED, statusCaptor.getValue());

    // Second cancellation shouldn't trigger additional callbacks
    reset(mockServerStreamListener);
    reset(mockClientStreamListener);
    serverStream.cancel(status);
    doPingPong(serverListener);
    verify(mockServerStreamListener, never()).closed(any(Status.class));
    verify(mockClientStreamListener, never()).closed(any(Status.class), any(Metadata.class));
  }

  @Test
  public void flowControlPushBack() throws Exception {
    server.start(serverListener);
    client.start(mockClientTransportListener);
    MockServerTransportListener serverTransportListener
        = serverListener.takeListenerOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    serverTransport = serverTransportListener.transport;

    ClientStream clientStream = client.newStream(methodDescriptor, new Metadata());
    clientStream.start(mockClientStreamListener);
    StreamCreation serverStreamCreation
        = serverTransportListener.takeStreamOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    assertEquals(methodDescriptor.getFullMethodName(), serverStreamCreation.method);
    ServerStream serverStream = serverStreamCreation.stream;
    ServerStreamListener mockServerStreamListener = serverStreamCreation.listener;
    serverStream.writeHeaders(new Metadata());

    Answer<Void> closeStream = new Answer<Void>() {
      @Override
      public Void answer(InvocationOnMock invocation) throws Exception {
        Object[] args = invocation.getArguments();
        ((InputStream) args[0]).close();
        return null;
      }
    };

    String largeMessage;
    {
      int size = 1 * 1024;
      StringBuffer sb = new StringBuffer(size);
      for (int i = 0; i < size; i++) {
        sb.append('a');
      }
      largeMessage = sb.toString();
    }

    doAnswer(closeStream).when(mockServerStreamListener).messageRead(any(InputStream.class));
    serverStream.request(1);
    verify(mockClientStreamListener, timeout(TIMEOUT_MS)).onReady();
    assertTrue(clientStream.isReady());
    final int maxToSend = 10 * 1024;
    int clientSent;
    // Verify that flow control will push back on client.
    for (clientSent = 0; clientStream.isReady(); clientSent++) {
      if (clientSent > maxToSend) {
        // It seems like flow control isn't working. _Surely_ flow control would have pushed-back
        // already. If this is normal, please configure the transport to buffer less.
        fail("Too many messages sent before isReady() returned false");
      }
      clientStream.writeMessage(methodDescriptor.streamRequest(largeMessage));
      clientStream.flush();
    }
    assertTrue(clientSent > 0);
    // Make sure there are at least a few messages buffered.
    for (; clientSent < 5; clientSent++) {
      clientStream.writeMessage(methodDescriptor.streamResponse(largeMessage));
      clientStream.flush();
    }
    doPingPong(serverListener);
    verify(mockServerStreamListener, timeout(TIMEOUT_MS)).messageRead(any(InputStream.class));

    doAnswer(closeStream).when(mockClientStreamListener).messageRead(any(InputStream.class));
    clientStream.request(1);
    verify(mockServerStreamListener, timeout(TIMEOUT_MS)).onReady();
    assertTrue(serverStream.isReady());
    int serverSent;
    // Verify that flow control will push back on server.
    for (serverSent = 0; serverStream.isReady(); serverSent++) {
      if (serverSent > maxToSend) {
        // It seems like flow control isn't working. _Surely_ flow control would have pushed-back
        // already. If this is normal, please configure the transport to buffer less.
        fail("Too many messages sent before isReady() returned false");
      }
      serverStream.writeMessage(methodDescriptor.streamResponse(largeMessage));
      serverStream.flush();
    }
    assertTrue(serverSent > 0);
    // Make sure there are at least a few messages buffered.
    for (; serverSent < 5; serverSent++) {
      serverStream.writeMessage(methodDescriptor.streamResponse(largeMessage));
      serverStream.flush();
    }
    doPingPong(serverListener);
    verify(mockClientStreamListener, timeout(TIMEOUT_MS)).messageRead(any(InputStream.class));

    serverStream.request(3);
    clientStream.request(3);
    doPingPong(serverListener);
    // times() is total number throughout the entire test
    verify(mockClientStreamListener, timeout(TIMEOUT_MS).times(4))
        .messageRead(any(InputStream.class));
    verify(mockServerStreamListener, timeout(TIMEOUT_MS).times(4))
        .messageRead(any(InputStream.class));

    // Request the rest
    serverStream.request(clientSent);
    clientStream.request(serverSent);
    verify(mockClientStreamListener, timeout(TIMEOUT_MS).times(clientSent))
        .messageRead(any(InputStream.class));
    verify(mockServerStreamListener, timeout(TIMEOUT_MS).times(serverSent))
        .messageRead(any(InputStream.class));

    verify(mockClientStreamListener, timeout(TIMEOUT_MS).times(2)).onReady();
    assertTrue(clientStream.isReady());
    verify(mockServerStreamListener, timeout(TIMEOUT_MS).times(2)).onReady();
    assertTrue(serverStream.isReady());

    // Request four more
    for (int i = 0; i < 5; i++) {
      clientStream.writeMessage(methodDescriptor.streamRequest(largeMessage));
      clientStream.flush();
      serverStream.writeMessage(methodDescriptor.streamResponse(largeMessage));
      serverStream.flush();
    }
    doPingPong(serverListener);
    verify(mockClientStreamListener, timeout(TIMEOUT_MS).times(clientSent + 4))
        .messageRead(any(InputStream.class));
    verify(mockServerStreamListener, timeout(TIMEOUT_MS).times(serverSent + 4))
        .messageRead(any(InputStream.class));

    // Drain exactly how many messages are left
    serverStream.request(1);
    clientStream.request(1);
    verify(mockServerStreamListener, timeout(TIMEOUT_MS).times(serverSent + 5))
        .messageRead(any(InputStream.class));
    verify(mockClientStreamListener, timeout(TIMEOUT_MS).times(clientSent + 5))
        .messageRead(any(InputStream.class));

    // And now check that the streams can still complete gracefully
    clientStream.writeMessage(methodDescriptor.streamRequest(largeMessage));
    clientStream.flush();
    clientStream.halfClose();
    doPingPong(serverListener);
    verify(mockServerStreamListener, never()).halfClosed();

    serverStream.request(1);
    verify(mockServerStreamListener, timeout(TIMEOUT_MS).times(serverSent + 6))
        .messageRead(any(InputStream.class));
    verify(mockServerStreamListener, timeout(TIMEOUT_MS)).halfClosed();

    serverStream.writeMessage(methodDescriptor.streamResponse(largeMessage));
    serverStream.flush();
    Status status = Status.OK.withDescription("... quite a lengthy discussion");
    serverStream.close(status, new Metadata());
    doPingPong(serverListener);
    verify(mockClientStreamListener, never()).closed(any(Status.class), any(Metadata.class));

    clientStream.request(1);
    verify(mockClientStreamListener, timeout(TIMEOUT_MS).times(clientSent + 6))
        .messageRead(any(InputStream.class));
    verify(mockServerStreamListener, timeout(TIMEOUT_MS)).closed(statusCaptor.capture());
    assertCodeEquals(Status.OK, statusCaptor.getValue());
    verify(mockClientStreamListener, timeout(TIMEOUT_MS))
        .closed(statusCaptor.capture(), any(Metadata.class));
    assertEquals(status.getCode(), statusCaptor.getValue().getCode());
    assertEquals(status.getDescription(), statusCaptor.getValue().getDescription());
  }

  /**
   * Helper that simply does an RPC. It can be used similar to a sleep for negative testing: to give
   * time for actions _not_ to happen. Since it is based on doing an actual RPC with actual
   * callbacks, it generally provides plenty of time for Runnables to execute. But it is also faster
   * on faster machines and more reliable on slower machines.
   */
  private void doPingPong(MockServerListener serverListener) throws InterruptedException {
    ManagedClientTransport client = newClientTransport();
    client.start(mock(ManagedClientTransport.Listener.class));
    ClientStream clientStream = client.newStream(methodDescriptor, new Metadata());
    ClientStreamListener mockClientStreamListener = mock(ClientStreamListener.class);
    clientStream.start(mockClientStreamListener);

    MockServerTransportListener serverTransportListener
        = serverListener.takeListenerOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    StreamCreation serverStreamCreation
        = serverTransportListener.takeStreamOrFail(TIMEOUT_MS, TimeUnit.MILLISECONDS);
    ServerStream serverStream = serverStreamCreation.stream;
    ServerStreamListener mockServerStreamListener = serverStreamCreation.listener;

    serverStream.close(Status.OK, new Metadata());
    verify(mockClientStreamListener, timeout(TIMEOUT_MS))
        .closed(any(Status.class), any(Metadata.class));
    verify(mockServerStreamListener, timeout(TIMEOUT_MS)).closed(any(Status.class));
    client.shutdown();
  }

  /**
   * Only assert that the Status.Code matches, but provide the entire actual result in case the
   * assertion fails.
   */
  private static void assertCodeEquals(Status expected, Status actual) {
    if (expected == null) {
      fail("expected should not be null");
    }
    if (actual == null || !expected.getCode().equals(actual.getCode())) {
      assertEquals(expected, actual);
    }
  }

  private static boolean waitForFuture(Future<?> future, long timeout, TimeUnit unit)
      throws InterruptedException {
    try {
      future.get(timeout, unit);
    } catch (ExecutionException ex) {
      throw new AssertionError(ex);
    } catch (TimeoutException ex) {
      return false;
    }
    return true;
  }

  private static class MockServerListener implements ServerListener {
    public final BlockingQueue<MockServerTransportListener> listeners
        = new LinkedBlockingQueue<MockServerTransportListener>();
    private final SettableFuture<?> shutdown = SettableFuture.create();

    @Override
    public ServerTransportListener transportCreated(ServerTransport transport) {
      MockServerTransportListener listener = new MockServerTransportListener(transport);
      listeners.add(listener);
      return listener;
    }

    @Override
    public void serverShutdown() {
      assertTrue(shutdown.set(null));
    }

    public boolean waitForShutdown(long timeout, TimeUnit unit) throws InterruptedException {
      return waitForFuture(shutdown, timeout, unit);
    }

    public MockServerTransportListener takeListenerOrFail(long timeout, TimeUnit unit)
        throws InterruptedException {
      MockServerTransportListener listener = listeners.poll(timeout, unit);
      if (listener == null) {
        fail("Timed out waiting for server transport");
      }
      return listener;
    }
  }

  private static class MockServerTransportListener implements ServerTransportListener {
    public final ServerTransport transport;
    public final BlockingQueue<StreamCreation> streams = new LinkedBlockingQueue<StreamCreation>();
    private final SettableFuture<?> terminated = SettableFuture.create();

    public MockServerTransportListener(ServerTransport transport) {
      this.transport = transport;
    }

    @Override
    public ServerStreamListener streamCreated(ServerStream stream, String method,
        Metadata headers) {
      ServerStreamListener listener = mock(ServerStreamListener.class);
      streams.add(new StreamCreation(stream, method, headers, listener));
      return listener;
    }

    @Override
    public void transportTerminated() {
      assertTrue(terminated.set(null));
    }

    public boolean waitForTermination(long timeout, TimeUnit unit) throws InterruptedException {
      return waitForFuture(terminated, timeout, unit);
    }

    public boolean isTerminated() {
      return terminated.isDone();
    }

    public StreamCreation takeStreamOrFail(long timeout, TimeUnit unit)
        throws InterruptedException {
      StreamCreation stream = streams.poll(timeout, unit);
      if (stream == null) {
        fail("Timed out waiting for server stream");
      }
      return stream;
    }
  }

  private static class StreamCreation {
    public final ServerStream stream;
    public final String method;
    public final Metadata headers;
    public final ServerStreamListener listener;

    public StreamCreation(ServerStream stream, String method,
        Metadata headers, ServerStreamListener listener) {
      this.stream = stream;
      this.method = method;
      this.headers = headers;
      this.listener = listener;
    }
  }

  private static class StringMarshaller implements MethodDescriptor.Marshaller<String> {
    public static final StringMarshaller INSTANCE = new StringMarshaller();

    @Override
    public InputStream stream(String value) {
      return new ByteArrayInputStream(value.getBytes(UTF_8));
    }

    @Override
    public String parse(InputStream stream) {
      try {
        return new String(ByteStreams.toByteArray(stream), UTF_8);
      } catch (IOException ex) {
        throw new RuntimeException(ex);
      }
    }
  }

  private static class StringBinaryMarshaller implements Metadata.BinaryMarshaller<String> {
    public static final StringBinaryMarshaller INSTANCE = new StringBinaryMarshaller();

    @Override
    public byte[] toBytes(String value) {
      return value.getBytes(UTF_8);
    }

    @Override
    public String parseBytes(byte[] serialized) {
      return new String(serialized, UTF_8);
    }
  }
}
