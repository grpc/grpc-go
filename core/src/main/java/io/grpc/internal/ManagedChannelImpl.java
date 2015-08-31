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

package io.grpc.internal;

import static io.grpc.internal.GrpcUtil.TIMER_SERVICE;

import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.ClientInterceptors;
import io.grpc.ExperimentalApi;
import io.grpc.ManagedChannel;
import io.grpc.MessageEncoding;
import io.grpc.MessageEncoding.Compressor;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.internal.ClientCallImpl.ClientTransportProvider;
import io.grpc.internal.ClientTransport.PingCallback;

import java.util.ArrayList;
import java.util.Collection;
import java.util.List;
import java.util.concurrent.Executor;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.logging.Logger;

import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;
import javax.annotation.concurrent.ThreadSafe;

/** A communication channel for making outgoing RPCs. */
@ThreadSafe
public final class ManagedChannelImpl extends ManagedChannel {
  private static final Logger log = Logger.getLogger(ManagedChannelImpl.class.getName());

  private final ClientTransportFactory transportFactory;
  private final ExecutorService executor;
  private final boolean usingSharedExecutor;
  private final String userAgent;
  private final Object lock = new Object();

  /**
   * Executor that runs deadline timers for requests.
   */
  private ScheduledExecutorService scheduledExecutor;

  // TODO(carl-mastrangelo): Allow clients to pass this in
  private final BackoffPolicy.Provider backoffPolicyProvider =
      new ExponentialBackoffPolicy.Provider();
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

  private long reconnectTimeMillis;
  private BackoffPolicy reconnectPolicy;

  private volatile Compressor defaultCompressor;

  private final ClientTransportProvider transportProvider = new ClientTransportProvider() {
    @Override
    public ClientTransport get() {
      return obtainActiveTransport();
    }
  };

  ManagedChannelImpl(ClientTransportFactory transportFactory, @Nullable ExecutorService executor,
      @Nullable String userAgent, List<ClientInterceptor> interceptors) {
    this.transportFactory = transportFactory;
    this.userAgent = userAgent;
    this.interceptorChannel = ClientInterceptors.intercept(new RealChannel(), interceptors);
    scheduledExecutor = SharedResourceHolder.get(TIMER_SERVICE);

    if (executor == null) {
      usingSharedExecutor = true;
      this.executor = SharedResourceHolder.get(GrpcUtil.SHARED_CHANNEL_EXECUTOR);
    } else {
      usingSharedExecutor = false;
      this.executor = executor;
    }
  }

  /**
   * Sets the default compression method for this Channel.  By default, new calls will use the
   * provided compressor.  Each individual Call can override this by specifying it in CallOptions.
   * If the remote host does not support the message encoding, the call will likely break.  There
   * is currently no provided way to discover what message encodings the remote host supports.
   * @param c The compressor to use.  If {@code null} no compression will by performed.  This is
   *          equivalent to using {@link MessageEncoding#NONE}.  If not null, the Comressor must be
   *          threadsafe.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/492")
  public void setDefaultCompressor(@Nullable Compressor c) {
    defaultCompressor = (c != null) ? c : MessageEncoding.NONE;
  }

  /**
   * Initiates an orderly shutdown in which preexisting calls continue but new calls are immediately
   * cancelled.
   */
  @Override
  public ManagedChannelImpl shutdown() {
    ClientTransport savedActiveTransport;
    synchronized (lock) {
      if (shutdown) {
        return this;
      }
      shutdown = true;
      // After shutdown there are no new calls, so no new cancellation tasks are needed
      scheduledExecutor = SharedResourceHolder.release(TIMER_SERVICE, scheduledExecutor);
      savedActiveTransport = activeTransport;
      if (savedActiveTransport != null) {
        activeTransport = null;
      } else if (transports.isEmpty()) {
        terminated = true;
        lock.notifyAll();
        onChannelTerminated();
      }
    }
    if (savedActiveTransport != null) {
      savedActiveTransport.shutdown();
    }
    return this;
  }

  /**
   * Initiates a forceful shutdown in which preexisting and new calls are cancelled. Although
   * forceful, the shutdown process is still not instantaneous; {@link #isTerminated()} will likely
   * return {@code false} immediately after this method returns.
   *
   * <p>NOT YET IMPLEMENTED. This method currently behaves identically to shutdown().
   */
  // TODO(ejona86): cancel preexisting calls.
  @Override
  public ManagedChannelImpl shutdownNow() {
    shutdown();
    return this;
  }

  @Override
  public boolean isShutdown() {
    synchronized (lock) {
      return shutdown;
    }
  }

