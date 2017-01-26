/*
 * Copyright 2015, Google Inc. All rights reserved.
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
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.isA;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.atLeast;
import static org.mockito.Mockito.atMost;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.spy;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.collect.ImmutableList;
import io.grpc.Attributes;
import io.grpc.CallCredentials;
import io.grpc.CallCredentials.MetadataApplier;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.Compressor;
import io.grpc.CompressorRegistry;
import io.grpc.Context;
import io.grpc.DecompressorRegistry;
import io.grpc.IntegerMarshaller;
import io.grpc.LoadBalancer;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.NameResolver;
import io.grpc.PickFirstBalancerFactory;
import io.grpc.ResolvedServerInfo;
import io.grpc.ResolvedServerInfoGroup;
import io.grpc.SecurityLevel;
import io.grpc.Status;
import io.grpc.StringMarshaller;
import io.grpc.TransportManager;
import io.grpc.internal.testing.StatsTestUtils.FakeStatsContextFactory;
import java.net.SocketAddress;
import java.net.URI;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedList;
import java.util.List;
import java.util.concurrent.CyclicBarrier;
import java.util.concurrent.Executor;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicLong;
import org.junit.After;
import org.junit.Before;
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
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

/** Unit tests for {@link ManagedChannelImpl}. */
@RunWith(JUnit4.class)
public class ManagedChannelImplTest {
  private static final List<ClientInterceptor> NO_INTERCEPTOR =
      Collections.<ClientInterceptor>emptyList();
  private static final Attributes NAME_RESOLVER_PARAMS =
      Attributes.newBuilder().set(NameResolver.Factory.PARAMS_DEFAULT_PORT, 447).build();
  private final MethodDescriptor<String, Integer> method =
      MethodDescriptor.<String, Integer>newBuilder()
          .setType(MethodType.UNKNOWN)
          .setFullMethodName("/service/method")
          .setRequestMarshaller(new StringMarshaller())
          .setResponseMarshaller(new IntegerMarshaller())
          .build();
  private final String serviceName = "fake.example.com";
  private final String authority = serviceName;
  private final String userAgent = "userAgent";
  private final String target = "fake://" + serviceName;
  private URI expectedUri;
  private final SocketAddress socketAddress = new SocketAddress() {};
  private final ResolvedServerInfo server = new ResolvedServerInfo(socketAddress, Attributes.EMPTY);
  private final FakeClock timer = new FakeClock();
  private final FakeClock executor = new FakeClock();
  private final FakeStatsContextFactory statsCtxFactory = new FakeStatsContextFactory();
  private SpyingLoadBalancerFactory loadBalancerFactory =
      new SpyingLoadBalancerFactory(PickFirstBalancerFactory.getInstance());

  @Rule public final ExpectedException thrown = ExpectedException.none();

  private ManagedChannelImpl channel;
  @Captor
  private ArgumentCaptor<Status> statusCaptor;
  @Captor
  private ArgumentCaptor<StatsTraceContext> statsTraceCtxCaptor;
  @Mock
  private ConnectionClientTransport mockTransport;
  @Mock
  private ClientTransportFactory mockTransportFactory;
  @Mock
  private ClientCall.Listener<Integer> mockCallListener;
  @Mock
  private ClientCall.Listener<Integer> mockCallListener2;
  @Mock
  private ClientCall.Listener<Integer> mockCallListener3;
  @Mock
  private SharedResourceHolder.Resource<ScheduledExecutorService> timerService;
  @Mock
  private CallCredentials creds;

  private ArgumentCaptor<ManagedClientTransport.Listener> transportListenerCaptor =
      ArgumentCaptor.forClass(ManagedClientTransport.Listener.class);
  private ArgumentCaptor<ClientStreamListener> streamListenerCaptor =
      ArgumentCaptor.forClass(ClientStreamListener.class);

  private void createChannel(
      NameResolver.Factory nameResolverFactory, List<ClientInterceptor> interceptors) {
    channel = new ManagedChannelImpl(target, new FakeBackoffPolicyProvider(),
        nameResolverFactory, NAME_RESOLVER_PARAMS, loadBalancerFactory,
        mockTransportFactory, DecompressorRegistry.getDefaultInstance(),
        CompressorRegistry.getDefaultInstance(), timerService, timer.getStopwatchSupplier(),
        ManagedChannelImpl.IDLE_TIMEOUT_MILLIS_DISABLE,
        executor.getScheduledExecutorService(), userAgent, interceptors, statsCtxFactory);
    // Force-exit the initial idle-mode
    channel.exitIdleModeAndGetLb();
  }

  @Before
  public void setUp() throws Exception {
    MockitoAnnotations.initMocks(this);
    expectedUri = new URI(target);
    when(mockTransportFactory.newClientTransport(
            any(SocketAddress.class), any(String.class), any(String.class)))
        .thenReturn(mockTransport);
    when(timerService.create()).thenReturn(timer.getScheduledExecutorService());
  }

  @After
  public void allPendingTasksAreRun() throws Exception {
    // The "never" verifications in the tests only hold up if all due tasks are done.
    // As for timer, although there may be scheduled tasks in a future time, since we don't test
    // any time-related behavior in this test suite, we only care the tasks that are due. This
    // would ignore any time-sensitive tasks, e.g., back-off and the idle timer.
    assertTrue(timer.getDueTasks() + " should be empty", timer.getDueTasks().isEmpty());
    assertEquals(executor.getPendingTasks() + " should be empty", 0, executor.numPendingTasks());
  }

