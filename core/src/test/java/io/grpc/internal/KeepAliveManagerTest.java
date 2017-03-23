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

import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.assertEquals;
import static org.mockito.Matchers.isA;
import static org.mockito.Mockito.doReturn;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;

import io.grpc.Status;
import io.grpc.internal.KeepAliveManager.ClientKeepAlivePinger;
import io.grpc.internal.KeepAliveManager.KeepAlivePinger;
import java.util.concurrent.Executor;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

@RunWith(JUnit4.class)
public final class KeepAliveManagerTest {
  private final FakeTicker ticker = new FakeTicker();
  private KeepAliveManager keepAliveManager;
  @Mock private KeepAlivePinger keepAlivePinger;
  @Mock private ConnectionClientTransport transport;
  @Mock private ScheduledExecutorService scheduler;
  @Captor
  private ArgumentCaptor<Status> statusCaptor;

  static class FakeTicker extends KeepAliveManager.Ticker {
    long time;

    @Override
    public long read() {
      return time;
    }
  }

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    keepAliveManager = new KeepAliveManager(keepAlivePinger, scheduler, ticker, 1000, 2000, false);
  }

  @Test
  public void sendKeepAlivePings() {
    ticker.time = 1;
    // Transport becomes active. We should schedule keepalive pings.
    keepAliveManager.onTransportActive();
    ArgumentCaptor<Long> delayCaptor = ArgumentCaptor.forClass(Long.class);
    ArgumentCaptor<Runnable> sendPingCaptor = ArgumentCaptor.forClass(Runnable.class);
    verify(scheduler, times(1)).schedule(sendPingCaptor.capture(), delayCaptor.capture(),
        isA(TimeUnit.class));
    Runnable sendPing = sendPingCaptor.getValue();
    Long delay = delayCaptor.getValue();
    assertEquals(1000 - 1, delay.longValue());

    ScheduledFuture<?> shutdownFuture = mock(ScheduledFuture.class);
    doReturn(shutdownFuture)
        .when(scheduler).schedule(isA(Runnable.class), isA(Long.class), isA(TimeUnit.class));
    // Mannually running the Runnable will send the ping. Shutdown task should be scheduled.
    ticker.time = 1000;
    sendPing.run();
    verify(keepAlivePinger).ping();
    verify(scheduler, times(2)).schedule(isA(Runnable.class), delayCaptor.capture(),
        isA(TimeUnit.class));
    delay = delayCaptor.getValue();
    // Keepalive timeout is 2000.
    assertEquals(2000, delay.longValue());

    // Ping succeeds. Reschedule another ping.
    ticker.time = 1100;
    keepAliveManager.onDataReceived();
    verify(scheduler, times(3)).schedule(isA(Runnable.class), delayCaptor.capture(),
        isA(TimeUnit.class));
    // Shutdown task has been cancelled.
    verify(shutdownFuture).cancel(isA(Boolean.class));
    delay = delayCaptor.getValue();
    // Next ping should be exactly 1000 nanoseconds later.
    assertEquals(1000, delay.longValue());
  }

  @Test
  public void keepAlivePingDelayedByIncomingData() {
    // Transport becomes active. We should schedule keepalive pings.
    keepAliveManager.onTransportActive();
    ArgumentCaptor<Runnable> sendPingCaptor = ArgumentCaptor.forClass(Runnable.class);
    verify(scheduler, times(1)).schedule(sendPingCaptor.capture(), isA(Long.class),
        isA(TimeUnit.class));
    Runnable sendPing = sendPingCaptor.getValue();

    // We receive some data. We may need to delay the ping.
    ticker.time = 1500;
    keepAliveManager.onDataReceived();
    ticker.time = 1600;
    sendPing.run();
    // We didn't send the ping.
    verify(transport, times(0)).ping(isA(ClientTransport.PingCallback.class),
        isA(Executor.class));
    // Instead we reschedule.
    ArgumentCaptor<Long> delayCaptor = ArgumentCaptor.forClass(Long.class);
    verify(scheduler, times(2)).schedule(isA(Runnable.class), delayCaptor.capture(),
        isA(TimeUnit.class));
    Long delay = delayCaptor.getValue();
    assertEquals(1500 + 1000 - 1600, delay.longValue());
  }

  @Test
  public void clientKeepAlivePinger_pingTimeout() {
    keepAlivePinger = new ClientKeepAlivePinger(transport);

    keepAlivePinger.onPingTimeout();

    verify(transport).shutdownNow(statusCaptor.capture());
    Status status = statusCaptor.getValue();
    assertThat(status.getCode()).isEqualTo(Status.Code.UNAVAILABLE);
    assertThat(status.getDescription()).isEqualTo(
        "Keepalive failed. The connection is likely gone");
  }

  @Test
  public void clientKeepAlivePinger_pingFailure() {
    keepAlivePinger = new ClientKeepAlivePinger(transport);
    keepAlivePinger.ping();
    ArgumentCaptor<ClientTransport.PingCallback> pingCallbackCaptor =
        ArgumentCaptor.forClass(ClientTransport.PingCallback.class);
    verify(transport).ping(pingCallbackCaptor.capture(), isA(Executor.class));
    ClientTransport.PingCallback pingCallback = pingCallbackCaptor.getValue();

    pingCallback.onFailure(new Throwable());

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
    ArgumentCaptor<Runnable> sendPingCaptor = ArgumentCaptor.forClass(Runnable.class);
    verify(scheduler, times(1))
        .schedule(sendPingCaptor.capture(), isA(Long.class), isA(TimeUnit.class));
    Runnable sendPing = sendPingCaptor.getValue();

    ScheduledFuture<?> shutdownFuture = mock(ScheduledFuture.class);
    doReturn(shutdownFuture)
        .when(scheduler).schedule(isA(Runnable.class), isA(Long.class), isA(TimeUnit.class));
    // Mannually running the Runnable will send the ping. Shutdown task should be scheduled.
    ticker.time = 1000;
    sendPing.run();

    keepAliveManager.onTransportTermination();

    // Shutdown task has been cancelled.
    verify(shutdownFuture).cancel(isA(Boolean.class));
  }

  @Test
  public void keepAlivePingTimesOut() {
    // Transport becomes active. We should schedule keepalive pings.
    keepAliveManager.onTransportActive();
    ArgumentCaptor<Runnable> sendPingCaptor = ArgumentCaptor.forClass(Runnable.class);
    verify(scheduler, times(1)).schedule(sendPingCaptor.capture(), isA(Long.class),
        isA(TimeUnit.class));
    Runnable sendPing = sendPingCaptor.getValue();

    ScheduledFuture<?> shutdownFuture = mock(ScheduledFuture.class);
    doReturn(shutdownFuture)
        .when(scheduler).schedule(isA(Runnable.class), isA(Long.class), isA(TimeUnit.class));
    // Mannually running the Runnable will send the ping. Shutdown task should be scheduled.
    ticker.time = 1000;
    sendPing.run();
    verify(keepAlivePinger).ping();
    ArgumentCaptor<Runnable> shutdownCaptor = ArgumentCaptor.forClass(Runnable.class);
    verify(scheduler, times(2)).schedule(shutdownCaptor.capture(), isA(Long.class),
        isA(TimeUnit.class));
    Runnable shutdown = shutdownCaptor.getValue();

    // We do not receive the ping response. Shutdown runnable runs.
    // TODO(zdapeng): use FakeClock.ScheduledExecutorService
    ticker.time = 3000;
    shutdown.run();
    verify(keepAlivePinger).onPingTimeout();

    // We receive the ping response too late.
    keepAliveManager.onDataReceived();
    // No more ping should be scheduled.
    verify(scheduler, times(2)).schedule(isA(Runnable.class), isA(Long.class),
        isA(TimeUnit.class));
  }

  @Test
  public void transportGoesIdle() {
    // Transport becomes active. We should schedule keepalive pings.
    keepAliveManager.onTransportActive();
    ArgumentCaptor<Runnable> sendPingCaptor = ArgumentCaptor.forClass(Runnable.class);
    verify(scheduler, times(1)).schedule(sendPingCaptor.capture(), isA(Long.class),
        isA(TimeUnit.class));
    Runnable sendPing = sendPingCaptor.getValue();

    // Transport becomes idle. Nothing should happen when ping runnable runs.
    keepAliveManager.onTransportIdle();
    sendPing.run();
    // Ping was not sent.
    verify(transport, times(0)).ping(isA(ClientTransport.PingCallback.class),
        isA(Executor.class));
    // No new ping got scheduled.
    verify(scheduler, times(1)).schedule(isA(Runnable.class), isA(Long.class), isA(TimeUnit.class));
  }

  @Test
  public void transportGoesIdle_doesntCauseIdleWhenEnabled() {
    keepAliveManager.onTransportTermination();
    keepAliveManager = new KeepAliveManager(keepAlivePinger, scheduler, ticker, 1000, 2000, true);
    keepAliveManager.onTransportStarted();

    // Keepalive scheduling should have started immediately.
    ArgumentCaptor<Runnable> runnableCaptor = ArgumentCaptor.forClass(Runnable.class);
    verify(scheduler).schedule(runnableCaptor.capture(), isA(Long.class),
        isA(TimeUnit.class));
    Runnable sendPing = runnableCaptor.getValue();

    keepAliveManager.onTransportActive();

    // Transport becomes idle. Should not impact the sending of the ping.
    keepAliveManager.onTransportIdle();
    sendPing.run();
    // Ping was sent.
    verify(keepAlivePinger).ping();
    // Shutdown is scheduled.
    verify(scheduler, times(2)).schedule(runnableCaptor.capture(), isA(Long.class),
        isA(TimeUnit.class));
    // Shutdown is triggered.
    runnableCaptor.getValue().run();
    verify(keepAlivePinger).onPingTimeout();
  }

  @Test
  public void transportGoesIdleAfterPingSent() {
    // Transport becomes active. We should schedule keepalive pings.
    keepAliveManager.onTransportActive();
    ArgumentCaptor<Runnable> sendPingCaptor = ArgumentCaptor.forClass(Runnable.class);
    verify(scheduler, times(1)).schedule(sendPingCaptor.capture(), isA(Long.class),
        isA(TimeUnit.class));
    Runnable sendPing = sendPingCaptor.getValue();

    ScheduledFuture<?> shutdownFuture = mock(ScheduledFuture.class);
    doReturn(shutdownFuture)
        .when(scheduler).schedule(isA(Runnable.class), isA(Long.class), isA(TimeUnit.class));
    // Mannually running the Runnable will send the ping. Shutdown task should be scheduled.
    // TODO(zdapeng): user FakeClock.ScheduledExecutorService
    ticker.time = 1000;
    sendPing.run();
    verify(keepAlivePinger).ping();
    verify(scheduler, times(2)).schedule(isA(Runnable.class), isA(Long.class), isA(TimeUnit.class));

    // Transport becomes idle. No more ping should be scheduled after we receive a ping response.
    keepAliveManager.onTransportIdle();
    ticker.time = 1100;
    keepAliveManager.onDataReceived();
    verify(scheduler, times(2)).schedule(isA(Runnable.class), isA(Long.class), isA(TimeUnit.class));
    // Shutdown task has been cancelled.
    verify(shutdownFuture).cancel(isA(Boolean.class));

    // Transport becomes active again. Another ping is scheduled.
    keepAliveManager.onTransportActive();
    verify(scheduler, times(3)).schedule(isA(Runnable.class), isA(Long.class), isA(TimeUnit.class));
  }

  @Test
  public void transportShutsdownAfterPingScheduled() {
    ScheduledFuture<?> pingFuture = mock(ScheduledFuture.class);
    doReturn(pingFuture)
        .when(scheduler).schedule(isA(Runnable.class), isA(Long.class), isA(TimeUnit.class));
    // Ping will be scheduled.
    keepAliveManager.onTransportActive();
    verify(scheduler, times(1)).schedule(isA(Runnable.class), isA(Long.class),
        isA(TimeUnit.class));
    // Transport is shutting down.
    keepAliveManager.onTransportTermination();
    // Ping future should have been cancelled.
    verify(pingFuture).cancel(isA(Boolean.class));
  }

  @Test
  public void transportShutsdownAfterPingSent() {
    keepAliveManager.onTransportActive();
    ArgumentCaptor<Runnable> sendPingCaptor = ArgumentCaptor.forClass(Runnable.class);
    verify(scheduler, times(1)).schedule(sendPingCaptor.capture(), isA(Long.class),
        isA(TimeUnit.class));
    Runnable sendPing = sendPingCaptor.getValue();

    ScheduledFuture<?> shutdownFuture = mock(ScheduledFuture.class);
    doReturn(shutdownFuture)
        .when(scheduler).schedule(isA(Runnable.class), isA(Long.class), isA(TimeUnit.class));
    // Mannually running the Runnable will send the ping. Shutdown task should be scheduled.
    ticker.time = 1000;
    sendPing.run();
    verify(keepAlivePinger).ping();
    verify(scheduler, times(2)).schedule(isA(Runnable.class), isA(Long.class),
        isA(TimeUnit.class));

    // Transport is shutting down.
    keepAliveManager.onTransportTermination();
    // Shutdown task has been cancelled.
    verify(shutdownFuture).cancel(isA(Boolean.class));
  }

  @Test
  public void pingSentThenIdleThenActiveThenAck() {
    keepAliveManager.onTransportActive();
    ArgumentCaptor<Runnable> sendPingCaptor = ArgumentCaptor.forClass(Runnable.class);
    verify(scheduler, times(1))
        .schedule(sendPingCaptor.capture(), isA(Long.class), isA(TimeUnit.class));
    Runnable sendPing = sendPingCaptor.getValue();
    ScheduledFuture<?> shutdownFuture = mock(ScheduledFuture.class);
    // ping scheduled
    doReturn(shutdownFuture)
        .when(scheduler).schedule(isA(Runnable.class), isA(Long.class), isA(TimeUnit.class));

    // Mannually running the Runnable will send the ping.
    ticker.time = 1000;
    sendPing.run();

    // shutdown scheduled
    verify(scheduler, times(2))
        .schedule(sendPingCaptor.capture(), isA(Long.class), isA(TimeUnit.class));
    verify(keepAlivePinger).ping();

    keepAliveManager.onTransportIdle();

    keepAliveManager.onTransportActive();

    keepAliveManager.onDataReceived();

    // another ping scheduled
    verify(scheduler, times(3))
        .schedule(sendPingCaptor.capture(), isA(Long.class), isA(TimeUnit.class));
  }
}

