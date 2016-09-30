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
import static org.junit.Assert.assertNotSame;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyString;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.never;
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
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.NameResolver;
import io.grpc.ResolvedServerInfo;
import io.grpc.ResolvedServerInfoGroup;
import io.grpc.Status;
import io.grpc.StringMarshaller;
import io.grpc.TransportManager.InterimTransport;
import io.grpc.TransportManager.OobTransportProvider;
import io.grpc.TransportManager;
import io.grpc.internal.TestUtils.MockClientTransportInfo;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.Matchers;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

import java.net.SocketAddress;
import java.net.URI;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collection;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;

/**
 * Unit tests for {@link ManagedChannelImpl}'s idle mode.
 */
@RunWith(JUnit4.class)
public class ManagedChannelImplIdlenessTest {
  private final FakeClock timer = new FakeClock();
  private final FakeClock executor = new FakeClock();
  private static final String authority = "fakeauthority";
  private static final String userAgent = "fakeagent";
  private static final long IDLE_TIMEOUT_SECONDS = 30;
  private ManagedChannelImpl channel;

  private final MethodDescriptor<String, Integer> method = MethodDescriptor.create(
      MethodDescriptor.MethodType.UNKNOWN, "/service/method",
      new StringMarshaller(), new IntegerMarshaller());

  private final List<ResolvedServerInfoGroup> servers = Lists.newArrayList();
  private final List<EquivalentAddressGroup> addressGroupList =
      new ArrayList<EquivalentAddressGroup>();
  
  @Mock private SharedResourceHolder.Resource<ScheduledExecutorService> timerService;
  @Mock private ClientTransportFactory mockTransportFactory;
  @Mock private LoadBalancer<ClientTransport> mockLoadBalancer;
  @Mock private LoadBalancer.Factory mockLoadBalancerFactory;
  @Mock private NameResolver mockNameResolver;
  @Mock private NameResolver.Factory mockNameResolverFactory;
  @Mock private ClientCall.Listener<Integer> mockCallListener;
  @Captor private ArgumentCaptor<NameResolver.Listener> nameResolverListenerCaptor;
  private BlockingQueue<MockClientTransportInfo> newTransports;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    when(timerService.create()).thenReturn(timer.getScheduledExecutorService());
    when(mockLoadBalancerFactory
        .newLoadBalancer(anyString(), Matchers.<TransportManager<ClientTransport>>any()))
        .thenReturn(mockLoadBalancer);
    when(mockNameResolver.getServiceAuthority()).thenReturn(authority);
    when(mockNameResolverFactory
        .newNameResolver(any(URI.class), any(Attributes.class)))
        .thenReturn(mockNameResolver);

    channel = new ManagedChannelImpl("fake://target", new FakeBackoffPolicyProvider(),
        mockNameResolverFactory, Attributes.EMPTY, mockLoadBalancerFactory,
        mockTransportFactory, DecompressorRegistry.getDefaultInstance(),
        CompressorRegistry.getDefaultInstance(), timerService, timer.getStopwatchSupplier(),
        TimeUnit.SECONDS.toMillis(IDLE_TIMEOUT_SECONDS),
        executor.getScheduledExecutorService(), userAgent,
        Collections.<ClientInterceptor>emptyList());
    newTransports = TestUtils.captureTransports(mockTransportFactory);

