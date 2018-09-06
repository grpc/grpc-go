/*
 * Copyright 2014 The gRPC Authors
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
import static io.grpc.InternalChannelz.id;
import static io.grpc.internal.GrpcUtil.MESSAGE_ENCODING_KEY;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNotSame;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.AdditionalAnswers.delegatesTo;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyString;
import static org.mockito.Matchers.eq;
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

import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.SettableFuture;
import io.grpc.Attributes;
import io.grpc.BinaryLog;
import io.grpc.Channel;
import io.grpc.Compressor;
import io.grpc.Context;
import io.grpc.Grpc;
import io.grpc.HandlerRegistry;
import io.grpc.IntegerMarshaller;
import io.grpc.InternalChannelz;
import io.grpc.InternalChannelz.ServerSocketsList;
import io.grpc.InternalChannelz.SocketStats;
import io.grpc.InternalInstrumented;
import io.grpc.InternalLogId;
import io.grpc.InternalServerInterceptors;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.ServerCall;
import io.grpc.ServerCall.Listener;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptor;
import io.grpc.ServerMethodDefinition;
import io.grpc.ServerServiceDefinition;
import io.grpc.ServerStreamTracer;
import io.grpc.ServerTransportFilter;
import io.grpc.ServiceDescriptor;
import io.grpc.Status;
import io.grpc.StringMarshaller;
import io.grpc.internal.ServerImpl.JumpToApplicationThreadServerStreamListener;
import io.grpc.internal.testing.SingleMessageProducer;
import io.grpc.internal.testing.TestServerStreamTracer;
import io.grpc.util.MutableHandlerRegistry;
import java.io.ByteArrayInputStream;
import java.io.File;
import java.io.IOException;
import java.io.InputStream;
import java.net.SocketAddress;
import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedList;
import java.util.List;
import java.util.concurrent.CyclicBarrier;
import java.util.concurrent.Executor;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;
import javax.annotation.Nullable;
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
  private static final MethodDescriptor<String, Integer> METHOD =
      MethodDescriptor.<String, Integer>newBuilder()
          .setType(MethodDescriptor.MethodType.UNKNOWN)
          .setFullMethodName("Waiter/serve")
          .setRequestMarshaller(STRING_MARSHALLER)
          .setResponseMarshaller(INTEGER_MARSHALLER)
          .build();
  private static final Context.Key<String> SERVER_ONLY = Context.key("serverOnly");
  private static final Context.Key<String> SERVER_TRACER_ADDED_KEY = Context.key("tracer-added");
  private static final Context.CancellableContext SERVER_CONTEXT =
      Context.ROOT.withValue(SERVER_ONLY, "yes").withCancellation();
  private static final FakeClock.TaskFilter CONTEXT_CLOSER_TASK_FITLER =
      new FakeClock.TaskFilter() {
        @Override
        public boolean shouldAccept(Runnable runnable) {
          return runnable instanceof ServerImpl.ContextCloser;
        }
      };
  private static final String AUTHORITY = "some_authority";

  @Rule public final ExpectedException thrown = ExpectedException.none();

  @BeforeClass
  public static void beforeStartUp() {
    // Cancel the root context. Server will fork it so the per-call context should not
    // be cancelled.
    SERVER_CONTEXT.cancel(null);
  }

  private final FakeClock executor = new FakeClock();
  private final FakeClock timer = new FakeClock();
  private final InternalChannelz channelz = new InternalChannelz();

  @Mock
  private ServerStreamTracer.Factory streamTracerFactory;
  private List<ServerStreamTracer.Factory> streamTracerFactories;
  private final TestServerStreamTracer streamTracer = new TestServerStreamTracer() {
      @Override
      public Context filterContext(Context context) {
        Context newCtx = super.filterContext(context);
        return newCtx.withValue(SERVER_TRACER_ADDED_KEY, "context added by tracer");
      }
    };
  @Mock
  private ObjectPool<Executor> executorPool;
  private Builder builder = new Builder();
  private MutableHandlerRegistry mutableFallbackRegistry = new MutableHandlerRegistry();
  private HandlerRegistry fallbackRegistry = mock(
      HandlerRegistry.class,
      delegatesTo(new HandlerRegistry() {
        @Override
        public ServerMethodDefinition<?, ?> lookupMethod(
            String methodName, @Nullable String authority) {
          return mutableFallbackRegistry.lookupMethod(methodName, authority);
        }

        @Override
        public List<ServerServiceDefinition> getServices() {
          return mutableFallbackRegistry.getServices();
        }
      }));
  private SimpleServer transportServer = new SimpleServer();
  private ServerImpl server;

  @Captor
  private ArgumentCaptor<Status> statusCaptor;
  @Captor
  private ArgumentCaptor<Metadata> metadataCaptor;
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
    builder.channelz = channelz;
    streamTracerFactories = Arrays.asList(streamTracerFactory);
    when(executorPool.getObject()).thenReturn(executor.getScheduledExecutorService());
    when(streamTracerFactory.newServerStreamTracer(anyString(), any(Metadata.class)))
        .thenReturn(streamTracer);
    when(stream.getAuthority()).thenReturn(AUTHORITY);
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
    createAndStartServer();
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
    createServer();
    server.shutdown();
    assertTrue(server.isShutdown());
    assertTrue(server.isTerminated());
    verifyNoMoreInteractions(executorPool);
  }

  @Test
  public void startStopImmediateWithChildTransport() throws IOException {
    createAndStartServer();
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
    createAndStartServer();
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
    createAndStartServer();
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
    createAndStartServer();
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
    createServer();
    try {
      server.start();
      fail("expected exception");
    } catch (IOException e) {
      assertSame(ex, e);
    }
    verifyNoMoreInteractions(executorPool);
  }

  @Test
  public void transportHandshakeTimeout_expired() throws Exception {
    class ShutdownRecordingTransport extends SimpleServerTransport {
      Status shutdownNowStatus;

      @Override public void shutdownNow(Status status) {
        shutdownNowStatus = status;
        super.shutdownNow(status);
      }
    }

    builder.handshakeTimeout(60, TimeUnit.SECONDS);
    createAndStartServer();
    ShutdownRecordingTransport serverTransport = new ShutdownRecordingTransport();
    transportServer.registerNewServerTransport(serverTransport);
    timer.forwardTime(59, TimeUnit.SECONDS);
    assertNull("shutdownNow status", serverTransport.shutdownNowStatus);
    // Don't call transportReady() in time
    timer.forwardTime(2, TimeUnit.SECONDS);
    assertNotNull("shutdownNow status", serverTransport.shutdownNowStatus);
  }

  @Test
  public void methodNotFound() throws Exception {
    createAndStartServer();
    ServerTransportListener transportListener
        = transportServer.registerNewServerTransport(new SimpleServerTransport());
    transportListener.transportReady(Attributes.EMPTY);
    Metadata requestHeaders = new Metadata();
    StatsTraceContext statsTraceCtx =
        StatsTraceContext.newServerContext(
            streamTracerFactories, "Waiter/nonexist", requestHeaders);
    when(stream.statsTraceContext()).thenReturn(statsTraceCtx);
    transportListener.streamCreated(stream, "Waiter/nonexist", requestHeaders);
    verify(stream).setListener(isA(ServerStreamListener.class));
    verify(stream, atLeast(1)).statsTraceContext();

    assertEquals(1, executor.runDueTasks());
    verify(stream).close(statusCaptor.capture(), any(Metadata.class));
    Status status = statusCaptor.getValue();
    assertEquals(Status.Code.UNIMPLEMENTED, status.getCode());
    assertEquals("Method not found: Waiter/nonexist", status.getDescription());

    verify(streamTracerFactory).newServerStreamTracer(eq("Waiter/nonexist"), same(requestHeaders));
    assertNull(streamTracer.getServerCallInfo());
    assertEquals(Status.Code.UNIMPLEMENTED, statusCaptor.getValue().getCode());
  }

  @Test
  public void decompressorNotFound() throws Exception {
    String decompressorName = "NON_EXISTENT_DECOMPRESSOR";
    createAndStartServer();
    ServerTransportListener transportListener
        = transportServer.registerNewServerTransport(new SimpleServerTransport());
    transportListener.transportReady(Attributes.EMPTY);
    Metadata requestHeaders = new Metadata();
    requestHeaders.put(MESSAGE_ENCODING_KEY, decompressorName);
    StatsTraceContext statsTraceCtx =
        StatsTraceContext.newServerContext(
            streamTracerFactories, "Waiter/nonexist", requestHeaders);
    when(stream.statsTraceContext()).thenReturn(statsTraceCtx);

    transportListener.streamCreated(stream, "Waiter/nonexist", requestHeaders);

    verify(stream).close(statusCaptor.capture(), any(Metadata.class));
    Status status = statusCaptor.getValue();
    assertEquals(Status.Code.UNIMPLEMENTED, status.getCode());
    assertEquals("Can't find decompressor for " + decompressorName, status.getDescription());
    verifyNoMoreInteractions(stream);
  }

  @Test
  public void basicExchangeSuccessful() throws Exception {
    createAndStartServer();
    basicExchangeHelper(METHOD, "Lots of pizza, please", 314, 50);
  }

  private void basicExchangeHelper(
      MethodDescriptor<String, Integer> method,
      String request,
      int firstResponse,
      Integer extraResponse) throws Exception {
    final Metadata.Key<String> metadataKey
        = Metadata.Key.of("inception", Metadata.ASCII_STRING_MARSHALLER);
    final AtomicReference<ServerCall<String, Integer>> callReference
        = new AtomicReference<ServerCall<String, Integer>>();
    final AtomicReference<Context> callContextReference = new AtomicReference<Context>();
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
                callContextReference.set(Context.current());
                return callListener;
              }
            }).build());
    ServerTransportListener transportListener
        = transportServer.registerNewServerTransport(new SimpleServerTransport());
    transportListener.transportReady(Attributes.EMPTY);

    Metadata requestHeaders = new Metadata();
    requestHeaders.put(metadataKey, "value");
    StatsTraceContext statsTraceCtx =
        StatsTraceContext.newServerContext(streamTracerFactories, "Waiter/serve", requestHeaders);
    when(stream.statsTraceContext()).thenReturn(statsTraceCtx);

    transportListener.streamCreated(stream, "Waiter/serve", requestHeaders);
    verify(stream).setListener(streamListenerCaptor.capture());
    ServerStreamListener streamListener = streamListenerCaptor.getValue();
    assertNotNull(streamListener);
    verify(stream, atLeast(1)).statsTraceContext();
    verify(fallbackRegistry, never()).lookupMethod(any(String.class), any(String.class));

    assertEquals(1, executor.runDueTasks());
    ServerCall<String, Integer> call = callReference.get();
    assertNotNull(call);
    assertEquals(
        new ServerCallInfoImpl<String, Integer>(
            call.getMethodDescriptor(),
            call.getAttributes(),
            call.getAuthority()),
        streamTracer.getServerCallInfo());
    verify(fallbackRegistry).lookupMethod("Waiter/serve", AUTHORITY);
    Context callContext = callContextReference.get();
    assertNotNull(callContext);
    assertEquals("context added by tracer", SERVER_TRACER_ADDED_KEY.get(callContext));

    streamListener.messagesAvailable(new SingleMessageProducer(STRING_MARSHALLER.stream(request)));
    assertEquals(1, executor.runDueTasks());
    verify(callListener).onMessage(request);

    Metadata responseHeaders = new Metadata();
    responseHeaders.put(metadataKey, "response value");
    call.sendHeaders(responseHeaders);
    verify(stream).writeHeaders(responseHeaders);
    verify(stream).setCompressor(isA(Compressor.class));

    call.sendMessage(firstResponse);
    ArgumentCaptor<InputStream> inputCaptor = ArgumentCaptor.forClass(InputStream.class);
    verify(stream).writeMessage(inputCaptor.capture());
    verify(stream).flush();
    assertEquals(firstResponse, INTEGER_MARSHALLER.parse(inputCaptor.getValue()).intValue());

    streamListener.halfClosed(); // All full; no dessert.
    assertEquals(1, executor.runDueTasks());
    verify(callListener).onHalfClose();

    if (extraResponse != null) {
      call.sendMessage(extraResponse);
      verify(stream, times(2)).writeMessage(inputCaptor.capture());
      verify(stream, times(2)).flush();
      assertEquals(
          (int) extraResponse, INTEGER_MARSHALLER.parse(inputCaptor.getValue()).intValue());
    }

    Metadata trailers = new Metadata();
    trailers.put(metadataKey, "another value");
    Status status = Status.OK.withDescription("A okay");
    call.close(status, trailers);
    verify(stream).close(status, trailers);

    streamListener.closed(Status.OK);
    assertEquals(1, executor.runDueTasks());
    verify(callListener).onComplete();

    verify(stream, atLeast(1)).statsTraceContext();
    verifyNoMoreInteractions(callListener);

    verify(streamTracerFactory).newServerStreamTracer(eq("Waiter/serve"), same(requestHeaders));
  }

  @Test
  public void transportFilters() throws Exception {
    final SocketAddress remoteAddr = mock(SocketAddress.class);
    final Attributes.Key<String> key1 = Attributes.Key.create("test-key1");
    final Attributes.Key<String> key2 = Attributes.Key.create("test-key2");
    final Attributes.Key<String> key3 = Attributes.Key.create("test-key3");
    final AtomicReference<Attributes> filter1TerminationCallbackArgument =
        new AtomicReference<Attributes>();
    final AtomicReference<Attributes> filter2TerminationCallbackArgument =
        new AtomicReference<Attributes>();
    final AtomicInteger readyCallbackCalled = new AtomicInteger(0);
    final AtomicInteger terminationCallbackCalled = new AtomicInteger(0);
    builder.addTransportFilter(new ServerTransportFilter() {
        @Override
        public Attributes transportReady(Attributes attrs) {
          assertEquals(Attributes.newBuilder()
              .set(Grpc.TRANSPORT_ATTR_REMOTE_ADDR, remoteAddr)
              .build(), attrs);
          readyCallbackCalled.incrementAndGet();
          return attrs.toBuilder()
              .set(key1, "yalayala")
              .set(key2, "blabla")
              .build();
        }

        @Override
        public void transportTerminated(Attributes attrs) {
          terminationCallbackCalled.incrementAndGet();
          filter1TerminationCallbackArgument.set(attrs);
        }
      });
    builder.addTransportFilter(new ServerTransportFilter() {
        @Override
        public Attributes transportReady(Attributes attrs) {
          assertEquals(Attributes.newBuilder()
              .set(Grpc.TRANSPORT_ATTR_REMOTE_ADDR, remoteAddr)
              .set(key1, "yalayala")
              .set(key2, "blabla")
              .build(), attrs);
          readyCallbackCalled.incrementAndGet();
          return attrs.toBuilder()
              .set(key1, "ouch")
              .set(key3, "puff")
              .build();
        }

        @Override
        public void transportTerminated(Attributes attrs) {
          terminationCallbackCalled.incrementAndGet();
          filter2TerminationCallbackArgument.set(attrs);
        }
      });
    Attributes expectedTransportAttrs = Attributes.newBuilder()
        .set(key1, "ouch")
        .set(key2, "blabla")
        .set(key3, "puff")
        .set(Grpc.TRANSPORT_ATTR_REMOTE_ADDR, remoteAddr)
        .build();

    createAndStartServer();
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
  public void interceptors() throws Exception {
    final LinkedList<Context> capturedContexts = new LinkedList<Context>();
    final Context.Key<String> key1 = Context.key("key1");
    final Context.Key<String> key2 = Context.key("key2");
    final Context.Key<String> key3 = Context.key("key3");
    ServerInterceptor intercepter1 = new ServerInterceptor() {
        @Override
        public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
            ServerCall<ReqT, RespT> call,
            Metadata headers,
            ServerCallHandler<ReqT, RespT> next) {
          Context ctx = Context.current().withValue(key1, "value1");
          Context origCtx = ctx.attach();
          try {
            capturedContexts.add(ctx);
            return next.startCall(call, headers);
          } finally {
            ctx.detach(origCtx);
          }
        }
      };
    ServerInterceptor intercepter2 = new ServerInterceptor() {
        @Override
        public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
            ServerCall<ReqT, RespT> call,
            Metadata headers,
            ServerCallHandler<ReqT, RespT> next) {
          Context ctx = Context.current().withValue(key2, "value2");
          Context origCtx = ctx.attach();
          try {
            capturedContexts.add(ctx);
            return next.startCall(call, headers);
          } finally {
            ctx.detach(origCtx);
          }
        }
      };
    ServerCallHandler<String, Integer> callHandler = new ServerCallHandler<String, Integer>() {
        @Override
        public ServerCall.Listener<String> startCall(
            ServerCall<String, Integer> call,
            Metadata headers) {
          capturedContexts.add(Context.current().withValue(key3, "value3"));
          return callListener;
        }
      };

    mutableFallbackRegistry.addService(
        ServerServiceDefinition.builder(new ServiceDescriptor("Waiter", METHOD))
            .addMethod(METHOD, callHandler).build());
    builder.intercept(intercepter2);
    builder.intercept(intercepter1);
    createServer();
    server.start();

    ServerTransportListener transportListener
        = transportServer.registerNewServerTransport(new SimpleServerTransport());
    transportListener.transportReady(Attributes.EMPTY);

    Metadata requestHeaders = new Metadata();
    StatsTraceContext statsTraceCtx =
        StatsTraceContext.newServerContext(streamTracerFactories, "Waiter/serve", requestHeaders);
    when(stream.statsTraceContext()).thenReturn(statsTraceCtx);

    transportListener.streamCreated(stream, "Waiter/serve", requestHeaders);
    assertEquals(1, executor.runDueTasks());

    Context ctx1 = capturedContexts.poll();
    assertEquals("value1", key1.get(ctx1));
    assertNull(key2.get(ctx1));
    assertNull(key3.get(ctx1));

    Context ctx2 = capturedContexts.poll();
    assertEquals("value1", key1.get(ctx2));
    assertEquals("value2", key2.get(ctx2));
    assertNull(key3.get(ctx2));

    Context ctx3 = capturedContexts.poll();
    assertEquals("value1", key1.get(ctx3));
    assertEquals("value2", key2.get(ctx3));
    assertEquals("value3", key3.get(ctx3));

    assertTrue(capturedContexts.isEmpty());
  }

  @Test
  public void exceptionInStartCallPropagatesToStream() throws Exception {
    createAndStartServer();
    final Status status = Status.ABORTED.withDescription("Oh, no!");
    mutableFallbackRegistry.addService(ServerServiceDefinition.builder(
        new ServiceDescriptor("Waiter", METHOD))
        .addMethod(METHOD,
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
    transportListener.transportReady(Attributes.EMPTY);

    Metadata requestHeaders = new Metadata();
    StatsTraceContext statsTraceCtx =
        StatsTraceContext.newServerContext(streamTracerFactories, "Waiter/serve", requestHeaders);
    when(stream.statsTraceContext()).thenReturn(statsTraceCtx);

    transportListener.streamCreated(stream, "Waiter/serve", requestHeaders);
    verify(stream).setListener(streamListenerCaptor.capture());
    ServerStreamListener streamListener = streamListenerCaptor.getValue();
    assertNotNull(streamListener);
    verify(stream, atLeast(1)).statsTraceContext();
    verifyNoMoreInteractions(stream);
    verify(fallbackRegistry, never()).lookupMethod(any(String.class), any(String.class));

    assertEquals(1, executor.runDueTasks());
    verify(fallbackRegistry).lookupMethod("Waiter/serve", AUTHORITY);
    verify(stream).close(same(status), notNull(Metadata.class));
    verify(stream, atLeast(1)).statsTraceContext();
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
    createAndStartServer();
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
    createAndStartServer();
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
    createAndStartServer();
    final AtomicBoolean  onReadyCalled = new AtomicBoolean(false);
    final AtomicBoolean onMessageCalled = new AtomicBoolean(false);
    final AtomicBoolean onHalfCloseCalled = new AtomicBoolean(false);
    final AtomicBoolean onCancelCalled = new AtomicBoolean(false);
    mutableFallbackRegistry.addService(ServerServiceDefinition.builder(
        new ServiceDescriptor("Waiter", METHOD))
        .addMethod(
            METHOD,
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
    transportListener.transportReady(Attributes.EMPTY);

    Metadata requestHeaders = new Metadata();
    StatsTraceContext statsTraceCtx =
        StatsTraceContext.newServerContext(streamTracerFactories, "Waitier/serve", requestHeaders);
    when(stream.statsTraceContext()).thenReturn(statsTraceCtx);

    transportListener.streamCreated(stream, "Waiter/serve", requestHeaders);
    verify(stream).setListener(streamListenerCaptor.capture());
    ServerStreamListener streamListener = streamListenerCaptor.getValue();
    assertNotNull(streamListener);

    streamListener.onReady();
    assertEquals(1, executor.runDueTasks());
    assertTrue(onReadyCalled.get());

    streamListener
        .messagesAvailable(new SingleMessageProducer(new ByteArrayInputStream(new byte[0])));
    assertEquals(1, executor.runDueTasks());
    assertTrue(onMessageCalled.get());

    streamListener.halfClosed();
    assertEquals(1, executor.runDueTasks());
    assertTrue(onHalfCloseCalled.get());

    streamListener.closed(Status.CANCELLED);
    assertEquals(1, executor.numPendingTasks(CONTEXT_CLOSER_TASK_FITLER));
    assertEquals(2, executor.runDueTasks());
    assertTrue(onCancelCalled.get());

    // Close should never be called if asserts in listener pass.
    verify(stream, times(0)).close(isA(Status.class), isNotNull(Metadata.class));
  }

  private ServerStreamListener testClientClose_setup(
      final AtomicReference<ServerCall<String, Integer>> callReference,
      final AtomicReference<Context> context,
      final AtomicBoolean contextCancelled) throws Exception {
    createAndStartServer();
    callListener = new ServerCall.Listener<String>() {
      @Override
      public void onReady() {
        context.set(Context.current());
        Context.current().addListener(new Context.CancellationListener() {
          @Override
          public void cancelled(Context context) {
            contextCancelled.set(true);
          }
        }, MoreExecutors.directExecutor());
      }
    };

    mutableFallbackRegistry.addService(ServerServiceDefinition.builder(
        new ServiceDescriptor("Waiter", METHOD))
        .addMethod(METHOD,
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
    transportListener.transportReady(Attributes.EMPTY);
    Metadata requestHeaders = new Metadata();
    StatsTraceContext statsTraceCtx =
        StatsTraceContext.newServerContext(streamTracerFactories, "Waitier/serve", requestHeaders);
    when(stream.statsTraceContext()).thenReturn(statsTraceCtx);
    transportListener.streamCreated(stream, "Waiter/serve", requestHeaders);
    verify(stream).setListener(streamListenerCaptor.capture());
    ServerStreamListener streamListener = streamListenerCaptor.getValue();
    assertNotNull(streamListener);

    streamListener.onReady();
    assertEquals(1, executor.runDueTasks());
    return streamListener;
  }

  @Test
  public void testClientClose_cancelTriggersImmediateCancellation() throws Exception {
    AtomicBoolean contextCancelled = new AtomicBoolean(false);
    AtomicReference<Context> context = new AtomicReference<Context>();
    AtomicReference<ServerCall<String, Integer>> callReference
        = new AtomicReference<ServerCall<String, Integer>>();

    ServerStreamListener streamListener = testClientClose_setup(callReference,
        context, contextCancelled);

    // For close status being non OK:
    // isCancelled is expected to be true immediately after calling closed(), without needing
    // to wait for the main executor to run any tasks.
    assertFalse(callReference.get().isCancelled());
    assertFalse(context.get().isCancelled());
    streamListener.closed(Status.CANCELLED);
    assertEquals(1, executor.numPendingTasks(CONTEXT_CLOSER_TASK_FITLER));
    assertEquals(2, executor.runDueTasks());
    assertTrue(callReference.get().isCancelled());
    assertTrue(context.get().isCancelled());
    assertTrue(contextCancelled.get());
  }

  @Test
  public void testClientClose_OkTriggersDelayedCancellation() throws Exception {
    AtomicBoolean contextCancelled = new AtomicBoolean(false);
    AtomicReference<Context> context = new AtomicReference<Context>();
    AtomicReference<ServerCall<String, Integer>> callReference
        = new AtomicReference<ServerCall<String, Integer>>();

    ServerStreamListener streamListener = testClientClose_setup(callReference,
        context, contextCancelled);

    // For close status OK:
    // isCancelled is expected to be true after all pending work is done
    assertFalse(callReference.get().isCancelled());
    assertFalse(context.get().isCancelled());
    streamListener.closed(Status.OK);
    assertFalse(callReference.get().isCancelled());
    assertFalse(context.get().isCancelled());

    assertEquals(1, executor.runDueTasks());
    assertTrue(callReference.get().isCancelled());
    assertTrue(context.get().isCancelled());
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
    createAndStartServer();

    assertThat(server.getPort()).isEqualTo(65535);
  }

  @Test
  public void getPortBeforeStartedFails() {
    transportServer = new SimpleServer();
    createServer();
    thrown.expect(IllegalStateException.class);
    thrown.expectMessage("started");
    server.getPort();
  }

  @Test
  public void getPortAfterTerminationFails() throws Exception {
    transportServer = new SimpleServer();
    createAndStartServer();
    server.shutdown();
    server.awaitTermination();
    thrown.expect(IllegalStateException.class);
    thrown.expectMessage("terminated");
    server.getPort();
  }

  @Test
  public void handlerRegistryPriorities() throws Exception {
    fallbackRegistry = mock(HandlerRegistry.class);
    builder.addService(
        ServerServiceDefinition.builder(new ServiceDescriptor("Waiter", METHOD))
            .addMethod(METHOD, callHandler).build());
    transportServer = new SimpleServer();
    createAndStartServer();

    ServerTransportListener transportListener
        = transportServer.registerNewServerTransport(new SimpleServerTransport());
    transportListener.transportReady(Attributes.EMPTY);
    Metadata requestHeaders = new Metadata();
    StatsTraceContext statsTraceCtx =
        StatsTraceContext.newServerContext(streamTracerFactories, "Waiter/serve", requestHeaders);
    when(stream.statsTraceContext()).thenReturn(statsTraceCtx);

    // This call will be handled by callHandler from the internal registry
    transportListener.streamCreated(stream, "Waiter/serve", requestHeaders);
    assertEquals(1, executor.runDueTasks());
    verify(callHandler).startCall(Matchers.<ServerCall<String, Integer>>anyObject(),
        Matchers.<Metadata>anyObject());
    // This call will be handled by the fallbackRegistry because it's not registred in the internal
    // registry.
    transportListener.streamCreated(stream, "Service1/Method2", requestHeaders);
    assertEquals(1, executor.runDueTasks());
    verify(fallbackRegistry).lookupMethod("Service1/Method2", AUTHORITY);

    verifyNoMoreInteractions(callHandler);
    verifyNoMoreInteractions(fallbackRegistry);
  }

  @Test
  public void messageRead_errorCancelsCall() throws Exception {
    JumpToApplicationThreadServerStreamListener listener
        = new JumpToApplicationThreadServerStreamListener(
            executor.getScheduledExecutorService(),
            executor.getScheduledExecutorService(),
            stream,
            Context.ROOT.withCancellation());
    ServerStreamListener mockListener = mock(ServerStreamListener.class);
    listener.setListener(mockListener);

    TestError expectedT = new TestError();
    doThrow(expectedT).when(mockListener)
        .messagesAvailable(any(StreamListener.MessageProducer.class));
    // Closing the InputStream is done by the delegated listener (generally ServerCallImpl)
    listener.messagesAvailable(mock(StreamListener.MessageProducer.class));
    try {
      executor.runDueTasks();
      fail("Expected exception");
    } catch (TestError t) {
      assertSame(expectedT, t);
      ensureServerStateNotLeaked();
    }
  }

  @Test
  public void messageRead_runtimeExceptionCancelsCall() throws Exception {
    JumpToApplicationThreadServerStreamListener listener
        = new JumpToApplicationThreadServerStreamListener(
            executor.getScheduledExecutorService(),
            executor.getScheduledExecutorService(),
            stream,
            Context.ROOT.withCancellation());
    ServerStreamListener mockListener = mock(ServerStreamListener.class);
    listener.setListener(mockListener);

    RuntimeException expectedT = new RuntimeException();
    doThrow(expectedT).when(mockListener)
        .messagesAvailable(any(StreamListener.MessageProducer.class));
    // Closing the InputStream is done by the delegated listener (generally ServerCallImpl)
    listener.messagesAvailable(mock(StreamListener.MessageProducer.class));
    try {
      executor.runDueTasks();
      fail("Expected exception");
    } catch (RuntimeException t) {
      assertSame(expectedT, t);
      ensureServerStateNotLeaked();
    }
  }

  @Test
  public void halfClosed_errorCancelsCall() {
    JumpToApplicationThreadServerStreamListener listener
        = new JumpToApplicationThreadServerStreamListener(
            executor.getScheduledExecutorService(),
            executor.getScheduledExecutorService(),
            stream,
            Context.ROOT.withCancellation());
    ServerStreamListener mockListener = mock(ServerStreamListener.class);
    listener.setListener(mockListener);

    TestError expectedT = new TestError();
    doThrow(expectedT).when(mockListener).halfClosed();
    listener.halfClosed();
    try {
      executor.runDueTasks();
      fail("Expected exception");
    } catch (TestError t) {
      assertSame(expectedT, t);
      ensureServerStateNotLeaked();
    }
  }

  @Test
  public void halfClosed_runtimeExceptionCancelsCall() {
    JumpToApplicationThreadServerStreamListener listener
        = new JumpToApplicationThreadServerStreamListener(
            executor.getScheduledExecutorService(),
            executor.getScheduledExecutorService(),
            stream,
            Context.ROOT.withCancellation());
    ServerStreamListener mockListener = mock(ServerStreamListener.class);
    listener.setListener(mockListener);

    RuntimeException expectedT = new RuntimeException();
    doThrow(expectedT).when(mockListener).halfClosed();
    listener.halfClosed();
    try {
      executor.runDueTasks();
      fail("Expected exception");
    } catch (RuntimeException t) {
      assertSame(expectedT, t);
      ensureServerStateNotLeaked();
    }
  }

  @Test
  public void onReady_errorCancelsCall() {
    JumpToApplicationThreadServerStreamListener listener
        = new JumpToApplicationThreadServerStreamListener(
            executor.getScheduledExecutorService(),
            executor.getScheduledExecutorService(),
            stream,
            Context.ROOT.withCancellation());
    ServerStreamListener mockListener = mock(ServerStreamListener.class);
    listener.setListener(mockListener);

    TestError expectedT = new TestError();
    doThrow(expectedT).when(mockListener).onReady();
    listener.onReady();
    try {
      executor.runDueTasks();
      fail("Expected exception");
    } catch (TestError t) {
      assertSame(expectedT, t);
      ensureServerStateNotLeaked();
    }
  }

  @Test
  public void onReady_runtimeExceptionCancelsCall() {
    JumpToApplicationThreadServerStreamListener listener
        = new JumpToApplicationThreadServerStreamListener(
            executor.getScheduledExecutorService(),
            executor.getScheduledExecutorService(),
            stream,
            Context.ROOT.withCancellation());
    ServerStreamListener mockListener = mock(ServerStreamListener.class);
    listener.setListener(mockListener);

    RuntimeException expectedT = new RuntimeException();
    doThrow(expectedT).when(mockListener).onReady();
    listener.onReady();
    try {
      executor.runDueTasks();
      fail("Expected exception");
    } catch (RuntimeException t) {
      assertSame(expectedT, t);
      ensureServerStateNotLeaked();
    }
  }

  @Test
  public void binaryLogInstalled() throws Exception {
    final SettableFuture<Boolean> intercepted = SettableFuture.create();
    final ServerInterceptor interceptor = new ServerInterceptor() {
      @Override
      public <ReqT, RespT> Listener<ReqT> interceptCall(ServerCall<ReqT, RespT> call,
          Metadata headers,
          ServerCallHandler<ReqT, RespT> next) {
        intercepted.set(true);
        return next.startCall(call, headers);
      }
    };

    builder.binlog = new BinaryLog() {
      @Override
      public void close() throws IOException {
        // noop
      }

      @Override
      public <ReqT, RespT> ServerMethodDefinition<?, ?> wrapMethodDefinition(
          ServerMethodDefinition<ReqT, RespT> oMethodDef) {
        return ServerMethodDefinition.create(
            oMethodDef.getMethodDescriptor(),
            InternalServerInterceptors.interceptCallHandlerCreate(
                interceptor,
                oMethodDef.getServerCallHandler()));
      }

      @Override
      public Channel wrapChannel(Channel channel) {
        return channel;
      }
    };
    createAndStartServer();
    basicExchangeHelper(METHOD, "Lots of pizza, please", 314, 50);
    assertTrue(intercepted.get());
  }

  @Test
  public void channelz_membership() throws Exception {
    createServer();
    assertTrue(builder.channelz.containsServer(server.getLogId()));
    server.shutdownNow().awaitTermination();
    assertFalse(builder.channelz.containsServer(server.getLogId()));
  }

  @Test
  public void channelz_serverStats() throws Exception {
    createAndStartServer();
    assertEquals(0, server.getStats().get().callsSucceeded);
    basicExchangeHelper(METHOD, "Lots of pizza, please", 314, null);
    assertEquals(1, server.getStats().get().callsSucceeded);
  }

  @Test
  public void channelz_transport_membershp() throws Exception {
    createAndStartServer();
    SimpleServerTransport transport = new SimpleServerTransport();

    ServerSocketsList before = builder.channelz
        .getServerSockets(id(server), id(transport), /*maxPageSize=*/ 1);
    assertThat(before.sockets).isEmpty();
    assertTrue(before.end);

    ServerTransportListener listener
        = transportServer.registerNewServerTransport(transport);
    ServerSocketsList added = builder.channelz
        .getServerSockets(id(server), id(transport), /*maxPageSize=*/ 1);
    assertThat(added.sockets).containsExactly(transport);
    assertTrue(before.end);

    listener.transportTerminated();
    ServerSocketsList after = builder.channelz
        .getServerSockets(id(server), id(transport), /*maxPageSize=*/ 1);
    assertThat(after.sockets).isEmpty();
    assertTrue(after.end);
  }

  private void createAndStartServer() throws IOException {
    createServer();
    server.start();
  }

  private void createServer() {
    assertNull(server);

    builder.fallbackHandlerRegistry(fallbackRegistry);
    builder.executorPool = executorPool;
    server = new ServerImpl(builder, transportServer, SERVER_CONTEXT);
  }

  private void verifyExecutorsAcquired() {
    verify(executorPool).getObject();
    verifyNoMoreInteractions(executorPool);
  }

  private void verifyExecutorsNotReturned() {
    verify(executorPool, never()).returnObject(any(Executor.class));
  }

  private void verifyExecutorsReturned() {
    verify(executorPool).returnObject(same(executor.getScheduledExecutorService()));
    verifyNoMoreInteractions(executorPool);
  }

  private void ensureServerStateNotLeaked() {
    verify(stream).close(statusCaptor.capture(), metadataCaptor.capture());
    assertEquals(Status.UNKNOWN, statusCaptor.getValue());
    assertNull(statusCaptor.getValue().getCause());
    assertTrue(metadataCaptor.getValue().keys().isEmpty());
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
    public List<InternalInstrumented<SocketStats>> getListenSockets() {
      return Collections.emptyList();
    }

    @Override
    public void shutdown() {
      listener.serverShutdown();
    }

    public ServerTransportListener registerNewServerTransport(SimpleServerTransport transport) {
      return transport.listener = listener.transportCreated(transport);
    }
  }

  private class SimpleServerTransport implements ServerTransport {
    ServerTransportListener listener;
    InternalLogId id = InternalLogId.allocate(getClass().getName());

    @Override
    public void shutdown() {
      listener.transportTerminated();
    }

    @Override
    public void shutdownNow(Status status) {
      listener.transportTerminated();
    }

    @Override
    public InternalLogId getLogId() {
      return id;
    }

    @Override
    public ScheduledExecutorService getScheduledExecutorService() {
      return timer.getScheduledExecutorService();
    }

    @Override
    public ListenableFuture<SocketStats> getStats() {
      SettableFuture<SocketStats> ret = SettableFuture.create();
      ret.set(null);
      return ret;
    }
  }

  private static class Builder extends AbstractServerImplBuilder<Builder> {
    @Override protected InternalServer buildTransportServer(
        List<ServerStreamTracer.Factory> streamTracerFactories) {
      throw new UnsupportedOperationException();
    }

    @Override public Builder useTransportSecurity(File f1, File f2)  {
      throw new UnsupportedOperationException();
    }
  }

  /** Allows more precise catch blocks than plain Error to avoid catching AssertionError. */
  private static final class TestError extends Error {}
}
