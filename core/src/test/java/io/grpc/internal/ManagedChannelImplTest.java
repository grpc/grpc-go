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
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.isA;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.spy;
import static org.mockito.Mockito.timeout;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.CompressorRegistry;
import io.grpc.DecompressorRegistry;
import io.grpc.IntegerMarshaller;
import io.grpc.LoadBalancer;
import io.grpc.ManagedChannel;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.NameResolver;
import io.grpc.ResolvedServerInfo;
import io.grpc.SimpleLoadBalancerFactory;
import io.grpc.Status;
import io.grpc.StringMarshaller;
import io.grpc.TransportManager;

import org.junit.After;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

import java.net.SocketAddress;
import java.net.URI;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.CyclicBarrier;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicLong;

/** Unit tests for {@link ManagedChannelImpl}. */
@RunWith(JUnit4.class)
public class ManagedChannelImplTest {
  private static final List<ClientInterceptor> NO_INTERCEPTOR =
      Collections.<ClientInterceptor>emptyList();
  private static final Attributes NAME_RESOLVER_PARAMS =
      Attributes.newBuilder().set(NameResolver.Factory.PARAMS_DEFAULT_PORT, 447).build();
  private final MethodDescriptor<String, Integer> method = MethodDescriptor.create(
      MethodDescriptor.MethodType.UNKNOWN, "/service/method",
      new StringMarshaller(), new IntegerMarshaller());
  private final ExecutorService executor = Executors.newSingleThreadExecutor();
  private final String serviceName = "fake.example.com";
  private final String authority = serviceName;
  private final String target = "fake://" + serviceName;
  private URI expectedUri;
  private final SocketAddress socketAddress = new SocketAddress() {};
  private final ResolvedServerInfo server = new ResolvedServerInfo(socketAddress, Attributes.EMPTY);
  private SpyingLoadBalancerFactory loadBalancerFactory =
      new SpyingLoadBalancerFactory(SimpleLoadBalancerFactory.getInstance());

  @Rule public final ExpectedException thrown = ExpectedException.none();

  @Mock
  private ClientTransport mockTransport;
  @Mock
  private ClientTransportFactory mockTransportFactory;
  @Mock
  private ClientCall.Listener<Integer> mockCallListener;
  @Mock
  private ClientCall.Listener<Integer> mockCallListener2;
  @Mock
  private ClientCall.Listener<Integer> mockCallListener3;

  private ArgumentCaptor<ClientTransport.Listener> transportListenerCaptor =
      ArgumentCaptor.forClass(ClientTransport.Listener.class);
  private ArgumentCaptor<ClientStreamListener> streamListenerCaptor =
      ArgumentCaptor.forClass(ClientStreamListener.class);

  private ManagedChannel createChannel(
      NameResolver.Factory nameResolverFactory, List<ClientInterceptor> interceptors) {
    return new ManagedChannelImpl(target, new FakeBackoffPolicyProvider(),
        nameResolverFactory, NAME_RESOLVER_PARAMS, loadBalancerFactory,
        mockTransportFactory, DecompressorRegistry.getDefaultInstance(),
        CompressorRegistry.getDefaultInstance(), executor, null, interceptors);
  }

  @Before
  public void setUp() throws Exception {
    MockitoAnnotations.initMocks(this);
    expectedUri = new URI(target);
    when(mockTransportFactory.newClientTransport(any(SocketAddress.class), any(String.class)))
        .thenReturn(mockTransport);
  }

  @After
  public void tearDown() {
    executor.shutdownNow();
  }

  @Test
  public void immediateDeadlineExceeded() {
    ManagedChannel channel = createChannel(
        new FakeNameResolverFactory(true), NO_INTERCEPTOR);
    ClientCall<String, Integer> call =
        channel.newCall(method, CallOptions.DEFAULT.withDeadlineNanoTime(System.nanoTime()));
    call.start(mockCallListener, new Metadata());
    verifyListenerClosed(mockCallListener, Status.Code.DEADLINE_EXCEEDED);
  }

  @Test
  public void shutdownWithNoTransportsEverCreated() {
    ManagedChannel channel = createChannel(
        new FakeNameResolverFactory(true), NO_INTERCEPTOR);
    verifyNoMoreInteractions(mockTransportFactory);
    channel.shutdown();
    assertTrue(channel.isShutdown());
    assertTrue(channel.isTerminated());
  }

