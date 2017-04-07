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

package io.grpc.inprocess;

import static com.google.common.base.Preconditions.checkNotNull;

import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.Compressor;
import io.grpc.Decompressor;
import io.grpc.Grpc;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.internal.ClientStream;
import io.grpc.internal.ClientStreamListener;
import io.grpc.internal.ConnectionClientTransport;
import io.grpc.internal.LogId;
import io.grpc.internal.ManagedClientTransport;
import io.grpc.internal.NoopClientStream;
import io.grpc.internal.ServerStream;
import io.grpc.internal.ServerStreamListener;
import io.grpc.internal.ServerTransport;
import io.grpc.internal.ServerTransportListener;
import io.grpc.internal.StatsTraceContext;
import java.io.InputStream;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import java.util.concurrent.Executor;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.CheckReturnValue;
import javax.annotation.concurrent.GuardedBy;
import javax.annotation.concurrent.ThreadSafe;

@ThreadSafe
class InProcessTransport implements ServerTransport, ConnectionClientTransport {
  private static final Logger log = Logger.getLogger(InProcessTransport.class.getName());

  private final LogId logId = LogId.allocate(getClass().getName());
  private final String name;
  private final String authority;
  private ServerTransportListener serverTransportListener;
  private Attributes serverStreamAttributes;
  private ManagedClientTransport.Listener clientTransportListener;
  @GuardedBy("this")
  private boolean shutdown;
  @GuardedBy("this")
  private boolean terminated;
  @GuardedBy("this")
  private Status shutdownStatus;
  @GuardedBy("this")
  private Set<InProcessStream> streams = new HashSet<InProcessStream>();

  public InProcessTransport(String name) {
    this(name, null);
  }

  public InProcessTransport(String name, String authority) {
    this.name = name;
    this.authority = authority;
  }

  @CheckReturnValue
  @Override
  public synchronized Runnable start(ManagedClientTransport.Listener listener) {
    this.clientTransportListener = listener;
    InProcessServer server = InProcessServer.findServer(name);
    if (server != null) {
      serverTransportListener = server.register(this);
    }
    if (serverTransportListener == null) {
      shutdownStatus = Status.UNAVAILABLE.withDescription("Could not find server: " + name);
      final Status localShutdownStatus = shutdownStatus;
      return new Runnable() {
        @Override
        public void run() {
          synchronized (InProcessTransport.this) {
            notifyShutdown(localShutdownStatus);
            notifyTerminated();
          }
        }
      };
    }
    return new Runnable() {
      @Override
      @SuppressWarnings("deprecation")
      public void run() {
        synchronized (InProcessTransport.this) {
          Attributes serverTransportAttrs = Attributes.newBuilder()
              .set(Grpc.TRANSPORT_ATTR_REMOTE_ADDR, new InProcessSocketAddress(name))
              .build();
          serverStreamAttributes = serverTransportListener.transportReady(serverTransportAttrs);
          clientTransportListener.transportReady();
        }
      }
    };
  }

  @Override
  public synchronized ClientStream newStream(
      final MethodDescriptor<?, ?> method, final Metadata headers, final CallOptions callOptions) {
    if (shutdownStatus != null) {
      final Status capturedStatus = shutdownStatus;
      return new NoopClientStream() {
        @Override
        public void start(ClientStreamListener listener) {
          listener.closed(capturedStatus, new Metadata());
        }
      };
    }
    return new InProcessStream(method, headers, authority).clientStream;
  }

  @Override
  public synchronized ClientStream newStream(
      final MethodDescriptor<?, ?> method, final Metadata headers) {
    return newStream(method, headers, CallOptions.DEFAULT);
  }

  @Override
  public synchronized void ping(final PingCallback callback, Executor executor) {
    if (terminated) {
      final Status shutdownStatus = this.shutdownStatus;
      executor.execute(new Runnable() {
        @Override
        public void run() {
          callback.onFailure(shutdownStatus.asRuntimeException());
        }
      });
    } else {
      executor.execute(new Runnable() {
        @Override
        public void run() {
          callback.onSuccess(0);
        }
      });
    }
  }

  @Override
  public synchronized void shutdown() {
    // Can be called multiple times: once for ManagedClientTransport, once for ServerTransport.
    if (shutdown) {
      return;
    }
    shutdownStatus = Status.UNAVAILABLE.withDescription("transport was requested to shut down");
    notifyShutdown(shutdownStatus);
    if (streams.isEmpty()) {
      notifyTerminated();
    }
  }

  @Override
  public void shutdownNow(Status reason) {
    checkNotNull(reason, "reason");
    List<InProcessStream> streamsCopy;
    synchronized (this) {
      shutdown();
      if (terminated) {
        return;
      }
      streamsCopy = new ArrayList<InProcessStream>(streams);
    }
    for (InProcessStream stream : streamsCopy) {
      stream.clientStream.cancel(reason);
    }
  }

  @Override
  public String toString() {
    return getLogId() + "(" + name + ")";
  }

  @Override
  public LogId getLogId() {
    return logId;
  }

