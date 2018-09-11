/*
 * Copyright 2015 The gRPC Authors
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
import static io.grpc.ConnectivityState.CONNECTING;
import static io.grpc.ConnectivityState.IDLE;
import static io.grpc.ConnectivityState.READY;
import static io.grpc.ConnectivityState.SHUTDOWN;
import static io.grpc.ConnectivityState.TRANSIENT_FAILURE;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.collect.Iterables;
import io.grpc.Attributes;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.InternalChannelz;
import io.grpc.InternalWithLogId;
import io.grpc.Status;
import io.grpc.internal.InternalSubchannel.CallTracingTransport;
import io.grpc.internal.InternalSubchannel.Index;
import io.grpc.internal.TestUtils.MockClientTransportInfo;
import java.net.SocketAddress;
import java.util.Arrays;
import java.util.LinkedList;
import java.util.List;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.After;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/**
 * Unit tests for {@link InternalSubchannel}.
 */
@RunWith(JUnit4.class)
public class InternalSubchannelTest {

  @Rule
  public final ExpectedException thrown = ExpectedException.none();

  private static final String AUTHORITY = "fakeauthority";
  private static final String USER_AGENT = "mosaic";
  private static final ConnectivityStateInfo UNAVAILABLE_STATE =
      ConnectivityStateInfo.forTransientFailure(Status.UNAVAILABLE);
  private static final ConnectivityStateInfo RESOURCE_EXHAUSTED_STATE =
      ConnectivityStateInfo.forTransientFailure(Status.RESOURCE_EXHAUSTED);
  private static final Status SHUTDOWN_REASON = Status.UNAVAILABLE.withDescription("for test");

  // For scheduled executor
  private final FakeClock fakeClock = new FakeClock();
  // For channelExecutor
  private final FakeClock fakeExecutor = new FakeClock();
  private final ChannelExecutor channelExecutor = new ChannelExecutor();

  private final InternalChannelz channelz = new InternalChannelz();

  @Mock private BackoffPolicy mockBackoffPolicy1;
  @Mock private BackoffPolicy mockBackoffPolicy2;
  @Mock private BackoffPolicy mockBackoffPolicy3;
  @Mock private BackoffPolicy.Provider mockBackoffPolicyProvider;
  @Mock private ClientTransportFactory mockTransportFactory;

  private final LinkedList<String> callbackInvokes = new LinkedList<String>();
  private final InternalSubchannel.Callback mockInternalSubchannelCallback =
      new InternalSubchannel.Callback() {
        @Override
        protected void onTerminated(InternalSubchannel is) {
          assertSame(internalSubchannel, is);
          callbackInvokes.add("onTerminated");
        }

        @Override
        protected void onStateChange(InternalSubchannel is, ConnectivityStateInfo newState) {
          assertSame(internalSubchannel, is);
          callbackInvokes.add("onStateChange:" + newState);
        }

        @Override
        protected void onInUse(InternalSubchannel is) {
          assertSame(internalSubchannel, is);
          callbackInvokes.add("onInUse");
        }

        @Override
        protected void onNotInUse(InternalSubchannel is) {
          assertSame(internalSubchannel, is);
          callbackInvokes.add("onNotInUse");
        }
      };

  private InternalSubchannel internalSubchannel;
  private BlockingQueue<MockClientTransportInfo> transports;

  @Before public void setUp() {
    MockitoAnnotations.initMocks(this);

    when(mockBackoffPolicyProvider.get())
        .thenReturn(mockBackoffPolicy1, mockBackoffPolicy2, mockBackoffPolicy3);
    when(mockBackoffPolicy1.nextBackoffNanos()).thenReturn(10L, 100L);
    when(mockBackoffPolicy2.nextBackoffNanos()).thenReturn(10L, 100L);
    when(mockBackoffPolicy3.nextBackoffNanos()).thenReturn(10L, 100L);
    transports = TestUtils.captureTransports(mockTransportFactory);
  }

  @After public void noMorePendingTasks() {
    assertEquals(0, fakeClock.numPendingTasks());
    assertEquals(0, fakeExecutor.numPendingTasks());
  }

  @Test(expected = IllegalArgumentException.class)
  public void constructor_emptyEagList_throws() {
    createInternalSubchannel(new EquivalentAddressGroup[0]);
  }

  @Test(expected = NullPointerException.class)
  public void constructor_eagListWithNull_throws() {
    createInternalSubchannel(new EquivalentAddressGroup[] {null});
  }

  @Test public void eagAttribute_propagatesToTransport() {
    SocketAddress addr = new SocketAddress() {};
    Attributes attr = Attributes.newBuilder().set(Attributes.Key.create("some-key"), "1").build();
    createInternalSubchannel(new EquivalentAddressGroup(Arrays.asList(addr), attr));

    // First attempt
    assertNull(internalSubchannel.obtainActiveTransport());
    assertEquals(CONNECTING, internalSubchannel.getState());
    verify(mockTransportFactory)
        .newClientTransport(addr, createClientTransportOptions().setEagAttributes(attr));
  }

