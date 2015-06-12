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

import io.grpc.transport.ServerListener;
import io.grpc.transport.ServerStream;
import io.grpc.transport.ServerStreamListener;
import io.grpc.transport.ServerTransport;
import io.grpc.transport.ServerTransportListener;

import java.io.IOException;
import java.io.InputStream;
import java.util.Collection;
import java.util.HashSet;
import java.util.concurrent.Executor;
import java.util.concurrent.TimeUnit;

/**
 * Default implementation of {@link Server}, for creation by transports.
 *
 * <p>Expected usage (by a theoretical TCP transport):
 * <pre><code>public class TcpTransportServerFactory {
 *   public static Server newServer(Executor executor, HandlerRegistry registry,
 *       String configuration) {
 *     return new ServerImpl(executor, registry, new TcpTransportServer(configuration));
 *   }
 * }</code></pre>
 *
 * <p>Starting the server starts the underlying transport for servicing requests. Stopping the
 * server stops servicing new requests and waits for all connections to terminate.
 */
public final class ServerImpl extends Server {
  private static final ServerStreamListener NOOP_LISTENER = new NoopListener();

  /** Executor for application processing. */
  private final Executor executor;
  private final HandlerRegistry registry;
  private boolean started;
  private boolean shutdown;
  private boolean terminated;
  private Runnable terminationRunnable;
  /** Service encapsulating something similar to an accept() socket. */
  private final io.grpc.transport.Server transportServer;
  private boolean transportServerTerminated;
  /** {@code transportServer} and services encapsulating something similar to a TCP connection. */
  private final Collection<ServerTransport> transports = new HashSet<ServerTransport>();

  /**
   * Construct a server.
   *
   * @param executor to call methods on behalf of remote clients
   * @param registry of methods to expose to remote clients.
   */
  public ServerImpl(Executor executor, HandlerRegistry registry,
      io.grpc.transport.Server transportServer) {
    this.executor = Preconditions.checkNotNull(executor, "executor");
    this.registry = Preconditions.checkNotNull(registry, "registry");
    this.transportServer = Preconditions.checkNotNull(transportServer, "transportServer");
  }

  /** Hack to allow executors to auto-shutdown. Not for general use. */
  // TODO(ejona86): Replace with a real API.
  synchronized void setTerminationRunnable(Runnable runnable) {
    this.terminationRunnable = runnable;
  }

  /**
   * Bind and start the server.
   *
   * @return {@code this} object
   * @throws IllegalStateException if already started
   * @throws IOException if unable to bind
   */
  public synchronized ServerImpl start() throws IOException {
    if (started) {
      throw new IllegalStateException("Already started");
    }
    // Start and wait for any port to actually be bound.
    transportServer.start(new ServerListenerImpl());
    started = true;
    return this;
  }

  /**
   * Initiates an orderly shutdown in which preexisting calls continue but new calls are rejected.
   */
  public synchronized ServerImpl shutdown() {
    if (shutdown) {
      return this;
    }
    transportServer.shutdown();
    shutdown = true;
    return this;
  }

  /**
   * Initiates a forceful shutdown in which preexisting and new calls are rejected. Although
   * forceful, the shutdown process is still not instantaneous; {@link #isTerminated()} will likely
   * return {@code false} immediately after this method returns.
   *
   * <p>NOT YET IMPLEMENTED. This method currently behaves identically to shutdown().
   */
  // TODO(ejona86): cancel preexisting calls.
  public synchronized ServerImpl shutdownNow() {
    shutdown();
    return this;
  }

  /**
   * Returns whether the server is shutdown. Shutdown servers reject any new calls, but may still
   * have some calls being processed.
   *
   * @see #shutdown()
   * @see #isTerminated()
   */
  public synchronized boolean isShutdown() {
    return shutdown;
  }

  /**
   * Waits for the server to become terminated, giving up if the timeout is reached.
   *
   * @return whether the server is terminated, as would be done by {@link #isTerminated()}.
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
   * Waits for the server to become terminated.
   */
  public synchronized void awaitTerminated() throws InterruptedException {
    while (!terminated) {
      wait();
    }
  }

  /**
   * Returns whether the server is terminated. Terminated servers have no running calls and
   * relevant resources released (like TCP connections).
   *
   * @see #isShutdown()
   */
  public synchronized boolean isTerminated() {
    return terminated;
  }