  @Override
  public boolean awaitTermination(long timeout, TimeUnit unit) throws InterruptedException {
    synchronized (lock) {
      long timeoutNanos = unit.toNanos(timeout);
      long endTimeNanos = System.nanoTime() + timeoutNanos;
      while (!terminated && (timeoutNanos = endTimeNanos - System.nanoTime()) > 0) {
        TimeUnit.NANOSECONDS.timedWait(lock, timeoutNanos);
      }
      return terminated;
    }
  }

  @Override
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
   * an {@link UnsupportedOperationException}. The operation may {@linkplain
   * io.grpc.internal.ClientTransport.PingCallback#pingFailed(Throwable) fail} due to transient
   * transport errors. In that case, trying again may succeed.
   *
   * @see ClientTransport#ping(ClientTransport.PingCallback, Executor)
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
    boolean hasCodecOverride = callOptions.getCompressor() != null;
    if (!hasCodecOverride && defaultCompressor != MessageEncoding.NONE) {
      callOptions = callOptions.withCompressor(defaultCompressor);
    }
    return interceptorChannel.newCall(method, callOptions);
  }

  @Override
  public String authority() {
    return interceptorChannel.authority();
  }

  private ClientTransport obtainActiveTransport() {
    ClientTransport savedActiveTransport = activeTransport;
    // If we know there is an active transport and we are not in backoff mode, return quickly.
    if (savedActiveTransport != null && !(savedActiveTransport instanceof InactiveTransport)) {
      return savedActiveTransport;
    }
    synchronized (lock) {
      if (shutdown) {
        return null;
      }
      savedActiveTransport = activeTransport;
      if (savedActiveTransport instanceof InactiveTransport) {
        if (System.nanoTime() > TimeUnit.MILLISECONDS.toNanos(reconnectTimeMillis)) {
          // The timeout expired, clear the inactive transport and update the shutdown status to
          // something that is retryable.
          activeTransport = null;
          savedActiveTransport = activeTransport;
        } else {
          // We are still in backoff mode, just return the inactive transport.
          return savedActiveTransport;
        }
      }

      if (savedActiveTransport != null) {
        return savedActiveTransport;
      }
      // There is no active transport, or we just finished backoff.  Create a new transport.
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
          scheduledExecutor)
              .setUserAgent(userAgent);
    }

    @Override
    public String authority() {
      return transportFactory.authority();
    }
  }

  private class TransportListener implements ClientTransport.Listener {
    private final ClientTransport transport;

    public TransportListener(ClientTransport transport) {
      this.transport = transport;
    }

    @Override
    public void transportReady() {
      synchronized (lock) {
        if (activeTransport == transport) {
          reconnectPolicy = null;
        }
      }
    }

    @Override
    public void transportShutdown(Status s) {
      synchronized (lock) {
        if (activeTransport == transport) {
          activeTransport = null;
          // This transport listener was attached to the active transport.
          if (s.isOk()) {
            return;
          }
          // Alright, something bad has happened.
          if (reconnectPolicy == null) {
            // This happens the first time something bad has happened.
            reconnectPolicy = backoffPolicyProvider.get();
            reconnectTimeMillis = TimeUnit.NANOSECONDS.toMillis(System.nanoTime());
          }
          activeTransport = new InactiveTransport(s);
          reconnectTimeMillis += reconnectPolicy.nextBackoffMillis();
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
        transportShutdown(Status.UNKNOWN.withDescription("transport shutdown for unknown reason"));
        transports.remove(transport);
        if (shutdown && transports.isEmpty()) {
          if (terminated) {
            log.warning("transportTerminated called after already terminated");
          }
          terminated = true;
          lock.notifyAll();
          onChannelTerminated();
        }
      }
    }
  }

  /**
   * If we're using the shared executor, returns its reference.
   */
  private void onChannelTerminated() {
    if (usingSharedExecutor) {
      SharedResourceHolder.release(GrpcUtil.SHARED_CHANNEL_EXECUTOR, executor);
    }
    // Release the transport factory so that it can deallocate any resources.
    transportFactory.release();
  }

  private static final class InactiveTransport implements ClientTransport {
    private final Status shutdownStatus;

    private InactiveTransport(Status s) {
      shutdownStatus = s;
    }

    @Override
    public ClientStream newStream(
        MethodDescriptor<?, ?> method, Metadata headers, ClientStreamListener listener) {
      listener.closed(shutdownStatus, new Metadata());
      return new ClientCallImpl.NoopClientStream();
    }

    @Override
    public void start(Listener listener) {
      throw new IllegalStateException();
    }

    @Override
    public void ping(final PingCallback callback, Executor executor) {
      executor.execute(new Runnable() {
        @Override
        public void run() {
          callback.pingFailed(shutdownStatus.asException());
        }
      });
    }

    @Override
    public void shutdown() {
      // no-op
    }
  }
}
