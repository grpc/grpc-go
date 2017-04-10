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

import static io.grpc.ConnectivityState.CONNECTING;
import static io.grpc.ConnectivityState.READY;
import static io.grpc.ConnectivityState.TRANSIENT_FAILURE;
import static junit.framework.TestCase.assertNotSame;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyObject;
import static org.mockito.Matchers.anyString;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.atLeast;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.inOrder;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.verifyZeroInteractions;
import static org.mockito.Mockito.when;

import com.google.common.collect.ImmutableList;
import io.grpc.Attributes;
import io.grpc.CallCredentials;
import io.grpc.CallCredentials.MetadataApplier;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.ClientStreamTracer;
import io.grpc.CompressorRegistry;
import io.grpc.ConnectivityStateInfo;
import io.grpc.Context;
import io.grpc.DecompressorRegistry;
import io.grpc.EquivalentAddressGroup;
import io.grpc.IntegerMarshaller;
import io.grpc.LoadBalancer;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancer.PickResult;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.LoadBalancer.Subchannel;
import io.grpc.LoadBalancer.SubchannelPicker;
import io.grpc.ManagedChannel;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.NameResolver;
import io.grpc.SecurityLevel;
import io.grpc.Status;
import io.grpc.StringMarshaller;
import io.grpc.internal.TestUtils.MockClientTransportInfo;
import java.net.SocketAddress;
import java.net.URI;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedList;
import java.util.List;
import java.util.concurrent.BlockingQueue;
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
import org.mockito.InOrder;
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
  private static final MethodDescriptor<String, Integer> method =
      MethodDescriptor.<String, Integer>newBuilder()
          .setType(MethodType.UNKNOWN)
          .setFullMethodName("/service/method")
          .setRequestMarshaller(new StringMarshaller())
          .setResponseMarshaller(new IntegerMarshaller())
          .build();
  private static final Attributes.Key<String> SUBCHANNEL_ATTR_KEY =
      Attributes.Key.of("subchannel-attr-key");
  private final String serviceName = "fake.example.com";
  private final String authority = serviceName;
  private final String userAgent = "userAgent";
  private final String target = "fake://" + serviceName;
  private URI expectedUri;
  private final SocketAddress socketAddress = new SocketAddress() {};
  private final EquivalentAddressGroup addressGroup = new EquivalentAddressGroup(socketAddress);
  private final FakeClock timer = new FakeClock();
  private final FakeClock executor = new FakeClock();
  private final FakeClock oobExecutor = new FakeClock();

  @Rule public final ExpectedException thrown = ExpectedException.none();

  private ManagedChannelImpl channel;
  private Helper helper;
  @Captor
  private ArgumentCaptor<Status> statusCaptor;
  @Captor
  private ArgumentCaptor<CallOptions> callOptionsCaptor;
  @Mock
  private LoadBalancer.Factory mockLoadBalancerFactory;
  @Mock
  private LoadBalancer mockLoadBalancer;
  @Captor
  private ArgumentCaptor<ConnectivityStateInfo> stateInfoCaptor;
  @Mock
  private SubchannelPicker mockPicker;
  @Mock
  private ClientTransportFactory mockTransportFactory;
  @Mock
  private ClientCall.Listener<Integer> mockCallListener;
  @Mock
  private ClientCall.Listener<Integer> mockCallListener2;
  @Mock
  private ClientCall.Listener<Integer> mockCallListener3;
  @Mock
  private ClientCall.Listener<Integer> mockCallListener4;
  @Mock
  private ClientCall.Listener<Integer> mockCallListener5;
  @Mock
  private ObjectPool<ScheduledExecutorService> timerServicePool;
  @Mock
  private ObjectPool<Executor> executorPool;
  @Mock
  private ObjectPool<Executor> oobExecutorPool;
  @Mock
  private CallCredentials creds;
  private BlockingQueue<MockClientTransportInfo> transports;

  private ArgumentCaptor<ClientStreamListener> streamListenerCaptor =
      ArgumentCaptor.forClass(ClientStreamListener.class);

  private void createChannel(
      NameResolver.Factory nameResolverFactory, List<ClientInterceptor> interceptors) {
    channel = new ManagedChannelImpl(target, new FakeBackoffPolicyProvider(),
        nameResolverFactory, NAME_RESOLVER_PARAMS, mockLoadBalancerFactory,
        mockTransportFactory, DecompressorRegistry.getDefaultInstance(),
        CompressorRegistry.getDefaultInstance(), timerServicePool, executorPool, oobExecutorPool,
        timer.getStopwatchSupplier(),  ManagedChannelImpl.IDLE_TIMEOUT_MILLIS_DISABLE, userAgent,
        interceptors);
    // Force-exit the initial idle-mode
    channel.exitIdleMode();
    assertEquals(0, timer.numPendingTasks());

    ArgumentCaptor<Helper> helperCaptor = ArgumentCaptor.forClass(null);
    verify(mockLoadBalancerFactory).newLoadBalancer(helperCaptor.capture());
    helper = helperCaptor.getValue();
  }

  @Before
  public void setUp() throws Exception {
    MockitoAnnotations.initMocks(this);
    expectedUri = new URI(target);
    when(mockLoadBalancerFactory.newLoadBalancer(any(Helper.class))).thenReturn(mockLoadBalancer);
    transports = TestUtils.captureTransports(mockTransportFactory);
    when(timerServicePool.getObject()).thenReturn(timer.getScheduledExecutorService());
    when(executorPool.getObject()).thenReturn(executor.getScheduledExecutorService());
    when(oobExecutorPool.getObject()).thenReturn(oobExecutor.getScheduledExecutorService());
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

  @Test
  @SuppressWarnings("unchecked")
  public void idleModeDisabled() {
    createChannel(new FakeNameResolverFactory(true), NO_INTERCEPTOR);

    // In this test suite, the channel is always created with idle mode disabled.
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
    verify(executorPool).getObject();
    verify(timerServicePool).getObject();
    verify(executorPool, never()).returnObject(anyObject());
    verify(timerServicePool, never()).returnObject(anyObject());
    verifyNoMoreInteractions(mockTransportFactory);
    channel.shutdown();
    assertTrue(channel.isShutdown());
    assertTrue(channel.isTerminated());
    verify(executorPool).returnObject(executor.getScheduledExecutorService());
    verify(timerServicePool).returnObject(timer.getScheduledExecutorService());
  }

  @Test
  public void callsAndShutdown() {
    subtestCallsAndShutdown(false, false);
  }

  @Test
  public void callsAndShutdownNow() {
    subtestCallsAndShutdown(true, false);
  }

  /** Make sure shutdownNow() after shutdown() has an effect. */
  @Test
  public void callsAndShutdownAndShutdownNow() {
    subtestCallsAndShutdown(false, true);
  }

  private void subtestCallsAndShutdown(boolean shutdownNow, boolean shutdownNowAfterShutdown) {
    FakeNameResolverFactory nameResolverFactory = new FakeNameResolverFactory(true);
    createChannel(nameResolverFactory, NO_INTERCEPTOR);
    verify(executorPool).getObject();
    verify(timerServicePool).getObject();
    ClientStream mockStream = mock(ClientStream.class);
    ClientStream mockStream2 = mock(ClientStream.class);
    Metadata headers = new Metadata();
    Metadata headers2 = new Metadata();

    // Configure the picker so that first RPC goes to delayed transport, and second RPC goes to
    // real transport.
    Subchannel subchannel = helper.createSubchannel(addressGroup, Attributes.EMPTY);
    subchannel.requestConnection();
    verify(mockTransportFactory).newClientTransport(
        any(SocketAddress.class), any(String.class), any(String.class));
    MockClientTransportInfo transportInfo = transports.poll();
    ConnectionClientTransport mockTransport = transportInfo.transport;
    verify(mockTransport).start(any(ManagedClientTransport.Listener.class));
    ManagedClientTransport.Listener transportListener = transportInfo.listener;
    when(mockTransport.newStream(same(method), same(headers), same(CallOptions.DEFAULT)))
        .thenReturn(mockStream);
    when(mockTransport.newStream(same(method), same(headers2), same(CallOptions.DEFAULT)))
        .thenReturn(mockStream2);
    transportListener.transportReady();
    when(mockPicker.pickSubchannel(
        new PickSubchannelArgsImpl(method, headers, CallOptions.DEFAULT))).thenReturn(
        PickResult.withNoResult());
    when(mockPicker.pickSubchannel(
        new PickSubchannelArgsImpl(method, headers2, CallOptions.DEFAULT))).thenReturn(
        PickResult.withSubchannel(subchannel));
    helper.updatePicker(mockPicker);

    // First RPC, will be pending
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    verifyNoMoreInteractions(mockTransportFactory);
    call.start(mockCallListener, headers);

    verify(mockTransport, never())
        .newStream(same(method), same(headers), same(CallOptions.DEFAULT));

    // Second RPC, will be assigned to the real transport
    ClientCall<String, Integer> call2 = channel.newCall(method, CallOptions.DEFAULT);
    call2.start(mockCallListener2, headers2);
    verify(mockTransport).newStream(same(method), same(headers2), same(CallOptions.DEFAULT));
    verify(mockTransport).newStream(same(method), same(headers2), same(CallOptions.DEFAULT));
    verify(mockStream2).start(any(ClientStreamListener.class));

    // Shutdown
    if (shutdownNow) {
      channel.shutdownNow();
    } else {
      channel.shutdown();
      if (shutdownNowAfterShutdown) {
        channel.shutdownNow();
        shutdownNow = true;
      }
    }
    assertTrue(channel.isShutdown());
    assertFalse(channel.isTerminated());
    assertEquals(1, nameResolverFactory.resolvers.size());
    verify(mockLoadBalancerFactory).newLoadBalancer(any(Helper.class));

    // Further calls should fail without going to the transport
    ClientCall<String, Integer> call3 = channel.newCall(method, CallOptions.DEFAULT);
    call3.start(mockCallListener3, headers2);
    timer.runDueTasks();
    executor.runDueTasks();

    verify(mockCallListener3).onClose(statusCaptor.capture(), any(Metadata.class));
    assertSame(Status.Code.UNAVAILABLE, statusCaptor.getValue().getCode());

    if (shutdownNow) {
      // LoadBalancer and NameResolver are shut down as soon as delayed transport is terminated.
      verify(mockLoadBalancer).shutdown();
      assertTrue(nameResolverFactory.resolvers.get(0).shutdown);
      // call should have been aborted by delayed transport
      executor.runDueTasks();
      verify(mockCallListener).onClose(same(ManagedChannelImpl.SHUTDOWN_NOW_STATUS),
          any(Metadata.class));
    } else {
      // LoadBalancer and NameResolver are still running.
      verify(mockLoadBalancer, never()).shutdown();
      assertFalse(nameResolverFactory.resolvers.get(0).shutdown);
      // call and call2 are still alive, and can still be assigned to a real transport
      SubchannelPicker picker2 = mock(SubchannelPicker.class);
      when(picker2.pickSubchannel(new PickSubchannelArgsImpl(method, headers, CallOptions.DEFAULT)))
          .thenReturn(PickResult.withSubchannel(subchannel));
      helper.updatePicker(picker2);
      executor.runDueTasks();
      verify(mockTransport).newStream(same(method), same(headers), same(CallOptions.DEFAULT));
      verify(mockStream).start(any(ClientStreamListener.class));
    }

    // After call is moved out of delayed transport, LoadBalancer, NameResolver and the transports
    // will be shutdown.
    verify(mockLoadBalancer).shutdown();
    assertTrue(nameResolverFactory.resolvers.get(0).shutdown);

    if (shutdownNow) {
      // Channel shutdownNow() all subchannels after shutting down LoadBalancer
      verify(mockTransport).shutdownNow(ManagedChannelImpl.SHUTDOWN_NOW_STATUS);
    } else {
      verify(mockTransport, never()).shutdownNow(any(Status.class));
    }
    // LoadBalancer should shutdown the subchannel
    subchannel.shutdown();
    verify(mockTransport).shutdown();

    // Killing the remaining real transport will terminate the channel
    transportListener.transportShutdown(Status.UNAVAILABLE);
    assertFalse(channel.isTerminated());
    verify(executorPool, never()).returnObject(anyObject());
    verify(timerServicePool, never()).returnObject(anyObject());
    transportListener.transportTerminated();
    assertTrue(channel.isTerminated());
    verify(executorPool).returnObject(executor.getScheduledExecutorService());
    verify(timerServicePool).returnObject(timer.getScheduledExecutorService());
    verifyNoMoreInteractions(oobExecutorPool);

    verify(mockTransportFactory).close();
    verifyNoMoreInteractions(mockTransportFactory);
    verify(mockTransport, atLeast(0)).getLogId();
    verifyNoMoreInteractions(mockTransport);
  }

  @Test
  public void shutdownNowWithMultipleOobChannels() {
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
  public void callOptionsExecutor() {
    Metadata headers = new Metadata();
    ClientStream mockStream = mock(ClientStream.class);
    FakeClock callExecutor = new FakeClock();
    createChannel(new FakeNameResolverFactory(true), NO_INTERCEPTOR);

    // Start a call with a call executor
    CallOptions options =
        CallOptions.DEFAULT.withExecutor(callExecutor.getScheduledExecutorService());
    ClientCall<String, Integer> call = channel.newCall(method, options);
    call.start(mockCallListener, headers);

    // Make the transport available
    Subchannel subchannel = helper.createSubchannel(addressGroup, Attributes.EMPTY);
    verify(mockTransportFactory, never()).newClientTransport(
        any(SocketAddress.class), any(String.class), any(String.class));
    subchannel.requestConnection();
    verify(mockTransportFactory).newClientTransport(
        any(SocketAddress.class), any(String.class), any(String.class));
    MockClientTransportInfo transportInfo = transports.poll();
    ConnectionClientTransport mockTransport = transportInfo.transport;
    ManagedClientTransport.Listener transportListener = transportInfo.listener;
    when(mockTransport.newStream(same(method), same(headers), any(CallOptions.class)))
        .thenReturn(mockStream);
    transportListener.transportReady();
    when(mockPicker.pickSubchannel(any(PickSubchannelArgs.class)))
        .thenReturn(PickResult.withSubchannel(subchannel));
    assertEquals(0, callExecutor.numPendingTasks());
    helper.updatePicker(mockPicker);

    // Real streams are started in the call executor if they were previously buffered.
    assertEquals(1, callExecutor.runDueTasks());
    verify(mockTransport).newStream(same(method), same(headers), same(options));
    verify(mockStream).start(streamListenerCaptor.capture());

    // Call listener callbacks are also run in the call executor
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
    verify(mockLoadBalancer).handleNameResolutionError(same(error));
  }

  @Test
  public void nameResolverReturnsEmptySubLists() {
    String errorDescription = "NameResolver returned an empty list";

    // Pass a FakeNameResolverFactory with an empty list
    createChannel(new FakeNameResolverFactory(), NO_INTERCEPTOR);

    // LoadBalancer received the error
    verify(mockLoadBalancerFactory).newLoadBalancer(any(Helper.class));
    verify(mockLoadBalancer).handleNameResolutionError(statusCaptor.capture());
    Status status = statusCaptor.getValue();
    assertSame(Status.Code.UNAVAILABLE, status.getCode());
    assertEquals(errorDescription, status.getDescription());
  }

  @Test
  public void loadBalancerThrowsInHandleResolvedAddresses() {
    RuntimeException ex = new RuntimeException("simulated");
    // Delay the success of name resolution until allResolved() is called
    FakeNameResolverFactory nameResolverFactory = new FakeNameResolverFactory(false);
    createChannel(nameResolverFactory, NO_INTERCEPTOR);

    verify(mockLoadBalancerFactory).newLoadBalancer(any(Helper.class));
    doThrow(ex).when(mockLoadBalancer).handleResolvedAddressGroups(
        Matchers.<List<EquivalentAddressGroup>>anyObject(), any(Attributes.class));

    // NameResolver returns addresses.
    nameResolverFactory.allResolved();

    // The LoadBalancer will receive the error that it has thrown.
    verify(mockLoadBalancer).handleNameResolutionError(statusCaptor.capture());
    Status status = statusCaptor.getValue();
    assertSame(Status.Code.INTERNAL, status.getCode());
    assertSame(ex, status.getCause());
  }

  @Test
  public void nameResolvedAfterChannelShutdown() {
    // Delay the success of name resolution until allResolved() is called.
    FakeNameResolverFactory nameResolverFactory = new FakeNameResolverFactory(false);
    createChannel(nameResolverFactory, NO_INTERCEPTOR);

    channel.shutdown();

    assertTrue(channel.isShutdown());
    assertTrue(channel.isTerminated());
    verify(mockLoadBalancer).shutdown();
    // Name resolved after the channel is shut down, which is possible if the name resolution takes
    // time and is not cancellable. The resolved address will be dropped.
    nameResolverFactory.allResolved();
    verifyNoMoreInteractions(mockLoadBalancer);
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
    InOrder inOrder = inOrder(mockLoadBalancer);

    List<SocketAddress> resolvedAddrs = Arrays.asList(badAddress, goodAddress);
    FakeNameResolverFactory nameResolverFactory = new FakeNameResolverFactory(resolvedAddrs);
    createChannel(nameResolverFactory, NO_INTERCEPTOR);

    // Start the call
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    Metadata headers = new Metadata();
    call.start(mockCallListener, headers);
    executor.runDueTasks();

    // Simulate name resolution results
    EquivalentAddressGroup addressGroup = new EquivalentAddressGroup(resolvedAddrs);
    inOrder.verify(mockLoadBalancer).handleResolvedAddressGroups(
        eq(Arrays.asList(addressGroup)), eq(Attributes.EMPTY));
    Subchannel subchannel = helper.createSubchannel(addressGroup, Attributes.EMPTY);
    when(mockPicker.pickSubchannel(any(PickSubchannelArgs.class)))
        .thenReturn(PickResult.withSubchannel(subchannel));
    subchannel.requestConnection();
    inOrder.verify(mockLoadBalancer).handleSubchannelState(
        same(subchannel), stateInfoCaptor.capture());
    assertEquals(CONNECTING, stateInfoCaptor.getValue().getState());

    // The channel will starts with the first address (badAddress)
    verify(mockTransportFactory)
        .newClientTransport(same(badAddress), any(String.class), any(String.class));
    verify(mockTransportFactory, times(0))
          .newClientTransport(same(goodAddress), any(String.class), any(String.class));

    MockClientTransportInfo badTransportInfo = transports.poll();
    // Which failed to connect
    badTransportInfo.listener.transportShutdown(Status.UNAVAILABLE);
    inOrder.verifyNoMoreInteractions();

    // The channel then try the second address (goodAddress)
    verify(mockTransportFactory)
          .newClientTransport(same(goodAddress), any(String.class), any(String.class));
    MockClientTransportInfo goodTransportInfo = transports.poll();
    when(goodTransportInfo.transport.newStream(
            any(MethodDescriptor.class), any(Metadata.class), any(CallOptions.class)))
        .thenReturn(mock(ClientStream.class));

    goodTransportInfo.listener.transportReady();
    inOrder.verify(mockLoadBalancer).handleSubchannelState(
        same(subchannel), stateInfoCaptor.capture());
    assertEquals(READY, stateInfoCaptor.getValue().getState());

    // A typical LoadBalancer will call this once the subchannel becomes READY
    helper.updatePicker(mockPicker);
    // Delayed transport uses the app executor to create real streams.
    executor.runDueTasks();

    verify(goodTransportInfo.transport).newStream(same(method), same(headers),
        same(CallOptions.DEFAULT));
    // The bad transport was never used.
    verify(badTransportInfo.transport, times(0)).newStream(any(MethodDescriptor.class),
        any(Metadata.class), any(CallOptions.class));
  }

  /**
   * Verify that if all resolved addresses failed to connect, a fail-fast call will fail, while a
   * wait-for-ready call will still be buffered.
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
    InOrder inOrder = inOrder(mockLoadBalancer);

    List<SocketAddress> resolvedAddrs = Arrays.asList(addr1, addr2);

    FakeNameResolverFactory nameResolverFactory = new FakeNameResolverFactory(resolvedAddrs);
    createChannel(nameResolverFactory, NO_INTERCEPTOR);

    // Start a wait-for-ready call
    ClientCall<String, Integer> call =
        channel.newCall(method, CallOptions.DEFAULT.withWaitForReady());
    Metadata headers = new Metadata();
    call.start(mockCallListener, headers);
    // ... and a fail-fast call
    ClientCall<String, Integer> call2 =
        channel.newCall(method, CallOptions.DEFAULT.withoutWaitForReady());
    call2.start(mockCallListener2, headers);
    executor.runDueTasks();

    // Simulate name resolution results
    EquivalentAddressGroup addressGroup = new EquivalentAddressGroup(resolvedAddrs);
    inOrder.verify(mockLoadBalancer).handleResolvedAddressGroups(
        eq(Arrays.asList(addressGroup)), eq(Attributes.EMPTY));
    Subchannel subchannel = helper.createSubchannel(addressGroup, Attributes.EMPTY);
    when(mockPicker.pickSubchannel(any(PickSubchannelArgs.class)))
        .thenReturn(PickResult.withSubchannel(subchannel));
    subchannel.requestConnection();
    inOrder.verify(mockLoadBalancer).handleSubchannelState(
        same(subchannel), stateInfoCaptor.capture());
    assertEquals(CONNECTING, stateInfoCaptor.getValue().getState());

    // Connecting to server1, which will fail
    verify(mockTransportFactory)
        .newClientTransport(same(addr1), any(String.class), any(String.class));
    verify(mockTransportFactory, times(0))
        .newClientTransport(same(addr2), any(String.class), any(String.class));
    MockClientTransportInfo transportInfo1 = transports.poll();
    transportInfo1.listener.transportShutdown(Status.UNAVAILABLE);

    // Connecting to server2, which will fail too
    verify(mockTransportFactory)
        .newClientTransport(same(addr2), any(String.class), any(String.class));
    MockClientTransportInfo transportInfo2 = transports.poll();
    Status server2Error = Status.UNAVAILABLE.withDescription("Server2 failed to connect");
    transportInfo2.listener.transportShutdown(server2Error);

    // ... which makes the subchannel enter TRANSIENT_FAILURE. The last error Status is propagated
    // to LoadBalancer.
    inOrder.verify(mockLoadBalancer).handleSubchannelState(
        same(subchannel), stateInfoCaptor.capture());
    assertEquals(TRANSIENT_FAILURE, stateInfoCaptor.getValue().getState());
    assertSame(server2Error, stateInfoCaptor.getValue().getStatus());

    // A typical LoadBalancer would create a picker with error
    SubchannelPicker picker2 = mock(SubchannelPicker.class);
    when(picker2.pickSubchannel(any(PickSubchannelArgs.class)))
        .thenReturn(PickResult.withError(server2Error));
    helper.updatePicker(picker2);
    executor.runDueTasks();

    // ... which fails the fail-fast call
    verify(mockCallListener2).onClose(same(server2Error), any(Metadata.class));
    // ... while the wait-for-ready call stays
    verifyNoMoreInteractions(mockCallListener);
    // No real stream was ever created
    verify(transportInfo1.transport, times(0))
        .newStream(any(MethodDescriptor.class), any(Metadata.class));
    verify(transportInfo2.transport, times(0))
        .newStream(any(MethodDescriptor.class), any(Metadata.class));
  }

  @Test
  public void subchannels() {
    createChannel(new FakeNameResolverFactory(true), NO_INTERCEPTOR);

    // createSubchannel() always return a new Subchannel
    Attributes attrs1 = Attributes.newBuilder().set(SUBCHANNEL_ATTR_KEY, "attr1").build();
    Attributes attrs2 = Attributes.newBuilder().set(SUBCHANNEL_ATTR_KEY, "attr2").build();
    Subchannel sub1 = helper.createSubchannel(addressGroup, attrs1);
    Subchannel sub2 = helper.createSubchannel(addressGroup, attrs2);
    assertNotSame(sub1, sub2);
    assertNotSame(attrs1, attrs2);
    assertSame(attrs1, sub1.getAttributes());
    assertSame(attrs2, sub2.getAttributes());
    assertSame(addressGroup, sub1.getAddresses());
    assertSame(addressGroup, sub2.getAddresses());

    // requestConnection()
    verify(mockTransportFactory, never()).newClientTransport(
        any(SocketAddress.class), any(String.class), any(String.class));
    sub1.requestConnection();
    verify(mockTransportFactory).newClientTransport(socketAddress, authority, userAgent);
    MockClientTransportInfo transportInfo1 = transports.poll();
    assertNotNull(transportInfo1);

    sub2.requestConnection();
    verify(mockTransportFactory, times(2)).newClientTransport(socketAddress, authority, userAgent);
    MockClientTransportInfo transportInfo2 = transports.poll();
    assertNotNull(transportInfo2);

    sub1.requestConnection();
    sub2.requestConnection();
    verify(mockTransportFactory, times(2)).newClientTransport(socketAddress, authority, userAgent);

    // shutdown() has a delay
    sub1.shutdown();
    timer.forwardTime(ManagedChannelImpl.SUBCHANNEL_SHUTDOWN_DELAY_SECONDS - 1, TimeUnit.SECONDS);
    sub1.shutdown();
    verify(transportInfo1.transport, never()).shutdown();
    timer.forwardTime(1, TimeUnit.SECONDS);
    verify(transportInfo1.transport).shutdown();

    // ... but not after Channel is terminating
    verify(mockLoadBalancer, never()).shutdown();
    channel.shutdown();
    verify(mockLoadBalancer).shutdown();
    verify(transportInfo2.transport, never()).shutdown();

    sub2.shutdown();
    verify(transportInfo2.transport).shutdown();
  }

  @Test
  public void subchannelsWhenChannelShutdownNow() {
    createChannel(new FakeNameResolverFactory(true), NO_INTERCEPTOR);
    Subchannel sub1 = helper.createSubchannel(addressGroup, Attributes.EMPTY);
    Subchannel sub2 = helper.createSubchannel(addressGroup, Attributes.EMPTY);
    sub1.requestConnection();
    sub2.requestConnection();

    assertEquals(2, transports.size());
    MockClientTransportInfo ti1 = transports.poll();
    MockClientTransportInfo ti2 = transports.poll();

    ti1.listener.transportReady();
    ti2.listener.transportReady();

    channel.shutdownNow();
    verify(ti1.transport).shutdownNow(any(Status.class));
    verify(ti2.transport).shutdownNow(any(Status.class));

    ti1.listener.transportShutdown(Status.UNAVAILABLE.withDescription("shutdown now"));
    ti2.listener.transportShutdown(Status.UNAVAILABLE.withDescription("shutdown now"));
    ti1.listener.transportTerminated();

    assertFalse(channel.isTerminated());
    ti2.listener.transportTerminated();
    assertTrue(channel.isTerminated());
  }

  @Test
  public void subchannelsNoConnectionShutdown() {
    createChannel(new FakeNameResolverFactory(true), NO_INTERCEPTOR);
    Subchannel sub1 = helper.createSubchannel(addressGroup, Attributes.EMPTY);
    Subchannel sub2 = helper.createSubchannel(addressGroup, Attributes.EMPTY);

    channel.shutdown();
    verify(mockLoadBalancer).shutdown();
    sub1.shutdown();
    assertFalse(channel.isTerminated());
    sub2.shutdown();
    assertTrue(channel.isTerminated());
    verify(mockTransportFactory, never()).newClientTransport(any(SocketAddress.class), anyString(),
        anyString());
  }

  @Test
  public void subchannelsNoConnectionShutdownNow() {
    createChannel(new FakeNameResolverFactory(true), NO_INTERCEPTOR);
    helper.createSubchannel(addressGroup, Attributes.EMPTY);
    helper.createSubchannel(addressGroup, Attributes.EMPTY);
    channel.shutdownNow();

    verify(mockLoadBalancer).shutdown();
    // Channel's shutdownNow() will call shutdownNow() on all subchannels and oobchannels.
    // Therefore, channel is terminated without relying on LoadBalancer to shutdown subchannels.
    assertTrue(channel.isTerminated());
    verify(mockTransportFactory, never()).newClientTransport(any(SocketAddress.class), anyString(),
        anyString());
  }

  @Test
  public void oobchannels() {
    createChannel(new FakeNameResolverFactory(true), NO_INTERCEPTOR);

    ManagedChannel oob1 = helper.createOobChannel(addressGroup, "oob1authority");
    ManagedChannel oob2 = helper.createOobChannel(addressGroup, "oob2authority");
    verify(oobExecutorPool, times(2)).getObject();

    assertEquals("oob1authority", oob1.authority());
    assertEquals("oob2authority", oob2.authority());

    // OOB channels create connections lazily.  A new call will initiate the connection.
    Metadata headers = new Metadata();
    ClientCall<String, Integer> call = oob1.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, headers);
    verify(mockTransportFactory).newClientTransport(socketAddress, "oob1authority", userAgent);
    MockClientTransportInfo transportInfo = transports.poll();
    assertNotNull(transportInfo);

    assertEquals(0, oobExecutor.numPendingTasks());
    transportInfo.listener.transportReady();
    assertEquals(1, oobExecutor.runDueTasks());
    verify(transportInfo.transport).newStream(same(method), same(headers),
        same(CallOptions.DEFAULT));

    // The transport goes away
    transportInfo.listener.transportShutdown(Status.UNAVAILABLE);
    transportInfo.listener.transportTerminated();

    // A new call will trigger a new transport
    ClientCall<String, Integer> call2 = oob1.newCall(method, CallOptions.DEFAULT);
    call2.start(mockCallListener2, headers);
    ClientCall<String, Integer> call3 =
        oob1.newCall(method, CallOptions.DEFAULT.withWaitForReady());
    call3.start(mockCallListener3, headers);
    verify(mockTransportFactory, times(2)).newClientTransport(
        socketAddress, "oob1authority", userAgent);
    transportInfo = transports.poll();
    assertNotNull(transportInfo);

    // This transport fails
    Status transportError = Status.UNAVAILABLE.withDescription("Connection refused");
    assertEquals(0, oobExecutor.numPendingTasks());
    transportInfo.listener.transportShutdown(transportError);
    assertTrue(oobExecutor.runDueTasks() > 0);

    // Fail-fast RPC will fail, while wait-for-ready RPC will still be pending
    verify(mockCallListener2).onClose(same(transportError), any(Metadata.class));
    verify(mockCallListener3, never()).onClose(any(Status.class), any(Metadata.class));

    // Shutdown
    assertFalse(oob1.isShutdown());
    assertFalse(oob2.isShutdown());
    oob1.shutdown();
    verify(oobExecutorPool, never()).returnObject(anyObject());
    oob2.shutdownNow();
    assertTrue(oob1.isShutdown());
    assertTrue(oob2.isShutdown());
    assertTrue(oob2.isTerminated());
    verify(oobExecutorPool).returnObject(oobExecutor.getScheduledExecutorService());

    // New RPCs will be rejected.
    assertEquals(0, oobExecutor.numPendingTasks());
    ClientCall<String, Integer> call4 = oob1.newCall(method, CallOptions.DEFAULT);
    ClientCall<String, Integer> call5 = oob2.newCall(method, CallOptions.DEFAULT);
    call4.start(mockCallListener4, headers);
    call5.start(mockCallListener5, headers);
    assertTrue(oobExecutor.runDueTasks() > 0);
    verify(mockCallListener4).onClose(statusCaptor.capture(), any(Metadata.class));
    Status status4 = statusCaptor.getValue();
    assertEquals(Status.Code.UNAVAILABLE, status4.getCode());
    verify(mockCallListener5).onClose(statusCaptor.capture(), any(Metadata.class));
    Status status5 = statusCaptor.getValue();
    assertEquals(Status.Code.UNAVAILABLE, status5.getCode());

    // The pending RPC will still be pending
    verify(mockCallListener3, never()).onClose(any(Status.class), any(Metadata.class));

    // This will shutdownNow() the delayed transport, terminating the pending RPC
    assertEquals(0, oobExecutor.numPendingTasks());
    oob1.shutdownNow();
    assertTrue(oobExecutor.runDueTasks() > 0);
    verify(mockCallListener3).onClose(any(Status.class), any(Metadata.class));

    // Shut down the channel, and it will not terminated because OOB channel has not.
    channel.shutdown();
    assertFalse(channel.isTerminated());
    // Delayed transport has already terminated.  Terminating the transport terminates the
    // subchannel, which in turn terimates the OOB channel, which terminates the channel.
    assertFalse(oob1.isTerminated());
    verify(oobExecutorPool).returnObject(oobExecutor.getScheduledExecutorService());
    transportInfo.listener.transportTerminated();
    assertTrue(oob1.isTerminated());
    assertTrue(channel.isTerminated());
    verify(oobExecutorPool, times(2)).returnObject(oobExecutor.getScheduledExecutorService());
  }

  @Test
  public void oobChannelsWhenChannelShutdownNow() {
    createChannel(new FakeNameResolverFactory(true), NO_INTERCEPTOR);
    ManagedChannel oob1 = helper.createOobChannel(addressGroup, "oob1Authority");
    ManagedChannel oob2 = helper.createOobChannel(addressGroup, "oob2Authority");

    oob1.newCall(method, CallOptions.DEFAULT).start(mockCallListener, new Metadata());
    oob2.newCall(method, CallOptions.DEFAULT).start(mockCallListener2, new Metadata());

    assertEquals(2, transports.size());
    MockClientTransportInfo ti1 = transports.poll();
    MockClientTransportInfo ti2 = transports.poll();

    ti1.listener.transportReady();
    ti2.listener.transportReady();

    channel.shutdownNow();
    verify(ti1.transport).shutdownNow(any(Status.class));
    verify(ti2.transport).shutdownNow(any(Status.class));

    ti1.listener.transportShutdown(Status.UNAVAILABLE.withDescription("shutdown now"));
    ti2.listener.transportShutdown(Status.UNAVAILABLE.withDescription("shutdown now"));
    ti1.listener.transportTerminated();

    assertFalse(channel.isTerminated());
    ti2.listener.transportTerminated();
    assertTrue(channel.isTerminated());
  }

  @Test
  public void oobChannelsNoConnectionShutdown() {
    createChannel(new FakeNameResolverFactory(true), NO_INTERCEPTOR);
    ManagedChannel oob1 = helper.createOobChannel(addressGroup, "oob1Authority");
    ManagedChannel oob2 = helper.createOobChannel(addressGroup, "oob2Authority");
    channel.shutdown();

    verify(mockLoadBalancer).shutdown();
    oob1.shutdown();
    assertTrue(oob1.isTerminated());
    assertFalse(channel.isTerminated());
    oob2.shutdown();
    assertTrue(oob2.isTerminated());
    assertTrue(channel.isTerminated());
    verify(mockTransportFactory, never()).newClientTransport(any(SocketAddress.class), anyString(),
        anyString());
  }

  @Test
  public void oobChannelsNoConnectionShutdownNow() {
    createChannel(new FakeNameResolverFactory(true), NO_INTERCEPTOR);
    helper.createOobChannel(addressGroup, "oob1Authority");
    helper.createOobChannel(addressGroup, "oob2Authority");
    channel.shutdownNow();

    verify(mockLoadBalancer).shutdown();
    assertTrue(channel.isTerminated());
    // Channel's shutdownNow() will call shutdownNow() on all subchannels and oobchannels.
    // Therefore, channel is terminated without relying on LoadBalancer to shutdown oobchannels.
    verify(mockTransportFactory, never()).newClientTransport(any(SocketAddress.class), anyString(),
        anyString());
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

    // First call will be on delayed transport.  Only newCall() is run within the expected context,
    // so that we can verify that the context is explicitly attached before calling newStream() and
    // applyRequestMetadata(), which happens after we detach the context from the thread.
    Context origCtx = ctx.attach();
    assertEquals("testValue", testKey.get());
    ClientCall<String, Integer> call = channel.newCall(method, callOptions);
    ctx.detach(origCtx);
    assertNull(testKey.get());
    call.start(mockCallListener, new Metadata());

    // Simulate name resolution results
    EquivalentAddressGroup addressGroup = new EquivalentAddressGroup(socketAddress);
    Subchannel subchannel = helper.createSubchannel(addressGroup, Attributes.EMPTY);
    subchannel.requestConnection();
    verify(mockTransportFactory).newClientTransport(
        same(socketAddress), eq(authority), eq(userAgent));
    MockClientTransportInfo transportInfo = transports.poll();
    final ConnectionClientTransport transport = transportInfo.transport;
    when(transport.getAttributes()).thenReturn(Attributes.EMPTY);
    doAnswer(new Answer<ClientStream>() {
        @Override
        public ClientStream answer(InvocationOnMock in) throws Throwable {
          newStreamContexts.add(Context.current());
          return mock(ClientStream.class);
        }
      }).when(transport).newStream(
          any(MethodDescriptor.class), any(Metadata.class), any(CallOptions.class));

    verify(creds, never()).applyRequestMetadata(
        any(MethodDescriptor.class), any(Attributes.class), any(Executor.class),
        any(MetadataApplier.class));

    // applyRequestMetadata() is called after the transport becomes ready.
    transportInfo.listener.transportReady();
    when(mockPicker.pickSubchannel(any(PickSubchannelArgs.class)))
        .thenReturn(PickResult.withSubchannel(subchannel));
    helper.updatePicker(mockPicker);
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
        any(MethodDescriptor.class), any(Metadata.class), any(CallOptions.class));

    // newStream() is called after apply() is called
    applierCaptor.getValue().apply(new Metadata());
    verify(transport).newStream(same(method), any(Metadata.class), same(callOptions));
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
        any(MethodDescriptor.class), any(Metadata.class), any(CallOptions.class));

    // Still, newStream() is called after apply() is called
    applierCaptor.getValue().apply(new Metadata());
    verify(transport, times(2)).newStream(same(method), any(Metadata.class), same(callOptions));
    assertEquals("testValue", testKey.get(newStreamContexts.poll()));

    assertNull(testKey.get());
  }

  @Test
  public void pickerReturnsStreamTracer_noDelay() {
    ClientStream mockStream = mock(ClientStream.class);
    ClientStreamTracer.Factory factory1 = mock(ClientStreamTracer.Factory.class);
    ClientStreamTracer.Factory factory2 = mock(ClientStreamTracer.Factory.class);
    createChannel(new FakeNameResolverFactory(true), NO_INTERCEPTOR);
    Subchannel subchannel = helper.createSubchannel(addressGroup, Attributes.EMPTY);
    subchannel.requestConnection();
    MockClientTransportInfo transportInfo = transports.poll();
    transportInfo.listener.transportReady();
    ClientTransport mockTransport = transportInfo.transport;
    when(mockTransport.newStream(
            any(MethodDescriptor.class), any(Metadata.class), any(CallOptions.class)))
        .thenReturn(mockStream);

    when(mockPicker.pickSubchannel(any(PickSubchannelArgs.class))).thenReturn(
        PickResult.withSubchannel(subchannel, factory2));
    helper.updatePicker(mockPicker);

    CallOptions callOptions = CallOptions.DEFAULT.withStreamTracerFactory(factory1);
    ClientCall<String, Integer> call = channel.newCall(method, callOptions);
    call.start(mockCallListener, new Metadata());

    verify(mockPicker).pickSubchannel(any(PickSubchannelArgs.class));
    verify(mockTransport).newStream(same(method), any(Metadata.class), callOptionsCaptor.capture());
    assertEquals(
        Arrays.asList(factory1, factory2),
        callOptionsCaptor.getValue().getStreamTracerFactories());
    // The factories are safely not stubbed because we do not expect any usage of them.
    verifyZeroInteractions(factory1);
    verifyZeroInteractions(factory2);
  }

  @Test
  public void pickerReturnsStreamTracer_delayed() {
    ClientStream mockStream = mock(ClientStream.class);
    ClientStreamTracer.Factory factory1 = mock(ClientStreamTracer.Factory.class);
    ClientStreamTracer.Factory factory2 = mock(ClientStreamTracer.Factory.class);
    createChannel(new FakeNameResolverFactory(true), NO_INTERCEPTOR);

    CallOptions callOptions = CallOptions.DEFAULT.withStreamTracerFactory(factory1);
    ClientCall<String, Integer> call = channel.newCall(method, callOptions);
    call.start(mockCallListener, new Metadata());

    Subchannel subchannel = helper.createSubchannel(addressGroup, Attributes.EMPTY);
    subchannel.requestConnection();
    MockClientTransportInfo transportInfo = transports.poll();
    transportInfo.listener.transportReady();
    ClientTransport mockTransport = transportInfo.transport;
    when(mockTransport.newStream(
            any(MethodDescriptor.class), any(Metadata.class), any(CallOptions.class)))
        .thenReturn(mockStream);
    when(mockPicker.pickSubchannel(any(PickSubchannelArgs.class))).thenReturn(
        PickResult.withSubchannel(subchannel, factory2));

    helper.updatePicker(mockPicker);
    assertEquals(1, executor.runDueTasks());

    verify(mockPicker).pickSubchannel(any(PickSubchannelArgs.class));
    verify(mockTransport).newStream(same(method), any(Metadata.class), callOptionsCaptor.capture());
    assertEquals(
        Arrays.asList(factory1, factory2),
        callOptionsCaptor.getValue().getStreamTracerFactories());
    // The factories are safely not stubbed because we do not expect any usage of them.
    verifyZeroInteractions(factory1);
    verifyZeroInteractions(factory2);
  }

  private static class FakeBackoffPolicyProvider implements BackoffPolicy.Provider {
    @Override
    public BackoffPolicy get() {
      return new BackoffPolicy() {
        @Override
        public long nextBackoffNanos() {
          return 1;
        }
      };
    }
  }

  private class FakeNameResolverFactory extends NameResolver.Factory {
    final List<EquivalentAddressGroup> servers;
    final boolean resolvedAtStart;
    final ArrayList<FakeNameResolver> resolvers = new ArrayList<FakeNameResolver>();

    FakeNameResolverFactory(boolean resolvedAtStart) {
      this.resolvedAtStart = resolvedAtStart;
      servers = Collections.singletonList(new EquivalentAddressGroup(socketAddress));
    }

    FakeNameResolverFactory(List<SocketAddress> servers) {
      resolvedAtStart = true;
      this.servers = Collections.singletonList(new EquivalentAddressGroup(servers));
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
        listener.onAddresses(servers, Attributes.EMPTY);
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
}
