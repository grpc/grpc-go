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

import com.google.common.base.Preconditions;
import com.google.common.base.Throwables;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;

import io.grpc.LoadBalancer;
import io.grpc.Status;

import java.net.SocketAddress;
import java.util.ArrayList;
import java.util.Collection;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import java.util.logging.Level;
import java.util.logging.Logger;

import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;
import javax.annotation.concurrent.ThreadSafe;

/**
 * Transports for a single server.
 */
@ThreadSafe
final class TransportSet {
  private static final Logger log = Logger.getLogger(TransportSet.class.getName());
  private static final SettableFuture<ClientTransport> NULL_VALUE_FUTURE;

  static {
    NULL_VALUE_FUTURE = SettableFuture.create();
    NULL_VALUE_FUTURE.set(null);
  }

  private final Object lock = new Object();
  private final SocketAddress server;
  private final String authority;
  private final BackoffPolicy.Provider backoffPolicyProvider;
  private final Callback callback;
  private final ClientTransportFactory transportFactory;
  private final ScheduledExecutorService scheduledExecutor;

  @GuardedBy("lock")
  @Nullable
  private BackoffPolicy reconnectPolicy;

  @GuardedBy("lock")
  @Nullable
  private ScheduledFuture<?> reconnectTask;

  /**
   * All transports that are not stopped. At the very least the value of {@link
   * activeTransportFuture} will be present, but previously used transports that still have streams
   * or are stopping may also be present.
   */
  @GuardedBy("lock")
  private final Collection<ClientTransport> transports = new ArrayList<ClientTransport>();

  private final LoadBalancer loadBalancer;

  @GuardedBy("lock")
  private boolean shutdown;

  /**
   * The future for the transport for new outgoing requests. 'lock' lock must be held when assigning
   * to activeTransportFuture.
   */
  private volatile SettableFuture<ClientTransport> activeTransportFuture;

  TransportSet(SocketAddress server, String authority, LoadBalancer loadBalancer,
      BackoffPolicy.Provider backoffPolicyProvider, ClientTransportFactory transportFactory,
      ScheduledExecutorService scheduledExecutor, Callback callback) {
    this.server = server;
    this.authority = authority;
    this.loadBalancer = loadBalancer;
    this.backoffPolicyProvider = backoffPolicyProvider;
    this.transportFactory = transportFactory;
    this.scheduledExecutor = scheduledExecutor;
    this.callback = callback;
    createActiveTransportFuture();
  }

  /**
   * Returns a future for the active transport that will be used to create new streams.
   *
   * <p>If this {@code TransportSet} has been shut down, the returned future will have {@code null}
   * value.
   */
  ListenableFuture<ClientTransport> obtainActiveTransport() {
    return activeTransportFuture;
  }

  /**
   * Creates a new activeTransportFuture, overwrites the current value and initiates the connection.
   *
   * <p>This method MUST ONLY be called in one of the following cases:
   * <ol>
   * <li>activeTransportFuture has never been assigned, thus it's null;</li>
   * <li>activeTransportFuture is done and its transport has been shut down.</li>
   * </ol>
   */
  private void createActiveTransportFuture() {
    synchronized (lock) {
      if (shutdown) {
        return;
      }
      Preconditions.checkState(activeTransportFuture == null || activeTransportFuture.isDone(),
          "activeTransportFuture is neither null nor done");
      activeTransportFuture = SettableFuture.create();
      scheduleConnection();
    }
  }

  // Can only be called when shutdown == false
  @GuardedBy("lock")
  private void scheduleConnection() {
    Preconditions.checkState(!shutdown, "Already shut down");
    Preconditions.checkState(reconnectTask == null || reconnectTask.isDone(),
        "previous reconnectTask is not done");
    long delayMillis;
    if (reconnectPolicy == null) {
      // First connect attempt
      delayMillis = 0;
      reconnectPolicy = backoffPolicyProvider.get();
    } else {
      // Reconnect attempts
      delayMillis = reconnectPolicy.nextBackoffMillis();
    }
    reconnectTask = scheduledExecutor.schedule(new Runnable() {
      @Override
      public void run() {
        synchronized (lock) {
          if (shutdown) {
            return;
          }
          ClientTransport newActiveTransport = transportFactory.newClientTransport(
              server, authority);
          log.log(Level.INFO, "Created transport {0} for {1}",
              new Object[] {newActiveTransport, server});
          transports.add(newActiveTransport);
          newActiveTransport.start(
              new TransportListener(newActiveTransport, activeTransportFuture));
          Preconditions.checkState(activeTransportFuture.set(newActiveTransport),
              "failed to set the new transport to the future");
        }
      }
    }, delayMillis, TimeUnit.MILLISECONDS);
  }

  /**
   * Shut down all transports, may run callback inline.
   */
  void shutdown() {
    SettableFuture<ClientTransport> savedActiveTransportFuture;
    boolean runCallback = false;
    synchronized (lock) {
      if (shutdown) {
        return;
      }
      shutdown = true;
      savedActiveTransportFuture = activeTransportFuture;
      activeTransportFuture = NULL_VALUE_FUTURE;
      if (transports.isEmpty()) {
        runCallback = true;
      }
      reconnectTask.cancel(false);
      // else: the callback will be run once all transports have been terminated
    }
    if (savedActiveTransportFuture != null) {
      if (savedActiveTransportFuture.isDone()) {
        try {
          // Should not throw any exception here
          savedActiveTransportFuture.get().shutdown();
        } catch (Exception e) {
          throw Throwables.propagate(e);
        }
      } else {
        savedActiveTransportFuture.set(null);
      }
    }
    if (runCallback) {
      callback.onTerminated();
    }
  }

  private class TransportListener implements ClientTransport.Listener {
    private final ClientTransport transport;
    private final SettableFuture<ClientTransport> transportFuture;

    public TransportListener(ClientTransport transport,
        SettableFuture<ClientTransport> transportFuture) {
      this.transport = transport;
      this.transportFuture = transportFuture;
    }

    @GuardedBy("lock")
    private boolean isAttachedToActiveTransport() {
      return activeTransportFuture == transportFuture;
    }

    @Override
    public void transportReady() {
      synchronized (lock) {
        log.log(Level.INFO, "Transport {0} for {1} is ready", new Object[] {transport, server});
        Preconditions.checkState(transportFuture.isDone(), "the transport future is not done");
        if (isAttachedToActiveTransport()) {
          reconnectPolicy = null;
        }
      }
      loadBalancer.transportReady(server, transport);
    }

    @Override
    public void transportShutdown(Status s) {
      synchronized (lock) {
        log.log(Level.INFO, "Transport {0} for {1} is being shutdown",
            new Object[] {transport, server});
        Preconditions.checkState(transportFuture.isDone(), "the transport future is not done");
        if (isAttachedToActiveTransport()) {
          createActiveTransportFuture();
        }
      }
      loadBalancer.transportShutdown(server, transport, s);
    }

    @Override
    public void transportTerminated() {
      boolean runCallback = false;
      synchronized (lock) {
        log.log(Level.INFO, "Transport {0} for {1} is terminated",
            new Object[] {transport, server});
        Preconditions.checkState(!isAttachedToActiveTransport(),
            "Listener is still attached to activeTransportFuture. "
            + "Seems transportTerminated was not called.");
        transports.remove(transport);
        if (shutdown && transports.isEmpty()) {
          runCallback = true;
        }
      }
      if (runCallback) {
        callback.onTerminated();
      }
    }
  }

  interface Callback {
    void onTerminated();
  }
}
