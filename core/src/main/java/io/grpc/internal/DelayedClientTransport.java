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
import com.google.common.base.Supplier;
import com.google.common.base.Suppliers;

import io.grpc.CallOptions;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;

import java.util.ArrayList;
import java.util.Collection;
import java.util.Iterator;
import java.util.LinkedHashSet;
import java.util.concurrent.Executor;

import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;

/**
 * A client transport that queues requests before a real transport is available. When a backing
 * transport supplier is later provided, this class delegates to the transports from it.
 *
 * <p>This transport owns the streams that it has created before {@link #setTransport} is
 * called. When {@link #setTransport} is called, the ownership of pending streams and subsequent new
 * streams are transferred to the given transport, thus this transport won't own any stream.
 */
class DelayedClientTransport implements ManagedClientTransport {
  private final Object lock = new Object();

  private final Executor streamCreationExecutor;

  private Listener listener;
  /** 'lock' must be held when assigning to transportSupplier. */
  private volatile Supplier<ClientTransport> transportSupplier;

  @GuardedBy("lock")
  private Collection<PendingStream> pendingStreams = new LinkedHashSet<PendingStream>();
  @GuardedBy("lock")
  private Collection<PendingPing> pendingPings = new ArrayList<PendingPing>();
  /**
   * When shutdown == true and pendingStreams == null, then the transport is considered terminated.
   */
  @GuardedBy("lock")
  private boolean shutdown;

  /**
   * The delayed client transport will come into a back-off interval if it fails to establish a real
   * transport for all addresses, namely the channel is in TRANSIENT_FAILURE. When in a back-off
   * interval, {@code backoffStatus != null}.
   *
   * <p>If the transport is in a back-off interval, then all fail fast streams (including the
   * pending as well as new ones) will fail immediately. New non-fail fast streams can be created as
   * {@link PendingStream} and will keep pending during this back-off period.
   */
  @GuardedBy("lock")
  @Nullable
  private Status backoffStatus;

  DelayedClientTransport(Executor streamCreationExecutor) {
    this.streamCreationExecutor = streamCreationExecutor;
  }

  @Override
  public void start(Listener listener) {
    this.listener = Preconditions.checkNotNull(listener, "listener");
  }

  /**
   * If the transport has acquired a transport {@link Supplier}, then returned stream is delegated
   * from its supplier.
   *
   * <p>If the new stream to be created is with fail fast call option and the delayed transport is
   * in a back-off interval, then a {@link FailingClientStream} is returned.
   *
   * <p>If it is not the above cases and the delayed transport is not shutdown, then a
   * {@link PendingStream} is returned; if the transport is shutdown, then a
   * {@link FailingClientStream} is returned.
   */
  @Override
  public ClientStream newStream(MethodDescriptor<?, ?> method, Metadata headers, CallOptions
      callOptions) {
    Supplier<ClientTransport> supplier = transportSupplier;
    if (supplier == null) {
      synchronized (lock) {
        // Check again, since it may have changed while waiting for lock
        supplier = transportSupplier;
        if (supplier == null && !shutdown) {
          if (backoffStatus != null && !callOptions.isWaitForReady()) {
            return new FailingClientStream(backoffStatus);
          }
          PendingStream pendingStream = new PendingStream(method, headers, callOptions);
          pendingStreams.add(pendingStream);
          if (pendingStreams.size() == 1) {
            listener.transportInUse(true);
          }
          return pendingStream;
        }
      }
    }
    if (supplier != null) {
      return supplier.get().newStream(method, headers, callOptions);
    }
    return new FailingClientStream(Status.UNAVAILABLE.withDescription("transport shutdown"));
  }

  @Override
  public ClientStream newStream(MethodDescriptor<?, ?> method, Metadata headers) {
    return newStream(method, headers, CallOptions.DEFAULT);
  }

