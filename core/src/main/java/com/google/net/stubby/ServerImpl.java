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

package com.google.net.stubby;

import com.google.common.base.Preconditions;
import com.google.common.base.Throwables;
import com.google.common.util.concurrent.AbstractService;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.Service;
import com.google.net.stubby.transport.ServerListener;
import com.google.net.stubby.transport.ServerStream;
import com.google.net.stubby.transport.ServerStreamListener;
import com.google.net.stubby.transport.ServerTransportListener;

import java.io.IOException;
import java.io.InputStream;
import java.util.Collection;
import java.util.Collections;
import java.util.HashSet;
import java.util.concurrent.Executor;

/**
 * Default implementation of {@link Server}, for creation by transports.
 *
 * <p>Expected usage (by a theoretical TCP transport):
 * <pre><code>public class TcpTransportServerFactory {
 *   public static Server newServer(Executor executor, HandlerRegistry registry,
 *       String configuration) {
 *     ServerImpl server = new ServerImpl(executor, registry);
 *     return server.setTransportServer(
 *         new TcpTransportServer(server.serverListener(), configuration));
 *   }
 * }</code></pre>
 *
 * <p>Starting the server starts the underlying transport for servicing requests. Stopping the
 * server stops servicing new requests and waits for all connections to terminate.
 */
public class ServerImpl extends AbstractService implements Server {
  private static final ServerStreamListener NOOP_LISTENER = new NoopListener();

  private final ServerListener serverListener = new ServerListenerImpl();
  private final ServerTransportListener serverTransportListener = new ServerTransportListenerImpl();
  /** Executor for application processing. */
  private final Executor executor;
  private final HandlerRegistry registry;
  /** Service encapsulating something similar to an accept() socket. */
  private Service transportServer;
  /** {@code transportServer} and services encapsulating something similar to a TCP connection. */
  private final Collection<Service> transports
      = Collections.synchronizedSet(new HashSet<Service>());

  /**
   * Construct a server. {@link #setTransportServer(Service)} must be called before starting the
   * server.
   *
   * @param executor
   */
  public ServerImpl(Executor executor, HandlerRegistry registry) {
    this.executor = Preconditions.checkNotNull(executor);
    this.registry = Preconditions.checkNotNull(registry);
  }

  /**
   * Set the transport server for the server. {@code transportServer} should be in state NEW and not
   * shared with any other {@code Server}s; it will be started and managed by the newly-created
   * server instance. Must be called before starting server.
   *
   * @return this object
   */
  public ServerImpl setTransportServer(Service transportServer) {
    Preconditions.checkState(state() == Server.State.NEW, "server must be in NEW state");
    Preconditions.checkState(this.transportServer == null, "transportServer already set");
    this.transportServer = Preconditions.checkNotNull(transportServer);
    Preconditions.checkArgument(
        transportServer.state() == Server.State.NEW, "transport server not in NEW state");
    transportServer.addListener(new TransportLifecycleListener(), MoreExecutors.directExecutor());
    transports.add(transportServer);
    // We assume that transport.state() won't change by another thread before we return from this
    // call.
    Preconditions.checkState(
        transportServer.state() == Server.State.NEW, "transport server changed state!");
    return this;
  }

  /** Listener to be called by transport factories to notify of new transport instances. */
  public ServerListener serverListener() {
    return serverListener;
  }

  @Override
  protected void doStart() {
    Preconditions.checkState(transportServer != null, "setTransportServer not called");
    transportServer.startAsync();
  }

  @Override
  protected void doStop() {
    stopTransports();
  }

  /**
   * Remove transport service from accounting list and notify of complete shutdown if necessary.
   *
   * @param transport service to remove
   * @return {@code true} if shutting down and it is now complete
   */
  private boolean transportClosed(Service transport) {
    boolean shutdownComplete;
    synchronized (transports) {
      if (!transports.remove(transport)) {
        throw new AssertionError("Transport already removed");
      }
      shutdownComplete = transports.isEmpty();
    }
    if (shutdownComplete) {
      Service.State state = state();
      if (state == Service.State.STOPPING) {
        notifyStopped();
      } else if (state == Service.State.FAILED) {
        // NOOP: already failed
      } else {
        notifyFailed(new IllegalStateException("server transport terminated unexpectedly"));
      }
    }
    return shutdownComplete;
  }

  /**
   * The transport server closed, so cleanup its resources and start shutdown.
   */
  private void transportServerClosed() {
    boolean shutdownComplete = transportClosed(transportServer);
    if (shutdownComplete) {
      return;
    }
    stopTransports();
  }

  /**
   * Shutdown all transports (including transportServer). Safe to be called even if previously
   * called.
   */
  private void stopTransports() {
    for (Service transport : transports.toArray(new Service[0])) {
      // transports list can be modified during this call, even if we hold the lock, due to
      // reentrancy.
      transport.stopAsync();
    }
  }

  private class ServerListenerImpl implements ServerListener {
    @Override
    public ServerTransportListener transportCreated(Service transport) {
      Service.State transportState = transport.state();
      Preconditions.checkArgument(
          transportState == Service.State.STARTING || transportState == Service.State.RUNNING,
          "Created transport should be starting or running");
      if (state() != Server.State.RUNNING) {
        transport.stopAsync();
        return serverTransportListener;
      }
      transports.add(transport);
      // transports list can be modified during this call, even if we hold the lock, due to
      // reentrancy.
      transport.addListener(new TransportServiceListener(transport),
          MoreExecutors.directExecutor());
      // We assume that transport.state() won't change by another thread before the listener was
      // registered.
      Preconditions.checkState(
          transport.state() == transportState, "transport changed state unexpectedly!");
      return serverTransportListener;
    }
  }

  /** Listens for lifecycle changes to the "accept() socket." */
  private class TransportLifecycleListener extends Service.Listener {
    @Override
    public void running() {
      notifyStarted();
    }

    @Override
    public void terminated(Service.State from) {
      transportServerClosed();
    }

    @Override
    public void failed(Service.State from, Throwable failure) {
      // TODO(ejona): Ideally we would want to force-stop transports before notifying application of
      // failure, but that would cause us to have an unrepresentative state since we would be
      // RUNNING but not accepting connections.
      notifyFailed(failure);
      transportServerClosed();
    }
  }

  /** Listens for lifecycle changes to a "TCP connection." */
  private class TransportServiceListener extends Service.Listener {
    private final Service transport;

    public TransportServiceListener(Service transport) {
      this.transport = transport;
    }

    @Override
    public void failed(Service.State from, Throwable failure) {
      transportClosed(transport);
    }

    @Override
    public void terminated(Service.State from) {
      transportClosed(transport);
    }
  }

  private class ServerTransportListenerImpl implements ServerTransportListener {
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
      // TODO(ejona): should we update fullMethodName to have the canonical path of the method?
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
    public void messageRead(InputStream value, int length) {
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
      // TODO(ejona): this is not thread-safe :)
      stream.close(status, trailers);
    }

    @Override
    public void messageRead(final InputStream message, final int length) {
      callExecutor.execute(new Runnable() {
        @Override
        public void run() {
          try {
            getListener().messageRead(message, length);
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
        stream.writeMessage(message, message.available(), null);
        stream.flush();
      } catch (Throwable t) {
        close(Status.fromThrowable(t), new Metadata.Trailers());
        throw Throwables.propagate(t);
      }
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
      public void messageRead(final InputStream message, int length) {
        if (cancelled) {
          return;
        }

        try {
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
    }
  }
}
