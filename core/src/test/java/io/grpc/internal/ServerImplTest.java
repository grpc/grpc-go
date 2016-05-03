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

package io.grpc.internal;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNotSame;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.isA;
import static org.mockito.Matchers.isNotNull;
import static org.mockito.Matchers.notNull;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.timeout;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;

import com.google.common.truth.Truth;
import com.google.common.util.concurrent.MoreExecutors;

import io.grpc.Compressor;
import io.grpc.CompressorRegistry;
import io.grpc.Context;
import io.grpc.DecompressorRegistry;
import io.grpc.HandlerRegistry;
import io.grpc.IntegerMarshaller;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.ServerServiceDefinition;
import io.grpc.Status;
import io.grpc.StringMarshaller;
import io.grpc.util.MutableHandlerRegistry;

import org.junit.After;
import org.junit.Before;
import org.junit.BeforeClass;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Matchers;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.concurrent.BrokenBarrierException;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.CyclicBarrier;
import java.util.concurrent.Executor;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

/** Unit tests for {@link ServerImpl}. */
@RunWith(JUnit4.class)
public class ServerImplTest {
  private static final IntegerMarshaller INTEGER_MARSHALLER = IntegerMarshaller.INSTANCE;
  private static final StringMarshaller STRING_MARSHALLER = StringMarshaller.INSTANCE;
  private static final Context.Key<String> SERVER_ONLY = Context.key("serverOnly");
  private static final Context.CancellableContext SERVER_CONTEXT =
      Context.ROOT.withValue(SERVER_ONLY, "yes").withCancellation();
  private final CompressorRegistry compressorRegistry = CompressorRegistry.getDefaultInstance();
  private final DecompressorRegistry decompressorRegistry =
      DecompressorRegistry.getDefaultInstance();

  @Rule public final ExpectedException thrown = ExpectedException.none();

  @BeforeClass
  public static void beforeStartUp() {
    // Cancel the root context. Server will fork it so the per-call context should not
    // be cancelled.
    SERVER_CONTEXT.cancel(null);
  }

  private ExecutorService executor = Executors.newSingleThreadExecutor();
  private InternalHandlerRegistry registry = new InternalHandlerRegistry.Builder().build();
  private MutableHandlerRegistry fallbackRegistry = new MutableHandlerRegistry();
  private SimpleServer transportServer = new SimpleServer();
  private ServerImpl server = new ServerImpl(executor, registry, fallbackRegistry, transportServer,
      SERVER_CONTEXT, decompressorRegistry, compressorRegistry);

  @Mock
  private ServerStream stream;

  @Mock
  private ServerCall.Listener<String> callListener;