  /**
   * The counterpart of {@link ManagedChannelImplIdlenessTest#enterIdleModeAfterForceExit}.
   */
  @Test
  @SuppressWarnings("unchecked")
  public void idleModeDisabled() {
    createChannel(new FakeNameResolverFactory(true), NO_INTERCEPTOR);
    assertEquals(1, loadBalancerFactory.balancers.size());

    // No task is scheduled to enter idle mode
    assertEquals(0, timer.numPendingTasks());
    assertEquals(0, executor.numPendingTasks());
  }

  @Test
  public void immediateDeadlineExceeded() {
    createChannel(new FakeNameResolverFactory(true), NO_INTERCEPTOR);
    ClientCall<String, Integer> call =
        channel.newCall(method, CallOptions.DEFAULT.withDeadlineAfter(0, TimeUnit.NANOSECONDS));
    call.start(mockCallListener, new Metadata());
    assertEquals(1, executor.runDueTasks());

    verify(mockCallListener).onClose(statusCaptor.capture(), any(Metadata.class));
    Status status = statusCaptor.getValue();
    assertSame(Status.DEADLINE_EXCEEDED.getCode(), status.getCode());
  }

  @Test
  public void shutdownWithNoTransportsEverCreated() {
    createChannel(new FakeNameResolverFactory(true), NO_INTERCEPTOR);
    verifyNoMoreInteractions(mockTransportFactory);
    channel.shutdown();
    assertTrue(channel.isShutdown());
    assertTrue(channel.isTerminated());
  }

  @Test
  public void twoCallsAndGracefulShutdown() {
    FakeNameResolverFactory nameResolverFactory = new FakeNameResolverFactory(true);
    createChannel(nameResolverFactory, NO_INTERCEPTOR);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    verifyNoMoreInteractions(mockTransportFactory);

    // Create transport and call
    ClientStream mockStream = mock(ClientStream.class);
    Metadata headers = new Metadata();
    when(mockTransportFactory.newClientTransport(
            any(SocketAddress.class), any(String.class), any(String.class)))
        .thenReturn(mockTransport);
    when(mockTransport.newStream(same(method), same(headers), same(CallOptions.DEFAULT),
            any(StatsTraceContext.class)))
        .thenReturn(mockStream);
    call.start(mockCallListener, headers);
    timer.runDueTasks();
    executor.runDueTasks();

    verify(mockTransportFactory)
        .newClientTransport(same(socketAddress), eq(authority), eq(userAgent));
    verify(mockTransport).start(transportListenerCaptor.capture());
    ManagedClientTransport.Listener transportListener = transportListenerCaptor.getValue();
    transportListener.transportReady();
    executor.runDueTasks();

    verify(mockTransport).newStream(same(method), same(headers), same(CallOptions.DEFAULT),
        statsTraceCtxCaptor.capture());
    assertEquals(statsCtxFactory.pollContextOrFail(),
        statsTraceCtxCaptor.getValue().getStatsContext());
    verify(mockStream).start(streamListenerCaptor.capture());
    verify(mockStream).setCompressor(isA(Compressor.class));
    ClientStreamListener streamListener = streamListenerCaptor.getValue();

    // Second call
    ClientCall<String, Integer> call2 = channel.newCall(method, CallOptions.DEFAULT);
    ClientStream mockStream2 = mock(ClientStream.class);
    Metadata headers2 = new Metadata();
    when(mockTransport.newStream(same(method), same(headers2), same(CallOptions.DEFAULT),
            any(StatsTraceContext.class)))
        .thenReturn(mockStream2);
    call2.start(mockCallListener2, headers2);
    verify(mockTransport).newStream(same(method), same(headers2), same(CallOptions.DEFAULT),
        statsTraceCtxCaptor.capture());
    assertEquals(statsCtxFactory.pollContextOrFail(),
        statsTraceCtxCaptor.getValue().getStatsContext());

    verify(mockStream2).start(streamListenerCaptor.capture());
    ClientStreamListener streamListener2 = streamListenerCaptor.getValue();
    Metadata trailers = new Metadata();
    streamListener2.closed(Status.CANCELLED, trailers);
    executor.runDueTasks();

    verify(mockCallListener2).onClose(Status.CANCELLED, trailers);

    // Shutdown
    channel.shutdown();
    assertTrue(channel.isShutdown());
    assertFalse(channel.isTerminated());
    verify(mockTransport).shutdown();
    assertEquals(1, nameResolverFactory.resolvers.size());
    assertTrue(nameResolverFactory.resolvers.get(0).shutdown);
    assertEquals(1, loadBalancerFactory.balancers.size());
    verify(loadBalancerFactory.balancers.get(0)).shutdown();

    // Further calls should fail without going to the transport
    ClientCall<String, Integer> call3 = channel.newCall(method, CallOptions.DEFAULT);
    call3.start(mockCallListener3, new Metadata());
    timer.runDueTasks();
    executor.runDueTasks();

    verify(mockCallListener3).onClose(statusCaptor.capture(), any(Metadata.class));
    assertSame(Status.Code.UNAVAILABLE, statusCaptor.getValue().getCode());

    // Finish shutdown
    transportListener.transportShutdown(Status.CANCELLED);
    assertFalse(channel.isTerminated());
    streamListener.closed(Status.CANCELLED, trailers);
    executor.runDueTasks();

    verify(mockCallListener).onClose(Status.CANCELLED, trailers);
    assertFalse(channel.isTerminated());

    transportListener.transportTerminated();
    assertTrue(channel.isTerminated());

    verify(mockTransportFactory).close();
    verifyNoMoreInteractions(mockTransportFactory);
    verify(mockTransport, atLeast(0)).getLogId();
    verifyNoMoreInteractions(mockTransport);
    verifyNoMoreInteractions(mockStream);
  }

