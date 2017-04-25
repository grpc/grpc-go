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
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import io.grpc.ConnectivityState;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.Status;
import io.grpc.internal.TestUtils.MockClientTransportInfo;
import java.net.SocketAddress;
import java.util.Arrays;
import java.util.LinkedList;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/**
 * Unit tests for {@link InternalSubchannel}.
 *
 * <p>It only tests the logic that is not covered by {@link ManagedChannelImplTransportManagerTest}.
 */
@RunWith(JUnit4.class)
public class InternalSubchannelTest {

  private static final String AUTHORITY = "fakeauthority";
  private static final String USER_AGENT = "mosaic";
  private static final ConnectivityStateInfo UNAVAILABLE_STATE =
      ConnectivityStateInfo.forTransientFailure(Status.UNAVAILABLE);
  private static final ConnectivityStateInfo RESOURCE_EXHAUSTED_STATE =
      ConnectivityStateInfo.forTransientFailure(Status.RESOURCE_EXHAUSTED);

  // For scheduled executor
  private final FakeClock fakeClock = new FakeClock();
  // For channelExecutor
  private final FakeClock fakeExecutor = new FakeClock();
  private final ChannelExecutor channelExecutor = new ChannelExecutor();

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
  private EquivalentAddressGroup addressGroup;
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

  @Test public void singleAddressReconnect() {
    SocketAddress addr = mock(SocketAddress.class);
    createInternalSubchannel(addr);
    assertEquals(ConnectivityState.IDLE, internalSubchannel.getState());

    // Invocation counters
    int transportsCreated = 0;
    int backoff1Consulted = 0;
    int backoff2Consulted = 0;
    int backoffReset = 0;

    // First attempt
    assertEquals(ConnectivityState.IDLE, internalSubchannel.getState());
    assertNoCallbackInvoke();
    assertNull(internalSubchannel.obtainActiveTransport());
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());
    verify(mockTransportFactory, times(++transportsCreated))
        .newClientTransport(addr, AUTHORITY, USER_AGENT);