  @Test public void singleAddressReconnect() {
    SocketAddress addr = mock(SocketAddress.class);
    createInternalSubchannel(addr);
    assertEquals(IDLE, internalSubchannel.getState());

    // Invocation counters
    int transportsCreated = 0;
    int backoff1Consulted = 0;
    int backoff2Consulted = 0;
    int backoffReset = 0;

    // First attempt
    assertEquals(IDLE, internalSubchannel.getState());
    assertNoCallbackInvoke();
    assertNull(internalSubchannel.obtainActiveTransport());
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    assertEquals(CONNECTING, internalSubchannel.getState());
    verify(mockTransportFactory, times(++transportsCreated))
        .newClientTransport(addr, createClientTransportOptions());

    // Fail this one. Because there is only one address to try, enter TRANSIENT_FAILURE.
    assertNoCallbackInvoke();
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(TRANSIENT_FAILURE, internalSubchannel.getState());
    assertExactCallbackInvokes("onStateChange:" + UNAVAILABLE_STATE);
    // Backoff reset and using first back-off value interval
    verify(mockBackoffPolicy1, times(++backoff1Consulted)).nextBackoffNanos();
    verify(mockBackoffPolicyProvider, times(++backoffReset)).get();

    // Second attempt
    // Transport creation doesn't happen until time is due
    fakeClock.forwardNanos(9);
    assertNull(internalSubchannel.obtainActiveTransport());
    verify(mockTransportFactory, times(transportsCreated))
        .newClientTransport(addr, createClientTransportOptions());
    assertEquals(TRANSIENT_FAILURE, internalSubchannel.getState());

    assertNoCallbackInvoke();
    fakeClock.forwardNanos(1);
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    assertEquals(CONNECTING, internalSubchannel.getState());
    verify(mockTransportFactory, times(++transportsCreated))
        .newClientTransport(addr, createClientTransportOptions());
    // Fail this one too
    assertNoCallbackInvoke();
    // Here we use a different status from the first failure, and verify that it's passed to
    // the callback.
    transports.poll().listener.transportShutdown(Status.RESOURCE_EXHAUSTED);
    assertEquals(TRANSIENT_FAILURE, internalSubchannel.getState());
    assertExactCallbackInvokes("onStateChange:" + RESOURCE_EXHAUSTED_STATE);
    // Second back-off interval
    verify(mockBackoffPolicy1, times(++backoff1Consulted)).nextBackoffNanos();
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();

    // Third attempt
    // Transport creation doesn't happen until time is due
    fakeClock.forwardNanos(99);
    assertNull(internalSubchannel.obtainActiveTransport());
    verify(mockTransportFactory, times(transportsCreated))
        .newClientTransport(addr, createClientTransportOptions());
    assertEquals(TRANSIENT_FAILURE, internalSubchannel.getState());
    assertNoCallbackInvoke();
    fakeClock.forwardNanos(1);
    assertEquals(CONNECTING, internalSubchannel.getState());
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    assertNull(internalSubchannel.obtainActiveTransport());
    verify(mockTransportFactory, times(++transportsCreated))
        .newClientTransport(addr, createClientTransportOptions());
    // Let this one succeed, will enter READY state.
    assertNoCallbackInvoke();
    transports.peek().listener.transportReady();
    assertExactCallbackInvokes("onStateChange:READY");
    assertEquals(READY, internalSubchannel.getState());
    assertSame(
        transports.peek().transport,
        ((CallTracingTransport) internalSubchannel.obtainActiveTransport()).delegate());

    // Close the READY transport, will enter IDLE state.
    assertNoCallbackInvoke();
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(IDLE, internalSubchannel.getState());
    assertExactCallbackInvokes("onStateChange:IDLE");

    // Back-off is reset, and the next attempt will happen immediately
    assertNull(internalSubchannel.obtainActiveTransport());
    assertEquals(CONNECTING, internalSubchannel.getState());
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    verify(mockTransportFactory, times(++transportsCreated))
        .newClientTransport(addr, createClientTransportOptions());

    // Final checks for consultations on back-off policies
    verify(mockBackoffPolicy1, times(backoff1Consulted)).nextBackoffNanos();
    verify(mockBackoffPolicy2, times(backoff2Consulted)).nextBackoffNanos();
  }