  @Override
  public Attributes getAttributes() {
    return Attributes.EMPTY;
  }

  private synchronized void notifyShutdown(Status s) {
    if (shutdown) {
      return;
    }
    shutdown = true;
    clientTransportListener.transportShutdown(s);
  }

  private synchronized void notifyTerminated() {
    if (terminated) {
      return;
    }
    terminated = true;
    clientTransportListener.transportTerminated();
    if (serverTransportListener != null) {
      serverTransportListener.transportTerminated();
    }
  }

  private class InProcessStream {
    private final InProcessServerStream serverStream = new InProcessServerStream();
    private final InProcessClientStream clientStream = new InProcessClientStream();
    private final Metadata headers;
    private final MethodDescriptor<?, ?> method;
    private volatile String authority;

    private InProcessStream(MethodDescriptor<?, ?> method, Metadata headers, String authority) {
      this.method = checkNotNull(method, "method");
      this.headers = checkNotNull(headers, "headers");
      this.authority = authority;
    }

    // Can be called multiple times due to races on both client and server closing at same time.
    private void streamClosed() {
      synchronized (InProcessTransport.this) {
        boolean justRemovedAnElement = streams.remove(this);
        if (streams.isEmpty() && justRemovedAnElement) {
          clientTransportListener.transportInUse(false);
          if (shutdown) {
            notifyTerminated();
          }
        }
      }
    }

    private class InProcessServerStream implements ServerStream {
      @GuardedBy("this")
      private ClientStreamListener clientStreamListener;
      @GuardedBy("this")
      private int clientRequested;
      @GuardedBy("this")
      private ArrayDeque<InputStream> clientReceiveQueue = new ArrayDeque<InputStream>();
      @GuardedBy("this")
      private Status clientNotifyStatus;
      @GuardedBy("this")
      private Metadata clientNotifyTrailers;
      // Only is intended to prevent double-close when client cancels.
      @GuardedBy("this")
      private boolean closed;

      private synchronized void setListener(ClientStreamListener listener) {
        clientStreamListener = listener;
      }

      @Override
      public void setListener(ServerStreamListener serverStreamListener) {
        clientStream.setListener(serverStreamListener);
      }

      @Override
      public void request(int numMessages) {
        boolean onReady = clientStream.serverRequested(numMessages);
        if (onReady) {
          synchronized (this) {
            if (!closed) {
              clientStreamListener.onReady();
            }
          }
        }
      }

      // This method is the only reason we have to synchronize field accesses.
      /**
       * Client requested more messages.
       *
       * @return whether onReady should be called on the server
       */
      private synchronized boolean clientRequested(int numMessages) {
        if (closed) {
          return false;
        }
        boolean previouslyReady = clientRequested > 0;
        clientRequested += numMessages;
        while (clientRequested > 0 && !clientReceiveQueue.isEmpty()) {
          clientRequested--;
          clientStreamListener.messageRead(clientReceiveQueue.poll());
        }
        // Attempt being reentrant-safe
        if (closed) {
          return false;
        }
        if (clientReceiveQueue.isEmpty() && clientNotifyStatus != null) {
          closed = true;
          clientStreamListener.closed(clientNotifyStatus, clientNotifyTrailers);
        }
        boolean nowReady = clientRequested > 0;
        return !previouslyReady && nowReady;
      }

      private void clientCancelled(Status status) {
        internalCancel(status);
      }

      @Override
      public synchronized void writeMessage(InputStream message) {
        if (closed) {
          return;
        }
        if (clientRequested > 0) {
          clientRequested--;
          clientStreamListener.messageRead(message);
        } else {
          clientReceiveQueue.add(message);
        }
      }

      @Override
      public void flush() {}

      @Override
      public synchronized boolean isReady() {
        if (closed) {
          return false;
        }
        return clientRequested > 0;
      }

      @Override
      public synchronized void writeHeaders(Metadata headers) {
        if (closed) {
          return;
        }
        clientStreamListener.headersRead(headers);
      }

      @Override
      public void close(Status status, Metadata trailers) {
        status = stripCause(status);
        synchronized (this) {
          if (closed) {
            return;
          }
          if (clientReceiveQueue.isEmpty()) {
            closed = true;
            clientStreamListener.closed(status, trailers);
          } else {
            clientNotifyStatus = status;
            clientNotifyTrailers = trailers;
          }
        }

        clientStream.serverClosed(Status.OK);
        streamClosed();
      }

      @Override
      public void cancel(Status status) {
        if (!internalCancel(Status.CANCELLED.withDescription("server cancelled stream"))) {
          return;
        }
        clientStream.serverClosed(status);
        streamClosed();
      }

      private synchronized boolean internalCancel(Status status) {
        if (closed) {
          return false;
        }
        closed = true;
        InputStream stream;
        while ((stream = clientReceiveQueue.poll()) != null) {
          try {
            stream.close();
          } catch (Throwable t) {
            log.log(Level.WARNING, "Exception closing stream", t);
          }
        }
        clientStreamListener.closed(status, new Metadata());
        return true;
      }

      @Override
      public void setMessageCompression(boolean enable) {
         // noop
      }

