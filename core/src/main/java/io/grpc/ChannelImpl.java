/*
 * Copyright 2014, Google Inc. All rights reserved.
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

package io.grpc;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;

import io.grpc.ClientCallImpl.ClientTransportProvider;
import io.grpc.transport.ClientTransport;
import io.grpc.transport.ClientTransport.PingCallback;
import io.grpc.transport.ClientTransportFactory;
import io.grpc.transport.HttpUtil;

import java.util.ArrayList;
import java.util.Collection;
import java.util.List;
import java.util.concurrent.Executor;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.TimeUnit;
import java.util.logging.Logger;

import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;
import javax.annotation.concurrent.ThreadSafe;

/** A communication channel for making outgoing RPCs. */
@ThreadSafe
public final class ChannelImpl extends Channel {
  private static final Logger log = Logger.getLogger(ChannelImpl.class.getName());
  private final ClientTransportFactory transportFactory;
  private final ExecutorService executor;
  private final String userAgent;
  private final Object lock = new Object();

  /**
   * Executor that runs deadline timers for requests.
   */
  private ScheduledExecutorService deadlineCancellationExecutor;
  /**
   * We delegate to this channel, so that we can have interceptors as necessary. If there aren't
   * any interceptors this will just be {@link RealChannel}.
   */
  private final Channel interceptorChannel;
  /**
   * All transports that are not stopped. At the very least {@link #activeTransport} will be
   * present, but previously used transports that still have streams or are stopping may also be
   * present.
   */
  @GuardedBy("lock")
  private Collection<ClientTransport> transports = new ArrayList<ClientTransport>();
  /**
   * The transport for new outgoing requests. 'this' lock must be held when assigning to
   * activeTransport.
   */
  private volatile ClientTransport activeTransport;
  @GuardedBy("lock")
  private boolean shutdown;
  @GuardedBy("lock")
  private boolean terminated;
  private Runnable terminationRunnable;

  private final ClientTransportProvider transportProvider = new ClientTransportProvider() {
    @Override
    public ClientTransport get() {
      return obtainActiveTransport();
    }
  };

  ChannelImpl(ClientTransportFactory transportFactory, ExecutorService executor,
      @Nullable String userAgent, List<ClientInterceptor> interceptors) {
    this.transportFactory = transportFactory;
    this.executor = executor;
    this.userAgent = userAgent;
    this.interceptorChannel = ClientInterceptors.intercept(new RealChannel(), interceptors);
    deadlineCancellationExecutor = SharedResourceHolder.get(TIMER_SERVICE);
  }

  /** Hack to allow executors to auto-shutdown. Not for general use. */
  // TODO(ejona86): Replace with a real API.
  void setTerminationRunnable(Runnable runnable) {
    this.terminationRunnable = runnable;
  }

  /**
   * Initiates an orderly shutdown in which preexisting calls continue but new calls are immediately
   * cancelled.
   */
  public ChannelImpl shutdown() {
    synchronized (lock) {
      if (shutdown) {
        return this;
      }
      shutdown = true;
      // After shutdown there are no new calls, so no new cancellation tasks are needed
      deadlineCancellationExecutor =
          SharedResourceHolder.release(TIMER_SERVICE, deadlineCancellationExecutor);
      if (activeTransport != null) {
        activeTransport.shutdown();
        activeTransport = null;
      } else if (transports.isEmpty()) {
        terminated = true;
        lock.notifyAll();
        if (terminationRunnable != null) {
          terminationRunnable.run();
        }
      }
      return this;
    }
  }

  /**
   * Initiates a forceful shutdown in which preexisting and new calls are cancelled. Although
   * forceful, the shutdown process is still not instantaneous; {@link #isTerminated()} will likely
   * return {@code false} immediately after this method returns.
   *
   * <p>NOT YET IMPLEMENTED. This method currently behaves identically to shutdown().
   */
  // TODO(ejona86): cancel preexisting calls.
  public ChannelImpl shutdownNow() {
    synchronized (lock) {
      shutdown();
      return this;
    }
  }