  @Test public void twoAddressesReconnect() {
    SocketAddress addr1 = mock(SocketAddress.class);
    SocketAddress addr2 = mock(SocketAddress.class);
    createInternalSubchannel(addr1, addr2);
    assertEquals(IDLE, internalSubchannel.getState());
    // Invocation counters
    int transportsAddr1 = 0;
    int transportsAddr2 = 0;
    int backoff1Consulted = 0;
    int backoff2Consulted = 0;
    int backoff3Consulted = 0;
    int backoffReset = 0;

    // First attempt
    assertNoCallbackInvoke();
    assertNull(internalSubchannel.obtainActiveTransport());
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    assertEquals(CONNECTING, internalSubchannel.getState());
    verify(mockTransportFactory, times(++transportsAddr1))
        .newClientTransport(addr1, createClientTransportOptions());

    // Let this one fail without success
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    // Still in CONNECTING
    assertNull(internalSubchannel.obtainActiveTransport());
    assertNoCallbackInvoke();
    assertEquals(CONNECTING, internalSubchannel.getState());

    // Second attempt will start immediately. Still no back-off policy.
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    verify(mockTransportFactory, times(++transportsAddr2))
        .newClientTransport(addr2, createClientTransportOptions());
    assertNull(internalSubchannel.obtainActiveTransport());
    // Fail this one too
    assertNoCallbackInvoke();
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    // All addresses have failed. Delayed transport will be in back-off interval.
    assertEquals(TRANSIENT_FAILURE, internalSubchannel.getState());
    assertExactCallbackInvokes("onStateChange:" + UNAVAILABLE_STATE);
    // Backoff reset and first back-off interval begins
    verify(mockBackoffPolicy1, times(++backoff1Consulted)).nextBackoffNanos();
    verify(mockBackoffPolicyProvider, times(++backoffReset)).get();

    // No reconnect during TRANSIENT_FAILURE even when requested.
    assertNull(internalSubchannel.obtainActiveTransport());
    assertNoCallbackInvoke();
    assertEquals(TRANSIENT_FAILURE, internalSubchannel.getState());

    // Third attempt is the first address, thus controlled by the first back-off interval.
    fakeClock.forwardNanos(9);
    verify(mockTransportFactory, times(transportsAddr1))
        .newClientTransport(addr1, createClientTransportOptions());
    assertEquals(TRANSIENT_FAILURE, internalSubchannel.getState());
    assertNoCallbackInvoke();
    fakeClock.forwardNanos(1);
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    assertEquals(CONNECTING, internalSubchannel.getState());
    verify(mockTransportFactory, times(++transportsAddr1))
        .newClientTransport(addr1, createClientTransportOptions());
    // Fail this one too
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(CONNECTING, internalSubchannel.getState());

    // Forth attempt will start immediately. Keep back-off policy.
    assertNull(internalSubchannel.obtainActiveTransport());
    assertEquals(CONNECTING, internalSubchannel.getState());
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    verify(mockTransportFactory, times(++transportsAddr2))
        .newClientTransport(addr2, createClientTransportOptions());
    // Fail this one too
    assertNoCallbackInvoke();
    transports.poll().listener.transportShutdown(Status.RESOURCE_EXHAUSTED);
    // All addresses have failed again. Delayed transport will be in back-off interval.
    assertExactCallbackInvokes("onStateChange:" + RESOURCE_EXHAUSTED_STATE);
    assertEquals(TRANSIENT_FAILURE, internalSubchannel.getState());
    // Second back-off interval begins
    verify(mockBackoffPolicy1, times(++backoff1Consulted)).nextBackoffNanos();
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();

    // Fifth attempt for the first address, thus controlled by the second back-off interval.
    assertEquals(TRANSIENT_FAILURE, internalSubchannel.getState());
    fakeClock.forwardNanos(99);
    verify(mockTransportFactory, times(transportsAddr1))
        .newClientTransport(addr1, createClientTransportOptions());
    assertEquals(TRANSIENT_FAILURE, internalSubchannel.getState());
    assertNoCallbackInvoke();
    fakeClock.forwardNanos(1);
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    assertEquals(CONNECTING, internalSubchannel.getState());
    verify(mockTransportFactory, times(++transportsAddr1))
        .newClientTransport(addr1, createClientTransportOptions());
    // Let it through
    assertNoCallbackInvoke();
    transports.peek().listener.transportReady();
    assertExactCallbackInvokes("onStateChange:READY");
    assertEquals(READY, internalSubchannel.getState());

    assertSame(
        transports.peek().transport,
        ((CallTracingTransport) internalSubchannel.obtainActiveTransport()).delegate());
    // Then close it.
    assertNoCallbackInvoke();
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertExactCallbackInvokes("onStateChange:IDLE");
    assertEquals(IDLE, internalSubchannel.getState());

    // First attempt after a successful connection. Old back-off policy should be ignored, but there
    // is not yet a need for a new one. Start from the first address.
    assertNull(internalSubchannel.obtainActiveTransport());
    assertEquals(CONNECTING, internalSubchannel.getState());
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    verify(mockTransportFactory, times(++transportsAddr1))
        .newClientTransport(addr1, createClientTransportOptions());
    // Fail the transport
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(CONNECTING, internalSubchannel.getState());

    // Second attempt will start immediately. Still no new back-off policy.
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    verify(mockTransportFactory, times(++transportsAddr2))
        .newClientTransport(addr2, createClientTransportOptions());
    // Fail this one too
    assertEquals(CONNECTING, internalSubchannel.getState());
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    // All addresses have failed. Enter TRANSIENT_FAILURE. Back-off in effect.
    assertExactCallbackInvokes("onStateChange:" + UNAVAILABLE_STATE);
    assertEquals(TRANSIENT_FAILURE, internalSubchannel.getState());
    // Back-off reset and first back-off interval begins
    verify(mockBackoffPolicy2, times(++backoff2Consulted)).nextBackoffNanos();
    verify(mockBackoffPolicyProvider, times(++backoffReset)).get();

    // Third attempt is the first address, thus controlled by the first back-off interval.
    fakeClock.forwardNanos(9);
    verify(mockTransportFactory, times(transportsAddr1))
        .newClientTransport(addr1, createClientTransportOptions());
    assertEquals(TRANSIENT_FAILURE, internalSubchannel.getState());
    assertNoCallbackInvoke();
    fakeClock.forwardNanos(1);
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    assertEquals(CONNECTING, internalSubchannel.getState());
    verify(mockTransportFactory, times(++transportsAddr1))
        .newClientTransport(addr1, createClientTransportOptions());

    // Final checks on invocations on back-off policies
    verify(mockBackoffPolicy1, times(backoff1Consulted)).nextBackoffNanos();
    verify(mockBackoffPolicy2, times(backoff2Consulted)).nextBackoffNanos();
    verify(mockBackoffPolicy3, times(backoff3Consulted)).nextBackoffNanos();
  }

  @Test
  public void updateAddresses_emptyEagList_throws() {
    SocketAddress addr = new FakeSocketAddress();
    createInternalSubchannel(addr);
    thrown.expect(IllegalArgumentException.class);
    internalSubchannel.updateAddresses(Arrays.<EquivalentAddressGroup>asList());
  }

  @Test
  public void updateAddresses_eagListWithNull_throws() {
    SocketAddress addr = new FakeSocketAddress();
    createInternalSubchannel(addr);
    List<EquivalentAddressGroup> eags = Arrays.asList((EquivalentAddressGroup) null);
    thrown.expect(NullPointerException.class);
    internalSubchannel.updateAddresses(eags);
  }

  @Test public void updateAddresses_intersecting_ready() {
    SocketAddress addr1 = mock(SocketAddress.class);
    SocketAddress addr2 = mock(SocketAddress.class);
    SocketAddress addr3 = mock(SocketAddress.class);
    createInternalSubchannel(addr1, addr2);
    assertEquals(IDLE, internalSubchannel.getState());

    // First address fails
    assertNull(internalSubchannel.obtainActiveTransport());
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockTransportFactory).newClientTransport(addr1, createClientTransportOptions());
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(CONNECTING, internalSubchannel.getState());