  @Override
  public void ping(final PingCallback callback, Executor executor) {
    Supplier<ClientTransport> supplier = transportSupplier;
    if (supplier == null) {
      synchronized (lock) {
        // Check again, since it may have changed while waiting for lock
        supplier = transportSupplier;
        if (supplier == null && !shutdown) {
          PendingPing pendingPing = new PendingPing(callback, executor);
          pendingPings.add(pendingPing);
          return;
        }
      }
    }
    if (supplier != null) {
      supplier.get().ping(callback, executor);
      return;
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
   * you still need to call {@link #setTransport} to make this transport terminated.
   */
  @Override
  public void shutdown() {
    synchronized (lock) {
      if (shutdown) {
        return;
      }
      shutdown = true;
      listener.transportShutdown(
          Status.UNAVAILABLE.withDescription("Channel requested transport to shut down"));
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
  @Override
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
        stream.cancel(status);
      }
      listener.transportTerminated();
    }
    // If savedPendingStreams == null, transportTerminated() has already been called in shutdown().
  }

  /**
   * Transfers all the pending and future streams and pings to the given transport.
   *
   * <p>May only be called after {@link #start(Listener)}.
   *
   * <p>{@code transport} will be used for all future calls to {@link #newStream}, even if this
   * transport is {@link #shutdown}.
   */
  public void setTransport(ClientTransport transport) {
    Preconditions.checkArgument(this != transport,
        "delayed transport calling setTransport on itself");
    setTransportSupplier(Suppliers.ofInstance(transport));
  }

  /**
   * Transfers all the pending and future streams and pings to the transports from the given {@link
   * Supplier}.
   *
   * <p>May only be called after {@link #start}. No effect if already called.
   *
   * <p>Each stream or ping will result in an invocation to {@link Supplier#get} once. The supplier
   * will be used for all future calls to {@link #newStream}, even if this transport is {@link
   * #shutdown}.
   */
  public void setTransportSupplier(final Supplier<ClientTransport> supplier) {
    synchronized (lock) {
      if (transportSupplier != null) {
        return;
      }
      Preconditions.checkState(listener != null, "start() not called");
      transportSupplier = Preconditions.checkNotNull(supplier, "supplier");
      for (PendingPing ping : pendingPings) {
        ping.createRealPing(supplier.get());
      }
      pendingPings = null;
      if (shutdown && pendingStreams != null) {
        listener.transportTerminated();
      }
      if (pendingStreams != null && !pendingStreams.isEmpty()) {
        final Collection<PendingStream> savedPendingStreams = pendingStreams;
        // createRealStream may be expensive. It will start real streams on the transport. If there
        // are pending requests, they will be serialized too, which may be expensive. Since we are
        // now on transport thread, we need to offload the work to an executor.
        streamCreationExecutor.execute(new Runnable() {
            @Override public void run() {
              for (final PendingStream stream : savedPendingStreams) {
                stream.createRealStream(supplier.get());
              }
              // TODO(zhangkun83): some transports (e.g., netty) may have a short delay between
              // stream.start() and transportInUse(true). If netty's transportInUse(true) is called
              // after the delayed transport's transportInUse(false), the channel may have a brief
              // period where all transports are not in-use, and may go to IDLE mode unexpectedly if
              // the IDLE timeout is shorter (e.g., 0) than that brief period. Maybe we should
              // have a minimum IDLE timeout?
              synchronized (lock) {
                listener.transportInUse(false);
              }
            }
        });
      }
      pendingStreams = null;
      if (!shutdown) {
        listener.transportReady();
      }
    }
  }

  public boolean hasPendingStreams() {
    synchronized (lock) {
      return pendingStreams != null && !pendingStreams.isEmpty();
    }
  }

  @VisibleForTesting
  int getPendingStreamsCount() {
    synchronized (lock) {
      return pendingStreams == null ? 0 : pendingStreams.size();
    }
  }

  /**
   * True return value indicates that the delayed transport is in a back-off interval (in
   * TRANSIENT_FAILURE), that all fail fast streams (including pending as well as new ones) should
   * fail immediately, and that non-fail fast streams can be created as {@link PendingStream} and
   * should keep pending during this back-off period.
   */
  @VisibleForTesting
  boolean isInBackoffPeriod() {
    synchronized (lock) {
      return backoffStatus != null;
    }
  }

  /**
   * Is only called at the beginning of {@link TransportSet#scheduleBackoff}.
   *
   * <p>Does jobs at the beginning of the back-off:
   *
   * <p>sets {@link #backoffStatus};
   *
   * <p>sets all pending streams with a fail fast call option of the delayed transport as
   * {@link FailingClientStream}s, and removes them from the list of pending streams of the
   * transport.
   *
   * @param status the causal status for triggering back-off.
   */
  void startBackoff(final Status status) {
    synchronized (lock) {
      Preconditions.checkState(backoffStatus == null);
      backoffStatus = Status.UNAVAILABLE.withDescription("Channel in TRANSIENT_FAILURE state")
          .withCause(status.asRuntimeException());
      final ArrayList<PendingStream> failFastPendingStreams = new ArrayList<PendingStream>();
      if (pendingStreams != null && !pendingStreams.isEmpty()) {
        final Iterator<PendingStream> it = pendingStreams.iterator();
        while (it.hasNext()) {
          PendingStream stream = it.next();
          if (!stream.callOptions.isWaitForReady()) {
            failFastPendingStreams.add(stream);
            it.remove();
          }
        }
        streamCreationExecutor.execute(new Runnable() {
          @Override
          public void run() {
            for (PendingStream stream : failFastPendingStreams) {
              stream.setStream(new FailingClientStream(status));
            }
          }
        });
      }
    }
  }

  /**
   * Is only called at the beginning of the callback function of {@code endOfCurrentBackoff} in the
   * {@link TransportSet#scheduleBackoff} method.
   */
  void endBackoff() {
    synchronized (lock) {
      backoffStatus = null;
    }
  }

  @Override
  public String getLogId() {
    return GrpcUtil.getLogId(this);
  }

  @VisibleForTesting
  @Nullable
  Supplier<ClientTransport> getTransportSupplier() {
    return transportSupplier;
  }

  private class PendingStream extends DelayedStream {
    private final MethodDescriptor<?, ?> method;
    private final Metadata headers;
    private final CallOptions callOptions;

    private PendingStream(MethodDescriptor<?, ?> method, Metadata headers,
        CallOptions callOptions) {
      this.method = method;
      this.headers = headers;
      this.callOptions = callOptions;
    }

    private void createRealStream(ClientTransport transport) {
      setStream(transport.newStream(method, headers, callOptions));
    }

    @Override
    public void cancel(Status reason) {
      super.cancel(reason);
      synchronized (lock) {
        if (pendingStreams != null) {
          boolean justRemovedAnElement = pendingStreams.remove(this);
          if (pendingStreams.isEmpty() && justRemovedAnElement) {
            listener.transportInUse(false);
            if (shutdown) {
              pendingStreams = null;
              listener.transportTerminated();
            }
          }
        }
      }
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
