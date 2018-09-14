/*
 * Copyright 2016 The gRPC Authors
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

import static io.grpc.ConnectivityState.READY;
import static io.grpc.ConnectivityState.TRANSIENT_FAILURE;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
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
import io.grpc.internal.FakeClock.ScheduledTask;
import io.grpc.internal.TestUtils.MockClientTransportInfo;
import java.net.SocketAddress;
import java.net.URI;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.Executor;
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
  private static final ProxyParameters NO_PROXY = null;
  private static final long IDLE_TIMEOUT_SECONDS = 30;
  private ManagedChannelImpl channel;

  private final MethodDescriptor<String, Integer> method =
      MethodDescriptor.<String, Integer>newBuilder()
          .setType(MethodType.UNKNOWN)
          .setFullMethodName("service/method")
          .setRequestMarshaller(new StringMarshaller())
          .setResponseMarshaller(new IntegerMarshaller())
          .build();

  private final List<EquivalentAddressGroup> servers = Lists.newArrayList();
  private final ObjectPool<Executor> executorPool =
      new FixedObjectPool<Executor>(executor.getScheduledExecutorService());
  private final ObjectPool<Executor> oobExecutorPool =
      new FixedObjectPool<Executor>(oobExecutor.getScheduledExecutorService());

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
    when(mockLoadBalancerFactory.newLoadBalancer(any(Helper.class))).thenReturn(mockLoadBalancer);
    when(mockNameResolver.getServiceAuthority()).thenReturn(AUTHORITY);
    when(mockNameResolverFactory
        .newNameResolver(any(URI.class), any(Attributes.class)))
        .thenReturn(mockNameResolver);
    when(mockTransportFactory.getScheduledExecutorService())
        .thenReturn(timer.getScheduledExecutorService());

    class Builder extends AbstractManagedChannelImplBuilder<Builder> {
      Builder(String target) {
        super(target);
      }

      @Override protected ClientTransportFactory buildTransportFactory() {
        throw new UnsupportedOperationException();
      }

      @Override public Builder usePlaintext() {
        throw new UnsupportedOperationException();
      }
    }

    Builder builder = new Builder("fake://target")
        .nameResolverFactory(mockNameResolverFactory)
        .loadBalancerFactory(mockLoadBalancerFactory)
        .idleTimeout(IDLE_TIMEOUT_SECONDS, TimeUnit.SECONDS)
        .userAgent(USER_AGENT);
    builder.executorPool = executorPool;
    channel = new ManagedChannelImpl(
        builder, mockTransportFactory, new FakeBackoffPolicyProvider(),
        oobExecutorPool, timer.getStopwatchSupplier(),
        Collections.<ClientInterceptor>emptyList(),
        TimeProvider.SYSTEM_TIME_PROVIDER);
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
        any(SocketAddress.class),
        any(ClientTransportFactory.ClientTransportOptions.class));
    verify(mockNameResolver, never()).start(any(NameResolver.Listener.class));
  }

  @After
  public void allPendingTasksAreRun() {
    Collection<ScheduledTask> pendingTimerTasks = timer.getPendingTasks();
    for (ScheduledTask a : pendingTimerTasks) {
      assertFalse(Rescheduler.isEnabled(a.command));
    }
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
    helper.updateBalancingState(READY, mockPicker);
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

    List<SocketAddress> changedList = new ArrayList<>(servers.get(0).getAddresses());
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
    helper.updateBalancingState(TRANSIENT_FAILURE, failingPicker);
    executor.runDueTasks();
    verify(mockCallListener).onClose(same(Status.UNAVAILABLE), any(Metadata.class));

    // ... so that the channel resets its in-use state
    assertFalse(channel.inUseStateAggregator.isInUse());

    // Now make an RPC on an OOB channel
    ManagedChannel oob = helper.createOobChannel(servers.get(0), "oobauthority");
    verify(mockTransportFactory, never())
        .newClientTransport(
            any(SocketAddress.class),
            eq(new ClientTransportFactory.ClientTransportOptions()
              .setAuthority("oobauthority")
              .setUserAgent(USER_AGENT)));
    ClientCall<String, Integer> oobCall = oob.newCall(method, CallOptions.DEFAULT);
    oobCall.start(mockCallListener2, new Metadata());
    verify(mockTransportFactory)
        .newClientTransport(
            any(SocketAddress.class),
            eq(new ClientTransportFactory.ClientTransportOptions()
              .setAuthority("oobauthority")
              .setUserAgent(USER_AGENT)));
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

    List<SocketAddress> changedList = new ArrayList<>(servers.get(0).getAddresses());
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