  @Test
  public void callAndShutdownNow() {
    FakeNameResolverFactory nameResolverFactory = new FakeNameResolverFactory(true);
    createChannel(nameResolverFactory, NO_INTERCEPTOR);
    verifyNoMoreInteractions(mockTransportFactory);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    verifyNoMoreInteractions(mockTransportFactory);

    // Create transport and call
    ClientStream mockStream = mock(ClientStream.class);
    Metadata headers = new Metadata();
    when(mockTransportFactory.newClientTransport(
            any(SocketAddress.class), any(String.class), any(String.class)))
        .thenReturn(mockTransport);
    when(mockTransport.newStream(same(method), same(headers), same(CallOptions.DEFAULT),
            any(StatsTraceContext.class)))
        .thenReturn(mockStream);
    call.start(mockCallListener, headers);
    timer.runDueTasks();
    executor.runDueTasks();

    verify(mockTransportFactory)
        .newClientTransport(same(socketAddress), eq(authority), any(String.class));
    verify(mockTransport).start(transportListenerCaptor.capture());
    ManagedClientTransport.Listener transportListener = transportListenerCaptor.getValue();
    transportListener.transportReady();
    executor.runDueTasks();

    verify(mockTransport).newStream(same(method), same(headers), same(CallOptions.DEFAULT),
        any(StatsTraceContext.class));

    verify(mockStream).start(streamListenerCaptor.capture());
    verify(mockStream).setCompressor(isA(Compressor.class));
    ClientStreamListener streamListener = streamListenerCaptor.getValue();

    // ShutdownNow
    channel.shutdownNow();
    assertTrue(channel.isShutdown());
    assertFalse(channel.isTerminated());
    // ShutdownNow may or may not invoke shutdown. Ideally it wouldn't, but it doesn't matter much
    // either way.
    verify(mockTransport, atMost(1)).shutdown();
    verify(mockTransport).shutdownNow(any(Status.class));
    assertEquals(1, nameResolverFactory.resolvers.size());
    assertTrue(nameResolverFactory.resolvers.get(0).shutdown);
    assertEquals(1, loadBalancerFactory.balancers.size());
    verify(loadBalancerFactory.balancers.get(0)).shutdown();

    // Further calls should fail without going to the transport
    ClientCall<String, Integer> call3 = channel.newCall(method, CallOptions.DEFAULT);
    call3.start(mockCallListener3, new Metadata());
    executor.runDueTasks();

    verify(mockCallListener3).onClose(statusCaptor.capture(), any(Metadata.class));
    assertSame(Status.Code.UNAVAILABLE, statusCaptor.getValue().getCode());

    // Finish shutdown
    transportListener.transportShutdown(Status.CANCELLED);
    assertFalse(channel.isTerminated());
    Metadata trailers = new Metadata();
    streamListener.closed(Status.CANCELLED, trailers);
    executor.runDueTasks();

    verify(mockCallListener).onClose(Status.CANCELLED, trailers);
    assertFalse(channel.isTerminated());

    transportListener.transportTerminated();
    assertTrue(channel.isTerminated());

    verify(mockTransportFactory).close();
    verifyNoMoreInteractions(mockTransportFactory);
    verify(mockTransport, atLeast(0)).getLogId();
    verifyNoMoreInteractions(mockTransport);
    verifyNoMoreInteractions(mockStream);
  }

  /** Make sure shutdownNow() after shutdown() has an effect. */
  @Test
  public void callAndShutdownAndShutdownNow() {
    createChannel(new FakeNameResolverFactory(true), NO_INTERCEPTOR);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);

    // Create transport and call
    ClientStream mockStream = mock(ClientStream.class);
    Metadata headers = new Metadata();
    when(mockTransport.newStream(same(method), same(headers), same(CallOptions.DEFAULT),
            any(StatsTraceContext.class)))
        .thenReturn(mockStream);
    call.start(mockCallListener, headers);
    timer.runDueTasks();
    executor.runDueTasks();

    verify(mockTransport).start(transportListenerCaptor.capture());
    ManagedClientTransport.Listener transportListener = transportListenerCaptor.getValue();
    transportListener.transportReady();
    executor.runDueTasks();

    verify(mockStream).start(streamListenerCaptor.capture());
    ClientStreamListener streamListener = streamListenerCaptor.getValue();

    // ShutdownNow
    channel.shutdown();
    channel.shutdownNow();
    // ShutdownNow may or may not invoke shutdown. Ideally it wouldn't, but it doesn't matter much
    // either way.
    verify(mockTransport, atMost(2)).shutdown();
    verify(mockTransport).shutdownNow(any(Status.class));

