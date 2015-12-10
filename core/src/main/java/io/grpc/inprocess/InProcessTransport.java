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

import io.grpc.Compressor;
import io.grpc.CompressorRegistry;
import io.grpc.DecompressorRegistry;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.internal.ClientStream;
import io.grpc.internal.ClientStreamListener;
import io.grpc.internal.ClientTransport;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.NoopClientStream;
import io.grpc.internal.ServerStream;
import io.grpc.internal.ServerStreamListener;
import io.grpc.internal.ServerTransport;
import io.grpc.internal.ServerTransportListener;

import java.io.InputStream;
import java.util.ArrayDeque;
import java.util.HashSet;
import java.util.Set;
import java.util.concurrent.Executor;
import java.util.logging.Level;
import java.util.logging.Logger;

import javax.annotation.concurrent.GuardedBy;
import javax.annotation.concurrent.ThreadSafe;

@ThreadSafe
class InProcessTransport implements ServerTransport, ClientTransport {
  private static final Logger log = Logger.getLogger(InProcessTransport.class.getName());

  private final String name;
  private ServerTransportListener serverTransportListener;
  private ClientTransport.Listener clientTransportListener;
  @GuardedBy("this")
  private boolean shutdown;
  @GuardedBy("this")
  private boolean terminated;
  @GuardedBy("this")
  private Status shutdownStatus;
  @GuardedBy("this")
  private Set<InProcessStream> streams = new HashSet<InProcessStream>();

  public InProcessTransport(String name) {
    this.name = name;
  }

  @Override
  public synchronized void start(ClientTransport.Listener listener) {
    this.clientTransportListener = listener;
    InProcessServer server = InProcessServer.findServer(name);
    if (server != null) {
      serverTransportListener = server.register(this);
    }
    if (serverTransportListener == null) {
      shutdownStatus = Status.UNAVAILABLE.withDescription("Could not find server: " + name);
      final Status localShutdownStatus = shutdownStatus;
      Thread shutdownThread = new Thread(new Runnable() {
        @Override
        public void run() {
          synchronized (InProcessTransport.this) {
            notifyShutdown(localShutdownStatus);
            notifyTerminated();
          }
        }
      });
      shutdownThread.setDaemon(true);
      shutdownThread.setName("grpc-inprocess-shutdown");
      shutdownThread.start();
    }
    Thread readyThread = new Thread(new Runnable() {
      @Override
      public void run() {
        synchronized (InProcessTransport.this) {
          clientTransportListener.transportReady();
        }
      }
    });
    readyThread.setDaemon(true);
    readyThread.setName("grpc-inprocess-ready");
    readyThread.start();
  }

  @Override
  public synchronized ClientStream newStream(MethodDescriptor<?, ?> method,
      Metadata headers, ClientStreamListener clientStreamListener) {
    if (shutdownStatus != null) {
      clientStreamListener.closed(shutdownStatus, new Metadata());
      return new NoopClientStream();
    }
    headers.removeAll(GrpcUtil.AUTHORITY_KEY);
    InProcessStream stream = new InProcessStream();
    stream.serverStream.setListener(clientStreamListener);
    ServerStreamListener serverStreamListener = serverTransportListener.streamCreated(
        stream.serverStream, method.getFullMethodName(), headers);
    stream.clientStream.setListener(serverStreamListener);
    streams.add(stream);
    return stream.clientStream;
  }

  @Override
  public synchronized void ping(final PingCallback callback, Executor executor) {
    if (terminated) {
      final Status shutdownStatus = this.shutdownStatus;
      executor.execute(new Runnable() {
        @Override
        public void run() {
          callback.pingFailed(shutdownStatus.asRuntimeException());
        }
      });
    } else {
      executor.execute(new Runnable() {
        @Override
        public void run() {
          callback.pingAcknowledged(0);
        }
      });
    }
  }

  @Override
  public synchronized void shutdown() {
    // Can be called multiple times: once for ClientTransport, once for ServerTransport.
    if (shutdown) {
      return;
    }
    shutdownStatus = Status.UNAVAILABLE.withDescription("transport was requested to shut down");
    notifyShutdown(Status.OK.withDescription(shutdownStatus.getDescription()));
    if (streams.isEmpty()) {
      notifyTerminated();
    }
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

    // Can be called multiple times due to races on both client and server closing at same time.
    private void streamClosed() {
      synchronized (InProcessTransport.this) {
        boolean justRemovedAnElement = streams.remove(this);
        if (shutdown && streams.isEmpty() && justRemovedAnElement) {
          notifyTerminated();
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
      public void setDecompressionRegistry(DecompressorRegistry registry) {}

      @Override
      public void request(int numMessages) {
        clientStream.serverRequested(numMessages);
      }

      // This method is the only reason we have to synchronize field accesses.
      private synchronized void clientRequested(int numMessages) {
        if (closed) {
          return;
        }
        clientRequested += numMessages;
        while (clientRequested > 0 && !clientReceiveQueue.isEmpty()) {
          clientRequested--;
          clientStreamListener.messageRead(clientReceiveQueue.poll());
        }
        // Attempt being reentrant-safe
        if (closed) {
          return;
        }
        if (clientReceiveQueue.isEmpty() && clientNotifyStatus != null) {
          closed = true;
          clientStreamListener.closed(clientNotifyStatus, clientNotifyTrailers);
        }
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
      public Compressor pickCompressor(Iterable<String> messageEncodings) {
        return null;
      }

      @Override
      public void setCompressionRegistry(CompressorRegistry registry) {}
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
        serverStream.clientRequested(numMessages);
      }

      // This method is the only reason we have to synchronize field accesses.
      private synchronized void serverRequested(int numMessages) {
        if (closed) {
          return;
        }
        serverRequested += numMessages;
        while (serverRequested > 0 && !serverReceiveQueue.isEmpty()) {
          serverRequested--;
          serverStreamListener.messageRead(serverReceiveQueue.poll());
        }
        if (serverReceiveQueue.isEmpty() && serverNotifyHalfClose) {
          serverNotifyHalfClose = false;
          serverStreamListener.halfClosed();
        }
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

      @Override
      public void cancel(Status reason) {
        if (!internalCancel(reason)) {
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
      public void setDecompressionRegistry(DecompressorRegistry registry) {}

      @Override
      public void setMessageCompression(boolean enable) {}

      @Override
      public Compressor pickCompressor(Iterable<String> messageEncodings) {
        return null;
      }

      @Override
      public void setCompressionRegistry(CompressorRegistry registry) {}
    }
  }
}
