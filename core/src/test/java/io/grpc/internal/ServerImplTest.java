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
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.isA;
import static org.mockito.Matchers.isNotNull;
import static org.mockito.Matchers.notNull;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.atLeast;
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.collect.ImmutableList;
import com.google.common.truth.Truth;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.instrumentation.stats.RpcConstants;
import com.google.instrumentation.stats.StatsContext;
import com.google.instrumentation.stats.TagValue;
import io.grpc.Attributes;
import io.grpc.Compressor;
import io.grpc.CompressorRegistry;
import io.grpc.Context;
import io.grpc.DecompressorRegistry;
import io.grpc.Grpc;
import io.grpc.HandlerRegistry;
import io.grpc.IntegerMarshaller;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.ServerServiceDefinition;
import io.grpc.ServerTransportFilter;
import io.grpc.ServiceDescriptor;
import io.grpc.Status;
import io.grpc.StringMarshaller;
import io.grpc.internal.ServerImpl.JumpToApplicationThreadServerStreamListener;
import io.grpc.internal.testing.StatsTestUtils;
import io.grpc.internal.testing.StatsTestUtils.FakeStatsContextFactory;
import io.grpc.util.MutableHandlerRegistry;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.net.SocketAddress;
import java.util.List;
import java.util.concurrent.CyclicBarrier;
import java.util.concurrent.Executor;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.After;
import org.junit.Before;
import org.junit.BeforeClass;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.Matchers;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/** Unit tests for {@link ServerImpl}. */
@RunWith(JUnit4.class)
public class ServerImplTest {
  private static final IntegerMarshaller INTEGER_MARSHALLER = IntegerMarshaller.INSTANCE;
  private static final StringMarshaller STRING_MARSHALLER = StringMarshaller.INSTANCE;
  private static final Context.Key<String> SERVER_ONLY = Context.key("serverOnly");
  private static final Context.CancellableContext SERVER_CONTEXT =
      Context.ROOT.withValue(SERVER_ONLY, "yes").withCancellation();
  private static final ImmutableList<ServerTransportFilter> NO_FILTERS = ImmutableList.of();

  private final FakeStatsContextFactory statsCtxFactory = new FakeStatsContextFactory();
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

  private final FakeClock executor = new FakeClock();
  private final FakeClock timer = new FakeClock();
  @Mock
  private ObjectPool<Executor> executorPool;
  @Mock
  private ObjectPool<ScheduledExecutorService> timerPool;
  private InternalHandlerRegistry registry = new InternalHandlerRegistry.Builder().build();
  private MutableHandlerRegistry mutableFallbackRegistry = new MutableHandlerRegistry();
  private HandlerRegistry fallbackRegistry = mutableFallbackRegistry;
  private SimpleServer transportServer = new SimpleServer();
  private ServerImpl server;

