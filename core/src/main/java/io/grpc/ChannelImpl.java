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

import com.google.common.base.Preconditions;
import com.google.common.base.Throwables;

import io.grpc.transport.ClientStream;
import io.grpc.transport.ClientStreamListener;
import io.grpc.transport.ClientTransport;
import io.grpc.transport.ClientTransportFactory;

import java.io.IOException;
import java.io.InputStream;
import java.util.ArrayList;
import java.util.Collection;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.logging.Logger;

import javax.annotation.concurrent.GuardedBy;
import javax.annotation.concurrent.ThreadSafe;

/** A communication channel for making outgoing RPCs. */
@ThreadSafe
public final class ChannelImpl implements Channel {

  private static final Logger log = Logger.getLogger(ChannelImpl.class.getName());

  private static class NoopClientStream implements ClientStream {
    @Override public void writeMessage(InputStream message, int length, Runnable accepted) {}
    @Override public void flush() {}
    @Override public void cancel() {}
    @Override public void halfClose() {}
    @Override public void request(int numMessages) {}
  }

  private final ClientTransportFactory transportFactory;
  private final ExecutorService executor;
  /**
   * All transports that are not stopped. At the very least {@link #activeTransport} will be
   * present, but previously used transports that still have streams or are stopping may also be
   * present.
   */
  @GuardedBy("this")
  private Collection<ClientTransport> transports = new ArrayList<ClientTransport>();
  /** The transport for new outgoing requests. */
  @GuardedBy("this")
  private ClientTransport activeTransport;
  @GuardedBy("this")
  private boolean shutdown;
  @GuardedBy("this")
  private boolean terminated;
  private Runnable terminationRunnable;

  public ChannelImpl(ClientTransportFactory transportFactory, ExecutorService executor) {
    this.transportFactory = transportFactory;
    this.executor = executor;
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
   * Creates a new outgoing call on the channel.
   */
  @Override
  public <ReqT, RespT> Call<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method) {
    return new CallImpl<ReqT, RespT>(method, new SerializingExecutor(executor));
  }

  private synchronized ClientTransport obtainActiveTransport() {
    if (shutdown) {
      return null;
    }
    if (activeTransport != null) {
      return activeTransport;
    }
    activeTransport = transportFactory.newClientTransport();
    transports.add(activeTransport);
    activeTransport.start(new TransportListener(activeTransport));
    return activeTransport;
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

  private class CallImpl<ReqT, RespT> extends Call<ReqT, RespT> {
    private final MethodDescriptor<ReqT, RespT> method;
    private final SerializingExecutor callExecutor;
    private final boolean unaryRequest;
    private ClientStream stream;

    public CallImpl(MethodDescriptor<ReqT, RespT> method, SerializingExecutor executor) {
      this.method = method;
      this.callExecutor = executor;
      this.unaryRequest = method.getType() == MethodType.UNARY
          || method.getType() == MethodType.SERVER_STREAMING;
    }

    @Override
    public void start(Listener<RespT> observer, Metadata.Headers headers) {
      Preconditions.checkState(stream == null, "Already started");
      ClientStreamListener listener = new ClientStreamListenerImpl(observer);
      ClientTransport transport = obtainActiveTransport();
      if (transport == null) {
        stream = new NoopClientStream();
        listener.closed(Status.CANCELLED.withDescription("Channel is shutdown"),
            new Metadata.Trailers());
        return;
      }
      try {
        stream = transport.newStream(method, headers, listener);
      } catch (IllegalStateException ex) {
        // We can race with the transport and end up trying to use a terminated transport.
        // TODO(ejona86): Improve the API to remove the possibility of the race.
        stream = new NoopClientStream();
        listener.closed(Status.fromThrowable(ex), new Metadata.Trailers());
        return;
      }
    }

    @Override
    public void request(int numMessages) {
      stream.request(numMessages);
    }

    @Override
    public void cancel() {
      // Cancel is called in exception handling cases, so it may be the case that the
      // stream was never successfully created.
      if (stream != null) {
        stream.cancel();
      }
    }

    @Override
    public void halfClose() {
      Preconditions.checkState(stream != null, "Not started");
      stream.halfClose();
    }

    private int available(InputStream is) {
      try {
        return is.available();
      } catch (IOException ex) {
        throw new RuntimeException(ex);
      }
    }

    @Override
    public void sendPayload(ReqT payload) {
      Preconditions.checkState(stream != null, "Not started");
      boolean failed = true;
      try {
        InputStream payloadIs = method.streamRequest(payload);
        stream.writeMessage(payloadIs, available(payloadIs), null);
        failed = false;
      } finally {
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

    private class ClientStreamListenerImpl implements ClientStreamListener {
      private final Listener<RespT> observer;
      private boolean closed;

      public ClientStreamListenerImpl(Listener<RespT> observer) {
        Preconditions.checkNotNull(observer);
        this.observer = observer;
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
      public void closed(final Status status, final Metadata.Trailers trailers) {
        callExecutor.execute(new Runnable() {
          @Override
          public void run() {
            closed = true;
            observer.onClose(status, trailers);
          }
        });
      }
    }
  }
}
