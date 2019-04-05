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

import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import static org.mockito.ArgumentMatchers.isA;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;

import com.google.common.collect.Iterables;
import io.grpc.Status;
import io.grpc.internal.KeepAliveManager.ClientKeepAlivePinger;
import io.grpc.internal.KeepAliveManager.KeepAlivePinger;
import java.util.concurrent.Executor;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.junit.MockitoJUnit;
import org.mockito.junit.MockitoRule;

@RunWith(JUnit4.class)
public final class KeepAliveManagerTest {

  private final FakeClock fakeClock = new FakeClock();
  private KeepAliveManager keepAliveManager;
  @Rule
  public final MockitoRule mocks = MockitoJUnit.rule();
  @Mock private KeepAlivePinger keepAlivePinger;

  @Before
  public void setUp() {
    ScheduledExecutorService scheduler = fakeClock.getScheduledExecutorService();
    keepAliveManager = new KeepAliveManager(keepAlivePinger, scheduler,
        fakeClock.getStopwatchSupplier().get(), 1000, 2000, false);
  }

  @Test
  public void sendKeepAlivePings() {
    fakeClock.forwardNanos(1);
    // Transport becomes active. We should schedule keepalive pings.
    keepAliveManager.onTransportActive();
    ScheduledFuture<?> future = Iterables.getFirst(fakeClock.getPendingTasks(), null);
    assertEquals(1000 - 1, future.getDelay(TimeUnit.NANOSECONDS));

    // Forward clock to keepAliveTimeInNanos will send the ping. Shutdown task should be scheduled.
    fakeClock.forwardNanos(999);
    verify(keepAlivePinger).ping();
    ScheduledFuture<?> shutdownFuture = Iterables.getFirst(fakeClock.getPendingTasks(), null);
    // Keepalive timeout is 2000.
    assertEquals(2000, shutdownFuture.getDelay(TimeUnit.NANOSECONDS));

    // Ping succeeds. Reschedule another ping.
    fakeClock.forwardNanos(100);
    keepAliveManager.onDataReceived();
    // Shutdown task has been cancelled.
    assertTrue(shutdownFuture.isCancelled());
    future = Iterables.getFirst(fakeClock.getPendingTasks(), null);
    // Next ping should be exactly 1000 nanoseconds later.
    assertEquals(1000, future.getDelay(TimeUnit.NANOSECONDS));
  }

  @Test
  public void keepAlivePingDelayedByIncomingData() {
    // Transport becomes active. We should schedule keepalive pings.
    keepAliveManager.onTransportActive();

    // We receive some data. We may need to delay the ping.
    fakeClock.forwardNanos(990);
    keepAliveManager.onDataReceived();
    fakeClock.forwardNanos(20);

    // We didn't send the ping.
    verify(keepAlivePinger, never()).ping();

    // Instead we reschedule.
    ScheduledFuture<?> future = Iterables.getFirst(fakeClock.getPendingTasks(), null);
    assertEquals(1000 - 20, future.getDelay(TimeUnit.NANOSECONDS));
  }

  @Test
  public void clientKeepAlivePinger_pingTimeout() {
    ConnectionClientTransport transport = mock(ConnectionClientTransport.class);
    keepAlivePinger = new ClientKeepAlivePinger(transport);

    keepAlivePinger.onPingTimeout();

    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(transport).shutdownNow(statusCaptor.capture());
    Status status = statusCaptor.getValue();
    assertThat(status.getCode()).isEqualTo(Status.Code.UNAVAILABLE);
    assertThat(status.getDescription()).isEqualTo(
        "Keepalive failed. The connection is likely gone");
  }

  @Test
  public void clientKeepAlivePinger_pingFailure() {
    ConnectionClientTransport transport = mock(ConnectionClientTransport.class);
    keepAlivePinger = new ClientKeepAlivePinger(transport);
    keepAlivePinger.ping();
    ArgumentCaptor<ClientTransport.PingCallback> pingCallbackCaptor =
        ArgumentCaptor.forClass(ClientTransport.PingCallback.class);
    verify(transport).ping(pingCallbackCaptor.capture(), isA(Executor.class));
    ClientTransport.PingCallback pingCallback = pingCallbackCaptor.getValue();

    pingCallback.onFailure(new Throwable());

    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(transport).shutdownNow(statusCaptor.capture());
    Status status = statusCaptor.getValue();
    assertThat(status.getCode()).isEqualTo(Status.Code.UNAVAILABLE);
    assertThat(status.getDescription()).isEqualTo(
        "Keepalive failed. The connection is likely gone");
  }


  @Test
  public void onTransportTerminationCancelsShutdownFuture() {
    // Transport becomes active. We should schedule keepalive pings.
    keepAliveManager.onTransportActive();
    fakeClock.forwardNanos(1000);

    ScheduledFuture<?> shutdownFuture = Iterables.getFirst(fakeClock.getPendingTasks(), null);

    keepAliveManager.onTransportTermination();

    // Shutdown task has been cancelled.
    assertTrue(shutdownFuture.isCancelled());
  }

  @Test
  public void keepAlivePingTimesOut() {
    // Transport becomes active. We should schedule keepalive pings.
    keepAliveManager.onTransportActive();

    // Forward clock to keepAliveTimeInNanos will send the ping. Shutdown task should be scheduled.
    fakeClock.forwardNanos(1000);
    verify(keepAlivePinger).ping();

    // We do not receive the ping response. Shutdown runnable runs.
    fakeClock.forwardNanos(2000);
    verify(keepAlivePinger).onPingTimeout();

    // We receive the ping response too late.
    keepAliveManager.onDataReceived();
    // No more ping should be scheduled.
    assertThat(fakeClock.getPendingTasks()).isEmpty();
  }

