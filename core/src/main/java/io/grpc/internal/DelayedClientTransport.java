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

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;

import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;

import java.util.ArrayList;
import java.util.Collection;
import java.util.HashSet;
import java.util.concurrent.Executor;

import javax.annotation.concurrent.GuardedBy;

/**
 * A client transport that queues requests before a real transport is available. When a backing
 * transport is later provided, this class delegates to it.
 *
 * <p>This transport owns the streams that it has created before {@link #setTransport} is
 * called. When {@link #setTransport} is called, the ownership of pending streams and subsequent new
 * streams are transferred to the given transport, thus this transport won't own any stream.
 */
class DelayedClientTransport implements ManagedClientTransport {
  private final Object lock = new Object();

  private Listener listener;
  /** 'lock' must be held when assigning to delegate. */
  private volatile ClientTransport delegate;

  @GuardedBy("lock")
  private Collection<PendingStream> pendingStreams = new HashSet<PendingStream>();
  @GuardedBy("lock")
  private Collection<PendingPing> pendingPings = new ArrayList<PendingPing>();
  /**
   * When shutdown == true and pendingStreams == null, then the transport is considered terminated.
   */
  @GuardedBy("lock")
  private boolean shutdown;

  @Override
  public void start(Listener listener) {
    this.listener = Preconditions.checkNotNull(listener, "listener");
  }

  @Override
  public ClientStream newStream(MethodDescriptor<?, ?> method, Metadata headers) {
    ClientTransport transport = delegate;
    if (transport != null) {
      return transport.newStream(method, headers);
    }
    synchronized (lock) {
      // Check again, since it may have changed while waiting for lock
      transport = delegate;
      if (transport != null) {
        return transport.newStream(method, headers);
      }
      if (!shutdown) {
        PendingStream pendingStream = new PendingStream(method, headers);
        pendingStreams.add(pendingStream);
        return pendingStream;
      }
    }
    DelayedStream stream = new DelayedStream();
    stream.setError(Status.UNAVAILABLE.withDescription("transport shutdown"));
    return stream;
  }

  public void ping(final PingCallback callback, Executor executor) {
    ClientTransport transport = delegate;
    if (transport != null) {
      transport.ping(callback, executor);
      return;
    }
    synchronized (lock) {
      // Check again, since it may have changed while waiting for lock
      transport = delegate;
      if (transport != null) {
        transport.ping(callback, executor);
        return;
      }
      if (!shutdown) {
        PendingPing pendingPing = new PendingPing(callback, executor);
        pendingPings.add(pendingPing);
        return;
      }
    }
    executor.execute(new Runnable() {
        @Override public void run() {
          callback.onFailure(
              Status.UNAVAILABLE.withDescription("transport shutdown").asException());
        }
      });
  }

  /**
   * Prevents creating any new streams until {@link #setTransport} is called. Buffered streams are
   * not failed, so if {@link #shutdown} is called when {@link #setTransport} has not been called,
   * you still need to call {@link setTransport} to make this transport terminated.
   */
  @Override
  public void shutdown() {
    synchronized (lock) {
      if (shutdown) {
        return;
      }
      shutdown = true;
      listener.transportShutdown(
          Status.OK.withDescription("Channel requested transport to shut down"));
      if (pendingStreams == null || pendingStreams.isEmpty()) {
        pendingStreams = null;
        listener.transportTerminated();
      }
    }
  }

  /**
   * Shuts down this transport and cancels all streams that it owns, hence immediately terminates
   * this transport.
   */
  public void shutdownNow(Status status) {
    shutdown();
    Collection<PendingStream> savedPendingStreams = null;
    synchronized (lock) {
      if (pendingStreams != null) {
        savedPendingStreams = pendingStreams;
        pendingStreams = null;
      }
    }
    if (savedPendingStreams != null) {
      for (PendingStream stream : savedPendingStreams) {
        stream.setError(status);
      }
      listener.transportTerminated();
    }
    // If savedPendingStreams == null, transportTerminated() has already been called in shutdown().
  }

  /**
   * Transfers all the pending and future requests and pings to the given transport.
   *
   * <p>May only be called after {@link #start(Listener)}.
   *
   * <p>{@code transport} will be used for all future calls to {@link #newStream}, even if this
   * transport is {@link #shutdown}.
   */
  public void setTransport(ClientTransport transport) {
    Collection<PendingStream> savedPendingStreams;
    synchronized (lock) {
      Preconditions.checkState(delegate == null, "setTransport already called");
      Preconditions.checkState(listener != null, "start() not called");
      delegate = Preconditions.checkNotNull(transport, "transport");
      for (PendingPing ping : pendingPings) {
        ping.createRealPing(transport);
      }
      pendingPings = null;
      if (shutdown && pendingStreams != null) {
        listener.transportTerminated();
      }
      savedPendingStreams = pendingStreams;
      pendingStreams = null;
      if (!shutdown) {
        listener.transportReady();
      }
    }
    // createRealStream may be expensive, so we run it outside of the lock.  It will start real
    // streams on the transport. If there are pending requests, they will be serialized too, which
    // may be expensive.
    // TODO(zhangkun83): may consider doing it in a different thread.
    if (savedPendingStreams != null) {
      for (PendingStream stream : savedPendingStreams) {
        stream.createRealStream(transport);
      }
    }
  }

  @VisibleForTesting
  int getPendingStreamsCount() {
    synchronized (lock) {
      return pendingStreams == null ? 0 : pendingStreams.size();
    }
  }

  private class PendingStream extends DelayedStream {
    private final MethodDescriptor<?, ?> method;
    private final Metadata headers;

    private PendingStream(MethodDescriptor<?, ?> method, Metadata headers) {
      this.method = method;
      this.headers = headers;
    }

    private void createRealStream(ClientTransport transport) {
      setStream(transport.newStream(method, headers));
    }

    // TODO(zhangkun83): DelayedStream.setError() doesn't have a clearly-defined semantic to be
    // overriden. Make it clear or find another method to override.
    @Override
    void setError(Status reason) {
      synchronized (lock) {
        if (pendingStreams != null) {
          pendingStreams.remove(this);
          if (shutdown && pendingStreams.isEmpty()) {
            pendingStreams = null;
            listener.transportTerminated();
          }
        }
      }
      super.setError(reason);
    }
  }

  private static class PendingPing {
    private final PingCallback callback;
    private final Executor executor;

    public PendingPing(PingCallback callback, Executor executor) {
      this.callback = callback;
      this.executor = executor;
    }

    public void createRealPing(ClientTransport transport) {
      try {
        transport.ping(callback, executor);
      } catch (final UnsupportedOperationException ex) {
        executor.execute(new Runnable() {
          @Override
          public void run() {
            callback.onFailure(ex);
          }
        });
      }
    }
  }
}