  /**
   * Remove transport service from accounting collection and notify of complete shutdown if
   * necessary.
   *
   * @param transport service to remove
   */
  private synchronized void transportClosed(ServerTransport transport) {
    if (!transports.remove(transport)) {
      throw new AssertionError("Transport already removed");
    }
    checkForTermination();
  }

  /** Notify of complete shutdown if necessary. */
  private synchronized void checkForTermination() {
    if (shutdown && transports.isEmpty() && transportServerTerminated) {
      if (terminated) {
        throw new AssertionError("Server already terminated");
      }
      terminated = true;
      notifyAll();
      if (terminationRunnable != null) {
        terminationRunnable.run();
      }
    }
  }

  private class ServerListenerImpl implements ServerListener {
    @Override
    public ServerTransportListener transportCreated(ServerTransport transport) {
      synchronized (ServerImpl.this) {
        transports.add(transport);
      }
      return new ServerTransportListenerImpl(transport);
    }

    @Override
    public void serverShutdown() {
      synchronized (ServerImpl.this) {
        // transports collection can be modified during shutdown(), even if we hold the lock, due
        // to reentrancy.
        for (ServerTransport transport
            : transports.toArray(new ServerTransport[transports.size()])) {
          transport.shutdown();
        }
        transportServerTerminated = true;
        checkForTermination();
      }
    }
  }

  private class ServerTransportListenerImpl implements ServerTransportListener {
    private final ServerTransport transport;

    public ServerTransportListenerImpl(ServerTransport transport) {
      this.transport = transport;
    }

    @Override
    public void transportTerminated() {
      transportClosed(transport);
    }

    @Override
    public ServerStreamListener streamCreated(final ServerStream stream, final String methodName,
        final Metadata.Headers headers) {
      SerializingExecutor serializingExecutor = new SerializingExecutor(executor);
      final JumpToApplicationThreadServerStreamListener jumpListener
          = new JumpToApplicationThreadServerStreamListener(serializingExecutor, stream);
      // Run in serializingExecutor so jumpListener.setListener() is called before any callbacks
      // are delivered, including any errors. Callbacks can still be triggered, but they will be
      // queued.
      serializingExecutor.execute(new Runnable() {
            @Override
            public void run() {
              ServerStreamListener listener = NOOP_LISTENER;
              try {
                HandlerRegistry.Method method = registry.lookupMethod(methodName);
                if (method == null) {
                  stream.close(
                      Status.UNIMPLEMENTED.withDescription("Method not found: " + methodName),
                      new Metadata.Trailers());
                  return;
                }
                listener = startCall(stream, methodName, method.getMethodDefinition(), headers);
              } catch (Throwable t) {
                stream.close(Status.fromThrowable(t), new Metadata.Trailers());
                throw Throwables.propagate(t);
              } finally {
                jumpListener.setListener(listener);
              }
            }
          });
      return jumpListener;
    }

    /** Never returns {@code null}. */
    private <ReqT, RespT> ServerStreamListener startCall(ServerStream stream, String fullMethodName,
        ServerMethodDefinition<ReqT, RespT> methodDef, Metadata.Headers headers) {
      // TODO(ejona86): should we update fullMethodName to have the canonical path of the method?
      final ServerCallImpl<ReqT, RespT> call = new ServerCallImpl<ReqT, RespT>(stream, methodDef);
      ServerCall.Listener<ReqT> listener
          = methodDef.getServerCallHandler().startCall(fullMethodName, call, headers);
      if (listener == null) {
        throw new NullPointerException(
            "startCall() returned a null listener for method " + fullMethodName);
      }
      return call.newServerStreamListener(listener);
    }
  }

  private static class NoopListener implements ServerStreamListener {
    @Override
    public void messageRead(InputStream value) {
      try {
        value.close();
      } catch (IOException e) {
        throw new RuntimeException(e);
      }
    }

    @Override
    public void halfClosed() {}

    @Override
    public void closed(Status status) {}

    @Override
    public void onReady() {}
  }

  /**
   * Dispatches callbacks onto an application-provided executor and correctly propagates
   * exceptions.
   */
  private static class JumpToApplicationThreadServerStreamListener implements ServerStreamListener {
    private final SerializingExecutor callExecutor;
    private final ServerStream stream;
    // Only accessed from callExecutor.
    private ServerStreamListener listener;

