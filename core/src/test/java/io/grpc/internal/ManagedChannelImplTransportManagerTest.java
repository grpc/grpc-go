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

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyString;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.timeout;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import io.grpc.Attributes;
import io.grpc.ClientInterceptor;
import io.grpc.CompressorRegistry;
import io.grpc.DecompressorRegistry;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.NameResolver;
import io.grpc.Status;
import io.grpc.StringMarshaller;
import io.grpc.TransportManager.InterimTransport;
import io.grpc.TransportManager;
import io.grpc.internal.TestUtils.MockClientTransportInfo;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Matchers;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.net.SocketAddress;
import java.net.URI;
import java.util.Arrays;
import java.util.Collections;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;

/**
 * Unit tests for {@link ManagedChannelImpl}'s {@link TransportManager} implementation as well as
 * {@link TransportSet}.
 */
@RunWith(JUnit4.class)
public class ManagedChannelImplTransportManagerTest {

  private static final String authority = "fakeauthority";

  private final ExecutorService executor = Executors.newSingleThreadExecutor();
  private final MethodDescriptor<String, String> method = MethodDescriptor.create(
      MethodDescriptor.MethodType.UNKNOWN, "/service/method",
      new StringMarshaller(), new StringMarshaller());
  private final MethodDescriptor<String, String> method2 = MethodDescriptor.create(
      MethodDescriptor.MethodType.UNKNOWN, "/service/method2",
      new StringMarshaller(), new StringMarshaller());

  private ManagedChannelImpl channel;

  @Mock private ClientTransportFactory mockTransportFactory;
  @Mock private LoadBalancer.Factory mockLoadBalancerFactory;
  @Mock private NameResolver mockNameResolver;
  @Mock private NameResolver.Factory mockNameResolverFactory;
  @Mock private BackoffPolicy.Provider mockBackoffPolicyProvider;
  @Mock private BackoffPolicy mockBackoffPolicy;

  private BlockingQueue<MockClientTransportInfo> transports;