  @Test
  public void twoCallsAndGracefulShutdown() {
    FakeNameResolverFactory nameResolverFactory = new FakeNameResolverFactory(true);
    ManagedChannel channel = createChannel(nameResolverFactory, NO_INTERCEPTOR);
    verifyNoMoreInteractions(mockTransportFactory);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    verifyNoMoreInteractions(mockTransportFactory);

    // Create transport and call
    ClientTransport mockTransport = mock(ClientTransport.class);
    ClientStream mockStream = mock(ClientStream.class);
    Metadata headers = new Metadata();
    when(mockTransportFactory.newClientTransport(any(SocketAddress.class), any(String.class)))
        .thenReturn(mockTransport);
    when(mockTransport.newStream(same(method), same(headers))).thenReturn(mockStream);
    call.start(mockCallListener, headers);
    verify(mockTransportFactory, timeout(1000))
        .newClientTransport(same(socketAddress), eq(authority));
    verify(mockTransport, timeout(1000)).start(transportListenerCaptor.capture());
    ClientTransport.Listener transportListener = transportListenerCaptor.getValue();
    verify(mockTransport, timeout(1000)).newStream(same(method), same(headers));
    verify(mockStream).start(streamListenerCaptor.capture());
    verify(mockStream).setDecompressionRegistry(isA(DecompressorRegistry.class));
    verify(mockStream).setCompressionRegistry(isA(CompressorRegistry.class));
    ClientStreamListener streamListener = streamListenerCaptor.getValue();

    // Second call
    ClientCall<String, Integer> call2 = channel.newCall(method, CallOptions.DEFAULT);
    ClientStream mockStream2 = mock(ClientStream.class);
    Metadata headers2 = new Metadata();
    when(mockTransport.newStream(same(method), same(headers2))).thenReturn(mockStream2);
    call2.start(mockCallListener2, headers2);
    verify(mockTransport, timeout(1000)).newStream(same(method), same(headers2));
    verify(mockStream2).start(streamListenerCaptor.capture());
    ClientStreamListener streamListener2 = streamListenerCaptor.getValue();
    Metadata trailers = new Metadata();
    streamListener2.closed(Status.CANCELLED, trailers);
    verifyListenerClosed(mockCallListener2, Status.Code.CANCELLED);

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
    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(mockCallListener3, timeout(1000))
        .onClose(statusCaptor.capture(), any(Metadata.class));
    assertSame(Status.Code.UNAVAILABLE, statusCaptor.getValue().getCode());

    // Finish shutdown
    transportListener.transportShutdown(Status.CANCELLED);
    assertFalse(channel.isTerminated());
    streamListener.closed(Status.CANCELLED, trailers);
    verifyListenerClosed(mockCallListener, Status.Code.CANCELLED);
    assertFalse(channel.isTerminated());

    transportListener.transportTerminated();
    assertTrue(channel.isTerminated());

    verify(mockTransportFactory).release();
    verifyNoMoreInteractions(mockTransportFactory);
    verifyNoMoreInteractions(mockTransport);
    verifyNoMoreInteractions(mockStream);
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
    ManagedChannel channel = createChannel(
        new FakeNameResolverFactory(true), Arrays.asList(interceptor));
    assertNotNull(channel.newCall(method, CallOptions.DEFAULT));
    assertEquals(1, atomic.get());
  }

  @Test
  public void testNoDeadlockOnShutdown() {
    ManagedChannel channel = createChannel(
        new FakeNameResolverFactory(true), NO_INTERCEPTOR);
    // Force creation of transport
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    Metadata headers = new Metadata();
    ClientStream mockStream = mock(ClientStream.class);
    when(mockTransport.newStream(same(method), same(headers))).thenReturn(mockStream);
    call.start(mockCallListener, headers);
    call.cancel();

    verify(mockTransport, timeout(1000)).start(transportListenerCaptor.capture());
    final ClientTransport.Listener transportListener = transportListenerCaptor.getValue();
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
  public void nameResolutionFailed() {
    Status error = Status.UNAVAILABLE.withCause(new Throwable("fake name resolution error"));
    ManagedChannel channel = createChannel(new FailingNameResolverFactory(error), NO_INTERCEPTOR);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, new Metadata());
    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(mockCallListener, timeout(1000)).onClose(statusCaptor.capture(), any(Metadata.class));
    Status status = statusCaptor.getValue();
    assertSame(error.getCode(), status.getCode());
    assertSame(error.getCause(), status.getCause());
  }