    public JumpToApplicationThreadServerStreamListener(SerializingExecutor executor,
        ServerStream stream) {
      this.callExecutor = executor;
      this.stream = stream;
    }

    private ServerStreamListener getListener() {
      if (listener == null) {
        throw new IllegalStateException("listener unset");
      }
      return listener;
    }

    private void setListener(ServerStreamListener listener) {
      Preconditions.checkNotNull(listener, "listener must not be null");
      Preconditions.checkState(this.listener == null, "Listener already set");
      this.listener = listener;
    }

    /**
     * Like {@link ServerCall#close(Status, Metadata.Trailers)}, but thread-safe for internal use.
     */
    private void internalClose(Status status, Metadata.Trailers trailers) {
      // TODO(ejona86): this is not thread-safe :)
      stream.close(status, trailers);
    }

    @Override
    public void messageRead(final InputStream message) {
      callExecutor.execute(new Runnable() {
        @Override
        public void run() {
          try {
            getListener().messageRead(message);
          } catch (Throwable t) {
            internalClose(Status.fromThrowable(t), new Metadata.Trailers());
            throw Throwables.propagate(t);
          }
        }
      });
    }

    @Override
    public void halfClosed() {
      callExecutor.execute(new Runnable() {
        @Override
        public void run() {
          try {
            getListener().halfClosed();
          } catch (Throwable t) {
            internalClose(Status.fromThrowable(t), new Metadata.Trailers());
            throw Throwables.propagate(t);
          }
        }
      });
    }

    @Override
    public void closed(final Status status) {
      callExecutor.execute(new Runnable() {
        @Override
        public void run() {
          getListener().closed(status);
        }
      });
    }

    @Override
    public void onReady() {
      callExecutor.execute(new Runnable() {
        @Override
        public void run() {
          getListener().onReady();
        }
      });
    }
  }

  private class ServerCallImpl<ReqT, RespT> extends ServerCall<RespT> {
    private final ServerStream stream;
    private final ServerMethodDefinition<ReqT, RespT> methodDef;
    private volatile boolean cancelled;

    public ServerCallImpl(ServerStream stream, ServerMethodDefinition<ReqT, RespT> methodDef) {
      this.stream = stream;
      this.methodDef = methodDef;
    }

    @Override
    public void request(int numMessages) {
      stream.request(numMessages);
    }

    @Override
    public void sendHeaders(Metadata.Headers headers) {
      stream.writeHeaders(headers);
    }

    @Override
    public void sendPayload(RespT payload) {
      try {
        InputStream message = methodDef.streamResponse(payload);
        stream.writeMessage(message);
        stream.flush();
      } catch (Throwable t) {
        close(Status.fromThrowable(t), new Metadata.Trailers());
        throw Throwables.propagate(t);
      }
    }

    @Override
    public boolean isReady() {
      return stream.isReady();
    }

    @Override
    public void close(Status status, Metadata.Trailers trailers) {
      stream.close(status, trailers);
    }

    @Override
    public boolean isCancelled() {
      return cancelled;
    }

    private ServerStreamListenerImpl newServerStreamListener(ServerCall.Listener<ReqT> listener) {
      return new ServerStreamListenerImpl(listener);
    }

    /**
     * All of these callbacks are assumed to called on an application thread, and the caller is
     * responsible for handling thrown exceptions.
     */
    private class ServerStreamListenerImpl implements ServerStreamListener {
      private final ServerCall.Listener<ReqT> listener;

      public ServerStreamListenerImpl(ServerCall.Listener<ReqT> listener) {
        this.listener = Preconditions.checkNotNull(listener, "listener must not be null");
      }

      @Override
      public void messageRead(final InputStream message) {
        try {
          if (cancelled) {
            return;
          }

          listener.onPayload(methodDef.parseRequest(message));
        } finally {
          try {
            message.close();
          } catch (IOException e) {
            throw new RuntimeException(e);
          }
        }
      }

      @Override
      public void halfClosed() {
        if (cancelled) {
          return;
        }

        listener.onHalfClose();
      }

      @Override
      public void closed(Status status) {
        if (status.isOk()) {
          listener.onComplete();
        } else {
          cancelled = true;
          listener.onCancel();
        }
      }

      @Override
      public void onReady() {
        if (cancelled) {
          return;
        }
        listener.onReady();
      }
    }
  }
}
