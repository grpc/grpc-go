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

package io.grpc;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.notNull;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.timeout;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;

import io.grpc.transport.ServerListener;
import io.grpc.transport.ServerStream;
import io.grpc.transport.ServerStreamListener;
import io.grpc.transport.ServerTransport;
import io.grpc.transport.ServerTransportListener;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.io.IOException;
import java.io.InputStream;
import java.util.concurrent.BrokenBarrierException;
import java.util.concurrent.CyclicBarrier;
import java.util.concurrent.Executor;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicReference;

/** Unit tests for {@link ServerImpl}. */
@RunWith(JUnit4.class)
public class ServerImplTest {
  private static final IntegerMarshaller INTEGER_MARSHALLER = IntegerMarshaller.INSTANCE;
  private static final StringMarshaller STRING_MARSHALLER = StringMarshaller.INSTANCE;

  private ExecutorService executor = Executors.newSingleThreadExecutor();
  private MutableHandlerRegistry registry = new MutableHandlerRegistryImpl();
  private SimpleServer transportServer = new SimpleServer();
  private ServerImpl server = new ServerImpl(executor, registry, transportServer);

  @Mock
  private ServerStream stream;

  @Mock
  private ServerCall.Listener<String> callListener;

  /** Set up for test. */
  @Before
  public void startUp() throws IOException {
    MockitoAnnotations.initMocks(this);

    server.start();
  }

  /** Tear down after test. */
  @After
  public void tearDown() {
    executor.shutdownNow();
  }

  @Test
  public void startStopImmediate() throws IOException {
    transportServer = new SimpleServer() {
      @Override
      public void shutdown() {}
    };
    ServerImpl server = new ServerImpl(executor, registry, transportServer);
    server.start();
    server.shutdown();
    assertTrue(server.isShutdown());
    assertFalse(server.isTerminated());
    transportServer.listener.serverShutdown();
    assertTrue(server.isTerminated());
  }

  @Test
  public void startStopImmediateWithChildTransport() throws IOException {
    ServerImpl server = new ServerImpl(executor, registry, transportServer);
    server.start();
    class DelayedShutdownServerTransport extends SimpleServerTransport {
      boolean shutdown;

      @Override
      public void shutdown() {
        shutdown = true;
      }
    }

    DelayedShutdownServerTransport serverTransport = new DelayedShutdownServerTransport();
    transportServer.registerNewServerTransport(serverTransport);
    server.shutdown();
    assertTrue(server.isShutdown());
    assertFalse(server.isTerminated());
    assertTrue(serverTransport.shutdown);
    serverTransport.listener.transportTerminated();
    assertTrue(server.isTerminated());
  }

  @Test
  public void transportServerFailsStartup() {
    final IOException ex = new IOException();
    class FailingStartupServer extends SimpleServer {
      @Override
      public void start(ServerListener listener) throws IOException {
        throw ex;
      }
    }

    ServerImpl server = new ServerImpl(executor, registry, new FailingStartupServer());
    try {
      server.start();
      fail("expected exception");
    } catch (IOException e) {
      assertSame(ex, e);
    }
  }

