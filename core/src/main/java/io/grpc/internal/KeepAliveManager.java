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

import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Stopwatch;
import com.google.common.util.concurrent.MoreExecutors;
import io.grpc.Status;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import javax.annotation.concurrent.GuardedBy;

/**
 * Manages keepalive pings.
 */
public class KeepAliveManager {
  private static final long MIN_KEEPALIVE_TIME_NANOS = TimeUnit.SECONDS.toNanos(10);
  private static final long MIN_KEEPALIVE_TIMEOUT_NANOS = TimeUnit.MILLISECONDS.toNanos(10L);

  private final ScheduledExecutorService scheduler;
  @GuardedBy("this")
  private final Stopwatch stopwatch;
  private final KeepAlivePinger keepAlivePinger;
  private final boolean keepAliveDuringTransportIdle;
  @GuardedBy("this")
  private State state = State.IDLE;
  @GuardedBy("this")
  private ScheduledFuture<?> shutdownFuture;
  @GuardedBy("this")
  private ScheduledFuture<?> pingFuture;
  private final Runnable shutdown = new LogExceptionRunnable(new Runnable() {
    @Override
    public void run() {
      boolean shouldShutdown = false;
      synchronized (KeepAliveManager.this) {
        if (state != State.DISCONNECTED) {
          // We haven't received a ping response within the timeout. The connection is likely gone
          // already. Shutdown the transport and fail all existing rpcs.
          state = State.DISCONNECTED;
          shouldShutdown = true;
        }
      }
      if (shouldShutdown) {
        keepAlivePinger.onPingTimeout();
      }
    }
  });
  private final Runnable sendPing = new LogExceptionRunnable(new Runnable() {
    @Override
    public void run() {
      boolean shouldSendPing = false;
      synchronized (KeepAliveManager.this) {
        pingFuture = null;
        if (state == State.PING_SCHEDULED) {
          shouldSendPing = true;
          state = State.PING_SENT;
          // Schedule a shutdown. It fires if we don't receive the ping response within the timeout.
          shutdownFuture = scheduler.schedule(shutdown, keepAliveTimeoutInNanos,
              TimeUnit.NANOSECONDS);
        } else if (state == State.PING_DELAYED) {
          // We have received some data. Reschedule the ping with the new time.
          pingFuture = scheduler.schedule(
              sendPing,
              keepAliveTimeInNanos - stopwatch.elapsed(TimeUnit.NANOSECONDS),
              TimeUnit.NANOSECONDS);
          state = State.PING_SCHEDULED;
        }
      }
      if (shouldSendPing) {
        // Send the ping.
        keepAlivePinger.ping();
      }
    }
  });

  private final long keepAliveTimeInNanos;
  private final long keepAliveTimeoutInNanos;

  private enum State {
    /*
     * We don't need to do any keepalives. This means the transport has no active rpcs and
     * keepAliveDuringTransportIdle == false.
     */
    IDLE,
    /*
     * We have scheduled a ping to be sent in the future. We may decide to delay it if we receive
     * some data.
     */
    PING_SCHEDULED,
    /*
     * We need to delay the scheduled keepalive ping.
     */
    PING_DELAYED,
    /*
     * The ping has been sent out. Waiting for a ping response.
     */
    PING_SENT,
    /*
     * Transport goes idle after ping has been sent.
     */
    IDLE_AND_PING_SENT,
    /*
     * The transport has been disconnected. We won't do keepalives any more.
     */
    DISCONNECTED,
  }

  /**
   * Creates a KeepAliverManager.
   */
  public KeepAliveManager(KeepAlivePinger keepAlivePinger, ScheduledExecutorService scheduler,
                          long keepAliveTimeInNanos, long keepAliveTimeoutInNanos,
                          boolean keepAliveDuringTransportIdle) {
    this(keepAlivePinger, scheduler, Stopwatch.createUnstarted(), keepAliveTimeInNanos,
        keepAliveTimeoutInNanos,
        keepAliveDuringTransportIdle);
  }

  @VisibleForTesting
  KeepAliveManager(KeepAlivePinger keepAlivePinger, ScheduledExecutorService scheduler,
      Stopwatch stopwatch, long keepAliveTimeInNanos, long keepAliveTimeoutInNanos,
                   boolean keepAliveDuringTransportIdle) {
    this.keepAlivePinger = checkNotNull(keepAlivePinger, "keepAlivePinger");
    this.scheduler = checkNotNull(scheduler, "scheduler");
    this.stopwatch = checkNotNull(stopwatch, "stopwatch");
    this.keepAliveTimeInNanos = keepAliveTimeInNanos;
    this.keepAliveTimeoutInNanos = keepAliveTimeoutInNanos;
    this.keepAliveDuringTransportIdle = keepAliveDuringTransportIdle;
    stopwatch.reset().start();
  }

