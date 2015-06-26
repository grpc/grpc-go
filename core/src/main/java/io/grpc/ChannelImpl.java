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
import com.google.common.base.Throwables;

import io.grpc.MethodDescriptor.MethodType;
import io.grpc.transport.ClientStream;
import io.grpc.transport.ClientStreamListener;
import io.grpc.transport.ClientTransport;
import io.grpc.transport.ClientTransport.PingCallback;
import io.grpc.transport.ClientTransportFactory;
import io.grpc.transport.HttpUtil;

import java.io.InputStream;
import java.util.ArrayList;
import java.util.Collection;
import java.util.List;
import java.util.concurrent.Executor;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
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

  private static class NoopClientStream implements ClientStream {
    @Override public void writeMessage(InputStream message) {}

    @Override public void flush() {}

    @Override public void cancel(Status reason) {}

    @Override public void halfClose() {}

    @Override public void request(int numMessages) {}

    /**
     * Always returns {@code false}, since this is only used when the startup of the {@link
     * ClientCall} fails (i.e. the {@link ClientCall} is closed).
     */
    @Override public boolean isReady() {
      return false;
    }
  }

  private final ClientTransportFactory transportFactory;
  private final ExecutorService executor;
  private final String userAgent;

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
  @GuardedBy("this")
  private Collection<ClientTransport> transports = new ArrayList<ClientTransport>();
  /**
   * The transport for new outgoing requests. 'this' lock must be held when assigning to
   * activeTransport.
   */
  private volatile ClientTransport activeTransport;
  @GuardedBy("this")
  private boolean shutdown;
  @GuardedBy("this")
  private boolean terminated;
  private Runnable terminationRunnable;

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
  public synchronized ChannelImpl shutdown() {
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
      notifyAll();
      if (terminationRunnable != null) {
        terminationRunnable.run();
      }
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
  public synchronized ChannelImpl shutdownNow() {
    shutdown();
    return this;
  }

  /**
   * Returns whether the channel is shutdown. Shutdown channels immediately cancel any new calls,
   * but may still have some calls being processed.
   *
   * @see #shutdown()
   * @see #isTerminated()
   */
  public synchronized boolean isShutdown() {
    return shutdown;
  }

  /**
   * Waits for the channel to become terminated, giving up if the timeout is reached.
   *
   * @return whether the channel is terminated, as would be done by {@link #isTerminated()}.
   */
  public synchronized boolean awaitTerminated(long timeout, TimeUnit unit)
      throws InterruptedException {
    long timeoutNanos = unit.toNanos(timeout);
    long endTimeNanos = System.nanoTime() + timeoutNanos;
    while (!terminated && (timeoutNanos = endTimeNanos - System.nanoTime()) > 0) {
      TimeUnit.NANOSECONDS.timedWait(this, timeoutNanos);
    }
    return terminated;
  }

  /**
   * Returns whether the channel is terminated. Terminated channels have no running calls and
   * relevant resources released (like TCP connections).
   *
   * @see #isShutdown()
   */
  public synchronized boolean isTerminated() {
    return terminated;
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
    synchronized (this) {
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
      return new CallImpl<ReqT, RespT>(method, new SerializingExecutor(executor), callOptions);
    }
  }

  private class TransportListener implements ClientTransport.Listener {
    private final ClientTransport transport;

    public TransportListener(ClientTransport transport) {
      this.transport = transport;
    }

    @Override
    public void transportShutdown() {
      synchronized (ChannelImpl.this) {
        if (activeTransport == transport) {
          activeTransport = null;
        }
      }
    }

    @Override
    public void transportTerminated() {
      synchronized (ChannelImpl.this) {
        if (activeTransport == transport) {
          log.warning("transportTerminated called without previous transportShutdown");
          activeTransport = null;
        }
        transportShutdown();
        transports.remove(transport);
        if (shutdown && transports.isEmpty()) {
          if (terminated) {
            log.warning("transportTerminated called after already terminated");
          }
          terminated = true;
          ChannelImpl.this.notifyAll();
          if (terminationRunnable != null) {
            terminationRunnable.run();
          }
        }
      }
    }
  }

  private final class CallImpl<ReqT, RespT> extends ClientCall<ReqT, RespT> {
    private final MethodDescriptor<ReqT, RespT> method;
    private final SerializingExecutor callExecutor;
    private final boolean unaryRequest;
    private final CallOptions callOptions;
    private ClientStream stream;
    private volatile ScheduledFuture<?> deadlineCancellationFuture;
    private boolean cancelCalled;
    private boolean halfCloseCalled;

    private CallImpl(MethodDescriptor<ReqT, RespT> method, SerializingExecutor executor,
        CallOptions callOptions) {
      this.method = method;
      this.callExecutor = executor;
      this.unaryRequest = method.getType() == MethodType.UNARY
          || method.getType() == MethodType.SERVER_STREAMING;
      this.callOptions = callOptions;
    }

    @Override
    public void start(Listener<RespT> observer, Metadata.Headers headers) {
      Preconditions.checkState(stream == null, "Already started");
      Long deadlineNanoTime = callOptions.getDeadlineNanoTime();
      ClientStreamListener listener = new ClientStreamListenerImpl(observer, deadlineNanoTime);
      ClientTransport transport;
      try {
        transport = obtainActiveTransport();
      } catch (RuntimeException ex) {
        closeCallPrematurely(listener, Status.fromThrowable(ex));
        return;
      }
      if (transport == null) {
        closeCallPrematurely(listener, Status.UNAVAILABLE.withDescription("Channel is shutdown"));
        return;
      }

      // Fill out timeout on the headers
      headers.removeAll(TIMEOUT_KEY);
      // Convert the deadline to timeout. Timeout is more favorable than deadline on the wire
      // because timeout tolerates the clock difference between machines.
      long timeoutMicros = 0;
      if (deadlineNanoTime != null) {
        timeoutMicros = TimeUnit.NANOSECONDS.toMicros(deadlineNanoTime - System.nanoTime());
        if (timeoutMicros <= 0) {
          closeCallPrematurely(listener, Status.DEADLINE_EXCEEDED);
          return;
        }
        headers.put(TIMEOUT_KEY, timeoutMicros);
      }

      // Fill out the User-Agent header.
      headers.removeAll(HttpUtil.USER_AGENT_KEY);
      if (userAgent != null) {
        headers.put(HttpUtil.USER_AGENT_KEY, userAgent);
      }

      try {
        stream = transport.newStream(method, headers, listener);
      } catch (IllegalStateException ex) {
        // We can race with the transport and end up trying to use a terminated transport.
        // TODO(ejona86): Improve the API to remove the possibility of the race.
        closeCallPrematurely(listener, Status.fromThrowable(ex));
      }
      // Start the deadline timer after stream creation because it will close the stream
      if (deadlineNanoTime != null) {
        deadlineCancellationFuture = startDeadlineTimer(timeoutMicros);
      }
    }

    @Override
    public void request(int numMessages) {
      Preconditions.checkState(stream != null, "Not started");
      stream.request(numMessages);
    }

    @Override
    public void cancel() {
      cancelCalled = true;
      // Cancel is called in exception handling cases, so it may be the case that the
      // stream was never successfully created.
      if (stream != null) {
        stream.cancel(Status.CANCELLED);
      }
    }

    @Override
    public void halfClose() {
      Preconditions.checkState(stream != null, "Not started");
      Preconditions.checkState(!cancelCalled, "call was cancelled");
      Preconditions.checkState(!halfCloseCalled, "call already half-closed");
      halfCloseCalled = true;
      stream.halfClose();
    }

    @Override
    public void sendPayload(ReqT payload) {
      Preconditions.checkState(stream != null, "Not started");
      Preconditions.checkState(!cancelCalled, "call was cancelled");
      Preconditions.checkState(!halfCloseCalled, "call was half-closed");
      boolean failed = true;
      try {
        InputStream payloadIs = method.streamRequest(payload);
        stream.writeMessage(payloadIs);
        failed = false;
      } finally {
        // TODO(notcarl): Find out if payloadIs needs to be closed.
        if (failed) {
          cancel();
        }
      }
      // For unary requests, we don't flush since we know that halfClose should be coming soon. This
      // allows us to piggy-back the END_STREAM=true on the last payload frame without opening the
      // possibility of broken applications forgetting to call halfClose without noticing.
      if (!unaryRequest) {
        stream.flush();
      }
    }

    @Override
    public boolean isReady() {
      return stream.isReady();
    }

    /**
     * Close the call before the stream is created.
     */
    private void closeCallPrematurely(ClientStreamListener listener, Status status) {
      Preconditions.checkState(stream == null, "Stream already created");
      stream = new NoopClientStream();
      listener.closed(status, new Metadata.Trailers());
    }

    private ScheduledFuture<?> startDeadlineTimer(long timeoutMicros) {
      return deadlineCancellationExecutor.schedule(new Runnable() {
        @Override
        public void run() {
          stream.cancel(Status.DEADLINE_EXCEEDED);
        }
      }, timeoutMicros, TimeUnit.MICROSECONDS);
    }

    private class ClientStreamListenerImpl implements ClientStreamListener {
      private final Listener<RespT> observer;
      private final Long deadlineNanoTime;
      private boolean closed;

      public ClientStreamListenerImpl(Listener<RespT> observer, Long deadlineNanoTime) {
        Preconditions.checkNotNull(observer);
        this.observer = observer;
        this.deadlineNanoTime = deadlineNanoTime;
      }

      @Override
      public void headersRead(final Metadata.Headers headers) {
        callExecutor.execute(new Runnable() {
          @Override
          public void run() {
            try {
              if (closed) {
                return;
              }

              observer.onHeaders(headers);
            } catch (Throwable t) {
              cancel();
              throw Throwables.propagate(t);
            }
          }
        });
      }

      @Override
      public void messageRead(final InputStream message) {
        callExecutor.execute(new Runnable() {
          @Override
          public void run() {
            try {
              if (closed) {
                return;
              }

              try {
                observer.onPayload(method.parseResponse(message));
              } finally {
                message.close();
              }
            } catch (Throwable t) {
              cancel();
              throw Throwables.propagate(t);
            }
          }
        });
      }

      @Override
      public void closed(Status status, Metadata.Trailers trailers) {
        if (status.getCode() == Status.Code.CANCELLED && deadlineNanoTime != null) {
          // When the server's deadline expires, it can only reset the stream with CANCEL and no
          // description. Since our timer may be delayed in firing, we double-check the deadline and
          // turn the failure into the likely more helpful DEADLINE_EXCEEDED status. This is always
          // safe, but we avoid wasting resources getting the nanoTime() when unnecessary.
          if (deadlineNanoTime <= System.nanoTime()) {
            status = Status.DEADLINE_EXCEEDED;
            // Replace trailers to prevent mixing sources of status and trailers.
            trailers = new Metadata.Trailers();
          }
        }
        final Status savedStatus = status;
        final Metadata.Trailers savedTrailers = trailers;
        callExecutor.execute(new Runnable() {
          @Override
          public void run() {
            closed = true;
            // manually optimize the volatile read
            ScheduledFuture<?> future = deadlineCancellationFuture;
            if (future != null) {
              future.cancel(false);
            }
            observer.onClose(savedStatus, savedTrailers);
          }
        });
      }

      @Override
      public void onReady() {
        callExecutor.execute(new Runnable() {
          @Override
          public void run() {
            observer.onReady();
          }
        });
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