  @Captor
  private ArgumentCaptor<Status> statusCaptor;
  @Captor
  private ArgumentCaptor<ServerStreamListener> streamListenerCaptor;

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
    when(executorPool.getObject()).thenReturn(executor.getScheduledExecutorService());
    when(timerPool.getObject()).thenReturn(timer.getScheduledExecutorService());
  }

  @After
  public void noPendingTasks() {
    assertEquals(0, executor.numPendingTasks());
    assertEquals(0, timer.numPendingTasks());
  }

  @Test
  public void startStopImmediate() throws IOException {
    transportServer = new SimpleServer() {
      @Override
      public void shutdown() {}
    };
    createAndStartServer(NO_FILTERS);
    server.shutdown();
    assertTrue(server.isShutdown());
    assertFalse(server.isTerminated());
    transportServer.listener.serverShutdown();
    assertTrue(server.isTerminated());
  }

  @Test
  public void stopImmediate() throws IOException {
    transportServer = new SimpleServer() {
      @Override
      public void shutdown() {
        throw new AssertionError("Should not be called, because wasn't started");
      }
    };
    createServer(NO_FILTERS);
    server.shutdown();
    assertTrue(server.isShutdown());
    assertTrue(server.isTerminated());
    verifyNoMoreInteractions(executorPool);
    verifyNoMoreInteractions(timerPool);
  }

  @Test
  public void startStopImmediateWithChildTransport() throws IOException {
    createAndStartServer(NO_FILTERS);
    verifyExecutorsAcquired();
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
    verifyExecutorsNotReturned();

    serverTransport.listener.transportTerminated();
    assertTrue(server.isTerminated());
    verifyExecutorsReturned();
  }

  @Test
  public void startShutdownNowImmediateWithChildTransport() throws IOException {
    createAndStartServer(NO_FILTERS);
    verifyExecutorsAcquired();
    class DelayedShutdownServerTransport extends SimpleServerTransport {
      boolean shutdown;

      @Override
      public void shutdown() {}

      @Override
      public void shutdownNow(Status reason) {
        shutdown = true;
      }
    }

    DelayedShutdownServerTransport serverTransport = new DelayedShutdownServerTransport();
    transportServer.registerNewServerTransport(serverTransport);
    server.shutdownNow();
    assertTrue(server.isShutdown());
    assertFalse(server.isTerminated());
    assertTrue(serverTransport.shutdown);
    verifyExecutorsNotReturned();

    serverTransport.listener.transportTerminated();
    assertTrue(server.isTerminated());
    verifyExecutorsReturned();
  }

  @Test
  public void shutdownNowAfterShutdown() throws IOException {
    createAndStartServer(NO_FILTERS);
    verifyExecutorsAcquired();
    class DelayedShutdownServerTransport extends SimpleServerTransport {
      boolean shutdown;

      @Override
      public void shutdown() {}

      @Override
      public void shutdownNow(Status reason) {
        shutdown = true;
      }
    }

    DelayedShutdownServerTransport serverTransport = new DelayedShutdownServerTransport();
    transportServer.registerNewServerTransport(serverTransport);
    server.shutdown();
    assertTrue(server.isShutdown());
    server.shutdownNow();
    assertFalse(server.isTerminated());
    assertTrue(serverTransport.shutdown);
    verifyExecutorsNotReturned();

    serverTransport.listener.transportTerminated();
    assertTrue(server.isTerminated());
    verifyExecutorsReturned();
  }

  @Test
  public void shutdownNowAfterSlowShutdown() throws IOException {
    transportServer = new SimpleServer() {
      @Override
      public void shutdown() {
        // Don't call super which calls listener.serverShutdown(). We'll call it manually.
      }
    };
    createAndStartServer(NO_FILTERS);
    verifyExecutorsAcquired();
    class DelayedShutdownServerTransport extends SimpleServerTransport {
      boolean shutdown;

      @Override
      public void shutdown() {}

      @Override
      public void shutdownNow(Status reason) {
        shutdown = true;
      }
    }

    DelayedShutdownServerTransport serverTransport = new DelayedShutdownServerTransport();
    transportServer.registerNewServerTransport(serverTransport);
    server.shutdown();
    server.shutdownNow();
    transportServer.listener.serverShutdown();
    assertTrue(server.isShutdown());
    assertFalse(server.isTerminated());

    verifyExecutorsNotReturned();
    serverTransport.listener.transportTerminated();
    verifyExecutorsReturned();
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

    transportServer = new FailingStartupServer();
    createServer(NO_FILTERS);
    try {
      server.start();
      fail("expected exception");
    } catch (IOException e) {
      assertSame(ex, e);
    }
    verifyNoMoreInteractions(executorPool);
    verifyNoMoreInteractions(timerPool);
  }

  @Test
  public void methodNotFound() throws Exception {
    createAndStartServer(NO_FILTERS);
    ServerTransportListener transportListener
        = transportServer.registerNewServerTransport(new SimpleServerTransport());
    Metadata requestHeaders = new Metadata();
    StatsTraceContext statsTraceCtx =
        transportListener.methodDetermined("Waiter/nonexist", requestHeaders);
    when(stream.statsTraceContext()).thenReturn(statsTraceCtx);
    transportListener.streamCreated(stream, "Waiter/nonexist", requestHeaders);
    verify(stream).setListener(isA(ServerStreamListener.class));
    verify(stream, atLeast(1)).statsTraceContext();

    assertEquals(1, executor.runDueTasks());
    verify(stream).close(statusCaptor.capture(), any(Metadata.class));
    Status status = statusCaptor.getValue();
    assertEquals(Status.Code.UNIMPLEMENTED, status.getCode());
    assertEquals("Method not found: Waiter/nonexist", status.getDescription());

    StatsTestUtils.MetricsRecord record = statsCtxFactory.pollRecord();
    assertNotNull(record);
    TagValue methodTag = record.tags.get(RpcConstants.RPC_SERVER_METHOD);
    assertNotNull(methodTag);
    assertEquals("Waiter/nonexist", methodTag.toString());
    TagValue statusTag = record.tags.get(RpcConstants.RPC_STATUS);
    assertNotNull(statusTag);
    assertEquals(Status.Code.UNIMPLEMENTED.toString(), statusTag.toString());
  }

  @Test
  public void basicExchangeSuccessful() throws Exception {
    createAndStartServer(NO_FILTERS);
    final Metadata.Key<String> metadataKey
        = Metadata.Key.of("inception", Metadata.ASCII_STRING_MARSHALLER);
    final Metadata.Key<StatsContext> statsHeaderKey
        = StatsTraceContext.createStatsHeader(statsCtxFactory);
    final AtomicReference<ServerCall<String, Integer>> callReference
        = new AtomicReference<ServerCall<String, Integer>>();
    MethodDescriptor<String, Integer> method = MethodDescriptor.<String, Integer>newBuilder()
        .setType(MethodDescriptor.MethodType.UNKNOWN)
        .setFullMethodName("Waiter/serve")
        .setRequestMarshaller(STRING_MARSHALLER)
        .setResponseMarshaller(INTEGER_MARSHALLER)
        .build();
    mutableFallbackRegistry.addService(ServerServiceDefinition.builder(
        new ServiceDescriptor("Waiter", method))
        .addMethod(
            method,
            new ServerCallHandler<String, Integer>() {
              @Override
              public ServerCall.Listener<String> startCall(
                  ServerCall<String, Integer> call,
                  Metadata headers) {
                assertEquals("Waiter/serve", call.getMethodDescriptor().getFullMethodName());
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
    StatsContext statsContextOnClient = statsCtxFactory.getDefault().with(
        StatsTestUtils.EXTRA_TAG, TagValue.create("extraTagValue"));
    requestHeaders.put(statsHeaderKey, statsContextOnClient);
    StatsTraceContext statsTraceCtx =
        transportListener.methodDetermined("Waiter/serve", requestHeaders);
    assertNotNull(statsTraceCtx);
    when(stream.statsTraceContext()).thenReturn(statsTraceCtx);

    transportListener.streamCreated(stream, "Waiter/serve", requestHeaders);
    verify(stream).setListener(streamListenerCaptor.capture());
    ServerStreamListener streamListener = streamListenerCaptor.getValue();
    assertNotNull(streamListener);
    verify(stream, atLeast(1)).statsTraceContext();

    assertEquals(1, executor.runDueTasks());
    ServerCall<String, Integer> call = callReference.get();
    assertNotNull(call);
    verify(stream).getAuthority();

    String order = "Lots of pizza, please";
    streamListener.messageRead(STRING_MARSHALLER.stream(order));
    assertEquals(1, executor.runDueTasks());
    verify(callListener).onMessage(order);

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
    assertEquals(1, executor.runDueTasks());
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
    assertEquals(1, executor.runDueTasks());
    verify(callListener).onComplete();

    verify(stream, atLeast(1)).statsTraceContext();
    verifyNoMoreInteractions(stream);
    verifyNoMoreInteractions(callListener);

    // Check stats
    StatsTestUtils.MetricsRecord record = statsCtxFactory.pollRecord();
    assertNotNull(record);
    TagValue methodTag = record.tags.get(RpcConstants.RPC_SERVER_METHOD);
    assertNotNull(methodTag);
    assertEquals("Waiter/serve", methodTag.toString());
    TagValue statusTag = record.tags.get(RpcConstants.RPC_STATUS);
    assertNotNull(statusTag);
    assertEquals(Status.Code.OK.toString(), statusTag.toString());
    TagValue extraTag = record.tags.get(StatsTestUtils.EXTRA_TAG);
    assertNotNull(extraTag);
    assertEquals("extraTagValue", extraTag.toString());
    assertNull(record.getMetric(RpcConstants.RPC_CLIENT_REQUEST_BYTES));
    assertNull(record.getMetric(RpcConstants.RPC_CLIENT_RESPONSE_BYTES));
    assertNull(record.getMetric(RpcConstants.RPC_CLIENT_UNCOMPRESSED_REQUEST_BYTES));
    assertNull(record.getMetric(RpcConstants.RPC_CLIENT_UNCOMPRESSED_RESPONSE_BYTES));
    // The test doesn't invoke MessageFramer and MessageDeframer which keep the sizes.
    // Thus the sizes reported to stats would be zero.
    assertEquals(0, record.getMetricAsLongOrFail(RpcConstants.RPC_SERVER_REQUEST_BYTES));
    assertEquals(0, record.getMetricAsLongOrFail(RpcConstants.RPC_SERVER_RESPONSE_BYTES));
    assertEquals(0,
        record.getMetricAsLongOrFail(RpcConstants.RPC_SERVER_UNCOMPRESSED_REQUEST_BYTES));
    assertEquals(0,
        record.getMetricAsLongOrFail(RpcConstants.RPC_SERVER_UNCOMPRESSED_RESPONSE_BYTES));
  }

  @Test
  public void transportFilters() throws Exception {
    final SocketAddress remoteAddr = mock(SocketAddress.class);
    final Attributes.Key<String> key1 = Attributes.Key.of("test-key1");
    final Attributes.Key<String> key2 = Attributes.Key.of("test-key2");
    final Attributes.Key<String> key3 = Attributes.Key.of("test-key3");
    final AtomicReference<Attributes> filter1TerminationCallbackArgument =
        new AtomicReference<Attributes>();
    final AtomicReference<Attributes> filter2TerminationCallbackArgument =
        new AtomicReference<Attributes>();
    final AtomicInteger readyCallbackCalled = new AtomicInteger(0);
    final AtomicInteger terminationCallbackCalled = new AtomicInteger(0);
    ServerTransportFilter filter1 = new ServerTransportFilter() {
        @Override
        public Attributes transportReady(Attributes attrs) {
          assertEquals(Attributes.newBuilder()
              .set(Grpc.TRANSPORT_ATTR_REMOTE_ADDR, remoteAddr)
              .build(), attrs);
          readyCallbackCalled.incrementAndGet();
          return Attributes.newBuilder(attrs)
              .set(key1, "yalayala")
              .set(key2, "blabla")
              .build();
        }

        @Override
        public void transportTerminated(Attributes attrs) {
          terminationCallbackCalled.incrementAndGet();
          filter1TerminationCallbackArgument.set(attrs);
        }
      };
    ServerTransportFilter filter2 = new ServerTransportFilter() {
        @Override
        public Attributes transportReady(Attributes attrs) {
          assertEquals(Attributes.newBuilder()
              .set(Grpc.TRANSPORT_ATTR_REMOTE_ADDR, remoteAddr)
              .set(key1, "yalayala")
              .set(key2, "blabla")
              .build(), attrs);
          readyCallbackCalled.incrementAndGet();
          return Attributes.newBuilder(attrs)
              .set(key1, "ouch")
              .set(key3, "puff")
              .build();
        }

        @Override
        public void transportTerminated(Attributes attrs) {
          terminationCallbackCalled.incrementAndGet();
          filter2TerminationCallbackArgument.set(attrs);
        }
      };
    Attributes expectedTransportAttrs = Attributes.newBuilder()
        .set(key1, "ouch")
        .set(key2, "blabla")
        .set(key3, "puff")
        .set(Grpc.TRANSPORT_ATTR_REMOTE_ADDR, remoteAddr)
        .build();

    createAndStartServer(ImmutableList.of(filter1, filter2));
    ServerTransportListener transportListener
        = transportServer.registerNewServerTransport(new SimpleServerTransport());
    Attributes transportAttrs = transportListener.transportReady(Attributes.newBuilder()
        .set(Grpc.TRANSPORT_ATTR_REMOTE_ADDR, remoteAddr).build());

    assertEquals(expectedTransportAttrs, transportAttrs);

    server.shutdown();
    server.awaitTermination();

    assertEquals(expectedTransportAttrs, filter1TerminationCallbackArgument.get());
    assertEquals(expectedTransportAttrs, filter2TerminationCallbackArgument.get());
    assertEquals(2, readyCallbackCalled.get());
    assertEquals(2, terminationCallbackCalled.get());
  }

  @Test
  public void exceptionInStartCallPropagatesToStream() throws Exception {
    createAndStartServer(NO_FILTERS);
    final Status status = Status.ABORTED.withDescription("Oh, no!");
    MethodDescriptor<String, Integer> method = MethodDescriptor.<String, Integer>newBuilder()
        .setType(MethodDescriptor.MethodType.UNKNOWN)
        .setFullMethodName("Waiter/serve")
        .setRequestMarshaller(STRING_MARSHALLER)
        .setResponseMarshaller(INTEGER_MARSHALLER)
        .build();
    mutableFallbackRegistry.addService(ServerServiceDefinition.builder(
        new ServiceDescriptor("Waiter", method))
        .addMethod(method,
            new ServerCallHandler<String, Integer>() {
              @Override
              public ServerCall.Listener<String> startCall(
                  ServerCall<String, Integer> call,
                  Metadata headers) {
                throw status.asRuntimeException();
              }
            }).build());
    ServerTransportListener transportListener
        = transportServer.registerNewServerTransport(new SimpleServerTransport());

    Metadata requestHeaders = new Metadata();
    StatsTraceContext statsTraceCtx =
        transportListener.methodDetermined("Waiter/serve", requestHeaders);
    assertNotNull(statsTraceCtx);
    when(stream.statsTraceContext()).thenReturn(statsTraceCtx);

    transportListener.streamCreated(stream, "Waiter/serve", requestHeaders);
    verify(stream).setListener(streamListenerCaptor.capture());
    ServerStreamListener streamListener = streamListenerCaptor.getValue();
    assertNotNull(streamListener);
    verify(stream, atLeast(1)).statsTraceContext();
    verifyNoMoreInteractions(stream);

    assertEquals(1, executor.runDueTasks());
    verify(stream).getAuthority();
    verify(stream).close(same(status), notNull(Metadata.class));
    verify(stream, atLeast(1)).statsTraceContext();
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
    createAndStartServer(NO_FILTERS);
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
    createAndStartServer(NO_FILTERS);
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
    createAndStartServer(NO_FILTERS);
    MethodDescriptor<String, Integer> method = MethodDescriptor.<String, Integer>newBuilder()
        .setType(MethodDescriptor.MethodType.UNKNOWN)
        .setFullMethodName("Waiter/serve")
        .setRequestMarshaller(STRING_MARSHALLER)
        .setResponseMarshaller(INTEGER_MARSHALLER)
        .build();
    final AtomicBoolean  onReadyCalled = new AtomicBoolean(false);
    final AtomicBoolean onMessageCalled = new AtomicBoolean(false);
    final AtomicBoolean onHalfCloseCalled = new AtomicBoolean(false);
    final AtomicBoolean onCancelCalled = new AtomicBoolean(false);
    mutableFallbackRegistry.addService(ServerServiceDefinition.builder(
        new ServiceDescriptor("Waiter", method))
        .addMethod(
            method,
            new ServerCallHandler<String, Integer>() {
              @Override
              public ServerCall.Listener<String> startCall(
                  ServerCall<String, Integer> call,
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
                    onReadyCalled.set(true);
                  }

                  @Override
                  public void onMessage(String message) {
                    checkContext();
                    onMessageCalled.set(true);
                  }

                  @Override
                  public void onHalfClose() {
                    checkContext();
                    onHalfCloseCalled.set(true);
                  }

                  @Override
                  public void onCancel() {
                    checkContext();
                    onCancelCalled.set(true);
                  }

                  @Override
                  public void onComplete() {
                    checkContext();
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

    Metadata requestHeaders = new Metadata();
    StatsTraceContext statsTraceCtx =
        transportListener.methodDetermined("Waiter/serve", requestHeaders);
    assertNotNull(statsTraceCtx);
    when(stream.statsTraceContext()).thenReturn(statsTraceCtx);

    transportListener.streamCreated(stream, "Waiter/serve", requestHeaders);
    verify(stream).setListener(streamListenerCaptor.capture());
    ServerStreamListener streamListener = streamListenerCaptor.getValue();
    assertNotNull(streamListener);

    streamListener.onReady();
    assertEquals(1, executor.runDueTasks());
    assertTrue(onReadyCalled.get());

    streamListener.messageRead(new ByteArrayInputStream(new byte[0]));
    assertEquals(1, executor.runDueTasks());
    assertTrue(onMessageCalled.get());

    streamListener.halfClosed();
    assertEquals(1, executor.runDueTasks());
    assertTrue(onHalfCloseCalled.get());

    streamListener.closed(Status.CANCELLED);
    assertEquals(1, executor.runDueTasks());
    assertTrue(onCancelCalled.get());

    // Close should never be called if asserts in listener pass.
    verify(stream, times(0)).close(isA(Status.class), isNotNull(Metadata.class));
  }

  @Test
  public void testClientCancelTriggersContextCancellation() throws Exception {
    createAndStartServer(NO_FILTERS);
    final AtomicBoolean contextCancelled = new AtomicBoolean(false);
    callListener = new ServerCall.Listener<String>() {
      @Override
      public void onReady() {
        Context.current().addListener(new Context.CancellationListener() {
          @Override
          public void cancelled(Context context) {
            contextCancelled.set(true);
          }
        }, MoreExecutors.directExecutor());
      }
    };

    final AtomicReference<ServerCall<String, Integer>> callReference
        = new AtomicReference<ServerCall<String, Integer>>();
    MethodDescriptor<String, Integer> method = MethodDescriptor.<String, Integer>newBuilder()
        .setType(MethodDescriptor.MethodType.UNKNOWN)
        .setFullMethodName("Waiter/serve")
        .setRequestMarshaller(STRING_MARSHALLER)
        .setResponseMarshaller(INTEGER_MARSHALLER)
        .build();

    mutableFallbackRegistry.addService(ServerServiceDefinition.builder(
        new ServiceDescriptor("Waiter", method))
        .addMethod(method,
            new ServerCallHandler<String, Integer>() {
              @Override
              public ServerCall.Listener<String> startCall(
                  ServerCall<String, Integer> call,
                  Metadata headers) {
                callReference.set(call);
                return callListener;
              }
            }).build());
    ServerTransportListener transportListener
        = transportServer.registerNewServerTransport(new SimpleServerTransport());
    Metadata requestHeaders = new Metadata();
    StatsTraceContext statsTraceCtx =
        transportListener.methodDetermined("Waiter/serve", requestHeaders);
    assertNotNull(statsTraceCtx);
    when(stream.statsTraceContext()).thenReturn(statsTraceCtx);
    transportListener.streamCreated(stream, "Waiter/serve", requestHeaders);
    verify(stream).setListener(streamListenerCaptor.capture());
    ServerStreamListener streamListener = streamListenerCaptor.getValue();
    assertNotNull(streamListener);

    streamListener.onReady();
    streamListener.closed(Status.CANCELLED);

    assertEquals(1, executor.runDueTasks());
    assertTrue(contextCancelled.get());
  }

  @Test
  public void getPort() throws Exception {
    transportServer = new SimpleServer() {
      @Override
      public int getPort() {
        return 65535;
      }
    };
    createAndStartServer(NO_FILTERS);

    Truth.assertThat(server.getPort()).isEqualTo(65535);
  }

  @Test
  public void getPortBeforeStartedFails() {
    transportServer = new SimpleServer();
    createServer(NO_FILTERS);
    thrown.expect(IllegalStateException.class);
    thrown.expectMessage("started");
    server.getPort();
  }

  @Test
  public void getPortAfterTerminationFails() throws Exception {
    transportServer = new SimpleServer();
    createAndStartServer(NO_FILTERS);
    server.shutdown();
    server.awaitTermination();
    thrown.expect(IllegalStateException.class);
    thrown.expectMessage("terminated");
    server.getPort();
  }

  @Test
  public void handlerRegistryPriorities() throws Exception {
    fallbackRegistry = mock(HandlerRegistry.class);
    MethodDescriptor<String, Integer> method1 = MethodDescriptor.<String, Integer>newBuilder()
        .setType(MethodDescriptor.MethodType.UNKNOWN)
        .setFullMethodName("Service1/Method1")
        .setRequestMarshaller(STRING_MARSHALLER)
        .setResponseMarshaller(INTEGER_MARSHALLER)
        .build();
    registry = new InternalHandlerRegistry.Builder()
        .addService(ServerServiceDefinition.builder(new ServiceDescriptor("Service1", method1))
            .addMethod(method1, callHandler).build())
        .build();
    transportServer = new SimpleServer();
    createAndStartServer(NO_FILTERS);

    ServerTransportListener transportListener
        = transportServer.registerNewServerTransport(new SimpleServerTransport());
    Metadata requestHeaders = new Metadata();
    StatsTraceContext statsTraceCtx =
        transportListener.methodDetermined("Waiter/serve", requestHeaders);
    assertNotNull(statsTraceCtx);
    when(stream.statsTraceContext()).thenReturn(statsTraceCtx);

    // This call will be handled by callHandler from the internal registry
    transportListener.streamCreated(stream, "Service1/Method1", requestHeaders);
    assertEquals(1, executor.runDueTasks());
    verify(callHandler).startCall(Matchers.<ServerCall<String, Integer>>anyObject(),
        Matchers.<Metadata>anyObject());
    // This call will be handled by the fallbackRegistry because it's not registred in the internal
    // registry.
    transportListener.streamCreated(stream, "Service1/Method2", requestHeaders);
    assertEquals(1, executor.runDueTasks());
    verify(fallbackRegistry).lookupMethod("Service1/Method2", null);

    verifyNoMoreInteractions(callHandler);
    verifyNoMoreInteractions(fallbackRegistry);
  }

  @Test
  public void messageRead_errorCancelsCall() throws Exception {
    JumpToApplicationThreadServerStreamListener listener
        = new JumpToApplicationThreadServerStreamListener(
            executor.getScheduledExecutorService(), stream, Context.ROOT.withCancellation());
    ServerStreamListener mockListener = mock(ServerStreamListener.class);
    listener.setListener(mockListener);

    Throwable expectedT = new AssertionError();
    doThrow(expectedT).when(mockListener).messageRead(any(InputStream.class));
    // Closing the InputStream is done by the delegated listener (generally ServerCallImpl)
    listener.messageRead(mock(InputStream.class));
    try {
      executor.runDueTasks();
      fail("Expected exception");
    } catch (Throwable t) {
      assertSame(expectedT, t);
      verify(stream).close(statusCaptor.capture(), any(Metadata.class));
      assertSame(expectedT, statusCaptor.getValue().getCause());
    }
  }

  @Test
  public void messageRead_runtimeExceptionCancelsCall() throws Exception {
    JumpToApplicationThreadServerStreamListener listener
        = new JumpToApplicationThreadServerStreamListener(
            executor.getScheduledExecutorService(), stream, Context.ROOT.withCancellation());
    ServerStreamListener mockListener = mock(ServerStreamListener.class);
    listener.setListener(mockListener);

    Throwable expectedT = new RuntimeException();
    doThrow(expectedT).when(mockListener).messageRead(any(InputStream.class));
    // Closing the InputStream is done by the delegated listener (generally ServerCallImpl)
    listener.messageRead(mock(InputStream.class));
    try {
      executor.runDueTasks();
      fail("Expected exception");
    } catch (Throwable t) {
      assertSame(expectedT, t);
      verify(stream).close(statusCaptor.capture(), any(Metadata.class));
      assertSame(expectedT, statusCaptor.getValue().getCause());
    }
  }

  @Test
  public void halfClosed_errorCancelsCall() {
    JumpToApplicationThreadServerStreamListener listener
        = new JumpToApplicationThreadServerStreamListener(
            executor.getScheduledExecutorService(), stream, Context.ROOT.withCancellation());
    ServerStreamListener mockListener = mock(ServerStreamListener.class);
    listener.setListener(mockListener);

    Throwable expectedT = new AssertionError();
    doThrow(expectedT).when(mockListener).halfClosed();
    listener.halfClosed();
    try {
      executor.runDueTasks();
      fail("Expected exception");
    } catch (Throwable t) {
      assertSame(expectedT, t);
      verify(stream).close(statusCaptor.capture(), any(Metadata.class));
      assertSame(expectedT, statusCaptor.getValue().getCause());
    }
  }

  @Test
  public void halfClosed_runtimeExceptionCancelsCall() {
    JumpToApplicationThreadServerStreamListener listener
        = new JumpToApplicationThreadServerStreamListener(
            executor.getScheduledExecutorService(), stream, Context.ROOT.withCancellation());
    ServerStreamListener mockListener = mock(ServerStreamListener.class);
    listener.setListener(mockListener);

    Throwable expectedT = new RuntimeException();
    doThrow(expectedT).when(mockListener).halfClosed();
    listener.halfClosed();
    try {
      executor.runDueTasks();
      fail("Expected exception");
    } catch (Throwable t) {
      assertSame(expectedT, t);
      verify(stream).close(statusCaptor.capture(), any(Metadata.class));
      assertSame(expectedT, statusCaptor.getValue().getCause());
    }
  }

  @Test
  public void onReady_errorCancelsCall() {
    JumpToApplicationThreadServerStreamListener listener
        = new JumpToApplicationThreadServerStreamListener(
            executor.getScheduledExecutorService(), stream, Context.ROOT.withCancellation());
    ServerStreamListener mockListener = mock(ServerStreamListener.class);
    listener.setListener(mockListener);

    Throwable expectedT = new AssertionError();
    doThrow(expectedT).when(mockListener).onReady();
    listener.onReady();
    try {
      executor.runDueTasks();
      fail("Expected exception");
    } catch (Throwable t) {
      assertSame(expectedT, t);
      verify(stream).close(statusCaptor.capture(), any(Metadata.class));
      assertSame(expectedT, statusCaptor.getValue().getCause());
    }
  }

  @Test
  public void onReady_runtimeExceptionCancelsCall() {
    JumpToApplicationThreadServerStreamListener listener
        = new JumpToApplicationThreadServerStreamListener(
            executor.getScheduledExecutorService(), stream, Context.ROOT.withCancellation());
    ServerStreamListener mockListener = mock(ServerStreamListener.class);
    listener.setListener(mockListener);

    Throwable expectedT = new RuntimeException();
    doThrow(expectedT).when(mockListener).onReady();
    listener.onReady();
    try {
      executor.runDueTasks();
      fail("Expected exception");
    } catch (Throwable t) {
      assertSame(expectedT, t);
      verify(stream).close(statusCaptor.capture(), any(Metadata.class));
      assertSame(expectedT, statusCaptor.getValue().getCause());
    }
  }

  private void createAndStartServer(List<ServerTransportFilter> filters) throws IOException {
    createServer(filters);
    server.start();
  }

  private void createServer(List<ServerTransportFilter> filters) {
    assertNull(server);
    server = new ServerImpl(executorPool, timerPool, registry, fallbackRegistry,
        transportServer, SERVER_CONTEXT, decompressorRegistry, compressorRegistry, filters,
        statsCtxFactory, GrpcUtil.STOPWATCH_SUPPLIER);
  }

  private void verifyExecutorsAcquired() {
    verify(executorPool).getObject();
    verify(timerPool).getObject();
    verifyNoMoreInteractions(executorPool);
    verifyNoMoreInteractions(timerPool);
  }

  private void verifyExecutorsNotReturned() {
    verify(executorPool, never()).returnObject(any(Executor.class));
    verify(timerPool, never()).returnObject(any(ScheduledExecutorService.class));
  }

  private void verifyExecutorsReturned() {
    verify(executorPool).returnObject(same(executor.getScheduledExecutorService()));
    verify(timerPool).returnObject(same(timer.getScheduledExecutorService()));
    verifyNoMoreInteractions(executorPool);
    verifyNoMoreInteractions(timerPool);
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

    @Override
    public void shutdownNow(Status status) {
      listener.transportTerminated();
    }

    @Override
    public LogId getLogId() {
      throw new UnsupportedOperationException();
    }
  }
}
