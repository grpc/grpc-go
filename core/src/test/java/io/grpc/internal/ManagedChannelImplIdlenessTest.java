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

package io.grpc.internal;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyString;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.google.common.collect.Lists;
import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.CompressorRegistry;
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
import io.grpc.Status;
import io.grpc.StringMarshaller;
import io.grpc.internal.TestUtils.MockClientTransportInfo;
import java.net.SocketAddress;
import java.net.URI;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.Executor;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/**
 * Unit tests for {@link ManagedChannelImpl}'s idle mode.
 */
@RunWith(JUnit4.class)
public class ManagedChannelImplIdlenessTest {
  private final FakeClock timer = new FakeClock();
  private final FakeClock executor = new FakeClock();
  private final FakeClock oobExecutor = new FakeClock();
  private static final String AUTHORITY = "fakeauthority";
  private static final String USER_AGENT = "fakeagent";
  private static final long IDLE_TIMEOUT_SECONDS = 30;
  private ManagedChannelImpl channel;

  private final MethodDescriptor<String, Integer> method =
      MethodDescriptor.<String, Integer>newBuilder()
          .setType(MethodType.UNKNOWN)
          .setFullMethodName("/service/method")
          .setRequestMarshaller(new StringMarshaller())
          .setResponseMarshaller(new IntegerMarshaller())
          .build();

  private final List<EquivalentAddressGroup> servers = Lists.newArrayList();

  @Mock private ObjectPool<ScheduledExecutorService> timerServicePool;
  @Mock private ObjectPool<Executor> executorPool;
  @Mock private ObjectPool<Executor> oobExecutorPool;
  @Mock private ClientTransportFactory mockTransportFactory;
  @Mock private LoadBalancer mockLoadBalancer;
  @Mock private LoadBalancer.Factory mockLoadBalancerFactory;
  @Mock private NameResolver mockNameResolver;
  @Mock private NameResolver.Factory mockNameResolverFactory;
  @Mock private ClientCall.Listener<Integer> mockCallListener;
  @Mock private ClientCall.Listener<Integer> mockCallListener2;
  @Captor private ArgumentCaptor<NameResolver.Listener> nameResolverListenerCaptor;
  private BlockingQueue<MockClientTransportInfo> newTransports;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    when(timerServicePool.getObject()).thenReturn(timer.getScheduledExecutorService());
    when(executorPool.getObject()).thenReturn(executor.getScheduledExecutorService());
    when(oobExecutorPool.getObject()).thenReturn(oobExecutor.getScheduledExecutorService());
    when(mockLoadBalancerFactory.newLoadBalancer(any(Helper.class))).thenReturn(mockLoadBalancer);
    when(mockNameResolver.getServiceAuthority()).thenReturn(AUTHORITY);
    when(mockNameResolverFactory
        .newNameResolver(any(URI.class), any(Attributes.class)))
        .thenReturn(mockNameResolver);

    channel = new ManagedChannelImpl("fake://target", new FakeBackoffPolicyProvider(),
        mockNameResolverFactory, Attributes.EMPTY, mockLoadBalancerFactory,
        mockTransportFactory, DecompressorRegistry.getDefaultInstance(),
        CompressorRegistry.getDefaultInstance(), timerServicePool, executorPool, oobExecutorPool,
        timer.getStopwatchSupplier(), TimeUnit.SECONDS.toMillis(IDLE_TIMEOUT_SECONDS), USER_AGENT,
        Collections.<ClientInterceptor>emptyList());
    newTransports = TestUtils.captureTransports(mockTransportFactory);