  /** Start keepalive monitoring. */
  public synchronized void onTransportStarted() {
    if (keepAliveDuringTransportIdle) {
      onTransportActive();
    }
  }

  /**
   * Transport has received some data so that we can delay sending keepalives.
   */
  public synchronized void onDataReceived() {
    stopwatch.reset().start();
    // We do not cancel the ping future here. This avoids constantly scheduling and cancellation in
    // a busy transport. Instead, we update the status here and reschedule later. So we actually
    // keep one sendPing task always in flight when there're active rpcs.
    if (state == State.PING_SCHEDULED) {
      state = State.PING_DELAYED;
    } else if (state == State.PING_SENT || state == State.IDLE_AND_PING_SENT) {
      // Ping acked or effectively ping acked. Cancel shutdown, and then if not idle,
      // schedule a new keep-alive ping.
      if (shutdownFuture != null) {
        shutdownFuture.cancel(false);
      }
      if (state == State.IDLE_AND_PING_SENT) {
        // not to schedule new pings until onTransportActive
        state = State.IDLE;
        return;
      }
      // schedule a new ping
      state = State.PING_SCHEDULED;
      checkState(pingFuture == null, "There should be no outstanding pingFuture");
      pingFuture = scheduler.schedule(sendPing, keepAliveTimeInNanos, TimeUnit.NANOSECONDS);
    }
  }

  /**
   * Transport has active streams. Start sending keepalives if necessary.
   */
  public synchronized void onTransportActive() {
    if (state == State.IDLE) {
      // When the transport goes active, we do not reset the nextKeepaliveTime. This allows us to
      // quickly check whether the connection is still working.
      state = State.PING_SCHEDULED;
      if (pingFuture == null) {
        pingFuture = scheduler.schedule(
            sendPing,
            keepAliveTimeInNanos - stopwatch.elapsed(TimeUnit.NANOSECONDS),
            TimeUnit.NANOSECONDS);
      }
    } else if (state == State.IDLE_AND_PING_SENT) {
      state = State.PING_SENT;
    } // Other states are possible when keepAliveDuringTransportIdle == true
  }

  /**
   * Transport has finished all streams.
   */
  public synchronized void onTransportIdle() {
    if (keepAliveDuringTransportIdle) {
      return;
    }
    if (state == State.PING_SCHEDULED || state == State.PING_DELAYED) {
      state = State.IDLE;
    }
    if (state == State.PING_SENT) {
      state = State.IDLE_AND_PING_SENT;
    }
  }

  /**
   * Transport is being terminated. We no longer need to do keepalives.
   */
  public synchronized void onTransportTermination() {
    if (state != State.DISCONNECTED) {
      state = State.DISCONNECTED;
      if (shutdownFuture != null) {
        shutdownFuture.cancel(false);
      }
      if (pingFuture != null) {
        pingFuture.cancel(false);
        pingFuture = null;
      }
    }
  }

  /**
   * Bumps keepalive time to 10 seconds if the specified value was smaller than that.
   */
  public static long clampKeepAliveTimeInNanos(long keepAliveTimeInNanos) {
    return Math.max(keepAliveTimeInNanos, MIN_KEEPALIVE_TIME_NANOS);
  }

  /**
   * Bumps keepalive timeout to 10 milliseconds if the specified value was smaller than that.
   */
  public static long clampKeepAliveTimeoutInNanos(long keepAliveTimeoutInNanos) {
    return Math.max(keepAliveTimeoutInNanos, MIN_KEEPALIVE_TIMEOUT_NANOS);
  }

  public interface KeepAlivePinger {
    /**
     * Sends out a keep-alive ping.
     */
    void ping();

    /**
     * Callback when Ping Ack was not received in KEEPALIVE_TIMEOUT. Should shutdown the transport.
     */
    void onPingTimeout();
  }

  /**
   * Default client side {@link KeepAlivePinger}.
   */
  public static final class ClientKeepAlivePinger implements KeepAlivePinger {
    private final ConnectionClientTransport transport;

    public ClientKeepAlivePinger(ConnectionClientTransport transport) {
      this.transport = transport;
    }

    @Override
    public void ping() {
      transport.ping(new ClientTransport.PingCallback() {
        @Override
        public void onSuccess(long roundTripTimeNanos) {}

        @Override
        public void onFailure(Throwable cause) {
          transport.shutdownNow(Status.UNAVAILABLE.withDescription(
              "Keepalive failed. The connection is likely gone"));
        }
      }, MoreExecutors.directExecutor());
    }

    @Override
    public void onPingTimeout() {
      transport.shutdownNow(Status.UNAVAILABLE.withDescription(
          "Keepalive failed. The connection is likely gone"));
    }
  }
}

