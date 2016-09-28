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
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.atLeast;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import io.grpc.CallOptions;
import io.grpc.ConnectivityState;
import io.grpc.EquivalentAddressGroup;
import io.grpc.IntegerMarshaller;
import io.grpc.LoadBalancer;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.StringMarshaller;
import io.grpc.internal.TestUtils.MockClientTransportInfo;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.net.SocketAddress;
import java.util.Arrays;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.Executor;

/**
 * Unit tests for {@link TransportSet}.
 *
 * <p>It only tests the logic that is not covered by {@link ManagedChannelImplTransportManagerTest}.
 */
@RunWith(JUnit4.class)
public class TransportSetTest {

  private static final String AUTHORITY = "fakeauthority";
  private static final String USER_AGENT = "mosaic";

  private FakeClock fakeClock;
  private FakeClock fakeExecutor;

  @Mock private LoadBalancer<ClientTransport> mockLoadBalancer;
  @Mock private BackoffPolicy mockBackoffPolicy1;
  @Mock private BackoffPolicy mockBackoffPolicy2;
  @Mock private BackoffPolicy mockBackoffPolicy3;
  @Mock private BackoffPolicy.Provider mockBackoffPolicyProvider;
  @Mock private ClientTransportFactory mockTransportFactory;
  @Mock private TransportSet.Callback mockTransportSetCallback;
  @Mock private ClientStreamListener mockStreamListener;

  private final MethodDescriptor<String, Integer> method = MethodDescriptor.create(
      MethodDescriptor.MethodType.UNKNOWN, "/service/method",
      new StringMarshaller(), new IntegerMarshaller());
  private final Metadata headers = new Metadata();
  private final CallOptions waitForReadyCallOptions = CallOptions.DEFAULT.withWaitForReady();
  private final CallOptions failFastCallOptions = CallOptions.DEFAULT;

  private TransportSet transportSet;
  private EquivalentAddressGroup addressGroup;
  private BlockingQueue<MockClientTransportInfo> transports;

  @Before public void setUp() {
    MockitoAnnotations.initMocks(this);
    fakeClock = new FakeClock();
    fakeExecutor = new FakeClock();

    when(mockBackoffPolicyProvider.get())
        .thenReturn(mockBackoffPolicy1, mockBackoffPolicy2, mockBackoffPolicy3);
    when(mockBackoffPolicy1.nextBackoffMillis()).thenReturn(10L, 100L);
    when(mockBackoffPolicy2.nextBackoffMillis()).thenReturn(10L, 100L);
    when(mockBackoffPolicy3.nextBackoffMillis()).thenReturn(10L, 100L);
    transports = TestUtils.captureTransports(mockTransportFactory);
  }

  @After public void noMorePendingTasks() {
    assertEquals(0, fakeClock.numPendingTasks());
    assertEquals(0, fakeExecutor.numPendingTasks());
  }

  @Test public void singleAddressReconnect() {
    SocketAddress addr = mock(SocketAddress.class);
    createTransportSet(addr);
    assertEquals(ConnectivityState.IDLE, transportSet.getState(false));

    // Invocation counters
    int transportsCreated = 0;
    int backoff1Consulted = 0;
    int backoff2Consulted = 0;
    int backoffReset = 0;
    int onAllAddressesFailed = 0;

    // First attempt
    transportSet.obtainActiveTransport().newStream(method, new Metadata(), waitForReadyCallOptions);
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    verify(mockTransportFactory, times(++transportsCreated))
        .newClientTransport(addr, AUTHORITY, USER_AGENT);

    // Fail this one
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, transportSet.getState(false));
    verify(mockTransportSetCallback, times(++onAllAddressesFailed)).onAllAddressesFailed();
    // Backoff reset and using first back-off value interval
    verify(mockBackoffPolicy1, times(++backoff1Consulted)).nextBackoffMillis();
    verify(mockBackoffPolicyProvider, times(++backoffReset)).get();

