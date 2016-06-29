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

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.MoreExecutors;

import io.grpc.Status;

import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;

/**
 * Manages keepalive pings.
 */
public class KeepAliveManager {
  private static final SystemTicker SYSTEM_TICKER = new SystemTicker();
  private static final long MIN_KEEPALIVE_DELAY_NANOS = TimeUnit.MINUTES.toNanos(1);

  private final ScheduledExecutorService scheduler;
  private final ManagedClientTransport transport;
  private final Ticker ticker;
  private State state = State.IDLE;
  private long nextKeepaliveTime;
  private ScheduledFuture<?> shutdownFuture;
  private ScheduledFuture<?> pingFuture;
  private final Runnable shutdown = new Runnable() {
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
        transport.shutdownNow(Status.UNAVAILABLE.withDescription(
            "Keepalive failed. The connection is likely gone"));
      }
    }
  };
  private final Runnable sendPing = new Runnable() {
    @Override
    public void run() {
      boolean shouldSendPing = false;
      synchronized (KeepAliveManager.this) { 
        if (state == State.PING_SCHEDULED) {
          shouldSendPing = true;
          state = State.PING_SENT;
          // Schedule a shutdown. It fires if we don't receive the ping response within the timeout.
          shutdownFuture = scheduler.schedule(shutdown, keepAliveTimeoutInNanos,
              TimeUnit.NANOSECONDS);
        } else if (state == State.PING_DELAYED) {
          // We have received some data. Reschedule the ping with the new time.
          pingFuture = scheduler.schedule(sendPing, nextKeepaliveTime - ticker.read(),
              TimeUnit.NANOSECONDS);
          state = State.PING_SCHEDULED;
        }
      }
      if (shouldSendPing) {
        // Send the ping.
        transport.ping(pingCallback, MoreExecutors.directExecutor());
      }
    }
  };
  private final KeepAlivePingCallback pingCallback = new KeepAlivePingCallback();
  private long keepAliveDelayInNanos;
  private long keepAliveTimeoutInNanos;

  private enum State {
    /*
     * Transport has no active rpcs. We don't need to do any keepalives.
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
  public KeepAliveManager(ManagedClientTransport transport, ScheduledExecutorService scheduler,
                          long keepAliveDelayInNanos, long keepAliveTimeoutInNanos) {
    this.transport = Preconditions.checkNotNull(transport, "transport");
    this.scheduler = Preconditions.checkNotNull(scheduler, "scheduler");
    this.ticker = SYSTEM_TICKER;
    // Set a minimum cap on keepalive dealy.
    this.keepAliveDelayInNanos = Math.max(MIN_KEEPALIVE_DELAY_NANOS, keepAliveDelayInNanos);
    this.keepAliveTimeoutInNanos = keepAliveTimeoutInNanos;
    nextKeepaliveTime = ticker.read() + keepAliveDelayInNanos;
  }

  @VisibleForTesting
  KeepAliveManager(ManagedClientTransport transport, ScheduledExecutorService scheduler,
                   Ticker ticker, long keepAliveDelayInNanos, long keepAliveTimeoutInNanos) {
    this.transport = Preconditions.checkNotNull(transport, "transport");
    this.scheduler = Preconditions.checkNotNull(scheduler, "scheduler");
    this.ticker = Preconditions.checkNotNull(ticker, "ticker");
    this.keepAliveDelayInNanos = keepAliveDelayInNanos;
    this.keepAliveTimeoutInNanos = keepAliveTimeoutInNanos;
    nextKeepaliveTime = ticker.read() + keepAliveDelayInNanos;
  }

  /**
   * Transport has received some data so that we can delay sending keepalives.
   */
  public synchronized void onDataReceived() {
    nextKeepaliveTime = ticker.read() + keepAliveDelayInNanos;
    // We do not cancel the ping future here. This avoids constantly scheduling and cancellation in
    // a busy transport. Instead, we update the status here and reschedule later. So we actually
    // keep one sendPing task always in flight when there're active rpcs.
    if (state == State.PING_SCHEDULED) {
      state = State.PING_DELAYED;
    }
  }

  /**
   * Transport has active streams. Start sending keepalives if necessary.
   */
  public synchronized void onTransportActive() {
    if (state == State.IDLE) {
      // When the transport goes active, we do not reset the nextKeepaliveTime. This allows us to
      // quickly check whether the conneciton is still working.
      state = State.PING_SCHEDULED;
      pingFuture = scheduler.schedule(sendPing, nextKeepaliveTime - ticker.read(),
          TimeUnit.NANOSECONDS);
    }
  }

  /**
   * Transport has finished all streams.
   */
  public synchronized void onTransportIdle() {
    if (state == State.PING_SCHEDULED || state == State.PING_DELAYED) {
      state = State.IDLE;
    }
    if (state == State.PING_SENT) {
      state = State.IDLE_AND_PING_SENT;
    }
  }
  
  /**
   * Transport is shutting down. We no longer need to do keepalives.
   */
  public synchronized void onTransportShutdown() {
    if (state != State.DISCONNECTED) {
      state = State.DISCONNECTED;
      if (shutdownFuture != null) {
        shutdownFuture.cancel(false);
      }
      if (pingFuture != null) {
        pingFuture.cancel(false);
      }
    }
  }  

  private class KeepAlivePingCallback implements ClientTransport.PingCallback {

    @Override
    public void onSuccess(long roundTripTimeNanos) {
      synchronized (KeepAliveManager.this) {
        shutdownFuture.cancel(false);
        nextKeepaliveTime = ticker.read() + keepAliveDelayInNanos;
        if (state == State.PING_SENT) {
          // We have received the ping response so there's no need to shutdown the transport.
          // Schedule a new keepalive ping.
          pingFuture = scheduler.schedule(sendPing, keepAliveDelayInNanos, TimeUnit.NANOSECONDS);
          state = State.PING_SCHEDULED;
        }
        if (state == State.IDLE_AND_PING_SENT) {
          // Transport went idle after we had sent out the ping. We don't need to schedule a new
          // ping.
          state = State.IDLE;
        }
      }
    }

    @Override
    public void onFailure(Throwable cause) {
      // Keepalive ping has failed. Shutdown the transport now.
      synchronized (KeepAliveManager.this) {
        shutdownFuture.cancel(false);
      }
      shutdown.run();
    }
  }

  // TODO(zsurocking): Classes below are copied from Deadline.java. We should consider share the
  // code.

  /** Time source representing nanoseconds since fixed but arbitrary point in time. */
  abstract static class Ticker {
    /** Returns the number of nanoseconds since this source's epoch. */
    public abstract long read();
  }

  private static class SystemTicker extends Ticker {
    @Override
    public long read() {
      return System.nanoTime();
    }
  }

}

