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
import static org.junit.Assert.assertNotSame;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyString;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.mock;
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
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

import java.net.SocketAddress;
import java.net.URI;
import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedList;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/**
 * Unit tests for {@link ManagedChannelImpl}'s {@link TransportManager} implementation as well as
 * {@link TransportSet}.
 */
@RunWith(JUnit4.class)
public class ManagedChannelImplTransportManagerTest {

  private static final String authority = "fakeauthority";
  private static final NameResolver.Factory nameResolverFactory = new NameResolver.Factory() {
    @Override
    public NameResolver newNameResolver(final URI targetUri, Attributes params) {
      return new NameResolver() {
        @Override public void start(final Listener listener) {
        }

        @Override public String getServiceAuthority() {
          return authority;
        }

        @Override public void shutdown() {
        }
      };
    }

    @Override
    public String getDefaultScheme() {
      return "fake";
    }
  };

  private final ExecutorService executor = Executors.newSingleThreadExecutor();

  private ManagedChannelImpl channel;

  @Mock private ClientTransportFactory mockTransportFactory;
  @Mock private LoadBalancer.Factory mockLoadBalancerFactory;
  @Mock private BackoffPolicy.Provider mockBackoffPolicyProvider;
  @Mock private BackoffPolicy mockBackoffPolicy;

  private TransportManager<ClientTransport> tm;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);

    when(mockBackoffPolicyProvider.get()).thenReturn(mockBackoffPolicy);
    @SuppressWarnings("unchecked")
    LoadBalancer<ClientTransport> loadBalancer = mock(LoadBalancer.class);
    when(mockLoadBalancerFactory
        .newLoadBalancer(anyString(), Matchers.<TransportManager<ClientTransport>>any()))
        .thenReturn(loadBalancer);

    channel = new ManagedChannelImpl("fake://target", mockBackoffPolicyProvider,
        nameResolverFactory, Attributes.EMPTY, mockLoadBalancerFactory,
        mockTransportFactory, DecompressorRegistry.getDefaultInstance(),
        CompressorRegistry.getDefaultInstance(), executor, null,
        Collections.<ClientInterceptor>emptyList());

    ArgumentCaptor<TransportManager<ClientTransport>> tmCaptor
        = ArgumentCaptor.forClass(null);
    verify(mockLoadBalancerFactory).newLoadBalancer(anyString(), tmCaptor.capture());
    tm = tmCaptor.getValue();
  }

  @After
  public void tearDown() {
    channel.shutdown();
    executor.shutdown();
  }

  @Test
  public void createAndReuseTransport() throws Exception {
    doAnswer(new Answer<ManagedClientTransport>() {
      @Override
      public ManagedClientTransport answer(InvocationOnMock invocation) throws Throwable {
        return mock(ManagedClientTransport.class);
      }
    }).when(mockTransportFactory).newClientTransport(any(SocketAddress.class), any(String.class));

    SocketAddress addr = mock(SocketAddress.class);
    EquivalentAddressGroup addressGroup = new EquivalentAddressGroup(addr);
    ClientTransport t1 = tm.getTransport(addressGroup);
    verify(mockTransportFactory).newClientTransport(addr, authority);
    ClientTransport t2 = tm.getTransport(addressGroup);
    assertSame(t1, t2);
    verify(mockBackoffPolicyProvider).get();
    verify(mockBackoffPolicy, times(0)).nextBackoffMillis();
    verifyNoMoreInteractions(mockTransportFactory);
  }

  @Test
  public void reconnect() throws Exception {
    SocketAddress addr1 = mock(SocketAddress.class);
    SocketAddress addr2 = mock(SocketAddress.class);
    EquivalentAddressGroup addressGroup = new EquivalentAddressGroup(Arrays.asList(addr1, addr2));

    LinkedList<MockClientTransportInfo> transports =
        TestUtils.captureTransports(mockTransportFactory);

    // Invocation counters
    int backoffReset = 0;

    // Pick the first transport
    ClientTransport t1 = tm.getTransport(addressGroup);
    assertNotNull(t1);
    verify(mockTransportFactory).newClientTransport(addr1, authority);
    verify(mockBackoffPolicyProvider, times(++backoffReset)).get();
    // Fail the first transport, without setting it to ready
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);

    // Subsequent getTransport() will use the next address
    ClientTransport t2a = tm.getTransport(addressGroup);
    assertNotNull(t2a);
    // Will keep the previous back-off policy, and not consult back-off policy
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    verify(mockTransportFactory).newClientTransport(addr2, authority);
    ClientTransport t2b = tm.getTransport(addressGroup);
    assertSame(t2a, t2b);
    assertNotSame(t1, t2a);
    // Make the second transport ready
    transports.peek().listener.transportReady();
    // Disconnect the second transport
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);

    // Subsequent getTransport() will use the next address, which is the first one since we have run
    // out of addresses.
    ClientTransport t3 = tm.getTransport(addressGroup);
    assertNotSame(t1, t3);
    assertNotSame(t2a, t3);
    // This time back-off policy was reset, because previous transport was succesfully connected.
    verify(mockBackoffPolicyProvider, times(++backoffReset)).get();
    // Back-off policy was never consulted.
    verify(mockBackoffPolicy, times(0)).nextBackoffMillis();
    verify(mockTransportFactory, times(2)).newClientTransport(addr1, authority);
    verifyNoMoreInteractions(mockTransportFactory);
    assertEquals(1, transports.size());
  }

  @Test
  public void reconnectWithBackoff() throws Exception {
    SocketAddress addr1 = mock(SocketAddress.class);
    SocketAddress addr2 = mock(SocketAddress.class);
    EquivalentAddressGroup addressGroup = new EquivalentAddressGroup(Arrays.asList(addr1, addr2));

    LinkedList<MockClientTransportInfo> transports =
        TestUtils.captureTransports(mockTransportFactory);

    // Invocation counters
    int transportsAddr1 = 0;
    int transportsAddr2 = 0;
    int backoffConsulted = 0;
    int backoffReset = 0;

    // First pick succeeds
    ClientTransport t1 = tm.getTransport(addressGroup);
    assertNotNull(t1);
    verify(mockTransportFactory, times(++transportsAddr1)).newClientTransport(addr1, authority);
    // Back-off policy was set initially.
    verify(mockBackoffPolicyProvider, times(++backoffReset)).get();
    transports.peek().listener.transportReady();
    // Then close it
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);

    // Second pick fails. This is the beginning of a series of failures.
    ClientTransport t2 = tm.getTransport(addressGroup);
    assertNotNull(t2);
    verify(mockTransportFactory, times(++transportsAddr1)).newClientTransport(addr1, authority);
    // Back-off policy was reset.
    verify(mockBackoffPolicyProvider, times(++backoffReset)).get();
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);

    // Third pick fails too
    ClientTransport t3 = tm.getTransport(addressGroup);
    assertNotNull(t3);
    verify(mockTransportFactory, times(++transportsAddr2)).newClientTransport(addr2, authority);
    // Back-off policy was not reset.
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);

    // Forth pick is on the first address, back-off policy kicks in.
    ClientTransport t4 = tm.getTransport(addressGroup);
    assertNotNull(t4);
    verify(mockTransportFactory, times(++transportsAddr1)).newClientTransport(addr1, authority);
    // Back-off policy was not reset, but was consulted.
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    verify(mockBackoffPolicy, times(++backoffConsulted)).nextBackoffMillis();
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
    MethodDescriptor<String, String> method = MethodDescriptor.create(
        MethodDescriptor.MethodType.UNKNOWN, "/service/method",
        new StringMarshaller(), new StringMarshaller());
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