    // Second attempt
    // Transport creation doesn't happen until time is due
    fakeClock.forwardMillis(9);
    verify(mockTransportFactory, times(transportsCreated))
        .newClientTransport(addr, AUTHORITY, USER_AGENT);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, transportSet.getState(false));
    fakeClock.forwardMillis(1);
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    verify(mockTransportFactory, times(++transportsCreated))
        .newClientTransport(addr, AUTHORITY, USER_AGENT);
    // Fail this one too
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, transportSet.getState(false));
    verify(mockTransportSetCallback, times(++onAllAddressesFailed)).onAllAddressesFailed();
    // Second back-off interval
    verify(mockBackoffPolicy1, times(++backoff1Consulted)).nextBackoffMillis();
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();

    // Third attempt
    // Transport creation doesn't happen until time is due
    fakeClock.forwardMillis(99);
    verify(mockTransportFactory, times(transportsCreated))
        .newClientTransport(addr, AUTHORITY, USER_AGENT);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, transportSet.getState(false));
    fakeClock.forwardMillis(1);
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    verify(mockTransportFactory, times(++transportsCreated))
        .newClientTransport(addr, AUTHORITY, USER_AGENT);
    // Let this one succeed
    transports.peek().listener.transportReady();
    assertEquals(ConnectivityState.READY, transportSet.getState(false));
    fakeClock.runDueTasks();
    verify(mockTransportSetCallback, never()).onConnectionClosedByServer(any(Status.class));
    // And close it
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.IDLE, transportSet.getState(false));
    verify(mockTransportSetCallback).onConnectionClosedByServer(same(Status.UNAVAILABLE));
    verify(mockTransportSetCallback, times(onAllAddressesFailed)).onAllAddressesFailed();

    // Back-off is reset, and the next attempt will happen immediately
    transportSet.obtainActiveTransport();
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    verify(mockTransportFactory, times(++transportsCreated))
        .newClientTransport(addr, AUTHORITY, USER_AGENT);

    // Final checks for consultations on back-off policies
    verify(mockBackoffPolicy1, times(backoff1Consulted)).nextBackoffMillis();
    verify(mockBackoffPolicy2, times(backoff2Consulted)).nextBackoffMillis();
    verify(mockTransportSetCallback, atLeast(0)).onInUse(transportSet);
    verify(mockTransportSetCallback, atLeast(0)).onNotInUse(transportSet);
    verifyNoMoreInteractions(mockTransportSetCallback);
    fakeExecutor.runDueTasks(); // Drain new 'real' stream creation; not important to this test.
  }

  @Test public void twoAddressesReconnect() {
    SocketAddress addr1 = mock(SocketAddress.class);
    SocketAddress addr2 = mock(SocketAddress.class);
    createTransportSet(addr1, addr2);
    assertEquals(ConnectivityState.IDLE, transportSet.getState(false));
    // Invocation counters
    int transportsAddr1 = 0;
    int transportsAddr2 = 0;
    int backoff1Consulted = 0;
    int backoff2Consulted = 0;
    int backoff3Consulted = 0;
    int backoffReset = 0;
    int onAllAddressesFailed = 0;

    // First attempt
    DelayedClientTransport delayedTransport1 =
        (DelayedClientTransport) transportSet.obtainActiveTransport();
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    verify(mockTransportFactory, times(++transportsAddr1))
        .newClientTransport(addr1, AUTHORITY, USER_AGENT);
    delayedTransport1.newStream(method, new Metadata(), waitForReadyCallOptions);
    // Let this one fail without success
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    assertNull(delayedTransport1.getTransportSupplier());
    verify(mockTransportSetCallback, times(onAllAddressesFailed)).onAllAddressesFailed();

    // Second attempt will start immediately. Still no back-off policy.
    DelayedClientTransport delayedTransport2 =
        (DelayedClientTransport) transportSet.obtainActiveTransport();
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    assertSame(delayedTransport1, delayedTransport2);
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    verify(mockTransportFactory, times(++transportsAddr2))
        .newClientTransport(addr2, AUTHORITY, USER_AGENT);
    // Fail this one too
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    // All addresses have failed. Delayed transport will be in back-off interval.
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, transportSet.getState(false));
    assertNull(delayedTransport2.getTransportSupplier());
    assertTrue(delayedTransport2.isInBackoffPeriod());
    verify(mockTransportSetCallback, times(++onAllAddressesFailed)).onAllAddressesFailed();
    // Backoff reset and first back-off interval begins
    verify(mockBackoffPolicy1, times(++backoff1Consulted)).nextBackoffMillis();
    verify(mockBackoffPolicyProvider, times(++backoffReset)).get();

    // Third attempt is the first address, thus controlled by the first back-off interval.
    DelayedClientTransport delayedTransport3 =
        (DelayedClientTransport) transportSet.obtainActiveTransport();
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, transportSet.getState(false));
    assertSame(delayedTransport2, delayedTransport3);
    fakeClock.forwardMillis(9);
    verify(mockTransportFactory, times(transportsAddr1))
        .newClientTransport(addr1, AUTHORITY, USER_AGENT);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, transportSet.getState(false));
    fakeClock.forwardMillis(1);
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    verify(mockTransportFactory, times(++transportsAddr1))
        .newClientTransport(addr1, AUTHORITY, USER_AGENT);
    // Fail this one too
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    assertNull(delayedTransport3.getTransportSupplier());
    verify(mockTransportSetCallback, times(onAllAddressesFailed)).onAllAddressesFailed();

    // Forth attempt will start immediately. Keep back-off policy.
    DelayedClientTransport delayedTransport4 =
        (DelayedClientTransport) transportSet.obtainActiveTransport();
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    assertSame(delayedTransport3, delayedTransport4);
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    verify(mockTransportFactory, times(++transportsAddr2))
        .newClientTransport(addr2, AUTHORITY, USER_AGENT);
    // Fail this one too
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    // All addresses have failed again. Delayed transport will be in back-off interval.
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, transportSet.getState(false));
    assertNull(delayedTransport4.getTransportSupplier());
    assertTrue(delayedTransport4.isInBackoffPeriod());
    verify(mockTransportSetCallback, times(++onAllAddressesFailed)).onAllAddressesFailed();
    // Second back-off interval begins
    verify(mockBackoffPolicy1, times(++backoff1Consulted)).nextBackoffMillis();
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();

    // Fifth attempt for the first address, thus controlled by the second back-off interval.
    DelayedClientTransport delayedTransport5 =
        (DelayedClientTransport) transportSet.obtainActiveTransport();
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, transportSet.getState(false));
    assertSame(delayedTransport4, delayedTransport5);
    fakeClock.forwardMillis(99);
    verify(mockTransportFactory, times(transportsAddr1))
        .newClientTransport(addr1, AUTHORITY, USER_AGENT);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, transportSet.getState(false));
    fakeClock.forwardMillis(1);
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    verify(mockTransportFactory, times(++transportsAddr1))
        .newClientTransport(addr1, AUTHORITY, USER_AGENT);
    // Let it through
    transports.peek().listener.transportReady();
    assertEquals(ConnectivityState.READY, transportSet.getState(false));
    // Delayed transport will see the connected transport.
    assertSame(transports.peek().transport, delayedTransport5.getTransportSupplier().get());
    verify(mockTransportSetCallback, never()).onConnectionClosedByServer(any(Status.class));
    // Then close it.
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.IDLE, transportSet.getState(false));
    verify(mockTransportSetCallback).onConnectionClosedByServer(same(Status.UNAVAILABLE));
    verify(mockTransportSetCallback, times(onAllAddressesFailed)).onAllAddressesFailed();

    // First attempt after a successful connection. Old back-off policy should be ignored, but there
    // is not yet a need for a new one. Start from the first address.
    DelayedClientTransport delayedTransport6 =
        (DelayedClientTransport) transportSet.obtainActiveTransport();
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    assertNotSame(delayedTransport5, delayedTransport6);
    delayedTransport6.newStream(method, headers, waitForReadyCallOptions);
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    verify(mockTransportFactory, times(++transportsAddr1))
        .newClientTransport(addr1, AUTHORITY, USER_AGENT);
    // Fail the transport
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    assertNull(delayedTransport6.getTransportSupplier());
    verify(mockTransportSetCallback, times(onAllAddressesFailed)).onAllAddressesFailed();

    // Second attempt will start immediately. Still no new back-off policy.
    DelayedClientTransport delayedTransport7 =
        (DelayedClientTransport) transportSet.obtainActiveTransport();
    assertSame(delayedTransport6, delayedTransport7);
    verify(mockBackoffPolicyProvider, times(backoffReset)).get();
    verify(mockTransportFactory, times(++transportsAddr2))
        .newClientTransport(addr2, AUTHORITY, USER_AGENT);
    // Fail this one too
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    // All addresses have failed. Delayed transport will be in back-off interval.
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, transportSet.getState(false));
    assertNull(delayedTransport7.getTransportSupplier());
    assertTrue(delayedTransport7.isInBackoffPeriod());
    verify(mockTransportSetCallback, times(++onAllAddressesFailed)).onAllAddressesFailed();
    // Back-off reset and first back-off interval begins
    verify(mockBackoffPolicy2, times(++backoff2Consulted)).nextBackoffMillis();
    verify(mockBackoffPolicyProvider, times(++backoffReset)).get();

    // Third attempt is the first address, thus controlled by the first back-off interval.
    DelayedClientTransport delayedTransport8 =
        (DelayedClientTransport) transportSet.obtainActiveTransport();
    assertSame(delayedTransport7, delayedTransport8);
    fakeClock.forwardMillis(9);
    verify(mockTransportFactory, times(transportsAddr1))
        .newClientTransport(addr1, AUTHORITY, USER_AGENT);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, transportSet.getState(false));
    fakeClock.forwardMillis(1);
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    verify(mockTransportFactory, times(++transportsAddr1))
        .newClientTransport(addr1, AUTHORITY, USER_AGENT);

    // Final checks on invocations on back-off policies
    verify(mockBackoffPolicy1, times(backoff1Consulted)).nextBackoffMillis();
    verify(mockBackoffPolicy2, times(backoff2Consulted)).nextBackoffMillis();
    verify(mockBackoffPolicy3, times(backoff3Consulted)).nextBackoffMillis();
    verify(mockTransportSetCallback, atLeast(0)).onInUse(transportSet);
    verify(mockTransportSetCallback, atLeast(0)).onNotInUse(transportSet);
    verifyNoMoreInteractions(mockTransportSetCallback);
    fakeExecutor.runDueTasks(); // Drain new 'real' stream creation; not important to this test.
  }

  @Test
  public void verifyFailFastAndNonFailFastBehaviors() {
    int pendingStreamsCount = 0;
    int failFastPendingStreamsCount = 0;

    final SocketAddress addr1 = mock(SocketAddress.class);
    final SocketAddress addr2 = mock(SocketAddress.class);
    createTransportSet(addr1, addr2);

    final DelayedClientTransport delayedTransport =
        (DelayedClientTransport) transportSet.obtainActiveTransport();
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    assertFalse(delayedTransport.isInBackoffPeriod());

    // Create a new fail fast stream.
    ClientStream ffStream = delayedTransport.newStream(method, headers, failFastCallOptions);
    ffStream.start(mockStreamListener);
    // Verify it is queued.
    assertEquals(++pendingStreamsCount, delayedTransport.getPendingStreamsCount());
    failFastPendingStreamsCount++;
    // Create a new non fail fast stream.
    delayedTransport.newStream(method, headers, waitForReadyCallOptions);
    // Verify it is queued.
    assertEquals(++pendingStreamsCount, delayedTransport.getPendingStreamsCount());

    // Let this 1st address fail without success.
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    assertFalse(delayedTransport.isInBackoffPeriod());
    // Verify pending streams still in queue.
    assertEquals(pendingStreamsCount, delayedTransport.getPendingStreamsCount());

    // Create a new fail fast stream.
    delayedTransport.newStream(method, headers, failFastCallOptions);
    // Verify it is queued.
    assertEquals(++pendingStreamsCount, delayedTransport.getPendingStreamsCount());
    failFastPendingStreamsCount++;
    // Create a new non fail fast stream
    delayedTransport.newStream(method, headers, waitForReadyCallOptions);
    // Verify it is queued.
    assertEquals(++pendingStreamsCount, delayedTransport.getPendingStreamsCount());

    // Let this 2nd address fail without success.
    Status failureStatus = Status.UNAVAILABLE.withDescription("some unique failure");
    transports.poll().listener.transportShutdown(failureStatus);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, transportSet.getState(false));
    assertTrue(delayedTransport.isInBackoffPeriod());
    // Fail fast pending streams should be cleared
    assertEquals(pendingStreamsCount - failFastPendingStreamsCount,
        delayedTransport.getPendingStreamsCount());
    pendingStreamsCount -= failFastPendingStreamsCount;
    failFastPendingStreamsCount = 0;
    fakeExecutor.runDueTasks();
    verify(mockStreamListener).closed(same(failureStatus), any(Metadata.class));

    // Create a new fail fast stream.
    delayedTransport.newStream(method, headers, failFastCallOptions);
    // Verify it is not queued.
    assertEquals(pendingStreamsCount, delayedTransport.getPendingStreamsCount());
    // Create a new non fail fast stream
    delayedTransport.newStream(method, headers, waitForReadyCallOptions);
    // Verify it is queued.
    assertEquals(++pendingStreamsCount, delayedTransport.getPendingStreamsCount());

    fakeClock.forwardMillis(10);
    // Now back-off is over
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    assertFalse(delayedTransport.isInBackoffPeriod());

    // Create a new fail fast stream.
    delayedTransport.newStream(method, headers, failFastCallOptions);
    // Verify it is queued.
    assertEquals(++pendingStreamsCount, delayedTransport.getPendingStreamsCount());
    failFastPendingStreamsCount++;
    assertEquals(1, failFastPendingStreamsCount);

    fakeExecutor.runDueTasks(); // Drain new 'real' stream creation; not important to this test.
  }

  @Test
  public void connectIsLazy() {
    SocketAddress addr = mock(SocketAddress.class);
    createTransportSet(addr);

    // Invocation counters
    int transportsCreated = 0;

    // Won't connect until requested
    verify(mockTransportFactory, times(transportsCreated))
        .newClientTransport(addr, AUTHORITY, USER_AGENT);

    // First attempt
    transportSet.obtainActiveTransport().newStream(method, new Metadata());
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    verify(mockTransportFactory, times(++transportsCreated))
        .newClientTransport(addr, AUTHORITY, USER_AGENT);

    // Fail this one
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, transportSet.getState(false));

    // Will always reconnect after back-off
    fakeClock.forwardMillis(10);
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    verify(mockTransportFactory, times(++transportsCreated))
        .newClientTransport(addr, AUTHORITY, USER_AGENT);

    // Make this one proceed
    transports.peek().listener.transportReady();
    assertEquals(ConnectivityState.READY, transportSet.getState(false));
    // Then go-away
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.IDLE, transportSet.getState(false));

    // Request immediately
    transportSet.obtainActiveTransport().newStream(method, new Metadata(), waitForReadyCallOptions);
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    verify(mockTransportFactory, times(++transportsCreated))
        .newClientTransport(addr, AUTHORITY, USER_AGENT);
    fakeExecutor.runDueTasks(); // Drain new 'real' stream creation; not important to this test.
  }

  @Test
  public void shutdownBeforeTransportCreatedWithPendingStream() throws Exception {
    SocketAddress addr = mock(SocketAddress.class);
    createTransportSet(addr);

    // First transport is created immediately
    ClientTransport pick = transportSet.obtainActiveTransport();
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    verify(mockTransportFactory).newClientTransport(addr, AUTHORITY, USER_AGENT);
    assertNotNull(pick);
    // Fail this one
    MockClientTransportInfo transportInfo = transports.poll();
    transportInfo.listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, transportSet.getState(false));
    transportInfo.listener.transportTerminated();

    // Second transport will wait for back-off
    pick = transportSet.obtainActiveTransport();
    assertTrue(pick instanceof DelayedClientTransport);
    // Start a stream, which will be pending in the delayed transport
    ClientStream pendingStream = pick.newStream(method, headers, waitForReadyCallOptions);
    pendingStream.start(mockStreamListener);

    // Shut down TransportSet before the transport is created. Further call to
    // obtainActiveTransport() gets failing transports
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, transportSet.getState(false));
    transportSet.shutdown();
    assertEquals(ConnectivityState.SHUTDOWN, transportSet.getState(false));
    pick = transportSet.obtainActiveTransport();
    assertNotNull(pick);
    assertTrue(pick instanceof FailingClientTransport);
    verify(mockTransportFactory).newClientTransport(addr, AUTHORITY, USER_AGENT);

    // Reconnect will eventually happen, even though TransportSet has been shut down
    fakeClock.forwardMillis(10);
    assertEquals(ConnectivityState.SHUTDOWN, transportSet.getState(false));
    verify(mockTransportFactory, times(2)).newClientTransport(addr, AUTHORITY, USER_AGENT);
    // The pending stream will be started on this newly started transport after it's ready.
    // The transport is shut down by TransportSet right after the stream is created.
    transportInfo = transports.poll();
    verify(transportInfo.transport, times(0)).shutdown();
    assertEquals(0, fakeExecutor.numPendingTasks());
    transportInfo.listener.transportReady();
    assertEquals(ConnectivityState.SHUTDOWN, transportSet.getState(false));
    verify(transportInfo.transport, times(0)).newStream(
        any(MethodDescriptor.class), any(Metadata.class));
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(transportInfo.transport).newStream(same(method), same(headers),
        same(waitForReadyCallOptions));
    verify(transportInfo.transport).shutdown();
    transportInfo.listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.SHUTDOWN, transportSet.getState(false));
    verify(mockTransportSetCallback, never()).onTerminated(any(TransportSet.class));
    // Terminating the transport will let TransportSet to be terminated.
    transportInfo.listener.transportTerminated();
    assertEquals(ConnectivityState.SHUTDOWN, transportSet.getState(false));
    verify(mockTransportSetCallback).onTerminated(transportSet);

    // No more transports will be created.
    fakeClock.forwardMillis(10000);
    assertEquals(ConnectivityState.SHUTDOWN, transportSet.getState(false));
    verifyNoMoreInteractions(mockTransportFactory);
    assertEquals(0, transports.size());
  }

  @Test
  public void shutdownBeforeTransportCreatedWithoutPendingStream() throws Exception {
    SocketAddress addr = mock(SocketAddress.class);
    createTransportSet(addr);

    // First transport is created immediately
    ClientTransport pick = transportSet.obtainActiveTransport();
    verify(mockTransportFactory).newClientTransport(addr, AUTHORITY, USER_AGENT);
    assertNotNull(pick);
    // Fail this one
    MockClientTransportInfo transportInfo = transports.poll();
    transportInfo.listener.transportShutdown(Status.UNAVAILABLE);
    transportInfo.listener.transportTerminated();

    // Second transport will wait for back-off
    pick = transportSet.obtainActiveTransport();
    assertTrue(pick instanceof DelayedClientTransport);

    // Shut down TransportSet before the transport is created. Futher call to
    // obtainActiveTransport() gets failing transports
    transportSet.shutdown();
    assertEquals(ConnectivityState.SHUTDOWN, transportSet.getState(false));
    pick = transportSet.obtainActiveTransport();
    assertEquals(ConnectivityState.SHUTDOWN, transportSet.getState(false));
    assertNotNull(pick);
    assertTrue(pick instanceof FailingClientTransport);

    // TransportSet terminated promptly.
    verify(mockTransportSetCallback).onTerminated(transportSet);
    assertEquals(ConnectivityState.SHUTDOWN, transportSet.getState(false));

    // No more transports will be created.
    fakeClock.forwardMillis(10000);
    assertEquals(ConnectivityState.SHUTDOWN, transportSet.getState(false));
    verifyNoMoreInteractions(mockTransportFactory);
    assertEquals(0, transports.size());
  }

  @Test
  public void shutdownBeforeTransportReady() throws Exception {
    SocketAddress addr = mock(SocketAddress.class);
    createTransportSet(addr);

    ClientTransport pick = transportSet.obtainActiveTransport();
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));
    MockClientTransportInfo transportInfo = transports.poll();
    assertNotSame(transportInfo.transport, pick);

    // Shutdown the TransportSet before the pending transport is ready
    transportSet.shutdown();
    assertEquals(ConnectivityState.SHUTDOWN, transportSet.getState(false));

    // The transport should've been shut down even though it's not the active transport yet.
    verify(transportInfo.transport).shutdown();
    transportInfo.listener.transportShutdown(Status.UNAVAILABLE);
    transportInfo.listener.transportTerminated();
    assertEquals(ConnectivityState.SHUTDOWN, transportSet.getState(false));
  }

  @Test
  public void obtainTransportAfterShutdown() throws Exception {
    SocketAddress addr = mock(SocketAddress.class);
    createTransportSet(addr);

    transportSet.shutdown();
    assertEquals(ConnectivityState.SHUTDOWN, transportSet.getState(false));
    ClientTransport pick = transportSet.obtainActiveTransport();
    assertNotNull(pick);
    verify(mockTransportFactory, times(0)).newClientTransport(addr, AUTHORITY, USER_AGENT);
    assertEquals(ConnectivityState.SHUTDOWN, transportSet.getState(false));
  }

  @Test
  public void requireConnectionThroughGetState() throws Exception {
    SocketAddress addr = mock(SocketAddress.class);
    createTransportSet(addr);

    assertEquals(ConnectivityState.IDLE, transportSet.getState(false));
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(true));
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(true));

    transportSet.obtainActiveTransport().newStream(method, new Metadata(), waitForReadyCallOptions);
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(true));

    // Fail it
    transports.peek().listener.transportShutdown(Status.UNAVAILABLE);
    // requireConnection == true doesn't skip the back-off
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, transportSet.getState(true));
    transports.poll().listener.transportTerminated();
    fakeClock.forwardMillis(9);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, transportSet.getState(true));
    fakeClock.forwardMillis(1);
    // Only when back-off is over, do we try to connect again
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(false));

    // Let it through and fail, thus a go-away
    transports.peek().listener.transportReady();
    transports.peek().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.IDLE, transportSet.getState(false));
    transports.poll().listener.transportTerminated();

    // Request for connecting again
    assertEquals(ConnectivityState.CONNECTING, transportSet.getState(true));
    // And fail it again
    transports.peek().listener.transportShutdown(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, transportSet.getState(true));

    // Shut it down
    transportSet.shutdown();
    assertEquals(ConnectivityState.SHUTDOWN, transportSet.getState(true));
    assertEquals(ConnectivityState.SHUTDOWN, transportSet.getState(false));
    assertFalse(transportSet.isTerminated());

    // Terminate it
    transports.poll().listener.transportTerminated();
    assertTrue(transportSet.isTerminated());
    assertEquals(ConnectivityState.SHUTDOWN, transportSet.getState(true));
    assertEquals(ConnectivityState.SHUTDOWN, transportSet.getState(false));

    fakeExecutor.runDueTasks(); // What tasks are scheduled is not important to this test.
  }

  @Test
  public void logId() {
    createTransportSet(mock(SocketAddress.class));
    assertEquals("TransportSet@" + Integer.toHexString(transportSet.hashCode()),
        transportSet.getLogId());
  }

  @Test
  public void inUseState() {
    SocketAddress addr = mock(SocketAddress.class);
    createTransportSet(addr);

    // Invocation counters
    int inUse = 0;
    int notInUse = 0;

    verify(mockTransportSetCallback, never()).onInUse(any(TransportSet.class));
    transportSet.obtainActiveTransport().newStream(method, new Metadata(), waitForReadyCallOptions);
    verify(mockTransportSetCallback, times(++inUse)).onInUse(transportSet);

    MockClientTransportInfo t0 = transports.poll();

    verify(mockTransportSetCallback, never()).onNotInUse(any(TransportSet.class));
    t0.listener.transportReady();
    // The race between delayed transport being terminated (thus not in-use) and
    // the real transport become in-use, caused a brief period of TransportSet not in-use.
    verify(mockTransportSetCallback, times(++notInUse)).onNotInUse(transportSet);
    // Delayed transport calls newStream() on the real transport in the executor
    fakeExecutor.runDueTasks();
    verify(t0.transport).newStream(
        same(method), any(Metadata.class), same(waitForReadyCallOptions));
    verify(mockTransportSetCallback, times(inUse)).onInUse(transportSet);
    t0.listener.transportInUse(true);
    verify(mockTransportSetCallback, times(++inUse)).onInUse(transportSet);

    t0.listener.transportInUse(false);
    verify(mockTransportSetCallback, times(++notInUse)).onNotInUse(transportSet);

    t0.listener.transportInUse(true);
    verify(mockTransportSetCallback, times(++inUse)).onInUse(transportSet);

    // Simulate that the server sends a go-away
    t0.listener.transportShutdown(Status.UNAVAILABLE);

    // Creates a new transport
    transportSet.obtainActiveTransport().newStream(method, new Metadata(), waitForReadyCallOptions);
    MockClientTransportInfo t1 = transports.poll();
    t1.listener.transportReady();
    // Delayed transport calls newStream() on the real transport in the executor
    fakeExecutor.runDueTasks();
    verify(t1.transport).newStream(
        same(method), any(Metadata.class), same(waitForReadyCallOptions));
    t1.listener.transportInUse(true);
    // No turbulance from the race mentioned eariler, because t0 has been in-use
    verify(mockTransportSetCallback, times(inUse)).onInUse(transportSet);
    verify(mockTransportSetCallback, times(notInUse)).onNotInUse(transportSet);

    // TransportSet is not in-use when both transports are not in-use.
    t1.listener.transportInUse(false);
    verify(mockTransportSetCallback, times(notInUse)).onNotInUse(transportSet);
    t0.listener.transportInUse(false);
    verify(mockTransportSetCallback, times(++notInUse)).onNotInUse(transportSet);
    verify(mockTransportSetCallback, times(inUse)).onInUse(transportSet);
  }

  @Test
  public void scheduleBackoff_DoNotScheduleEndOfBackoffIfAlreadyShutdown() {
    // Setup
    final boolean[] startBackoffAndShutdownAreCalled = {false};
    Executor executor = new Executor() {
      @Override
      public void execute(Runnable command) {
        if (command.getClass().getName().contains("FailTheFailFastPendingStreams")) {
          // shutdown during startBackoff
          transportSet.shutdown();
          startBackoffAndShutdownAreCalled[0] = true;
        }
        fakeExecutor.scheduledExecutorService.execute(command);
      }
    };
    SocketAddress addr = mock(SocketAddress.class);
    addressGroup = new EquivalentAddressGroup(Arrays.asList(addr));
    transportSet = new TransportSet(addressGroup, AUTHORITY, USER_AGENT, mockLoadBalancer,
        mockBackoffPolicyProvider, mockTransportFactory, fakeClock.scheduledExecutorService,
        fakeClock.stopwatchSupplier, executor, mockTransportSetCallback);

    // Attempt and fail, scheduleBackoff should be triggered,
    // and transportSet.shutdown should be triggered by setup
    transportSet.obtainActiveTransport().newStream(method, new Metadata(), waitForReadyCallOptions);
    transports.poll().listener.transportShutdown(Status.UNAVAILABLE);
    verify(mockTransportSetCallback, times(1)).onAllAddressesFailed();
    assertTrue(startBackoffAndShutdownAreCalled[0]);

    fakeExecutor.runDueTasks();
    // verify endOfBackoff not scheduled
    verify(mockBackoffPolicy1, never()).nextBackoffMillis();
  }

  private void createTransportSet(SocketAddress ... addrs) {
    addressGroup = new EquivalentAddressGroup(Arrays.asList(addrs));
    transportSet = new TransportSet(addressGroup, AUTHORITY, USER_AGENT, mockLoadBalancer,
        mockBackoffPolicyProvider, mockTransportFactory, fakeClock.scheduledExecutorService,
        fakeClock.stopwatchSupplier, fakeExecutor.scheduledExecutorService,
        mockTransportSetCallback);
  }
}