  /**
   * Returns whether the channel is shutdown. Shutdown channels immediately cancel any new calls,
   * but may still have some calls being processed.
   *
   * @see #shutdown()
   * @see #isTerminated()
   */
  public boolean isShutdown() {
    synchronized (lock) {
      return shutdown;
    }
  }

  /**
   * Waits for the channel to become terminated, giving up if the timeout is reached.
   *
   * @return whether the channel is terminated, as would be done by {@link #isTerminated()}.
   */
  public boolean awaitTerminated(long timeout, TimeUnit unit) throws InterruptedException {
    synchronized (lock) {
      long timeoutNanos = unit.toNanos(timeout);
      long endTimeNanos = System.nanoTime() + timeoutNanos;
      while (!terminated && (timeoutNanos = endTimeNanos - System.nanoTime()) > 0) {
        TimeUnit.NANOSECONDS.timedWait(lock, timeoutNanos);
      }
      return terminated;
    }
  }

  /**
   * Returns whether the channel is terminated. Terminated channels have no running calls and
   * relevant resources released (like TCP connections).
   *
   * @see #isShutdown()
   */
  public boolean isTerminated() {
    synchronized (lock) {
      return terminated;
    }
  }

  /**
   * Pings the remote endpoint to verify that the transport is still active. When an acknowledgement
   * is received, the given callback will be invoked using the given executor.
   *
   * <p>If the underlying transport has no mechanism by when to send a ping, this method may throw
   * an {@link UnsupportedOperationException}. The operation may
   * {@linkplain PingCallback#pingFailed(Throwable) fail} due to transient transport errors. In
   * that case, trying again may succeed.
   *
   * @see ClientTransport#ping(PingCallback, Executor)
   */
  public void ping(final PingCallback callback, final Executor executor) {
    try {
      obtainActiveTransport().ping(callback, executor);
    } catch (final RuntimeException ex) {
      executor.execute(new Runnable() {
        @Override
        public void run() {
          callback.pingFailed(ex);
        }
      });
    }
  }