  @Mock
  private ServerCallHandler<String, Integer> callHandler;

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
    ServerImpl server = new ServerImpl(executor, registry, fallbackRegistry, transportServer,
        SERVER_CONTEXT, decompressorRegistry, compressorRegistry);
    server.start();
    server.shutdown();
    assertTrue(server.isShutdown());
    assertFalse(server.isTerminated());
    transportServer.listener.serverShutdown();
    assertTrue(server.isTerminated());
  }

  @Test
  public void stopImmediate() {
    transportServer = new SimpleServer() {
      @Override
      public void shutdown() {
        throw new AssertionError("Should not be called, because wasn't started");
      }
    };
    ServerImpl server = new ServerImpl(executor, registry, fallbackRegistry, transportServer,
        SERVER_CONTEXT, decompressorRegistry, compressorRegistry);
    server.shutdown();
    assertTrue(server.isShutdown());
    assertTrue(server.isTerminated());
  }

  @Test
  public void startStopImmediateWithChildTransport() throws IOException {
    ServerImpl server = new ServerImpl(executor, registry, fallbackRegistry, transportServer,
        SERVER_CONTEXT, decompressorRegistry, compressorRegistry);
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

    ServerImpl server = new ServerImpl(executor, registry, fallbackRegistry,
        new FailingStartupServer(), SERVER_CONTEXT, decompressorRegistry, compressorRegistry);
    try {
      server.start();
      fail("expected exception");
    } catch (IOException e) {
      assertSame(ex, e);
    }
  }

  @Test
  public void basicExchangeSuccessful() throws Exception {
    final Metadata.Key<String> metadataKey
        = Metadata.Key.of("inception", Metadata.ASCII_STRING_MARSHALLER);
    final AtomicReference<ServerCall<Integer>> callReference
        = new AtomicReference<ServerCall<Integer>>();
    fallbackRegistry.addService(ServerServiceDefinition.builder("Waiter")
        .addMethod(
            MethodDescriptor.create(
                MethodType.UNKNOWN, "Waiter/serve", STRING_MARSHALLER, INTEGER_MARSHALLER),
            new ServerCallHandler<String, Integer>() {
              @Override
              public ServerCall.Listener<String> startCall(
                  MethodDescriptor<String, Integer> method,
                  ServerCall<Integer> call,
                  Metadata headers) {
                assertEquals("Waiter/serve", method.getFullMethodName());
                assertNotNull(call);
                assertNotNull(headers);
                assertEquals("value", headers.get(metadataKey));
                callReference.set(call);
                return callListener;
              }
            }).build());
    ServerTransportListener transportListener
        = transportServer.registerNewServerTransport(new SimpleServerTransport());

    Metadata requestHeaders = new Metadata();
    requestHeaders.put(metadataKey, "value");
    ServerStreamListener streamListener
        = transportListener.streamCreated(stream, "Waiter/serve", requestHeaders);
    assertNotNull(streamListener);

    executeBarrier(executor).await();
    ServerCall<Integer> call = callReference.get();
    assertNotNull(call);

    String order = "Lots of pizza, please";
    streamListener.messageRead(STRING_MARSHALLER.stream(order));
    verify(callListener, timeout(2000)).onMessage(order);

    Metadata responseHeaders = new Metadata();
    responseHeaders.put(metadataKey, "response value");
    call.sendHeaders(responseHeaders);
    verify(stream).writeHeaders(responseHeaders);
    verify(stream).setCompressor(isA(Compressor.class));

    call.sendMessage(314);
    ArgumentCaptor<InputStream> inputCaptor = ArgumentCaptor.forClass(InputStream.class);
    verify(stream).writeMessage(inputCaptor.capture());
    verify(stream).flush();
    assertEquals(314, INTEGER_MARSHALLER.parse(inputCaptor.getValue()).intValue());

    streamListener.halfClosed(); // All full; no dessert.
    executeBarrier(executor).await();
    verify(callListener).onHalfClose();

    call.sendMessage(50);
    verify(stream, times(2)).writeMessage(inputCaptor.capture());
    verify(stream, times(2)).flush();
    assertEquals(50, INTEGER_MARSHALLER.parse(inputCaptor.getValue()).intValue());

    Metadata trailers = new Metadata();
    trailers.put(metadataKey, "another value");
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
    fallbackRegistry.addService(ServerServiceDefinition.builder("Waiter")
        .addMethod(
            MethodDescriptor.create(MethodType.UNKNOWN, "Waiter/serve",
              STRING_MARSHALLER, INTEGER_MARSHALLER),
            new ServerCallHandler<String, Integer>() {
              @Override
              public ServerCall.Listener<String> startCall(
                  MethodDescriptor<String, Integer> method,
                  ServerCall<Integer> call,
                  Metadata headers) {
                throw status.asRuntimeException();
              }
            }).build());
    ServerTransportListener transportListener
        = transportServer.registerNewServerTransport(new SimpleServerTransport());

    ServerStreamListener streamListener
        = transportListener.streamCreated(stream, "Waiter/serve", new Metadata());
    assertNotNull(streamListener);
    verifyNoMoreInteractions(stream);

    barrier.await();
    executeBarrier(executor).await();
    verify(stream).close(same(status), notNull(Metadata.class));
    verifyNoMoreInteractions(stream);
  }

  @Test
  public void testNoDeadlockOnShutdown() throws Exception {
    final Object lock = new Object();
    final CyclicBarrier barrier = new CyclicBarrier(2);
    class MaybeDeadlockingServer extends SimpleServer {
      @Override
      public void shutdown() {
        // To deadlock, a lock would need to be held while this method is in progress.
        try {
          barrier.await();
        } catch (Exception ex) {
          throw new AssertionError(ex);
        }
        // If deadlock is possible with this setup, this sychronization completes the loop because
        // the serverShutdown needs a lock that Server is holding while calling this method.
        synchronized (lock) {
        }
      }
    }

    transportServer = new MaybeDeadlockingServer();
    ServerImpl server = new ServerImpl(executor, registry, fallbackRegistry, transportServer,
        SERVER_CONTEXT, decompressorRegistry, compressorRegistry);
    server.start();
    new Thread() {
      @Override
      public void run() {
        synchronized (lock) {
          try {
            barrier.await();
          } catch (Exception ex) {
            throw new AssertionError(ex);
          }
          // To deadlock, a lock would be needed for this call to proceed.
          transportServer.listener.serverShutdown();
        }
      }
    }.start();
    server.shutdown();
  }

  @Test
  public void testNoDeadlockOnTransportShutdown() throws Exception {
    final Object lock = new Object();
    final CyclicBarrier barrier = new CyclicBarrier(2);
    class MaybeDeadlockingServerTransport extends SimpleServerTransport {
      @Override
      public void shutdown() {
        // To deadlock, a lock would need to be held while this method is in progress.
        try {
          barrier.await();
        } catch (Exception ex) {
          throw new AssertionError(ex);
        }
        // If deadlock is possible with this setup, this sychronization completes the loop
        // because the transportTerminated needs a lock that Server is holding while calling this
        // method.
        synchronized (lock) {
        }
      }
    }

    final ServerTransportListener transportListener
        = transportServer.registerNewServerTransport(new MaybeDeadlockingServerTransport());
    new Thread() {
      @Override
      public void run() {
        synchronized (lock) {
          try {
            barrier.await();
          } catch (Exception ex) {
            throw new AssertionError(ex);
          }
          // To deadlock, a lock would be needed for this call to proceed.
          transportListener.transportTerminated();
        }
      }
    }.start();
    server.shutdown();
  }

  @Test
  public void testCallContextIsBoundInListenerCallbacks() throws Exception {
    fallbackRegistry.addService(ServerServiceDefinition.builder("Waiter")
        .addMethod(
            MethodDescriptor.create(
                MethodType.UNKNOWN, "Waiter/serve", STRING_MARSHALLER, INTEGER_MARSHALLER),
            new ServerCallHandler<String, Integer>() {
              @Override
              public ServerCall.Listener<String> startCall(
                  MethodDescriptor<String, Integer> method,
                  ServerCall<Integer> call,
                  Metadata headers) {
                // Check that the current context is a descendant of SERVER_CONTEXT
                final Context initial = Context.current();
                assertEquals("yes", SERVER_ONLY.get(initial));
                assertNotSame(SERVER_CONTEXT, initial);
                assertFalse(initial.isCancelled());
                return new ServerCall.Listener<String>() {

                  @Override
                  public void onReady() {
                    checkContext();
                    super.onReady();
                  }

                  @Override
                  public void onMessage(String message) {
                    checkContext();
                    super.onMessage(message);
                  }

                  @Override
                  public void onHalfClose() {
                    checkContext();
                    super.onHalfClose();
                  }

                  @Override
                  public void onCancel() {
                    checkContext();
                    super.onCancel();
                  }

                  @Override
                  public void onComplete() {
                    checkContext();
                    super.onComplete();
                  }

                  private void checkContext() {
                    // Check that the bound context is the same as the initial one.
                    assertSame(initial, Context.current());
                  }
                };
              }
            }).build());
    ServerTransportListener transportListener
        = transportServer.registerNewServerTransport(new SimpleServerTransport());

    ServerStreamListener streamListener
        = transportListener.streamCreated(stream, "Waiter/serve", new Metadata());
    assertNotNull(streamListener);

    streamListener.onReady();
    streamListener.messageRead(new ByteArrayInputStream(new byte[0]));
    streamListener.halfClosed();
    streamListener.closed(Status.CANCELLED);

    // Close should never be called if asserts in listener pass.
    verify(stream, times(0)).close(isA(Status.class), isNotNull(Metadata.class));
  }

  @Test
  public void testClientCancelTriggersContextCancellation() throws Exception {
    final CountDownLatch latch = new CountDownLatch(1);
    callListener = new ServerCall.Listener<String>() {
      @Override
      public void onReady() {
        Context.current().addListener(new Context.CancellationListener() {
          @Override
          public void cancelled(Context context) {
            latch.countDown();
          }
        }, MoreExecutors.directExecutor());
      }
    };

    final AtomicReference<ServerCall<Integer>> callReference
        = new AtomicReference<ServerCall<Integer>>();
    fallbackRegistry.addService(ServerServiceDefinition.builder("Waiter")
        .addMethod(
            MethodDescriptor.create(
                MethodType.UNKNOWN, "Waiter/serve", STRING_MARSHALLER, INTEGER_MARSHALLER),
            new ServerCallHandler<String, Integer>() {
              @Override
              public ServerCall.Listener<String> startCall(
                  MethodDescriptor<String, Integer> method,
                  ServerCall<Integer> call,
                  Metadata headers) {
                callReference.set(call);
                return callListener;
              }
            }).build());
    ServerTransportListener transportListener
        = transportServer.registerNewServerTransport(new SimpleServerTransport());

    ServerStreamListener streamListener
        = transportListener.streamCreated(stream, "Waiter/serve", new Metadata());
    assertNotNull(streamListener);

    streamListener.onReady();
    streamListener.closed(Status.CANCELLED);

    assertTrue(latch.await(5, TimeUnit.SECONDS));
  }

  @Test
  public void getPort() throws Exception {
    transportServer = new SimpleServer() {
      @Override
      public int getPort() {
        return 65535;
      }
    };
    ServerImpl server = new ServerImpl(executor, registry, fallbackRegistry, transportServer,
        SERVER_CONTEXT, decompressorRegistry, compressorRegistry);
    server.start();

    Truth.assertThat(server.getPort()).isEqualTo(65535);
  }

  @Test
  public void getPortBeforeStartedFails() {
    transportServer = new SimpleServer();
    ServerImpl server = new ServerImpl(executor, registry, fallbackRegistry, transportServer,
        SERVER_CONTEXT, decompressorRegistry, compressorRegistry);
    thrown.expect(IllegalStateException.class);
    thrown.expectMessage("started");
    server.getPort();
  }

  @Test
  public void getPortAfterTerminationFails() throws Exception {
    transportServer = new SimpleServer();
    ServerImpl server = new ServerImpl(executor, registry, fallbackRegistry, transportServer,
        SERVER_CONTEXT, decompressorRegistry, compressorRegistry);
    server.start();
    server.shutdown();
    server.awaitTermination();
    thrown.expect(IllegalStateException.class);
    thrown.expectMessage("terminated");
    server.getPort();
  }

  @Test
  public void handlerRegistryPriorities() throws Exception {
    HandlerRegistry fallbackRegistry = mock(HandlerRegistry.class);
    MethodDescriptor<String, Integer> method1 = MethodDescriptor.create(
        MethodType.UNKNOWN, "Service1/Method1", STRING_MARSHALLER, INTEGER_MARSHALLER);
    registry = new InternalHandlerRegistry.Builder()
        .addService(ServerServiceDefinition.builder("Service1")
            .addMethod(method1, callHandler).build())
        .build();
    transportServer = new SimpleServer();
    ServerImpl server = new ServerImpl(executor, registry, fallbackRegistry, transportServer,
        SERVER_CONTEXT, decompressorRegistry, compressorRegistry);
    server.start();

    ServerTransportListener transportListener
        = transportServer.registerNewServerTransport(new SimpleServerTransport());
    // This call will be handled by callHandler from the internal registry
    transportListener.streamCreated(stream, "Service1/Method1", new Metadata());
    // This call will be handled by the fallbackRegistry because it's not registred in the internal
    // registry.
    transportListener.streamCreated(stream, "Service1/Method2", new Metadata());

    verify(callHandler, timeout(2000)).startCall(same(method1),
        Matchers.<ServerCall<Integer>>anyObject(), Matchers.<Metadata>anyObject());
    verify(fallbackRegistry, timeout(2000)).lookupMethod("Service1/Method2", null);
    verifyNoMoreInteractions(callHandler);
    verifyNoMoreInteractions(fallbackRegistry);
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
          Thread.currentThread().interrupt();
          throw new RuntimeException(ex);
        } catch (BrokenBarrierException ex) {
          throw new RuntimeException(ex);
        }
      }
    });
    return barrier;
  }

  private static class SimpleServer implements io.grpc.internal.InternalServer {
    ServerListener listener;

    @Override
    public void start(ServerListener listener) throws IOException {
      this.listener = listener;
    }

    @Override
    public int getPort() {
      return -1;
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