  private TransportManager<ClientTransport> tm;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);

    when(mockBackoffPolicyProvider.get()).thenReturn(mockBackoffPolicy);
    when(mockNameResolver.getServiceAuthority()).thenReturn(authority);
    when(mockNameResolverFactory
        .newNameResolver(any(URI.class), any(Attributes.class)))
        .thenReturn(mockNameResolver);
    @SuppressWarnings("unchecked")
    LoadBalancer<ClientTransport> loadBalancer = mock(LoadBalancer.class);
    when(mockLoadBalancerFactory
        .newLoadBalancer(anyString(), Matchers.<TransportManager<ClientTransport>>any()))
        .thenReturn(loadBalancer);

    channel = new ManagedChannelImpl("fake://target", mockBackoffPolicyProvider,
        mockNameResolverFactory, Attributes.EMPTY, mockLoadBalancerFactory,
        mockTransportFactory, DecompressorRegistry.getDefaultInstance(),
        CompressorRegistry.getDefaultInstance(), executor, null,
        Collections.<ClientInterceptor>emptyList());

    ArgumentCaptor<TransportManager<ClientTransport>> tmCaptor
        = ArgumentCaptor.forClass(null);
    verify(mockNameResolverFactory).newNameResolver(any(URI.class), any(Attributes.class));
    verify(mockNameResolver).start(any(NameResolver.Listener.class));
    verify(mockLoadBalancerFactory).newLoadBalancer(anyString(), tmCaptor.capture());
    tm = tmCaptor.getValue();
    transports = TestUtils.captureTransports(mockTransportFactory);
  }

  @After
  public void tearDown() {
    channel.shutdown();
    executor.shutdown();
  }

  @Test
  public void createAndReuseTransport() throws Exception {
    SocketAddress addr = mock(SocketAddress.class);
    EquivalentAddressGroup addressGroup = new EquivalentAddressGroup(addr);
    ClientTransport t1 = tm.getTransport(addressGroup);
    verify(mockTransportFactory, timeout(1000)).newClientTransport(addr, authority);
    // The real transport
    MockClientTransportInfo transportInfo = transports.poll(1, TimeUnit.SECONDS);
    transportInfo.listener.transportReady();
    ClientTransport t2 = tm.getTransport(addressGroup);
    assertTrue(t1 instanceof DelayedClientTransport);
    assertFalse(t2 instanceof DelayedClientTransport);
    assertSame(transportInfo.transport, t2);
    verify(mockBackoffPolicyProvider).get();
    verify(mockBackoffPolicy, times(0)).nextBackoffMillis();
    verifyNoMoreInteractions(mockTransportFactory);
  }

  @Test
  public void reconnect() throws Exception {
    SocketAddress addr1 = mock(SocketAddress.class);
    SocketAddress addr2 = mock(SocketAddress.class);
    EquivalentAddressGroup addressGroup = new EquivalentAddressGroup(Arrays.asList(addr1, addr2));

    // Invocation counters
    int backoffReset = 0;

    // Pick the first transport
    ClientTransport t1 = tm.getTransport(addressGroup);
    assertNotNull(t1);
    verify(mockTransportFactory, timeout(1000)).newClientTransport(addr1, authority);
    verify(mockBackoffPolicyProvider, times(++backoffReset)).get();
    // Fail the first transport, without setting it to ready
    MockClientTransportInfo transportInfo = transports.poll(1, TimeUnit.SECONDS);
    ClientTransport rt1 = transportInfo.transport;
    transportInfo.listener.transportShutdown(Status.UNAVAILABLE);

    // Subsequent getTransport() will use the next address
    ClientTransport t2 = tm.getTransport(addressGroup);
    assertNotNull(t2);
    t2.newStream(method, new Metadata());
    // Will keep the previous back-off policy, and not consult back-off policy
    verify(mockTransportFactory, timeout(1000)).newClientTransport(addr2, authority);
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    transportInfo = transports.poll(1, TimeUnit.SECONDS);
    ClientTransport rt2 = transportInfo.transport;
    // Make the second transport ready
    transportInfo.listener.transportReady();
    verify(rt2, timeout(1000)).newStream(same(method), any(Metadata.class));
    // Disconnect the second transport
    transportInfo.listener.transportShutdown(Status.UNAVAILABLE);

    // Subsequent getTransport() will use the next address, which is the first one since we have run
    // out of addresses.
    ClientTransport t3 = tm.getTransport(addressGroup);
    t3.newStream(method2, new Metadata());
    verify(mockTransportFactory, timeout(1000).times(2)).newClientTransport(addr1, authority);
    // This time back-off policy was reset, because previous transport was succesfully connected.
    verify(mockBackoffPolicyProvider, times(++backoffReset)).get();
    transportInfo = transports.poll(1, TimeUnit.SECONDS);
    ClientTransport rt3 = transportInfo.transport;
    transportInfo.listener.transportReady();
    verify(rt3, timeout(1000)).newStream(same(method2), any(Metadata.class));

    verify(rt1, times(0)).newStream(any(MethodDescriptor.class), any(Metadata.class));
    // Back-off policy was never consulted.
    verify(mockBackoffPolicy, times(0)).nextBackoffMillis();
    verifyNoMoreInteractions(mockTransportFactory);
    // Never refreshed NameResolver
    verify(mockNameResolver, times(0)).refresh();
  }

  @Test
  public void reconnectWithBackoff() throws Exception {
    SocketAddress addr1 = mock(SocketAddress.class);
    SocketAddress addr2 = mock(SocketAddress.class);
    EquivalentAddressGroup addressGroup = new EquivalentAddressGroup(Arrays.asList(addr1, addr2));

    // Invocation counters
    int transportsAddr1 = 0;
    int transportsAddr2 = 0;
    int backoffConsulted = 0;
    int backoffReset = 0;
    int nameResolverRefresh = 0;

    // First pick succeeds
    ClientTransport t1 = tm.getTransport(addressGroup);
    assertNotNull(t1);
    verify(mockTransportFactory, timeout(1000).times(++transportsAddr1))
        .newClientTransport(addr1, authority);
    // Back-off policy was set initially.
    verify(mockBackoffPolicyProvider, times(++backoffReset)).get();
    MockClientTransportInfo transportInfo = transports.poll(1, TimeUnit.SECONDS);
    transportInfo.listener.transportReady();
    // Then close it
    transportInfo.listener.transportShutdown(Status.UNAVAILABLE);
    verify(mockNameResolver, times(nameResolverRefresh)).refresh();

    // Second pick fails. This is the beginning of a series of failures.
    ClientTransport t2 = tm.getTransport(addressGroup);
    assertNotNull(t2);
    verify(mockTransportFactory, timeout(1000).times(++transportsAddr1))
        .newClientTransport(addr1, authority);
    // Back-off policy was reset.
    verify(mockBackoffPolicyProvider, times(++backoffReset)).get();
    transports.poll(1, TimeUnit.SECONDS).listener.transportShutdown(Status.UNAVAILABLE);
    verify(mockNameResolver, times(nameResolverRefresh)).refresh();

    // Third pick fails too
    ClientTransport t3 = tm.getTransport(addressGroup);
    assertNotNull(t3);
    verify(mockTransportFactory, timeout(1000).times(++transportsAddr2))
        .newClientTransport(addr2, authority);
    // Back-off policy was not reset.
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    transports.poll(1, TimeUnit.SECONDS).listener.transportShutdown(Status.UNAVAILABLE);
    verify(mockNameResolver, times(++nameResolverRefresh)).refresh();

    // Forth pick is on the first address, back-off policy kicks in.
    ClientTransport t4 = tm.getTransport(addressGroup);
    assertNotNull(t4);
    verify(mockTransportFactory, timeout(1000).times(++transportsAddr1))
        .newClientTransport(addr1, authority);
    // Back-off policy was not reset, but was consulted.
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    verify(mockBackoffPolicy, times(++backoffConsulted)).nextBackoffMillis();
    verify(mockNameResolver, times(nameResolverRefresh)).refresh();
  }

  @Test
  public void createFailingTransport() {
    Status error = Status.UNAVAILABLE.augmentDescription("simulated");
    FailingClientTransport transport = (FailingClientTransport) tm.createFailingTransport(error);
    assertSame(error, transport.error);
  }

  @Test
  public void createInterimTransport() {
    InterimTransport<ClientTransport> interimTransport = tm.createInterimTransport();
    ClientTransport transport = interimTransport.transport();
    assertTrue(transport instanceof DelayedClientTransport);
    ClientStream s1 = transport.newStream(method, new Metadata());
    ClientStreamListener sl1 = mock(ClientStreamListener.class);
    s1.start(sl1);

    // Shutting down the channel will shutdown the interim transport, thus refusing further streams,
    // but will continue existing streams.
    channel.shutdown();
    ClientStream s2 = transport.newStream(method, new Metadata());
    ClientStreamListener sl2 = mock(ClientStreamListener.class);
    s2.start(sl2);
    verify(sl2).closed(any(Status.class), any(Metadata.class));
    verify(sl1, times(0)).closed(any(Status.class), any(Metadata.class));
    assertFalse(channel.isTerminated());

    // After channel has shut down, createInterimTransport() will get you a transport that has
    // already set error.
    ClientTransport transportAfterShutdown = tm.createInterimTransport().transport();
    ClientStream s3 = transportAfterShutdown.newStream(method, new Metadata());
    ClientStreamListener sl3 = mock(ClientStreamListener.class);
    s3.start(sl3);
    verify(sl3).closed(any(Status.class), any(Metadata.class));

    // Closing the interim transport with error will terminate the interim transport, which in turn
    // allows channel to terminate.
    interimTransport.closeWithError(Status.UNAVAILABLE);
    verify(sl1).closed(same(Status.UNAVAILABLE), any(Metadata.class));
    assertTrue(channel.isTerminated());
  }
}