    // Finish shutdown
    transportListener.transportShutdown(Status.CANCELLED);
    assertFalse(channel.isTerminated());
    Metadata trailers = new Metadata();
    streamListener.closed(Status.CANCELLED, trailers);
    executor.runDueTasks();

    verify(mockCallListener).onClose(Status.CANCELLED, trailers);
    assertFalse(channel.isTerminated());

    transportListener.transportTerminated();
    assertTrue(channel.isTerminated());
  }


  @Test
  public void interceptor() throws Exception {
    final AtomicLong atomic = new AtomicLong();
    ClientInterceptor interceptor = new ClientInterceptor() {
      @Override
      public <RequestT, ResponseT> ClientCall<RequestT, ResponseT> interceptCall(
          MethodDescriptor<RequestT, ResponseT> method, CallOptions callOptions,
          Channel next) {
        atomic.set(1);
        return next.newCall(method, callOptions);
      }
    };
    createChannel(new FakeNameResolverFactory(true), Arrays.asList(interceptor));
    assertNotNull(channel.newCall(method, CallOptions.DEFAULT));
    assertEquals(1, atomic.get());
  }

  @Test
  public void testNoDeadlockOnShutdown() {
    createChannel(new FakeNameResolverFactory(true), NO_INTERCEPTOR);
    // Force creation of transport
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    Metadata headers = new Metadata();
    ClientStream mockStream = mock(ClientStream.class);
    when(mockTransport.newStream(same(method), same(headers))).thenReturn(mockStream);
    call.start(mockCallListener, headers);
    timer.runDueTasks();
    executor.runDueTasks();
    call.cancel("Cancel for test", null);
    executor.runDueTasks();

    verify(mockTransport).start(transportListenerCaptor.capture());
    final ManagedClientTransport.Listener transportListener = transportListenerCaptor.getValue();
    final Object lock = new Object();
    final CyclicBarrier barrier = new CyclicBarrier(2);
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
          transportListener.transportShutdown(Status.CANCELLED);
        }
      }
    }.start();
    doAnswer(new Answer<Void>() {
      @Override
      public Void answer(InvocationOnMock invocation) {
        // To deadlock, a lock would need to be held while this method is in progress.
        try {
          barrier.await();
        } catch (Exception ex) {
          throw new AssertionError(ex);
        }
        // If deadlock is possible with this setup, this sychronization completes the loop because
        // the transportShutdown needs a lock that Channel is holding while calling this method.
        synchronized (lock) {
        }
        return null;
      }
    }).when(mockTransport).shutdown();
    channel.shutdown();

    transportListener.transportTerminated();
  }

  @Test
  public void callOptionsExecutor() {
    Metadata headers = new Metadata();
    ClientStream mockStream = mock(ClientStream.class);
    when(mockTransport.newStream(same(method), same(headers), any(CallOptions.class),
            any(StatsTraceContext.class)))
        .thenReturn(mockStream);
    FakeClock callExecutor = new FakeClock();
    createChannel(new FakeNameResolverFactory(true), NO_INTERCEPTOR);
    CallOptions options =
        CallOptions.DEFAULT.withExecutor(callExecutor.getScheduledExecutorService());

    ClientCall<String, Integer> call = channel.newCall(method, options);
    call.start(mockCallListener, headers);
    timer.runDueTasks();
    executor.runDueTasks();

    verify(mockTransport).start(transportListenerCaptor.capture());
    assertEquals(0, executor.numPendingTasks());
    transportListenerCaptor.getValue().transportReady();
    // Real streams are started in the channel's executor
    assertEquals(1, executor.runDueTasks());

    verify(mockTransport).newStream(same(method), same(headers), same(options),
        any(StatsTraceContext.class));
    verify(mockStream).start(streamListenerCaptor.capture());
    ClientStreamListener streamListener = streamListenerCaptor.getValue();
    Metadata trailers = new Metadata();
    assertEquals(0, callExecutor.numPendingTasks());
    streamListener.closed(Status.CANCELLED, trailers);
    verify(mockCallListener, never()).onClose(same(Status.CANCELLED), same(trailers));
    assertEquals(1, callExecutor.runDueTasks());

    verify(mockCallListener).onClose(same(Status.CANCELLED), same(trailers));
  }

  @Test
  public void nameResolutionFailed() {
    Status error = Status.UNAVAILABLE.withCause(new Throwable("fake name resolution error"));

    // Name resolution is started as soon as channel is created.
    createChannel(new FailingNameResolverFactory(error), NO_INTERCEPTOR);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, new Metadata());
    timer.runDueTasks();
    executor.runDueTasks();

    // The call failed with the name resolution error
    verify(mockCallListener).onClose(statusCaptor.capture(), any(Metadata.class));
    Status status = statusCaptor.getValue();
    assertSame(error.getCode(), status.getCode());
    assertSame(error.getCause(), status.getCause());
    // LoadBalancer received the same error
    assertEquals(1, loadBalancerFactory.balancers.size());
    verify(loadBalancerFactory.balancers.get(0)).handleNameResolutionError(same(error));
  }

  @Test
  public void nameResolverReturnsEmptySubLists() {
    String errorDescription = "NameResolver returned an empty list";

    // Name resolution is started as soon as channel is created
    createChannel(new FakeNameResolverFactory(), NO_INTERCEPTOR);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, new Metadata());
    timer.runDueTasks();
    executor.runDueTasks();

    // The call failed with the name resolution error
    verify(mockCallListener).onClose(statusCaptor.capture(), any(Metadata.class));
    Status status = statusCaptor.getValue();
    assertSame(Status.Code.UNAVAILABLE, status.getCode());
    assertTrue(status.getDescription(), status.getDescription().contains(errorDescription));
    // LoadBalancer received the same error
    assertEquals(1, loadBalancerFactory.balancers.size());
    verify(loadBalancerFactory.balancers.get(0)).handleNameResolutionError(statusCaptor.capture());
    status = statusCaptor.getValue();
    assertSame(Status.Code.UNAVAILABLE, status.getCode());
    assertEquals(errorDescription, status.getDescription());
  }

  @Test
  public void loadBalancerThrowsInHandleResolvedAddresses() {
    RuntimeException ex = new RuntimeException("simulated");
    // Delay the success of name resolution until allResolved() is called
    FakeNameResolverFactory nameResolverFactory = new FakeNameResolverFactory(false);
    createChannel(nameResolverFactory, NO_INTERCEPTOR);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, new Metadata());
    timer.runDueTasks();
    executor.runDueTasks();

    assertEquals(1, loadBalancerFactory.balancers.size());
    LoadBalancer<?> loadBalancer = loadBalancerFactory.balancers.get(0);
    doThrow(ex).when(loadBalancer).handleResolvedAddresses(
        Matchers.<List<ResolvedServerInfoGroup>>anyObject(), any(Attributes.class));

    // NameResolver returns addresses.
    nameResolverFactory.allResolved();
    executor.runDueTasks();

    // The call failed with the error thrown from handleResolvedAddresses()
    verify(mockCallListener).onClose(statusCaptor.capture(), any(Metadata.class));
    Status status = statusCaptor.getValue();
    assertSame(Status.Code.INTERNAL, status.getCode());
    assertSame(ex, status.getCause());
    // The LoadBalancer received the same error
    verify(loadBalancer).handleNameResolutionError(statusCaptor.capture());
    status = statusCaptor.getValue();
    assertSame(Status.Code.INTERNAL, status.getCode());
    assertSame(ex, status.getCause());
  }

  @Test
  public void nameResolvedAfterChannelShutdown() {
    // Delay the success of name resolution until allResolved() is called.
    FakeNameResolverFactory nameResolverFactory = new FakeNameResolverFactory(false);
    createChannel(nameResolverFactory, NO_INTERCEPTOR);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    Metadata headers = new Metadata();

    call.start(mockCallListener, headers);
    timer.runDueTasks();
    executor.runDueTasks();
    channel.shutdown();

    assertTrue(channel.isShutdown());
    // Name resolved after the channel is shut down, which is possible if the name resolution takes
    // time and is not cancellable. The resolved address will still be passed to the LoadBalancer.
    nameResolverFactory.allResolved();
    executor.runDueTasks();

    verify(mockTransportFactory, never())
        .newClientTransport(any(SocketAddress.class), any(String.class), any(String.class));
  }

  /**
   * Verify that if the first resolved address points to a server that cannot be connected, the call
   * will end up with the second address which works.
   */
  @Test
  public void firstResolvedServerFailedToConnect() throws Exception {
    final SocketAddress goodAddress = new SocketAddress() {
        @Override public String toString() {
          return "goodAddress";
        }
      };
    final SocketAddress badAddress = new SocketAddress() {
        @Override public String toString() {
          return "badAddress";
        }
      };
    final ResolvedServerInfo goodServer = new ResolvedServerInfo(goodAddress, Attributes.EMPTY);
    final ResolvedServerInfo badServer = new ResolvedServerInfo(badAddress, Attributes.EMPTY);
    final ConnectionClientTransport goodTransport = mock(ConnectionClientTransport.class);
    final ConnectionClientTransport badTransport = mock(ConnectionClientTransport.class);
    when(goodTransport.newStream(
            any(MethodDescriptor.class), any(Metadata.class), any(CallOptions.class),
            any(StatsTraceContext.class)))
        .thenReturn(mock(ClientStream.class));
    when(mockTransportFactory.newClientTransport(
            same(goodAddress), any(String.class), any(String.class)))
        .thenReturn(goodTransport);
    when(mockTransportFactory.newClientTransport(
            same(badAddress), any(String.class), any(String.class)))
        .thenReturn(badTransport);

    FakeNameResolverFactory nameResolverFactory =
        new FakeNameResolverFactory(Arrays.asList(badServer, goodServer));
    createChannel(nameResolverFactory, NO_INTERCEPTOR);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    Metadata headers = new Metadata();

    // Start a call. The channel will starts with the first address (badAddress)
    call.start(mockCallListener, headers);
    timer.runDueTasks();
    executor.runDueTasks();

    ArgumentCaptor<ManagedClientTransport.Listener> badTransportListenerCaptor =
        ArgumentCaptor.forClass(ManagedClientTransport.Listener.class);
    verify(badTransport).start(badTransportListenerCaptor.capture());
    verify(mockTransportFactory)
        .newClientTransport(same(badAddress), any(String.class), any(String.class));
    verify(mockTransportFactory, times(0))
          .newClientTransport(same(goodAddress), any(String.class), any(String.class));
    badTransportListenerCaptor.getValue().transportShutdown(Status.UNAVAILABLE);

    // The channel then try the second address (goodAddress)
    ArgumentCaptor<ManagedClientTransport.Listener> goodTransportListenerCaptor =
        ArgumentCaptor.forClass(ManagedClientTransport.Listener.class);
    verify(mockTransportFactory)
          .newClientTransport(same(goodAddress), any(String.class), any(String.class));
    verify(goodTransport).start(goodTransportListenerCaptor.capture());
    goodTransportListenerCaptor.getValue().transportReady();
    executor.runDueTasks();

    verify(goodTransport).newStream(same(method), same(headers), same(CallOptions.DEFAULT),
        any(StatsTraceContext.class));
    // The bad transport was never used.
    verify(badTransport, times(0)).newStream(any(MethodDescriptor.class), any(Metadata.class));
  }

  /**
   * Verify that if all resolved addresses failed to connect, the call will fail.
   */
  @Test
  public void allServersFailedToConnect() throws Exception {
    final SocketAddress addr1 = new SocketAddress() {
        @Override public String toString() {
          return "addr1";
        }
      };
    final SocketAddress addr2 = new SocketAddress() {
        @Override public String toString() {
          return "addr2";
        }
      };
    final ResolvedServerInfo server1 = new ResolvedServerInfo(addr1, Attributes.EMPTY);
    final ResolvedServerInfo server2 = new ResolvedServerInfo(addr2, Attributes.EMPTY);
    final ConnectionClientTransport transport1 = mock(ConnectionClientTransport.class);
    final ConnectionClientTransport transport2 = mock(ConnectionClientTransport.class);
    when(mockTransportFactory.newClientTransport(same(addr1), any(String.class), any(String.class)))
        .thenReturn(transport1);
    when(mockTransportFactory.newClientTransport(same(addr2), any(String.class), any(String.class)))
        .thenReturn(transport2);

    FakeNameResolverFactory nameResolverFactory =
        new FakeNameResolverFactory(Arrays.asList(server1, server2));
    createChannel(nameResolverFactory, NO_INTERCEPTOR);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    Metadata headers = new Metadata();

    // Start a call. The channel will starts with the first address, which will fail to connect.
    call.start(mockCallListener, headers);
    timer.runDueTasks();
    executor.runDueTasks();

    verify(transport1).start(transportListenerCaptor.capture());
    verify(mockTransportFactory)
        .newClientTransport(same(addr1), any(String.class), any(String.class));
    verify(mockTransportFactory, times(0))
        .newClientTransport(same(addr2), any(String.class), any(String.class));
    transportListenerCaptor.getValue().transportShutdown(Status.UNAVAILABLE);

    // The channel then try the second address, which will fail to connect too.
    verify(transport2).start(transportListenerCaptor.capture());
    verify(mockTransportFactory)
        .newClientTransport(same(addr2), any(String.class), any(String.class));
    verify(transport2).start(transportListenerCaptor.capture());
    transportListenerCaptor.getValue().transportShutdown(Status.UNAVAILABLE);
    executor.runDueTasks();

    // Call fails
    verify(mockCallListener).onClose(statusCaptor.capture(), any(Metadata.class));
    assertEquals(Status.Code.UNAVAILABLE, statusCaptor.getValue().getCode());
    // No real stream was ever created
    verify(transport1, times(0)).newStream(any(MethodDescriptor.class), any(Metadata.class));
    verify(transport2, times(0)).newStream(any(MethodDescriptor.class), any(Metadata.class));
  }

  /**
   * Verify that if the first resolved address points to a server that is at first connected, but
   * disconnected later, all calls will stick to the first address.
   */
  @Test
  public void firstResolvedServerConnectedThenDisconnected() throws Exception {
    final SocketAddress addr1 = new SocketAddress() {
        @Override public String toString() {
          return "addr1";
        }
      };
    final SocketAddress addr2 = new SocketAddress() {
        @Override public String toString() {
          return "addr2";
        }
      };
    final ResolvedServerInfo server1 = new ResolvedServerInfo(addr1, Attributes.EMPTY);
    final ResolvedServerInfo server2 = new ResolvedServerInfo(addr2, Attributes.EMPTY);
    // Addr1 will have two transports throughout this test.
    final ConnectionClientTransport transport1 = mock(ConnectionClientTransport.class);
    final ConnectionClientTransport transport2 = mock(ConnectionClientTransport.class);
    when(transport1.newStream(
            any(MethodDescriptor.class), any(Metadata.class), any(CallOptions.class),
            any(StatsTraceContext.class)))
        .thenReturn(mock(ClientStream.class));
    when(transport2.newStream(
            any(MethodDescriptor.class), any(Metadata.class), any(CallOptions.class),
            any(StatsTraceContext.class)))
        .thenReturn(mock(ClientStream.class));
    when(mockTransportFactory.newClientTransport(same(addr1), any(String.class), any(String.class)))
        .thenReturn(transport1, transport2);

    FakeNameResolverFactory nameResolverFactory =
        new FakeNameResolverFactory(Arrays.asList(server1, server2));
    createChannel(nameResolverFactory, NO_INTERCEPTOR);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    Metadata headers = new Metadata();

    // First call will use the first address
    call.start(mockCallListener, headers);
    timer.runDueTasks();
    executor.runDueTasks();

    verify(mockTransportFactory)
        .newClientTransport(same(addr1), any(String.class), any(String.class));
    verify(transport1).start(transportListenerCaptor.capture());
    transportListenerCaptor.getValue().transportReady();
    executor.runDueTasks();

    verify(transport1).newStream(same(method), same(headers), same(CallOptions.DEFAULT),
        any(StatsTraceContext.class));
    transportListenerCaptor.getValue().transportShutdown(Status.UNAVAILABLE);

    // Second call still use the first address, since it was successfully connected.
    ClientCall<String, Integer> call2 = channel.newCall(method, CallOptions.DEFAULT);
    call2.start(mockCallListener, headers);
    verify(transport2).start(transportListenerCaptor.capture());
    verify(mockTransportFactory, times(2))
        .newClientTransport(same(addr1), any(String.class), any(String.class));
    transportListenerCaptor.getValue().transportReady();
    executor.runDueTasks();

    verify(transport2).newStream(same(method), same(headers), same(CallOptions.DEFAULT),
        any(StatsTraceContext.class));
  }

  @Test
  public void uriPattern() {
    assertTrue(ManagedChannelImpl.URI_PATTERN.matcher("a:/").matches());
    assertTrue(ManagedChannelImpl.URI_PATTERN.matcher("Z019+-.:/!@ #~ ").matches());
    assertFalse(ManagedChannelImpl.URI_PATTERN.matcher("a/:").matches()); // "/:" not matched
    assertFalse(ManagedChannelImpl.URI_PATTERN.matcher("0a:/").matches()); // '0' not matched
    assertFalse(ManagedChannelImpl.URI_PATTERN.matcher("a,:/").matches()); // ',' not matched
    assertFalse(ManagedChannelImpl.URI_PATTERN.matcher(" a:/").matches()); // space not matched
  }

  /**
   * Test that information such as the Call's context, MethodDescriptor, authority, executor are
   * propagated to newStream() and applyRequestMetadata().
   */
  @Test
  public void informationPropagatedToNewStreamAndCallCredentials() {
    createChannel(new FakeNameResolverFactory(true), NO_INTERCEPTOR);
    CallOptions callOptions = CallOptions.DEFAULT.withCallCredentials(creds);
    final Context.Key<String> testKey = Context.key("testing");
    Context ctx = Context.current().withValue(testKey, "testValue");
    final LinkedList<Context> credsApplyContexts = new LinkedList<Context>();
    final LinkedList<Context> newStreamContexts = new LinkedList<Context>();
    doAnswer(new Answer<Void>() {
        @Override
        public Void answer(InvocationOnMock in) throws Throwable {
          credsApplyContexts.add(Context.current());
          return null;
        }
      }).when(creds).applyRequestMetadata(
          any(MethodDescriptor.class), any(Attributes.class), any(Executor.class),
          any(MetadataApplier.class));

    final ConnectionClientTransport transport = mock(ConnectionClientTransport.class);
    when(transport.getAttributes()).thenReturn(Attributes.EMPTY);
    when(mockTransportFactory.newClientTransport(any(SocketAddress.class), any(String.class),
            any(String.class))).thenReturn(transport);
    doAnswer(new Answer<ClientStream>() {
        @Override
        public ClientStream answer(InvocationOnMock in) throws Throwable {
          newStreamContexts.add(Context.current());
          return mock(ClientStream.class);
        }
      }).when(transport).newStream(
          any(MethodDescriptor.class), any(Metadata.class), any(CallOptions.class),
          any(StatsTraceContext.class));

    // First call will be on delayed transport.  Only newCall() is run within the expected context,
    // so that we can verify that the context is explicitly attached before calling newStream() and
    // applyRequestMetadata(), which happens after we detach the context from the thread.
    Context origCtx = ctx.attach();
    assertEquals("testValue", testKey.get());
    ClientCall<String, Integer> call = channel.newCall(method, callOptions);
    ctx.detach(origCtx);
    assertNull(testKey.get());
    call.start(mockCallListener, new Metadata());

    ArgumentCaptor<ManagedClientTransport.Listener> transportListenerCaptor =
        ArgumentCaptor.forClass(ManagedClientTransport.Listener.class);
    verify(mockTransportFactory).newClientTransport(
        same(socketAddress), eq(authority), eq(userAgent));
    verify(transport).start(transportListenerCaptor.capture());
    verify(creds, never()).applyRequestMetadata(
        any(MethodDescriptor.class), any(Attributes.class), any(Executor.class),
        any(MetadataApplier.class));

    // applyRequestMetadata() is called after the transport becomes ready.
    transportListenerCaptor.getValue().transportReady();
    executor.runDueTasks();
    ArgumentCaptor<Attributes> attrsCaptor = ArgumentCaptor.forClass(Attributes.class);
    ArgumentCaptor<MetadataApplier> applierCaptor = ArgumentCaptor.forClass(MetadataApplier.class);
    verify(creds).applyRequestMetadata(same(method), attrsCaptor.capture(),
        same(executor.getScheduledExecutorService()), applierCaptor.capture());
    assertEquals("testValue", testKey.get(credsApplyContexts.poll()));
    assertEquals(authority, attrsCaptor.getValue().get(CallCredentials.ATTR_AUTHORITY));
    assertEquals(SecurityLevel.NONE,
        attrsCaptor.getValue().get(CallCredentials.ATTR_SECURITY_LEVEL));
    verify(transport, never()).newStream(
        any(MethodDescriptor.class), any(Metadata.class), any(CallOptions.class),
        any(StatsTraceContext.class));

    // newStream() is called after apply() is called
    applierCaptor.getValue().apply(new Metadata());
    verify(transport).newStream(same(method), any(Metadata.class), same(callOptions),
        any(StatsTraceContext.class));
    assertEquals("testValue", testKey.get(newStreamContexts.poll()));
    // The context should not live beyond the scope of newStream() and applyRequestMetadata()
    assertNull(testKey.get());


    // Second call will not be on delayed transport
    origCtx = ctx.attach();
    call = channel.newCall(method, callOptions);
    ctx.detach(origCtx);
    call.start(mockCallListener, new Metadata());

    verify(creds, times(2)).applyRequestMetadata(same(method), attrsCaptor.capture(),
        same(executor.getScheduledExecutorService()), applierCaptor.capture());
    assertEquals("testValue", testKey.get(credsApplyContexts.poll()));
    assertEquals(authority, attrsCaptor.getValue().get(CallCredentials.ATTR_AUTHORITY));
    assertEquals(SecurityLevel.NONE,
        attrsCaptor.getValue().get(CallCredentials.ATTR_SECURITY_LEVEL));
    // This is from the first call
    verify(transport).newStream(
        any(MethodDescriptor.class), any(Metadata.class), any(CallOptions.class),
        any(StatsTraceContext.class));

    // Still, newStream() is called after apply() is called
    applierCaptor.getValue().apply(new Metadata());
    verify(transport, times(2)).newStream(same(method), any(Metadata.class), same(callOptions),
        any(StatsTraceContext.class));
    assertEquals("testValue", testKey.get(newStreamContexts.poll()));

    assertNull(testKey.get());
  }

  private static class FakeBackoffPolicyProvider implements BackoffPolicy.Provider {
    @Override
    public BackoffPolicy get() {
      return new BackoffPolicy() {
        @Override
        public long nextBackoffMillis() {
          return 1;
        }
      };
    }
  }

  private class FakeNameResolverFactory extends NameResolver.Factory {
    final List<ResolvedServerInfoGroup> servers;
    final boolean resolvedAtStart;
    final ArrayList<FakeNameResolver> resolvers = new ArrayList<FakeNameResolver>();

    FakeNameResolverFactory(boolean resolvedAtStart) {
      this.resolvedAtStart = resolvedAtStart;
      servers = Collections.singletonList(ResolvedServerInfoGroup.builder().add(server).build());
    }

    FakeNameResolverFactory(List<ResolvedServerInfo> servers) {
      resolvedAtStart = true;
      this.servers = Collections.singletonList(
          ResolvedServerInfoGroup.builder().addAll(servers).build());
    }

    public FakeNameResolverFactory() {
      resolvedAtStart = true;
      this.servers = ImmutableList.of();
    }

    @Override
    public NameResolver newNameResolver(final URI targetUri, Attributes params) {
      if (!expectedUri.equals(targetUri)) {
        return null;
      }
      assertSame(NAME_RESOLVER_PARAMS, params);
      FakeNameResolver resolver = new FakeNameResolver();
      resolvers.add(resolver);
      return resolver;
    }

    @Override
    public String getDefaultScheme() {
      return "fake";
    }

    void allResolved() {
      for (FakeNameResolver resolver : resolvers) {
        resolver.resolved();
      }
    }

    private class FakeNameResolver extends NameResolver {
      Listener listener;
      boolean shutdown;

      @Override public String getServiceAuthority() {
        return expectedUri.getAuthority();
      }

      @Override public void start(final Listener listener) {
        this.listener = listener;
        if (resolvedAtStart) {
          resolved();
        }
      }

      void resolved() {
        listener.onUpdate(servers, Attributes.EMPTY);
      }

      @Override public void shutdown() {
        shutdown = true;
      }
    }
  }

  private static class FailingNameResolverFactory extends NameResolver.Factory {
    final Status error;

    FailingNameResolverFactory(Status error) {
      this.error = error;
    }

    @Override
    public NameResolver newNameResolver(URI notUsedUri, Attributes params) {
      return new NameResolver() {
        @Override public String getServiceAuthority() {
          return "irrelevant-authority";
        }

        @Override public void start(final Listener listener) {
          listener.onError(error);
        }

        @Override public void shutdown() {}
      };
    }

    @Override
    public String getDefaultScheme() {
      return "fake";
    }
  }

  private static class SpyingLoadBalancerFactory extends LoadBalancer.Factory {
    private final LoadBalancer.Factory delegate;
    private final List<LoadBalancer<?>> balancers = new ArrayList<LoadBalancer<?>>();

    private SpyingLoadBalancerFactory(LoadBalancer.Factory delegate) {
      this.delegate = delegate;
    }

    @Override
    public <T> LoadBalancer<T> newLoadBalancer(String serviceName, TransportManager<T> tm) {
      LoadBalancer<T> lb = spy(delegate.newLoadBalancer(serviceName, tm));
      balancers.add(lb);
      return lb;
    }
  }
}