  /*
   * Creates a new outgoing call on the channel.
   */
  @Override
  public <ReqT, RespT> ClientCall<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method,
      CallOptions callOptions) {
    return interceptorChannel.newCall(method, callOptions);
  }

  private ClientTransport obtainActiveTransport() {
    ClientTransport savedActiveTransport = activeTransport;
    if (savedActiveTransport != null) {
      return savedActiveTransport;
    }
    synchronized (lock) {
      if (shutdown) {
        return null;
      }
      savedActiveTransport = activeTransport;
      if (savedActiveTransport != null) {
        return savedActiveTransport;
      }
      ClientTransport newActiveTransport = transportFactory.newClientTransport();
      transports.add(newActiveTransport);
      boolean failed = true;
      try {
        newActiveTransport.start(new TransportListener(newActiveTransport));
        failed = false;
      } finally {
        if (failed) {
          transports.remove(newActiveTransport);
        }
      }
      // It's possible that start() called transportShutdown() and transportTerminated(). If so, we
      // wouldn't want to make it the active transport.
      if (transports.contains(newActiveTransport)) {
        // start() must return before we set activeTransport, since activeTransport is accessed
        // without a lock.
        activeTransport = newActiveTransport;
      }
      return newActiveTransport;
    }
  }

  private class RealChannel extends Channel {
    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method,
        CallOptions callOptions) {
      return new ClientCallImpl<ReqT, RespT>(
          method,
          new SerializingExecutor(executor),
          callOptions,
          transportProvider,
          deadlineCancellationExecutor)
              .setUserAgent(userAgent);
    }
  }

  private class TransportListener implements ClientTransport.Listener {
    private final ClientTransport transport;

    public TransportListener(ClientTransport transport) {
      this.transport = transport;
    }

    @Override
    public void transportShutdown(Status s) {
      // TODO(carl-mastrangelo): use this status to determine if and how to retry the connection.
      synchronized (lock) {
        if (activeTransport == transport) {
          activeTransport = null;
        }
      }
    }

    @Override
    public void transportTerminated() {
      synchronized (lock) {
        if (activeTransport == transport) {
          log.warning("transportTerminated called without previous transportShutdown");
          activeTransport = null;
        }
        // TODO(notcarl): replace this with something more meaningful
        transportShutdown(Status.UNKNOWN);
        transports.remove(transport);
        if (shutdown && transports.isEmpty()) {
          if (terminated) {
            log.warning("transportTerminated called after already terminated");
          }
          terminated = true;
          lock.notifyAll();
          if (terminationRunnable != null) {
            terminationRunnable.run();
          }
        }
      }
    }
  }

  /**
   * Intended for internal use only.
   */
  // TODO(johnbcoughlin) make this package private when we can do so with the tests.
  @VisibleForTesting
  public static final Metadata.Key<Long> TIMEOUT_KEY =
      Metadata.Key.of(HttpUtil.TIMEOUT, new TimeoutMarshaller());

  /**
   * Marshals a microseconds representation of the timeout to and from a string representation,
   * consisting of an ASCII decimal representation of a number with at most 8 digits, followed by a
   * unit:
   * u = microseconds
   * m = milliseconds
   * S = seconds
   * M = minutes
   * H = hours
   *
   * <p>The representation is greedy with respect to precision. That is, 2 seconds will be
   * represented as `2000000u`.</p>
   *
   * <p>See <a href="https://github.com/grpc/grpc-common/blob/master/PROTOCOL-HTTP2.md#requests">the
   * request header definition</a></p>
   */
  @VisibleForTesting
  static class TimeoutMarshaller implements Metadata.AsciiMarshaller<Long> {
    @Override
    public String toAsciiString(Long timeoutMicros) {
      Preconditions.checkArgument(timeoutMicros >= 0, "Negative timeout");
      long timeout;
      String timeoutUnit;
      // the smallest integer with 9 digits
      int cutoff = 100000000;
      if (timeoutMicros < cutoff) {
        timeout = timeoutMicros;
        timeoutUnit = "u";
      } else if (timeoutMicros / 1000 < cutoff) {
        timeout = timeoutMicros / 1000;
        timeoutUnit = "m";
      } else if (timeoutMicros / (1000 * 1000) < cutoff) {
        timeout = timeoutMicros / (1000 * 1000);
        timeoutUnit = "S";
      } else if (timeoutMicros / (60 * 1000 * 1000) < cutoff) {
        timeout = timeoutMicros / (60 * 1000 * 1000);
        timeoutUnit = "M";
      } else if (timeoutMicros / (60L * 60L * 1000L * 1000L) < cutoff) {
        timeout = timeoutMicros / (60L * 60L * 1000L * 1000L);
        timeoutUnit = "H";
      } else {
        throw new IllegalArgumentException("Timeout too large");
      }
      return Long.toString(timeout) + timeoutUnit;
    }

    @Override
    public Long parseAsciiString(String serialized) {
      String valuePart = serialized.substring(0, serialized.length() - 1);
      char unit = serialized.charAt(serialized.length() - 1);
      long factor;
      switch (unit) {
        case 'u':
          factor = 1; break;
        case 'm':
          factor = 1000L; break;
        case 'S':
          factor = 1000L * 1000L; break;
        case 'M':
          factor = 60L * 1000L * 1000L; break;
        case 'H':
          factor = 60L * 60L * 1000L * 1000L; break;
        default:
          throw new IllegalArgumentException(String.format("Invalid timeout unit: %s", unit));
      }
      return Long.parseLong(valuePart) * factor;
    }
  }

  private static final SharedResourceHolder.Resource<ScheduledExecutorService> TIMER_SERVICE =
      new SharedResourceHolder.Resource<ScheduledExecutorService>() {
        @Override
        public ScheduledExecutorService create() {
          return Executors.newSingleThreadScheduledExecutor(new ThreadFactory() {
            @Override
            public Thread newThread(Runnable r) {
              Thread thread = new Thread(r);
              thread.setDaemon(true);
              return thread;
            }
          });
        }

        @Override
        public void close(ScheduledExecutorService instance) {
          instance.shutdown();
        }
      };
}