    for (int i = 0; i < 2; i++) {
      ArrayList<SocketAddress> addrs = Lists.newArrayList();
      for (int j = 0; j < 2; j++) {
        addrs.add(new FakeSocketAddress("servergroup" + i + "server" + j));
      }
      servers.add(new EquivalentAddressGroup(addrs));
    }
    verify(mockNameResolverFactory).newNameResolver(any(URI.class), any(Attributes.class));
    // Verify the initial idleness
    verify(mockLoadBalancerFactory, never()).newLoadBalancer(any(Helper.class));
    verify(mockTransportFactory, never()).newClientTransport(
        any(SocketAddress.class), anyString(), anyString());
    verify(mockNameResolver, never()).start(any(NameResolver.Listener.class));
  }

  @After
  public void allPendingTasksAreRun() {
    assertEquals(timer.getPendingTasks() + " should be empty", 0, timer.numPendingTasks());
    assertEquals(executor.getPendingTasks() + " should be empty", 0, executor.numPendingTasks());
  }

  @Test
  public void newCallExitsIdleness() throws Exception {
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, new Metadata());

    verify(mockLoadBalancerFactory).newLoadBalancer(any(Helper.class));

    verify(mockNameResolver).start(nameResolverListenerCaptor.capture());
    // Simulate new address resolved to make sure the LoadBalancer is correctly linked to
    // the NameResolver.
    nameResolverListenerCaptor.getValue().onAddresses(servers, Attributes.EMPTY);
    verify(mockLoadBalancer).handleResolvedAddressGroups(servers, Attributes.EMPTY);
  }

  @Test
  public void newCallRefreshesIdlenessTimer() throws Exception {
    // First call to exit the initial idleness, then immediately cancel the call.
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, new Metadata());
    call.cancel("For testing", null);

    // Verify that we have exited the idle mode
    verify(mockLoadBalancerFactory).newLoadBalancer(any(Helper.class));
    assertFalse(channel.inUseStateAggregator.isInUse());

    // Move closer to idleness, but not yet.
    timer.forwardTime(IDLE_TIMEOUT_SECONDS - 1, TimeUnit.SECONDS);
    verify(mockLoadBalancer, never()).shutdown();
    assertFalse(channel.inUseStateAggregator.isInUse());

    // A new call would refresh the timer
    call = channel.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, new Metadata());
    call.cancel("For testing", null);
    assertFalse(channel.inUseStateAggregator.isInUse());

    // ... so that passing the same length of time will not trigger idle mode
    timer.forwardTime(IDLE_TIMEOUT_SECONDS - 1, TimeUnit.SECONDS);
    verify(mockLoadBalancer, never()).shutdown();
    assertFalse(channel.inUseStateAggregator.isInUse());

    // ... until the time since last call has reached the timeout
    timer.forwardTime(1, TimeUnit.SECONDS);
    verify(mockLoadBalancer).shutdown();
    assertFalse(channel.inUseStateAggregator.isInUse());

    // Drain the app executor, which runs the call listeners
    verify(mockCallListener, never()).onClose(any(Status.class), any(Metadata.class));
    assertEquals(2, executor.runDueTasks());
    verify(mockCallListener, times(2)).onClose(any(Status.class), any(Metadata.class));
  }

  @Test
  public void delayedTransportHoldsOffIdleness() throws Exception {
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, new Metadata());
    assertTrue(channel.inUseStateAggregator.isInUse());

    // As long as the delayed transport is in-use (by the pending RPC), the channel won't go idle.
    timer.forwardTime(IDLE_TIMEOUT_SECONDS * 2, TimeUnit.SECONDS);
    assertTrue(channel.inUseStateAggregator.isInUse());

    // Cancelling the only RPC will reset the in-use state.
    assertEquals(0, executor.numPendingTasks());
    call.cancel("In test", null);
    assertEquals(1, executor.runDueTasks());
    assertFalse(channel.inUseStateAggregator.isInUse());
    // And allow the channel to go idle.
    timer.forwardTime(IDLE_TIMEOUT_SECONDS - 1, TimeUnit.SECONDS);
    verify(mockLoadBalancer, never()).shutdown();
    timer.forwardTime(1, TimeUnit.SECONDS);
    verify(mockLoadBalancer).shutdown();
  }

  @Test
  public void realTransportsHoldsOffIdleness() throws Exception {
    final EquivalentAddressGroup addressGroup = servers.get(1);

    // Start a call, which goes to delayed transport
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, new Metadata());

    // Verify that we have exited the idle mode
    ArgumentCaptor<Helper> helperCaptor = ArgumentCaptor.forClass(null);
    verify(mockLoadBalancerFactory).newLoadBalancer(helperCaptor.capture());
    Helper helper = helperCaptor.getValue();
    assertTrue(channel.inUseStateAggregator.isInUse());

    // Assume LoadBalancer has received an address, then create a subchannel.
    Subchannel subchannel = helper.createSubchannel(addressGroup, Attributes.EMPTY);
    subchannel.requestConnection();
    MockClientTransportInfo t0 = newTransports.poll();
    t0.listener.transportReady();

    SubchannelPicker mockPicker = mock(SubchannelPicker.class);
    when(mockPicker.pickSubchannel(any(PickSubchannelArgs.class)))
        .thenReturn(PickResult.withSubchannel(subchannel));
    helper.updatePicker(mockPicker);
    // Delayed transport creates real streams in the app executor
    executor.runDueTasks();

    // Delayed transport exits in-use, while real transport has not entered in-use yet.
    assertFalse(channel.inUseStateAggregator.isInUse());

    // Now it's in-use
    t0.listener.transportInUse(true);
    assertTrue(channel.inUseStateAggregator.isInUse());

    // As long as the transport is in-use, the channel won't go idle.
    timer.forwardTime(IDLE_TIMEOUT_SECONDS * 2, TimeUnit.SECONDS);
    assertTrue(channel.inUseStateAggregator.isInUse());

    t0.listener.transportInUse(false);
    assertFalse(channel.inUseStateAggregator.isInUse());
    // And allow the channel to go idle.
    timer.forwardTime(IDLE_TIMEOUT_SECONDS - 1, TimeUnit.SECONDS);
    verify(mockLoadBalancer, never()).shutdown();
    timer.forwardTime(1, TimeUnit.SECONDS);
    verify(mockLoadBalancer).shutdown();
  }

  @Test
  public void updateSubchannelAddresses_newAddressConnects() {
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, new Metadata()); // Create LB
    ArgumentCaptor<Helper> helperCaptor = ArgumentCaptor.forClass(null);
    verify(mockLoadBalancerFactory).newLoadBalancer(helperCaptor.capture());
    Helper helper = helperCaptor.getValue();
    Subchannel subchannel = helper.createSubchannel(servers.get(0), Attributes.EMPTY);

    subchannel.requestConnection();
    MockClientTransportInfo t0 = newTransports.poll();
    t0.listener.transportReady();

    helper.updateSubchannelAddresses(subchannel, servers.get(1));

    subchannel.requestConnection();
    MockClientTransportInfo t1 = newTransports.poll();
    t1.listener.transportReady();
  }

  @Test
  public void updateSubchannelAddresses_existingAddressDoesNotConnect() {
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, new Metadata()); // Create LB
    ArgumentCaptor<Helper> helperCaptor = ArgumentCaptor.forClass(null);
    verify(mockLoadBalancerFactory).newLoadBalancer(helperCaptor.capture());
    Helper helper = helperCaptor.getValue();
    Subchannel subchannel = helper.createSubchannel(servers.get(0), Attributes.EMPTY);

    subchannel.requestConnection();
    MockClientTransportInfo t0 = newTransports.poll();
    t0.listener.transportReady();

    List<SocketAddress> changedList = new ArrayList<SocketAddress>(servers.get(0).getAddresses());
    changedList.add(new FakeSocketAddress("aDifferentServer"));
    helper.updateSubchannelAddresses(subchannel, new EquivalentAddressGroup(changedList));

    subchannel.requestConnection();
    assertNull(newTransports.poll());
  }

  @Test
  public void oobTransportDoesNotAffectIdleness() {
    // Start a call, which goes to delayed transport
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, new Metadata());

    // Verify that we have exited the idle mode
    ArgumentCaptor<Helper> helperCaptor = ArgumentCaptor.forClass(null);
    verify(mockLoadBalancerFactory).newLoadBalancer(helperCaptor.capture());
    Helper helper = helperCaptor.getValue();

    // Fail the RPC
    SubchannelPicker failingPicker = mock(SubchannelPicker.class);
    when(failingPicker.pickSubchannel(any(PickSubchannelArgs.class)))
        .thenReturn(PickResult.withError(Status.UNAVAILABLE));
    helper.updatePicker(failingPicker);
    executor.runDueTasks();
    verify(mockCallListener).onClose(same(Status.UNAVAILABLE), any(Metadata.class));

    // ... so that the channel resets its in-use state
    assertFalse(channel.inUseStateAggregator.isInUse());

    // Now make an RPC on an OOB channel
    ManagedChannel oob = helper.createOobChannel(servers.get(0), "oobauthority");
    verify(mockTransportFactory, never())
        .newClientTransport(any(SocketAddress.class), same("oobauthority"), same(USER_AGENT));
    ClientCall<String, Integer> oobCall = oob.newCall(method, CallOptions.DEFAULT);
    oobCall.start(mockCallListener2, new Metadata());
    verify(mockTransportFactory)
        .newClientTransport(any(SocketAddress.class), same("oobauthority"), same(USER_AGENT));
    MockClientTransportInfo oobTransportInfo = newTransports.poll();
    assertEquals(0, newTransports.size());
    // The OOB transport reports in-use state
    oobTransportInfo.listener.transportInUse(true);

    // But it won't stop the channel from going idle
    verify(mockLoadBalancer, never()).shutdown();
    timer.forwardTime(IDLE_TIMEOUT_SECONDS, TimeUnit.SECONDS);
    verify(mockLoadBalancer).shutdown();
  }

  @Test
  public void updateOobChannelAddresses_newAddressConnects() {
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, new Metadata()); // Create LB
    ArgumentCaptor<Helper> helperCaptor = ArgumentCaptor.forClass(null);
    verify(mockLoadBalancerFactory).newLoadBalancer(helperCaptor.capture());
    Helper helper = helperCaptor.getValue();
    ManagedChannel oobChannel = helper.createOobChannel(servers.get(0), "localhost");

    oobChannel.newCall(method, CallOptions.DEFAULT).start(mockCallListener, new Metadata());
    MockClientTransportInfo t0 = newTransports.poll();
    t0.listener.transportReady();

    helper.updateOobChannelAddresses(oobChannel, servers.get(1));

    oobChannel.newCall(method, CallOptions.DEFAULT).start(mockCallListener, new Metadata());
    MockClientTransportInfo t1 = newTransports.poll();
    t1.listener.transportReady();
  }

  @Test
  public void updateOobChannelAddresses_existingAddressDoesNotConnect() {
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, new Metadata()); // Create LB
    ArgumentCaptor<Helper> helperCaptor = ArgumentCaptor.forClass(null);
    verify(mockLoadBalancerFactory).newLoadBalancer(helperCaptor.capture());
    Helper helper = helperCaptor.getValue();
    ManagedChannel oobChannel = helper.createOobChannel(servers.get(0), "localhost");

    oobChannel.newCall(method, CallOptions.DEFAULT).start(mockCallListener, new Metadata());
    MockClientTransportInfo t0 = newTransports.poll();
    t0.listener.transportReady();

    List<SocketAddress> changedList = new ArrayList<SocketAddress>(servers.get(0).getAddresses());
    changedList.add(new FakeSocketAddress("aDifferentServer"));
    helper.updateOobChannelAddresses(oobChannel, new EquivalentAddressGroup(changedList));

    oobChannel.newCall(method, CallOptions.DEFAULT).start(mockCallListener, new Metadata());
    assertNull(newTransports.poll());
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

  private static class FakeSocketAddress extends SocketAddress {
    final String name;

    FakeSocketAddress(String name) {
      this.name = name;
    }

    @Override
    public String toString() {
      return "FakeSocketAddress-" + name;
    }
  }
}
