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
import static org.mockito.Matchers.anyBoolean;
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
import static org.mockito.Mockito.timeout;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.Compressor;
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
import java.util.Collections;
import java.util.List;
import java.util.concurrent.CyclicBarrier;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
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
  private final String userAgent = "userAgent";
  private final String target = "fake://" + serviceName;
  private URI expectedUri;
  private final SocketAddress socketAddress = new SocketAddress() {};
  private final ResolvedServerInfo server = new ResolvedServerInfo(socketAddress, Attributes.EMPTY);
  private SpyingLoadBalancerFactory loadBalancerFactory =
      new SpyingLoadBalancerFactory(SimpleLoadBalancerFactory.getInstance());

  @Rule public final ExpectedException thrown = ExpectedException.none();

  @Captor
  private ArgumentCaptor<Status> statusCaptor;
  @Mock
  private ManagedClientTransport mockTransport;
  @Mock
  private ClientTransportFactory mockTransportFactory;
  @Mock
  private ClientCall.Listener<Integer> mockCallListener;
  @Mock
  private ClientCall.Listener<Integer> mockCallListener2;
  @Mock
  private ClientCall.Listener<Integer> mockCallListener3;

  private ArgumentCaptor<ManagedClientTransport.Listener> transportListenerCaptor =
      ArgumentCaptor.forClass(ManagedClientTransport.Listener.class);
  private ArgumentCaptor<ClientStreamListener> streamListenerCaptor =
      ArgumentCaptor.forClass(ClientStreamListener.class);

  private ManagedChannel createChannel(
      NameResolver.Factory nameResolverFactory, List<ClientInterceptor> interceptors) {
    return new ManagedChannelImpl(target, new FakeBackoffPolicyProvider(),
        nameResolverFactory, NAME_RESOLVER_PARAMS, loadBalancerFactory,
        mockTransportFactory, DecompressorRegistry.getDefaultInstance(),
        CompressorRegistry.getDefaultInstance(), executor, userAgent, interceptors);
  }

  @Before
  public void setUp() throws Exception {
    MockitoAnnotations.initMocks(this);
    expectedUri = new URI(target);
    when(mockTransportFactory.newClientTransport(
            any(SocketAddress.class), any(String.class), any(String.class)))
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
        channel.newCall(method, CallOptions.DEFAULT.withDeadlineAfter(0, TimeUnit.NANOSECONDS));
    call.start(mockCallListener, new Metadata());
    verify(mockCallListener, timeout(1000)).onClose(statusCaptor.capture(), any(Metadata.class));
    Status status = statusCaptor.getValue();
    assertSame(Status.DEADLINE_EXCEEDED.getCode(), status.getCode());
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
    ClientStream mockStream = mock(ClientStream.class);
    Metadata headers = new Metadata();
    when(mockTransportFactory.newClientTransport(
            any(SocketAddress.class), any(String.class), any(String.class)))
        .thenReturn(mockTransport);
    when(mockTransport.newStream(same(method), same(headers))).thenReturn(mockStream);
    call.start(mockCallListener, headers);
    verify(mockTransportFactory, timeout(1000))
        .newClientTransport(same(socketAddress), eq(authority), eq(userAgent));
    verify(mockTransport, timeout(1000)).start(transportListenerCaptor.capture());
    ManagedClientTransport.Listener transportListener = transportListenerCaptor.getValue();
    transportListener.transportReady();
    verify(mockTransport, timeout(1000)).newStream(same(method), same(headers));
    verify(mockStream, timeout(1000)).start(streamListenerCaptor.capture());
    verify(mockStream).setCompressor(isA(Compressor.class));
    // Depends on how quick the real transport is created, ClientCallImpl may start on mockStream
    // directly, or on a DelayedStream which later starts mockStream. In the former case,
    // setMessageCompression() is not called. In the latter case, it is (in
    // DelayedStream.startStream()).
    verify(mockStream, atMost(1)).setMessageCompression(anyBoolean());
    ClientStreamListener streamListener = streamListenerCaptor.getValue();

    // Second call
    ClientCall<String, Integer> call2 = channel.newCall(method, CallOptions.DEFAULT);
    ClientStream mockStream2 = mock(ClientStream.class);
    Metadata headers2 = new Metadata();
    when(mockTransport.newStream(same(method), same(headers2))).thenReturn(mockStream2);
    call2.start(mockCallListener2, headers2);
    verify(mockTransport, timeout(1000)).newStream(same(method), same(headers2));
    verify(mockStream2, timeout(1000)).start(streamListenerCaptor.capture());
    ClientStreamListener streamListener2 = streamListenerCaptor.getValue();
    Metadata trailers = new Metadata();
    streamListener2.closed(Status.CANCELLED, trailers);
    verify(mockCallListener2, timeout(1000)).onClose(Status.CANCELLED, trailers);

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
    verify(mockCallListener3, timeout(1000))
        .onClose(statusCaptor.capture(), any(Metadata.class));
    assertSame(Status.Code.UNAVAILABLE, statusCaptor.getValue().getCode());

    // Finish shutdown
    transportListener.transportShutdown(Status.CANCELLED);
    assertFalse(channel.isTerminated());
    streamListener.closed(Status.CANCELLED, trailers);
    verify(mockCallListener, timeout(1000)).onClose(Status.CANCELLED, trailers);
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
    call.cancel("Cancel for test", null);

    verify(mockTransport, timeout(1000)).start(transportListenerCaptor.capture());
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
    when(mockTransport.newStream(same(method), same(headers))).thenReturn(mockStream);
    FakeClock fakeExecutor = new FakeClock();
    ManagedChannel channel = createChannel(
        new FakeNameResolverFactory(true), NO_INTERCEPTOR);

    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT.withExecutor(
        fakeExecutor.scheduledExecutorService));
    call.start(mockCallListener, headers);

    verify(mockTransport, timeout(1000)).start(transportListenerCaptor.capture());
    transportListenerCaptor.getValue().transportReady();
    verify(mockTransport, timeout(1000)).newStream(same(method), same(headers));
    verify(mockStream, timeout(1000)).start(streamListenerCaptor.capture());
    ClientStreamListener streamListener = streamListenerCaptor.getValue();
    Metadata trailers = new Metadata();
    assertEquals(0, fakeExecutor.numPendingTasks());
    streamListener.closed(Status.CANCELLED, trailers);
    verify(mockCallListener, never()).onClose(same(Status.CANCELLED), same(trailers));
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockCallListener).onClose(same(Status.CANCELLED), same(trailers));
  }

  @Test
  public void nameResolutionFailed() {
    Status error = Status.UNAVAILABLE.withCause(new Throwable("fake name resolution error"));

    // Name resolution is started as soon as channel is created.
    ManagedChannel channel = createChannel(new FailingNameResolverFactory(error), NO_INTERCEPTOR);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, new Metadata());

    // The call failed with the name resolution error
    verify(mockCallListener, timeout(1000)).onClose(statusCaptor.capture(), any(Metadata.class));
    Status status = statusCaptor.getValue();
    assertSame(error.getCode(), status.getCode());
    assertSame(error.getCause(), status.getCause());
    // LoadBalancer received the same error
    assertEquals(1, loadBalancerFactory.balancers.size());
    verify(loadBalancerFactory.balancers.get(0)).handleNameResolutionError(same(error));
  }

  @Test
  public void nameResolverReturnsEmptyList() {
    String errorDescription = "NameResolver returned an empty list";

    // Name resolution is started as soon as channel is created
    ManagedChannel channel = createChannel(
        new FakeNameResolverFactory(new ArrayList<ResolvedServerInfo>()), NO_INTERCEPTOR);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, new Metadata());

    // The call failed with the name resolution error
    verify(mockCallListener, timeout(1000)).onClose(statusCaptor.capture(), any(Metadata.class));
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
    ManagedChannel channel = createChannel(nameResolverFactory, NO_INTERCEPTOR);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    call.start(mockCallListener, new Metadata());
    assertEquals(1, loadBalancerFactory.balancers.size());
    LoadBalancer<?> loadBalancer = loadBalancerFactory.balancers.get(0);
    doThrow(ex).when(loadBalancer).handleResolvedAddresses(
        Matchers.<List<ResolvedServerInfo>>anyObject(), any(Attributes.class));

    // NameResolver returns addresses.
    nameResolverFactory.allResolved();

    // The call failed with the error thrown from handleResolvedAddresses()
    verify(mockCallListener, timeout(1000)).onClose(statusCaptor.capture(), any(Metadata.class));
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
    ManagedChannel channel = createChannel(nameResolverFactory, NO_INTERCEPTOR);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    Metadata headers = new Metadata();

    call.start(mockCallListener, headers);
    channel.shutdown();
    assertTrue(channel.isShutdown());
    // Name resolved after the channel is shut down, which is possible if the name resolution takes
    // time and is not cancellable. The resolved address will still be passed to the LoadBalancer.
    nameResolverFactory.allResolved();

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
    final ManagedClientTransport goodTransport = mock(ManagedClientTransport.class);
    final ManagedClientTransport badTransport = mock(ManagedClientTransport.class);
    when(goodTransport.newStream(any(MethodDescriptor.class), any(Metadata.class)))
        .thenReturn(mock(ClientStream.class));
    when(mockTransportFactory.newClientTransport(
            same(goodAddress), any(String.class), any(String.class)))
        .thenReturn(goodTransport);
    when(mockTransportFactory.newClientTransport(
            same(badAddress), any(String.class), any(String.class)))
        .thenReturn(badTransport);

    FakeNameResolverFactory nameResolverFactory =
        new FakeNameResolverFactory(Arrays.asList(badServer, goodServer));
    ManagedChannel channel = createChannel(nameResolverFactory, NO_INTERCEPTOR);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    Metadata headers = new Metadata();

    // Start a call. The channel will starts with the first address (badAddress)
    call.start(mockCallListener, headers);
    ArgumentCaptor<ManagedClientTransport.Listener> badTransportListenerCaptor =
        ArgumentCaptor.forClass(ManagedClientTransport.Listener.class);
    verify(badTransport, timeout(1000)).start(badTransportListenerCaptor.capture());
    verify(mockTransportFactory)
        .newClientTransport(same(badAddress), any(String.class), any(String.class));
    verify(mockTransportFactory, times(0))
          .newClientTransport(same(goodAddress), any(String.class), any(String.class));
    badTransportListenerCaptor.getValue().transportShutdown(Status.UNAVAILABLE);

    // The channel then try the second address (goodAddress)
    ArgumentCaptor<ManagedClientTransport.Listener> goodTransportListenerCaptor =
        ArgumentCaptor.forClass(ManagedClientTransport.Listener.class);
    verify(mockTransportFactory, timeout(1000))
          .newClientTransport(same(goodAddress), any(String.class), any(String.class));
    verify(goodTransport, timeout(1000)).start(goodTransportListenerCaptor.capture());
    goodTransportListenerCaptor.getValue().transportReady();
    verify(goodTransport, timeout(1000)).newStream(same(method), same(headers));
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
    final ManagedClientTransport transport1 = mock(ManagedClientTransport.class);
    final ManagedClientTransport transport2 = mock(ManagedClientTransport.class);
    when(mockTransportFactory.newClientTransport(same(addr1), any(String.class), any(String.class)))
        .thenReturn(transport1);
    when(mockTransportFactory.newClientTransport(same(addr2), any(String.class), any(String.class)))
        .thenReturn(transport2);

    FakeNameResolverFactory nameResolverFactory =
        new FakeNameResolverFactory(Arrays.asList(server1, server2));
    ManagedChannel channel = createChannel(nameResolverFactory, NO_INTERCEPTOR);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    Metadata headers = new Metadata();

    // Start a call. The channel will starts with the first address, which will fail to connect.
    call.start(mockCallListener, headers);
    verify(transport1, timeout(1000)).start(transportListenerCaptor.capture());
    verify(mockTransportFactory)
        .newClientTransport(same(addr1), any(String.class), any(String.class));
    verify(mockTransportFactory, times(0))
        .newClientTransport(same(addr2), any(String.class), any(String.class));
    transportListenerCaptor.getValue().transportShutdown(Status.UNAVAILABLE);

    // The channel then try the second address, which will fail to connect too.
    verify(transport2, timeout(1000)).start(transportListenerCaptor.capture());
    verify(mockTransportFactory)
        .newClientTransport(same(addr2), any(String.class), any(String.class));
    verify(transport2, timeout(1000)).start(transportListenerCaptor.capture());
    transportListenerCaptor.getValue().transportShutdown(Status.UNAVAILABLE);

    // Call fails
    verify(mockCallListener, timeout(1000)).onClose(statusCaptor.capture(), any(Metadata.class));
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
    final ManagedClientTransport transport1 = mock(ManagedClientTransport.class);
    final ManagedClientTransport transport2 = mock(ManagedClientTransport.class);
    when(transport1.newStream(any(MethodDescriptor.class), any(Metadata.class)))
        .thenReturn(mock(ClientStream.class));
    when(transport2.newStream(any(MethodDescriptor.class), any(Metadata.class)))
        .thenReturn(mock(ClientStream.class));
    when(mockTransportFactory.newClientTransport(same(addr1), any(String.class), any(String.class)))
        .thenReturn(transport1, transport2);

    FakeNameResolverFactory nameResolverFactory =
        new FakeNameResolverFactory(Arrays.asList(server1, server2));
    ManagedChannel channel = createChannel(nameResolverFactory, NO_INTERCEPTOR);
    ClientCall<String, Integer> call = channel.newCall(method, CallOptions.DEFAULT);
    Metadata headers = new Metadata();

    // First call will use the first address
    call.start(mockCallListener, headers);
    verify(mockTransportFactory, timeout(1000))
        .newClientTransport(same(addr1), any(String.class), any(String.class));
    verify(transport1, timeout(1000)).start(transportListenerCaptor.capture());
    transportListenerCaptor.getValue().transportReady();
    verify(transport1, timeout(1000)).newStream(same(method), same(headers));
    transportListenerCaptor.getValue().transportShutdown(Status.UNAVAILABLE);

    // Second call still use the first address, since it was successfully connected.
    ClientCall<String, Integer> call2 = channel.newCall(method, CallOptions.DEFAULT);
    call2.start(mockCallListener, headers);
    verify(transport2, timeout(1000)).start(transportListenerCaptor.capture());
    verify(mockTransportFactory, times(2))
        .newClientTransport(same(addr1), any(String.class), any(String.class));
    transportListenerCaptor.getValue().transportReady();
    verify(transport2, timeout(1000)).newStream(same(method), same(headers));
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