      @Override
      public void setCompressor(Compressor compressor) {}

      @Override
      public void setDecompressor(Decompressor decompressor) {}

      @Override public Attributes getAttributes() {
        return serverStreamAttributes;
      }

      @Override
      public String getAuthority() {
        return InProcessStream.this.authority;
      }

      @Override
      public StatsTraceContext statsTraceContext() {
        // TODO(zhangkun83): InProcessTransport by-passes framer and deframer, thus message sizses
        // are not counted.  Therefore Stats is currently disabled.
        // (https://github.com/grpc/grpc-java/issues/2284)
        return StatsTraceContext.NOOP;
      }
    }

    private class InProcessClientStream implements ClientStream {
      @GuardedBy("this")
      private ServerStreamListener serverStreamListener;
      @GuardedBy("this")
      private int serverRequested;
      @GuardedBy("this")
      private ArrayDeque<InputStream> serverReceiveQueue = new ArrayDeque<InputStream>();
      @GuardedBy("this")
      private boolean serverNotifyHalfClose;
      // Only is intended to prevent double-close when server closes.
      @GuardedBy("this")
      private boolean closed;

      private synchronized void setListener(ServerStreamListener listener) {
        this.serverStreamListener = listener;
      }

      @Override
      public void request(int numMessages) {
        boolean onReady = serverStream.clientRequested(numMessages);
        if (onReady) {
          synchronized (this) {
            if (!closed) {
              serverStreamListener.onReady();
            }
          }
        }
      }

      // This method is the only reason we have to synchronize field accesses.
      /**
       * Client requested more messages.
       *
       * @return whether onReady should be called on the server
       */
      private synchronized boolean serverRequested(int numMessages) {
        if (closed) {
          return false;
        }
        boolean previouslyReady = serverRequested > 0;
        serverRequested += numMessages;
        while (serverRequested > 0 && !serverReceiveQueue.isEmpty()) {
          serverRequested--;
          serverStreamListener.messageRead(serverReceiveQueue.poll());
        }
        if (serverReceiveQueue.isEmpty() && serverNotifyHalfClose) {
          serverNotifyHalfClose = false;
          serverStreamListener.halfClosed();
        }
        boolean nowReady = serverRequested > 0;
        return !previouslyReady && nowReady;
      }

      private void serverClosed(Status status) {
        internalCancel(status);
      }

      @Override
      public synchronized void writeMessage(InputStream message) {
        if (closed) {
          return;
        }
        if (serverRequested > 0) {
          serverRequested--;
          serverStreamListener.messageRead(message);
        } else {
          serverReceiveQueue.add(message);
        }
      }

      @Override
      public void flush() {}

      @Override
      public synchronized boolean isReady() {
        if (closed) {
          return false;
        }
        return serverRequested > 0;
      }

      // Must be thread-safe for shutdownNow()
      @Override
      public void cancel(Status reason) {
        if (!internalCancel(stripCause(reason))) {
          return;
        }
        serverStream.clientCancelled(reason);
        streamClosed();
      }

      private synchronized boolean internalCancel(Status reason) {
        if (closed) {
          return false;
        }
        closed = true;
        InputStream stream;
        while ((stream = serverReceiveQueue.poll()) != null) {
          try {
            stream.close();
          } catch (Throwable t) {
            log.log(Level.WARNING, "Exception closing stream", t);
          }
        }
        serverStreamListener.closed(reason);
        return true;
      }

      @Override
      public synchronized void halfClose() {
        if (closed) {
          return;
        }
        if (serverReceiveQueue.isEmpty()) {
          serverStreamListener.halfClosed();
        } else {
          serverNotifyHalfClose = true;
        }
      }

      @Override
      public void setMessageCompression(boolean enable) {}

      @Override
      public void setAuthority(String string) {
        InProcessStream.this.authority = string;
      }

      @Override
      public void start(ClientStreamListener listener) {
        serverStream.setListener(listener);

        synchronized (InProcessTransport.this) {
          streams.add(InProcessTransport.InProcessStream.this);
          if (streams.size() == 1) {
            clientTransportListener.transportInUse(true);
          }
          serverTransportListener.streamCreated(serverStream, method.getFullMethodName(), headers);
        }
      }

      @Override
      public Attributes getAttributes() {
        return Attributes.EMPTY;
      }

      @Override
      public void setCompressor(Compressor compressor) {}

      @Override
      public void setDecompressor(Decompressor decompressor) {}

      @Override
      public void setMaxInboundMessageSize(int maxSize) {}

      @Override
      public void setMaxOutboundMessageSize(int maxSize) {}
    }
  }

  /**
   * Returns a new status with the same code and description, but stripped of any other information
   * (i.e. cause).
   *
   * <p>This is, so that the InProcess transport behaves in the same way as the other transports,
   * when exchanging statuses between client and server and vice versa.
   */
  private static Status stripCause(Status status) {
    if (status == null) {
      return null;
    }
    return Status
        .fromCodeValue(status.getCode().value())
        .withDescription(status.getDescription());
  }
}