  @Test
  public void nameResolvedAfterChannelShutdown() {
    FakeNameResolverFactory nameResolverFactory =
        new FakeNameResolverFactory(false);
    ManagedChannel channel = createChannel(nameResolverFactory, NO_INTERCEPTOR);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    Metadata headers = new Metadata();
    call.start(mockCallListener, headers);
    channel.shutdown();
    assertTrue(channel.isShutdown());
    assertTrue(channel.isTerminated());
    // Name resolved after the channel is shut down, which is possible if the name resolution takes
    // time and is not cancellable. The resolved address will still be passed to the LoadBalancer.
    nameResolverFactory.allResolved();
    verify(mockTransportFactory, never())
        .newClientTransport(any(SocketAddress.class), any(String.class));
  }

  /**
   * Verify that if one resolved address points to a bad server, the retry will use another address.
   */
  @Test
  public void firstResolvedServerIsBad() throws Exception {
    final SocketAddress goodAddress = new SocketAddress() {};
    final SocketAddress badAddress = new SocketAddress() {};
    final ResolvedServerInfo goodServer = new ResolvedServerInfo(goodAddress, Attributes.EMPTY);
    final ResolvedServerInfo badServer = new ResolvedServerInfo(badAddress, Attributes.EMPTY);
    final ClientTransport goodTransport = mock(ClientTransport.class);
    final ClientTransport badTransport = mock(ClientTransport.class);
    when(mockTransportFactory.newClientTransport(same(goodAddress), any(String.class)))
        .thenReturn(goodTransport);
    when(mockTransportFactory.newClientTransport(same(badAddress), any(String.class)))
        .thenReturn(badTransport);

    FakeNameResolverFactory nameResolverFactory =
        new FakeNameResolverFactory(Arrays.asList(badServer, goodServer));
    ManagedChannel channel = createChannel(nameResolverFactory, NO_INTERCEPTOR);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    Metadata headers = new Metadata();
    ClientStream badStream = mock(ClientStream.class);
    when(badTransport.newStream(same(method), same(headers))).thenReturn(badStream);
    doAnswer(new Answer<ClientStream>() {
      @Override
      public ClientStream answer(InvocationOnMock invocation) throws Throwable {
        Object[] args = invocation.getArguments();
        final ClientStreamListener listener = (ClientStreamListener) args[0];
        listener.closed(Status.UNAVAILABLE, new Metadata());
        return mock(ClientStream.class);
      }
    }).when(badStream).start(any(ClientStreamListener.class));
    when(goodTransport.newStream(same(method), same(headers))).thenReturn(mock(ClientStream.class));

    // First try should fail with the bad address.
    call.start(mockCallListener, headers);
    ArgumentCaptor<ClientTransport.Listener> badTransportListenerCaptor =
        ArgumentCaptor.forClass(ClientTransport.Listener.class);
    verifyListenerClosed(mockCallListener, Status.Code.UNAVAILABLE);
    verify(badTransport, timeout(1000)).start(badTransportListenerCaptor.capture());
    badTransportListenerCaptor.getValue().transportShutdown(Status.UNAVAILABLE);

    // Retry should work with the good address.
    ClientCall<String, Integer> call2 = channel.newCall(method, CallOptions.DEFAULT);
    call2.start(mockCallListener, headers);
    verify(goodTransport, timeout(1000)).newStream(same(method), same(headers));
  }

  private void verifyListenerClosed(ClientCall.Listener<?> mockCallListener,
      Status.Code statusCode) {
    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(mockCallListener, timeout(1000)).onClose(statusCaptor.capture(), any(Metadata.class));
    Status status = statusCaptor.getValue();
    assertEquals(statusCode, status.getCode());
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
    final List<ResolvedServerInfo> servers;
    final boolean resolvedAtStart;
    final ArrayList<FakeNameResolver> resolvers = new ArrayList<FakeNameResolver>();

    FakeNameResolverFactory(boolean resolvedAtStart) {
      this.resolvedAtStart = resolvedAtStart;
      servers = Collections.singletonList(server);
    }

    FakeNameResolverFactory(List<ResolvedServerInfo> servers) {
      resolvedAtStart = true;
      this.servers = servers;
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

  private class FailingNameResolverFactory extends NameResolver.Factory {
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

  private class SpyingLoadBalancerFactory extends LoadBalancer.Factory {
    private final LoadBalancer.Factory delegate;
    private final List<LoadBalancer> balancers = new ArrayList<LoadBalancer>();

    private SpyingLoadBalancerFactory(LoadBalancer.Factory delegate) {
      this.delegate = delegate;
    }

    @Override
    public LoadBalancer newLoadBalancer(String serviceName, TransportManager tm) {
      LoadBalancer lb = spy(delegate.newLoadBalancer(serviceName, tm));
      balancers.add(lb);
      return lb;
    }
  }
}