    // Second address connects
    verify(mockTransportFactory).newClientTransport(addr2, createClientTransportOptions());
    transports.peek().listener.transportReady();
    assertExactCallbackInvokes("onStateChange:READY");
    assertEquals(READY, internalSubchannel.getState());

    // Update addresses
    internalSubchannel.updateAddresses(
        Arrays.asList(new EquivalentAddressGroup(Arrays.asList(addr2, addr3))));
    assertNoCallbackInvoke();
    assertEquals(READY, internalSubchannel.getState());
    verify(transports.peek().transport, never()).shutdown(any(Status.class));
    verify(transports.peek().transport, never()).shutdownNow(any(Status.class));

    // And new addresses chosen when re-connecting
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertExactCallbackInvokes("onStateChange:IDLE");

    assertNull(internalSubchannel.obtainActiveTransport());
    assertEquals(0, fakeClock.numPendingTasks());
    verify(mockTransportFactory, times(2))
        .newClientTransport(addr2, createClientTransportOptions());
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    verify(mockTransportFactory).newClientTransport(addr3, createClientTransportOptions());
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    verifyNoMoreInteractions(mockTransportFactory);

    fakeClock.forwardNanos(10); // Drain retry, but don't care about result
  }

  @Test public void updateAddresses_intersecting_connecting() {
    SocketAddress addr1 = mock(SocketAddress.class);
    SocketAddress addr2 = mock(SocketAddress.class);
    SocketAddress addr3 = mock(SocketAddress.class);
    createInternalSubchannel(addr1, addr2);
    assertEquals(IDLE, internalSubchannel.getState());

    // First address fails
    assertNull(internalSubchannel.obtainActiveTransport());
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockTransportFactory).newClientTransport(addr1, createClientTransportOptions());
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(CONNECTING, internalSubchannel.getState());

    // Second address connecting
    verify(mockTransportFactory).newClientTransport(addr2, createClientTransportOptions());
    assertNoCallbackInvoke();
    assertEquals(CONNECTING, internalSubchannel.getState());

    // Update addresses
    internalSubchannel.updateAddresses(
        Arrays.asList(new EquivalentAddressGroup(Arrays.asList(addr2, addr3))));
    assertNoCallbackInvoke();
    assertEquals(CONNECTING, internalSubchannel.getState());
    verify(transports.peek().transport, never()).shutdown(any(Status.class));
    verify(transports.peek().transport, never()).shutdownNow(any(Status.class));

    // And new addresses chosen when re-connecting
    transports.peek().listener.transportReady();
    assertExactCallbackInvokes("onStateChange:READY");
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertExactCallbackInvokes("onStateChange:IDLE");

    assertNull(internalSubchannel.obtainActiveTransport());
    assertEquals(0, fakeClock.numPendingTasks());
    verify(mockTransportFactory, times(2))
        .newClientTransport(addr2, createClientTransportOptions());
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    verify(mockTransportFactory).newClientTransport(addr3, createClientTransportOptions());
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    verifyNoMoreInteractions(mockTransportFactory);

    fakeClock.forwardNanos(10); // Drain retry, but don't care about result
  }

  @Test public void updateAddresses_disjoint_idle() {
    SocketAddress addr1 = mock(SocketAddress.class);
    SocketAddress addr2 = mock(SocketAddress.class);

    createInternalSubchannel(addr1);
    internalSubchannel.updateAddresses(Arrays.asList(new EquivalentAddressGroup(addr2)));

    // Nothing happened on address update
    verify(mockTransportFactory, never())
        .newClientTransport(addr1, createClientTransportOptions());
    verify(mockTransportFactory, never())
        .newClientTransport(addr2, createClientTransportOptions());
    verifyNoMoreInteractions(mockTransportFactory);
    assertNoCallbackInvoke();
    assertEquals(IDLE, internalSubchannel.getState());

    // But new address chosen when connecting
    assertNull(internalSubchannel.obtainActiveTransport());
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockTransportFactory).newClientTransport(addr2, createClientTransportOptions());

    // And no other addresses attempted
    assertEquals(0, fakeClock.numPendingTasks());
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertExactCallbackInvokes("onStateChange:" + UNAVAILABLE_STATE);
    assertEquals(TRANSIENT_FAILURE, internalSubchannel.getState());
    verifyNoMoreInteractions(mockTransportFactory);

    fakeClock.forwardNanos(10); // Drain retry, but don't care about result
  }

  @Test public void updateAddresses_disjoint_ready() {
    SocketAddress addr1 = mock(SocketAddress.class);
    SocketAddress addr2 = mock(SocketAddress.class);
    SocketAddress addr3 = mock(SocketAddress.class);
    SocketAddress addr4 = mock(SocketAddress.class);
    createInternalSubchannel(addr1, addr2);
    assertEquals(IDLE, internalSubchannel.getState());

    // First address fails
    assertNull(internalSubchannel.obtainActiveTransport());
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockTransportFactory).newClientTransport(addr1, createClientTransportOptions());
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(CONNECTING, internalSubchannel.getState());

    // Second address connects
    verify(mockTransportFactory).newClientTransport(addr2, createClientTransportOptions());
    transports.peek().listener.transportReady();
    assertExactCallbackInvokes("onStateChange:READY");
    assertEquals(READY, internalSubchannel.getState());

    // Update addresses
    internalSubchannel.updateAddresses(
        Arrays.asList(new EquivalentAddressGroup(Arrays.asList(addr3, addr4))));
    assertExactCallbackInvokes("onStateChange:IDLE");
    assertEquals(IDLE, internalSubchannel.getState());
    verify(transports.peek().transport).shutdown(any(Status.class));

    // And new addresses chosen when re-connecting
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertNoCallbackInvoke();
    assertEquals(IDLE, internalSubchannel.getState());

    assertNull(internalSubchannel.obtainActiveTransport());
    assertEquals(0, fakeClock.numPendingTasks());
    verify(mockTransportFactory).newClientTransport(addr3, createClientTransportOptions());
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    verify(mockTransportFactory).newClientTransport(addr4, createClientTransportOptions());
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    verifyNoMoreInteractions(mockTransportFactory);

    fakeClock.forwardNanos(10); // Drain retry, but don't care about result
  }

  @Test public void updateAddresses_disjoint_connecting() {
    SocketAddress addr1 = mock(SocketAddress.class);
    SocketAddress addr2 = mock(SocketAddress.class);
    SocketAddress addr3 = mock(SocketAddress.class);
    SocketAddress addr4 = mock(SocketAddress.class);
    createInternalSubchannel(addr1, addr2);
    assertEquals(IDLE, internalSubchannel.getState());

    // First address fails
    assertNull(internalSubchannel.obtainActiveTransport());
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockTransportFactory).newClientTransport(addr1, createClientTransportOptions());
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(CONNECTING, internalSubchannel.getState());

    // Second address connecting
    verify(mockTransportFactory).newClientTransport(addr2, createClientTransportOptions());
    assertNoCallbackInvoke();
    assertEquals(CONNECTING, internalSubchannel.getState());

    // Update addresses
    internalSubchannel.updateAddresses(
        Arrays.asList(new EquivalentAddressGroup(Arrays.asList(addr3, addr4))));
    assertNoCallbackInvoke();
    assertEquals(CONNECTING, internalSubchannel.getState());

    // And new addresses chosen immediately
    verify(transports.poll().transport).shutdown(any(Status.class));
    assertNoCallbackInvoke();
    assertEquals(CONNECTING, internalSubchannel.getState());

    assertNull(internalSubchannel.obtainActiveTransport());
    assertEquals(0, fakeClock.numPendingTasks());
    verify(mockTransportFactory).newClientTransport(addr3, createClientTransportOptions());
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    verify(mockTransportFactory).newClientTransport(addr4, createClientTransportOptions());
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    verifyNoMoreInteractions(mockTransportFactory);

    fakeClock.forwardNanos(10); // Drain retry, but don't care about result
  }

  @Test
  public void connectIsLazy() {
    SocketAddress addr = mock(SocketAddress.class);
    createInternalSubchannel(addr);

    // Invocation counters
    int transportsCreated = 0;

    // Won't connect until requested
    verify(mockTransportFactory, times(transportsCreated))
        .newClientTransport(addr, createClientTransportOptions());

    // First attempt
    internalSubchannel.obtainActiveTransport();
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockTransportFactory, times(++transportsCreated))
        .newClientTransport(addr, createClientTransportOptions());

    // Fail this one
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertExactCallbackInvokes("onStateChange:" + UNAVAILABLE_STATE);

    // Will always reconnect after back-off
    fakeClock.forwardNanos(10);
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockTransportFactory, times(++transportsCreated))
        .newClientTransport(addr, createClientTransportOptions());

    // Make this one proceed
    transports.peek().listener.transportReady();
    assertExactCallbackInvokes("onStateChange:READY");
    // Then go-away
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertExactCallbackInvokes("onStateChange:IDLE");

    // No scheduled tasks that would ever try to reconnect ...
    assertEquals(0, fakeClock.numPendingTasks());
    assertEquals(0, fakeExecutor.numPendingTasks());

    // ... until it's requested.
    internalSubchannel.obtainActiveTransport();
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockTransportFactory, times(++transportsCreated))
        .newClientTransport(addr, createClientTransportOptions());
  }

  @Test
  public void shutdownWhenReady() throws Exception {
    SocketAddress addr = mock(SocketAddress.class);
    createInternalSubchannel(addr);

    internalSubchannel.obtainActiveTransport();
    MockClientTransportInfo transportInfo = transports.poll();
    transportInfo.listener.transportReady();
    assertExactCallbackInvokes("onStateChange:CONNECTING", "onStateChange:READY");

    internalSubchannel.shutdown(SHUTDOWN_REASON);
    verify(transportInfo.transport).shutdown(same(SHUTDOWN_REASON));
    assertExactCallbackInvokes("onStateChange:SHUTDOWN");

    transportInfo.listener.transportTerminated();
    assertExactCallbackInvokes("onTerminated");
    verify(transportInfo.transport, never()).shutdownNow(any(Status.class));
  }

  @Test
  public void shutdownBeforeTransportCreated() throws Exception {
    SocketAddress addr = mock(SocketAddress.class);
    createInternalSubchannel(addr);

    // First transport is created immediately
    internalSubchannel.obtainActiveTransport();
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockTransportFactory).newClientTransport(addr, createClientTransportOptions());

    // Fail this one
    MockClientTransportInfo transportInfo = transports.poll();
    transportInfo.listener.transportShutdown(Status.UNAVAILABLE);
    transportInfo.listener.transportTerminated();

    // Entering TRANSIENT_FAILURE, waiting for back-off
    assertExactCallbackInvokes("onStateChange:" + UNAVAILABLE_STATE);

    // Save the reconnectTask before shutting down
    FakeClock.ScheduledTask reconnectTask = null;
    for (FakeClock.ScheduledTask task : fakeClock.getPendingTasks()) {
      if (task.command.toString().contains("EndOfCurrentBackoff")) {
        assertNull("There shouldn't be more than one reconnectTask", reconnectTask);
        assertFalse(task.isDone());
        reconnectTask = task;
      }
    }
    assertNotNull("There should be at least one reconnectTask", reconnectTask);

    // Shut down InternalSubchannel before the transport is created.
    internalSubchannel.shutdown(SHUTDOWN_REASON);
    assertTrue(reconnectTask.isCancelled());
    // InternalSubchannel terminated promptly.
    assertExactCallbackInvokes("onStateChange:SHUTDOWN", "onTerminated");

    // Simulate a race between reconnectTask cancellation and execution -- the task runs anyway.
    // This should not lead to the creation of a new transport.
    reconnectTask.command.run();

    // Futher call to obtainActiveTransport() is no-op.
    assertNull(internalSubchannel.obtainActiveTransport());
    assertEquals(SHUTDOWN, internalSubchannel.getState());
    assertNoCallbackInvoke();

    // No more transports will be created.
    fakeClock.forwardNanos(10000);
    assertEquals(SHUTDOWN, internalSubchannel.getState());
    verifyNoMoreInteractions(mockTransportFactory);
    assertEquals(0, transports.size());
    assertNoCallbackInvoke();
  }

  @Test
  public void shutdownBeforeTransportReady() throws Exception {
    SocketAddress addr = mock(SocketAddress.class);
    createInternalSubchannel(addr);

    internalSubchannel.obtainActiveTransport();
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    MockClientTransportInfo transportInfo = transports.poll();

    // Shutdown the InternalSubchannel before the pending transport is ready
    assertNull(internalSubchannel.obtainActiveTransport());
    internalSubchannel.shutdown(SHUTDOWN_REASON);
    assertExactCallbackInvokes("onStateChange:SHUTDOWN");

    // The transport should've been shut down even though it's not the active transport yet.
    verify(transportInfo.transport).shutdown(same(SHUTDOWN_REASON));
    transportInfo.listener.transportShutdown(Status.UNAVAILABLE);
    assertNoCallbackInvoke();
    transportInfo.listener.transportTerminated();
    assertExactCallbackInvokes("onTerminated");
    assertEquals(SHUTDOWN, internalSubchannel.getState());
  }

  @Test
  public void shutdownNow() throws Exception {
    SocketAddress addr = mock(SocketAddress.class);
    createInternalSubchannel(addr);

    internalSubchannel.obtainActiveTransport();
    MockClientTransportInfo t1 = transports.poll();
    t1.listener.transportReady();
    assertExactCallbackInvokes("onStateChange:CONNECTING", "onStateChange:READY");
    t1.listener.transportShutdown(Status.UNAVAILABLE);
    assertExactCallbackInvokes("onStateChange:IDLE");

    internalSubchannel.obtainActiveTransport();
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    MockClientTransportInfo t2 = transports.poll();

    Status status = Status.UNAVAILABLE.withDescription("Requested");
    internalSubchannel.shutdownNow(status);

    verify(t1.transport).shutdownNow(same(status));
    verify(t2.transport).shutdownNow(same(status));
    assertExactCallbackInvokes("onStateChange:SHUTDOWN");
  }

  @Test
  public void obtainTransportAfterShutdown() throws Exception {
    SocketAddress addr = mock(SocketAddress.class);
    createInternalSubchannel(addr);

    internalSubchannel.shutdown(SHUTDOWN_REASON);
    assertExactCallbackInvokes("onStateChange:SHUTDOWN", "onTerminated");
    assertEquals(SHUTDOWN, internalSubchannel.getState());
    assertNull(internalSubchannel.obtainActiveTransport());
    verify(mockTransportFactory, times(0))
        .newClientTransport(addr, createClientTransportOptions());
    assertNoCallbackInvoke();
    assertEquals(SHUTDOWN, internalSubchannel.getState());
  }

  @Test
  public void logId() {
    createInternalSubchannel(mock(SocketAddress.class));

    assertNotNull(internalSubchannel.getLogId());
  }

  @Test
  public void inUseState() {
    SocketAddress addr = mock(SocketAddress.class);
    createInternalSubchannel(addr);

    internalSubchannel.obtainActiveTransport();
    MockClientTransportInfo t0 = transports.poll();
    t0.listener.transportReady();
    assertExactCallbackInvokes("onStateChange:CONNECTING", "onStateChange:READY");
    t0.listener.transportInUse(true);
    assertExactCallbackInvokes("onInUse");

    t0.listener.transportInUse(false);
    assertExactCallbackInvokes("onNotInUse");

    t0.listener.transportInUse(true);
    assertExactCallbackInvokes("onInUse");
    t0.listener.transportShutdown(Status.UNAVAILABLE);
    assertExactCallbackInvokes("onStateChange:IDLE");

    assertNull(internalSubchannel.obtainActiveTransport());
    MockClientTransportInfo t1 = transports.poll();
    t1.listener.transportReady();
    assertExactCallbackInvokes("onStateChange:CONNECTING", "onStateChange:READY");
    t1.listener.transportInUse(true);
    // InternalSubchannel is already in-use, thus doesn't call the callback
    assertNoCallbackInvoke();

    t1.listener.transportInUse(false);
    // t0 is still in-use
    assertNoCallbackInvoke();

    t0.listener.transportInUse(false);
    assertExactCallbackInvokes("onNotInUse");
  }

  @Test
  public void transportTerminateWithoutExitingInUse() {
    // An imperfect transport that terminates without going out of in-use. InternalSubchannel will
    // clear the in-use bit for it.
    SocketAddress addr = mock(SocketAddress.class);
    createInternalSubchannel(addr);

    internalSubchannel.obtainActiveTransport();
    MockClientTransportInfo t0 = transports.poll();
    t0.listener.transportReady();
    assertExactCallbackInvokes("onStateChange:CONNECTING", "onStateChange:READY");
    t0.listener.transportInUse(true);
    assertExactCallbackInvokes("onInUse");

    t0.listener.transportShutdown(Status.UNAVAILABLE);
    assertExactCallbackInvokes("onStateChange:IDLE");
    t0.listener.transportTerminated();
    assertExactCallbackInvokes("onNotInUse");
  }

  @Test
  public void transportStartReturnsRunnable() {
    SocketAddress addr1 = mock(SocketAddress.class);
    SocketAddress addr2 = mock(SocketAddress.class);
    createInternalSubchannel(addr1, addr2);
    final AtomicInteger runnableInvokes = new AtomicInteger(0);
    Runnable startRunnable = new Runnable() {
        @Override
        public void run() {
          runnableInvokes.incrementAndGet();
        }
      };
    transports = TestUtils.captureTransports(mockTransportFactory, startRunnable);

    assertEquals(0, runnableInvokes.get());
    internalSubchannel.obtainActiveTransport();
    assertEquals(1, runnableInvokes.get());
    internalSubchannel.obtainActiveTransport();
    assertEquals(1, runnableInvokes.get());

    MockClientTransportInfo t0 = transports.poll();
    t0.listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(2, runnableInvokes.get());

    // 2nd address: reconnect immediatly
    MockClientTransportInfo t1 = transports.poll();
    t1.listener.transportShutdown(Status.UNAVAILABLE);

    // Addresses exhausted, waiting for back-off.
    assertEquals(2, runnableInvokes.get());
    // Run out the back-off period
    fakeClock.forwardNanos(10);
    assertEquals(3, runnableInvokes.get());

    // This test doesn't care about scheduled InternalSubchannel callbacks.  Clear it up so that
    // noMorePendingTasks() won't fail.
    fakeExecutor.runDueTasks();
    assertEquals(3, runnableInvokes.get());
  }

  @Test
  public void resetConnectBackoff() throws Exception {
    SocketAddress addr = mock(SocketAddress.class);
    createInternalSubchannel(addr);

    // Move into TRANSIENT_FAILURE to schedule reconnect
    internalSubchannel.obtainActiveTransport();
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockTransportFactory).newClientTransport(addr, createClientTransportOptions());
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertExactCallbackInvokes("onStateChange:" + UNAVAILABLE_STATE);

    // Save the reconnectTask
    FakeClock.ScheduledTask reconnectTask = null;
    for (FakeClock.ScheduledTask task : fakeClock.getPendingTasks()) {
      if (task.command.toString().contains("EndOfCurrentBackoff")) {
        assertNull("There shouldn't be more than one reconnectTask", reconnectTask);
        assertFalse(task.isDone());
        reconnectTask = task;
      }
    }
    assertNotNull("There should be at least one reconnectTask", reconnectTask);

    internalSubchannel.resetConnectBackoff();

    verify(mockTransportFactory, times(2))
        .newClientTransport(addr, createClientTransportOptions());
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    assertTrue(reconnectTask.isCancelled());

    // Simulate a race between cancel and the task scheduler. Should be a no-op.
    reconnectTask.command.run();
    assertNoCallbackInvoke();
    verify(mockTransportFactory, times(2))
        .newClientTransport(addr, createClientTransportOptions());
    verify(mockBackoffPolicyProvider, times(1)).get();

    // Fail the reconnect attempt to verify that a fresh reconnect policy is generated after
    // invoking resetConnectBackoff()
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertExactCallbackInvokes("onStateChange:" + UNAVAILABLE_STATE);
    verify(mockBackoffPolicyProvider, times(2)).get();
    fakeClock.forwardNanos(10);
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    assertEquals(CONNECTING, internalSubchannel.getState());
  }

  @Test
  public void resetConnectBackoff_noopOnIdleTransport() throws Exception {
    SocketAddress addr = mock(SocketAddress.class);
    createInternalSubchannel(addr);
    assertEquals(IDLE, internalSubchannel.getState());

    internalSubchannel.resetConnectBackoff();

    assertNoCallbackInvoke();
  }

  @Test
  public void channelzMembership() throws Exception {
    SocketAddress addr1 = mock(SocketAddress.class);
    createInternalSubchannel(addr1);
    internalSubchannel.obtainActiveTransport();

    MockClientTransportInfo t0 = transports.poll();
    assertTrue(channelz.containsClientSocket(t0.transport.getLogId()));
    t0.listener.transportTerminated();
    assertFalse(channelz.containsClientSocket(t0.transport.getLogId()));
  }

  @Test
  public void channelzStatContainsTransport() throws Exception {
    SocketAddress addr = new SocketAddress() {};
    assertThat(transports).isEmpty();
    createInternalSubchannel(addr);
    internalSubchannel.obtainActiveTransport();

    InternalWithLogId registeredTransport
        = Iterables.getOnlyElement(internalSubchannel.getStats().get().sockets);
    MockClientTransportInfo actualTransport = Iterables.getOnlyElement(transports);
    assertEquals(actualTransport.transport.getLogId(), registeredTransport.getLogId());
  }

  @Test public void index_looping() {
    Attributes.Key<String> key = Attributes.Key.create("some-key");
    Attributes attr1 = Attributes.newBuilder().set(key, "1").build();
    Attributes attr2 = Attributes.newBuilder().set(key, "2").build();
    Attributes attr3 = Attributes.newBuilder().set(key, "3").build();
    SocketAddress addr1 = new FakeSocketAddress();
    SocketAddress addr2 = new FakeSocketAddress();
    SocketAddress addr3 = new FakeSocketAddress();
    SocketAddress addr4 = new FakeSocketAddress();
    SocketAddress addr5 = new FakeSocketAddress();
    Index index = new Index(Arrays.asList(
        new EquivalentAddressGroup(Arrays.asList(addr1, addr2), attr1),
        new EquivalentAddressGroup(Arrays.asList(addr3), attr2),
        new EquivalentAddressGroup(Arrays.asList(addr4, addr5), attr3)));
    assertThat(index.getCurrentAddress()).isSameAs(addr1);
    assertThat(index.getCurrentEagAttributes()).isSameAs(attr1);
    assertThat(index.isAtBeginning()).isTrue();
    assertThat(index.isValid()).isTrue();

    index.increment();
    assertThat(index.getCurrentAddress()).isSameAs(addr2);
    assertThat(index.getCurrentEagAttributes()).isSameAs(attr1);
    assertThat(index.isAtBeginning()).isFalse();
    assertThat(index.isValid()).isTrue();

    index.increment();
    assertThat(index.getCurrentAddress()).isSameAs(addr3);
    assertThat(index.getCurrentEagAttributes()).isSameAs(attr2);
    assertThat(index.isAtBeginning()).isFalse();
    assertThat(index.isValid()).isTrue();

    index.increment();
    assertThat(index.getCurrentAddress()).isSameAs(addr4);
    assertThat(index.getCurrentEagAttributes()).isSameAs(attr3);
    assertThat(index.isAtBeginning()).isFalse();
    assertThat(index.isValid()).isTrue();

    index.increment();
    assertThat(index.getCurrentAddress()).isSameAs(addr5);
    assertThat(index.getCurrentEagAttributes()).isSameAs(attr3);
    assertThat(index.isAtBeginning()).isFalse();
    assertThat(index.isValid()).isTrue();

    index.increment();
    assertThat(index.isAtBeginning()).isFalse();
    assertThat(index.isValid()).isFalse();

    index.reset();
    assertThat(index.getCurrentAddress()).isSameAs(addr1);
    assertThat(index.getCurrentEagAttributes()).isSameAs(attr1);
    assertThat(index.isAtBeginning()).isTrue();
    assertThat(index.isValid()).isTrue();

    // We want to make sure both groupIndex and addressIndex are reset
    index.increment();
    index.increment();
    index.increment();
    index.increment();
    assertThat(index.getCurrentAddress()).isSameAs(addr5);
    assertThat(index.getCurrentEagAttributes()).isSameAs(attr3);
    index.reset();
    assertThat(index.getCurrentAddress()).isSameAs(addr1);
    assertThat(index.getCurrentEagAttributes()).isSameAs(attr1);
  }

  @Test public void index_updateGroups_resets() {
    SocketAddress addr1 = new FakeSocketAddress();
    SocketAddress addr2 = new FakeSocketAddress();
    SocketAddress addr3 = new FakeSocketAddress();
    Index index = new Index(Arrays.asList(
        new EquivalentAddressGroup(Arrays.asList(addr1)),
        new EquivalentAddressGroup(Arrays.asList(addr2, addr3))));
    index.increment();
    index.increment();
    // We want to make sure both groupIndex and addressIndex are reset
    index.updateGroups(Arrays.asList(
        new EquivalentAddressGroup(Arrays.asList(addr1)),
        new EquivalentAddressGroup(Arrays.asList(addr2, addr3))));
    assertThat(index.getCurrentAddress()).isSameAs(addr1);
  }

  @Test public void index_seekTo() {
    SocketAddress addr1 = new FakeSocketAddress();
    SocketAddress addr2 = new FakeSocketAddress();
    SocketAddress addr3 = new FakeSocketAddress();
    Index index = new Index(Arrays.asList(
        new EquivalentAddressGroup(Arrays.asList(addr1, addr2)),
        new EquivalentAddressGroup(Arrays.asList(addr3))));
    assertThat(index.seekTo(addr3)).isTrue();
    assertThat(index.getCurrentAddress()).isSameAs(addr3);
    assertThat(index.seekTo(addr1)).isTrue();
    assertThat(index.getCurrentAddress()).isSameAs(addr1);
    assertThat(index.seekTo(addr2)).isTrue();
    assertThat(index.getCurrentAddress()).isSameAs(addr2);
    index.seekTo(new FakeSocketAddress());
    // Failed seekTo doesn't change the index
    assertThat(index.getCurrentAddress()).isSameAs(addr2);
  }

  /** Create ClientTransportOptions. Should not be reused if it may be mutated. */
  private ClientTransportFactory.ClientTransportOptions createClientTransportOptions() {
    return new ClientTransportFactory.ClientTransportOptions()
        .setAuthority(AUTHORITY)
        .setUserAgent(USER_AGENT);
  }

  private void createInternalSubchannel(SocketAddress ... addrs) {
    createInternalSubchannel(new EquivalentAddressGroup(Arrays.asList(addrs)));
  }

  private void createInternalSubchannel(EquivalentAddressGroup ... addrs) {
    List<EquivalentAddressGroup> addressGroups = Arrays.asList(addrs);
    internalSubchannel = new InternalSubchannel(addressGroups, AUTHORITY, USER_AGENT,
        mockBackoffPolicyProvider, mockTransportFactory, fakeClock.getScheduledExecutorService(),
        fakeClock.getStopwatchSupplier(), channelExecutor, mockInternalSubchannelCallback,
        channelz, CallTracer.getDefaultFactory().create(), null,
        new TimeProvider() {
          @Override
          public long currentTimeNanos() {
            return fakeClock.getTicker().read();
          }
        });
  }

  private void assertNoCallbackInvoke() {
    while (fakeExecutor.runDueTasks() > 0) {}
    assertEquals(0, callbackInvokes.size());
  }

  private void assertExactCallbackInvokes(String ... expectedInvokes) {
    assertEquals(0, channelExecutor.numPendingTasks());
    assertEquals(Arrays.asList(expectedInvokes), callbackInvokes);
    callbackInvokes.clear();
  }

  private static class FakeSocketAddress extends SocketAddress {}
}