    // Fail this one. Because there is only one address to try, enter TRANSIENT_FAILURE.
    assertNoCallbackInvoke();
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, internalSubchannel.getState());
    assertExactCallbackInvokes("onStateChange:" + UNAVAILABLE_STATE);
    // Backoff reset and using first back-off value interval
    verify(mockBackoffPolicy1, times(++backoff1Consulted)).nextBackoffNanos();
    verify(mockBackoffPolicyProvider, times(++backoffReset)).get();

    // Second attempt
    // Transport creation doesn't happen until time is due
    fakeClock.forwardNanos(9);
    assertNull(internalSubchannel.obtainActiveTransport());
    verify(mockTransportFactory, times(transportsCreated))
        .newClientTransport(addr, AUTHORITY, USER_AGENT);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, internalSubchannel.getState());

    assertNoCallbackInvoke();
    fakeClock.forwardNanos(1);
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());
    verify(mockTransportFactory, times(++transportsCreated))
        .newClientTransport(addr, AUTHORITY, USER_AGENT);
    // Fail this one too
    assertNoCallbackInvoke();
    // Here we use a different status from the first failure, and verify that it's passed to
    // the callback.
    transports.poll().listener.transportShutdown(Status.RESOURCE_EXHAUSTED);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, internalSubchannel.getState());
    assertExactCallbackInvokes("onStateChange:" + RESOURCE_EXHAUSTED_STATE);
    // Second back-off interval
    verify(mockBackoffPolicy1, times(++backoff1Consulted)).nextBackoffNanos();
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();

    // Third attempt
    // Transport creation doesn't happen until time is due
    fakeClock.forwardNanos(99);
    assertNull(internalSubchannel.obtainActiveTransport());
    verify(mockTransportFactory, times(transportsCreated))
        .newClientTransport(addr, AUTHORITY, USER_AGENT);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, internalSubchannel.getState());
    assertNoCallbackInvoke();
    fakeClock.forwardNanos(1);
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    assertNull(internalSubchannel.obtainActiveTransport());
    verify(mockTransportFactory, times(++transportsCreated))
        .newClientTransport(addr, AUTHORITY, USER_AGENT);
    // Let this one succeed, will enter READY state.
    assertNoCallbackInvoke();
    transports.peek().listener.transportReady();
    assertExactCallbackInvokes("onStateChange:READY");
    assertEquals(ConnectivityState.READY, internalSubchannel.getState());
    assertSame(transports.peek().transport, internalSubchannel.obtainActiveTransport());

    // Close the READY transport, will enter IDLE state.
    assertNoCallbackInvoke();
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.IDLE, internalSubchannel.getState());
    assertExactCallbackInvokes("onStateChange:IDLE");

    // Back-off is reset, and the next attempt will happen immediately
    assertNull(internalSubchannel.obtainActiveTransport());
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    verify(mockTransportFactory, times(++transportsCreated))
        .newClientTransport(addr, AUTHORITY, USER_AGENT);

    // Final checks for consultations on back-off policies
    verify(mockBackoffPolicy1, times(backoff1Consulted)).nextBackoffNanos();
    verify(mockBackoffPolicy2, times(backoff2Consulted)).nextBackoffNanos();
  }

  @Test public void twoAddressesReconnect() {
    SocketAddress addr1 = mock(SocketAddress.class);
    SocketAddress addr2 = mock(SocketAddress.class);
    createInternalSubchannel(addr1, addr2);
    assertEquals(ConnectivityState.IDLE, internalSubchannel.getState());
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
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());
    verify(mockTransportFactory, times(++transportsAddr1))
        .newClientTransport(addr1, AUTHORITY, USER_AGENT);

    // Let this one fail without success
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    // Still in CONNECTING
    assertNull(internalSubchannel.obtainActiveTransport());
    assertNoCallbackInvoke();
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());

    // Second attempt will start immediately. Still no back-off policy.
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    verify(mockTransportFactory, times(++transportsAddr2))
        .newClientTransport(addr2, AUTHORITY, USER_AGENT);
    assertNull(internalSubchannel.obtainActiveTransport());
    // Fail this one too
    assertNoCallbackInvoke();
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    // All addresses have failed. Delayed transport will be in back-off interval.
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, internalSubchannel.getState());
    assertExactCallbackInvokes("onStateChange:" + UNAVAILABLE_STATE);
    // Backoff reset and first back-off interval begins
    verify(mockBackoffPolicy1, times(++backoff1Consulted)).nextBackoffNanos();
    verify(mockBackoffPolicyProvider, times(++backoffReset)).get();

    // No reconnect during TRANSIENT_FAILURE even when requested.
    assertNull(internalSubchannel.obtainActiveTransport());
    assertNoCallbackInvoke();
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, internalSubchannel.getState());

    // Third attempt is the first address, thus controlled by the first back-off interval.
    fakeClock.forwardNanos(9);
    verify(mockTransportFactory, times(transportsAddr1))
        .newClientTransport(addr1, AUTHORITY, USER_AGENT);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, internalSubchannel.getState());
    assertNoCallbackInvoke();
    fakeClock.forwardNanos(1);
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());
    verify(mockTransportFactory, times(++transportsAddr1))
        .newClientTransport(addr1, AUTHORITY, USER_AGENT);
    // Fail this one too
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());

    // Forth attempt will start immediately. Keep back-off policy.
    assertNull(internalSubchannel.obtainActiveTransport());
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    verify(mockTransportFactory, times(++transportsAddr2))
        .newClientTransport(addr2, AUTHORITY, USER_AGENT);
    // Fail this one too
    assertNoCallbackInvoke();
    transports.poll().listener.transportShutdown(Status.RESOURCE_EXHAUSTED);
    // All addresses have failed again. Delayed transport will be in back-off interval.
    assertExactCallbackInvokes("onStateChange:" + RESOURCE_EXHAUSTED_STATE);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, internalSubchannel.getState());
    // Second back-off interval begins
    verify(mockBackoffPolicy1, times(++backoff1Consulted)).nextBackoffNanos();
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();

    // Fifth attempt for the first address, thus controlled by the second back-off interval.
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, internalSubchannel.getState());
    fakeClock.forwardNanos(99);
    verify(mockTransportFactory, times(transportsAddr1))
        .newClientTransport(addr1, AUTHORITY, USER_AGENT);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, internalSubchannel.getState());
    assertNoCallbackInvoke();
    fakeClock.forwardNanos(1);
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());
    verify(mockTransportFactory, times(++transportsAddr1))
        .newClientTransport(addr1, AUTHORITY, USER_AGENT);
    // Let it through
    assertNoCallbackInvoke();
    transports.peek().listener.transportReady();
    assertExactCallbackInvokes("onStateChange:READY");
    assertEquals(ConnectivityState.READY, internalSubchannel.getState());

    assertSame(transports.peek().transport, internalSubchannel.obtainActiveTransport());
    // Then close it.
    assertNoCallbackInvoke();
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertExactCallbackInvokes("onStateChange:IDLE");
    assertEquals(ConnectivityState.IDLE, internalSubchannel.getState());

    // First attempt after a successful connection. Old back-off policy should be ignored, but there
    // is not yet a need for a new one. Start from the first address.
    assertNull(internalSubchannel.obtainActiveTransport());
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    verify(mockTransportFactory, times(++transportsAddr1))
        .newClientTransport(addr1, AUTHORITY, USER_AGENT);
    // Fail the transport
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());

    // Second attempt will start immediately. Still no new back-off policy.
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    verify(mockTransportFactory, times(++transportsAddr2))
        .newClientTransport(addr2, AUTHORITY, USER_AGENT);
    // Fail this one too
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    // All addresses have failed. Enter TRANSIENT_FAILURE. Back-off in effect.
    assertExactCallbackInvokes("onStateChange:" + UNAVAILABLE_STATE);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, internalSubchannel.getState());
    // Back-off reset and first back-off interval begins
    verify(mockBackoffPolicy2, times(++backoff2Consulted)).nextBackoffNanos();
    verify(mockBackoffPolicyProvider, times(++backoffReset)).get();

    // Third attempt is the first address, thus controlled by the first back-off interval.
    fakeClock.forwardNanos(9);
    verify(mockTransportFactory, times(transportsAddr1))
        .newClientTransport(addr1, AUTHORITY, USER_AGENT);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, internalSubchannel.getState());
    assertNoCallbackInvoke();
    fakeClock.forwardNanos(1);
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());
    verify(mockTransportFactory, times(++transportsAddr1))
        .newClientTransport(addr1, AUTHORITY, USER_AGENT);

    // Final checks on invocations on back-off policies
    verify(mockBackoffPolicy1, times(backoff1Consulted)).nextBackoffNanos();
    verify(mockBackoffPolicy2, times(backoff2Consulted)).nextBackoffNanos();
    verify(mockBackoffPolicy3, times(backoff3Consulted)).nextBackoffNanos();
  }

  @Test public void updateAddresses_intersecting_ready() {
    SocketAddress addr1 = mock(SocketAddress.class);
    SocketAddress addr2 = mock(SocketAddress.class);
    SocketAddress addr3 = mock(SocketAddress.class);
    createInternalSubchannel(addr1, addr2);
    assertEquals(ConnectivityState.IDLE, internalSubchannel.getState());

    // First address fails
    assertNull(internalSubchannel.obtainActiveTransport());
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockTransportFactory).newClientTransport(addr1, AUTHORITY, USER_AGENT);
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());

    // Second address connects
    verify(mockTransportFactory).newClientTransport(addr2, AUTHORITY, USER_AGENT);
    transports.peek().listener.transportReady();
    assertExactCallbackInvokes("onStateChange:READY");
    assertEquals(ConnectivityState.READY, internalSubchannel.getState());

    // Update addresses
    internalSubchannel.updateAddresses(new EquivalentAddressGroup(Arrays.asList(addr2, addr3)));
    assertNoCallbackInvoke();
    assertEquals(ConnectivityState.READY, internalSubchannel.getState());
    verify(transports.peek().transport, never()).shutdown();
    verify(transports.peek().transport, never()).shutdownNow(any(Status.class));

    // And new addresses chosen when re-connecting
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertExactCallbackInvokes("onStateChange:IDLE");

    assertNull(internalSubchannel.obtainActiveTransport());
    assertEquals(0, fakeClock.numPendingTasks());
    verify(mockTransportFactory, times(2)).newClientTransport(addr2, AUTHORITY, USER_AGENT);
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    verify(mockTransportFactory).newClientTransport(addr3, AUTHORITY, USER_AGENT);
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    verifyNoMoreInteractions(mockTransportFactory);

    fakeClock.forwardNanos(10); // Drain retry, but don't care about result
  }

  @Test public void updateAddresses_intersecting_connecting() {
    SocketAddress addr1 = mock(SocketAddress.class);
    SocketAddress addr2 = mock(SocketAddress.class);
    SocketAddress addr3 = mock(SocketAddress.class);
    createInternalSubchannel(addr1, addr2);
    assertEquals(ConnectivityState.IDLE, internalSubchannel.getState());

    // First address fails
    assertNull(internalSubchannel.obtainActiveTransport());
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockTransportFactory).newClientTransport(addr1, AUTHORITY, USER_AGENT);
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());

    // Second address connecting
    verify(mockTransportFactory).newClientTransport(addr2, AUTHORITY, USER_AGENT);
    assertNoCallbackInvoke();
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());

    // Update addresses
    internalSubchannel.updateAddresses(new EquivalentAddressGroup(Arrays.asList(addr2, addr3)));
    assertNoCallbackInvoke();
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());
    verify(transports.peek().transport, never()).shutdown();
    verify(transports.peek().transport, never()).shutdownNow(any(Status.class));

    // And new addresses chosen when re-connecting
    transports.peek().listener.transportReady();
    assertExactCallbackInvokes("onStateChange:READY");
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertExactCallbackInvokes("onStateChange:IDLE");

    assertNull(internalSubchannel.obtainActiveTransport());
    assertEquals(0, fakeClock.numPendingTasks());
    verify(mockTransportFactory, times(2)).newClientTransport(addr2, AUTHORITY, USER_AGENT);
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    verify(mockTransportFactory).newClientTransport(addr3, AUTHORITY, USER_AGENT);
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    verifyNoMoreInteractions(mockTransportFactory);

    fakeClock.forwardNanos(10); // Drain retry, but don't care about result
  }

  @Test public void updateAddresses_disjoint_idle() {
    SocketAddress addr1 = mock(SocketAddress.class);
    SocketAddress addr2 = mock(SocketAddress.class);

    createInternalSubchannel(addr1);
    internalSubchannel.updateAddresses(new EquivalentAddressGroup(addr2));

    // Nothing happened on address update
    verify(mockTransportFactory, never()).newClientTransport(addr1, AUTHORITY, USER_AGENT);
    verify(mockTransportFactory, never()).newClientTransport(addr2, AUTHORITY, USER_AGENT);
    verifyNoMoreInteractions(mockTransportFactory);
    assertNoCallbackInvoke();
    assertEquals(ConnectivityState.IDLE, internalSubchannel.getState());

    // But new address chosen when connecting
    assertNull(internalSubchannel.obtainActiveTransport());
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockTransportFactory).newClientTransport(addr2, AUTHORITY, USER_AGENT);

    // And no other addresses attempted
    assertEquals(0, fakeClock.numPendingTasks());
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertExactCallbackInvokes("onStateChange:" + UNAVAILABLE_STATE);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, internalSubchannel.getState());
    verifyNoMoreInteractions(mockTransportFactory);

    fakeClock.forwardNanos(10); // Drain retry, but don't care about result
  }

  @Test public void updateAddresses_disjoint_ready() {
    SocketAddress addr1 = mock(SocketAddress.class);
    SocketAddress addr2 = mock(SocketAddress.class);
    SocketAddress addr3 = mock(SocketAddress.class);
    SocketAddress addr4 = mock(SocketAddress.class);
    createInternalSubchannel(addr1, addr2);
    assertEquals(ConnectivityState.IDLE, internalSubchannel.getState());

    // First address fails
    assertNull(internalSubchannel.obtainActiveTransport());
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockTransportFactory).newClientTransport(addr1, AUTHORITY, USER_AGENT);
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());

    // Second address connects
    verify(mockTransportFactory).newClientTransport(addr2, AUTHORITY, USER_AGENT);
    transports.peek().listener.transportReady();
    assertExactCallbackInvokes("onStateChange:READY");
    assertEquals(ConnectivityState.READY, internalSubchannel.getState());

    // Update addresses
    internalSubchannel.updateAddresses(new EquivalentAddressGroup(Arrays.asList(addr3, addr4)));
    assertExactCallbackInvokes("onStateChange:IDLE");
    assertEquals(ConnectivityState.IDLE, internalSubchannel.getState());
    verify(transports.peek().transport).shutdown();

    // And new addresses chosen when re-connecting
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertNoCallbackInvoke();
    assertEquals(ConnectivityState.IDLE, internalSubchannel.getState());

    assertNull(internalSubchannel.obtainActiveTransport());
    assertEquals(0, fakeClock.numPendingTasks());
    verify(mockTransportFactory).newClientTransport(addr3, AUTHORITY, USER_AGENT);
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    verify(mockTransportFactory).newClientTransport(addr4, AUTHORITY, USER_AGENT);
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
    assertEquals(ConnectivityState.IDLE, internalSubchannel.getState());

    // First address fails
    assertNull(internalSubchannel.obtainActiveTransport());
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockTransportFactory).newClientTransport(addr1, AUTHORITY, USER_AGENT);
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());

    // Second address connecting
    verify(mockTransportFactory).newClientTransport(addr2, AUTHORITY, USER_AGENT);
    assertNoCallbackInvoke();
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());

    // Update addresses
    internalSubchannel.updateAddresses(new EquivalentAddressGroup(Arrays.asList(addr3, addr4)));
    assertNoCallbackInvoke();
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());

    // And new addresses chosen immediately
    verify(transports.poll().transport).shutdown();
    assertNoCallbackInvoke();
    assertEquals(ConnectivityState.CONNECTING, internalSubchannel.getState());

    assertNull(internalSubchannel.obtainActiveTransport());
    assertEquals(0, fakeClock.numPendingTasks());
    verify(mockTransportFactory).newClientTransport(addr3, AUTHORITY, USER_AGENT);
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    verify(mockTransportFactory).newClientTransport(addr4, AUTHORITY, USER_AGENT);
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
        .newClientTransport(addr, AUTHORITY, USER_AGENT);

    // First attempt
    internalSubchannel.obtainActiveTransport();
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockTransportFactory, times(++transportsCreated))
        .newClientTransport(addr, AUTHORITY, USER_AGENT);

    // Fail this one
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertExactCallbackInvokes("onStateChange:" + UNAVAILABLE_STATE);

    // Will always reconnect after back-off
    fakeClock.forwardNanos(10);
    assertExactCallbackInvokes("onStateChange:CONNECTING");
    verify(mockTransportFactory, times(++transportsCreated))
        .newClientTransport(addr, AUTHORITY, USER_AGENT);

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
        .newClientTransport(addr, AUTHORITY, USER_AGENT);
  }

  @Test
  public void shutdownWhenReady() throws Exception {
    SocketAddress addr = mock(SocketAddress.class);
    createInternalSubchannel(addr);

    internalSubchannel.obtainActiveTransport();
    MockClientTransportInfo transportInfo = transports.poll();
    transportInfo.listener.transportReady();
    assertExactCallbackInvokes("onStateChange:CONNECTING", "onStateChange:READY");

    internalSubchannel.shutdown();
    verify(transportInfo.transport).shutdown();
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
    verify(mockTransportFactory).newClientTransport(addr, AUTHORITY, USER_AGENT);

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
    internalSubchannel.shutdown();
    assertTrue(reconnectTask.isCancelled());
    // InternalSubchannel terminated promptly.
    assertExactCallbackInvokes("onStateChange:SHUTDOWN", "onTerminated");

    // Simulate a race between reconnectTask cancellation and execution -- the task runs anyway.
    // This should not lead to the creation of a new transport.
    reconnectTask.command.run();

    // Futher call to obtainActiveTransport() is no-op.
    assertNull(internalSubchannel.obtainActiveTransport());
    assertEquals(ConnectivityState.SHUTDOWN, internalSubchannel.getState());
    assertNoCallbackInvoke();

    // No more transports will be created.
    fakeClock.forwardNanos(10000);
    assertEquals(ConnectivityState.SHUTDOWN, internalSubchannel.getState());
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
    internalSubchannel.shutdown();
    assertExactCallbackInvokes("onStateChange:SHUTDOWN");

    // The transport should've been shut down even though it's not the active transport yet.
    verify(transportInfo.transport).shutdown();
    transportInfo.listener.transportShutdown(Status.UNAVAILABLE);
    assertNoCallbackInvoke();
    transportInfo.listener.transportTerminated();
    assertExactCallbackInvokes("onTerminated");
    assertEquals(ConnectivityState.SHUTDOWN, internalSubchannel.getState());
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

    internalSubchannel.shutdown();
    assertExactCallbackInvokes("onStateChange:SHUTDOWN", "onTerminated");
    assertEquals(ConnectivityState.SHUTDOWN, internalSubchannel.getState());
    assertNull(internalSubchannel.obtainActiveTransport());
    verify(mockTransportFactory, times(0)).newClientTransport(addr, AUTHORITY, USER_AGENT);
    assertNoCallbackInvoke();
    assertEquals(ConnectivityState.SHUTDOWN, internalSubchannel.getState());
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

  private void createInternalSubchannel(SocketAddress ... addrs) {
    addressGroup = new EquivalentAddressGroup(Arrays.asList(addrs));
    internalSubchannel = new InternalSubchannel(addressGroup, AUTHORITY, USER_AGENT,
        mockBackoffPolicyProvider, mockTransportFactory, fakeClock.getScheduledExecutorService(),
        fakeClock.getStopwatchSupplier(), channelExecutor, mockInternalSubchannelCallback);
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
}