    for (int i = 0; i < 2; i++) {
      ResolvedServerInfoGroup.Builder resolvedServerInfoGroup = ResolvedServerInfoGroup.builder();
      for (int j = 0; j < 2; j++) {
        resolvedServerInfoGroup.add(
            new ResolvedServerInfo(new FakeSocketAddress("servergroup" + i + "server" + j)));
      }
      servers.add(resolvedServerInfoGroup.build());
      addressGroupList.add(resolvedServerInfoGroup.build().toEquivalentAddressGroup());
    }
    verify(mockNameResolverFactory).newNameResolver(any(URI.class), any(Attributes.class));
    // Verify the initial idleness
    verify(mockLoadBalancerFactory, never()).newLoadBalancer(
        anyString(), Matchers.<TransportManager<ClientTransport>>any());
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
    final EquivalentAddressGroup addressGroup = addressGroupList.get(1);
    doAnswer(new Answer<ClientTransport>() {
        @Override
        public ClientTransport answer(InvocationOnMock invocation) throws Throwable {
          return channel.tm.getTransport(addressGroup);
        }
      }).when(mockLoadBalancer).pickTransport(any(Attributes.class));

    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, new Metadata());

    verify(mockLoadBalancerFactory).newLoadBalancer(anyString(), same(channel.tm));
    // NameResolver is started in the scheduled executor
    timer.runDueTasks();
    verify(mockNameResolver).start(nameResolverListenerCaptor.capture());

    // LoadBalancer is used right after created.
    verify(mockLoadBalancer).pickTransport(any(Attributes.class));
    verify(mockTransportFactory).newClientTransport(
        addressGroup.getAddresses().get(0), authority, userAgent);

    // Simulate new address resolved
    nameResolverListenerCaptor.getValue().onUpdate(servers, Attributes.EMPTY);
    verify(mockLoadBalancer).handleResolvedAddresses(servers, Attributes.EMPTY);
  }

  @Test
  public void enterIdleModeAfterForceExit() throws Exception {
    forceExitIdleMode();

    // Trigger the creation of TransportSets
    for (EquivalentAddressGroup addressGroup : addressGroupList) {
      channel.tm.getTransport(addressGroup);
      verify(mockTransportFactory).newClientTransport(
          addressGroup.getAddresses().get(0), authority, userAgent);
    }
    ArrayList<MockClientTransportInfo> transports = new ArrayList<MockClientTransportInfo>();
    newTransports.drainTo(transports);
    assertEquals(addressGroupList.size(), transports.size());

    InterimTransport<ClientTransport> interimTransport = channel.tm.createInterimTransport();

    // Without actually using these transports, will eventually enter idle mode
    walkIntoIdleMode(transports);
  }

  @Test
  public void interimTransportHoldsOffIdleness() throws Exception {
    final EquivalentAddressGroup addressGroup = addressGroupList.get(1);
    doAnswer(new Answer<ClientTransport>() {
        @Override
        public ClientTransport answer(InvocationOnMock invocation) throws Throwable {
          return channel.tm.createInterimTransport().transport();
        }
      }).when(mockLoadBalancer).pickTransport(any(Attributes.class));

    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, new Metadata());
    assertTrue(channel.inUseStateAggregator.isInUse());
    // NameResolver is started in the scheduled executor
    timer.runDueTasks();

    // As long as the interim transport is in-use (by the pending RPC), the channel won't go idle.
    timer.forwardTime(IDLE_TIMEOUT_SECONDS * 2, TimeUnit.SECONDS);
    assertTrue(channel.inUseStateAggregator.isInUse());

    // Cancelling the only RPC will reset the in-use state.
    assertEquals(0, executor.numPendingTasks());
    call.cancel("In test", null);
    assertEquals(1, executor.runDueTasks());
    assertFalse(channel.inUseStateAggregator.isInUse());
    // And allow the channel to go idle.
    walkIntoIdleMode(Collections.<MockClientTransportInfo>emptyList());
  }

  @Test
  public void realTransportsHoldsOffIdleness() throws Exception {
    final EquivalentAddressGroup addressGroup = addressGroupList.get(1);
    doAnswer(new Answer<ClientTransport>() {
        @Override
        public ClientTransport answer(InvocationOnMock invocation) throws Throwable {
          return channel.tm.getTransport(addressGroup);
        }
      }).when(mockLoadBalancer).pickTransport(any(Attributes.class));

    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, new Metadata());

    // A TransportSet is in-use, while the stream is pending in a delayed transport
    assertTrue(channel.inUseStateAggregator.isInUse());
    // NameResolver is started in the scheduled executor
    timer.runDueTasks();

    // Making the real transport ready, will release the delayed transport.
    // The TransportSet is *not* in-use before the real transport become in-use.
    MockClientTransportInfo t0 = newTransports.poll();
    assertEquals(0, executor.numPendingTasks());
    t0.listener.transportReady();
    // Real streams are started in the executor
    assertEquals(1, executor.runDueTasks());
    assertFalse(channel.inUseStateAggregator.isInUse());
    t0.listener.transportInUse(true);
    assertTrue(channel.inUseStateAggregator.isInUse());

    // As long as the transport is in-use, the channel won't go idle.
    timer.forwardTime(IDLE_TIMEOUT_SECONDS * 2, TimeUnit.SECONDS);

    t0.listener.transportInUse(false);
    assertFalse(channel.inUseStateAggregator.isInUse());
    // And allow the channel to go idle.
    walkIntoIdleMode(Arrays.asList(t0));
  }

  @Test
  public void idlenessDecommissionsTransports() throws Exception {
    EquivalentAddressGroup addressGroup = addressGroupList.get(0);
    forceExitIdleMode();

    channel.tm.getTransport(addressGroup);
    MockClientTransportInfo t0 = newTransports.poll();
    t0.listener.transportReady();
    assertSame(t0.transport, channelTmGetTransportUnwrapped(addressGroup));

    walkIntoIdleMode(Arrays.asList(t0));
    verify(t0.transport).shutdown();

    forceExitIdleMode();
    channel.tm.getTransport(addressGroup);
    MockClientTransportInfo t1 = newTransports.poll();
    t1.listener.transportReady();

    assertSame(t1.transport, channelTmGetTransportUnwrapped(addressGroup));
    assertNotSame(t0.transport, channelTmGetTransportUnwrapped(addressGroup));

    channel.shutdown();
    verify(t1.transport).shutdown();
    channel.shutdownNow();
    verify(t0.transport).shutdownNow(any(Status.class));
    verify(t1.transport).shutdownNow(any(Status.class));

    t1.listener.transportTerminated();
    assertFalse(channel.isTerminated());
    t0.listener.transportTerminated();
    assertTrue(channel.isTerminated());
  }

  @Test
  public void loadBalancerShouldNotCreateConnectionsWhenIdle() throws Exception {
    // Acts as a misbehaving LoadBalancer that tries to create connections when channel is in idle,
    // which means the LoadBalancer is supposedly shutdown.
    assertSame(ManagedChannelImpl.IDLE_MODE_TRANSPORT,
        channel.tm.getTransport(addressGroupList.get(0)));
    OobTransportProvider<ClientTransport> oobProvider =
        channel.tm.createOobTransportProvider(addressGroupList.get(0), "authority");
    assertSame(ManagedChannelImpl.IDLE_MODE_TRANSPORT, oobProvider.get());
    oobProvider.close();
    verify(mockTransportFactory, never()).newClientTransport(
        any(SocketAddress.class), anyString(), anyString());
    // We don't care for delayed (interim) transports, because they don't create connections.
  }

  private void walkIntoIdleMode(Collection<MockClientTransportInfo> currentTransports) {
    timer.forwardTime(IDLE_TIMEOUT_SECONDS - 1, TimeUnit.SECONDS);
    verify(mockLoadBalancer, never()).shutdown();
    verify(mockNameResolver, never()).shutdown();
    for (MockClientTransportInfo transport : currentTransports) {
      verify(transport.transport, never()).shutdown();
    }
    timer.forwardTime(1, TimeUnit.SECONDS);
    verify(mockLoadBalancer).shutdown();
    verify(mockNameResolver).shutdown();
    for (MockClientTransportInfo transport : currentTransports) {
      verify(transport.transport).shutdown();
    }
  }

  private void forceExitIdleMode() {
    channel.exitIdleMode();
    // NameResolver is started in the scheduled executor
    timer.runDueTasks();
  }

  private ClientTransport channelTmGetTransportUnwrapped(EquivalentAddressGroup addressGroup) {
    return ((ForwardingConnectionClientTransport) channel.tm.getTransport(addressGroup)).delegate();
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