  @Test
  public void basicExchangeSuccessful() throws Exception {
    final Metadata.Key<Integer> metadataKey
        = Metadata.Key.of("inception", Metadata.INTEGER_MARSHALLER);
    final AtomicReference<ServerCall<Integer>> callReference
        = new AtomicReference<ServerCall<Integer>>();
    registry.addService(ServerServiceDefinition.builder("Waiter")
        .addMethod("serve", STRING_MARSHALLER, INTEGER_MARSHALLER,
          new ServerCallHandler<String, Integer>() {
            @Override
            public ServerCall.Listener<String> startCall(String fullMethodName,
                ServerCall<Integer> call, Metadata.Headers headers) {
              assertEquals("/Waiter/serve", fullMethodName);
              assertNotNull(call);
              assertNotNull(headers);
              assertEquals(0, headers.get(metadataKey).intValue());
              callReference.set(call);
              return callListener;
            }
          }).build());
    ServerTransportListener transportListener
        = transportServer.registerNewServerTransport(new SimpleServerTransport());

    Metadata.Headers headers = new Metadata.Headers();
    headers.put(metadataKey, 0);
    ServerStreamListener streamListener
        = transportListener.streamCreated(stream, "/Waiter/serve", headers);
    assertNotNull(streamListener);

    executeBarrier(executor).await();
    ServerCall<Integer> call = callReference.get();
    assertNotNull(call);

    String order = "Lots of pizza, please";
    streamListener.messageRead(STRING_MARSHALLER.stream(order));
    verify(callListener, timeout(2000)).onPayload(order);

    call.sendPayload(314);
    ArgumentCaptor<InputStream> inputCaptor = ArgumentCaptor.forClass(InputStream.class);
    verify(stream).writeMessage(inputCaptor.capture());
    verify(stream).flush();
    assertEquals(314, INTEGER_MARSHALLER.parse(inputCaptor.getValue()).intValue());

    streamListener.halfClosed(); // All full; no dessert.
    executeBarrier(executor).await();
    verify(callListener).onHalfClose();

    call.sendPayload(50);
    verify(stream, times(2)).writeMessage(inputCaptor.capture());
    verify(stream, times(2)).flush();
    assertEquals(50, INTEGER_MARSHALLER.parse(inputCaptor.getValue()).intValue());

    Metadata.Trailers trailers = new Metadata.Trailers();
    trailers.put(metadataKey, 3);
    Status status = Status.OK.withDescription("A okay");
    call.close(status, trailers);
    verify(stream).close(status, trailers);

    streamListener.closed(Status.OK);
    executeBarrier(executor).await();
    verify(callListener).onComplete();

    verifyNoMoreInteractions(stream);
    verifyNoMoreInteractions(callListener);
  }

  @Test
  public void exceptionInStartCallPropagatesToStream() throws Exception {
    CyclicBarrier barrier = executeBarrier(executor);
    final Status status = Status.ABORTED.withDescription("Oh, no!");
    registry.addService(ServerServiceDefinition.builder("Waiter")
        .addMethod("serve", STRING_MARSHALLER, INTEGER_MARSHALLER,
          new ServerCallHandler<String, Integer>() {
            @Override
            public ServerCall.Listener<String> startCall(String fullMethodName,
                ServerCall<Integer> call, Metadata.Headers headers) {
              throw status.asRuntimeException();
            }
          }).build());
    ServerTransportListener transportListener
        = transportServer.registerNewServerTransport(new SimpleServerTransport());

    ServerStreamListener streamListener
        = transportListener.streamCreated(stream, "/Waiter/serve", new Metadata.Headers());
    assertNotNull(streamListener);
    verifyNoMoreInteractions(stream);

    barrier.await();
    executeBarrier(executor).await();
    verify(stream).close(same(status), notNull(Metadata.Trailers.class));
    verifyNoMoreInteractions(stream);
  }

  /**
   * Useful for plugging a single-threaded executor from processing tasks, or for waiting until a
   * single-threaded executor has processed queued tasks.
   */
  private static CyclicBarrier executeBarrier(Executor executor) {
    final CyclicBarrier barrier = new CyclicBarrier(2);
    executor.execute(new Runnable() {
      @Override
      public void run() {
        try {
          barrier.await();
        } catch (InterruptedException ex) {
          throw new RuntimeException(ex);
        } catch (BrokenBarrierException ex) {
          throw new RuntimeException(ex);
        }
      }
    });
    return barrier;
  }

  private static class SimpleServer implements io.grpc.transport.Server {
    ServerListener listener;

    @Override
    public void start(ServerListener listener) throws IOException {
      this.listener = listener;
    }

    @Override
    public void shutdown() {
      listener.serverShutdown();
    }

    public ServerTransportListener registerNewServerTransport(SimpleServerTransport transport) {
      return transport.listener = listener.transportCreated(transport);
    }
  }

  private static class SimpleServerTransport implements ServerTransport {
    ServerTransportListener listener;

    @Override
    public void shutdown() {
      listener.transportTerminated();
    }
  }
}