  @Test
  public void transportGoesIdle() {
    // Transport becomes active. We should schedule keepalive pings.
    keepAliveManager.onTransportActive();
    assertThat(fakeClock.getPendingTasks()).hasSize(1);

    // Transport becomes idle. Nothing should happen when ping runnable runs.
    keepAliveManager.onTransportIdle();
    fakeClock.forwardNanos(1000);
    // Ping was not sent.
    verify(keepAlivePinger, never()).ping();
    // No new ping got scheduled.
    assertThat(fakeClock.getPendingTasks()).isEmpty();

    // But when transport goes back to active
    keepAliveManager.onTransportActive();
    fakeClock.runDueTasks();
    // Ping is now sent.
    verify(keepAlivePinger).ping();
  }

  @Test
  public void transportGoesIdle_doesntCauseIdleWhenEnabled() {
    keepAliveManager.onTransportTermination();
    ScheduledExecutorService scheduler = fakeClock.getScheduledExecutorService();
    keepAliveManager = new KeepAliveManager(keepAlivePinger, scheduler, 1000, 2000, true);
    keepAliveManager.onTransportStarted();

    // Keepalive scheduling should have started immediately.
    assertThat(fakeClock.getPendingTasks()).hasSize(1);

    keepAliveManager.onTransportActive();

    // Transport becomes idle. Should not impact the sending of the ping.
    keepAliveManager.onTransportIdle();
    fakeClock.forwardNanos(1000);
    // Ping was sent.
    verify(keepAlivePinger).ping();
    // Shutdown is scheduled.
    assertThat(fakeClock.getPendingTasks()).hasSize(1);
    // Shutdown is triggered.
    fakeClock.forwardNanos(2000);
    verify(keepAlivePinger).onPingTimeout();
  }

  @Test
  public void transportGoesIdleAfterPingSent() {
    // Transport becomes active. We should schedule keepalive pings.
    keepAliveManager.onTransportActive();

    // Forward clock to keepAliveTimeInNanos will send the ping. Shutdown task should be scheduled.
    fakeClock.forwardNanos(1000);
    ScheduledFuture<?> shutdownFuture = Iterables.getFirst(fakeClock.getPendingTasks(), null);
    verify(keepAlivePinger).ping();
    assertThat(fakeClock.getPendingTasks()).hasSize(1);

    // Transport becomes idle. No more ping should be scheduled after we receive a ping response.
    keepAliveManager.onTransportIdle();
    fakeClock.forwardNanos(100);
    keepAliveManager.onDataReceived();
    assertTrue(shutdownFuture.isCancelled());
    assertThat(fakeClock.getPendingTasks()).isEmpty();
    // Transport becomes active again. Another ping is scheduled.
    keepAliveManager.onTransportActive();
    assertThat(fakeClock.getPendingTasks()).hasSize(1);
  }

  @Test
  public void transportGoesIdleBeforePingSent() {

    // Transport becomes active. We should schedule keepalive pings.
    keepAliveManager.onTransportActive();
    assertThat(fakeClock.getPendingTasks()).hasSize(1);
    ScheduledFuture<?> pingFuture = Iterables.getFirst(fakeClock.getPendingTasks(), null);

    // Data is received, and we go to ping delayed
    keepAliveManager.onDataReceived();

    // Transport becomes idle while the 1st ping is still scheduled
    keepAliveManager.onTransportIdle();

    // Transport becomes active again, we don't need to reschedule another ping
    keepAliveManager.onTransportActive();
    assertThat(fakeClock.getPendingTasks()).containsExactly(pingFuture);
  }

  @Test
  public void transportShutsdownAfterPingScheduled() {
    // Ping will be scheduled.
    keepAliveManager.onTransportActive();
    assertThat(fakeClock.getPendingTasks()).hasSize(1);
    ScheduledFuture<?> pingFuture = Iterables.getFirst(fakeClock.getPendingTasks(), null);
    // Transport is shutting down.
    keepAliveManager.onTransportTermination();
    // Ping future should have been cancelled.
    assertTrue(pingFuture.isCancelled());
  }

  @Test
  public void transportShutsdownAfterPingSent() {
    keepAliveManager.onTransportActive();
    // Forward clock to keepAliveTimeInNanos will send the ping. Shutdown task should be scheduled.
    fakeClock.forwardNanos(1000);
    verify(keepAlivePinger).ping();
    assertThat(fakeClock.getPendingTasks()).hasSize(1);
    ScheduledFuture<?> shutdownFuture = Iterables.getFirst(fakeClock.getPendingTasks(), null);

    // Transport is shutting down.
    keepAliveManager.onTransportTermination();
    // Shutdown task has been cancelled.
    assertTrue(shutdownFuture.isCancelled());
  }

  @Test
  public void pingSentThenIdleThenActiveThenAck() {
    keepAliveManager.onTransportActive();
    // Forward clock to keepAliveTimeInNanos will send the ping. Shutdown task should be scheduled.
    fakeClock.forwardNanos(1000);
    verify(keepAlivePinger).ping();

    // shutdown scheduled
    assertThat(fakeClock.getPendingTasks()).hasSize(1);

    keepAliveManager.onTransportIdle();

    keepAliveManager.onTransportActive();

    keepAliveManager.onDataReceived();

    // another ping scheduled
    assertThat(fakeClock.getPendingTasks()).hasSize(1);
    fakeClock.forwardNanos(1000);
    verify(keepAlivePinger, times(2)).ping();
  }
}

